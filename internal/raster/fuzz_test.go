package raster

import (
	"bytes"
	"errors"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// exercise adds and decodes src and checks the contract of every step.
func exercise(t *testing.T, src string) {
	set := NewSet(newBudget())
	s, err := set.Add(src)
	if err != nil {
		switch r := reasonOf(err); r {
		case ReasonScheme, ReasonType, ReasonEncoding, ReasonFormat, ReasonSize:
		default:
			t.Fatalf("Add: %v (reason %v)", err, r)
		}
		return
	}
	if s.Width < 1 || s.Height < 1 || s.Width > MaxSide || s.Height > MaxSide ||
		s.Width*s.Height > MaxPixels || s.cost > MaxDecodeBytes || s.scans > MaxJPEGScans {
		t.Fatalf("source out of the caps: %+v", s)
	}
	if again, err := set.Add(src); again != s || err != nil {
		t.Fatal("duplicate not shared")
	}
	d, err := Decode(s, newBudget())
	if err != nil {
		var le *budget.LimitError
		if reasonOf(err) != ReasonCorrupt && !errors.As(err, &le) {
			t.Fatalf("Decode: %v", err)
		}
		return
	}
	var cw countWriter
	if err := d.WriteSamples(&cw); err != nil {
		t.Fatal(err)
	}
	n := 3
	if d.Gray {
		n = 1
	}
	if cw.n != d.Width*d.Height*n {
		t.Fatalf("%d sample bytes for %d×%d×%d", cw.n, d.Width, d.Height, n)
	}
	cw.n = 0
	if err := d.WriteAlpha(&cw); err != nil || cw.n != d.Width*d.Height {
		t.Fatalf("alpha: %d bytes, %v", cw.n, err)
	}
	if s.Format == JPEG {
		if err := d.WriteJPEG(io.Discard); err != nil {
			t.Fatal(err)
		}
	}
}

type countWriter struct{ n int }

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

// fuzzSeeds returns small valid and crafted images.
func fuzzSeeds(t testing.TB) [][]byte {
	seeds := [][]byte{
		encodePNG(t, gradient(3, 2, color.NRGBAModel, true)),
		encodePNG(t, gradient(3, 2, color.Gray16Model, false)),
		encodeJPEG(t, gradient(9, 9, color.NRGBAModel, false)),
		synthJPEG(jpegSpec{w: 17, h: 9, nComp: 3, sof: mSOF2, acBands: 3}),
		synthJPEG(jpegSpec{w: 9, h: 9, nComp: 4, sof: mSOF0, extra: adobeAPP14(2)}),
		rawPNG(ihdr(5, 3, 2, pngPaletted, pngAdam7), pngChunk("PLTE", []byte{1, 2, 3, 4, 5, 6}), pngChunk("tRNS", []byte{0}), idat(t, zeroRows(5, 3, 1, true)), iend),
	}
	pngs, err := filepath.Glob("../../testdata/images/*.png")
	if err != nil {
		t.Fatal(err)
	}
	jpegs, err := filepath.Glob("../../testdata/images/*.jp*g")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range append(pngs, jpegs...) {
		b, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatal(err)
		}
		if len(b) <= 32<<10 {
			seeds = append(seeds, b)
			n++
		}
	}
	if n == 0 {
		t.Fatal("no corpus seed")
	}
	return seeds
}

func FuzzRasterAdd(f *testing.F) {
	for _, b := range fuzzSeeds(f) {
		f.Add(pngURL(b))
	}
	for _, s := range []string{"", "data:", "data:image/png;base64,", "DATA:IMAGE/JPG; BASE64 ,/9j/", "http://x", "data:image/png;base64,iVBORw0KGgo="} {
		f.Add(s)
	}
	f.Fuzz(exercise)
}

func FuzzRasterDecode(f *testing.F) {
	for _, b := range fuzzSeeds(f) {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		exercise(t, pngURL(data))
		for _, magic := range []string{pngMagic, jpegMagic} {
			if !bytes.HasPrefix(data, []byte(magic)) {
				exercise(t, jpegURL(append([]byte(magic), data...)))
			}
		}
	})
}
