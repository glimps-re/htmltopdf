package raster

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func newBudget() *budget.Budget {
	return budget.New(context.Background(), budget.Defaults(), nil)
}

// dataURL wraps b in a data: URL of media type typ.
func dataURL(typ string, b []byte) string {
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b)
}

func pngURL(b []byte) string  { return dataURL("image/png", b) }
func jpegURL(b []byte) string { return dataURL("image/jpeg", b) }

// gradient fills an image of the given type with a deterministic pattern
// whose alpha, for the types that have one, varies with x when alpha is set.
func gradient(w, h int, model color.Model, alpha bool) image.Image {
	r := image.Rect(0, 0, w, h)
	var m interface {
		image.Image
		Set(x, y int, c color.Color)
	}
	switch model {
	case color.GrayModel:
		m = image.NewGray(r)
	case color.Gray16Model:
		m = image.NewGray16(r)
	case color.NRGBA64Model:
		m = image.NewNRGBA64(r)
	default:
		m = image.NewNRGBA(r)
	}
	for y := range h {
		for x := range w {
			a := uint8(0xff)
			if alpha {
				a = uint8(x * 7)
			}
			m.Set(x, y, color.NRGBA{uint8(x & 0xff), uint8(y & 0xff), uint8((x ^ y) & 0xff), a})
		}
	}
	return m
}

func encodePNG(tb testing.TB, m image.Image) []byte {
	tb.Helper()
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, m); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(tb testing.TB, m image.Image) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: 80}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// pngChunk returns a PNG chunk with its CRC.
func pngChunk(typ string, data []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)&0x7fffffff))
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b[4:]))
}

func ihdr(w, h uint32, depth, colorType, interlace byte) []byte {
	d := binary.BigEndian.AppendUint32(nil, w)
	d = binary.BigEndian.AppendUint32(d, h)
	return pngChunk("IHDR", append(d, depth, colorType, 0, 0, interlace))
}

// rawPNG assembles a PNG from chunks.
func rawPNG(chunks ...[]byte) []byte {
	b := []byte(pngMagic)
	for _, c := range chunks {
		b = append(b, c...)
	}
	return b
}

// idat compresses raw (filter bytes included) into an IDAT chunk.
func idat(tb testing.TB, raw []byte) []byte {
	tb.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		tb.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		tb.Fatal(err)
	}
	return pngChunk("IDAT", buf.Bytes())
}

var iend = pngChunk("IEND", nil)

// jpegSpec describes a synthetic Huffman JPEG whose blocks only carry a DC
// value: enough for image/jpeg to allocate and reconstruct every block.
type jpegSpec struct {
	w, h        int
	nComp       int    // 1, 3 or 4; components named 1, 2, 3, 4, sampled 1×1
	sof         byte   // mSOF0 (one interleaved scan) or mSOF2
	precision   byte   // 0 means 8
	acBands     int    // progressive: AC scans per component, after the DC scan
	appMarkers  int    // APP1 segments written before the SOF
	truncateEnd bool   // drop the EOI and the last byte of entropy data
	headerOnly  bool   // stop after the tables, before the first SOS
	sub420      bool   // luma sampled 2×2: 4:2:0 chroma subsampling
	extra       []byte // segment bytes inserted after the SOI
}

// huffman builds the canonical codes of a DHT table.
type huffCode struct {
	code uint32
	n    uint8
}

func canonical(bits [16]byte, vals []byte) map[byte]huffCode {
	out := make(map[byte]huffCode)
	code, k := uint32(0), 0
	for l := range 16 {
		for range bits[l] {
			out[vals[k]] = huffCode{code, uint8(l + 1)}
			code++
			k++
		}
		code <<= 1
	}
	return out
}

var (
	dcBits = [16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1}
	dcVals = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	acBits = [16]byte{0, 0, 0, 15}
	acVals = []byte{0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0}
)

type bitWriter struct {
	out   []byte
	acc   uint32
	nbits uint
}

func (w *bitWriter) put(v uint32, n uint8) {
	for i := int(n) - 1; i >= 0; i-- {
		w.acc = w.acc<<1 | (v>>uint(i))&1
		w.nbits++
		if w.nbits == 8 {
			b := byte(w.acc & 0xff)
			w.out = append(w.out, b)
			if b == 0xff {
				w.out = append(w.out, 0)
			}
			w.acc, w.nbits = 0, 0
		}
	}
}

func (w *bitWriter) flush() []byte {
	for w.nbits != 0 {
		w.put(1, 1)
	}
	return w.out
}

func segmentBytes(marker byte, data []byte) []byte {
	n := len(data) + 2
	b := []byte{0xff, marker, byte(n >> 8 & 0xff), byte(n & 0xff)}
	return append(b, data...)
}

