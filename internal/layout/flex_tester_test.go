package layout

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func flexDocs() map[string]string {
	item := func(st, inner string) string { return `<div style="` + st + `">` + inner + `</div>` }
	flex := func(st string, kids ...string) string {
		return `<div style="display:flex;` + st + `">` + strings.Join(kids, "") + `</div>`
	}
	lines := strings.Repeat("line<br>", 200)
	many := func(n int, st string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = item(st, fmt.Sprintf("i%d word", i))
		}
		return out
	}
	docs := map[string]string{
		"empty":                       flex(""),
		"whitespace only":             flex("", " \n "),
		"text only":                   flex("", "just text here"),
		"mixed text and spans":        flex("", "text <span>span</span> <b>bold</b> tail"),
		"grow 1e6":                    flex("width:400px", item("flex:1000000", "a a a"), item("flex:1e6 1e6", "b"), item("flex-grow:99999999999", "c")),
		"shrink 1e6":                  flex("width:100px", item("flex:0 1000000 300px", "aaa bbb"), item("flex:0 1e6 300px", "ccc ddd")),
		"grow and shrink 0":           flex("width:100px", item("flex:0 0 300px", "aaa bbb ccc"), item("flex:0 0 300px", "ddd")),
		"huge basis":                  flex("width:100px", item("flex:1 1 1e9px", "huge"), item("flex:1 1 1e12px", "huger")),
		"percent basis":               flex("width:300px", item("flex:0 0 50%", "half"), item("flex:0 0 80%", "most"), item("flex-basis:250%", "x")),
		"basis 0 grow 0":              flex("width:300px", item("flex:0 0 0", "zero width"), item("flex:0 1 0px", "z")),
		"width over max-width":        flex("width:300px", item("width:200px;max-width:10px", "wide but capped"), item("max-width:0", "none")),
		"max-width contradicts basis": flex("width:300px", item("flex:1 1 400px;max-width:50px", "m"), item("flex:1 1 10px;width:500px;max-width:20px", "n")),
		"huge gap":                    flex("width:300px;gap:1e9px", item("", "a"), item("", "b"), item("", "c")),
		"huge row gap wrap":           flex("width:300px;flex-wrap:wrap;row-gap:1e9px", item("width:200px", "a"), item("width:200px", "b")),
		"negative free space justify": flex("width:100px;justify-content:center", item("width:200px", "wide one"), item("width:200px", "wide two")),
		"wrap wider items":            flex("width:100px;flex-wrap:wrap", item("width:300px", "w1"), item("width:300px", "w2"), item("flex:0 0 250px", "w3")),
		"wrap grow":                   flex("width:300px;flex-wrap:wrap", item("flex:1 1 200px", "a"), item("flex:2 1 200px", "b"), item("flex:1 1 200px", "c")),
		"wrap 500 items":              flex("width:300px;flex-wrap:wrap;gap:3px", many(500, "width:70px")...),
		"nowrap 500 items":            flex("width:300px", many(500, "")...),
		"lines taller than a page":    flex("flex-wrap:wrap", item("width:100%", lines), item("width:100%", lines), item("width:100%", "z")),
		"one item taller than a page": flex("", item("", lines), item("", "short")),
		"stretch with tall sibling":   flex("align-items:stretch", item("", lines), item("background:#eee", "short"), item("", "mid")),
		"align center tall":           flex("align-items:center", item("", lines), item("", "short")),
		"align end tall":              flex("align-items:flex-end", item("", lines), item("", "short")),
		"justify space-between":       flex("justify-content:space-between", item("", "a"), item("", "b"), item("", "c")),
		"justify space-around one":    flex("justify-content:space-around", item("", "only")),
		"justify space-evenly 2":      flex("justify-content:space-evenly", item("", "a"), item("", "b")),
		"nowrap items in flex":        flex("width:100px", item("white-space:nowrap", strings.Repeat("longword ", 20)), item("", "b")),
		"anywhere break-all items":    flex("width:50px", item("overflow-wrap:anywhere", strings.Repeat("x", 300)), item("word-break:break-all", strings.Repeat("y", 300))),
		"inline-block items":          flex("width:200px", `<span style="display:inline-block">a a a</span>`, `<span style="display:inline-block;width:50%">b</span>`),
		"item with a table":           flex("", item("", `<table><tr><td>a</td><td>b</td></tr></table>`), item("", "x")),
		"item with a link":            flex("", item("", `<a href="https://e.test/">link</a>`), item("", "x")),
		"margins and padding":         flex("width:200px", item("margin:-30px 10px;padding:1e6px", "m"), item("margin:0 1e9px", "n")),
		"letter-spacing":              flex("width:200px", item("letter-spacing:1e6px", "w"), item("", "x")),
		"flex in a table cell":        `<table><tr><td>` + flex("", item("", "a b"), item("", "c")) + `</td><td>` + flex("flex-wrap:wrap", many(20, "width:60px")...) + `</td></tr></table>`,
		"flex in a narrow table cell": `<table style="width:80px"><tr><td>` + flex("", item("", "aaa bbb"), item("flex:1", "ccc ddd")) + `</td></tr></table>`,
		"flex in an atom":             `<p>a <span style="display:inline-block">` + flex("", item("", "in"), item("flex:1", "atom")) + `</span> b</p>`,
		"flex in an atom, wrapping":   `<p>a <span style="display:inline-block;width:100px">` + flex("flex-wrap:wrap", many(30, "width:40px")...) + `</span> b</p>`,
		"tall flex in an atom":        `<p>a <span style="display:inline-block">` + flex("", item("", lines), item("", lines)) + `</span> b</p>`,
		"flex in avoid":               `<div style="break-inside:avoid">` + flex("", item("", lines), item("", "x")) + `</div>`,
		"avoid between flexes":        flex("break-after:avoid", item("", "a")) + flex("break-before:avoid;break-after:page", item("", "b")) + flex("", item("", "c")),
		"items with page breaks":      flex("", item("break-before:page", "a"), item("break-after:page", "b"), item("break-inside:avoid", "c")),
		"flex in flex":                flex("", flex("flex:1", item("flex:1", "x"), item("", "y")), flex("flex:2;flex-wrap:wrap", many(10, "width:50px")...)),
		"flex with grid child":        flex("", `<div style="display:grid;grid-template-columns:1fr 1fr"><span>g1</span><span>g2</span></div>`, item("", "z")),
		"opacity and radius":          flex("opacity:.5;border-radius:50%;border:3px solid", item("opacity:.5;background:#f00", "o")),
		"overflow hidden":             flex("overflow:hidden;width:50px", item("width:200px", "clipped text here")),
	}
	for _, d := range []int{50, 240} {
		docs[fmt.Sprintf("nested %d", d)] = strings.Repeat(`<div style="display:flex"><div style="flex:1">t `, d) + "leaf" + strings.Repeat("</div></div>", d)
		docs[fmt.Sprintf("nested %d wrap", d)] = strings.Repeat(`<div style="display:flex;flex-wrap:wrap"><div style="width:60%">t `, d) + "leaf" + strings.Repeat("</div></div>", d)
		docs[fmt.Sprintf("nested %d column of atoms", d)] = strings.Repeat(`<div style="display:flex"><span style="display:inline-block">t `, d) + "leaf" + strings.Repeat("</span></div>", d)
	}
	return docs
}

