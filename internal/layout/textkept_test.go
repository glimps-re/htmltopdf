package layout

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"

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

// A decoy cell that takes the last of the 1000 columns does not hide the cells
// after it: their content goes to the end of the last kept cell, with a warning.
func TestTableColumnClampKeepsTheCellsAfterIt(t *testing.T) {
	cells := func(n int, f func(i int) string) string {
		var sb strings.Builder
		for i := range n {
			sb.WriteString(f(i))
		}
		return sb.String()
	}
	for name, html := range map[string]string{
		"decoy then text":                  `<table><tr><td colspan=1000>decoy</td><td>PHISHTEXT</td></tr></table>`,
		"1001 cells":                       `<table><tr>` + cells(1001, func(i int) string { return fmt.Sprintf("<td>c%d</td>", i) }) + `</tr></table>`,
		"decoy then 5 cells":               `<table><tr><td colspan=1000>decoy</td>` + cells(5, func(i int) string { return fmt.Sprintf("<td>PHISH%dTEXT</td>", i) }) + `</tr></table>`,
		"nested table":                     `<table><tr><td colspan=1000>decoy</td><td><table><tr><td>PHISHNEST</td></tr></table></td></tr></table>`,
		"huge colspan 99999":               `<table><tr><td colspan=99999>decoy</td><td>PHISHTEXT</td></tr></table>`,
		"after a normal cell":              `<table><tr><td>first</td><td colspan=1000>decoy</td><td>PHISHTEXT</td></tr></table>`,
		"abs box in a moved cell":          `<table><tr><td colspan=1000>decoy</td><td>x<div style="position:absolute;top:5px">PHISHTEXT</div></td></tr></table>`,
		"inline abs box in a moved cell":   `<table><tr><td colspan=1000>decoy</td><td>x<span style="position:absolute;top:5px">PHISHTEXT</span></td></tr></table>`,
		"cell with only an inline abs box": `<table><tr><td colspan=1000>decoy</td><td><span style="position:absolute;top:5px">PHISHTEXT</span></td></tr></table>`,
		"cell with only an abs box":        `<table><tr><td colspan=1000>decoy</td><td><div style="position:absolute;top:5px">PHISHTEXT</div></td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, html, opts{extra: "@page { size: 400px 300px; margin: 0 } body { margin: 0 }"})
			pages := r.all(t)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			got := textOf(pages)
			for _, w := range []string{"PHISHTEXT", "PHISH0TEXT", "PHISH4TEXT", "PHISHNEST", "c1000"} {
				if strings.Contains(html, w) && strings.Count(got, w) != 1 {
					t.Errorf("%s drawn %d times", w, strings.Count(got, w))
				}
			}
			ws, _ := r.b.Warnings()
			folded := slices.ContainsFunc(ws, func(w budget.Warning) bool {
				return w.Code == budget.WarnClamped && w.Detail == box.ClampedColumns
			})
			if !folded {
				t.Errorf("no %q warning: %v", box.ClampedColumns, ws)
			}
		})
	}
}

// A grid with more tracks than the clamp keeps every item: the tracks are cut
// at 1000 and the items go on in further rows.
func TestGridTrackClampKeepsEveryItem(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<div style="display:grid;grid-template-columns:repeat(1100,1fr)">`)
	for i := range 1500 {
		fmt.Fprintf(&sb, "<div>g%d</div>", i)
	}
	sb.WriteString("</div>")
	r := newRun(t, sb.String(), opts{extra: "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"})
	pages := r.all(t)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	got := textOf(pages)
	for _, w := range []string{"g0", "g999", "g1000", "g1499"} {
		if strings.Count(got, w) < 1 {
			t.Errorf("%s not drawn", w)
		}
	}
}

