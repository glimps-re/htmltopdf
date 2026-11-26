package testcorpus

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"math"
	"strings"
)

// ImageCase is a crafted <img src> value of design M6 §9. Want is the
// image-rejected reason the raster layer must give ("scheme", "type",
// "encoding", "format", "size", "corrupt") or "" when the image is drawn. A
// "corrupt" image has a valid header and fails when decoded, so its space is
// kept. Limits are those of design M6 §4 (MaxSide 16384, MaxPixels 2048²,
// MaxDecodeBytes 32 MiB, MaxJPEGScans 32, MaxJPEGMarkers 4096).
type ImageCase struct {
	Name, Src, Want string
}

// ImageDoc is a hostile document made of several <img>. Rejects counts the
// expected image-rejected warnings per reason; Want is the render outcome.
type ImageDoc struct {
	Name    string
	Want    Outcome
	Rejects map[string]int
	HTML    func() []byte
}

func u32(n int) uint32 {
	if n < 0 || uint64(n) > math.MaxUint32 {
		panic("testcorpus: value out of range")
	}
	return uint32(n)
}

func u16(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		panic("testcorpus: value out of range")
	}
	return uint16(n)
}

func dataURL(mime string, b []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// bitWriter packs bits least significant first, as DEFLATE does.
type bitWriter struct {
	out  []byte
	acc  uint32
	nacc uint
}

func (w *bitWriter) bits(v int, n uint) {
	w.acc |= uint32(v&0xffff) << w.nacc
	w.nacc += n
	for w.nacc >= 8 {
		w.out = append(w.out, byte(w.acc&0xff))
		w.acc >>= 8
		w.nacc -= 8
	}
}

// huff writes a Huffman code, most significant bit first.
func (w *bitWriter) huff(code int, n uint) {
	var r int
	for i := range n {
		r = r<<1 | code>>i&1
	}
	w.bits(r, n)
}

func (w *bitWriter) flush() []byte {
	if w.nacc > 0 {
		w.out = append(w.out, byte(w.acc&0xff))
		w.acc, w.nacc = 0, 0
	}
	return w.out
}

func (w *bitWriter) fixedLiteral(v int) {
	switch {
	case v < 144:
		w.huff(0x30+v, 8)
	case v < 256:
		w.huff(0x190+v-144, 9)
	case v < 280:
		w.huff(v-256, 7)
	default:
		w.huff(0xc0+v-280, 8)
	}
}

var (
	lenBase  = [...]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258}
	lenExtra = [...]uint{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
	disBase  = [...]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}
	disExtra = [...]uint{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}
)

func (w *bitWriter) match(length, dist int) {
	lc := len(lenBase) - 1
	for lenBase[lc] > length {
		lc--
	}
	w.fixedLiteral(257 + lc)
	w.bits(length-lenBase[lc], lenExtra[lc])
	dc := len(disBase) - 1
	for disBase[dc] > dist {
		dc--
	}
	w.huff(dc, 5)
	w.bits(dist-disBase[dc], disExtra[dc])
}

// zlibRepeat returns a zlib stream that inflates to total bytes: pattern
// repeated cyclically. The encoder is hand written (one fixed-Huffman block)
// so the bytes do not depend on the Go version, and a decompression bomb costs
// about 13 bits per 258 bytes. len(pattern) must be in [1, 32768].
func zlibRepeat(pattern []byte, total int) []byte {
	period := len(pattern)
	if period < 1 || period > 32768 {
		panic("testcorpus: bad pattern period")
	}
	w := &bitWriter{}
	w.bits(1, 1)
	w.bits(1, 2)
	lit := min(period, total)
	for i := range lit {
		w.fixedLiteral(int(pattern[i]))
	}
	for done := lit; done < total; {
		n := min(258, total-done)
		if n < 3 {
			for i := range n {
				w.fixedLiteral(int(pattern[(done+i)%period]))
			}
			break
		}
		w.match(n, period)
		done += n
	}
	w.fixedLiteral(256)
	ad := adler32.New()
	chunk := bytes.Repeat(pattern, (64<<10+period-1)/period)
	for done := 0; done < total; {
		n := min(len(chunk), total-done)
		_, _ = ad.Write(chunk[:n]) // hash.Hash.Write never returns an error
		done += n
	}
	out := append([]byte{0x78, 0x01}, w.flush()...)
	return binary.BigEndian.AppendUint32(out, ad.Sum32())
}

