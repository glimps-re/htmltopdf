package htmltopdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/paint"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// Hostile suite of the M4 features through Render. Bounds are Work, Boxes or Glyphs counts against a twin
// where the public Result exposes none of them (page count, PDF bytes and the text itself stand in), or
// heap-max against a 2-run twin with an absolute cap, measured only where a case says so; time is only
// the one-minute guard. Every case checks that each input text is in the PDF extraction exactly once: a
// clamp may degrade the rendering, never hide text.

type h4Run struct {
	res  *Result
	data []byte
	doc  *pdftest.Doc
	peak uint64
	took time.Duration
	err  error
	// work, boxes and glyphs are the budget counts of the render (Work, Boxes, Glyphs).
	work, boxes, glyphs int64
}

// h4Render renders html with opts under the one-minute guard. The heap peak is measured, and held to the
// ceiling whatever the outcome, only when measure is set. A failed render is returned in err; a
// successful one is validated (own reader and pdfcpu).
func h4Render(t *testing.T, opts Options, html string, measure bool) h4Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), m3Guard)
	defer cancel()
	c, err := New(opts)
	if err != nil {
		return h4Run{err: err}
	}
	var r h4Run
	run := func() {
		var buf bytes.Buffer
		var b *budget.Budget
		r.res, b, r.err = c.render(ctx, strings.NewReader(html), &buf)
		r.data = buf.Bytes()
		if b != nil {
			r.work, r.boxes, r.glyphs = b.Used(budget.Work), b.Used(budget.Boxes), b.Used(budget.Glyphs)
		}
	}
	start := time.Now()
	if measure {
		r.peak = peakOf(run)
		if r.peak > m3HeapCeil {
			t.Errorf("peak heap %d MiB above %d MiB (error %v)", r.peak>>20, m3HeapCeil>>20, r.err)
		}
	} else {
		run()
	}
	r.took = time.Since(start)
	if r.err != nil {
		return r
	}
	r.doc = pdftest.Validate(t, r.data)
	return r
}

// h4RenderTwin renders twice and returns the run with the larger heap peak: one run varies by several MiB
// under load.
func h4RenderTwin(t *testing.T, opts Options, html string) h4Run {
	t.Helper()
	a, b := h4Render(t, opts, html, true), h4Render(t, opts, html, true)
	if b.peak > a.peak {
		return b
	}
	return a
}

// h4Markers returns n distinct marker words and the HTML paragraphs holding them, one marker each.
func h4Markers(prefix string, n int) (words []string, html string) {
	var sb strings.Builder
	for i := range n {
		w := fmt.Sprintf("%s%05d", prefix, i)
		words = append(words, w)
		fmt.Fprintf(&sb, "<p>%s</p>", w)
	}
	return words, sb.String()
}

// h4Tokens counts the words (runs of letters and digits) of the extracted text of r.
func h4Tokens(r h4Run) map[string]int {
	tokens := map[string]int{}
	for _, w := range strings.FieldsFunc(pagesText(r.doc), func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }) {
		tokens[w]++
	}
	return tokens
}

func h4TextOnce(t *testing.T, r h4Run, markers []string) {
	t.Helper()
	tokens := h4Tokens(r)
	for _, w := range markers {
		if n := tokens[w]; n != 1 {
			t.Errorf("text %q appears %d times in the PDF, want once", w, n)
			return
		}
	}
}

// h4MediaBoxes fails unless every page has the media box of a wPx x hPx paper.
func h4MediaBoxes(t *testing.T, r h4Run, wPx, hPx float64) {
	t.Helper()
	for i, p := range r.doc.Pages {
		if !near(p.MediaBox[2], wPx*paint.PxToPt) || !near(p.MediaBox[3], hPx*paint.PxToPt) {
			t.Errorf("page %d: media box %v, want %.2f x %.2f pt", i+1, p.MediaBox, wPx*paint.PxToPt, hPx*paint.PxToPt)
			return
		}
	}
}

// The harness itself sees every marker of a plain render exactly once.
func TestM4HarnessSeesEveryMarkerOnce(t *testing.T) {
	words, html := h4Markers("HARN", 300)
	r := h4Render(t, referenceOptions(t), html, false)
	if r.err != nil {
		t.Fatal(r.err)
	}
	h4TextOnce(t, r, words)
}

// h4Warnings lists the warnings of a render as "code: detail".
func h4Warnings(res *Result) []string {
	var out []string
	for _, w := range res.Warnings {
		out = append(out, fmt.Sprintf("%s: %s", w.Code, w.Detail))
	}
	return out
}
