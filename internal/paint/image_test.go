package paint

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/raster"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// pngBytes encodes a w×h image whose pixels are opaque, or half transparent
// when alpha is set.
func pngBytes(tb testing.TB, w, h int, alpha bool) []byte {
	tb.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := uint8(255)
			if alpha && (x+y)%2 == 0 {
				a = 128
			}
			m.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 3), B: 99, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func addSource(tb testing.TB, set *raster.Set, src string) *raster.Source {
	tb.Helper()
	s, err := set.Add(src)
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func imageItem(s *raster.Source) display.Image {
	return display.Image{Rect: display.Rect{X: 10, Y: 20, W: 100, H: 50}, Src: s, Opacity: 1}
}

// One Source drawn on three pages is one XObject, drawn by each page; a second
// Source first drawn later is the next name.
func TestImageOneXObjectPerSource(t *testing.T) {
	e := newEnv(t)
	set := raster.NewSet(e.b)
	a := addSource(t, set, dataURL("image/png", pngBytes(t, 8, 4, false)))
	b := addSource(t, set, dataURL("image/png", pngBytes(t, 3, 3, true)))
	var contents []string
	for i := range 3 {
		l := display.List{imageItem(a)}
		if i == 1 {
			l = append(l, imageItem(b), imageItem(a))
		}
		c, _ := e.page(t, a4, l)
		contents = append(contents, c)
	}
	doc := e.finish(t)
	if len(doc.Images) != 2 {
		t.Fatalf("%d images, want 2", len(doc.Images))
	}
	for i, c := range contents {
		if !strings.Contains(c, "/Im0 Do") {
			t.Errorf("page %d does not draw Im0:\n%s", i+1, c)
		}
		balanced(t, c)
	}
	if n := strings.Count(contents[1], "/Im0 Do"); n != 2 || !strings.Contains(contents[1], "/Im1 Do") {
		t.Errorf("page 2 draws Im0 %d times, Im1 %v:\n%s", n, strings.Contains(contents[1], "/Im1 Do"), contents[1])
	}
	im0, im1 := doc.Images["Im0"], doc.Images["Im1"]
	if im0.Width != 8 || im0.Height != 4 || im0.Mask != nil || im0.DCT {
		t.Errorf("Im0 %dx%d mask %v dct %v, want 8x4 Flate without mask", im0.Width, im0.Height, im0.Mask != nil, im0.DCT)
	}
	if im1.Width != 3 || im1.Mask == nil {
		t.Errorf("Im1 %dx%d mask %v, want 3x3 with a soft mask", im1.Width, im1.Height, im1.Mask != nil)
	}
	if !strings.Contains(contents[0], "75 0 0 37.5 7.5 697.5 cm") {
		t.Errorf("image matrix not in points (100x50 px at 10,20):\n%s", contents[0])
	}
}

// A JPEG is re-encoded: the output holds a DCT stream that is not the source
// stream.
func TestImageJPEGReencoded(t *testing.T) {
	m := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for i := range m.Pix {
		m.Pix[i] = uint8(i * 13)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	s := addSource(t, raster.NewSet(e.b), dataURL("image/jpeg", buf.Bytes()))
	e.page(t, a4, display.List{imageItem(s)})
	doc := e.finish(t)
	im := doc.Images["Im0"]
	if im == nil || !im.DCT || im.Width != 32 || im.Height != 16 {
		t.Fatalf("image %+v, want a 32x16 DCT image", im)
	}
	if bytes.Equal(im.Data, buf.Bytes()) || bytes.Contains(e.buf.Bytes(), buf.Bytes()[2:64]) {
		t.Error("the source JPEG bytes reach the output")
	}
}

// Every corpus image the raster layer draws paints into a valid PDF, gray
// JPEGs and CMYK included.
func TestImageCorpusFiles(t *testing.T) {
	for _, name := range []string{"video-001.png", "video-001.jpeg", "video-001.progressive.jpeg", "video-001.cmyk.jpeg", "video-005.gray.jpeg", "video-005.gray.png"} {
		t.Run(name, func(t *testing.T) {
			root, err := os.OpenRoot(filepath.Join("..", "..", "testdata", "images"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			}()
			data, err := fs.ReadFile(root.FS(), name)
			if err != nil {
				t.Fatal(err)
			}
			mime := "image/png"
			if strings.HasSuffix(name, ".jpeg") {
				mime = "image/jpeg"
			}
			e := newEnv(t)
			s := addSource(t, raster.NewSet(e.b), dataURL(mime, data))
			e.page(t, a4, display.List{imageItem(s)})
			doc := e.finish(t)
			im := doc.Images["Im0"]
			if im == nil || im.Width != s.Width || im.Height != s.Height || im.DCT != (s.Format == raster.JPEG) {
				t.Fatalf("image %+v for a %dx%d source", im, s.Width, s.Height)
			}
			if gray := strings.Contains(name, "gray"); im.Gray != gray {
				t.Errorf("gray %v, want %v", im.Gray, gray)
			}
		})
	}
}

// A Source whose data is corrupt behind a valid header draws nothing, is
// warned once with the reason and is not decoded again (its Work is charged
// once); the page renders.
func TestImageCorruptOnce(t *testing.T) {
	var corrupt string
	for _, c := range testcorpus.ImageCases() {
		if c.Name == "png_truncated_idat" {
			corrupt = c.Src
		}
	}
	if corrupt == "" {
		t.Fatal("corpus case png_truncated_idat not found")
	}
	e := newEnv(t)
	s := addSource(t, raster.NewSet(e.b), corrupt)
	ok := addSource(t, raster.NewSet(e.b), dataURL("image/png", pngBytes(t, 2, 2, false)))
	c1, _ := e.page(t, a4, display.List{imageItem(s), imageItem(ok)})
	work := e.b.Used(budget.Work)
	c2, _ := e.page(t, a4, display.List{imageItem(s), imageItem(s)})
	if used := e.b.Used(budget.Work) - work; used > 10 {
		t.Errorf("drawing the failed image again cost %d Work: decoded again?", used)
	}
	doc := e.finish(t)
	if len(doc.Images) != 1 || !strings.Contains(c1, "/Im0 Do") || strings.Contains(c2, "Do") {
		t.Errorf("%d images; page 1:\n%s\npage 2:\n%s", len(doc.Images), c1, c2)
	}
	ws, _ := e.b.Warnings()
	var got []string
	for _, w := range ws {
		got = append(got, fmt.Sprintf("%s/%s x%d", w.Code, w.Detail, w.Count))
	}
	if len(got) != 1 || got[0] != "image-rejected/corrupt x1" {
		t.Errorf("warnings %v, want one image-rejected/corrupt", got)
	}
}

// A budget error while decoding fails the page: it is not a rejected image.
func TestImageBudgetErrorFails(t *testing.T) {
	e := newEnv(t)
	s := addSource(t, raster.NewSet(e.b), dataURL("image/png", pngBytes(t, 64, 64, false)))
	l := budget.Defaults()
	l[budget.Work] = e.b.Used(budget.Work) + 100
	e.painter.b = budget.New(t.Context(), l, nil)
	if err := e.painter.b.Charge(budget.Work, e.b.Used(budget.Work)); err != nil {
		t.Fatal(err)
	}
	_, err := e.painter.Page(&pdf.Content{}, a4, display.List{imageItem(s)})
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("err %v, want a Work LimitError", err)
	}
	if ws, _ := e.painter.b.Warnings(); len(ws) != 0 {
		t.Errorf("warnings %v, want none", ws)
	}
}

// The writer's pixel and image caps and a raster reject are rejected images;
// budget and other writer errors fail the render.
func TestImageRejectReason(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{fmt.Errorf("%w: 4097x1024 pixels", pdf.ErrImage), "size"},
		{&raster.RejectError{Reason: raster.ReasonCorrupt}, "corrupt"},
		{pdf.ErrTooManyImages, "count"},
		{&budget.LimitError{Kind: budget.OutputBytes}, ""},
		{errors.New("write failed"), ""},
	} {
		reason, ok := rejectReason(tc.err)
		if reason != tc.reason || ok != (tc.reason != "") {
			t.Errorf("%v: %q %v, want %q", tc.err, reason, ok, tc.reason)
		}
	}
}

// Opacity sets the fill alpha (ExtGState ca) inside the image's q…Q, before
// its Do, also over a soft mask; opaque resets an alpha left by a fill before;
// opacity 0 draws nothing and does not decode.
func TestImageOpacity(t *testing.T) {
	tests := []struct {
		name    string
		opacity float64
		alpha   bool
		want    string // operators before the Do, "" for no draw
		ca      string // the /ca of the ExtGState used, "" for none
	}{
		{"opacity 1 after a translucent fill", 1, false, "q\n/GT0 gs\n0 0 0 rg\nq\n75 0 0 37.5 7.5 697.5 cm\n/Im0 Do\nQ\nQ\n", ""},
		{"opacity 0.5", 0.5, false, "q\n/GT127 gs\n0 0 0 rg\nq\n75 0 0 37.5 7.5 697.5 cm\n/Im0 Do\nQ\nQ\n", "/GT127 << /Type /ExtGState /ca 0.502"},
		{"opacity 0.5 over a soft mask", 0.5, true, "q\n/GT127 gs\n0 0 0 rg\nq\n75 0 0 37.5 7.5 697.5 cm\n/Im0 Do\nQ\nQ\n", "/GT127 << /Type /ExtGState /ca 0.502"},
		{"opacity 0", 0, true, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			s := addSource(t, raster.NewSet(e.b), dataURL("image/png", pngBytes(t, 4, 4, tc.alpha)))
			it := imageItem(s)
			it.Opacity = tc.opacity
			c, _ := e.page(t, a4, display.List{
				display.FillRect{Rect: display.Rect{W: 5, H: 5}, Color: css.Color{R: 1, A: 0.25}},
				it,
			})
			doc := e.finish(t)
			balanced(t, c)
			if tc.want == "" {
				if strings.Contains(c, "Do") || len(doc.Images) != 0 {
					t.Errorf("drawn or written at opacity 0:\n%s", c)
				}
				return
			}
			if !strings.HasSuffix(c, tc.want) {
				t.Errorf("content ends\n%s\nwant\n%s", c, tc.want)
			}
			if tc.ca != "" && !bytes.Contains(e.buf.Bytes(), []byte(tc.ca)) {
				t.Errorf("no ExtGState %q in the output", tc.ca)
			}
			if im := doc.Images["Im0"]; im == nil || (im.Mask != nil) != tc.alpha {
				t.Errorf("image %+v, want a soft mask %v", im, tc.alpha)
			}
		})
	}
}

