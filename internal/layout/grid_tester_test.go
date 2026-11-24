package layout

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func gridDocs() map[string]string {
	item := func(st, inner string) string { return `<div style="` + st + `">` + inner + `</div>` }
	grid := func(st string, kids ...string) string {
		return `<div style="display:grid;` + st + `">` + strings.Join(kids, "") + `</div>`
	}
	lines := strings.Repeat("line<br>", 200)
	many := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = item("", fmt.Sprintf("i%d word", i))
		}
		return out
	}
	docs := map[string]string{
		"empty":                    grid(""),
		"whitespace only":          grid("", " \n "),
		"text only":                grid("", "just text here"),
		"one auto column":          grid("", many(5)...),
		"fr below 1":               grid("grid-template-columns:0.2fr 0.3fr;width:400px", many(4)...),
		"fr zero":                  grid("grid-template-columns:0fr 1fr;width:400px", many(4)...),
		"fr huge":                  grid("grid-template-columns:1e9fr 1fr;width:400px", many(4)...),
		"fr negative":              grid("grid-template-columns:-1fr 1fr;width:400px", many(4)...),
		"percent over 100":         grid("grid-template-columns:80% 80%;width:400px", many(4)...),
		"percent and px conflict":  grid("grid-template-columns:50% 400px 1fr;width:300px", many(6)...),
		"px beyond the container":  grid("grid-template-columns:1e9px 1e9px;width:300px", many(4)...),
		"gap larger than the box":  grid("grid-template-columns:1fr 1fr 1fr;width:100px;gap:1e9px", many(6)...),
		"negative gap":             grid("grid-template-columns:1fr 1fr;width:300px;gap:-50px", many(4)...),
		"row gap huge":             grid("grid-template-columns:1fr;row-gap:1e9px", many(3)...),
		"auto min max content":     grid("grid-template-columns:auto min-content max-content 1fr;width:400px", many(8)...),
		"items wider than tracks":  grid("grid-template-columns:50px 50px;width:100px", item("width:400px", "wide wide"), item("min-width:300px", "w"), item("", "c"), item("", "d")),
		"width over max-width":     grid("grid-template-columns:1fr 1fr", item("width:200px;max-width:10px", "capped"), item("max-width:0", "none")),
		"items with margins":       grid("grid-template-columns:1fr 1fr;width:200px", item("margin:-30px 1e9px;padding:1e6px", "m"), item("margin:0 -1e9px", "n")),
		"one column, many items":   grid("", many(300)...),
		"repeat 1000 columns":      grid("grid-template-columns:repeat(1000,1fr)", many(1000)...),
		"repeat 1001 columns":      grid("grid-template-columns:repeat(1001,1fr)", many(1001)...),
		"repeat 100000 columns":    grid("grid-template-columns:repeat(100000,1fr)", many(50)...),
		"repeat nested sizes":      grid("grid-template-columns:repeat(50,1px 1fr 3%)", many(200)...),
		"more items than columns":  grid("grid-template-columns:1fr 1fr 1fr", many(10)...),
		"fewer items than columns": grid("grid-template-columns:1fr 1fr 1fr 1fr 1fr", many(2)...),
		"align start end center":   grid("grid-template-columns:1fr 1fr 1fr;align-items:end", item("", lines), item("", "short"), item("", "mid")),
		"align center":             grid("grid-template-columns:1fr 1fr;align-items:center", item("", lines), item("", "short")),
		"stretch tall item":        grid("grid-template-columns:1fr 1fr", item("", lines), item("background:#eee", "short")),
		"rows taller than a page":  grid("grid-template-columns:1fr 1fr", item("", lines), item("", lines), item("", lines), item("", "z")),
		"one row three pages":      grid("grid-template-columns:1fr", item("", lines+lines+lines), item("", "next")),
		"grid in flex":             `<div style="display:flex">` + grid("flex:1;grid-template-columns:1fr 1fr", many(4)...) + item("flex:1", "side") + `</div>`,
		"flex in grid":             grid("grid-template-columns:1fr 1fr", `<div style="display:flex"><span>a</span><span>b</span></div>`, item("", "x")),
		"grid in grid":             grid("grid-template-columns:1fr 2fr", grid("grid-template-columns:1fr 1fr", many(3)...), grid("", many(3)...)),
		"grid in a table cell":     `<table><tr><td>` + grid("grid-template-columns:1fr 1fr", many(6)...) + `</td><td>` + grid("", many(3)...) + `</td></tr></table>`,
		"grid in a narrow cell":    `<table style="width:80px"><tr><td>` + grid("grid-template-columns:1fr 1fr", item("", "aaa bbb"), item("", "ccc ddd")) + `</td></tr></table>`,
		"grid in an atom":          `<p>a <span style="display:inline-block">` + grid("grid-template-columns:1fr 1fr", item("", "in"), item("", "atom")) + `</span> b</p>`,
		"tall grid in an atom":     `<p>a <span style="display:inline-block">` + grid("grid-template-columns:1fr 1fr", item("", lines), item("", lines)) + `</span> b</p>`,
		"items with atoms":         grid("grid-template-columns:1fr 1fr;width:200px", `<span style="display:inline-block;width:50%">a</span>`, `<span style="display:inline-block">b b b</span>`),
		"items with a table":       grid("grid-template-columns:1fr 1fr", item("", `<table><tr><td>a</td><td>b</td></tr></table>`), item("", "x")),
		"items with links":         grid("grid-template-columns:1fr 1fr", item("", `<a href="https://e.test/">link</a>`), item("", "x")),
		"items with page breaks":   grid("grid-template-columns:1fr 1fr", item("break-before:page", "a"), item("break-after:page", "b"), item("break-inside:avoid", "c"), item("", "d")),
		"grid in avoid":            `<div style="break-inside:avoid">` + grid("grid-template-columns:1fr 1fr", item("", lines), item("", "x")) + `</div>`,
		"avoid between grids":      grid("break-after:avoid", item("", "a")) + grid("break-before:avoid;break-after:page", item("", "b")) + grid("", item("", "c")),
		"opacity and radius":       grid("opacity:.5;border-radius:50%;border:3px solid;grid-template-columns:1fr", item("opacity:.5;background:#f00", "o")),
		"overflow hidden":          grid("overflow:hidden;width:50px;grid-template-columns:1fr", item("width:200px", "clipped text here")),
		"nowrap items":             grid("width:100px;grid-template-columns:1fr 1fr", item("white-space:nowrap", strings.Repeat("longword ", 20)), item("", "b")),
		"break-all items":          grid("width:50px;grid-template-columns:1fr 1fr", item("overflow-wrap:anywhere", strings.Repeat("x", 300)), item("word-break:break-all", strings.Repeat("y", 300))),
		"padding and border":       grid("padding:1e6px;border:1e6px solid;grid-template-columns:1fr 1fr", many(4)...),
		"letter-spacing":           grid("grid-template-columns:1fr 1fr;width:200px", item("letter-spacing:1e6px", "w"), item("", "x")),
	}
	for _, d := range []int{50, 240} {
		docs[fmt.Sprintf("nested %d", d)] = strings.Repeat(`<div style="display:grid;grid-template-columns:1fr"><div>t `, d) + "leaf" + strings.Repeat("</div></div>", d)
		docs[fmt.Sprintf("nested %d two columns", d)] = strings.Repeat(`<div style="display:grid;grid-template-columns:1fr 1fr"><div>t `, d) + "leaf" + strings.Repeat("</div></div>", d)
	}
	return docs
}

