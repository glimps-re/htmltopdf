package layout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// tableDoc is a page of 800x600 px without margins and a body without margin.
const tableDoc = smallPage + " body{margin:0} table{table-layout:fixed;border-spacing:0} td,th{padding:0}"

// fragsOf returns the fragments of tag in page order, depth first.
func fragsOf(pages []*Page, tag string) []*frag {
	var out []*frag
	var walk func(f *frag)
	walk = func(f *frag) {
		if f.box.Tag.String() == tag {
			out = append(out, f)
		}
		for _, c := range f.children {
			walk(c)
		}
	}
	for _, pg := range pages {
		if pg.root != nil {
			walk(pg.root)
		}
	}
	return out
}

func rects(fs []*frag) string {
	var sb strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&sb, "%s,%s %sx%s;", num(f.rect.X), num(f.rect.Y), num(f.rect.W), num(f.rect.H))
	}
	return sb.String()
}

func TestTableColumnsShareTheWidth(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td>a<td>b<tr><td>c<td>d</table>`, opts{css: tableDoc})
	pages := r.all(t)
	got := rects(fragsOf(pages, "td"))
	if !strings.HasPrefix(got, "0,0 200x") || !strings.Contains(got, "200,0 200x") {
		t.Fatalf("cells %s", got)
	}
	tds := fragsOf(pages, "td")
	if len(tds) != 4 || tds[2].rect.Y <= tds[0].rect.Y || tds[2].rect.Y != tds[0].rect.Y+tds[0].rect.H {
		t.Fatalf("rows do not stack: %s", got)
	}
}

func TestTableSpecifiedAndColspan(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td style="width:100px">a<td>b<tr><td colspan=2>c<tr><td colspan=2>d</table>`, opts{css: tableDoc})
	pages := r.all(t)
	tds := fragsOf(pages, "td")
	if len(tds) != 4 {
		t.Fatalf("%d cells", len(tds))
	}
	if !near(tds[0].rect.W, 100) || !near(tds[1].rect.W, 300) || !near(tds[1].rect.X, 100) {
		t.Errorf("widths %s", rects(tds))
	}
	if !near(tds[2].rect.W, 400) || !near(tds[3].rect.W, 400) {
		t.Errorf("colspan widths %s", rects(tds[2:]))
	}
}

// px columns wider than the table keep their widths and the table overflows
// (Chrome, testdata/chrome_cols.txt case "px only 600,500 (over)").
func TestTableOverflowingWidthsDoNotShrink(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td style="width:600px">a<td style="width:200px">b</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if !near(tds[0].rect.W, 600) || !near(tds[1].rect.W, 200) || !near(tds[1].rect.X, 600) {
		t.Fatalf("widths %s", rects(tds))
	}
}

func TestTableSeparatedSpacing(t *testing.T) {
	r := newRun(t, `<table style="width:400px;border-spacing:10px 4px"><tr><td>a<td>b<tr><td>c<td>d</table>`, opts{css: tableDoc})
	pages := r.all(t)
	tds := fragsOf(pages, "td")
	if !near(tds[0].rect.X, 10) || !near(tds[0].rect.W, 185) || !near(tds[1].rect.X, 205) {
		t.Errorf("cells %s", rects(tds))
	}
	if !near(tds[0].rect.Y, 4) || !near(tds[2].rect.Y, tds[0].rect.Y+tds[0].rect.H+4) {
		t.Errorf("rows %s", rects(tds))
	}
	tbl := fragsOf(pages, "table")[0]
	if !near(tbl.rect.H, tds[2].rect.Y+tds[2].rect.H+4) {
		t.Errorf("table height %v", tbl.rect.H)
	}
}