// A 1e7 px tall box in a nested flex container, laid out apart, is not cut as if
// it did not fit the page it is measured on: the text before it stays.
func TestTallBoxInANestedFlexKeepsText(t *testing.T) {
	html := `<div style="display:flex"><div>FIRST</div><div style="display:flex"><div style="height:1e7px">SECOND</div></div></div>`
	r := newRun(t, html, opts{extra: "@page { size: 300px 200px; margin: 10px }", limits: func(l *budget.Limits) { l[budget.Pages] = 100 }})
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if err != nil {
			break
		}
		pages = append(pages, pg)
	}
	got := textOf(pages)
	if !strings.Contains(got, "FIRST") || !strings.Contains(got, "SECOND") {
		t.Errorf("text %q", got)
	}
}

// The unbounded page that cells and items are laid out on is taller than any
// margin, height and padding the author can chain: text after them is kept.
func TestScratchPageHoldsTheTallestChain(t *testing.T) {
	html := `<table><tr><td style="padding:1e7px 0"><div style="margin-top:1e7px;height:1e7px">TALL</div>AFTER</td></tr></table>`
	r := newRun(t, html, opts{extra: "@page { size: 19200px 19200px; margin: 0 }", limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, pg)
	}
	if got := textOf(pages); !strings.Contains(got, "TALL") || !strings.Contains(got, "AFTER") {
		t.Errorf("text %q", got)
	}
}