// Hostile grids end with a limit or with valid, finite pages whose text is all
// there and inside the page.
func TestM205GridHostileDocuments(t *testing.T) {
	for name, body := range gridDocs() {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			r := newRun(t, body, opts{extra: smallPage, css: flat})
			pages, err := r.drain(t)
			if err != nil {
				t.Skipf("stopped by a limit: %v", err)
			}
			assertFinite(t, pages)
			assertTextKept(t, body, pages)
			assertWorkBound(t, r, body, 50)
			if d := time.Since(start); d > time.Minute {
				t.Errorf("took %v", d)
			}
		})
	}
}

// 10 000 items in repeat(1000, 1fr) and in repeat(4, 1fr), 1 000 columns with
// 1 000 items: bounded memory, no limit error, no text lost.
func TestM205GridManyItems(t *testing.T) {
	item := func(i int) string { return fmt.Sprintf(`<div>item %d</div>`, i) }
	for _, tc := range []struct {
		name, cols string
		n          int
	}{
		{"10000 items in 4 columns", "repeat(4,1fr)", 10000},
		{"10000 items in 1000 columns", "repeat(1000,1fr)", 10000},
		{"1000 items in 1000 columns", "repeat(1000,1fr)", 1000},
		{"10000 items in 100000 columns", "repeat(100000,1fr)", 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString(`<div style="display:grid;grid-template-columns:` + tc.cols + `">`)
			for i := range tc.n {
				sb.WriteString(item(i))
			}
			sb.WriteString("</div>")
			body := sb.String()
			var elapsed time.Duration
			var work, glyphs int64
			var warned bool
			peak := peakHeap(func() {
				start := time.Now()
				r := newRun(t, body, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Boxes] = budget.Defaults()[budget.Boxes] }})
				pages, err := r.drain(t)
				elapsed = time.Since(start)
				work, glyphs = r.b.Used(budget.Work), r.b.Used(budget.Glyphs)
				ws, _ := r.b.Warnings()
				for _, w := range ws {
					if w.Code == budget.WarnClamped {
						warned = true
					}
				}
				if err != nil {
					t.Errorf("stopped: %v", err)
					return
				}
				assertFinite(t, pages)
				assertTextKept(t, body, pages)
			})
			t.Logf("%v, work %d, glyphs %d, peak heap %d MiB, clamp warning %v", elapsed, work, glyphs, peak>>20, warned)
			if limit := int64(200*tc.n + 20_000); work > limit {
				t.Errorf("%d work units for %d items, want at most %d (94 per item measured: linear, 2x margin)", work, tc.n, limit)
			}
			if peak > peakHeapCap() {
				t.Errorf("peak heap %d MiB", peak>>20)
			}
			if strings.Contains(tc.cols, "100000") && !warned {
				t.Error("repeat(100000) is not reported as clamped")
			}
			if elapsed > time.Minute {
				t.Errorf("took %v", elapsed)
			}
		})
	}
}

