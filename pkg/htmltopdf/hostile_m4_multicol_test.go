package htmltopdf

import (
	"fmt"
	"strings"
	"testing"
)

// h4WordCount requires each word to appear n times in the PDF text.
func h4WordCount(t *testing.T, r h4Run, n int, words ...string) {
	t.Helper()
	tokens := h4Tokens(r)
	for _, w := range words {
		if tokens[w] != n {
			t.Errorf("%q appears %d times, want %d", w, tokens[w], n)
		}
	}
}

// h4Within1Percent reports whether a is within 1 % of b, or one glyph.
func h4Within1Percent(a, b int64) bool {
	diff := max(a-b, b-a)
	return diff <= 1 || diff*100 <= b
}

// 100 nested multicol containers: work and glyphs stay linear in the depth (twice the depth costs at most
// 2.5 times), the boxes are those of the same nesting without columns and so are the glyphs (within 1 %:
// only the outer container balances, with its trial glyphs rewound), the text once.
func TestM4MulticolNesting(t *testing.T) {
	nest := func(depth int, style string) string {
		return strings.Repeat(`<div style="`+style+`">`, depth) + "NESTEDTEXT one two three" + strings.Repeat("</div>", depth)
	}
	run := func(html string) h4Run {
		r := h4Render(t, referenceOptions(t), html, true)
		if r.err != nil {
			t.Fatal(r.err)
		}
		h4TextOnce(t, r, []string{"NESTEDTEXT", "one", "two", "three"})
		return r
	}
	deep, half := run(nest(100, "columns:2")), run(nest(50, "columns:2"))
	plain := run(nest(100, "margin:0"))
	if deep.work > 5*half.work/2 || deep.glyphs > 5*half.glyphs/2 {
		t.Errorf("depth 100: work %d glyphs %d; depth 50: work %d glyphs %d: not linear", deep.work, deep.glyphs, half.work, half.glyphs)
	}
	if deep.work > 3*plain.work || deep.boxes != plain.boxes || !h4Within1Percent(deep.glyphs, plain.glyphs) {
		t.Errorf("work %d boxes %d glyphs %d; without columns work %d boxes %d glyphs %d", deep.work, deep.boxes, deep.glyphs, plain.work, plain.boxes, plain.glyphs)
	}
}

// columns:2 around hundreds of pages of table rows (a row is a column high): the pages come out (496 at the
// full size, just under MaxPages), the text once, and the work and boxes stay within about twice those of
// a one-column table of half the rows.
func TestM4MulticolManyPagesOfRows(t *testing.T) {
	rows := h4Size(1100, 330) // about 2.2 rows per page
	cells, words := h4Items("ROW", rows, `<tr><td style="height:900px">%s</td></tr>`)
	halfCells := strings.Join(strings.SplitAfter(cells, "</tr>")[:rows/2], "")
	r := h4Render(t, referenceOptions(t), `<div style="columns:2"><table>`+cells+"</table></div>", true)
	twin := h4Render(t, referenceOptions(t), `<div><table>`+halfCells+"</table></div>", false)
	if r.err != nil || twin.err != nil {
		t.Fatal(r.err, twin.err)
	}
	h4TextOnce(t, r, words)
	if r.res.Pages < rows*4/10 || r.res.Pages > DefaultLimits().MaxPages || r.res.Pages != twin.res.Pages {
		t.Errorf("%d pages with %d rows in two columns, %d for half the rows in one", r.res.Pages, rows, twin.res.Pages)
	}
	if r.work > 5*twin.work/2 || r.boxes > 2*twin.boxes+twin.boxes/20 {
		t.Errorf("work %d boxes %d; twin of half the rows: work %d boxes %d", r.work, r.boxes, twin.work, twin.boxes)
	}
}