// An image off the page, empty, without opacity or without a Source is never
// decoded nor written.
func TestImageSkips(t *testing.T) {
	e := newEnv(t)
	off := addSource(t, raster.NewSet(e.b), dataURL("image/png", pngBytes(t, 3, 3, false)))
	c, _ := e.page(t, a4, display.List{
		display.Image{Rect: display.Rect{X: 900, Y: 20, W: 10, H: 10}, Src: off, Opacity: 1},
		display.Image{Rect: display.Rect{X: 10, Y: 20, W: 10, H: 10}, Src: off},
		display.Image{Rect: display.Rect{X: 10, Y: 20, W: 0, H: 10}, Src: off, Opacity: 1},
		display.Image{Rect: display.Rect{W: 10, H: 10}, Opacity: 1},
	})
	doc := e.finish(t)
	if len(doc.Images) != 0 || strings.Contains(c, "Do") {
		t.Errorf("%d images written, content:\n%s", len(doc.Images), c)
	}
}

// bigAlphaPNG is a 2048×2048 PNG with alpha: an image at raster.MaxPixels.
func bigAlphaPNG(tb testing.TB) string {
	tb.Helper()
	const side = 2048
	m := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			i := m.PixOffset(x, y)
			m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = uint8(x&0xff), uint8(y&0xff), uint8((x^y)&0xff), uint8(255-(x+y)&63)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		tb.Fatal(err)
	}
	return dataURL("image/png", buf.Bytes())
}