// Nothing laid out apart is cut, whatever the lengths: every word of these boxes
// is drawn on a page, as often as the document has it, with line heights, heights
// and inline-block chains at the clamp.
func TestApartLayoutsKeepEveryWord(t *testing.T) {
	numbered := func(prefix string, n int) (text string, want map[string]int) {
		want = map[string]int{}
		var sb strings.Builder
		for i := range n {
			w := fmt.Sprintf("%s%d", prefix, i)
			sb.WriteString(w + " ")
			want[w]++
		}
		return sb.String(), want
	}
	type tc struct {
		html string
		want map[string]int
	}
	build := func(wrap func(text string) string, prefix string, n int) tc {
		text, want := numbered(prefix, n)
		return tc{wrap(text), want}
	}
	tall := `width:1px;line-height:1e7px`
	cases := map[string]tc{
		"td": build(func(s string) string { return `<table><tr><td style="` + tall + `">` + s + `</td></tr></table>` }, "t", 12),
		"td avoid": build(func(s string) string {
			return `<table><tr style="break-inside:avoid"><td style="` + tall + `">` + s + `</td></tr></table>`
		}, "v", 12),
		"flex item": build(func(s string) string {
			return `<div style="display:flex"><div style="` + tall + `">` + s + `</div></div>`
		}, "f", 12),
		"grid item": build(func(s string) string {
			return `<div style="display:grid"><div style="` + tall + `">` + s + `</div></div>`
		}, "g", 12),
		"inline-block": build(func(s string) string {
			return `<p><span style="display:inline-block;` + tall + `">` + s + `</span></p>`
		}, "i", 12),
		"abs box": build(func(s string) string {
			return `<div style="position:relative"><div style="position:absolute;` + tall + `">` + s + `</div></div>`
		}, "a", 12),
		"multicol td": build(func(s string) string {
			return `<table><tr><td style="columns:2;` + tall + `">` + s + `</td></tr></table>`
		}, "m", 12),
		"saturated multicol": build(func(s string) string {
			return `<table><tr><td><div style="columns:3;line-height:3e6px;width:30px">` + s + `</div></td></tr></table>`
		}, "s", 40),
		"fixed height td": build(func(s string) string {
			return `<table><tr><td><div style="height:40px;columns:2;width:60px">` + s + `</div></td></tr></table>`
		}, "h", 40),
		"fixed height flex": build(func(s string) string {
			return `<div style="display:flex"><div style="height:40px;columns:2;width:60px">` + s + `</div></div>`
		}, "k", 40),
		"fixed height block": build(func(s string) string {
			return `<p><span style="display:inline-block;height:40px;columns:2;width:60px">` + s + `</span></p>`
		}, "b", 40),
		"flex short lines":                    {`<div style="display:flex"><div><p style="width:1px;line-height:10000px">` + strings.Repeat("ww ", 1100) + `ENDTEXT</p></div></div>`, map[string]int{"ww": 1100, "ENDTEXT": 1}},
		"grid short lines":                    {`<div style="display:grid"><div><p style="width:1px;line-height:10000px">` + strings.Repeat("ww ", 1100) + `ENDTEXT</p></div></div>`, map[string]int{"ww": 1100, "ENDTEXT": 1}},
		"td short lines":                      {`<table><tr><td><p style="width:1px;line-height:10000px">` + strings.Repeat("ww ", 1100) + `ENDTEXT</p></td></tr></table>`, map[string]int{"ww": 1100, "ENDTEXT": 1}},
		"tall blocks columns":                 {`<table><tr><td><div style="columns:3">` + strings.Repeat(`<div style="height:1e7px">t</div>`, 9) + `ENDTEXT</div></td></tr></table>`, map[string]int{"t": 9, "ENDTEXT": 1}},
		"8 atoms":                             {`<p>` + strings.Repeat(`<span style="display:inline-block;height:1e7px">z</span> `, 8) + `END</p>`, map[string]int{"z": 8, "END": 1}},
		"atom chain":                          {`<table><tr><td><p>x <span style="display:inline-block;padding:1e7px 0;border:1e7px solid;height:1e7px;vertical-align:-1e7px">ATOM</span> ENDTEXT</p></td></tr></table>`, map[string]int{"ATOM": 1, "ENDTEXT": 1}},
		"nested bottoms":                      {`<table><tr><td><div style="padding-bottom:1e7px;border-bottom:1e7px solid"><div style="padding-bottom:1e7px;border-bottom:1e7px solid"><div style="padding-bottom:1e7px;border-bottom:1e7px solid">n0</div></div></div>n1 n2</td></tr></table>`, map[string]int{"n0": 1, "n1": 1, "n2": 1}},
		"tall lines after tall blocks, avoid": {`<table><tr><td>` + strings.Repeat(`<div style="height:1e7px">t</div>`, 3) + `<div style="break-inside:avoid"><p style="width:1px;line-height:1e7px">aa bb cc dd ee ff gg hh ii jj kk ENDTEXT</p></div></td></tr></table>`, map[string]int{"t": 3, "aa": 1, "kk": 1, "ENDTEXT": 1}},
		"tall lines after a padded block":     {`<table><tr><td><div style="padding-bottom:1e7px;border-bottom:1e7px solid"><div style="height:1e7px">t</div><div style="height:1e7px">u</div></div><p style="width:1px;line-height:1e7px">aa bb cc dd ee ff gg hh ii jj kk ENDTEXT</p></td></tr></table>`, map[string]int{"t": 1, "u": 1, "aa": 1, "kk": 1, "ENDTEXT": 1}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, c.html, opts{extra: "@page { size: 19200px 19200px; margin: 0 }", limits: func(l *budget.Limits) { l[budget.Pages] = 100000; l[budget.Work] = 1 << 31 }})
			var pages []*Page
			for {
				pg, err := r.pag.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				pages = append(pages, pg)
			}
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %.200s", lost)
			}
			got := map[string]int{}
			for _, pg := range pages {
				for _, w := range pageWords(pg) {
					got[w.text]++
				}
			}
			for w, n := range c.want {
				if got[w] < n {
					t.Errorf("word %q drawn %d times, want %d", w, got[w], n)
				}
			}
		})
	}
}

// overflowCase is a document whose words must all be drawn with their
// baseline inside a page area, on at most maxPages pages (0: no bound).
type overflowCase struct {
	name     string
	html     string
	words    []string
	maxPages int
}

func numberedLines(prefix string, n int) (string, []string) {
	var words []string
	for i := range n {
		words = append(words, fmt.Sprintf("%s%d", prefix, i))
	}
	return strings.Join(words, "<br>"), words
}

