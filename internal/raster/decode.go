package raster

import (
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// decoders is indexed by Format. It is a variable so that the tests can
// inject a panicking decoder.
var decoders = [...]func(io.Reader) (image.Image, error){
	PNG:  png.Decode,
	JPEG: jpeg.Decode,
}

// errGrayJPEG is returned by WriteJPEG for a gray image that image/jpeg would
// encode with 3 components; Decode never produces one from a JPEG source.
var errGrayJPEG = errors.New("raster: gray image is not *image.Gray")

// Decoded is a decoded image in 8-bit samples, ready for the PDF writer.
type Decoded struct {
	Width, Height int
	Gray          bool // DeviceGray, else DeviceRGB
	Alpha         bool // at least one pixel is not opaque

	img image.Image
}

// Decode decodes s fully. It charges s's Work before decoding (so a LimitError comes
// before any CPU is spent) and recovers decoder panics. Errors: *RejectError
// (ReasonCorrupt, also when the decoded bounds differ from the header) or a budget error.
func Decode(s *Source, b *budget.Budget) (*Decoded, error) {
	if err := b.Charge(budget.Work, s.work()); err != nil {
		return nil, err
	}
	if err := b.Poll(); err != nil {
		return nil, err
	}
	img, err := decodeImage(s)
	if err != nil {
		return nil, err
	}
	if cmyk, ok := img.(*image.CMYK); ok {
		img = cmykToRGBA(cmyk)
	}
	return &Decoded{
		Width:  s.Width,
		Height: s.Height,
		Gray:   s.gray,
		Alpha:  hasAlpha(img),
		img:    img,
	}, nil
}

// decodeImage runs the decoder of s.Format. The decoders are not trusted to
// stay within their contract on hostile data: a panic or bounds that differ
// from the header reject the image.
func decodeImage(s *Source) (img image.Image, err error) {
	defer func() {
		if recover() != nil {
			img, err = nil, reject(ReasonCorrupt)
		}
	}()
	img, err = decoders[s.Format](s.open())
	if err != nil || img == nil || img.Bounds() != image.Rect(0, 0, s.Width, s.Height) {
		return nil, reject(ReasonCorrupt)
	}
	return img, nil
}

// cmykToRGBA converts m in place to an opaque RGBA image with the naive
// formula of color.CMYKToRGB: no allocation, and image/jpeg encodes an RGBA
// image without its per-pixel interface path.
func cmykToRGBA(m *image.CMYK) *image.RGBA {
	for y := m.Rect.Min.Y; y < m.Rect.Max.Y; y++ {
		row := m.Pix[m.PixOffset(m.Rect.Min.X, y):][:4*m.Rect.Dx()]
		for i := 0; i < len(row); i += 4 {
			row[i], row[i+1], row[i+2] = color.CMYKToRGB(row[i], row[i+1], row[i+2], row[i+3])
			row[i+3] = 0xff
		}
	}
	return &image.RGBA{Pix: m.Pix, Stride: m.Stride, Rect: m.Rect}
}

// hasAlpha reports whether a pixel of m has an 8-bit alpha below 255. Only the
// types image/png produces with an alpha channel can have one.
func hasAlpha(m image.Image) bool {
	b := m.Bounds()
	switch m := m.(type) {
	case *image.NRGBA:
		return anyBelow(m.Pix, m.Stride, b.Dx()*4, 3, 4, b.Dy())
	case *image.RGBA:
		return anyBelow(m.Pix, m.Stride, b.Dx()*4, 3, 4, b.Dy())
	case *image.NRGBA64:
		return anyBelow(m.Pix, m.Stride, b.Dx()*8, 6, 8, b.Dy())
	case *image.RGBA64:
		return anyBelow(m.Pix, m.Stride, b.Dx()*8, 6, 8, b.Dy())
	case *image.Paletted:
		table := paletteTable(m.Palette)
		for y := range b.Dy() {
			for _, i := range m.Pix[y*m.Stride:][:b.Dx()] {
				if table[i][3] != 0xff {
					return true
				}
			}
		}
	case *image.Gray, *image.Gray16, *image.YCbCr:
	default:
		buf := make([]byte, 4*b.Dx())
		for y := range b.Dy() {
			genericRow(m, b.Min.Y+y, buf)
			if anyBelow(buf, 0, len(buf), 3, 4, 1) {
				return true
			}
		}
	}
	return false
}

// anyBelow reports whether a byte at offset off of a pixel of step bytes, in
// rows of rowLen bytes, is below 0xff.
func anyBelow(pix []byte, stride, rowLen, off, step, rows int) bool {
	for y := range rows {
		row := pix[y*stride:][:rowLen]
		for i := off; i < len(row); i += step {
			if row[i] != 0xff {
				return true
			}
		}
	}
	return false
}

// paletteTable converts p to non-premultiplied RGBA. Entries past the
// palette are opaque black, so a hostile index never reads out of bounds.
func paletteTable(p color.Palette) *[256][4]byte {
	var t [256][4]byte
	for i := range t {
		t[i] = [4]byte{0, 0, 0, 0xff}
	}
	for i, c := range p[:min(len(p), len(t))] {
		n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
		t[i] = [4]byte{n.R, n.G, n.B, n.A}
	}
	return &t
}

// WriteSamples writes Height rows of Width×(1|3) bytes: 8-bit gray when Gray,
// else RGB, not premultiplied. It allocates one row.
func (d *Decoded) WriteSamples(w io.Writer) error {
	if d.Gray {
		return d.writeRows(w, packGray)
	}
	return d.writeRows(w, packRGB)
}

// WriteAlpha writes Height rows of Width bytes: the 8-bit alpha of each pixel.
// It allocates one row.
func (d *Decoded) WriteAlpha(w io.Writer) error {
	return d.writeRows(w, packAlpha)
}

// WriteJPEG re-encodes the image through image/jpeg at JPEGQuality: one
// component when Gray, else three. image/jpeg allocates its own buffers, not a
// copy of the image.
func (d *Decoded) WriteJPEG(w io.Writer) error {
	if _, ok := d.img.(*image.Gray); d.Gray && !ok {
		return errGrayJPEG
	}
	return jpeg.Encode(w, d.img, &jpeg.Options{Quality: JPEGQuality})
}

// writeRows converts each row to NRGBA in one buffer, packs it in place and
// writes it.
func (d *Decoded) writeRows(w io.Writer, pack func([]byte) []byte) error {
	buf := make([]byte, 4*d.Width)
	var table *[256][4]byte
	if p, ok := d.img.(*image.Paletted); ok {
		table = paletteTable(p.Palette)
	}
	for y := range d.Height {
		nrgbaRow(d.img, table, y, buf)
		if _, err := w.Write(pack(buf)); err != nil {
			return err
		}
	}
	return nil
}

func packGray(b []byte) []byte {
	n := len(b) / 4
	for i := range n {
		b[i] = b[4*i]
	}
	return b[:n]
}

func packRGB(b []byte) []byte {
	n := len(b) / 4
	for i := range n {
		b[3*i], b[3*i+1], b[3*i+2] = b[4*i], b[4*i+1], b[4*i+2]
	}
	return b[:3*n]
}

func packAlpha(b []byte) []byte {
	n := len(b) / 4
	for i := range n {
		b[i] = b[4*i+3]
	}
	return b[:n]
}

// nrgbaRow writes row y of m (relative to its origin) as non-premultiplied
// 8-bit RGBA into buf, whose length is 4×width. 16-bit samples keep their
// high byte. table is the converted palette of a Paletted image.
func nrgbaRow(m image.Image, table *[256][4]byte, y int, buf []byte) {
	n := len(buf) / 4
	switch m := m.(type) {
	case *image.Gray:
		for i, v := range m.Pix[y*m.Stride:][:n] {
			buf[4*i], buf[4*i+1], buf[4*i+2], buf[4*i+3] = v, v, v, 0xff
		}
	case *image.Gray16:
		src := m.Pix[y*m.Stride:][:2*n]
		for i := range n {
			v := src[2*i]
			buf[4*i], buf[4*i+1], buf[4*i+2], buf[4*i+3] = v, v, v, 0xff
		}
	case *image.NRGBA:
		copy(buf, m.Pix[y*m.Stride:][:4*n])
	case *image.NRGBA64:
		src := m.Pix[y*m.Stride:][:8*n]
		for i := range 4 * n {
			buf[i] = src[2*i]
		}
	case *image.RGBA:
		src := m.Pix[y*m.Stride:][:4*n]
		for i := 0; i < len(src); i += 4 {
			unpremultiply(buf[i:i+4], uint32(src[i])*0x101, uint32(src[i+1])*0x101, uint32(src[i+2])*0x101, uint32(src[i+3])*0x101)
		}
	case *image.RGBA64:
		src := m.Pix[y*m.Stride:][:8*n]
		for i := range n {
			s := src[8*i:]
			unpremultiply(buf[4*i:4*i+4], be16(s), be16(s[2:]), be16(s[4:]), be16(s[6:]))
		}
	case *image.Paletted:
		for i, c := range m.Pix[y*m.Stride:][:n] {
			copy(buf[4*i:4*i+4], table[c][:])
		}
	case *image.YCbCr:
		for i := range n {
			yy, cb, cr := m.Y[m.YOffset(i, y)], m.Cb[m.COffset(i, y)], m.Cr[m.COffset(i, y)]
			buf[4*i], buf[4*i+1], buf[4*i+2] = color.YCbCrToRGB(yy, cb, cr)
			buf[4*i+3] = 0xff
		}
	default:
		genericRow(m, m.Bounds().Min.Y+y, buf)
	}
}

func be16(b []byte) uint32 { return uint32(b[0])<<8 | uint32(b[1]) }

// unpremultiply stores the 8-bit non-premultiplied form of a 16-bit
// premultiplied colour. A colour channel above alpha (invalid premultiplied
// data) saturates.
func unpremultiply(dst []byte, r, g, b, a uint32) {
	switch a {
	case 0xffff:
		dst[0], dst[1], dst[2] = hi8(r), hi8(g), hi8(b)
	case 0:
		dst[0], dst[1], dst[2] = 0, 0, 0
	default:
		dst[0], dst[1], dst[2] = hi8(min(r*0xffff/a, 0xffff)), hi8(min(g*0xffff/a, 0xffff)), hi8(min(b*0xffff/a, 0xffff))
	}
	dst[3] = hi8(a)
}

func hi8(v uint32) byte { return byte(v >> 8 & 0xff) }

// genericRow converts row y (absolute) of any image through color.NRGBAModel.
func genericRow(m image.Image, y int, buf []byte) {
	b := m.Bounds()
	for i := range len(buf) / 4 {
		c, _ := color.NRGBAModel.Convert(m.At(b.Min.X+i, y)).(color.NRGBA)
		buf[4*i], buf[4*i+1], buf[4*i+2], buf[4*i+3] = c.R, c.G, c.B, c.A
	}
}