// synthJPEG writes the JPEG described by s.
func synthJPEG(s jpegSpec) []byte {
	if s.precision == 0 {
		s.precision = 8
	}
	out := []byte{0xff, 0xd8}
	out = append(out, s.extra...)
	for range s.appMarkers {
		out = append(out, segmentBytes(0xe1, []byte("x"))...)
	}
	q := make([]byte, 65)
	for i := 1; i < 65; i++ {
		q[i] = 1
	}
	out = append(out, segmentBytes(mDQT, q)...)
	sof := []byte{s.precision, byte(s.h >> 8 & 0xff), byte(s.h & 0xff), byte(s.w >> 8 & 0xff), byte(s.w & 0xff), byte(s.nComp & 0xff)}
	for c := range s.nComp {
		sof = append(sof, byte(c+1), byte(s.sampling(c)*0x11&0xff), 0)
	}
	out = append(out, segmentBytes(s.sof, sof)...)
	dht := append([]byte{0x00}, dcBits[:]...)
	dht = append(dht, dcVals...)
	dht = append(dht, 0x10)
	dht = append(dht, acBits[:]...)
	dht = append(dht, acVals...)
	out = append(out, segmentBytes(mDHT, dht)...)
	if s.headerOnly {
		return out
	}
	dc, ac := canonical(dcBits, dcVals), canonical(acBits, acVals)
	hMax := s.sampling(0)
	mxx, myy := (s.w+8*hMax-1)/(8*hMax), (s.h+8*hMax-1)/(8*hMax)

	sos := []byte{byte(s.nComp & 0xff)}
	for c := range s.nComp {
		sos = append(sos, byte(c+1), 0x00)
	}
	if s.sof == mSOF2 {
		sos = append(sos, 0, 0, 0)
	} else {
		sos = append(sos, 0, 63, 0)
	}
	out = append(out, segmentBytes(mSOS, sos)...)
	var bw bitWriter
	prev := make([]int, s.nComp)
	block := func(b, c int) {
		v := ((b*7+c*13)%32 - 16) * 8
		putDC(&bw, dc, v-prev[c])
		prev[c] = v
		if s.sof != mSOF2 {
			h := ac[0x00]
			bw.put(h.code, h.n)
		}
	}
	if s.nComp == 1 {
		for b := range s.blocks(0, mxx, myy) {
			block(b, 0)
		}
	} else {
		for m := range mxx * myy {
			for c := range s.nComp {
				for j := range s.sampling(c) * s.sampling(c) {
					block(m*4+j, c)
				}
			}
		}
	}
	entropy := bw.flush()
	if s.truncateEnd && s.acBands == 0 {
		return append(out, entropy[:len(entropy)-1]...)
	}
	out = append(out, entropy...)
	for c := range s.nComp {
		for band := range s.acBands {
			lo, hi := 1+band*63/s.acBands, (band+1)*63/s.acBands
			out = append(out, segmentBytes(mSOS, []byte{1, byte((c + 1) & 0xff), 0x00, byte(lo & 0xff), byte(hi & 0xff), 0})...)
			var aw bitWriter
			for left := s.blocks(c, mxx, myy); left > 0; {
				run := min(left, 32767)
				r := 0
				for 1<<(r+1) <= run {
					r++
				}
				h := ac[byte(r<<4)]
				aw.put(h.code, h.n)
				aw.put(uint32((run-1<<r)&0x7fff), uint8(r))
				left -= run
			}
			out = append(out, aw.flush()...)
		}
	}
	if s.truncateEnd {
		return out[:len(out)-1]
	}
	return append(out, 0xff, mEOI)
}

const mDQT = 0xdb

// sampling is the horizontal and vertical sampling factor of component c.
func (s jpegSpec) sampling(c int) int {
	if s.sub420 && c == 0 && s.nComp > 1 {
		return 2
	}
	return 1
}

// blocks is the number of blocks of component c in a non-interleaved scan:
// image/jpeg skips the blocks of the MCU grid that start outside the image.
func (s jpegSpec) blocks(c, mxx, myy int) int {
	f := s.sampling(c)
	return min(mxx*f, (s.w+7)/8) * min(myy*f, (s.h+7)/8)
}

func putDC(w *bitWriter, dc map[byte]huffCode, diff int) {
	mag, bitsV := diff, diff
	if diff < 0 {
		mag, bitsV = -diff, diff-1
	}
	cat := 0
	for mag>>cat != 0 {
		cat++
	}
	h := dc[byte(cat)]
	w.put(h.code, h.n)
	if cat > 0 {
		w.put(uint32(bitsV&(1<<cat-1)&0xfff), uint8(cat))
	}
}

// adam7 is the pass grid of PNG interlacing: x0, y0, dx, dy.
var adam7 = [7][4]int{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}

// zeroRows returns the raw IDAT data (filter bytes included) of a w×h image
// of bpp bytes per pixel whose samples are all zero, interlaced or not.
func zeroRows(w, h, bpp int, interlaced bool) []byte {
	if !interlaced {
		return make([]byte, h*(1+w*bpp))
	}
	n := 0
	for _, p := range adam7 {
		pw, ph := (w-p[0]+p[2]-1)/p[2], (h-p[1]+p[3]-1)/p[3]
		if pw > 0 && ph > 0 {
			n += ph * (1 + pw*bpp)
		}
	}
	return make([]byte, n)
}