// zlibStored returns a zlib stream of stored blocks holding raw.
func zlibStored(raw []byte) []byte {
	out := []byte{0x78, 0x01}
	rest := raw
	for {
		n := min(len(rest), 65535)
		final := byte(0)
		if n == len(rest) {
			final = 1
		}
		out = append(out, final, byte(n&0xff), byte(n>>8&0xff), ^byte(n&0xff), ^byte(n>>8&0xff))
		out = append(out, rest[:n]...)
		rest = rest[n:]
		if final == 1 {
			break
		}
	}
	return binary.BigEndian.AppendUint32(out, adler32.Checksum(raw))
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

func pngChunk(typ string, data []byte) []byte {
	b := make([]byte, 0, len(data)+12)
	b = binary.BigEndian.AppendUint32(b, u32(len(data)))
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b[4:]))
}

func pngIHDR(w, h int, depth, colorType, interlace byte) []byte {
	d := binary.BigEndian.AppendUint32(nil, u32(w))
	d = binary.BigEndian.AppendUint32(d, u32(h))
	return pngChunk("IHDR", append(d, depth, colorType, 0, 0, interlace))
}

func joinBytes(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// pngWith builds signature, IHDR, extra chunks, one IDAT holding zlib, IEND.
func pngWith(w, h int, depth, colorType, interlace byte, zlib []byte, extra ...[]byte) []byte {
	return joinBytes(pngSignature, pngIHDR(w, h, depth, colorType, interlace), joinBytes(extra...),
		pngChunk("IDAT", zlib), pngChunk("IEND", nil))
}

// pngSolid is a w×h image whose every pixel is px (bytes per pixel as stored)
// with filter 0 rows: valid at any size, tiny when compressed.
func pngSolid(w, h int, depth, colorType byte, px []byte) []byte {
	row := append([]byte{0}, bytes.Repeat(px, w)...)
	return pngWith(w, h, depth, colorType, 0, zlibRepeat(row, len(row)*h))
}

// pngHeaderOnly claims w×h gray pixels and carries one byte of data.
func pngHeaderOnly(w, h int) []byte {
	return pngWith(w, h, 8, 0, 0, zlibStored([]byte{0}))
}

// rgba16 is the content of the colour images: distinct values in every sample,
// a varying alpha.
func rgba16(x, y int) [4]uint16 {
	return [4]uint16{u16((x*1777 + y*13) & 0xffff), u16(y * 2851 & 0xffff), u16((x ^ y) * 4099 & 0xffff), u16(65535 - x*100)}
}

// pngSpec describes a generated PNG: its samples come from sample(x, y, channel).
type pngSpec struct {
	w, h       int
	depth, ct  byte
	adam7      bool
	filters    bool // rows use the filter types 0 to 4 in turn
	plte, trns []byte
	sample     func(x, y, c int) int
}

func (s pngSpec) channels() int {
	switch s.ct {
	case 2:
		return 3
	case 4:
		return 2
	case 6:
		return 4
	}
	return 1
}

var adam7Passes = [7][4]int{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}

// zlib returns the stored zlib stream of the image data, one appendPass per
// pass: a plain image is the single pass {0, 0, 1, 1}, so interlaced twins stay
// in sync with their plain version.
func (s pngSpec) zlib() []byte {
	passes := [][4]int{{0, 0, 1, 1}}
	if s.adam7 {
		passes = adam7Passes[:]
	}
	var raw []byte
	for _, p := range passes {
		raw = s.appendPass(raw, p[0], p[1], p[2], p[3])
	}
	return zlibStored(raw)
}

func (s pngSpec) png(extra ...[]byte) []byte {
	var chunks [][]byte
	if s.plte != nil {
		chunks = append(chunks, pngChunk("PLTE", s.plte))
	}
	if s.trns != nil {
		chunks = append(chunks, pngChunk("tRNS", s.trns))
	}
	var interlace byte
	if s.adam7 {
		interlace = 1
	}
	return pngWith(s.w, s.h, s.depth, s.ct, interlace, s.zlib(), append(chunks, extra...)...)
}

// appendPass appends the rows of one pass; a pass without pixels adds nothing.
func (s pngSpec) appendPass(raw []byte, x0, y0, dx, dy int) []byte {
	if x0 >= s.w || y0 >= s.h {
		return raw
	}
	bpp := max(1, s.channels()*int(s.depth)/8)
	var prev []byte
	for row, y := 0, y0; y < s.h; row, y = row+1, y+dy {
		var bw msbWriter
		for x := x0; x < s.w; x += dx {
			for c := range s.channels() {
				bw.put(s.sample(x, y, c), uint(s.depth))
			}
		}
		cur := bw.flush()
		f := 0
		if s.filters {
			f = row % 5
		}
		raw = append(raw, byte(f&0xff))
		raw = append(raw, filterRow(f, cur, prev, bpp)...)
		prev = cur
	}
	return raw
}

// msbWriter packs samples most significant bit first, as PNG does.
type msbWriter struct {
	out  []byte
	acc  uint
	nacc uint
}

func (w *msbWriter) put(v int, bits uint) {
	w.acc = w.acc<<bits | uint(v&0xffff)&(1<<bits-1)
	w.nacc += bits
	for w.nacc >= 8 {
		w.out = append(w.out, byte(w.acc>>(w.nacc-8)&0xff))
		w.nacc -= 8
	}
	w.acc &= 1<<w.nacc - 1
}

func (w *msbWriter) flush() []byte {
	if w.nacc > 0 {
		w.out = append(w.out, byte(w.acc<<(8-w.nacc)&0xff))
		w.acc, w.nacc = 0, 0
	}
	return w.out
}

func filterRow(f int, cur, prev []byte, bpp int) []byte {
	out := make([]byte, len(cur))
	for i, x := range cur {
		var a, b, c int
		if i >= bpp {
			a = int(cur[i-bpp])
		}
		if prev != nil {
			b = int(prev[i])
			if i >= bpp {
				c = int(prev[i-bpp])
			}
		}
		var p int
		switch f {
		case 1:
			p = a
		case 2:
			p = b
		case 3:
			p = (a + b) / 2
		case 4:
			p = paeth(a, b, c)
		}
		out[i] = x - byte(p&0xff)
	}
	return out
}

func paeth(a, b, c int) int {
	pa, pb, pc := abs(b-c), abs(a-c), abs(a+b-2*c)
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func gradientSamples(depth byte) func(x, y, c int) int {
	mask := 1<<depth - 1
	return func(x, y, c int) int { return (x*37 + y*91 + c*53) & mask }
}

func paletteSamples(depth byte) func(x, y, c int) int {
	return func(x, y, _ int) int { return (x + 3*y) % (1 << depth) }
}

func rgbaSamples(depth byte) func(x, y, c int) int {
	return func(x, y, c int) int { return int(rgba16(x, y)[c]) >> (16 - depth) }
}

// palette returns the 1<<depth entries of PLTE and an alpha for each of them (tRNS).
func palette(depth byte) (plte, alphas []byte) {
	for i := range 1 << depth {
		plte = append(plte, byte(i*53&0xff), byte(i*97&0xff), byte(i*29&0xff))
		alphas = append(alphas, byte(i*37&0xff))
	}
	return plte, alphas
}

// pngVariants returns the generated images of every PNG variant the decoders
// support, each 33×31 unless named otherwise, with filter types 0 to 4 across
// the rows.
func pngVariants() []ImageCase {
	var cs []ImageCase
	add := func(name string, s pngSpec) {
		cs = append(cs, ImageCase{Name: name, Src: dataURL(pngMIME, s.png())})
	}
	spec := func(depth, ct byte, sample func(x, y, c int) int) pngSpec {
		return pngSpec{w: 33, h: 31, depth: depth, ct: ct, filters: true, sample: sample}
	}
	for _, d := range []byte{1, 2, 4, 8, 16} {
		add(fmt.Sprintf("png_gray%d_33x31", d), spec(d, 0, gradientSamples(d)))
	}
	for _, d := range []byte{8, 16} {
		add(fmt.Sprintf("png_rgb%d_33x31", d), spec(d, 2, gradientSamples(d)))
		add(fmt.Sprintf("png_graya%d_33x31", d), spec(d, 4, gradientSamples(d)))
		add(fmt.Sprintf("png_rgba%d_33x31", d), spec(d, 6, rgbaSamples(d)))
	}
	for _, d := range []byte{1, 2, 4, 8} {
		s := spec(d, 3, paletteSamples(d))
		s.plte, _ = palette(d)
		add(fmt.Sprintf("png_pal%d_33x31", d), s)
	}
	pal8 := spec(8, 3, paletteSamples(8))
	pal8.plte, pal8.trns = palette(8)
	add("png_pal8_trns_33x31", pal8)
	pal4 := spec(4, 3, paletteSamples(4))
	pal4.w, pal4.h, pal4.adam7 = 31, 31, true
	pal4.plte, _ = palette(4)
	add("png_pal4_adam7_31x31", pal4)
	gray2 := spec(2, 0, gradientSamples(2))
	gray2.trns = []byte{0, 3}
	add("png_gray2_trns_33x31", gray2)
	rgb16 := spec(16, 2, gradientSamples(16))
	rgb16.trns = []byte{0, 0, 0, 53, 0, 106} // the pixel at (0, 0)
	add("png_rgb16_trns_33x31", rgb16)
	twin := pngSpec{w: 37, h: 23, depth: 16, ct: 6, sample: rgbaSamples(16)}
	add("png_rgba16_37x23", twin)
	twin.adam7 = true
	add("png_rgba16_37x23_adam7", twin)
	return cs
}

// corruptCRC flips a bit of the CRC of the first chunk named typ.
func corruptCRC(png []byte, typ string) []byte {
	out := bytes.Clone(png)
	i := bytes.Index(out, []byte(typ))
	if i < 4 {
		panic("testcorpus: chunk not found")
	}
	n := int(binary.BigEndian.Uint32(out[i-4:]))
	out[i+4+n] ^= 0x01
	return out
}

func tEXtChunks(total int) []byte {
	var b bytes.Buffer
	text := append([]byte("k\x00"), bytes.Repeat([]byte("hostile text chunk "), 3000)...)
	for b.Len() < total {
		b.Write(pngChunk("tEXt", text))
	}
	return b.Bytes()
}

func jpegSeg(marker byte, payload []byte) []byte {
	b := []byte{0xff, marker}
	b = binary.BigEndian.AppendUint16(b, u16(len(payload)+2))
	return append(b, payload...)
}

var (
	jpegSOI = []byte{0xff, 0xd8}
	jpegEOI = []byte{0xff, 0xd9}
	jpegDQT = jpegSeg(0xdb, append([]byte{0}, bytes.Repeat([]byte{1}, 64)...))
	// One code of one bit ('0') for symbol 0: DC category 0 and AC end of block.
	jpegDHT = joinBytes(
		jpegSeg(0xc4, append(append([]byte{0x00, 1}, make([]byte, 15)...), 0)),
		jpegSeg(0xc4, append(append([]byte{0x10, 1}, make([]byte, 15)...), 0)))
)

func jpegSOF(marker, precision byte, w, h int, comps int) []byte {
	p := []byte{precision}
	p = binary.BigEndian.AppendUint16(p, u16(h))
	p = binary.BigEndian.AppendUint16(p, u16(w))
	p = append(p, byte(comps&0xff))
	for i := range comps {
		p = append(p, byte(i+1), 0x11, 0)
	}
	return jpegSeg(marker, p)
}

func jpegSOS(comps int, se byte) []byte {
	p := []byte{byte(comps & 0xff)}
	for i := range comps {
		p = append(p, byte(i+1), 0x00)
	}
	return jpegSeg(0xda, append(p, 0, se, 0))
}

// jpegBaseline is a baseline JPEG of w×h whose every sample is mid-grey: the
// entropy data is 2 bits per block (DC category 0, end of block), all zero.
func jpegBaseline(w, h, comps int) []byte {
	blocks := (w + 7) / 8 * ((h + 7) / 8) * comps
	return joinBytes(jpegSOI, jpegDQT, jpegSOF(0xc0, 8, w, h, comps), jpegDHT, jpegSOS(comps, 63),
		make([]byte, (2*blocks+7)/8), jpegEOI)
}

// jpegProgressive is a progressive JPEG whose scans are all the DC first scan
// (1 bit per block). scans repeats it.
func jpegProgressive(w, h, comps, scans int) []byte {
	blocks := (w + 7) / 8 * ((h + 7) / 8) * comps
	scan := joinBytes(jpegSOS(comps, 0), make([]byte, (blocks+7)/8))
	parts := [][]byte{jpegSOI, jpegDQT, jpegSOF(0xc2, 8, w, h, comps), jpegDHT}
	for range scans {
		parts = append(parts, scan)
	}
	return joinBytes(append(parts, jpegEOI)...)
}

func jpegWithMarkers(n int) []byte {
	flood := bytes.Repeat([]byte{0xff, 0xe1, 0x00, 0x02}, n)
	return joinBytes(jpegSOI, flood, jpegDQT, jpegSOF(0xc0, 8, 8, 8, 1), jpegDHT, jpegSOS(1, 63), []byte{0}, jpegEOI)
}

func jpegTruncatedAfterSOS() []byte {
	return joinBytes(jpegSOI, jpegDQT, jpegSOF(0xc0, 8, 64, 64, 1), jpegDHT, jpegSOS(1, 63), []byte{0, 0})
}

// jpegMinimal carries the given SOF, one SOS and one byte of entropy data.
func jpegMinimal(sof []byte) []byte {
	return joinBytes(jpegSOI, jpegDQT, sof, jpegDHT, jpegSOS(1, 63), []byte{0}, jpegEOI)
}

const (
	pngMIME  = "image/png"
	jpegMIME = "image/jpeg"
)

// Limits of design M6 §4 that the generators and the tests sit on; the corpus
// must not import raster.
const (
	imgMaxSide        = 16384
	imgEdge           = 2048 // side of a square of MaxPixels
	imgMaxPixels      = imgEdge * imgEdge
	imgMaxDecodeBytes = 32 << 20
	imgMaxJPEGScans   = 32
	imgMaxJPEGMarkers = 4096
)

// ImageCases returns the crafted image sources of design M6 §9. Generation is
// deterministic and each source is at most budget.Defaults()[budget.InputBytes] long.
func ImageCases() []ImageCase {
	tinyPNG := pngSolid(1, 1, 8, 2, []byte{200, 30, 30})
	small := pngSpec{w: 64, h: 64, depth: 8, ct: 6, sample: rgbaSamples(8)}
	smallPNG, smallZlib := small.png(), small.zlib()
	jpeg8 := jpegBaseline(8, 8, 1)
	pngBomb := pngWith(1, 1, 8, 0, 0, zlibRepeat([]byte{0}, 128<<20))
	zTXtBomb := pngChunk("zTXt", append([]byte("k\x00\x00"), zlibRepeat([]byte{0}, 64<<20)...))
	halfData := func() []byte {
		raw := bytes.Repeat([]byte{0}, 64*(1+64*4)/2)
		return pngWith(64, 64, 8, 6, 0, zlibStored(raw))
	}

	cs := []ImageCase{
		{"png_deflate_bomb_1x1", dataURL(pngMIME, pngBomb), "corrupt"},
		{"png_ztxt_bomb_skipped", dataURL(pngMIME, pngWith(1, 1, 8, 2, 0, zlibStored([]byte{0, 1, 2, 3}), zTXtBomb)), ""},
		{"png_header_16384x16384", dataURL(pngMIME, pngHeaderOnly(imgMaxSide, imgMaxSide)), "size"},
		{"png_header_65535x1", dataURL(pngMIME, pngHeaderOnly(65535, 1)), "size"},
		{"png_header_0x10", dataURL(pngMIME, pngWith(0, 10, 8, 0, 0, zlibStored([]byte{0}))), "format"},
		{"png_header_2147483647x2147483647", dataURL(pngMIME, pngHeaderOnly(math.MaxInt32, math.MaxInt32)), "format"},
		{"png_rgba16_2048x2048_limit", dataURL(pngMIME, pngSolid(imgEdge, imgEdge, 16, 6, make([]byte, 8))), ""},
		{"png_rgba16_2049x2048_over", dataURL(pngMIME, pngSolid(imgEdge+1, imgEdge, 16, 6, make([]byte, 8))), "size"},
		{"png_gray_16384x8_side_limit", dataURL(pngMIME, pngSolid(imgMaxSide, 8, 8, 0, []byte{128})), ""},
		{"png_gray_16385x8_side_over", dataURL(pngMIME, pngSolid(imgMaxSide+1, 8, 8, 0, []byte{128})), "size"},
		{"png_truncated_idat", dataURL(pngMIME, truncateIDAT(smallPNG)), "corrupt"},
		{"png_bad_idat_crc", dataURL(pngMIME, corruptCRC(smallPNG, "IDAT")), "corrupt"},
		{"png_idat_before_ihdr", dataURL(pngMIME, joinBytes(pngSignature, pngChunk("IDAT", smallZlib), pngIHDR(64, 64, 8, 6, 0), pngChunk("IEND", nil))), "format"},
		{"png_text_chunks_1400k", dataURL(pngMIME, pngWith(64, 64, 8, 6, 0, smallZlib, tEXtChunks(1400<<10))), ""},
		{"png_half_data", dataURL(pngMIME, halfData()), "corrupt"},
		{"png_signature_only", dataURL(pngMIME, pngSignature), "format"},

		{"jpeg_sof_65500x65500", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc0, 8, 65500, 65500, 1))), "size"},
		{"jpeg_gray_16384x16384_pixels", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc0, 8, imgMaxSide, imgMaxSide, 1))), "size"},
		{"jpeg_gray_16384x8_side_limit", dataURL(jpegMIME, jpegBaseline(imgMaxSide, 8, 1)), ""},
		{"jpeg_gray_16385x8_side_over", dataURL(jpegMIME, jpegBaseline(imgMaxSide+1, 8, 1)), "size"},
		{"jpeg_gray_2048x2048_limit", dataURL(jpegMIME, jpegBaseline(imgEdge, imgEdge, 1)), ""},
		{"jpeg_gray_2049x2048_over", dataURL(jpegMIME, jpegBaseline(imgEdge+1, imgEdge, 1)), "size"},
		{"jpeg_ycc_2048x2048_baseline", dataURL(jpegMIME, jpegBaseline(imgEdge, imgEdge, 3)), ""},
		{"jpeg_progressive_ycc_1024x1024", dataURL(jpegMIME, jpegProgressive(1024, 1024, 3, 1)), ""},
		{"jpeg_progressive_ycc_2048x2048_cost", dataURL(jpegMIME, jpegProgressive(imgEdge, imgEdge, 3, 1)), "size"},
		{"jpeg_progressive_32_scans", dataURL(jpegMIME, jpegProgressive(8, 8, 1, imgMaxJPEGScans)), ""},
		{"jpeg_progressive_33_scans", dataURL(jpegMIME, jpegProgressive(8, 8, 1, imgMaxJPEGScans+1)), "format"},
		{"jpeg_markers_4000", dataURL(jpegMIME, jpegWithMarkers(4000)), ""},
		{"jpeg_markers_5000", dataURL(jpegMIME, jpegWithMarkers(5000)), "format"},
		{"jpeg_truncated_after_sos", dataURL(jpegMIME, jpegTruncatedAfterSOS()), "corrupt"},
		{"jpeg_arithmetic_sof9", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc9, 8, 8, 8, 1))), "format"},
		{"jpeg_12bit_sof1", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc1, 12, 8, 8, 1))), "format"},
		{"jpeg_lossless_sof3", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc3, 8, 8, 8, 1))), "format"},
		{"jpeg_two_components", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc0, 8, 8, 8, 2))), "format"},
		{"jpeg_zero_width", dataURL(jpegMIME, jpegMinimal(jpegSOF(0xc0, 8, 0, 8, 1))), "format"},

		{"url_invalid_base64_char", "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG)[:20] + "!!!!", "encoding"},
		{"url_bad_base64_padding", "data:image/png;base64,iVBORw0KGgo===", "encoding"},
		{"url_missing_base64", "data:image/png," + "%89PNG%0D%0A%1A%0A", "encoding"},
		{"url_gif_type", "data:image/gif;base64,R0lGODlhAQABAAAAACw=", "type"},
		{"url_svg_type", "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)), "type"},
		{"url_no_type", "data:;base64," + base64.StdEncoding.EncodeToString(tinyPNG), "type"},
		{"url_http", "http://169.254.169.254/latest/meta-data/x.png", "scheme"},
		{"url_file", "file:///etc/passwd", "scheme"},
		{"url_protocol_relative", "//attacker.example/x.png", "scheme"},
		{"url_relative", "logo.png", "scheme"},
		{"url_empty", "", "scheme"},
		{"url_empty_payload", "data:image/png;base64,", "format"},
		{"url_unknown_magic", dataURL(pngMIME, []byte("not an image at all")), "format"},

		{"url_jpeg_labelled_png", dataURL(pngMIME, jpeg8), ""},
		{"url_png_labelled_jpeg", dataURL(jpegMIME, tinyPNG), ""},
		{"url_type_image_jpg", dataURL("image/jpg", jpeg8), ""},
		{"url_scheme_and_type_uppercase", "DATA:IMAGE/PNG;BASE64," + base64.StdEncoding.EncodeToString(tinyPNG), ""},
		{"url_type_parameters", "data:image/png;charset=utf-8;name=a.png;base64," + base64.StdEncoding.EncodeToString(tinyPNG), ""},
		{"url_base64_whitespace", "data:image/png;base64," + wrapBase64(base64.StdEncoding.EncodeToString(tinyPNG)), ""},
	}
	return append(cs, pngVariants()...)
}

