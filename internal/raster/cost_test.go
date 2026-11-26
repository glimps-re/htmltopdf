package raster

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"runtime"
	"testing"
)

// adobeAPP14 is an Adobe segment with the given transform.
func adobeAPP14(transform byte) []byte {
	return segmentBytes(mAPP14, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, transform})
}

type costCase struct {
	name string
	src  func(testing.TB) string
}

// costCases are the decoder variants at the largest size the caps accept.
func costCases() []costCase {
	png := func(m func() image.Image) func(testing.TB) string {
		return func(tb testing.TB) string { return pngURL(encodePNG(tb, m())) }
	}
	jpg := func(s jpegSpec) func(testing.TB) string {
		return func(testing.TB) string { return jpegURL(synthJPEG(s)) }
	}
	return []costCase{
		{"png-gray8", png(func() image.Image { return gradient(2048, 2048, color.GrayModel, false) })},
		{"png-gray16", png(func() image.Image { return gradient(2048, 2048, color.Gray16Model, false) })},
		{"png-rgba8", png(func() image.Image { return gradient(2048, 2048, color.NRGBAModel, true) })},
		{"png-rgb8", png(func() image.Image { return gradient(2048, 2048, color.NRGBAModel, false) })},
		{"png-rgba16", png(func() image.Image { return gradient(2048, 2048, color.NRGBA64Model, true) })},
		{"png-paletted", png(func() image.Image {
			m := image.NewPaletted(image.Rect(0, 0, 2048, 2048), color.Palette{color.NRGBA{1, 2, 3, 4}, color.Black, color.White})
			for i := range m.Pix {
				m.Pix[i] = uint8(i % 3)
			}
			return m
		})},
		{"png-gray8-trns", func(tb testing.TB) string {
			return pngURL(rawPNG(ihdr(2048, 2048, 8, pngGray, 0), pngChunk("tRNS", []byte{0, 7}), idat(tb, zeroRows(2048, 2048, 1, false)), iend))
		}},
		{"png-interlaced-rgba8", func(tb testing.TB) string {
			return pngURL(rawPNG(ihdr(2048, 2048, 8, 6, pngAdam7), idat(tb, zeroRows(2048, 2048, 4, true)), iend))
		}},
		{"png-interlaced-paletted", func(tb testing.TB) string {
			return pngURL(rawPNG(ihdr(2048, 2048, 8, pngPaletted, pngAdam7), pngChunk("PLTE", []byte{1, 2, 3}), idat(tb, zeroRows(2048, 2048, 1, true)), iend))
		}},
		{"png-wide", png(func() image.Image { return gradient(MaxSide, MaxPixels/MaxSide, color.NRGBA64Model, true) })},
		{"jpeg-gray", jpg(jpegSpec{w: 2048, h: 2048, nComp: 1, sof: mSOF0})},
		{"jpeg-ycbcr444", jpg(jpegSpec{w: 2048, h: 2048, nComp: 3, sof: mSOF0})},
		{"jpeg-rgb", jpg(jpegSpec{w: 2048, h: 2048, nComp: 3, sof: mSOF0, extra: adobeAPP14(0)})},
		{"jpeg-cmyk", jpg(jpegSpec{w: 2048, h: 2048, nComp: 4, sof: mSOF0, extra: adobeAPP14(0)})},
		{"jpeg-ycck", jpg(jpegSpec{w: 2048, h: 2048, nComp: 4, sof: mSOF0, extra: adobeAPP14(2)})},
		{"jpeg-progressive-gray", jpg(jpegSpec{w: 2048, h: 2048, nComp: 1, sof: mSOF2, acBands: 2})},
		{"jpeg-progressive-ycbcr", jpg(jpegSpec{w: 1440, h: 1440, nComp: 3, sof: mSOF2, acBands: 2})},
	}
}

// totalAlloc returns the bytes allocated by f: an upper bound of the peak of
// the live heap it adds, whatever the GC does meanwhile.
func totalAlloc(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestCostBoundsDecoderAllocations(t *testing.T) {
	for _, c := range costCases() {
		t.Run(c.name, func(t *testing.T) {
			src := c.src(t)
			s, err := NewSet(newBudget()).Add(src)
			if err != nil {
				t.Fatal(err)
			}
			var d *Decoded
			got := totalAlloc(func() {
				d, err = Decode(s, newBudget())
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%T allocated %d, cost %d (%.2f)", d.img, got, s.cost, float64(got)/float64(s.cost))
			if float64(got) > 1.1*float64(s.cost) {
				t.Errorf("allocated %d > cost %d + 10%%", got, s.cost)
			}
		})
	}
}

// noise returns w×h deterministic pseudo-random NRGBA pixels: incompressible
// data that maximises the entropy-decoding cost per pixel.
func noise(w, h int, alpha bool) image.Image {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	x := uint32(1)
	for i := range m.Pix {
		x = x*1664525 + 1013904223
		m.Pix[i] = byte(x >> 24)
		if !alpha && i%4 == 3 {
			m.Pix[i] = 0xff
		}
	}
	return m
}

// shapeCases are the variants whose cost is not in their pixel count: whole
// MCUs of a one-pixel-high JPEG, per-row overhead, incompressible payloads.
func shapeCases() []costCase {
	jpg := func(s jpegSpec) func(testing.TB) string {
		return func(testing.TB) string { return jpegURL(synthJPEG(s)) }
	}
	return []costCase{
		{"jpeg-16384x1-444", jpg(jpegSpec{w: MaxSide, h: 1, nComp: 3, sof: mSOF0})},
		{"jpeg-16384x1-420", jpg(jpegSpec{w: MaxSide, h: 1, nComp: 3, sof: mSOF0, sub420: true})},
		{"jpeg-16384x1-444-31scans", jpg(jpegSpec{w: MaxSide, h: 1, nComp: 3, sof: mSOF2, acBands: 10})},
		{"jpeg-16384x1-420-31scans", jpg(jpegSpec{w: MaxSide, h: 1, nComp: 3, sof: mSOF2, acBands: 10, sub420: true})},
		{"jpeg-1x16384-420", jpg(jpegSpec{w: 1, h: MaxSide, nComp: 3, sof: mSOF0, sub420: true})},
		{"jpeg-noise-q100-800", func(tb testing.TB) string {
			tb.Helper()
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, noise(800, 800, false), &jpeg.Options{Quality: 100}); err != nil {
				tb.Fatal(err)
			}
			return jpegURL(buf.Bytes())
		}},
		{"png-1x16384-rgba", func(tb testing.TB) string { return pngURL(encodePNG(tb, gradient(1, MaxSide, color.NRGBAModel, true))) }},
		{"png-noise-700", func(tb testing.TB) string { return pngURL(encodePNG(tb, noise(700, 700, true))) }},
	}
}
