package layout

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

const colPage = "@page { size: 800px 600px; margin: 0 } body { margin: 0 } p { margin: 0 }"

func mcPara(prefix string, words int) string {
	var sb strings.Builder
	for i := range words {
		fmt.Fprintf(&sb, "%s%03d ", prefix, i)
	}
	return sb.String()
}

func clampedColumns(r *run) bool {
	ws, _ := r.b.Warnings()
	for _, w := range ws {
		if w.Code == budget.WarnClamped && w.Detail == "columns" {
			return true
		}
	}
	return false
}

func TestColumnGeometry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		st      style.Computed
		w       float64
		n       int
		cw, gap float64
		clamped bool
	}{
		{"two with the gap normal", style.Computed{ColumnCount: 2, ColumnGapNormal: true, FontSize: 16}, 600, 2, 292, 16, false},
		{"gap in px", style.Computed{ColumnCount: 3, ColumnGap: style.Dim{Kind: style.DimPx, Value: 30}, FontSize: 16}, 600, 3, 180, 30, false},
		{"gap in percent", style.Computed{ColumnCount: 2, ColumnGap: style.Dim{Kind: style.DimPercent, Value: 10}, FontSize: 16}, 600, 2, 270, 60, false},
		{"negative gap is 0", style.Computed{ColumnCount: 2, ColumnGap: style.Dim{Kind: style.DimPx, Value: -5}, FontSize: 16}, 600, 2, 300, 0, false},
		{"the gap leaves no room for two", style.Computed{ColumnCount: 2, ColumnGap: style.Dim{Kind: style.DimPx, Value: 1000}, FontSize: 16}, 600, 1, 600, 1000, true},
		{"1000 columns are clamped to 1 px", style.Computed{ColumnCount: 1000, ColumnGap: style.Dim{Kind: style.DimPx, Value: 0}, FontSize: 16}, 600, 600, 1, 0, true},
		{"clamped with a gap", style.Computed{ColumnCount: 1000, ColumnGapNormal: true, FontSize: 16}, 600, 36, 600.0/36 - 35*16/36.0, 16, true},
		{"no width", style.Computed{ColumnCount: 5, ColumnGapNormal: true, FontSize: 16}, 0, 1, 0, 16, true},
		{"one column", style.Computed{ColumnCount: 0, FontSize: 16}, 600, 1, 600, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, clamped := columnGeometry(&tc.st, tc.w)
			if g.n != tc.n || math.Abs(g.w-tc.cw) > 1e-9 || math.Abs(g.gap-tc.gap) > 1e-9 || clamped != tc.clamped {
				t.Errorf("got %+v clamped %v, want n=%d w=%v gap=%v clamped %v", g, clamped, tc.n, tc.cw, tc.gap, tc.clamped)
			}
			if g.w < 0 || g.n > 1 && g.w < minColumnWidth-1e-9 {
				t.Errorf("column width %v", g.w)
			}
		})
	}
}

// The columns of a box are as wide as the count and the gap say, and the
// content flows over them in order.
func TestMulticolFlowsOverColumns(t *testing.T) {
	html := `<div id="c" style="columns:3;column-gap:20px;width:600px"><p>` + mcPara("a", 300) + `</p></div>`
	pages := newRun(t, html, opts{extra: colPage}).all(t)
	if len(pages) != 1 {
		t.Fatalf("%d pages", len(pages))
	}
	xs := map[float64]int{}
	for _, ln := range linesOf(pages[0]) {
		xs[math.Round(ln.rect.X*100)/100]++
		if math.Abs(ln.rect.W-186.666667) > 0.01 {
			t.Errorf("line %v wide", ln.rect.W)
		}
	}
	if len(xs) != 3 || xs[0] == 0 || xs[206.67] == 0 || xs[413.33] == 0 {
		t.Errorf("column x of the lines: %v", xs)
	}
}

