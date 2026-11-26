package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"runtime"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// samples returns deterministic w*h*n bytes.
func samples(w, h, n int) []byte {
	b := make([]byte, w*h*n)
	for i := range b {
		b[i] = byte((i*7 + i/w) & 0xff)
	}
	return b
}

func rawImage(w, h int, gray bool, data, alpha []byte) Image {
	img := Image{Width: w, Height: h, Gray: gray, Data: func(o io.Writer) error {
		_, err := o.Write(data)
		return err
	}}
	if alpha != nil {
		img.Alpha = func(o io.Writer) error {
			_, err := o.Write(alpha)
			return err
		}
	}
	return img
}

func jpegBytes(t testing.TB, w, h int, gray bool) []byte {
	t.Helper()
	var src image.Image
	if gray {
		g := image.NewGray(image.Rect(0, 0, w, h))
		for i := range g.Pix {
			g.Pix[i] = byte(i * 3)
		}
		src = g
	} else {
		m := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				m.Set(x, y, color.RGBA{byte(x * 9), byte(y * 9), byte(x + y), 255})
			}
		}
		src = m
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pageWith(t testing.TB, w *Writer, ids ...ImageID) {
	t.Helper()
	c := &Content{}
	for i, id := range ids {
		c.DrawImage(id, float64(10+i), 20, 50, 40)
	}
	if _, err := w.AddPage(200, 200, c, false, nil); err != nil {
		t.Fatal(err)
	}
}

func finish(t testing.TB, w *Writer) {
	t.Helper()
	if err := w.Close(Info{Title: "img"}); err != nil {
		t.Fatal(err)
	}
}