func TestTableCollapsedBorders(t *testing.T) {
	r := newRun(t, `<table style="width:400px;border-collapse:collapse"><tr><td style="border:2px solid red">a<td style="border:4px solid blue">b</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	// Shared edge: the wider (blue, 4px) wins; each cell draws half of it.
	if tds[0].border[style.Right] != 2 || tds[1].border[style.Left] != 2 {
		t.Errorf("shared edge %v %v", tds[0].border, tds[1].border)
	}
	if tds[0].borderColor[style.Right] != tds[1].borderColor[style.Left] || tds[0].borderColor[style.Right].B != 1 {
		t.Errorf("shared edge color %v %v", tds[0].borderColor, tds[1].borderColor)
	}
	// Outer edges keep the full width, in the cell's own color.
	if tds[0].border[style.Left] != 2 || tds[1].border[style.Right] != 4 || tds[0].border[style.Top] != 2 {
		t.Errorf("outer edges %v %v", tds[0].border, tds[1].border)
	}
}

func TestTableCollapsedTableBorderWins(t *testing.T) {
	r := newRun(t, `<table style="width:400px;border-collapse:collapse;border:3px solid black"><tr><td style="border:2px solid red">a</table>`, opts{css: tableDoc})
	td := fragsOf(r.all(t), "td")[0]
	if td.border != [4]float64{} {
		t.Fatalf("a narrower cell border at the table edge is hidden by the table's: %v", td.border)
	}
}

func TestTableVerticalAlign(t *testing.T) {
	html := `<table style="width:400px"><tr>
<td style="vertical-align:top">a<br>a<br>a<td style="vertical-align:middle">b<td style="vertical-align:bottom">c</table>`
	r := newRun(t, html, opts{css: tableDoc})
	pages := r.all(t)
	tds := fragsOf(pages, "td")
	h := tds[0].rect.H
	for _, td := range tds {
		if !near(td.rect.H, h) {
			t.Fatalf("cells of a row share its height: %s", rects(tds))
		}
	}
	top, mid, bot := tds[0].lines[0].rect.Y, tds[1].lines[0].rect.Y, tds[2].lines[0].rect.Y
	lineH := tds[1].lines[0].rect.H
	if !near(top, 0) || !near(mid, (h-lineH)/2) || !near(bot, h-lineH) {
		t.Errorf("line tops %v %v %v, row %v line %v", top, mid, bot, h, lineH)
	}
}

func TestTableBaselineAlign(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td style="vertical-align:baseline;font-size:40px">a<td style="vertical-align:baseline;font-size:10px">b</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if !near(tds[0].lines[0].baseline, tds[1].lines[0].baseline) {
		t.Fatalf("baselines %v %v", tds[0].lines[0].baseline, tds[1].lines[0].baseline)
	}
}

func TestTableRowAndGroupBackgrounds(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tbody style="background:#eee"><tr style="background:#ddd"><td>a<tr><td>b</tbody></table>`, opts{css: tableDoc})
	pages := r.all(t)
	tb := fragsOf(pages, "tbody")
	if len(tb) != 1 || len(tb[0].children) != 2 || !near(tb[0].rect.H, tb[0].children[0].rect.H+tb[0].children[1].rect.H) {
		t.Fatalf("group fragment %v", tb)
	}
	var kinds []string
	for _, it := range pages[0].Items {
		kinds = append(kinds, fmt.Sprintf("%T", it))
	}
	if strings.Count(strings.Join(kinds, ","), "display.FillRect") != 2 {
		t.Errorf("items %v", kinds)
	}
}