func truncateIDAT(png []byte) []byte {
	i := bytes.Index(png, []byte("IDAT"))
	n := int(binary.BigEndian.Uint32(png[i-4:]))
	return png[:i+4+n/2]
}

// wrapBase64 inserts the white space a hand-formatted payload carries.
func wrapBase64(s string) string {
	var b strings.Builder
	ws := []string{"\n", " ", "\t", "\r\n"}
	for i := 0; i < len(s); i += 16 {
		b.WriteString(s[i:min(i+16, len(s))])
		b.WriteString(ws[i/16%len(ws)])
	}
	return b.String()
}

func imgTag(src, attrs string) string {
	return fmt.Sprintf(`<img src="%s"%s>`, src, attrs)
}

// ImageDocs returns the hostile documents made of many images (design M6 §9),
// each at most budget.Defaults()[budget.InputBytes] long.
func ImageDocs() []ImageDoc {
	px := func(i int) string {
		return dataURL(pngMIME, pngSolid(1, 1, 8, 2, []byte{byte(i & 0xff), byte(i >> 1 & 0xff), 7}))
	}
	gray2048 := func(i int) string {
		return dataURL(pngMIME, pngSolid(imgEdge, imgEdge, 8, 0, []byte{byte(10 * (i + 1) & 0xff)}))
	}
	same := px(1)
	many := func(n int, src func(int) string) func() []byte {
		return func() []byte {
			var b strings.Builder
			for i := range n {
				b.WriteString(imgTag(src(i), ""))
			}
			return doc("", b.String())
		}
	}
	layouts := func() []byte {
		i := imgTag(same, ` width="32" height="32" alt="x"`)
		css := `.f{display:flex}.g{display:grid;grid-template-columns:1fr 1fr}` +
			`.m{column-count:3}.a{position:absolute;top:10px;left:10px}.x{position:fixed;bottom:0;right:0}`
		body := `<table><tr><td>` + i + `</td><td>` + i + `</td></tr></table>` +
			`<div class="f">` + i + i + `</div><div class="g">` + i + i + i + `</div>` +
			`<div class="m">` + repeat(i+" text ", 200) + `</div><div class="a">` + i + `</div><div class="x">` + i + `</div>` +
			`<a href="https://example.test/">` + i + `</a><div style="border-radius:20px;overflow:hidden;opacity:.5;transform:rotate(7deg)">` + i + `</div>`
		return doc(style(css), body)
	}
	return []ImageDoc{
		{"img_dup_10000", Success, nil, many(10000, func(int) string { return same })},
		{"img_distinct_64", Success, nil, many(64, px)},
		{"img_distinct_65", Degraded, map[string]int{"count": 1}, many(65, px)},
		{"img_total_4x2048sq", Success, nil, many(4, gray2048)},
		{"img_total_5x2048sq", Degraded, map[string]int{"total": 1}, many(5, gray2048)},
		{"img_nested_inlines_500", Success, nil, func() []byte {
			return doc("", repeat("<span>", 500)+imgTag(same, "")+repeat("</span>", 500))
		}},
		{"img_layout_contexts", Success, nil, layouts},
		{"img_display_none_not_counted", Success, nil, func() []byte {
			var b strings.Builder
			for i := range 65 {
				b.WriteString(imgTag(px(i), ` style="display:none"`))
			}
			return doc("", b.String()+imgTag(px(200), "")+"<p>visible</p>")
		}},
		{"img_alt_fallback_rejected", Degraded, map[string]int{"scheme": 1, "format": 1}, func() []byte {
			return doc("", imgTag("http://attacker.example/a.png", ` alt="ALT SCHEME"`)+
				imgTag(dataURL(pngMIME, []byte("junk")), ` alt="ALT FORMAT"`)+"<p>after</p>")
		}},
		{"img_progressive_32_scans_work", Limit, nil, func() []byte {
			return doc("", imgTag(dataURL(jpegMIME, jpegProgressive(imgEdge, imgEdge, 1, imgMaxJPEGScans)), ""))
		}},
		{"img_extreme_sizes", Robust, nil, func() []byte {
			s := same
			return doc(style(`.h{width:1e30px;height:1e30px}.n{width:-5px}.z{width:0;height:0}.p{width:1000000%;max-width:none}`),
				imgTag(s, ` width="99999999999" height="99999999999"`)+imgTag(s, ` class="h"`)+imgTag(s, ` class="n"`)+
					imgTag(s, ` class="z"`)+imgTag(s, ` class="p"`)+imgTag(s, ` width="0" height="-3"`)+imgTag(s, ` width="1e30" height="NaN"`))
		}},
	}
}