// Content that ends in the columns is balanced: the three columns end at the
// same line, within one.
func TestMulticolBalances(t *testing.T) {
	html := `<div id="c" style="columns:3;column-gap:20px"><p>` + mcPara("a", 250) + `</p></div>`
	pages := newRun(t, html, opts{extra: colPage}).all(t)
	perCol := map[float64]int{}
	for _, ln := range linesOf(pages[0]) {
		perCol[ln.rect.X]++
	}
	if len(perCol) != 3 {
		t.Fatalf("columns: %v", perCol)
	}
	lo, hi := math.MaxInt, 0
	for _, n := range perCol {
		lo, hi = min(lo, n), max(hi, n)
	}
	if hi-lo > 1 {
		t.Errorf("unbalanced columns: %v", perCol)
	}
	if c := fragByID(t, pages, "c"); c.rect.H >= 600 || c.rect.H < float64(hi)*18-1 {
		t.Errorf("container %v tall for columns of %d lines", c.rect.H, hi)
	}
}

// A segment that continues on the next page is not balanced: the columns of
// the page are full and the container runs to the bottom of the page.
func TestMulticolFillsPagesBeforeTheLast(t *testing.T) {
	html := `<div id="c" style="columns:2;column-gap:20px"><p>` + mcPara("a", 4000) + `</p></div>`
	pages := newRun(t, html, opts{extra: colPage}).all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	for i, pg := range pages[:len(pages)-1] {
		var top, bottom float64
		for _, c := range frags([]*Page{pg}, "div") {
			top, bottom = c.rect.Y, c.rect.Y+c.rect.H
		}
		if top != 0 || bottom != 600 {
			t.Errorf("page %d: container from %v to %v", i+1, top, bottom)
		}
	}
	last := pages[len(pages)-1]
	var h float64
	for _, c := range frags([]*Page{last}, "div") {
		h = c.rect.H
	}
	if h >= 600 {
		t.Errorf("the last segment fills the page: %v", h)
	}
	// Every word once and in order.
	var got []string
	for _, pg := range pages {
		cols := map[float64][]string{}
		var order []float64
		for _, ln := range linesOf(pg) {
			if _, ok := cols[ln.rect.X]; !ok {
				order = append(order, ln.rect.X)
			}
			for _, it := range ln.items {
				if it.text != nil {
					cols[ln.rect.X] = append(cols[ln.rect.X], strings.Fields(it.text.Text)...)
				}
			}
		}
		for _, x := range order {
			got = append(got, cols[x]...)
		}
	}
	if len(got) != 4000 || got[0] != "a000" || got[3999] != "a3999" {
		t.Errorf("%d words, first %q last %q", len(got), got[0], got[len(got)-1])
	}
}

func TestMulticolRules(t *testing.T) {
	rules := func(pg *Page) []display.FillRect {
		var out []display.FillRect
		for _, it := range pg.Items {
			if fr, ok := it.(display.FillRect); ok && fr.Color.R == 1 && fr.Color.G == 0 {
				out = append(out, fr)
			}
		}
		return out
	}
	css := `.c { columns: 3; column-gap: 20px; column-rule: 2px solid #f00; width: 600px }`
	t.Run("between the columns that have content", func(t *testing.T) {
		pages := newRun(t, `<div class="c"><p>`+mcPara("a", 300)+`</p></div>`, opts{extra: colPage, css: css}).all(t)
		got := rules(pages[0])
		if len(got) != 2 {
			t.Fatalf("%d rules", len(got))
		}
		for i, fr := range got {
			wantX := float64(i+1)*186.666667 + float64(i)*20 + 9
			if math.Abs(fr.Rect.X-wantX) > 0.01 || fr.Rect.W != 2 || fr.Rect.Y != 0 || fr.Rect.H < 10 {
				t.Errorf("rule %d: %+v, want x %v", i, fr.Rect, wantX)
			}
		}
	})
	t.Run("not next to an empty column", func(t *testing.T) {
		pages := newRun(t, `<div class="c"><p>a b c</p></div>`, opts{extra: colPage, css: css}).all(t)
		if got := rules(pages[0]); len(got) != 0 {
			t.Errorf("%d rules for one column of content", len(got))
		}
	})
	t.Run("none without a style", func(t *testing.T) {
		pages := newRun(t, `<div class="c" style="column-rule-style:none"><p>`+mcPara("a", 300)+`</p></div>`, opts{extra: colPage, css: css}).all(t)
		if got := rules(pages[0]); len(got) != 0 {
			t.Errorf("%d rules", len(got))
		}
	})
	t.Run("one per page, as long as the columns", func(t *testing.T) {
		pages := newRun(t, `<div class="c"><p>`+mcPara("a", 3000)+`</p></div>`, opts{extra: colPage, css: css}).all(t)
		for i, pg := range pages[:len(pages)-1] {
			if got := rules(pg); len(got) != 2 || got[0].Rect.H != 600 {
				t.Errorf("page %d: %+v", i+1, got)
			}
		}
	})
}