func TestTableCaptionAndEmpty(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><caption>cap</caption></table><p>after</p>`, opts{css: tableDoc})
	pages := r.all(t)
	if len(pages) != 1 {
		t.Fatalf("%d pages", len(pages))
	}
	caps := fragsOf(pages, "caption")
	if len(caps) != 1 || caps[0].rect.H <= 0 {
		t.Fatalf("caption %v", caps)
	}
	r = newRun(t, `<table></table><div style="display:table"></div><p>x</p>`, opts{css: tableDoc})
	if got := len(r.all(t)); got != 1 {
		t.Fatalf("%d pages", got)
	}
}

func TestTableAutoLayoutHasNoWarning(t *testing.T) {
	for _, html := range []string{
		`<table style="table-layout:auto"><tr><td>a</table>`,
		`<table><thead><tr><th>h</thead><tbody><tr><td>a<td>b</tbody></table>`,
		`<table style="border-collapse:collapse;width:100%;table-layout:fixed"><tr><td colspan=2>a<tr><td>b<td>c</table>`,
	} {
		r := newRun(t, html, opts{css: smallPage})
		r.all(t)
		if ws := warningList(r.b); len(ws) != 0 {
			t.Errorf("%s: warnings %v", html, ws)
		}
	}
}

func warningList(b *budget.Budget) []budget.Warning {
	ws, _ := b.Warnings()
	return ws
}

func TestTablePaginationRepeatsHeader(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<table style="width:600px"><thead><tr>`)
	for c := range 6 {
		fmt.Fprintf(&sb, "<th>h%d", c)
	}
	sb.WriteString(`</thead><tbody>`)
	for i := range 20 {
		sb.WriteString("<tr>")
		for c := range 6 {
			fmt.Fprintf(&sb, "<td>r%dc%d", i, c)
		}
	}
	sb.WriteString(`</tbody></table>`)
	r := newRun(t, sb.String(), opts{css: tableDoc + " td,th{line-height:100px}"})
	pages := r.all(t)
	if len(pages) != 4 {
		t.Fatalf("%d pages, want 4", len(pages))
	}
	for i, pg := range pages {
		heads := fragsOf([]*Page{pg}, "thead")
		if len(heads) != 1 {
			t.Errorf("page %d: %d header groups", i+1, len(heads))
			continue
		}
		if !near(heads[0].rect.H, 100) {
			t.Errorf("page %d: header %v", i+1, heads[0].rect)
		}
		for _, row := range fragsOf([]*Page{pg}, "tr") {
			if row.rect.Y+row.rect.H > 600.01 {
				t.Errorf("page %d: row %v leaves the page", i+1, row.rect)
			}
		}
	}
	rows := 0
	for _, pg := range pages {
		rows += len(fragsOf([]*Page{pg}, "tr"))
	}
	if rows != 21+3 {
		t.Errorf("%d row fragments, want 24 (21 + 3 repeated headers)", rows)
	}
	golden(t, "table-pagination", pages)
}

func TestTableAfterContentMovesWholeWhenNothingFits(t *testing.T) {
	r := newRun(t, `<div style="line-height:550px">x</div><table style="width:400px"><tr><td style="line-height:100px">a</table>`, opts{css: tableDoc})
	pages := r.all(t)
	if len(pages) != 2 || len(fragsOf(pages[0:1], "td")) != 0 || len(fragsOf(pages[1:], "td")) != 1 {
		t.Fatalf("%d pages: %s", len(pages), dumpPages(t, pages))
	}
}

// rowFixture is the CSS of tasks.md: 200 px rows, no spacing.
const rowFixture = "body{margin:0} table{border-collapse:collapse} td{padding:0;line-height:200px}"

func TestTableSingleLineRowsAreIndivisible(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<table>")
	for range 12 {
		sb.WriteString("<tr><td>x")
	}
	sb.WriteString("</table>")
	r := newRun(t, sb.String(), opts{css: "@page{size:A4;margin:0}" + rowFixture})
	pages := r.all(t)
	if len(pages) != 3 {
		t.Fatalf("%d pages for 12 rows", len(pages))
	}
	counts := []int{5, 5, 2}
	for i, pg := range pages {
		rows := fragsOf([]*Page{pg}, "tr")
		if len(rows) != counts[i] {
			t.Errorf("page %d: %d rows, want %d", i+1, len(rows), counts[i])
		}
		for _, row := range rows {
			if !near(row.rect.H, 200) {
				t.Errorf("page %d: row height %v, want exactly 200", i+1, row.rect.H)
			}
		}
	}
}

func TestTableRowSplitsBetweenLines(t *testing.T) {
	// Rows of 100 px lines: page 600 px, a header of one line, a 3-line row of
	// two cells that starts at 400 px and breaks after its second line.
	html := `<table style="width:600px"><thead><tr><th>h</thead><tbody>
<tr><td>a<td>b<tr><td>c<td>d<tr><td>e<td>f
<tr><td>1<br>2<br>3<td>4<br>5<br>6</tbody></table>`
	r := newRun(t, html, opts{css: tableDoc + " td,th{line-height:100px}"})
	pages := r.all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages: %s", len(pages), dumpPages(t, pages))
	}
	count := func(pg *Page) (lines, cells int) {
		rows := fragsOf([]*Page{pg}, "tr")
		last := rows[len(rows)-1]
		for _, c := range last.children {
			lines += len(c.lines)
			cells++
		}
		return lines, cells
	}
	l1, c1 := count(pages[0])
	if c1 != 2 || l1 != 4 {
		t.Errorf("page 1: last row has %d cells and %d lines, want 2 cells with 2 lines each", c1, l1)
	}
	l2, c2 := count(pages[1])
	if c2 != 2 || l2 != 2 {
		t.Errorf("page 2: last row has %d cells and %d lines, want 2 cells with 1 line each", c2, l2)
	}
	heads := fragsOf(pages[1:], "thead")
	if len(heads) != 1 {
		t.Fatalf("page 2 has %d header groups", len(heads))
	}
	rows := fragsOf(pages[1:], "tr")
	if !near(rows[0].rect.Y, 0) || !near(rows[1].rect.Y, 100) {
		t.Errorf("continuation does not follow the repeated header: %s", rects(rows))
	}
	for _, td := range fragsOf(pages[1:], "td") {
		if td.first {
			t.Errorf("a continued cell is not a first fragment")
		}
	}
	// No line is lost or drawn twice.
	total := 0
	for _, pg := range pages {
		for _, td := range fragsOf([]*Page{pg}, "td") {
			total += len(td.lines)
		}
	}
	if total != 6+6 {
		t.Errorf("%d lines in cells, want 12", total)
	}
}

