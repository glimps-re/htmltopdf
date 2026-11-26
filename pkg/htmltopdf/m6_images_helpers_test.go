package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// m6Guard stops a render that hangs; the targets are asserted separately.
const m6Guard = time.Minute

// m6Run is one render of a document with the layout kept.
type m6Run struct {
	res   *Result
	data  []byte
	err   error
	doc   *pdftest.Doc
	took  time.Duration
	peak  uint64
	tree  *box.Box
	pages []*layout.Page
}

// m6Render renders html with opts and keeps the box tree and the pages. A
// successful render is validated by pdfcpu and the own reader.
func m6Render(tb testing.TB, opts Options, html string) m6Run {
	tb.Helper()
	c := mustNew(tb, opts)
	var r m6Run
	hooked := *c
	hooked.pageHook = func(root *box.Box, pg *layout.Page) { r.tree = root; r.pages = append(r.pages, pg) }
	ctx, cancel := context.WithTimeout(context.Background(), m6Guard)
	defer cancel()
	var buf bytes.Buffer
	r.peak = peakOf(func() {
		start := time.Now()
		r.res, r.err = hooked.Render(ctx, strings.NewReader(html), &buf)
		r.took = time.Since(start)
	})
	r.data = buf.Bytes()
	if r.res == nil {
		tb.Fatal("nil Result")
	}
	if r.err == nil {
		r.doc = pdftest.Validate(tb, r.data)
	}
	return r
}

// m6Hostile holds a render of a hostile document to the contract of the
// library: no internal error, success or a LimitError and nothing else, no text
// lost, every glyph reaching a page area, a valid PDF and a consistent Result.
// It returns the LimitError, if any.
func m6Hostile(tb testing.TB, r m6Run) *LimitError {
	tb.Helper()
	return m6HostileGlyphs(tb, r, "")
}

// m6HostileGlyphs is m6Hostile for a document whose glyphs are known to reach
// outside the page area (known names why). The glyphs are then logged, and the
// call fails when none is outside: the entry is stale and must go. TextLost and
// the Result checks still run.
func m6HostileGlyphs(tb testing.TB, r m6Run, known string) *LimitError {
	tb.Helper()
	var le *LimitError
	switch {
	case r.err == nil:
	case errors.As(r.err, &le):
	default:
		tb.Fatalf("render failed with %T %v: only a LimitError is acceptable", r.err, r.err)
	}
	if r.err != nil {
		return le
	}
	if lost := layout.TextLost(r.tree, r.pages); lost != "" {
		tb.Errorf("text lost: %s", lost)
	}
	out := glyphsOutsidePages(r.pages)
	switch {
	case out != "" && known != "":
		tb.Logf("known limit (%s), glyphs outside every page area: %s", known, out)
	case out == "" && known != "":
		tb.Errorf("the known limit %q no longer holds: remove its entry", known)
	case out != "":
		tb.Errorf("glyphs outside every page area: %s", out)
	}
	if r.res.Pages < 1 || r.res.Pages != len(r.pages) || r.res.Pages != len(r.doc.Pages) || r.res.Bytes != int64(len(r.data)) {
		tb.Errorf("Result %+v, %d laid out pages, %d in the PDF, %d bytes", *r.res, len(r.pages), len(r.doc.Pages), len(r.data))
	}
	return nil
}

// glyphsOutsidePages lists, capped, the text runs that reach no page area: the
// run starts right of the page area or ends left of it, or its glyphs are
// all above or below it. TextLost does not look at it for text placed by an
// offset or a line box.
func glyphsOutsidePages(pages []*layout.Page) string {
	const slack = 1.0
	var out []string
	for _, pg := range pages {
		w, h := pg.Area.W/pg.Scale, pg.Area.H/pg.Scale
		for _, it := range pg.Items {
			t, ok := it.(*display.Text)
			if !ok || len(t.Glyphs) == 0 {
				continue
			}
			x1 := t.X + t.Width()
			if t.Baseline+0.3*t.Size < -slack || t.Baseline-t.Size > h+slack || t.X > w+slack || x1 < -slack {
				if len(out) < 5 {
					out = append(out, fmt.Sprintf("page %d %.40q at x %.0f..%.0f baseline %.0f (area %.0f×%.0f)", pg.Number, t.Text, t.X, x1, t.Baseline, w, h))
				}
			}
		}
	}
	return strings.Join(out, "; ")
}

// m6Rejects returns the image-rejected warnings of a Result by reason, and
// fails on any other warning.
func m6Rejects(tb testing.TB, res *Result) map[string]int {
	tb.Helper()
	got := map[string]int{}
	for _, w := range res.Warnings {
		if w.Code != WarnImageRejected {
			tb.Errorf("unexpected warning %+v", w)
			continue
		}
		got[w.Detail] += w.Count
	}
	return got
}

var imDo = regexp.MustCompile(`/Im\d+ Do`)

// m6Do counts the image draws of all pages.
func m6Do(doc *pdftest.Doc) int {
	n := 0
	for _, p := range doc.Pages {
		n += len(imDo.FindAll(p.Content, -1))
	}
	return n
}

// m6AllRejects is m6Rejects for documents whose text may warn of other things
// (missing glyphs for the bidi markers, unsupported properties).
func m6AllRejects(res *Result) map[string]int {
	got := map[string]int{}
	for _, w := range res.Warnings {
		if w.Code == WarnImageRejected {
			got[w.Detail] += w.Count
		}
	}
	return got
}