// A box in a multi-column container is laid out as one column.
func TestMulticolNestedIsOneColumn(t *testing.T) {
	html := `<div style="columns:2;column-gap:20px;width:620px"><div id="in" style="columns:3"><p>` + mcPara("n", 120) + `</p></div></div>`
	r := newRun(t, html, opts{extra: colPage})
	pages := r.all(t)
	in := fragByID(t, pages, "in")
	if ws, _ := r.b.Warnings(); !slices.ContainsFunc(ws, func(w budget.Warning) bool {
		return w.Code == budget.WarnUnsupportedProperty && w.Detail == "nested columns"
	}) {
		t.Errorf("no warning for the nested columns: %v", ws)
	}
	if in.rect.W != 300 || len(in.rules) != 0 {
		t.Errorf("inner container %+v", in.rect)
	}
	for _, ln := range linesOf(pages[0]) {
		if ln.rect.W != 300 {
			t.Fatalf("a line of the inner container is %v wide: it made columns", ln.rect.W)
		}
	}
}

// A container with one column, or none, is a block.
func TestMulticolOneColumnIsABlock(t *testing.T) {
	for _, st := range []string{"columns:1", "columns:auto", "column-count:1;column-rule:1px solid red"} {
		pages := newRun(t, `<div id="c" style="`+st+`"><p>`+mcPara("a", 60)+`</p></div>`, opts{extra: colPage}).all(t)
		c := fragByID(t, pages, "c")
		if len(c.rules) != 0 || c.rect.W != 800 {
			t.Errorf("%s: %+v", st, c.rect)
		}
	}
}

// A table that continues in the next column repeats its header there, at the
// column's x, whichever column it is (not the one it was first laid out in).
func TestMulticolTableHeaderRepeatsInEveryColumn(t *testing.T) {
	var rows strings.Builder
	for i := range 40 {
		fmt.Fprintf(&rows, "<tr><td>r%02d</td><td>b</td></tr>", i)
	}
	html := `<div style="columns:3;column-gap:20px;width:600px"><table style="width:100%;border-spacing:0"><thead><tr><th>H</th><th>I</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div>`
	pages := newRun(t, html, opts{extra: "@page { size: 800px 200px; margin: 0 } body { margin: 0 }"}).all(t)
	first := map[float64]int{}
	walkPages(pages[:1], func(f *frag) {
		if f.box.Tag.String() == "th" && f.rect.X < 100+413.34 && (f.rect.X < 1 || f.rect.X > 200 && f.rect.X < 220 || f.rect.X > 410) {
			first[math.Round(f.rect.X)]++
		}
	})
	if first[0] != 1 || first[207] != 1 || first[413] != 1 {
		t.Errorf("first header cell of each column on the first page: %v", first)
	}
}

