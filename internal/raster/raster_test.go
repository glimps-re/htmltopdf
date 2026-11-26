package raster

import (
	"context"
	"errors"
	"image"
	"image/color"
	"io"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func reasonOf(err error) Reason {
	var re *RejectError
	if errors.As(err, &re) {
		return re.Reason
	}
	return 0
}

// pngHeaderOnly is a PNG whose header is valid but whose IDAT is empty: the
// header pass accepts it, which is all the size tests need.
func pngHeaderOnly(w, h uint32, depth, colorType, interlace byte) string {
	chunks := [][]byte{ihdr(w, h, depth, colorType, interlace)}
	if colorType == pngPaletted {
		chunks = append(chunks, pngChunk("PLTE", []byte{0, 0, 0}))
	}
	return pngURL(rawPNG(append(chunks, pngChunk("IDAT", nil), iend)...))
}

func TestAddReasons(t *testing.T) {
	tiny := pngURL(encodePNG(t, gradient(1, 1, color.NRGBAModel, false)))
	jpg := encodeJPEG(t, gradient(8, 8, color.NRGBAModel, false))
	for _, c := range []struct {
		name string
		src  string
		want Reason
	}{
		{"png", tiny, 0},
		{"jpeg", jpegURL(jpg), 0},
		{"jpeg labelled png", pngURL(jpg), 0},
		{"png labelled jpg", strings.Replace(tiny, "image/png", "image/jpg", 1), 0},
		{"http", "http://example.com/a.png", ReasonScheme},
		{"file", "file:///etc/passwd", ReasonScheme},
		{"protocol-relative", "//host/a.png", ReasonScheme},
		{"gif", "data:image/gif;base64,R0lGODlhAQABAAAAACw=", ReasonType},
		{"no base64", "data:image/png,%89PNG", ReasonEncoding},
		{"invalid char", tiny[:30] + "*" + tiny[31:], ReasonEncoding},
		{"empty payload", "data:image/png;base64,", ReasonFormat},
		{"unknown magic", pngURL([]byte("GIF89a......")), ReasonFormat},
		{"png magic only", pngURL([]byte(pngMagic)), ReasonFormat},
		{"png zero width", pngHeaderOnly(0, 10, 8, 6, 0), ReasonFormat},
		{"png bad ihdr crc", func() string {
			b := rawPNG(ihdr(1, 1, 8, 6, 0), iend)
			b[29]++
			return pngURL(b)
		}(), ReasonFormat},
		{"png idat before ihdr", pngURL(rawPNG(pngChunk("IDAT", nil), ihdr(1, 1, 8, 6, 0), iend)), ReasonFormat},
		{"png bad colour type", pngHeaderOnly(1, 1, 8, 5, 0), ReasonFormat},
		{"png side at limit", pngHeaderOnly(MaxSide, MaxPixels/MaxSide, 8, 6, 0), 0},
		{"png side over", pngHeaderOnly(MaxSide+1, 1, 8, 6, 0), ReasonSize},
		{"png height over", pngHeaderOnly(1, MaxSide+1, 8, 6, 0), ReasonSize},
		{"png 65535x1", pngHeaderOnly(65535, 1, 8, 6, 0), ReasonSize},
		{"png 16384 square", pngHeaderOnly(16384, 16384, 8, 6, 0), ReasonSize},
		{"png max uint32", pngHeaderOnly(1<<32-1, 1<<32-1, 8, 6, 0), ReasonFormat},
		{"png pixels at limit", pngHeaderOnly(2048, 2048, 8, 6, 0), 0},
		{"png pixels over", pngHeaderOnly(2049, 2048, 8, 6, 0), ReasonSize},
		{"png rgba16 cost at limit", pngHeaderOnly(2048, 2048, 16, 6, 0), 0},
		{"png interlaced rgba8 cost at limit", pngHeaderOnly(2048, 2048, 8, 6, pngAdam7), 0},
		{"png interlaced rgba16 cost over", pngHeaderOnly(2048, 2048, 16, 6, pngAdam7), ReasonSize},
		{"png interlaced rgba16 cost at limit", pngHeaderOnly(2048, 1024, 16, 6, pngAdam7), 0},
		{"png interlaced rgba16 cost just over", pngHeaderOnly(2048, 1025, 16, 6, pngAdam7), ReasonSize},
		{"png tEXt flood", pngURL(rawPNG(append([][]byte{ihdr(1, 1, 8, 0, 0)},
			append(repeatChunk(pngChunk("tEXt", []byte("k\x00"+strings.Repeat("v", 1000))), 1500),
				idat(t, []byte{0, 0}), iend)...)...)), 0},
		{"jpeg truncated before sof", jpegURL(jpg[:20]), ReasonFormat},
		{"jpeg no sof", jpegURL([]byte{0xff, 0xd8, 0xff, 0xd9}), ReasonFormat},
		{"jpeg sos before sof", jpegURL([]byte{0xff, 0xd8, 0xff, mSOS, 0, 2, 0xff, 0xd9}), ReasonFormat},
		{"jpeg short segment", jpegURL([]byte{0xff, 0xd8, 0xff, 0xe1, 0, 1, 0xff, 0xd9}), ReasonFormat},
		{"jpeg segment past end", jpegURL([]byte{0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}), ReasonFormat},
		{"jpeg arithmetic sof9", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: 0xc9})), ReasonFormat},
		{"jpeg lossless sof3", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: 0xc3})), ReasonFormat},
		{"jpeg 12-bit sof1", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: mSOF1, precision: 12})), ReasonFormat},
		{"jpeg 8-bit sof1", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: mSOF1})), 0},
		{"jpeg 2 components", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 2, sof: mSOF0})), ReasonFormat},
		{"jpeg two sof", jpegURL(synthJPEG(jpegSpec{
			w: 8, h: 8, nComp: 1, sof: mSOF0,
			extra: segmentBytes(mSOF0, []byte{8, 0, 8, 0, 8, 1, 1, 0x11, 0}),
		})), ReasonFormat},
		{"jpeg zero height", jpegURL(synthJPEG(jpegSpec{w: 8, h: 0, nComp: 1, sof: mSOF0})), ReasonFormat},
		{"jpeg sampling 0", jpegURL(synthJPEG(jpegSpec{
			w: 8, h: 8, nComp: 1, sof: mSOF0,
			extra: segmentBytes(mSOF0, []byte{8, 0, 8, 0, 8, 1, 1, 0x01, 0}),
		})), ReasonFormat},
		{"jpeg 65500 square", jpegURL(synthJPEG(jpegSpec{w: 65500, h: 65500, nComp: 1, sof: mSOF0, headerOnly: true})), ReasonSize},
		{"jpeg pixels over", jpegURL(synthJPEG(jpegSpec{w: 2049, h: 2048, nComp: 1, sof: mSOF0})), ReasonSize},
		{"jpeg scans at limit", jpegURL(synthJPEG(jpegSpec{w: 16, h: 16, nComp: 1, sof: mSOF2, acBands: MaxJPEGScans - 1})), 0},
		{"jpeg scans over", jpegURL(synthJPEG(jpegSpec{w: 16, h: 16, nComp: 1, sof: mSOF2, acBands: MaxJPEGScans})), ReasonFormat},
		{"jpeg markers at limit", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: mSOF0, appMarkers: MaxJPEGMarkers - 4})), 0},
		{"jpeg markers over", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: mSOF0, appMarkers: MaxJPEGMarkers - 3})), ReasonFormat},
		{"jpeg 5000 app markers", jpegURL(synthJPEG(jpegSpec{w: 8, h: 8, nComp: 1, sof: mSOF0, appMarkers: 5000})), ReasonFormat},
		{"jpeg progressive cost at limit", jpegURL(synthJPEG(jpegSpec{w: 1488, h: 1488, nComp: 3, sof: mSOF2, acBands: 1})), 0},
		{"jpeg progressive cost just over", jpegURL(synthJPEG(jpegSpec{w: 1489, h: 1489, nComp: 3, sof: mSOF2, acBands: 1})), ReasonSize},
		{"jpeg cmyk cost at limit", jpegURL(synthJPEG(jpegSpec{w: 2048, h: 2048, nComp: 4, sof: mSOF0, extra: adobeAPP14(0)})), 0},
		{"jpeg cmyk padded cost just over", jpegURL(synthJPEG(jpegSpec{w: 2041, h: 2049, nComp: 4, sof: mSOF0, headerOnly: true, extra: adobeAPP14(0)})), ReasonSize},
		{"jpeg truncated after sos", jpegURL(synthJPEG(jpegSpec{w: 64, h: 64, nComp: 1, sof: mSOF0, truncateEnd: true})), 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := NewSet(newBudget()).Add(c.src)
			if got := reasonOf(err); got != c.want {
				t.Fatalf("got %v (%v), want %v", got, err, c.want)
			}
			if err != nil && reasonOf(err) == 0 {
				t.Fatalf("non-reject error %v", err)
			}
			if c.want == 0 && (s == nil || s.Width < 1 || s.Height < 1 || s.cost > MaxDecodeBytes) {
				t.Fatalf("bad source %+v", s)
			}
		})
	}
}