func TestM204FlexHostileDocuments(t *testing.T) {
	for name, body := range flexDocs() {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			r := newRun(t, body, opts{extra: smallPage, css: flat})
			pages, err := r.drain(t)
			if err != nil {
				t.Skipf("stopped by a limit: %v", err)
			}
			assertFinite(t, pages)
			assertTextKept(t, body, pages)
			if d := time.Since(start); d > 10*time.Second && !raceEnabled {
				t.Errorf("took %v", d)
			}
		})
	}
}

func TestM204TenThousandItems(t *testing.T) {
	item := func(i int) string { return fmt.Sprintf(`<div style="flex:1 1 auto">item %d</div>`, i) }
	var sb strings.Builder
	sb.WriteString(`<div style="display:flex;flex-wrap:wrap;width:700px">`)
	for i := range 10000 {
		sb.WriteString(item(i))
	}
	sb.WriteString("</div>")
	body := sb.String()
	var elapsed time.Duration
	var work, glyphs int64
	peak := peakHeap(func() {
		start := time.Now()
		r := newRun(t, body, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Boxes] = budget.Defaults()[budget.Boxes] }})
		pages, err := r.drain(t)
		elapsed = time.Since(start)
		work, glyphs = r.b.Used(budget.Work), r.b.Used(budget.Glyphs)
		if err != nil {
			t.Logf("stopped: %v", err)
			return
		}
		assertFinite(t, pages)
		assertTextKept(t, body, pages)
	})
	t.Logf("10 000 items: %v, work %d, glyphs %d, peak heap %d MiB", elapsed, work, glyphs, peak>>20)
	if peak > 256<<20 {
		t.Errorf("peak heap %d MiB", peak>>20)
	}
	if elapsed > 5*time.Second && !raceEnabled {
		t.Errorf("took %v", elapsed)
	}
}