func TestAddImageRoundTrip(t *testing.T) {
	gray := samples(5, 3, 1)
	rgb := samples(4, 6, 3)
	rgba := samples(3, 3, 3)
	alpha := samples(3, 3, 1)
	jc := jpegBytes(t, 16, 16, false)
	jg := jpegBytes(t, 8, 8, true)

	var buf bytes.Buffer
	w := newTestWriter(&buf)
	var ids []ImageID
	for _, img := range []Image{
		rawImage(5, 3, true, gray, nil),
		rawImage(4, 6, false, rgb, nil),
		rawImage(3, 3, false, rgba, alpha),
		{Width: 16, Height: 16, Filter: ImageDCT, Data: func(o io.Writer) error { _, err := o.Write(jc); return err }},
		{Width: 8, Height: 8, Gray: true, Filter: ImageDCT, Data: func(o io.Writer) error { _, err := o.Write(jg); return err }, Alpha: func(o io.Writer) error {
			_, err := o.Write(samples(8, 8, 1))
			return err
		}},
	} {
		id, err := w.AddImage(img)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	pageWith(t, w, ids...)
	finish(t, w)

	doc := pdftest.Validate(t, buf.Bytes())
	if len(doc.Images) != 5 {
		t.Fatalf("%d images, want 5", len(doc.Images))
	}
	check := func(name string, wantData, wantMask []byte, dct, grayCS bool) {
		t.Helper()
		im := doc.Images[name]
		if im == nil {
			t.Fatalf("%s missing", name)
		}
		if !bytes.Equal(im.Data, wantData) || !bytes.Equal(im.Mask, wantMask) || im.DCT != dct || im.Gray != grayCS {
			t.Errorf("%s: data/mask/filter/colour space differ", name)
		}
	}
	check("Im0", gray, nil, false, true)
	check("Im1", rgb, nil, false, false)
	check("Im2", rgba, alpha, false, false)
	check("Im3", jc, nil, true, false)
	check("Im4", jg, samples(8, 8, 1), true, true)
	for i := 0; i < 5; i++ {
		if !bytes.Contains(doc.Pages[0].Content, []byte("/Im"+string(rune('0'+i))+" Do")) {
			t.Errorf("content lacks Do for Im%d", i)
		}
	}
}

func TestDrawImageOperators(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id, err := w.AddImage(rawImage(1, 1, true, []byte{9}, nil))
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{}
	c.DrawImage(id, 10, 20, 30.5, 40)
	c.DrawImage(id, 0, 0, 1, 1)
	if got, want := string(c.Bytes()), "q\n30.5 0 0 40 10 20 cm\n/Im0 Do\nQ\nq\n1 0 0 1 0 0 cm\n/Im0 Do\nQ\n"; got != want {
		t.Errorf("content %q, want %q", got, want)
	}
	if len(c.images) != 1 {
		t.Errorf("%d resources for one image drawn twice", len(c.images))
	}
}

func TestDrawImageErrors(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id, err := w.AddImage(rawImage(1, 1, true, []byte{1}, nil))
	if err != nil {
		t.Fatal(err)
	}
	inf := 1e300
	for name, f := range map[string]func(*Content){
		"zero id": func(c *Content) { c.DrawImage(ImageID{}, 0, 0, 1, 1) },
		"zero w":  func(c *Content) { c.DrawImage(id, 0, 0, 0, 1) },
		"neg h":   func(c *Content) { c.DrawImage(id, 0, 0, 1, -1) },
		"nan":     func(c *Content) { c.DrawImage(id, 0, 0, nan(), 1) },
		"huge":    func(c *Content) { c.DrawImage(id, 0, 0, inf*inf, 1) },
		"huge x":  func(c *Content) { c.DrawImage(id, inf*inf, 0, 1, 1) },
	} {
		c := &Content{}
		f(c)
		if !errors.Is(c.Err(), ErrImage) {
			t.Errorf("%s: err %v, want ErrImage", name, c.Err())
		}
		if len(c.Bytes()) != 0 {
			t.Errorf("%s: wrote %q", name, c.Bytes())
		}
	}
}

func TestImageForeignAndAfterClose(t *testing.T) {
	var b1, b2 bytes.Buffer
	w1, w2 := newTestWriter(&b1), newTestWriter(&b2)
	id, err := w1.AddImage(rawImage(1, 1, true, []byte{1}, nil))
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{}
	c.DrawImage(id, 0, 0, 1, 1)
	if _, err := w2.AddPage(10, 10, c, false, nil); !errors.Is(err, ErrForeignImage) {
		t.Errorf("foreign image: %v", err)
	}
	pageWith(t, w1, id)
	finish(t, w1)
	if _, err := w1.AddImage(rawImage(1, 1, true, []byte{1}, nil)); !errors.Is(err, ErrClosed) {
		t.Errorf("after close: %v", err)
	}
}

func TestAddImageValidation(t *testing.T) {
	ok := func(o io.Writer) error { _, err := o.Write([]byte{0}); return err }
	for name, img := range map[string]Image{
		"zero width": {Width: 0, Height: 1, Data: ok},
		"neg height": {Width: 1, Height: -1, Data: ok},
		"too wide":   {Width: MaxImageSide + 1, Height: 1, Data: ok},
		"too high":   {Width: 1, Height: MaxImageSide + 1, Data: ok},
		"nil data":   {Width: 1, Height: 1},
		"bad filter": {Width: 1, Height: 1, Filter: 9, Data: ok},
	} {
		var buf bytes.Buffer
		w := newTestWriter(&buf)
		if _, err := w.AddImage(img); !errors.Is(err, ErrImage) {
			t.Errorf("%s: %v, want ErrImage", name, err)
		}
		if buf.Len() != 0 || w.Written() != 0 {
			t.Errorf("%s: wrote %d bytes", name, buf.Len())
		}
		if _, err := w.AddImage(rawImage(1, 1, true, []byte{1}, nil)); err != nil {
			t.Errorf("%s: writer unusable after a refused image: %v", name, err)
		}
	}
}

func TestAddImageDataMismatch(t *testing.T) {
	good := samples(4, 4, 3)
	cases := map[string]Image{
		"short data":  rawImage(4, 4, false, good[:len(good)-1], nil),
		"long data":   rawImage(4, 4, false, append(bytes.Clone(good), 0), nil),
		"empty":       rawImage(4, 4, false, nil, nil),
		"gray as rgb": rawImage(4, 4, true, good, nil),
		"short alpha": rawImage(4, 4, false, good, samples(4, 4, 1)[:15]),
		"long alpha":  rawImage(4, 4, false, good, samples(4, 5, 1)),
		"not jpeg":    {Width: 4, Height: 4, Filter: ImageDCT, Data: func(o io.Writer) error { _, err := o.Write([]byte("GIF89a")); return err }},
		"empty jpeg":  {Width: 4, Height: 4, Filter: ImageDCT, Data: func(io.Writer) error { return nil }},
	}
	for name, img := range cases {
		var buf bytes.Buffer
		w := newTestWriter(&buf)
		if _, err := w.AddImage(img); !errors.Is(err, ErrImageData) {
			t.Errorf("%s: %v, want ErrImageData", name, err)
			continue
		}
		if _, err := w.AddImage(rawImage(1, 1, true, []byte{1}, nil)); !errors.Is(err, ErrImageData) {
			t.Errorf("%s: error not sticky: %v", name, err)
		}
	}
}

func TestAddImageCallbackError(t *testing.T) {
	boom := errors.New("boom")
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	_, err := w.AddImage(Image{Width: 2, Height: 2, Gray: true, Data: func(o io.Writer) error {
		_, _ = o.Write([]byte{1})
		return boom
	}})
	if !errors.Is(err, boom) {
		t.Fatalf("err %v, want boom", err)
	}
	if _, err := w.AddPage(10, 10, &Content{}, false, nil); !errors.Is(err, boom) {
		t.Errorf("error not sticky: %v", err)
	}
}

func TestAddImageLimit(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	var ids []ImageID
	for i := 0; i < MaxImages; i++ {
		id, err := w.AddImage(rawImage(1, 1, true, []byte{byte(i)}, nil))
		if err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	if _, err := w.AddImage(rawImage(1, 1, true, []byte{1}, nil)); !errors.Is(err, ErrTooManyImages) {
		t.Fatalf("65th image: %v", err)
	}
	pageWith(t, w, ids...)
	finish(t, w)
	if doc := pdftest.Validate(t, buf.Bytes()); len(doc.Images) != MaxImages {
		t.Errorf("%d images in the document", len(doc.Images))
	}
}

func TestAddImageOutputBudget(t *testing.T) {
	data := samples(64, 64, 3)
	for _, max := range []int64{10, 200, 1000} {
		var buf bytes.Buffer
		w := NewWriter(&buf, newBudget(max))
		_, err := w.AddImage(rawImage(64, 64, false, data, samples(64, 64, 1)))
		var le *budget.LimitError
		if !errors.As(err, &le) || le.Kind != budget.OutputBytes {
			t.Errorf("max %d: err %v, want an OutputBytes LimitError", max, err)
		}
		if int64(buf.Len()) > max {
			t.Errorf("max %d: wrote %d bytes", max, buf.Len())
		}
	}
}

func TestImageWriteError(t *testing.T) {
	w := NewWriter(failAfter{n: 300}, newBudget(1<<30))
	_, err := w.AddImage(rawImage(64, 64, false, samples(64, 64, 3), nil))
	if !errors.Is(err, errDisk) {
		t.Fatalf("err %v, want the write error", err)
	}
}

var errDisk = errors.New("disk full")

type failAfter struct{ n int }

func (f failAfter) Write(p []byte) (int, error) {
	if len(p) > f.n {
		return 0, errDisk
	}
	return len(p), nil
}

func buildImageDoc(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	a, err := w.AddImage(rawImage(32, 32, false, samples(32, 32, 3), samples(32, 32, 1)))
	if err != nil {
		t.Fatal(err)
	}
	pageWith(t, w, a)
	b, err := w.AddImage(rawImage(8, 8, true, samples(8, 8, 1), nil))
	if err != nil {
		t.Fatal(err)
	}
	pageWith(t, w, b, a)
	finish(t, w)
	return buf.Bytes()
}

func TestImageDeterministic(t *testing.T) {
	first := sha256.Sum256(buildImageDoc(t))
	for i := 0; i < 10; i++ {
		if got := sha256.Sum256(buildImageDoc(t)); got != first {
			t.Fatalf("run %d differs", i)
		}
	}
	pdftest.Validate(t, buildImageDoc(t))
}

func TestNoImageNoXObject(t *testing.T) {
	if bytes.Contains(buildDoc(t, "t", nil), []byte("XObject")) {
		t.Error("a document without images lists /XObject")
	}
}

type countSink struct{ n int64 }

func (c *countSink) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// TestAddImageHeap checks, with the zlib encoder already pooled, that a 4 Mi px RGB image is streamed: the heap grows
// by far less than the compressed image (incompressible samples).
func TestAddImageHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("4 Mi px image")
	}
	const side = 2048
	row := make([]byte, side*3)
	var x uint32 = 1
	img := Image{Width: side, Height: side, Data: func(o io.Writer) error {
		for y := 0; y < side; y++ {
			for i := range row {
				x = x*1664525 + 1013904223
				row[i] = byte(x >> 24)
			}
			if _, err := o.Write(row); err != nil {
				return err
			}
		}
		return nil
	}}
	sink := &countSink{}
	w := NewWriter(sink, newBudget(1<<30))
	var peak uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	runtime.GC()
	warm := NewWriter(io.Discard, newBudget(1<<30))
	if _, err := warm.AddImage(rawImage(8, 8, false, samples(8, 8, 3), nil)); err != nil {
		t.Fatal(err)
	}
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
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
	_, err := w.AddImage(img)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if sink.n < 12<<20 {
		t.Fatalf("only %d bytes written: samples compressed, the test proves nothing", sink.n)
	}
	limit := int64(1 << 20)
	if raceEnabled {
		limit += 2 << 20 // the race detector makes sync.Pool drop the pooled zlib encoder
	}
	if peak > base.HeapAlloc+uint64(limit) {
		t.Errorf("heap grew by %d bytes while streaming %d bytes", peak-base.HeapAlloc, sink.n)
	}
}