// A row cut between lines continues at the x of the next column.
func TestMulticolCutRowContinuesInTheNextColumn(t *testing.T) {
	cell := strings.Repeat("line<br>", 12)
	html := `<div style="columns:2;column-gap:20px;width:600px"><table style="width:100%;border-spacing:0"><tr><td>` + cell + `</td></tr><tr><td>b</td></tr></table></div>`
	pages := newRun(t, html, opts{extra: "@page { size: 800px 120px; margin: 0 } body { margin: 0 }"}).all(t)
	var xs []float64
	walkPages(pages, func(f *frag) {
		if f.box.Tag.String() == "td" {
			xs = append(xs, f.rect.X)
		}
	})
	if len(xs) < 2 {
		t.Fatalf("td fragments: %v", xs)
	}
	for _, x := range xs {
		if math.Abs(x) > 0.01 && math.Abs(x-310) > 0.01 {
			t.Errorf("td fragment at x=%v: not a column x", x)
		}
	}
	seen := false
	for _, x := range xs {
		seen = seen || math.Abs(x-310) < 0.01
	}
	if !seen {
		t.Errorf("no td fragment in the second column: %v", xs)
	}
}

// Orphans and widows of 2 apply to a paragraph at a column break.
func TestMulticolOrphansAndWidows(t *testing.T) {
	linesPerColumn := func(html string) map[float64]int {
		pages := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 } p { margin: 0; line-height: 16px }"}).all(t)
		perCol := map[float64]int{}
		for _, pg := range pages[:1] {
			for _, ln := range linesOf(pg) {
				perCol[ln.rect.X]++
			}
		}
		return perCol
	}
	// Room for six lines in each of two columns. A paragraph of 13 would leave one
	// line for the next page, so the widows pull one over: 6 + 5.
	got := linesPerColumn(`<div style="columns:2;column-gap:0"><p>` + strings.Repeat("w<br>", 12) + `w</p></div>`)
	if got[0] != 6 || got[200] != 5 {
		t.Errorf("widows: %v", got)
	}
	// A paragraph that would leave one line alone at the end of a column starts
	// the next one: 5 + 6.
	got = linesPerColumn(`<div style="columns:2;column-gap:0"><p>` + strings.Repeat("a<br>", 4) + `a</p><p>` + strings.Repeat("b<br>", 9) + `b</p></div>`)
	if got[0] != 5 || got[200] != 6 {
		t.Errorf("orphans: %v", got)
	}
}

// A forced page break inside the columns ends the segment: what is before it is
// balanced, the rest is on the next page.
func TestMulticolForcedBreak(t *testing.T) {
	html := `<div id="c" style="columns:2;column-gap:20px"><p>` + mcPara("a", 120) + `</p><p style="break-before:page">` + mcPara("b", 60) + `</p></div>`
	pages := newRun(t, html, opts{extra: colPage}).all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	c := frags(pages[:1], "div")[0]
	if c.rect.H >= 600 {
		t.Errorf("the segment before the forced break fills the page: %v", c.rect.H)
	}
	if got := textOf(pages[1:]); strings.Contains(got, "a119") || !strings.Contains(got, "b000") {
		t.Errorf("page 2: %.60q", got)
	}
}

// A container that starts where no line of it fits goes whole to the next page.
func TestMulticolMovesWhenNothingFits(t *testing.T) {
	html := `<div style="height:590px"></div><div id="c" style="columns:2"><p>` + mcPara("a", 40) + `</p></div>`
	pages := newRun(t, html, opts{extra: colPage}).all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	if got := textOf(pages[:1]); got != "" {
		t.Errorf("page 1 holds %.40q", got)
	}
	if c := fragByID(t, pages, "c"); c.rect.Y != 0 {
		t.Errorf("container at y=%v", c.rect.Y)
	}
}