// Flexbox §9.7 on round numbers: the items are the <i> elements of a 300 px
// wide container with no padding, text of one short word.
func TestM204FlexDistributionFromTheSpec(t *testing.T) {
	tests := []struct {
		name, css string
		items     []string
		x, w      []float64
	}{
		{"flex:1 each", "", []string{"flex:1", "flex:1", "flex:1"}, []float64{0, 100, 200}, []float64{100, 100, 100}},
		{"1 and 2 from basis 0", "", []string{"flex:1", "flex:2"}, []float64{0, 100}, []float64{100, 200}},
		{"grow from fixed basis", "", []string{"flex:1 1 100px", "flex:3 1 100px"}, []float64{0, 125}, []float64{125, 175}},
		{"shrink equal", "", []string{"flex:0 1 200px", "flex:0 1 200px"}, []float64{0, 150}, []float64{150, 150}},
		{"shrink is weighted by basis", "", []string{"flex:0 1 300px", "flex:0 1 100px"}, []float64{0, 225}, []float64{225, 75}},
		{"no shrink", "", []string{"flex:none;width:200px", "flex:none;width:200px"}, []float64{0, 200}, []float64{200, 200}},
		{"max-width freezes", "", []string{"flex:1;max-width:50px", "flex:1"}, []float64{0, 50}, []float64{50, 250}},
		{"justify end", "justify-content:flex-end", []string{"width:50px", "width:50px"}, []float64{200, 250}, []float64{50, 50}},
		{"justify center", "justify-content:center", []string{"width:50px", "width:50px"}, []float64{100, 150}, []float64{50, 50}},
		{"justify space-between", "justify-content:space-between", []string{"width:50px", "width:50px", "width:50px"}, []float64{0, 125, 250}, []float64{50, 50, 50}},
		{"justify space-around", "justify-content:space-around", []string{"width:50px", "width:50px"}, []float64{50, 200}, []float64{50, 50}},
		{"justify space-evenly", "justify-content:space-evenly", []string{"width:50px", "width:50px"}, []float64{66.6667, 183.3333}, []float64{50, 50}},
		{"gap", "gap:10px", []string{"width:50px", "width:50px", "width:50px"}, []float64{0, 60, 120}, []float64{50, 50, 50}},
		{"gap with grow", "column-gap:20px", []string{"flex:1", "flex:1"}, []float64{0, 160}, []float64{140, 140}},
		{"negative free space starts at 0", "justify-content:center", []string{"flex:none;width:200px", "flex:none;width:200px"}, []float64{0, 200}, []float64{200, 200}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString(`<div style="display:flex;width:300px;` + tt.css + `">`)
			for _, st := range tt.items {
				sb.WriteString(`<i style="display:block;` + st + `">w</i>`)
			}
			sb.WriteString("</div>")
			pages := newRun(t, sb.String(), opts{extra: smallPage, css: flat}).all(t)
			got := frags(pages, "i")
			if len(got) != len(tt.items) {
				t.Fatalf("%d items", len(got))
			}
			for i, f := range got {
				if !nearTol(f.rect.X, tt.x[i], 0.01) || !nearTol(f.rect.W, tt.w[i], 0.01) {
					t.Errorf("item %d: x %.3f w %.3f, want %.3f %.3f", i, f.rect.X, f.rect.W, tt.x[i], tt.w[i])
				}
			}
		})
	}
}

