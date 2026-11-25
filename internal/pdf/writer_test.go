package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func newBudget(maxOut int64) *budget.Budget {
	l := budget.Defaults()
	l[budget.OutputBytes] = maxOut
	return budget.New(context.Background(), l, nil)
}

func newTestWriter(buf *bytes.Buffer) *Writer {
	return NewWriter(buf, newBudget(1<<30))
}

func sampleContent() *Content {
	c := &Content{}
	c.Save()
	c.FillColor(Color{R: 1, A: 0.5})
	c.Rect(10, 10, 100, 50)
	c.Fill()
	c.StrokeColor(Color{B: 1, A: 1})
	c.LineWidth(2)
	c.RoundedRect(20, 20, 80, 40, [4]float64{5, 0, 10, 100})
	c.Stroke()
	c.Save()
	c.Concat(1, 0, 0, -1, 0, 842)
	c.Rect(0, 0, 50, 50)
	c.Clip()
	c.Restore()
	c.Restore()
	return c
}

func buildDoc(t testing.TB, title string, links []Link) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	if _, err := w.AddPage(595.28, 841.89, sampleContent(), false, links); err != nil {
		t.Fatal(err)
	}
	deferred, err := w.AddPage(300, 200, nil, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddPage(300, 200, &Content{}, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteDeferred(deferred, sampleContent()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{Title: title}); err != nil {
		t.Fatal(err)
	}
	if w.Written() != int64(buf.Len()) {
		t.Fatalf("Written() = %d, buffer = %d", w.Written(), buf.Len())
	}
	return buf.Bytes()
}

func TestDocumentValidates(t *testing.T) {
	links := []Link{
		{Rect: [4]float64{10, 10, 100, 30}, URI: "https://example.com/a(b)\\c"},
		{Rect: [4]float64{0, 0, 1, 1}, URI: "MAILTO:a@b.c"},
	}
	doc := pdftest.Validate(t, buildDoc(t, "Report", links))
	if len(doc.Pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(doc.Pages))
	}
	if doc.Title != "Report" {
		t.Errorf("title = %q", doc.Title)
	}
	if doc.Pages[0].MediaBox != [4]float64{0, 0, 595.28, 841.89} {
		t.Errorf("mediabox = %v", doc.Pages[0].MediaBox)
	}
	if len(doc.Pages[0].Links) != 2 || doc.Pages[0].Links[0].URI != links[0].URI {
		t.Errorf("links = %+v", doc.Pages[0].Links)
	}
	if !bytes.Contains(doc.Pages[1].Content, []byte("/GT127 gs")) {
		t.Errorf("deferred content = %q", doc.Pages[1].Content)
	}
}

func TestDeterministic(t *testing.T) {
	a, b := buildDoc(t, "x", nil), buildDoc(t, "x", nil)
	if !bytes.Equal(a, b) {
		t.Fatal("two runs differ")
	}
	if bytes.Equal(a, buildDoc(t, "y", nil)) {
		t.Fatal("different documents share bytes")
	}
	if da, db := pdftest.Validate(t, a).ID, pdftest.Validate(t, buildDoc(t, "y", nil)).ID; da == db {
		t.Fatal("/ID does not depend on content")
	}
}

func TestTitleRoundTrip(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "Hello", "Hello"},
		{"empty", "", ""},
		{"specials", "a(b)c\\d", "a(b)c\\d"},
		{"controls stripped", "a(b)\\c\x00d\re\x7f\u0085f", "a(b)\\cdef"},
		{"accent", "é", "é"},
		{"astral", "😀 ok", "😀 ok"},
		{"mixed", "()\\\x00\ré😀", "()\\é😀"},
		{"bidi and zero width", "a\u202eb\u200bc\ufeffd", "abcd"},
		{"invalid utf8", "a\xffb", "a�b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := pdftest.Validate(t, buildDoc(t, tt.in, nil))
			if doc.Title != tt.want {
				t.Fatalf("title = %q, want %q", doc.Title, tt.want)
			}
		})
	}
}