// Containers with a solid column-rule against the same containers without it (columns:2 in both): boxes,
// glyphs and work are those of the twin, the rule adds only its own paint, the text once.
func TestM4MulticolRulesCostNothingMore(t *testing.T) {
	n := h4Size(4000, 1500)
	build := func(rule string) (string, []string) {
		items, words := h4Items("CR", n, `<div class=c>%s alpha beta</div>`)
		return fmt.Sprintf(`<style>.c{columns:2;%s}</style>`, rule) + items, words
	}
	html, words := build("column-rule:2px solid #c00")
	twinHTML, _ := build("")
	r := h4Render(t, referenceOptions(t), html, true)
	twin := h4Render(t, referenceOptions(t), twinHTML, false)
	if r.err != nil || twin.err != nil {
		t.Fatal(r.err, twin.err)
	}
	h4TextOnce(t, r, words)
	h4WordCount(t, r, n, "alpha", "beta")
	if r.boxes != twin.boxes || r.glyphs != twin.glyphs || r.work > twin.work+twin.work/8+int64(n*8) || r.res.Bytes > 2*twin.res.Bytes+1<<20 {
		t.Errorf("work %d boxes %d glyphs %d bytes %d; twin work %d boxes %d glyphs %d bytes %d", r.work, r.boxes, r.glyphs, r.res.Bytes, twin.work, twin.boxes, twin.glyphs, twin.res.Bytes)
	}
}

// 10 000 small multicol containers: each is laid out several times to balance its columns, and the glyphs
// of those trial layouts are rewound: the glyphs charged equal those of the same containers without
// columns (within 1 %), the work stays within 21 times theirs (the balancing passes), and the text is
// present once.
func TestM4MulticolTenThousandContainers(t *testing.T) {
	n := h4Size(10000, 3000)
	build := func(css string) (string, []string) {
		items, words := h4Items("TC", n, `<div class=c>%s alpha beta</div>`)
		return `<style>.c{` + css + `}</style>` + items, words
	}
	html, words := build("columns:2;column-rule:2px solid #c00")
	plainHTML, _ := build("margin:0")
	r := h4Render(t, referenceOptions(t), html, true)
	twin := h4Render(t, referenceOptions(t), plainHTML, false)
	if r.err != nil || twin.err != nil {
		t.Fatal(r.err, twin.err)
	}
	h4TextOnce(t, r, words)
	h4WordCount(t, r, n, "alpha", "beta")
	if !h4Within1Percent(r.glyphs, twin.glyphs) || r.work > 21*twin.work {
		t.Errorf("glyphs %d work %d; without columns glyphs %d work %d", r.glyphs, r.work, twin.glyphs, twin.work)
	}
}

// Absurd column geometry around a long flow: invalid values are dropped and huge ones clamped, so each row
// renders with the text in the PDF once and the heap capped; a 1e9 px height ends on the pages limit.
func TestM4MulticolAbsurdGeometry(t *testing.T) {
	words, flow := h4Markers("MC", 200)
	for _, tc := range []struct {
		style     string
		wantLimit Limit // "" when the row renders
	}{
		{"columns:1000000000", ""},
		{"columns:2;column-gap:1e9px", ""},
		{"column-width:1e9px", ""},
		{"column-width:0.0001px", ""},
		{"columns:2;column-gap:-100px", ""},
		{"columns:0 0px", ""},
		{"columns:2;column-rule:1e9px solid red", ""},
		{"columns:2;height:1e9px", LimitPages}, // 1e7 px of height after the clamp: thousands of pages
		{"columns:99999;width:1px", ""},
	} {
		t.Run(tc.style, func(t *testing.T) {
			r := h4Render(t, referenceOptions(t), `<div style="`+tc.style+`">`+flow+`</div>`, true)
			if tc.wantLimit != "" {
				if !h4IsLimit(r.err, tc.wantLimit) {
					t.Fatalf("%v, want a LimitError on %s", r.err, tc.wantLimit)
				}
				return
			}
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4TextOnce(t, r, words)
		})
	}
}