func TestTableTallSingleLineRowIsSliced(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td style="line-height:2000px">x</table><p>after</p>`, opts{css: tableDoc})
	pages := r.all(t)
	if len(pages) < 4 {
		t.Fatalf("%d pages for a 2000 px row on 600 px pages", len(pages))
	}
	baselines := 0
	for _, pg := range pages {
		for _, td := range fragsOf([]*Page{pg}, "td") {
			baselines += len(td.lines)
			if td.rect.H > 600.01 {
				t.Errorf("slice of %v px on a 600 px page", td.rect.H)
			}
		}
	}
	if baselines != 1 {
		t.Errorf("the line is drawn %d times, want once", baselines)
	}
	if ps := fragsOf(pages[len(pages)-1:], "p"); len(ps) != 1 {
		t.Errorf("the paragraph after the table is not on the last page")
	}
	found := false
	for _, w := range warningList(r.b) {
		found = found || w.Code == budget.WarnClamped
	}
	if !found {
		t.Error("no warning for the sliced row")
	}
}

func TestTableCutRow(t *testing.T) {
	iv := []interval{{0, 100}, {100, 200}, {200, 300}}
	for _, tc := range []struct {
		from, avail, want float64
		ok                bool
	}{
		{0, 250, 200, true},
		{0, 100, 100, true},
		{0, 99, 0, false},
		{0, 300, 300, true},
		{100, 150, 200, true},
		{100, 50, 0, false},
	} {
		got, ok := cutRow(iv, tc.from, tc.avail)
		if ok != tc.ok || ok && !near(got, tc.want) {
			t.Errorf("cutRow(from %v, avail %v) = %v, %v; want %v, %v", tc.from, tc.avail, got, ok, tc.want, tc.ok)
		}
	}
	// A line that straddles a cut moves the cut before it.
	if got, ok := cutRow([]interval{{0, 100}, {100, 400}}, 0, 250); !ok || !near(got, 100) {
		t.Errorf("tall line: %v %v", got, ok)
	}
	if _, ok := cutRow(nil, 0, 100); ok {
		t.Error("a row without lines has a cut")
	}
}

func TestTableNested(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td><table style="width:100%"><tr><td>x<td>y</table><td>z</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if len(tds) != 4 {
		t.Fatalf("%d cells", len(tds))
	}
	if !near(tds[1].rect.W, 100) {
		t.Errorf("nested cell %s", rects(tds))
	}
}

func TestTableHostileColspan(t *testing.T) {
	var html []byte
	for _, c := range testcorpus.SpikeHostile() {
		if c.Name == "hostile_colspan" {
			html = c.HTML()
		}
	}
	if html == nil {
		t.Fatal("no hostile_colspan case")
	}
	start := time.Now()
	pages, err := pipelineRun(t, html, nil)
	if err != nil || pages == 0 {
		t.Fatalf("%d pages, %v", pages, err)
	}
	if d := time.Since(start); d > time.Minute {
		t.Errorf("took %v", d)
	}
	r := newRun(t, string(html), opts{css: tableDoc})
	r.all(t)
	assertWorkBound(t, r, string(html), 40)
	found := false
	for _, w := range warningList(r.b) {
		found = found || w.Code == budget.WarnClamped
	}
	if !found {
		t.Error("no clamped warning")
	}
	for _, td := range fragsOf(r.all(t), "td") {
		if math.IsNaN(td.rect.W) || td.rect.W < 0 {
			t.Fatalf("cell %v", td.rect)
		}
	}
}

func TestTableMoreColumnsThanTheLimit(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<table style=\"width:4000px\"><tr>")
	for range 1100 {
		sb.WriteString("<td>x")
	}
	sb.WriteString("</table>")
	r := newRun(t, sb.String(), opts{css: tableDoc})
	if n := len(fragsOf(r.all(t), "td")); n != 1000 {
		t.Fatalf("%d cells laid out, want 1000", n)
	}
	found := false
	for _, w := range warningList(r.b) {
		found = found || (w.Code == budget.WarnClamped && w.Detail == "table columns > 1000")
	}
	if !found {
		t.Fatal("no warning for the cells past column 1000")
	}
}

func TestTableScratchDoesNotLeak(t *testing.T) {
	// Content after a table starts where the table ends, with its margins.
	r := newRun(t, `<p style="margin:10px 0">a</p><table style="width:400px"><tr><td>b</table><p style="margin:10px 0">c</p>`, opts{css: tableDoc})
	pages := r.all(t)
	ps := fragsOf(pages, "p")
	tbl := fragsOf(pages, "table")[0]
	if len(ps) != 2 || !near(ps[1].rect.Y, tbl.rect.Y+tbl.rect.H+10) {
		t.Fatalf("p after table at %v, table %v", ps[1].rect, tbl.rect)
	}
}

func pipelineRun(tb testing.TB, html []byte, mod func(*budget.Limits)) (pages int, err error) {
	tb.Helper()
	l := budget.Defaults()
	if mod != nil {
		mod(&l)
	}
	b := budget.New(context.Background(), l, nil)
	d, err := dom.Parse(strings.NewReader(string(html)), b)
	if err != nil {
		return 0, err
	}
	var docSheets []*style.Sheet
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Tag.String() != "style" || e.NS != dom.NSHTML {
			continue
		}
		var sb strings.Builder
		for r := e.FirstChild; r != dom.NoNode; r = d.NextSibling(r) {
			if ti, ok := r.Text(); ok {
				sb.WriteString(d.Texts[ti].Data)
			}
		}
		ss, err := css.ParseStylesheet([]byte(sb.String()), css.OriginDocument, b)
		if err != nil {
			return 0, err
		}
		sh, err := style.Compile(ss, b)
		if err != nil {
			return 0, err
		}
		docSheets = append(docSheets, sh)
	}
	r := style.NewResolver(uaCompiled(tb), nil, docSheets, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
	root, err := box.Build(d, r, b)
	if err != nil {
		return 0, err
	}
	sh := text.NewShaper(testFonts(tb), b)
	p := NewPaginator(root, Config{Page: r.Page(style.PageContext{Number: 1}), Fonts: testFonts(tb), Shaper: sh, Budget: b})
	for {
		_, err := p.Next()
		if errors.Is(err, io.EOF) {
			return pages, nil
		}
		if err != nil {
			return pages, err
		}
		pages++
	}
}

func hostileCase(tb testing.TB, name string) (testcorpus.Case, []byte) {
	tb.Helper()
	for _, c := range testcorpus.Hostile(budget.Defaults()) {
		if c.Name == name {
			return c, c.HTML()
		}
	}
	tb.Fatalf("no hostile case %q", name)
	return testcorpus.Case{}, nil
}

func TestTablePageLimitCriterion(t *testing.T) {
	c, html := hostileCase(t, "pages_table_tall_rows_over")
	_, err := pipelineRun(t, html, c.Limits)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Pages {
		t.Fatalf("2,505 tall rows: %v, want LimitPages", err)
	}
	c, html = hostileCase(t, "pages_table_tall_rows_under")
	pages, err := pipelineRun(t, html, c.Limits)
	if err != nil || pages != 499 {
		t.Fatalf("2,495 tall rows: %d pages, %v; want 499", pages, err)
	}
}

func heapMax(stop <-chan struct{}, out chan<- uint64) {
	var peak uint64
	var ms runtime.MemStats
	for {
		runtime.ReadMemStats(&ms)
		peak = max(peak, ms.HeapAlloc)
		select {
		case <-stop:
			out <- peak
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestTableRowStress(t *testing.T) {
	if testing.Short() {
		t.Skip("row stress")
	}
	for _, name := range []string{"pages_table_rows_over", "pages_table_rows_under"} {
		c, html := hostileCase(t, name)
		runtime.GC()
		stop, peak := make(chan struct{}), make(chan uint64, 1)
		go heapMax(stop, peak)
		start := time.Now()
		pages, err := pipelineRun(t, html, c.Limits)
		took := time.Since(start)
		close(stop)
		heap := <-peak
		t.Logf("%s: %d pages, %v, %v, heap max %d MiB", name, pages, err, took.Round(time.Millisecond), heap>>20)
		if took > time.Minute {
			t.Errorf("%s: took %v", name, took)
		}
		if heap > 128<<20 {
			t.Errorf("%s: heap max %d MiB", name, heap>>20)
		}
		var le *budget.LimitError
		switch name {
		case "pages_table_rows_over":
			if !errors.As(err, &le) || le.Kind != budget.Pages {
				t.Errorf("%s: %v, want LimitPages", name, err)
			}
		default:
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}

func TestTableHiddenBorderSuppressesTheSharedEdge(t *testing.T) {
	r := newRun(t, `<table style="width:400px;border-collapse:collapse"><tr><td style="border:3px solid red">a<td style="border:3px solid red;border-left-style:hidden">b</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if tds[0].border[style.Right] != 0 || tds[1].border[style.Left] != 0 {
		t.Fatalf("hidden edge drawn: %v %v", tds[0].border, tds[1].border)
	}
}

