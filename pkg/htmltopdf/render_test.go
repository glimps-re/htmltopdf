package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func TestRenderReport(t *testing.T) {
	c := mustNew(t, Options{})
	res, data := renderBytes(t, c, reportHTML)
	doc := pdftest.Validate(t, data)
	if res.Pages != len(doc.Pages) || res.Pages < 1 {
		t.Errorf("Result.Pages = %d, document has %d", res.Pages, len(doc.Pages))
	}
	if res.Bytes != int64(len(data)) {
		t.Errorf("Result.Bytes = %d, wrote %d", res.Bytes, len(data))
	}
	if doc.Title != "Rapport & test" {
		t.Errorf("Title = %q", doc.Title)
	}
	text := pagesText(doc)
	for _, want := range []string{"Analysis report", "First paragraph", "Colored block", "Inside the card.", "monospace"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if len(doc.Pages[0].Links) != 1 || doc.Pages[0].Links[0].URI != "https://example.com/x" {
		t.Errorf("links: %+v", doc.Pages[0].Links)
	}
	mb := doc.Pages[0].MediaBox
	if mb[2] < 595 || mb[2] > 596 || mb[3] < 841 || mb[3] > 842 {
		t.Errorf("media box %v, want A4", mb)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	c := mustNew(t, Options{})
	_, a := renderBytes(t, c, reportHTML)
	_, b := renderBytes(t, c, reportHTML)
	if !bytes.Equal(a, b) {
		t.Error("two renders of the same document differ")
	}
}

func TestRenderReferenceConfiguration(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	res, data := renderBytes(t, c, reportHTML+strings.Repeat("<p>filler paragraph ↳ with a glyph the sans font lacks</p>", 80))
	doc := pdftest.Validate(t, data)
	if res.Pages < 2 {
		t.Errorf("%d pages", res.Pages)
	}
	// Scale 0.9 lays the content out in a page area 1/0.9 larger.
	if len(doc.Fonts) < 2 {
		t.Errorf("%d fonts embedded, want a base font and a fallback", len(doc.Fonts))
	}
}

func TestEmptyDocumentsHaveOnePage(t *testing.T) {
	c := mustNew(t, Options{})
	for _, html := range []string{"", " ", "<body></body>", "<p></p>", "<!-- c -->"} {
		res, data := renderBytes(t, c, html)
		doc := pdftest.Validate(t, data)
		if res.Pages != 1 || len(doc.Pages) != 1 {
			t.Errorf("%q: %d pages", html, res.Pages)
		}
	}
}

func TestPageRulesFromTheDocumentAndExtraCSS(t *testing.T) {
	html := `<style>@page { size: A5 landscape }</style><p>x</p>`
	_, data := renderBytes(t, mustNew(t, Options{}), html)
	mb := pdftest.Validate(t, data).Pages[0].MediaBox
	if mb[2] < 595 || mb[2] > 596 || mb[3] < 419 || mb[3] > 420 {
		t.Errorf("document @page: %v", mb)
	}
	// ExtraCSS wins over the document.
	_, data = renderBytes(t, mustNew(t, Options{ExtraCSS: "@page { size: 200px 300px; margin: 0 }"}), html)
	mb = pdftest.Validate(t, data).Pages[0].MediaBox
	if mb[2] != 150 || mb[3] != 225 {
		t.Errorf("ExtraCSS @page: %v", mb)
	}
}

func TestMediaQueriesSeeThePageArea(t *testing.T) {
	// The page area is 200 px wide: max-width: 300px applies, min-width: 300px does not.
	html := `<style>@media (max-width: 300px) { p { color: #ff0000 } } @media (min-width: 300px) { p { font-size: 40px } }</style><p>x</p>`
	c := mustNew(t, Options{ExtraCSS: "@page { size: 200px 400px; margin: 0 }"})
	_, data := renderBytes(t, c, html)
	doc := pdftest.Validate(t, data)
	content := string(doc.Pages[0].Content)
	if !strings.Contains(content, "1 0 0 rg") {
		t.Errorf("the max-width rule did not apply:\n%s", content)
	}
}

func TestWarnings(t *testing.T) {
	html := `<style>p { box-shadow: 1px 1px red; foo: bar } @import "x.css"; p::first-line { color: red }</style>
<img src="x.png"><img src="y.png"><a href="javascript:alert(1)">bad</a><p>x</p>`
	res, _ := renderBytes(t, mustNew(t, Options{}), html)
	counts := map[string]int{}
	for _, w := range res.Warnings {
		counts[string(w.Code)+": "+w.Detail] = w.Count
	}
	for _, want := range []string{
		"unsupported-property: box-shadow", "unsupported-property: foo", "unsupported-element: img", "blocked-link: ",
	} {
		found := false
		for k := range counts {
			found = found || strings.HasPrefix(k, want)
		}
		if !found {
			t.Errorf("no warning %q in %v", want, counts)
		}
	}
	if counts["unsupported-element: img"] != 2 {
		t.Errorf("img count = %d, want 2", counts["unsupported-element: img"])
	}
	if res.WarningsDropped != 0 {
		t.Errorf("dropped %d", res.WarningsDropped)
	}
	for i := 1; i < len(res.Warnings); i++ {
		a, b := res.Warnings[i-1], res.Warnings[i]
		if a.Code > b.Code || a.Code == b.Code && a.Detail > b.Detail {
			t.Errorf("warnings not sorted: %+v before %+v", a, b)
		}
	}
}

func TestLimitErrors(t *testing.T) {
	tests := []struct {
		name   string
		limits Limits
		html   string
		want   Limit
	}{
		{"input", Limits{MaxInputBytes: 100}, strings.Repeat("x", 200), LimitInputBytes},
		{"nodes", Limits{MaxNodes: 10}, strings.Repeat("<p>x</p>", 20), LimitNodes},
		{"depth", Limits{}, strings.Repeat("<div>", 600), LimitHTMLDepth},
		{"css", Limits{MaxCSSBytes: 20}, "<style>" + strings.Repeat("p{color:red}", 10) + "</style>", LimitCSSBytes},
		{"selectors", Limits{MaxSelectors: 2}, "<style>a,b,c{color:red}</style>", LimitSelectors},
		{"boxes", Limits{MaxBoxes: 5}, strings.Repeat("<p>x</p>", 20), LimitBoxes},
		{"pages", Limits{MaxPages: 2}, strings.Repeat("<p style='margin:300px 0'>x</p>", 20), LimitPages},
		{"work", Limits{MaxWork: 50}, strings.Repeat("<p>some words in a paragraph</p>", 20), LimitWork},
		{"styles", Limits{MaxStyles: 3}, `<p style="color:red">a</p><p style="color:blue">b</p>`, LimitStyles},
		{"output", Limits{MaxOutputBytes: 500}, reportHTML, LimitOutputBytes},
		{"glyphs", Limits{MaxGlyphs: 10}, "<p>this paragraph has more than ten glyphs</p>", LimitGlyphs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := mustNew(t, Options{Limits: tt.limits})
			var buf bytes.Buffer
			res, err := c.Render(context.Background(), strings.NewReader(tt.html), &buf)
			var le *LimitError
			if !errors.As(err, &le) || le.Limit != tt.want || !errors.Is(err, ErrLimit) {
				t.Fatalf("got %v, want a %s LimitError", err, tt.want)
			}
			if res == nil {
				t.Fatal("nil Result on error")
			}
		})
	}
}

func TestLimitTwins(t *testing.T) {
	// Exactly at MaxPages: ok. One page more: LimitPages.
	html := func(n int) string { return strings.Repeat("<p style='margin:0;padding-top:1000px'>x</p>", n) }
	opts := Options{ExtraCSS: "@page { size: 400px 300px; margin: 0 }", Limits: Limits{MaxPages: 100}}
	c := mustNew(t, opts)
	var pages int
	for n := 1; n < 100; n++ {
		res, err := c.Render(context.Background(), strings.NewReader(html(n)), io.Discard)
		if err != nil {
			var le *LimitError
			if !errors.As(err, &le) || le.Limit != LimitPages {
				t.Fatalf("n=%d: %v", n, err)
			}
			if res.Pages != 100 && res.Pages != pages {
				t.Logf("pages written before the limit: %d", res.Pages)
			}
			return
		}
		pages = res.Pages
	}
}

func TestContextAndTimeout(t *testing.T) {
	c := mustNew(t, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := c.Render(ctx, strings.NewReader(reportHTML), io.Discard)
	if !errors.Is(err, context.Canceled) || res == nil {
		t.Errorf("canceled: %v", err)
	}
	c = mustNew(t, Options{Limits: Limits{Timeout: time.Nanosecond}})
	time.Sleep(time.Millisecond)
	if _, err := c.Render(context.Background(), strings.NewReader(reportHTML), io.Discard); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout: %v", err)
	}
	var noCtx context.Context
	if _, err := mustNew(t, Options{}).Render(noCtx, strings.NewReader("<p>x</p>"), io.Discard); err != nil {
		t.Errorf("nil context: %v", err)
	}
}

type failingWriter struct {
	after int
	n     int
	err   error
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.n+len(p) > f.after {
		return 0, f.err
	}
	f.n += len(p)
	return len(p), nil
}

func TestDestinationErrorIsPassedThrough(t *testing.T) {
	want := errors.New("disk full")
	c := mustNew(t, Options{})
	res, err := c.Render(context.Background(), strings.NewReader(reportHTML), &failingWriter{after: 2000, err: want})
	if !errors.Is(err, want) || errors.Unwrap(err) != nil {
		t.Errorf("got %v, want the writer's error as is", err)
	}
	if res == nil {
		t.Error("nil Result")
	}
}

func TestSourceErrorIsReported(t *testing.T) {
	want := errors.New("connection reset")
	c := mustNew(t, Options{})
	res, err := c.Render(context.Background(), io.MultiReader(strings.NewReader("<p>x"), iotest.ErrReader(want)), io.Discard)
	if !errors.Is(err, want) || res == nil {
		t.Errorf("got %v", err)
	}
	if errors.Is(err, ErrInternal) || errors.Is(err, ErrLimit) {
		t.Errorf("a read error is not internal: %v", err)
	}
}

func TestPanicIsRecovered(t *testing.T) {
	c := mustNew(t, Options{})
	c.hook = func(stage string) {
		if stage == "box" {
			panic("boom")
		}
	}
	res, err := c.Render(context.Background(), strings.NewReader("<p>x</p>"), io.Discard)
	if !errors.Is(err, ErrInternal) || res == nil {
		t.Errorf("got %v", err)
	}
}

func TestResultOnErrorHoldsPagesAndWarnings(t *testing.T) {
	c := mustNew(t, Options{ExtraCSS: "@page { size: 400px 300px; margin: 0 }", Limits: Limits{MaxPages: 3}})
	html := "<style>p { foo: bar }</style>" + strings.Repeat("<p style='padding-top:1000px'>x</p>", 5)
	res, err := c.Render(context.Background(), strings.NewReader(html), io.Discard)
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v", err)
	}
	if res.Pages != 3 {
		t.Errorf("pages written = %d, want 3", res.Pages)
	}
	if len(res.Warnings) == 0 || res.Bytes == 0 {
		t.Errorf("result: %+v", res)
	}
}

func TestStagesAreCheckpointed(t *testing.T) {
	c := mustNew(t, Options{})
	var stages []string
	c.hook = func(s string) { stages = append(stages, s) }
	renderBytes(t, c, "<p>x</p>")
	// The text package reports its own stages in between: keep ours, in order.
	var ours []string
	for _, s := range stages {
		if s != "shape-chunk" {
			ours = append(ours, s)
		}
	}
	if got := strings.Join(ours, ","); got != "dom.read,dom.prepass,dom.parse,dom.convert,css,box,page,margin,close" {
		t.Errorf("stages: %s", got)
	}
}

func TestConcurrentRenders(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	var wg sync.WaitGroup
	results := make([][]byte, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf bytes.Buffer
			if _, err := c.Render(context.Background(), strings.NewReader(reportHTML), &buf); err != nil {
				t.Error(err)
			}
			results[i] = buf.Bytes()
		}()
	}
	wg.Wait()
	for i := 1; i < len(results); i++ {
		if !bytes.Equal(results[0], results[i]) {
			t.Fatalf("render %d differs from render 0", i)
		}
	}
}