func checkOverflowCases(t *testing.T, cases []overflowCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRun(t, c.html, opts{extra: "@page { size: 300px 200px; margin: 10px }", limits: func(l *budget.Limits) { l[budget.Pages] = 500 }})
			pages := r.all(t)
			if c.maxPages > 0 && len(pages) > c.maxPages {
				t.Errorf("%d pages, want at most %d", len(pages), c.maxPages)
			}
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			inside := map[string]bool{}
			for _, pg := range pages {
				for _, it := range pg.Items {
					if tx, ok := it.(*display.Text); ok && tx.Baseline >= 0 && tx.Baseline <= pg.Area.H/pg.Scale {
						for _, w := range strings.Fields(tx.Text) {
							inside[w] = true
						}
					}
				}
			}
			for _, w := range c.words {
				if !inside[w] {
					t.Errorf("word %q has no baseline inside a page area", w)
				}
			}
		})
	}
}

// Text overflowing a fixed-height box inside a flex or grid item, a cell or an
// inline-block is paginated with its band, row or line, as in block flow: every
// word lands inside a page area.
func TestOverflowOfFixedHeightItemsLandsOnAPage(t *testing.T) {
	f, fw := numberedLines("f", 13)
	g, gw := numberedLines("g", 13)
	n, nw := numberedLines("n", 30)
	d, dw := numberedLines("d", 13)
	i, iw := numberedLines("i", 13)
	checkOverflowCases(t, []overflowCase{
		{"reduced from fuzz 1fe26645476306f3", `<style>p { margin: 3px }</style><v style='display:flex'><v>x<div>><div>x<div style='height:0;line-height:90px'>><div>LAST<v`, []string{"x", "LAST"}, 0},
		{"flex height 10px", `<div style="display:flex"><div><div style="height:10px">` + f + `</div></div></div><p>AFTER</p>`, append(fw, "AFTER"), 0},
		{"grid height 10px", `<div style="display:grid"><div style="height:10px">` + g + `</div></div>`, gw, 0},
		{"nested flex height 0", `<div style="display:flex"><div style="display:flex"><div><div style="height:0">` + n + `</div></div></div></div>`, nw, 0},
		{"td height 10px", `<table><tr><td><div style="height:10px">` + d + `</div></td></tr></table><p>AFTER</p>`, append(dw, "AFTER"), 0},
		{"inline-block height 10px", `<span style="display:inline-block"><div style="height:10px">` + i + `</div></span><p>AFTER</p>`, append(iw, "AFTER"), 0},
		{"fuzz c8f1412b86617286", `<A stYle='displAY:inline-BloCk'>0<B stYle='line-height:7'>0<div stYle=height:00>0<div></div>0</div>0`, []string{"0"}, 0},
	})
}

// Columns are balanced to the overflow the fit tests count: balanced to the
// boxes alone, they were too short for it and the band was sliced again on
// every page, up to the page limit.
func TestOverflowInColumnsDoesNotRunAway(t *testing.T) {
	two, twoW := numberedLines("a", 2)
	five, fiveW := numberedLines("b", 5)
	three, threeW := numberedLines("c", 3)
	zero, zeroW := numberedLines("z", 2)
	grid, gridW := numberedLines("g", 5)
	ib, ibW := numberedLines("i", 3)
	after := func(w []string) []string { return append(w, "AFTER") }
	checkOverflowCases(t, []overflowCase{
		{"flex 2 lines", `<div style="columns:2"><div style="display:flex"><div style="height:10px">` + two + `</div></div></div><p>AFTER</p>`, after(twoW), 2},
		{"flex 5 lines", `<div style="columns:2"><div style="display:flex"><div style="height:10px">` + five + `</div></div></div><p>AFTER</p>`, after(fiveW), 2},
		{"td 3 lines", `<div style="columns:2"><table><tr><td><div style="height:10px">` + three + `</div></td></tr></table></div><p>AFTER</p>`, after(threeW), 2},
		{"line-height 0", `<div style="columns:2"><div style="display:flex"><div style="line-height:0">` + zero + `</div></div></div><p>AFTER</p>`, after(zeroW), 2},
		{"grid 5 lines", `<div style="columns:2"><div style="display:grid"><div style="height:10px">` + grid + `</div></div></div><p>AFTER</p>`, after(gridW), 2},
		{"inline-block 3 lines", `<div style="columns:2"><span style="display:inline-block"><div style="height:10px">` + ib + `</div></span></div><p>AFTER</p>`, after(ibW), 2},
	})
}