// paintBig paints src once on a page of a new document written to io.Discard.
func paintBig(tb testing.TB, src string) {
	tb.Helper()
	b := testBudget()
	w := pdf.NewWriter(io.Discard, b)
	p := NewPainter(w, nil, b)
	s, err := raster.NewSet(b).Add(src)
	if err != nil {
		tb.Fatal(err)
	}
	c := &pdf.Content{}
	if _, err := p.Page(c, a4, display.List{display.Image{Rect: display.Rect{W: 800, H: 800}, Src: s, Opacity: 1}}); err != nil {
		tb.Fatal(err)
	}
	if _, err := w.AddPage(600, 750, c, false, nil); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(pdf.Info{}); err != nil {
		tb.Fatal(err)
	}
}

// Painting a 2048×2048 PNG with alpha stays within a 64 MiB heap target: one
// decoded image, one row and the zlib state at a time.
func TestImagePaintHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("4 Mi px image")
	}
	src := bigAlphaPNG(t)
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
			runtime.Gosched()
		}
	}()
	paintBig(t, src)
	close(stop)
	<-done
	var grown uint64
	if peak > base.HeapAlloc {
		grown = peak - base.HeapAlloc
	}
	t.Logf("heap grew by %.1f MiB", float64(grown)/(1<<20))
	if grown > 64<<20 {
		t.Errorf("heap grew by %d bytes, target 64 MiB", grown)
	}
}

func BenchmarkPaintImage2048Alpha(b *testing.B) {
	src := bigAlphaPNG(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		paintBig(b, src)
	}
}