func TestTableBaselineWithoutLines(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><tr><td style="vertical-align:baseline"><td style="vertical-align:baseline">text</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if len(tds) != 2 || tds[0].rect.H != tds[1].rect.H {
		t.Fatalf("cells %s", rects(tds))
	}
}

func TestTableExactlyTheColumnLimitIsNotWarned(t *testing.T) {
	r := newRun(t, "<table style=\"width:4000px\"><tr>"+strings.Repeat("<td>x", 1000)+"</table>", opts{css: tableDoc})
	if n := len(fragsOf(r.all(t), "td")); n != 1000 {
		t.Fatalf("%d cells", n)
	}
	for _, w := range warningList(r.b) {
		if w.Detail == "table columns > 1000" {
			t.Fatal("warned at the limit")
		}
	}
}

func tallHeaderTable(headRows, bodyRows int) string {
	var sb strings.Builder
	sb.WriteString(`<table style="width:600px"><thead>`)
	for range headRows {
		sb.WriteString("<tr><th>h")
	}
	sb.WriteString("</thead><tbody>")
	for range bodyRows {
		sb.WriteString("<tr><td>b")
	}
	sb.WriteString("</tbody></table>")
	return sb.String()
}

// A header too tall to repeat is measured once, not on every page.
func TestTableTallHeaderIsNotRelaidPerPage(t *testing.T) {
	const head, body = 2000, 2000
	css := tableDoc + " td,th{line-height:20px}"
	with := newRun(t, tallHeaderTable(head, body), opts{css: css})
	withPages := with.all(t)
	without := newRun(t, tallHeaderTable(0, head+body), opts{css: css})
	without.all(t)
	workWith, workWithout := with.b.Used(budget.Work), without.b.Used(budget.Work)
	t.Logf("%d pages: work %d with a tall thead, %d without", len(withPages), workWith, workWithout)
	if float64(workWith) > 1.5*float64(workWithout) {
		t.Errorf("a tall thead charged %d work units against %d without (measured 1.26x, bound 1.5x): it is laid out again on every page", workWith, workWithout)
	}
	if got := len(fragsOf(withPages[100:101], "thead")); got != 0 {
		t.Errorf("a header taller than half a page was repeated")
	}
}

