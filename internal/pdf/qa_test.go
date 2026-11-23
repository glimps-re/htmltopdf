package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func qaBudget(maxOut int64) *budget.Budget {
	l := budget.Defaults()
	l[budget.OutputBytes] = maxOut
	return budget.New(context.Background(), l, nil)
}

func qaContent(opacity float64) *pdf.Content {
	c := &pdf.Content{}
	c.Save()
	c.FillColor(pdf.Color{R: 0.2, G: 0.4, B: 0.6, A: opacity})
	c.RoundedRect(1, 2, 300, 200, [4]float64{3, 4, 5, 6})
	c.Fill()
	c.Restore()
	return c
}

func qaDoc(tb testing.TB, title string, pages int, reverseDeferred bool) []byte {
	tb.Helper()
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, qaBudget(1<<30))
	var refs []pdf.Ref
	for i := range pages {
		r, err := w.AddPage(200+float64(i), 300, nil, true, []pdf.Link{{Rect: [4]float64{0, 0, 5, 5}, URI: fmt.Sprintf("https://a.b/%d", i)}})
		if err != nil {
			tb.Fatal(err)
		}
		refs = append(refs, r)
	}
	for i := range refs {
		j := i
		if reverseDeferred {
			j = len(refs) - 1 - i
		}
		if err := w.WriteDeferred(refs[j], qaContent(float64(j%5)/4)); err != nil {
			tb.Fatal(err)
		}
	}
	if err := w.Close(pdf.Info{Title: title}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func TestQADeferredOrderDoesNotBreakStructure(t *testing.T) {
	for _, rev := range []bool{false, true} {
		doc := pdftest.Validate(t, qaDoc(t, "t", 7, rev))
		if len(doc.Pages) != 7 {
			t.Fatalf("pages = %d", len(doc.Pages))
		}
		for i, p := range doc.Pages {
			if p.MediaBox[2] != 200+float64(i) || len(p.Links) != 1 || !strings.HasSuffix(p.Links[0].URI, fmt.Sprintf("/%d", i)) {
				t.Errorf("rev=%v page %d: %+v", rev, i, p)
			}
			if len(p.Content) == 0 {
				t.Errorf("rev=%v page %d: empty content", rev, i)
			}
		}
	}
}

func TestQADeterministicAcrossGoroutines(t *testing.T) {
	want := qaDoc(t, "é😀()\\", 5, true)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				if got := qaDoc(t, "é😀()\\", 5, true); !bytes.Equal(got, want) {
					t.Error("concurrent writers produced different bytes")
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestQATitleEdges(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"only controls gives no title", "\x00\x01\r\n\xe2\x80\xae\xe2\x80\x8b", ""},
		{"trailing backslash", "a\\", "a\\"},
		{"unmatched parens", "((( )", "((( )"},
		{"hex-looking", "<FEFF0041>", "<FEFF0041>"},
		{"name-like", "/Type /Catalog", "/Type /Catalog"},
		{"stream injection", ") >> endobj 99 0 obj << /A (", ") >> endobj 99 0 obj << /A ("},
		{"all four astral bytes", "\U0010FFFF", "\U0010FFFF"},
		{"noncharacter", "a￾b", "a￾b"},
		{"lone surrogate bytes", "a\xed\xa0\x80b", "a���b"},
		{"tab is a control", "a\tb", "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := pdftest.Validate(t, qaDoc(t, tt.in, 1, false))
			if doc.Title != tt.want {
				t.Fatalf("title = %q, want %q", doc.Title, tt.want)
			}
		})
	}
}

func TestQAHugeTitleIsBounded(t *testing.T) {
	data := qaDoc(t, strings.Repeat("é😀", 1<<19), 1, false)
	if len(data) > 8<<10 {
		t.Fatalf("a 3 MiB title produced a %d byte file", len(data))
	}
	doc := pdftest.Validate(t, data)
	if n := len(doc.Title); n > pdf.MaxTitleBytes || !strings.HasPrefix(doc.Title, "é😀") {
		t.Fatalf("title is %d bytes", n)
	}
}

func TestQAEveryOpacityLevel(t *testing.T) {
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, qaBudget(1<<30))
	c := &pdf.Content{}
	for i := 0; i <= 255; i++ {
		c.FillColor(pdf.Color{A: float64(i) / 255})
		c.Rect(0, 0, 1, 1)
		c.Fill()
	}
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(pdf.Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if got := strings.Count(string(doc.Pages[0].Content), " gs\n"); got != 256 {
		t.Errorf("%d gs operators, want 256 (one per level change, alpha 1 included after alpha 254)", got)
	}
}