// A grid whose row is taller than a page is sliced over pages: the row is laid
// and shaped a constant number of times, not once per page.
func TestM205TallRowIsNotReshapedPerPage(t *testing.T) {
	measure := func(lines int) (pages int, glyphs, work int64) {
		body := `<div style="display:grid;grid-template-columns:1fr 1fr"><div>` + strings.Repeat("line<br>", lines) + `</div><div>x</div></div>`
		r := newRun(t, body, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Glyphs] = 1 << 26 }})
		ps, err := r.drain(t)
		if err != nil {
			t.Fatalf("stopped on a limit: %v", err)
		}
		return len(ps), r.b.Used(budget.Glyphs), r.b.Used(budget.Work)
	}
	p1, g1, w1 := measure(300)
	p3, g3, w3 := measure(900)
	p6, g6, w6 := measure(1800)
	t.Logf("300 lines: %d pages, %d glyphs, work %d; 900: %d, %d, %d; 1800: %d, %d, %d", p1, g1, w1, p3, g3, w3, p6, g6, w6)
	if p3 < 3 || p6 < 6 {
		t.Fatalf("the rows are not taller than 3 and 6 pages: %d and %d pages", p3, p6)
	}
	// Glyphs must grow with the text (x3, x6), not with text times pages.
	if float64(g3) > 3.3*float64(g1) || float64(g6) > 6.6*float64(g1) {
		t.Errorf("glyphs %d, %d, %d for 300, 900, 1800 lines: shaping grows faster than the text", g1, g3, g6)
	}
	if float64(w6) > 6.6*float64(w1)+1000 {
		t.Errorf("work %d for 1800 lines against %d for 300", w6, w1)
	}
}