// A negative margin at the end of the content of columns, however large, does
// not make their balanced height smaller than the content: every word is drawn
// on a page, within two pages.
func TestNegativeEndMarginInColumnsDoesNotRunAway(t *testing.T) {
	two, twoW := numberedLines("a", 2)
	six, sixW := numberedLines("s", 6)
	after := func(w ...string) []string { return append(slices.Clone(w), "AFTER") }
	cols := func(style, body string) string {
		return `<div style="columns:2;` + style + `">` + body + `</div><p>AFTER</p>`
	}
	checkOverflowCases(t, []overflowCase{
		{"p margin -19px", `<div style="columns:2"><p style="margin:0 0 -19px">w0</p></div>`, []string{"w0"}, 2},
		{"div margin -30px", `<div style="columns:2"><div style="margin-bottom:-30px">w0</div></div>`, []string{"w0"}, 2},
		{"margin -500px", cols("", `<p style="margin:0 0 -500px">w0</p>`), after("w0"), 2},
		{"margin -1e7px", cols("", `<p style="margin:0 0 -10000000px">w0</p>`), after("w0"), 2},
		{"two lines -1e7px", cols("", `<div style="margin-bottom:-10000000px">`+two+`</div>`), after(twoW...), 2},
		{"nested through the last child", cols("", `<div><div><p style="margin:0 0 -50px">w0</p></div></div>`), after("w0"), 2},
		{"three columns", cols("columns:3", `<p>w0</p><p style="margin:0 0 -100px">`+six+`</p>`), after(slices.Concat(sixW, []string{"w0"})...), 2},
		{"unbreakable block", cols("", `<div style="break-inside:avoid;margin-bottom:-30px">`+six+`</div>`), after(sixW...), 2},
		{"nested columns, unsupported: inner one is plain flow", cols("", `<div style="columns:2"><p style="margin:0 0 -40px">w0</p></div>`), after("w0"), 2},
		{"with a height", cols("height:40px", `<p style="margin:0 0 -40px">`+two+`</p>`), after(twoW...), 2},
		{"in a flex item", `<div style="display:flex"><div style="columns:2"><p style="margin:0 0 -40px">w0</p></div></div><p>AFTER</p>`, after("w0"), 2},
		{"in a cell", `<table><tr><td><div style="columns:2"><p style="margin:0 0 -40px">w0</p></div></td></tr></table><p>AFTER</p>`, after("w0"), 2},
	})
}

// flexWrapped is a document of groups × 25 lines, about 2.5 groups a page, in
// block flow or wrapped in an item of the given display.
func flexWrapped(display string, groups int) string {
	body := strings.Repeat("<div>"+strings.Repeat("x<br>", 25)+"</div>", groups)
	return `<div style="display:` + display + `"><div>` + body + `</div></div>`
}

func layOutWrapped(tb testing.TB, display string, groups int) (work int64, pages int) {
	r := newRun(tb, flexWrapped(display, groups), opts{
		extra:  "@page { size: 300px 200px; margin: 10px }",
		limits: func(l *budget.Limits) { l[budget.Pages] = 2000; l[budget.Work] = 1 << 31 },
	})
	pages = len(r.all(tb))
	return r.b.Used(budget.Work), pages
}

// A long document wrapped in a flex or grid item is cut from its band on every
// page, and what the cuts look at is charged to Work: each cut skips what ended
// above it and what starts below it, so the layout costs about what the same
// document costs in block flow (the item is measured, then laid out: factor 2),
// and doubling the document doubles the Work, not its size times its pages.
// It guards the cut index through the Work the cutter charges.
func TestFlexWrappedDocumentCostsLikeBlock(t *testing.T) {
	block, blockPages := layOutWrapped(t, "block", 600)
	for _, display := range []string{"flex", "grid"} {
		half, _ := layOutWrapped(t, display, 300)
		work, pages := layOutWrapped(t, display, 600)
		t.Logf("%s: work %d for %d pages (%d for half), block %d for %d pages", display, work, pages, half, block, blockPages)
		if pages < 1400 {
			t.Fatalf("%s: %d pages, want about 1500", display, pages)
		}
		if work > 2*block {
			t.Errorf("%s: work %d against %d in block flow", display, work, block)
		}
		if float64(work) > 2.2*float64(half) {
			t.Errorf("%s: work %d for 1500 pages, %d for 750: not linear", display, work, half)
		}
	}
}