func nearTol(a, b, tol float64) bool { return a-b < tol && b-a < tol }

// A flex container taller than a page is a monolithic stripe sliced with
// cutFrag: it is laid out and shaped a constant number of times (the item texts
// twice: intrinsic size and layout), not once per page.
func TestM204TallFlexIsNotReshapedPerPage(t *testing.T) {
	doc := func(n int) (plain, wrapped string) {
		var paras strings.Builder
		for i := range n {
			fmt.Fprintf(&paras, "<p>paragraph %d %s</p>", i, strings.Repeat("lorem ipsum dolor sit amet ", 12))
		}
		return paras.String(), `<div style="display:flex"><div style="flex:1">` + paras.String() + `</div></div>`
	}
	type m struct {
		pages        int
		glyphs, work int64
	}
	measure := func(body string) m {
		r := newRun(t, body, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Glyphs] = 1 << 26 }})
		ps, err := r.drain(t)
		if err != nil {
			t.Fatalf("stopped on a limit: %v", err)
		}
		return m{len(ps), r.b.Used(budget.Glyphs), r.b.Used(budget.Work)}
	}
	_, f200 := doc(200)
	p400, f400 := doc(400)
	plain, wrapped, wrapped200 := measure(p400), measure(f400), measure(f200)
	t.Logf("400 paragraphs plain %+v, flex wrapper %+v; 200 paragraphs flex wrapper %+v", plain, wrapped, wrapped200)
	if d := float64(wrapped.pages - plain.pages); math.Abs(d) > 0.05*float64(plain.pages) {
		t.Errorf("%d pages with the flex wrapper, %d without", wrapped.pages, plain.pages)
	}
	if float64(wrapped.glyphs) > 2.1*float64(plain.glyphs) {
		t.Errorf("%d glyphs with the flex wrapper, %d without: more than the two shapings of the design", wrapped.glyphs, plain.glyphs)
	}
	if float64(wrapped.glyphs) > 2.1*float64(wrapped200.glyphs) {
		t.Errorf("glyphs %d for 400 paragraphs against %d for 200: shaping grows with the number of pages", wrapped.glyphs, wrapped200.glyphs)
	}
	if float64(wrapped.work) > 2.1*float64(wrapped200.work) {
		t.Errorf("work %d for 400 paragraphs against %d for 200", wrapped.work, wrapped200.work)
	}
}

// The same invariant at 2000 paragraphs, with the default Glyphs and Work
// limits (only the page limit is raised): a flex wrapper must neither stop on a
// limit nor cost more than twice its block twin.
func TestM204TallFlexTwinOf2000Paragraphs(t *testing.T) {
	var paras strings.Builder
	for i := range 2000 {
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
	fp, fg, fw, err := measure(`<div style="display:flex"><div style="flex:1">` + paras.String() + `</div></div>`)
	t.Logf("block: %d pages, %d glyphs, work %d; flex wrapper: %d pages, %d glyphs, work %d, err %v", pp, pg, pw, fp, fg, fw, err)
	if err != nil {
		t.Fatalf("the flex wrapper stopped on a limit after %d pages: %v", fp, err)
	}
	if d := float64(fp - pp); math.Abs(d) > 0.05*float64(pp) {
		t.Errorf("%d pages with the flex wrapper, %d in block", fp, pp)
	}
	if float64(fg) > 2.1*float64(pg) {
		t.Errorf("%d glyphs with the flex wrapper, %d in block (the design allows two shapings)", fg, pg)
	}
	if float64(fw) > 2*float64(pw) {
		t.Errorf("work %d with the flex wrapper, %d in block (> 2x)", fw, pw)
	}
}