// The same grid with 1 column holds 400 and 2000 paragraphs, with a block twin:
// page counts within 5 %, glyphs at most twice the twin.
func TestM205TallGridTwinOfParagraphs(t *testing.T) {
	for _, n := range []int{400, 2000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var paras strings.Builder
			for i := range n {
				fmt.Fprintf(&paras, "<p>paragraph %d %s</p>", i, strings.Repeat("lorem ipsum dolor sit amet ", 12))
			}
			measure := func(body string) (pages int, glyphs, work int64, err error) {
				r := newRun(t, body, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) {
					l[budget.Pages] = 5000
					l[budget.Glyphs] = budget.Defaults()[budget.Glyphs]
					l[budget.Work] = budget.Defaults()[budget.Work]
				}})
				ps, err := r.drain(t)
				return len(ps), r.b.Used(budget.Glyphs), r.b.Used(budget.Work), err
			}
			pp, pg, pw, err := measure(paras.String())
			if err != nil {
				t.Fatal(err)
			}
			gp, gg, gw, err := measure(`<div style="display:grid">` + paras.String() + `</div>`)
			t.Logf("block: %d pages, %d glyphs, work %d; grid: %d pages, %d glyphs, work %d, err %v", pp, pg, pw, gp, gg, gw, err)
			if err != nil {
				t.Fatalf("the grid stopped on a limit after %d pages: %v", gp, err)
			}
			if d := float64(gp - pp); math.Abs(d) > 0.05*float64(pp) {
				t.Errorf("%d pages in the grid, %d in block", gp, pp)
			}
			if float64(gg) > 2.1*float64(pg) {
				t.Errorf("%d glyphs in the grid, %d in block (more than 2.1x)", gg, pg)
			}
			if float64(gw) > 2.1*float64(pw) {
				t.Errorf("work %d in the grid, %d in block (> 2.1x)", gw, pw)
			}
		})
	}
}

// A percentage padding or margin of a grid item is relative to its grid area:
// 5% of a 200px column is 10px.
func TestM205PercentagesResolveAgainstTheColumn(t *testing.T) {
	body := `<div style="display:grid;grid-template-columns:repeat(4,200px);width:1000px">` +
		`<div id=a style="padding:5%">a</div><div id=b style="margin-left:5%">b</div><div id=c style="padding:5%;width:50%">c</div><div id=d>d</div></div>`
	pages := newRun(t, body, opts{extra: smallPage, css: flat}).all(t)
	get := func(id string) *frag {
		var found *frag
		for _, pg := range pages {
			walk(pg.root, func(f *frag) {
				if f.box.ID == id && found == nil {
					found = f
				}
			})
		}
		if found == nil {
			t.Fatalf("no #%s", id)
		}
		return found
	}
	a := get("a")
	for i, side := range []string{"top", "right", "bottom", "left"} {
		if !near(a.padding[i], 10) {
			t.Errorf("padding-%s of #a = %v, want 10", side, a.padding[i])
		}
	}
	if !near(a.rect.W, 200) {
		t.Errorf("#a is %vpx wide, want the 200px column", a.rect.W)
	}
	b := get("b")
	if !near(b.rect.X-get("a").rect.X, 200+10) {
		t.Errorf("#b starts %vpx after #a, want 210 (column + 5%% margin of 10px)", b.rect.X-get("a").rect.X)
	}
	c := get("c")
	if !near(c.rect.W, 100+20) && !near(c.rect.W, 100) {
		t.Errorf("#c is %vpx wide: 50%% of the 200px column is 100px of content (or border box)", c.rect.W)
	}
}