func TestQAContentLimits(t *testing.T) {
	c := &pdf.Content{}
	for range pdf.MaxSaveDepth {
		c.Save()
	}
	if c.Err() != nil || c.Depth() != pdf.MaxSaveDepth {
		t.Fatalf("depth at the limit: err=%v depth=%d", c.Err(), c.Depth())
	}
	c.Save()
	if !errors.Is(c.Err(), pdf.ErrSaveDepth) {
		t.Fatalf("over the limit: %v", c.Err())
	}

	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), pdf.MaxCoord + 1, -pdf.MaxCoord - 1} {
		c := &pdf.Content{}
		c.Rect(v, 0, 1, 1)
		if !errors.Is(c.Err(), pdf.ErrNumber) {
			t.Errorf("Rect(%v): %v", v, c.Err())
		}
		var buf bytes.Buffer
		w := pdf.NewWriter(&buf, qaBudget(1<<20))
		if _, err := w.AddPage(10, 10, c, false, nil); err == nil || buf.Len() != 0 {
			t.Errorf("a content with an error must be refused without output: %v, %d bytes", err, buf.Len())
		}
	}
	for _, v := range []float64{pdf.MaxCoord, -pdf.MaxCoord, 1e-12, -1e-12, math.SmallestNonzeroFloat64, math.Copysign(0, -1)} {
		c := &pdf.Content{}
		c.Rect(v, v, 1, 1)
		if c.Err() != nil {
			t.Errorf("Rect(%v): %v", v, c.Err())
		}
		fields := strings.Fields(string(c.Bytes()))
		for _, f := range fields[:len(fields)-1] {
			if _, err := strconv.ParseFloat(f, 64); err != nil || strings.ContainsAny(f, "eE") || f == "-0" {
				t.Errorf("Rect(%v) wrote operand %q", v, f)
			}
		}
	}
	c = &pdf.Content{}
	c.LineWidth(math.NaN())
	if c.Err() == nil {
		t.Error("NaN line width accepted")
	}
	c = &pdf.Content{}
	c.FillColor(pdf.Color{R: math.Inf(1), G: math.Inf(-1), B: 2, A: math.Inf(1)})
	if c.Err() != nil || strings.ContainsAny(string(c.Bytes()), "eEIN") {
		t.Errorf("infinite color components must clamp: %v %q", c.Err(), c.Bytes())
	}
	c = &pdf.Content{}
	c.RoundedRect(0, 0, math.NaN(), 5, [4]float64{math.NaN(), -1, math.Inf(1), 1e30})
	if c.Err() == nil {
		t.Error("NaN width accepted")
	}
	c = &pdf.Content{}
	c.RoundedRect(0, 0, 10, 10, [4]float64{math.NaN(), -1, math.Inf(1), 1e30})
	if c.Err() != nil || strings.ContainsAny(string(c.Bytes()), "eEIN") {
		t.Errorf("hostile radii must clamp: %v", c.Err())
	}
}

func TestQAOutputLimitSweep(t *testing.T) {
	var full bytes.Buffer
	{
		w := pdf.NewWriter(&full, qaBudget(1<<30))
		build := func(w *pdf.Writer) error {
			if _, err := w.AddPage(100, 100, qaContent(0.5), false, []pdf.Link{{Rect: [4]float64{0, 0, 1, 1}, URI: "https://a.b"}}); err != nil {
				return err
			}
			r, err := w.AddPage(100, 100, nil, true, nil)
			if err != nil {
				return err
			}
			if err := w.WriteDeferred(r, qaContent(0.25)); err != nil {
				return err
			}
			return w.Close(pdf.Info{Title: "é"})
		}
		if err := build(w); err != nil {
			t.Fatal(err)
		}
		size := int64(full.Len())
		for limit := int64(0); limit <= size; limit++ {
			var buf bytes.Buffer
			b := qaBudget(limit)
			err := build(pdf.NewWriter(&buf, b))
			if limit == size {
				if err != nil || !bytes.Equal(buf.Bytes(), full.Bytes()) {
					t.Fatalf("at the limit (%d): err=%v", limit, err)
				}
				continue
			}
			var le *budget.LimitError
			if !errors.As(err, &le) || le.Kind != budget.OutputBytes || le.Max != limit {
				t.Fatalf("limit %d: err = %v", limit, err)
			}
			if int64(buf.Len()) > limit {
				t.Fatalf("limit %d: %d bytes written", limit, buf.Len())
			}
			if _, perr := pdftest.Parse(buf.Bytes()); perr == nil {
				t.Fatalf("limit %d: a truncated PDF parses", limit)
			}
		}
	}
}

func TestQABudgetAccountsEveryByte(t *testing.T) {
	var buf bytes.Buffer
	b := qaBudget(1 << 30)
	w := pdf.NewWriter(&buf, b)
	if _, err := w.AddPage(10, 10, qaContent(1), false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(pdf.Info{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if b.Used(budget.OutputBytes) != int64(buf.Len()) || w.Written() != int64(buf.Len()) {
		t.Fatalf("budget %d, Written %d, buffer %d", b.Used(budget.OutputBytes), w.Written(), buf.Len())
	}
}

func TestQAParserRejectsEveryTruncation(t *testing.T) {
	data := qaDoc(t, "t", 2, false)
	for n := range len(data) - 1 {
		if _, err := pdftest.Parse(data[:n]); err == nil {
			t.Fatalf("prefix of %d/%d bytes parses", n, len(data))
		}
	}
}

func TestQAParserSurvivesByteCorruption(t *testing.T) {
	data := qaDoc(t, "t", 2, false)
	rejected := 0
	for i := range data {
		for _, b := range []byte{0x00, 0xff, '9', ' '} {
			if data[i] == b {
				continue
			}
			mut := bytes.Clone(data)
			mut[i] = b
			if _, err := pdftest.Parse(mut); err != nil {
				rejected++
			}
		}
	}
	if rejected < len(data) {
		t.Errorf("only %d corruptions rejected over %d bytes: the parser is too lenient", rejected, len(data))
	}
}

func TestQAPDFCPURejectsBrokenFiles(t *testing.T) {
	data := qaDoc(t, "t", 1, false)
	if err := pdftest.ValidatePDFCPU(data); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string][]byte{
		"no header": bytes.Replace(data, []byte("%PDF-1.7"), []byte("%XXX-1.7"), 1),
		"bad root":  bytes.Replace(data, []byte("/Root 4 0 R"), []byte("/Root 9 0 R"), 1),
		"truncated": data[:len(data)/2],
	} {
		if bytes.Equal(mut, data) {
			continue
		}
		if err := pdftest.ValidatePDFCPU(mut); err == nil {
			t.Errorf("%s: pdfcpu strict accepted a broken file", name)
		}
	}
}
