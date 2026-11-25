package layout

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"

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