// A table cell of L inline lines (x<br> each) cut across many columns: every cut walks the row, so the
// render must either end on the work limit or have been charged for those walks, at least one walk of
// the whole row per cut, with at most 70 lines in a column. (L is bounded by MaxNodes at about 49 000;
// without that charge the render costs seconds that Work never sees.)
func TestM4MulticolLongCellCutIsCharged(t *testing.T) {
	const maxLinesPerColumn = 70 // a column of 1000 px holds at most 1000 / 16 px lines
	// Each line is two nodes (the text and the br): about as many as MaxNodes allows.
	lines := h4Size(DefaultLimits().MaxNodes/2-1000, 20000)
	for _, columns := range []string{"columns:1000"} {
		t.Run(columns, func(t *testing.T) {
			html := `<div style="` + columns + `"><table><tr><td>` + strings.Repeat("x<br>", lines) + `</td></tr></table></div>`
			r := h4Render(t, referenceOptions(t), html, true)
			if r.err != nil {
				if !h4IsLimit(r.err, LimitWork) {
					t.Fatalf("%v, want the output or a LimitError on MaxWork", r.err)
				}
				return
			}
			cuts := lines / maxLinesPerColumn
			if floor := int64(cuts) * int64(lines); r.work < floor {
				t.Errorf("work %d for about %d cuts of a %d line row, want at least %d", r.work, cuts, lines, floor)
			}
		})
	}
}

// A wrapping flex container of N full-width items, each on its own line, in 50 columns: the cost of the
// columns is a fixed planning allowance (measured 1.4 M) plus the one-column twin's at the small size, and
// at most 1.75 times the twin's at the large one (measured 1.58, against 6 to 30 times with the quadratic
// re-planning); the text is in the PDF once.
func TestM4MulticolFlexWrapIsLinear(t *testing.T) {
	const planning = 2 << 20
	run := func(columns string, n int) h4Run {
		items, words := h4Items("FW", n, `<div style="width:100%%">%s</div>`)
		r := h4Render(t, referenceOptions(t), `<div style="`+columns+`"><div style="display:flex;flex-wrap:wrap">`+items+"</div></div>", true)
		if r.err != nil {
			t.Fatalf("%s, %d items: %v", columns, n, r.err)
		}
		h4TextOnce(t, r, words)
		return r
	}
	for i, n := range []int{h4Size(5000, 2000), h4Size(20000, 8000)} {
		r, twin := run("columns:50", n), run("margin:0", n)
		t.Logf("%d items: work %d pages %d; one column work %d", n, r.work, r.res.Pages, twin.work)
		bound := twin.work + planning
		if i == 1 {
			bound = 7 * twin.work / 4
		}
		if r.work > bound {
			t.Errorf("%d items: work %d in columns, one column %d, bound %d", n, r.work, twin.work, bound)
		}
	}
}

// The column count is clamped: 1000, 1001 and 1e9 columns give the same pages, boxes and glyphs, and the text
// is present once.
func TestM4MulticolColumnCountIsClamped(t *testing.T) {
	words, flow := h4Markers("CC", 200)
	var base h4Run
	for i, count := range []string{"1000", "1001", "1000000000"} {
		r := h4Render(t, referenceOptions(t), `<div style="columns:`+count+`">`+flow+`</div>`, true)
		if r.err != nil {
			t.Fatal(r.err)
		}
		h4TextOnce(t, r, words)
		if i == 0 {
			base = r
		} else if r.res.Pages != base.res.Pages || r.boxes != base.boxes || r.glyphs != base.glyphs {
			t.Errorf("columns:%s: pages %d boxes %d glyphs %d; columns:1000: pages %d boxes %d glyphs %d", count, r.res.Pages, r.boxes, r.glyphs, base.res.Pages, base.boxes, base.glyphs)
		}
	}
}

// Balancing spreads the content over the columns: 200 two-column containers of thirty lines each take at most
// 0.6 times the pages of the same containers in one column, so nothing stays all in the first column.
func TestM4MulticolBalancingSpreadsTheContent(t *testing.T) {
	const lines = 30
	para := strings.Repeat("<p>one line of text</p>", lines)
	render := func(style string) h4Run {
		r := h4Render(t, referenceOptions(t), strings.Repeat(`<div style="`+style+`">`+para+`</div>`, 200), true)
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r
	}
	balanced, plain := render("columns:2"), render("margin:0")
	if float64(balanced.res.Pages) > 0.6*float64(plain.res.Pages) {
		t.Errorf("%d pages in two columns, %d in one: the columns are not balanced", balanced.res.Pages, plain.res.Pages)
	}
}