// Columns inside a flex item and an atomic inline are laid out, and their rules
// move with them.
func TestMulticolInScratchLayouts(t *testing.T) {
	css := `.c { columns: 2; column-gap: 10px; column-rule: 2px solid #f00 }`
	for name, html := range map[string]string{
		"flex item":     `<div style="display:flex"><div class="c" style="width:300px;margin-left:40px"><p>` + mcPara("a", 60) + `</p></div></div>`,
		"inline-block":  `<p><span class="c" style="display:inline-block;width:300px"><span style="display:block">` + mcPara("a", 60) + `</span></span></p>`,
		"on a table td": `<table><tr><td class="c" style="width:300px">` + mcPara("a", 60) + `</td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			pages := newRun(t, html, opts{extra: colPage, css: css}).all(t)
			var rule *display.FillRect
			for _, it := range pages[0].Items {
				if fr, ok := it.(display.FillRect); ok && fr.Color.R == 1 && fr.Color.G == 0 {
					rule = &fr
				}
			}
			if rule == nil {
				t.Fatal("no rule")
			}
			xs := map[float64]bool{}
			for _, ln := range linesOf(pages[0]) {
				xs[ln.rect.X] = true
			}
			if len(xs) != 2 {
				t.Fatalf("column x of the lines: %v", xs)
			}
			var left, right float64 = math.MaxFloat64, 0
			for x := range xs {
				left, right = math.Min(left, x), math.Max(right, x)
			}
			if rule.Rect.X < left+100 || rule.Rect.X > right {
				t.Errorf("rule at x=%v between columns at %v and %v", rule.Rect.X, left, right)
			}
		})
	}
}

// columns: 1000 is clamped so that every column is at least 1 px; 999 gives the
// same layout, and a count past the clamp warns.
func TestMulticolClampsTheCount(t *testing.T) {
	doc := func(n string) (*run, string) {
		r := newRun(t, `<div style="columns:`+n+`;column-gap:0;width:600px"><p>`+mcPara("w", 30)+`</p></div>`, opts{extra: colPage})
		return r, dumpPages(t, r.all(t))
	}
	r1000, d1000 := doc("1000")
	_, d600 := doc("600")
	r999, d999 := doc("999")
	if d1000 != d600 || d999 != d600 {
		t.Error("1000, 999 and 600 columns do not lay out alike")
	}
	if !clampedColumns(r1000) || !clampedColumns(r999) {
		t.Error("no clamp warning")
	}
	r2, _ := doc("2")
	if clampedColumns(r2) {
		t.Error("columns: 2 warned")
	}
}

// The layout of the content of a balanced segment is bounded: at most 20
// passes, and the Work is at most 21 times that of the same content in one
// column.
func TestMulticolBalanceIsBounded(t *testing.T) {
	var body strings.Builder
	for i := range 40 {
		fmt.Fprintf(&body, `<p style="margin:0 0 %dpx">%s</p>`, i%9, mcPara(fmt.Sprintf("p%02d_", i), 5+i*7%31))
	}
	css := "@page { size: 800px 4000px; margin: 0 } body { margin: 0 }"
	multi := newRun(t, `<div style="columns:3;column-gap:20px">`+body.String()+`</div>`, opts{extra: colPage, css: css})
	passes, most := 0, 0
	multi.pag.l.onColumns = func() { passes++ }
	for {
		_, err := multi.pag.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		most = max(most, passes)
		passes = 0
	}
	twin := newRun(t, `<div>`+body.String()+`</div>`, opts{extra: colPage, css: css})
	twin.all(t)
	if most < 3 || most > maxBalancePasses {
		t.Errorf("%d passes on the page, want 3 to %d", most, maxBalancePasses)
	}
	if w, tw := multi.b.Used(budget.Work), twin.b.Used(budget.Work); w > 21*tw {
		t.Errorf("work %d, the one-column twin %d: more than 21x", w, tw)
	}
}

// A page of table rows in columns costs what one column of them costs: the work
// is linear in the rows, and the heap stays bounded.
func TestMulticolTenThousandRows(t *testing.T) {
	if testing.Short() {
		t.Skip("10 000 rows twice")
	}
	start := time.Now()
	rowsDoc := func(n int) string {
		var sb strings.Builder
		for i := range n {
			fmt.Fprintf(&sb, "<tr><td>r%d</td><td>x</td></tr>", i)
		}
		return `<div style="columns:2;column-gap:20px"><table style="width:100%"><thead><tr><th>A</th><th>B</th></tr></thead><tbody>` + sb.String() + `</tbody></table></div>`
	}
	work := func(n int) (int64, int) {
		r := newRun(t, rowsDoc(n), opts{extra: colPage, limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
		var pages []*Page
		heap := peakHeap(func() { pages = r.all(t) })
		if heap > peakHeapCap() {
			t.Errorf("%d rows: heap peak %d MiB", n, heap>>20)
		}
		return r.b.Used(budget.Work), len(pages)
	}
	w5, p5 := work(5000)
	w10, p10 := work(10000)
	if p10 < 2*p5-2 || p10 > 2*p5+2 {
		t.Errorf("pages: %d for 5000 rows, %d for 10000", p5, p10)
	}
	if float64(w10) > 2.1*float64(w5) {
		t.Errorf("work %d for 10000 rows, %d for 5000: not linear", w10, w5)
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

func BenchmarkMulticol(b *testing.B) {
	var body strings.Builder
	for i := range 40 {
		fmt.Fprintf(&body, `<p style="margin:0 0 %dpx">%s</p>`, i%9, mcPara(fmt.Sprintf("p%02d_", i), 5+i*7%31))
	}
	var rows strings.Builder
	for i := range 10000 {
		fmt.Fprintf(&rows, "<tr><td>r%d</td><td>x</td></tr>", i)
	}
	for name, html := range map[string]string{
		"balanced":      `<div style="columns:3;column-gap:20px">` + body.String() + `</div>`,
		"one column":    `<div>` + body.String() + `</div>`,
		"10000 rows":    `<div style="columns:2"><table>` + rows.String() + `</table></div>`,
		"1 column rows": `<div><table>` + rows.String() + `</table></div>`,
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				b.StopTimer()
				r := newRun(b, html, opts{extra: "@page { size: 800px 4000px; margin: 0 } body { margin: 0 }", limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
				b.StartTimer()
				r.all(b)
			}
		})
	}
}

// Outside a multi-column container a paragraph breaks as before: no orphans or
// widows control (7 lines in a page of 6 leave one alone on the next).
func TestOrphansWidowsOnlyInColumns(t *testing.T) {
	html := `<p>` + strings.Repeat("w<br>", 6) + `w</p>`
	pages := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 0 } body { margin: 0 } p { margin: 0; line-height: 16px }"}).all(t)
	if len(pages) != 2 || len(linesOf(pages[0])) != 6 || len(linesOf(pages[1])) != 1 {
		t.Errorf("%d pages, %d and %d lines", len(pages), len(linesOf(pages[0])), len(linesOf(pages[len(pages)-1])))
	}
}

// What multi-column layout does not support is warned about.
func TestMulticolUnsupportedIsWarned(t *testing.T) {
	for _, st := range []string{"column-width:100px", "column-span:all", "column-fill:auto", "break-before:column", "break-after:column", "min-height:30px"} {
		r := newRun(t, `<div style="columns:2;`+st+`"><p>a</p></div>`, opts{extra: colPage})
		r.all(t)
		if ws, _ := r.b.Warnings(); len(ws) == 0 {
			t.Errorf("%s: no warning", st)
		}
	}
}

// The trial layouts of the balancing are not emitted: Glyphs counts what the
// pages hold, as it does for the same content in plain blocks.
func TestMulticolChargesGlyphsOnce(t *testing.T) {
	body := `<p>` + mcPara("g", 60) + `</p><p>` + mcPara("h", 45) + `</p>`
	charged := func(html string) (glyphs, work int64) {
		r := newRun(t, html, opts{extra: colPage})
		r.all(t)
		return r.b.Used(budget.Glyphs), r.b.Used(budget.Work)
	}
	pg, pw := charged(`<div>` + body + `</div>`)
	mg, mw := charged(`<div style="columns:3">` + body + `</div>`)
	if pg == 0 || mg > pg+pg/100 || mg+mg/100 < pg {
		t.Errorf("glyphs: %d in columns, %d plain", mg, pg)
	}
	if mw > 21*pw {
		t.Errorf("work: %d in columns, %d plain", mw, pw)
	}
}

func threeParagraphs() string {
	return `<p>a<br>a<br>a</p><p>b<br>b<br>b</p><p>c<br>c<br>c</p>`
}

// Three paragraphs of three lines in two columns balance to two and one
// paragraph, 96 px: a last line that cannot be pulled over moves its paragraph.
func TestMulticolWidowsDoNotBreakTheBalance(t *testing.T) {
	css := "p { line-height: 16px } td { padding: 0 }"
	for name, html := range map[string]string{
		"block":     `<div id="c" style="columns:2;column-gap:0">` + threeParagraphs() + `</div>`,
		"flex item": `<div style="display:flex"><div id="c" style="columns:2;column-gap:0;width:200px">` + threeParagraphs() + `</div></div>`,
		"td":        `<table style="border-spacing:0"><tr><td id="c" style="columns:2;column-gap:0;width:200px">` + threeParagraphs() + `</td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			pages := newRun(t, html, opts{extra: colPage, css: css}).all(t)
			if c := fragByID(t, pages, "c"); math.Abs(c.rect.H-96) > 0.01 {
				t.Errorf("height %v, want 96", c.rect.H)
			}
		})
	}
}