func TestTableLayoutIsReleasedWhenTheTableIsDone(t *testing.T) {
	r := newRun(t, strings.Repeat(`<table><tr><td colspan=1000>x</table>`, 50), opts{css: tableDoc})
	r.all(t)
	if n := len(r.pag.l.tables); n != 0 {
		t.Fatalf("%d table layouts kept after the render", n)
	}
}

func TestTableManyColspanTablesStayInBudget(t *testing.T) {
	html := strings.Repeat(`<table><td colspan=1000></table>`, 6000)
	runtime.GC()
	stop, peak := make(chan struct{}), make(chan uint64, 1)
	go heapMax(stop, peak)
	r := newRun(t, html, opts{css: tableDoc})
	r.all(t)
	close(stop)
	heap := <-peak
	if heap > 128<<20 && !raceEnabled {
		t.Errorf("heap max %d MiB", heap>>20)
	}
}

func TestTableHugeBorderSpacingDoesNotFloodPages(t *testing.T) {
	r := newRun(t, `<table style="border-spacing:0 2000px"><tr><td style="line-height:400px">x</table>`, opts{css: tableDoc})
	pages := r.all(t)
	if len(pages) > 3 {
		t.Fatalf("%d pages", len(pages))
	}
	found := false
	for _, w := range warningList(r.b) {
		found = found || (w.Code == budget.WarnClamped && w.Detail == "border-spacing")
	}
	if !found {
		t.Error("no clamped warning for the spacing")
	}
}