func BenchmarkFlexWrappedDocument(b *testing.B) {
	for _, display := range []string{"block", "flex"} {
		b.Run(display, func(b *testing.B) {
			for b.Loop() {
				layOutWrapped(b, display, 600)
			}
		})
	}
}

// Glyphs below a line box (a font taller than the line-height) are not
// overflow the fit tests count: they neither grow balanced columns nor make the
// balancing search for a height no column reaches.
func TestGlyphOverflowDoesNotGrowColumns(t *testing.T) {
	lay := func(size string) (float64, int64) {
		html := `<div style="columns:2;line-height:10px;font-size:` + size + `">` + strings.Repeat("w<br>", 10) + `</div><p>AFTER</p>`
		r := newRun(t, html, opts{extra: "@page { size: 300px 200px; margin: 10px }"})
		pages := r.all(t)
		if lost := TextLost(r.root, pages); lost != "" {
			t.Errorf("font-size %s: text lost: %s", size, lost)
		}
		return pages[0].root.children[0].children[0].rect.H, r.b.Used(budget.Work)
	}
	h, work := lay("30px")
	twinH, twinWork := lay("16px")
	if h != twinH {
		t.Errorf("columns %vpx tall with 30px glyphs, %vpx with 16px", h, twinH)
	}
	if float64(work) > 1.5*float64(twinWork) {
		t.Errorf("work %d with 30px glyphs, %d with 16px", work, twinWork)
	}
}

// A small negative end margin in columns leaves what follows where margin 0
// puts it: the margin is not applied after the container either. Divs, not
// paragraphs: colPage zeroes the margins of p.
func TestNegativeEndMarginInColumnsKeepsWhatFollows(t *testing.T) {
	after := func(margin string) (page int, baseline float64) {
		html := `<div style="columns:2"><div style="margin-bottom:` + margin + `">w0</div></div><div>AFTER</div>`
		pages := newRun(t, html, opts{extra: colPage}).all(t)
		for i, pg := range pages {
			for _, tx := range texts(pg.Items) {
				if tx.Text == "AFTER" {
					return i, tx.Baseline
				}
			}
		}
		t.Fatalf("margin %s: AFTER not drawn", margin)
		return 0, 0
	}
	p0, b0 := after("0")
	p5, b5 := after("-5px")
	if p0 != p5 || math.Abs(b0-b5) > balanceEps {
		t.Errorf("AFTER on page %d at %v with margin -5px, page %d at %v with 0", p5, b5, p0, b0)
	}
}

// A negative end margin still cancels the margins that a column break truncated
// (Fragmentation 3 §5.2): a0-a2 fill the first column, b0-b2 the second, and the
// columns are as tall as three lines.
func TestNegativeEndMarginCancelsTruncatedMargins(t *testing.T) {
	html := `<div style="columns:2;column-rule:2px solid red"><p style="margin:0 0 60px">a0<br>a1<br>a2</p>` +
		`<p style="margin:0 0 -60px">b0<br>b1<br>b2</p></div><p style="margin:0">AFTER</p>`
	pages := newRun(t, html, opts{}).all(t)
	if len(pages) != 1 {
		t.Fatalf("%d pages", len(pages))
	}
	var rules []display.Rect
	for _, f := range fragsOf(pages, "div") {
		rules = append(rules, f.rules...)
	}
	if len(rules) != 1 || rules[0].H != 54 {
		t.Errorf("column rules %v, want one 54 px tall", rules)
	}
	found := false
	for _, tx := range texts(pages[0].Items) {
		if tx.Text == "AFTER" {
			found = true
			if tx.Baseline != 76.5 {
				t.Errorf("AFTER at %v, want 76.5", tx.Baseline)
			}
		}
	}
	if !found {
		t.Error("AFTER not drawn")
	}
}