func BenchmarkAddImage(b *testing.B) {
	const side = 1024
	data := samples(side, side, 3)
	alpha := samples(side, side, 1)
	img := rawImage(side, side, false, data, alpha)
	b.SetBytes(int64(len(data) + len(alpha)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := NewWriter(io.Discard, newBudget(1<<30))
		if _, err := w.AddImage(img); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAddImageDCT(b *testing.B) {
	data := jpegBytes(b, 256, 256, false)
	img := Image{Width: 256, Height: 256, Filter: ImageDCT, Data: func(o io.Writer) error { _, err := o.Write(data); return err }}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := NewWriter(io.Discard, newBudget(1<<30))
		if _, err := w.AddImage(img); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAddImagePixelCap(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	called := false
	img := Image{Width: 2049, Height: 2048, Data: func(io.Writer) error { called = true; return nil }}
	if _, err := w.AddImage(img); !errors.Is(err, ErrImage) {
		t.Fatalf("err %v, want ErrImage", err)
	}
	if called || buf.Len() != 0 {
		t.Errorf("Data called %v, %d bytes written", called, buf.Len())
	}
	img.Width = 2048
	img.Data = func(io.Writer) error { return nil }
	if _, err := w.AddImage(img); !errors.Is(err, ErrImageData) {
		t.Errorf("2048x2048 is within the cap: %v", err)
	}
}

func TestAddImageWorkBudget(t *testing.T) {
	l := budget.Defaults()
	l[budget.Work] = 2
	var buf bytes.Buffer
	w := NewWriter(&buf, budget.New(context.Background(), l, nil))
	data := samples(64, 64, 3)
	_, err := w.AddImage(Image{Width: 64, Height: 64, Data: func(o io.Writer) error {
		_, _ = o.Write(data) // ignores the error on purpose
		return nil
	}})
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("err %v, want a Work LimitError", err)
	}
}

func TestAddImageIgnoredWriteError(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, newBudget(300))
	data := samples(64, 64, 3)
	_, err := w.AddImage(Image{Width: 64, Height: 64, Data: func(o io.Writer) error {
		_, _ = o.Write(data) // ignores the error on purpose
		return nil
	}})
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.OutputBytes {
		t.Fatalf("err %v, want an OutputBytes LimitError", err)
	}
}

func TestDrawImageSaveDepth(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id, err := w.AddImage(rawImage(1, 1, true, []byte{1}, nil))
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{}
	for i := 0; i < MaxSaveDepth; i++ {
		c.Save()
	}
	n := len(c.Bytes())
	c.DrawImage(id, 0, 0, 1, 1)
	if !errors.Is(c.Err(), ErrSaveDepth) || len(c.Bytes()) != n {
		t.Errorf("err %v, appended %d bytes", c.Err(), len(c.Bytes())-n)
	}
}