// A row cut across many columns pays for every cut: the walk of the row is
// charged to Work, so a hostile cell ends in a LimitError instead of CPU time.
func TestMulticolRowCutAcrossColumnsIsCharged(t *testing.T) {
	start := time.Now()
	html := `<div style="columns:1000;column-gap:0"><table style="width:100%;border-spacing:0"><tr><td style="padding:0">` + strings.Repeat("l<br>", 100000) + `</td></tr></table></div>`
	r := newRun(t, html, opts{extra: "@page { size: 800px 200px; margin: 0 } body { margin: 0 }"})
	var err error
	for err == nil {
		_, err = r.pag.Next()
	}
	var le *budget.LimitError
	if !errors.Is(err, io.EOF) && !errors.As(err, &le) {
		t.Fatal(err)
	}
	if r.b.Used(budget.Work) < 20_000_000 {
		t.Errorf("work %d for 100000 cut lines", r.b.Used(budget.Work))
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

// Flex items in columns are planned once, not once per column: the work stays
// within twice that of the one-column twin.
func TestMulticolFlexItemsWorkIsLinear(t *testing.T) {
	items := strings.Repeat(`<div style="width:100px;flex:none">x</div>`, 20000)
	work := func(style string) int64 {
		r := newRun(t, `<div style="`+style+`"><div style="display:flex;flex-wrap:wrap">`+items+`</div></div>`, opts{extra: "@page { size: 800px 600px; margin: 0 } body { margin: 0 }", limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
		r.all(t)
		return r.b.Used(budget.Work)
	}
	twin, multi := work(""), work("columns:50;column-gap:0")
	if multi > 2*twin+100_000 {
		t.Errorf("work %d in columns, %d in one", multi, twin)
	}
}

// A fixed box in a balanced container is registered once, not once per pass.
func TestMulticolFixedBoxIsPlacedOnce(t *testing.T) {
	html := `<div style="columns:3;column-gap:10px"><div style="position:fixed;left:0">MARK</div><p>` + mcPara("w", 80) + `</p></div>`
	r := newRun(t, html, opts{extra: colPage})
	passes := 0
	r.pag.l.onColumns = func() { passes++ }
	total := 0
	for _, pg := range r.all(t) {
		total += countText(pg, "MARK")
	}
	if passes < 3 {
		t.Fatalf("%d passes: the container was not balanced", passes)
	}
	if total != 1 {
		t.Errorf("%d copies of the fixed box on one page", total)
	}
}
