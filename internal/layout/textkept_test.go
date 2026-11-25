package layout

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestNegativeMarginsKeepEveryText(t *testing.T) {
	for _, m := range []string{"0", "-50", "-250", "-1000", "-1e7", "1e7"} {
		t.Run(m, func(t *testing.T) {
			html := `<div style="height:300px;break-inside:avoid">FLOWBLOCK</div>TEXTA<div style="margin-top:` + m + `px"><div>TEXTB</div></div><div style="height:50px">TAIL</div>`
			r := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 }"})
			pages := r.all(t)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			for _, w := range []string{"FLOWBLOCK", "TEXTA", "TEXTB", "TAIL"} {
				n := 0
				for _, pg := range pages {
					n += countText(pg, w)
				}
				if n != 1 {
					t.Errorf("%s drawn %d times", w, n)
				}
			}
		})
	}
}

// Where Chrome keeps a negative margin on the page, so do we.
func TestNegativeMarginWithinThePage(t *testing.T) {
	html := `<p style="margin:0">A</p><div style="margin-top:-8px"><div>B</div></div>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 }"})
	pages := r.all(t)
	ys := map[string]float64{}
	for _, w := range pageWords(pages[0]) {
		ys[w.text] = w.y / 0.75
	}
	if d := ys["B"] - ys["A"]; d < 8 || d > 12 {
		t.Errorf("B is %v px under A, want about 10 (18 - 8)", d)
	}
}

// 10 000 siblings with margins of ±1e7 px: no text is lost, the work is linear
// and the render ends.
func TestHugeMarginsBetweenSiblings(t *testing.T) {
	start := time.Now()
	build := func(n int) string {
		var sb strings.Builder
		for i := range n {
			m := "1e7"
			if i%2 == 1 {
				m = "-1e7"
			}
			fmt.Fprintf(&sb, `<div style="margin:%spx 0">w%d</div>`, m, i)
		}
		return sb.String()
	}
	work := func(n int) int64 {
		r := newRun(t, build(n), opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 }", limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
		var pages []*Page
		for {
			pg, err := r.pag.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("%d siblings: %v", n, err)
			}
			pages = append(pages, pg)
		}
		if lost := TextLost(r.root, pages); lost != "" {
			t.Errorf("%d siblings: text lost: %s", n, lost)
		}
		return r.b.Used(budget.Work)
	}
	w1, w2 := work(5000), work(10000)
	if float64(w2) > 2.2*float64(w1) {
		t.Errorf("work %d for 10000 siblings, %d for 5000: not linear", w2, w1)
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

// No text of the report corpus (templates 00-03, small and large) is lost.
func TestCorpusKeepsEveryText(t *testing.T) {
	if testing.Short() {
		t.Skip("renders the large reports")
	}
	for _, tpl := range []string{"00-host-connector", "01-modern", "02-executive", "03-analyst"} {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(testcorpus.FixtureName(tpl, size), func(t *testing.T) {
				html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), tpl, size)
				if err != nil {
					t.Fatal(err)
				}
				r := newRun(t, string(html), opts{limits: func(l *budget.Limits) { l[budget.Pages] = 2000; l[budget.Work] = 1 << 31 }})
				if lost := TextLost(r.root, r.all(t)); lost != "" {
					t.Errorf("text lost: %s", lost)
				}
			})
		}
	}
}

// Only a font size of exactly 0 hides text: the smallest positive size is drawn.
func TestTinyTextIsStillDrawn(t *testing.T) {
	for _, size := range []string{"0.001px", "1e-9px"} {
		r := newRun(t, `<p style="font-size:`+size+`">TINY</p>`, opts{extra: "@page { size: 400px 100px; margin: 0 }"})
		pages := r.all(t)
		if got := textOf(pages); !strings.Contains(strings.ToUpper(got), "TINY") {
			t.Errorf("%s: text %q", size, got)
		}
		if lost := TextLost(r.root, pages); lost != "" {
			t.Errorf("%s: text lost: %s", size, lost)
		}
	}
}

// dropText removes the text runs holding s from the fragment trees of the pages,
// as a layout bug that loses a box would.
func dropText(pages []*Page, s string) {
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			for _, ln := range f.lines {
				items := ln.items[:0]
				for _, it := range ln.items {
					if it.text == nil || !strings.Contains(it.text.Text, s) {
						items = append(items, it)
					}
				}
				ln.items = items
			}
		})
	}
}

// The copies of a repeated table header do not stand for a body cell that no
// page draws, and the check works box by box.
func TestTextLostIsPerBoxNotPerLetter(t *testing.T) {
	var rows strings.Builder
	for i := range 60 {
		fmt.Fprintf(&rows, "<tr><td>row%02d</td><td>cell</td></tr>", i)
	}
	html := `<table><thead><tr><th>row</th><th>cell</th></tr></thead><tbody>` + rows.String() + `</tbody></table>`
	r := newRun(t, html, opts{extra: "@page { size: 300px 100px; margin: 0 } body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages: the header does not repeat", len(pages))
	}
	if lost := TextLost(r.root, pages); lost != "" {
		t.Fatalf("whole render: %s", lost)
	}
	dropText(pages, "row41")
	lost := TextLost(r.root, pages)
	if !strings.Contains(lost, "row41") || strings.Contains(lost, "row40") {
		t.Errorf("a lost body cell is reported as %q", lost)
	}
}

func TestTextLostChecksTextUnderOverflowHidden(t *testing.T) {
	css := "@page { size: 400px 200px; margin: 0 } body { margin: 0 }"
	r := newRun(t, `<div style="overflow:hidden;height:40px"><p>INSIDE</p><p>second</p><p>third</p><p>fourth</p></div>`, opts{extra: css})
	pages := r.all(t)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Fatalf("whole render: %s", lost)
	}
	dropText(pages, "INSIDE")
	if lost := TextLost(r.root, pages); !strings.Contains(lost, "INSIDE") {
		t.Errorf("a clipped box's first text lost: %q", lost)
	}
	// What the clip drops by design, and a clip without area, are not losses.
	r = newRun(t, `<div style="overflow:hidden;height:0"><p>nothing</p></div>`, opts{extra: css})
	pages = r.all(t)
	dropText(pages, "nothing")
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("a clip of height 0: %q", lost)
	}
}

// Negative margins in cells, flex and grid items and inline-blocks, which are
// laid out apart and then placed, keep their text on the page too.
func TestNegativeMarginsInApartLayoutsKeepText(t *testing.T) {
	for name, html := range map[string]string{
		"td":                  `<p>A</p><table><tr><td><div style="margin-top:-286px">W1</div><div style="margin-top:-100px">W2</div></td></tr></table>`,
		"flex item":           `<div style="display:flex"><div style="margin-top:-300px">W1</div><div style="margin-bottom:-500px;height:500px">W2</div></div>`,
		"flex end":            `<div style="display:flex;align-items:flex-end"><div style="margin-bottom:-400px">W1</div><div style="height:30px">W2</div></div>`,
		"flex center":         `<div style="display:flex;align-items:center"><div style="margin-bottom:-400px">W1</div><div>W2</div></div>`,
		"flex auto margin":    `<div style="display:flex;height:50px"><div style="margin-top:-300px;margin-bottom:auto">W1</div><div>W2</div></div>`,
		"grid item":           `<div style="display:grid;grid-template-columns:1fr 1fr"><div style="margin-top:-300px">W1</div><div style="margin-bottom:-400px">W2</div></div>`,
		"inline-block":        `<p>x <span style="display:inline-block"><span style="display:block;margin-top:-300px">W1</span></span> W2</p>`,
		"nested relative":     `<div style="position:relative;left:1px"><div style="margin-top:-286px">W1</div></div><p>W2</p>`,
		"after a page break":  `<div style="height:300px;break-inside:avoid">F</div>W1<div style="margin-top:-250px"><div>W2</div></div>`,
		"negative bottom":     `<div style="margin-bottom:-1e7px">W1</div><div>W2</div>`,
		"collapse with empty": `<div style="margin-top:-50px"></div><div style="margin-top:-200px">W1</div>W2`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 }"})
			pages := r.all(t)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
		})
	}
}

// A relative ancestor does not hide the loss of a box that is not offset: the
// check runs on the position before any relative offset.
func TestTextLostUnderARelativeAncestor(t *testing.T) {
	r := newRun(t, `<div style="position:relative;left:1px"><p>KEPT</p><p>VANISH</p></div>`, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 }"})
	pages := r.all(t)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Fatal(lost)
	}
	dropText(pages, "VANISH")
	if lost := TextLost(r.root, pages); !strings.Contains(lost, "VANISH") {
		t.Errorf("lost text under a relative ancestor not reported: %q", lost)
	}
}

// Author lengths that place text outside the page clip it, as Chrome does: the
// text is still emitted (an emission check, not a visibility one), nothing
// panics and the work stays bounded.
func TestAuthorLengthsOutsideThePageStillEmitText(t *testing.T) {
	for name, html := range map[string]string{
		"line-height":      `<p style="line-height:1e6px;margin:0">LINE</p>`,
		"vertical-align":   `<p style="margin:0">a <span style="vertical-align:-1e6px">VALIGN</span></p>`,
		"vertical-align +": `<p style="margin:0">a <span style="vertical-align:1e6px">VALIGNUP</span></p>`,
		"text-indent":      `<p style="text-indent:-1e6px;margin:0">INDENT</p>`,
		"margin-left":      `<p style="margin:0 0 0 -1e6px">MARGIN</p>`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 }"})
			pages := r.all(t)
			got := strings.ToUpper(textOf(pages))
			for _, w := range []string{"LINE", "VALIGN", "INDENT", "MARGIN"} {
				if strings.Contains(html, w) && !strings.Contains(got, w) {
					t.Errorf("%s: %q not emitted (got %q)", name, w, got)
				}
			}
			if w := r.b.Used(budget.Work); w > 100_000 {
				t.Errorf("work %d", w)
			}
		})
	}
}