func repeatChunk(c []byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = c
	}
	return out
}

// distinctPNG returns the n-th distinct 2×1 gray PNG.
func distinctPNG(t *testing.T, n int) string {
	t.Helper()
	m := gradient(2, 1, color.GrayModel, false)
	g, _ := m.(interface{ SetGray(x, y int, c color.Gray) })
	g.SetGray(0, 0, color.Gray{Y: uint8(n & 0xff)})
	if n > 255 {
		g.SetGray(1, 0, color.Gray{Y: uint8(n >> 8 & 0xff)})
	}
	return pngURL(encodePNG(t, m))
}

func TestAddCountsDistinctImages(t *testing.T) {
	set := NewSet(newBudget())
	first, err := set.Add(distinctPNG(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < MaxImages; i++ {
		if _, err := set.Add(distinctPNG(t, i)); err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
	}
	if _, err := set.Add(distinctPNG(t, MaxImages)); reasonOf(err) != ReasonCount {
		t.Fatalf("image %d: %v, want count", MaxImages+1, err)
	}
	again, err := set.Add(distinctPNG(t, 0))
	if err != nil || again != first {
		t.Fatalf("duplicate after the cap: %p, %v; want %p", again, err, first)
	}
	b := set.b
	used := b.Used(budget.Work)
	for _, src := range []string{"http://x/a.png", "data:image/png;base64,*", distinctPNG(t, MaxImages+1)} {
		if _, err := set.Add(src); reasonOf(err) != ReasonCount {
			t.Fatalf("%.30q after the cap: %v, want count", src, err)
		}
	}
	if b.Used(budget.Work) != used {
		t.Error("a src past the cap was read")
	}
}

func TestAddTotalPixels(t *testing.T) {
	set := NewSet(newBudget())
	per := MaxTotalPixels / MaxPixels
	for i := range per {
		if _, err := set.Add(pngHeaderOnly(2048, 2048, 8, 0, 0) + strings.Repeat(" ", i)); err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
	}
	if _, err := set.Add(pngHeaderOnly(1, 1, 8, 0, 0)); reasonOf(err) != ReasonTotal {
		t.Fatalf("one pixel over the total: %v, want total", err)
	}
	set = NewSet(newBudget())
	if _, err := set.Add(pngHeaderOnly(2048, 2047, 8, 0, 0)); err != nil {
		t.Fatal(err)
	}
	for i := range per - 1 {
		if _, err := set.Add(pngHeaderOnly(2048, 2048, 8, 0, 0) + strings.Repeat(" ", i)); err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
	}
	if _, err := set.Add(pngHeaderOnly(2048, 1, 8, 0, 0)); err != nil {
		t.Fatalf("total exactly at the limit: %v", err)
	}
}

func TestAddDeduplicatesWithoutCharging(t *testing.T) {
	b := newBudget()
	set := NewSet(b)
	src := pngURL(encodePNG(t, gradient(3, 3, color.NRGBAModel, true)))
	s1, err := set.Add(src)
	if err != nil {
		t.Fatal(err)
	}
	used := b.Used(budget.Work)
	if want := int64(len(src)+63)/64 + headerWork; used != want {
		t.Errorf("Work %d, want %d", used, want)
	}
	for range 10000 {
		s2, err := set.Add(src)
		if err != nil || s2 != s1 {
			t.Fatalf("duplicate: %p, %v", s2, err)
		}
	}
	if b.Used(budget.Work) != used {
		t.Errorf("duplicates charged %d Work", b.Used(budget.Work)-used)
	}
	if s1.Format != PNG || s1.Width != 3 || s1.Height != 3 || s1.gray {
		t.Errorf("source %+v", s1)
	}
}

func TestAddBudgetErrorPropagates(t *testing.T) {
	l := budget.Defaults()
	l[budget.Work] = 10
	set := NewSet(budget.New(context.Background(), l, nil))
	_, err := set.Add(pngURL(encodePNG(t, gradient(1, 1, color.GrayModel, false))))
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("got %v, want a Work LimitError", err)
	}
	if reasonOf(err) != 0 {
		t.Fatal("budget error reported as a rejection")
	}
}

func TestReasonStrings(t *testing.T) {
	want := []string{"scheme", "type", "encoding", "format", "size", "count", "total", "corrupt"}
	for i, w := range want {
		r := Reason(i + 1)
		if r.String() != w {
			t.Errorf("%d: %q, want %q", r, r.String(), w)
		}
		if got := (&RejectError{Reason: r}).Error(); !strings.HasSuffix(got, w) {
			t.Errorf("Error() = %q", got)
		}
	}
	if Reason(0).String() != "Reason(0)" || Reason(99).String() != "Reason(99)" {
		t.Error("unknown reasons")
	}
}

func TestAddRecoversHeaderPanics(t *testing.T) {
	for _, f := range []Format{PNG, JPEG} {
		saved := configDecoders[f]
		configDecoders[f] = func(io.Reader) (image.Config, error) { panic("header decoder bug") }
		src := pngURL(encodePNG(t, gradient(2, 2, color.GrayModel, false)))
		if f == JPEG {
			src = jpegURL(encodeJPEG(t, gradient(8, 8, color.GrayModel, false)))
		}
		_, err := NewSet(newBudget()).Add(src)
		configDecoders[f] = saved
		if reasonOf(err) != ReasonFormat {
			t.Errorf("format %d: %v, want format", f, err)
		}
	}
}