func TestSanitizeTitleLimit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"just under", strings.Repeat("a", MaxTitleBytes-1), MaxTitleBytes - 1},
		{"at the limit", strings.Repeat("a", MaxTitleBytes), MaxTitleBytes},
		{"over", strings.Repeat("a", MaxTitleBytes+1), MaxTitleBytes},
		{"rune ends at the limit", strings.Repeat("a", MaxTitleBytes-2) + "é", MaxTitleBytes},
		{"rune straddles the limit", strings.Repeat("a", MaxTitleBytes-1) + "é", MaxTitleBytes - 1},
		{"astral ends at the limit", strings.Repeat("a", MaxTitleBytes-4) + "😀", MaxTitleBytes},
		{"astral straddles", strings.Repeat("a", MaxTitleBytes-3) + "😀", MaxTitleBytes - 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeTitle(tt.in); len(got) != tt.want {
				t.Fatalf("len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestAppendString(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "()"},
		{"abc", "(abc)"},
		{"a(b)\\", `(a\(b\)\\)`},
		{"é", "<FEFF00E9>"},
		{"\x00", "<FEFF0000>"},
		{"a\r", "<FEFF0061000D>"},
		{"😀", "<FEFFD83DDE00>"},
		{"\xff", "<FEFFFFFD>"},
		{"\x7f", "<FEFF007F>"},
	}
	for _, tt := range tests {
		if got := string(appendString(nil, tt.in)); got != tt.want {
			t.Errorf("appendString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestAppendNum(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{-0.00001, "0"},
		{1, "1"},
		{-1.5, "-1.5"},
		{0.12345, "0.1235"},
		{100, "100"},
		{MaxCoord, "1000000000"},
		{-MaxCoord, "-1000000000"},
		{MaxCoord - 1, "999999999"},
	}
	for _, tt := range tests {
		got, err := appendNum(nil, tt.in)
		if err != nil || string(got) != tt.want {
			t.Errorf("appendNum(%v) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), MaxCoord + 1, -MaxCoord - 1, 1e300} {
		if got, err := appendNum([]byte("x"), v); !errors.Is(err, ErrNumber) || string(got) != "x" {
			t.Errorf("appendNum(%v) = %q, %v; want ErrNumber and dst untouched", v, got, err)
		}
	}
}

func FuzzTitle(f *testing.F) {
	for _, s := range []string{"", "a", "()\\", "\x00\r", "é😀", "\xff\xfe", "\u202e"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		doc, err := pdftest.Parse(buildDoc(t, s, nil))
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Title) > MaxTitleBytes*3 {
			t.Fatalf("title too long: %d", len(doc.Title))
		}
	})
}

func TestMaxOutputBytes(t *testing.T) {
	run := func(limit int64) (*Writer, *bytes.Buffer, error) {
		var buf bytes.Buffer
		w := NewWriter(&buf, newBudget(limit))
		c := sampleContent()
		if _, err := w.AddPage(595.28, 841.89, c, false, nil); err != nil {
			return w, &buf, err
		}
		d, err := w.AddPage(300, 200, nil, true, nil)
		if err != nil {
			return w, &buf, err
		}
		if _, err = w.AddPage(300, 200, &Content{}, false, nil); err != nil {
			return w, &buf, err
		}
		if err = w.WriteDeferred(d, sampleContent()); err != nil {
			return w, &buf, err
		}
		return w, &buf, w.Close(Info{Title: "t"})
	}
	_, full, err := run(1 << 30)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(full.Len())
	if _, buf, err := run(size); err != nil || int64(buf.Len()) != size {
		t.Fatalf("at the limit: err=%v len=%d size=%d", err, buf.Len(), size)
	}
	w, buf, err := run(size - 1)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.OutputBytes {
		t.Fatalf("just over: err = %v, want OutputBytes LimitError", err)
	}
	if int64(buf.Len()) > size-1 || w.Written() != int64(buf.Len()) {
		t.Fatalf("wrote %d bytes past a limit of %d", buf.Len(), size-1)
	}
	if err := w.Close(Info{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("second Close = %v", err)
	}
}

type failWriter struct{ left int }

func (f *failWriter) Write(p []byte) (int, error) {
	if len(p) > f.left {
		n := f.left
		f.left = 0
		return n, errors.New("disk full")
	}
	f.left -= len(p)
	return len(p), nil
}

func TestUnderlyingWriteError(t *testing.T) {
	w := NewWriter(&failWriter{left: 100}, newBudget(1<<20))
	_, err := w.AddPage(100, 100, sampleContent(), false, nil)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if _, err2 := w.AddPage(100, 100, &Content{}, false, nil); err2 == nil {
		t.Fatal("writer must stay failed")
	}
}

func TestShortWrite(t *testing.T) {
	w := NewWriter(shortWriter{}, newBudget(1<<20))
	if _, err := w.AddPage(10, 10, &Content{}, false, nil); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("err = %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestPageSize(t *testing.T) {
	tests := []struct {
		name string
		w, h float64
		ok   bool
	}{
		{"max", MaxPageSize, MaxPageSize, true},
		{"just over width", MaxPageSize + 0.01, 100, false},
		{"just over height", 100, MaxPageSize + 0.01, false},
		{"tiny", 0.01, 0.01, true},
		{"zero", 0, 100, false},
		{"negative", 100, -1, false},
		{"nan", math.NaN(), 100, false},
		{"inf", 100, math.Inf(1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := newTestWriter(&buf)
			_, err := w.AddPage(tt.w, tt.h, &Content{}, false, nil)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if !tt.ok && (!errors.Is(err, ErrPageSize) || buf.Len() != 0) {
				t.Fatalf("err = %v, wrote %d bytes", err, buf.Len())
			}
		})
	}
}

func TestLinkValidation(t *testing.T) {
	rect := [4]float64{0, 0, 10, 10}
	long := func(n int) string { return "https://a.b/" + strings.Repeat("x", n-len("https://a.b/")) }
	tests := []struct {
		name string
		l    Link
		ok   bool
	}{
		{"https", Link{Rect: rect, URI: "https://example.com"}, true},
		{"http upper", Link{Rect: rect, URI: "HTTP://example.com"}, true},
		{"mailto", Link{Rect: rect, URI: "mailto:a@b.c"}, true},
		{"at the limit", Link{Rect: rect, URI: long(MaxURIBytes)}, true},
		{"just over", Link{Rect: rect, URI: long(MaxURIBytes + 1)}, false},
		{"javascript", Link{Rect: rect, URI: "javascript:alert(1)"}, false},
		{"leading control", Link{Rect: rect, URI: "\x01javascript:alert(1)"}, false},
		{"file", Link{Rect: rect, URI: "file:///etc/passwd"}, false},
		{"no scheme", Link{Rect: rect, URI: "https"}, false},
		{"empty", Link{Rect: rect, URI: ""}, false},
		{"non-ascii", Link{Rect: rect, URI: "https://é.com"}, false},
		{"space ok", Link{Rect: rect, URI: "https://a.b/ c"}, true},
		{"newline", Link{Rect: rect, URI: "https://a.b/\n"}, false},
		{"nan rect", Link{Rect: [4]float64{math.NaN(), 0, 1, 1}, URI: "https://a.b"}, false},
		{"huge rect", Link{Rect: [4]float64{0, 0, MaxCoord + 1, 1}, URI: "https://a.b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := newTestWriter(&buf)
			_, err := w.AddPage(100, 100, &Content{}, false, []Link{tt.l})
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if !tt.ok && (!errors.Is(err, ErrInvalidLink) || buf.Len() != 0) {
				t.Fatalf("err = %v, wrote %d bytes", err, buf.Len())
			}
			if tt.ok {
				if err := w.Close(Info{}); err != nil {
					t.Fatal(err)
				}
				doc := pdftest.Validate(t, buf.Bytes())
				if doc.Pages[0].Links[0].URI != tt.l.URI {
					t.Fatalf("uri = %q", doc.Pages[0].Links[0].URI)
				}
			}
		})
	}
}

func TestWriterErrors(t *testing.T) {
	t.Run("no pages", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		if err := w.Close(Info{}); !errors.Is(err, ErrNoPages) {
			t.Fatal(err)
		}
	})
	t.Run("pending deferred", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		if _, err := w.AddPage(10, 10, nil, true, nil); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(Info{}); !errors.Is(err, ErrPendingContent) {
			t.Fatal(err)
		}
	})
	t.Run("not deferred", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		p, err := w.AddPage(10, 10, &Content{}, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteDeferred(p, &Content{}); !errors.Is(err, ErrNotDeferred) {
			t.Fatal(err)
		}
	})
	t.Run("deferred twice", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		p, _ := w.AddPage(10, 10, nil, true, nil)
		if err := w.WriteDeferred(p, &Content{}); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteDeferred(p, &Content{}); !errors.Is(err, ErrNotDeferred) {
			t.Fatal(err)
		}
	})
	t.Run("bad content", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		bad := &Content{}
		bad.Save()
		p, _ := w.AddPage(10, 10, nil, true, nil)
		for _, c := range []*Content{nil, bad} {
			if _, err := w.AddPage(10, 10, c, false, nil); err == nil {
				t.Errorf("AddPage accepted %+v", c)
			}
			if err := w.WriteDeferred(p, c); err == nil {
				t.Errorf("WriteDeferred accepted %+v", c)
			}
		}
	})
	t.Run("after close", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		p, _ := w.AddPage(10, 10, nil, true, nil)
		_ = w.WriteDeferred(p, &Content{})
		if err := w.Close(Info{}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.AddPage(10, 10, &Content{}, false, nil); !errors.Is(err, ErrClosed) {
			t.Error(err)
		}
		if err := w.WriteDeferred(p, &Content{}); !errors.Is(err, ErrClosed) {
			t.Error(err)
		}
	})
}

func TestManyPages(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	for i := 0; i < 200; i++ {
		if _, err := w.AddPage(100, 100, sampleContent(), false, []Link{{Rect: [4]float64{0, 0, 1, 1}, URI: "https://a.b"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	if doc := pdftest.Validate(t, buf.Bytes()); len(doc.Pages) != 200 {
		t.Fatalf("pages = %d", len(doc.Pages))
	}
}

func BenchmarkAddPage(b *testing.B) {
	c := sampleContent()
	b.ReportAllocs()
	var buf bytes.Buffer
	w := NewWriter(&buf, newBudget(math.MaxInt64))
	for i := 0; i < b.N; i++ {
		if _, err := w.AddPage(595, 842, c, false, nil); err != nil {
			b.Fatal(err)
		}
		buf.Reset()
	}
}

func TestSanitizeTitleIsBounded(t *testing.T) {
	huge := strings.Repeat("é", 1<<22)
	if got := sanitizeTitle(huge); len(got) != MaxTitleBytes {
		t.Fatalf("len = %d", len(got))
	}
	allocs := testing.AllocsPerRun(5, func() { sanitizeTitle(huge) })
	if allocs > 2 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func TestDeferredPageKeepsItsBody(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	page, err := w.AddPage(300, 200, sampleContent(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteDeferred(page, sampleContent()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	one := pdftest.Validate(t, buildOne(t))
	if want := 2*len(one.Pages[0].Content) + 1; len(doc.Pages) != 1 || len(doc.Pages[0].Content) != want {
		t.Errorf("content %d bytes, want the body and the deferred stream joined (%d)", len(doc.Pages[0].Content), want)
	}
	if !bytes.Contains(buf.Bytes(), []byte("/Contents [")) {
		t.Error("/Contents is not an array of two streams")
	}
}

func buildOne(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	if _, err := w.AddPage(300, 200, sampleContent(), false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
