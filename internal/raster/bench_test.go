package raster

import (
	"compress/zlib"
	"image/color"
	"io"
	"testing"
)

// reencode writes d as the PDF writer does: Flate samples and alpha, or a JPEG.
func reencode(d *Decoded, jpeg bool) error {
	if jpeg {
		return d.WriteJPEG(io.Discard)
	}
	zw := zlib.NewWriter(io.Discard)
	if err := d.WriteSamples(zw); err != nil {
		return err
	}
	if d.Alpha {
		if err := d.WriteAlpha(zw); err != nil {
			return err
		}
	}
	return zw.Close()
}

func benchCases() []costCase {
	return []costCase{
		{"png-rgba-1mp", func(tb testing.TB) string { return pngURL(encodePNG(tb, gradient(1024, 1024, color.NRGBAModel, true))) }},
		{"png-16bit-1mp", func(tb testing.TB) string {
			return pngURL(encodePNG(tb, gradient(1024, 1024, color.NRGBA64Model, true)))
		}},
		{"jpeg-1mp", func(tb testing.TB) string {
			return jpegURL(encodeJPEG(tb, gradient(1024, 1024, color.NRGBAModel, false)))
		}},
		{"jpeg-progressive-1mp", func(testing.TB) string {
			return jpegURL(synthJPEG(jpegSpec{w: 1024, h: 1024, nComp: 3, sof: mSOF2, acBands: 3}))
		}},
		{"png-rgba-4mp", func(tb testing.TB) string { return pngURL(encodePNG(tb, gradient(2048, 2048, color.NRGBAModel, true))) }},
	}
}

func BenchmarkRasterDecode(b *testing.B) {
	for _, c := range benchCases() {
		b.Run(c.name, func(b *testing.B) {
			s, err := NewSet(newBudget()).Add(c.src(b))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				d, err := Decode(s, newBudget())
				if err != nil {
					b.Fatal(err)
				}
				if err := reencode(d, s.Format == JPEG); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(s.work()), "ns/work")
		})
	}
}