// The content width of a table, and so the cache key of its layout, is the same
// on every page: a split row resumes at the same columns.
func TestTableWidthIsConstantAcrossPages(t *testing.T) {
	r := newRun(t, `<div style="margin:0 10%"><table style="width:80%"><tr><td>1<br>2<br>3<br>4<br>5<br>6<td>b</table></div>`, opts{css: tableDoc + " td{line-height:200px}"})
	pages := r.all(t)
	if len(pages) < 2 {
		t.Fatalf("%d pages", len(pages))
	}
	var x, w float64
	for i, pg := range pages {
		for _, td := range fragsOf([]*Page{pg}, "td")[:1] {
			if i == 0 {
				x, w = td.rect.X, td.rect.W
			} else if !near(td.rect.X, x) || !near(td.rect.W, w) {
				t.Errorf("page %d: first cell at %v,%v want %v,%v", i+1, td.rect.X, td.rect.W, x, w)
			}
		}
	}
}

func BenchmarkTable(b *testing.B) {
	for _, rows := range []int{1000, 10000} {
		html := tallHeaderTable(1, rows)
		b.Run(fmt.Sprintf("%d rows", rows), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := pipelineRun(b, []byte(html), func(l *budget.Limits) { l[budget.Pages] = 100000; l[budget.Nodes] = 1 << 22; l[budget.Boxes] = 1 << 22 }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Columns that no cell starts or ends at share a track, but the widths are
// those of the column-by-column computation.
func TestTableTrackCompressionKeepsTheColumnWidths(t *testing.T) {
	r := newRun(t, `<table style="width:400px;border-spacing:10px 0"><tr><td style="width:100px">a<td>b<td>c<td>d<tr><td colspan=3>e<td>f<tr><td>g<td colspan=3>h</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	// 4 columns, spacing 10: 400 - 50 = 350 for the columns, 100 specified, 3 auto of 83.33.
	auto := 250.0 / 3
	want := []struct{ x, w float64 }{
		{10, 100},
		{120, auto},
		{120 + auto + 10, auto},
		{120 + 2*auto + 20, auto},
		{10, 100 + 2*auto + 20},
		{120 + 2*auto + 20, auto},
		{10, 100},
		{120, 3*auto + 20},
	}
	if len(tds) != len(want) {
		t.Fatalf("%d cells", len(tds))
	}
	for i, w := range want {
		if !near(tds[i].rect.X, w.x) || !near(tds[i].rect.W, w.w) {
			t.Errorf("cell %d at %v, width %v; want %v, %v", i, tds[i].rect.X, tds[i].rect.W, w.x, w.w)
		}
	}
}

func TestTableColumnWidths(t *testing.T) {
	html := `<table style="width:400px"><colgroup><col style="width:50px"><col style="width:25%"><col></colgroup><tr><td style="width:300px">a<td>b<td>c<td>d</table>`
	r := newRun(t, html, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	// col widths win over the cell width; the fourth column (no col) and the
	// third (auto col) share what is left: 400 - 50 - 100 = 250.
	want := []struct{ x, w float64 }{{0, 50}, {50, 100}, {150, 125}, {275, 125}}
	if len(tds) != 4 {
		t.Fatalf("%d cells", len(tds))
	}
	for i, w := range want {
		if !near(tds[i].rect.X, w.x) || !near(tds[i].rect.W, w.w) {
			t.Errorf("cell %d at %v width %v, want %v %v", i, tds[i].rect.X, tds[i].rect.W, w.x, w.w)
		}
	}
}

func TestTableColgroupWithoutColsAndSpan(t *testing.T) {
	html := `<table style="width:200px"><colgroup span=2 style="width:50px"></colgroup><colgroup><col span=1 style="width:100px"></colgroup><tr><td>a<td>b<td>c</table>`
	r := newRun(t, html, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if len(tds) != 3 || !near(tds[0].rect.W, 50) || !near(tds[1].rect.W, 50) || !near(tds[2].rect.W, 100) {
		t.Fatalf("cells %s", rects(tds))
	}
}

func TestTableColumnsWithoutCells(t *testing.T) {
	r := newRun(t, `<table style="width:400px"><col style="width:100px"><col><tr><td>a</table>`, opts{css: tableDoc})
	tds := fragsOf(r.all(t), "td")
	if len(tds) != 1 || !near(tds[0].rect.W, 100) {
		t.Fatalf("cells %s", rects(tds))
	}
}

func TestTableHostileColumns(t *testing.T) {
	for name, html := range map[string]string{
		"huge span":      `<table style="width:400px"><col span=99999999 style="width:1px"><tr><td>a</table>`,
		"100k cols":      `<table style="width:400px">` + strings.Repeat(`<col style="width:1px">`, 100_000) + `<tr><td>a</table>`,
		"colgroup+cols":  `<table style="width:400px"><colgroup span=500><col span=700><col span=700></colgroup><tr><td>a<td>b</table>`,
		"nested":         `<table style="width:400px"><colgroup><colgroup><col span=3></colgroup></colgroup><tr><td>a</table>`,
		"zero and junk":  `<table style="width:400px"><col span=0><col span=x style="width:50%"><tr><td>a</table>`,
		"percent over 1": `<table style="width:400px"><col style="width:900%"><col style="width:900%"><tr><td>a<td>b</table>`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, html, opts{css: tableDoc, limits: func(l *budget.Limits) { l[budget.Nodes] = 1 << 20 }})
			start := time.Now()
			pages := r.all(t)
			if d := time.Since(start); d > time.Minute {
				t.Errorf("took %v", d)
			}
			assertWorkBound(t, r, html, 20)
			for _, td := range fragsOf(pages, "td") {
				if math.IsNaN(td.rect.W) || math.IsInf(td.rect.W, 0) || td.rect.W < 0 {
					t.Fatalf("cell %v", td.rect)
				}
			}
			if len(r.pag.l.tables) > 0 {
				t.Fatal("table layout kept")
			}
		})
	}
}

func TestTableColumnRuns(t *testing.T) {
	r := newRun(t, `<table><colgroup><col span=3 style="width:10px"><col></colgroup><colgroup span=2 style="width:5%"></colgroup><col span=2000></table>`, opts{css: tableDoc})
	var tb *box.Box
	var find func(b *box.Box)
	find = func(b *box.Box) {
		if b.Tag.String() == "table" {
			tb = b
		}
		for _, c := range b.Children {
			find(c)
		}
	}
	find(r.root)
	runs, total := columnRuns(tb)
	if total != maxTableCols {
		t.Fatalf("total %d, runs %+v", total, runs)
	}
	want := []colRun{{start: 0, n: 3}, {start: 3, n: 1}, {start: 4, n: 2}, {start: 6, n: 994}}
	if len(runs) != len(want) {
		t.Fatalf("runs %+v", runs)
	}
	for i, w := range want {
		if runs[i].start != w.start || runs[i].n != w.n {
			t.Errorf("run %d: %+v, want start %d n %d", i, runs[i], w.start, w.n)
		}
	}
	if !runs[0].sized() || runs[1].sized() || !runs[2].sized() {
		t.Errorf("sized flags %+v", runs)
	}
}

func TestTableManyColumnTablesStayInBudget(t *testing.T) {
	html := strings.Repeat(`<table><col span=1000 style="width:1px"><tr><td>x</table>`, 5000)
	runtime.GC()
	stop, peak := make(chan struct{}), make(chan uint64, 1)
	go heapMax(stop, peak)
	r := newRun(t, html, opts{css: tableDoc})
	r.all(t)
	close(stop)
	if heap := <-peak; heap > 128<<20 && !raceEnabled {
		t.Errorf("heap max %d MiB", heap>>20)
	}
}
