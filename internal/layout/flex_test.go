package layout

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
)

// flexRun lays the container #c out directly (the dispatch from block layout
// is separate) in a content box of width w.
func flexRun(t *testing.T, container, items string, w float64) ([]stripe, float64) {
	t.Helper()
	r := newRun(t, `<div id=c style="display:flex;`+container+`">`+items+`</div>`, opts{extra: smallPage, css: flat})
	return flexStripesOf(t, r.pag.l, boxByID(t, r.root, "c"), cbox{x: 10, w: w})
}

// flexStripesOf lays every line of the container c out, as flex() does one by one.
func flexStripesOf(tb testing.TB, l *layouter, c *box.Box, inner cbox) ([]stripe, float64) {
	tb.Helper()
	p, err := l.planFlex(c, inner.w)
	if err != nil {
		tb.Fatal(err)
	}
	var out []stripe
	for _, line := range p.lines {
		s, err := l.layLine(line, inner, p.colGap, c.Style, 0)
		if err != nil {
			tb.Fatal(err)
		}
		out = append(out, s)
	}
	return out, p.rowGap
}

func item(style, content string) string {
	return `<div style="` + style + `">` + content + `</div>`
}

func xs(s stripe) []float64 {
	var out []float64
	for _, f := range s.frags {
		out = append(out, f.rect.X)
	}
	return out
}

func ws(s stripe) []float64 {
	var out []float64
	for _, f := range s.frags {
		out = append(out, f.rect.W)
	}
	return out
}

func sameFloats(a, b []float64) bool {
	return slices.EqualFunc(a, b, func(x, y float64) bool { return math.Abs(x-y) <= 0.01 })
}

func TestFlexGrowAndShrink(t *testing.T) {
	for _, tt := range []struct {
		name, container, items string
		want                   []float64
	}{
		{"flex:1 shares equally", "", strings.Repeat(item("flex:1", "a"), 3), []float64{100, 100, 100}},
		{"grow ratio", "", item("flex:1", "a") + item("flex:2", "a"), []float64{100, 200}},
		{"grow from a basis", "", item("flex:1 1 100px", "a") + item("flex:1 1 50px", "a"), []float64{175, 125}},
		{"grow stops at max-width", "", item("flex:1;max-width:80px", "a") + item("flex:1", "a"), []float64{80, 220}},
		{"flex:none keeps its width", "", item("flex:none;width:50px", "a") + item("flex:1", "a"), []float64{50, 250}},
		{"shrink equal bases", "", strings.Repeat(item("width:200px", "a"), 2), []float64{150, 150}},
		{"shrink scaled by the base", "", item("width:300px", "a") + item("width:100px", "a"), []float64{225, 75}},
		{"flex:none overflows", "", strings.Repeat(item("flex:none;width:200px", "a"), 2), []float64{200, 200}},
		{"shrink 0 on one", "", item("width:200px;flex-shrink:0", "a") + item("width:200px", "a"), []float64{200, 100}},
		{"no grow without free space", "", item("width:200px;flex-grow:1", "a") + item("width:100px;flex-grow:1", "a"), []float64{200, 100}},
		{"sum of factors under 1", "", item("flex:0.25 1 0", "a"), []float64{75}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := flexRun(t, tt.container, tt.items, 300)
			if len(s) != 1 {
				t.Fatalf("%d lines", len(s))
			}
			got := ws(s[0])
			if !sameFloats(got, tt.want) {
				t.Errorf("widths %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlexAutoBasisIsMaxContent(t *testing.T) {
	s, _ := flexRun(t, "", item("", "aaa")+item("", "bbbbbb"), 300)
	if got, want := ws(s[0]), []float64{widthOf(t, "aaa", ""), widthOf(t, "bbbbbb", "")}; !sameFloats(got, want) {
		t.Errorf("auto basis: %v, want %v", got, want)
	}
}

func TestFlexItemsStopAtTheirMinContent(t *testing.T) {
	long := "WWWWWWWWWWWWWWWWWW"
	minContent := widthOf(t, long, "")
	s, _ := flexRun(t, "", item("width:400px", long)+item("width:200px", "a b"), 300)
	got := ws(s[0])
	if !near(got[0], minContent) || !near(got[1], 300-minContent) {
		t.Errorf("widths %v: the first item stops at its min-content %v and the second takes the rest", got, minContent)
	}
	s, _ = flexRun(t, "", item("flex:1 1 0", long)+item("flex:1 1 0", "a"), 300)
	if got := ws(s[0]); got[0] < minContent-0.01 {
		t.Errorf("a flex:1 item has an automatic minimum: %v", got)
	}
	s, _ = flexRun(t, "", item("width:200px", long)+item("width:200px", "a b"), 300)
	if got := ws(s[0]); !near(got[0], 200) {
		t.Errorf("a specified width below the min-content is the minimum: %v", got)
	}
}

func TestFlexWrap(t *testing.T) {
	items := strings.Repeat(item("width:120px;flex:none", "a"), 5)
	s, _ := flexRun(t, "flex-wrap:wrap", items, 300)
	if len(s) != 3 || len(s[0].frags) != 2 || len(s[2].frags) != 1 {
		t.Fatalf("%d lines", len(s))
	}
	s, _ = flexRun(t, "flex-wrap:wrap;column-gap:70px", items, 300)
	if len(s) != 5 {
		t.Errorf("a gap that leaves room for one item only: %d lines", len(s))
	}
	s, _ = flexRun(t, "", items, 300)
	if len(s) != 1 || len(s[0].frags) != 5 {
		t.Errorf("nowrap is one line: %d", len(s))
	}
	s, _ = flexRun(t, "flex-wrap:wrap", item("width:500px;flex:none", "a")+item("width:10px", "a"), 300)
	if len(s) != 2 || len(s[0].frags) != 1 {
		t.Errorf("an item wider than the container fills a line: %d lines", len(s))
	}
	s, _ = flexRun(t, "flex-wrap:wrap", item("flex:1 1 200px", "a")+item("flex:1 1 200px", "a"), 300)
	if len(s) != 2 || !near(s[0].frags[0].rect.W, 300) {
		t.Errorf("wrapped items grow on their own line: %d lines", len(s))
	}
}

func TestFlexJustifyContent(t *testing.T) {
	items := strings.Repeat(item("width:50px;flex:none", "a"), 2)
	for _, tt := range []struct {
		j    string
		want []float64
	}{
		{"flex-start", []float64{10, 60}},
		{"normal", []float64{10, 60}},
		{"end", []float64{210, 260}},
		{"center", []float64{110, 160}},
		{"space-between", []float64{10, 260}},
		{"space-around", []float64{60, 210}},
		{"space-evenly", []float64{10 + 200.0/3, 10 + 200.0/3*2 + 50}},
	} {
		t.Run(tt.j, func(t *testing.T) {
			s, _ := flexRun(t, "justify-content:"+tt.j, items, 300)
			got := xs(s[0])
			if !sameFloats(got, tt.want) {
				t.Errorf("x %v, want %v", got, tt.want)
			}
		})
	}
	s, _ := flexRun(t, "justify-content:center", item("width:200px;flex:none", "a")+item("width:200px;flex:none", "a"), 300)
	if got := xs(s[0]); !sameFloats(got, []float64{10, 210}) {
		t.Errorf("negative free space starts at the start: %v", got)
	}
	s, _ = flexRun(t, "justify-content:space-between", item("width:50px", "a"), 300)
	if got := xs(s[0]); !sameFloats(got, []float64{10}) {
		t.Errorf("one item with space-between: %v", got)
	}
}

func TestFlexAlignItems(t *testing.T) {
	items := item("flex:none;width:50px;margin:2px", "a<br>b<br>c") + item("flex:none;width:50px;margin:2px;background:red", "a")
	s, _ := flexRun(t, "align-items:stretch", items, 300)
	line := s[0].h
	tall := s[0].frags[0].rect.H
	if !near(line, tall+4) || !near(s[0].frags[1].rect.H, tall) || !near(s[0].frags[1].rect.Y, 2) {
		t.Errorf("stretch: line %v, heights %v %v, y %v", line, tall, s[0].frags[1].rect.H, s[0].frags[1].rect.Y)
	}
	for _, tt := range []struct {
		align string
		y     func(line, h float64) float64
	}{
		{"flex-start", func(_, _ float64) float64 { return 2 }},
		{"start", func(_, _ float64) float64 { return 2 }},
		{"end", func(line, h float64) float64 { return line - 2 - h }},
		{"flex-end", func(line, h float64) float64 { return line - 2 - h }},
		{"center", func(line, h float64) float64 { return (line-4-h)/2 + 2 }},
	} {
		t.Run(tt.align, func(t *testing.T) {
			s, _ := flexRun(t, "align-items:"+tt.align, items, 300)
			small := s[0].frags[1]
			if !near(s[0].frags[0].rect.H, tall) || !near(small.rect.H, tall/3) {
				t.Errorf("heights %v %v: only stretch changes them", s[0].frags[0].rect.H, small.rect.H)
			}
			if !near(small.rect.Y, tt.y(s[0].h, small.rect.H)) {
				t.Errorf("y %v, want %v", small.rect.Y, tt.y(s[0].h, small.rect.H))
			}
		})
	}
}

func TestFlexGaps(t *testing.T) {
	items := strings.Repeat(item("width:50px;flex:none", "a"), 4)
	s, row := flexRun(t, "flex-wrap:wrap;column-gap:10px;row-gap:7px;width:130px", items, 130)
	if row != 7 || len(s) != 2 {
		t.Fatalf("row gap %v, %d lines", row, len(s))
	}
	if got := xs(s[0]); !sameFloats(got, []float64{10, 70}) {
		t.Errorf("column gap: %v", got)
	}
	s, _ = flexRun(t, "column-gap:10px", strings.Repeat(item("flex:1", "a"), 3), 300)
	if got := ws(s[0]); !sameFloats(got, []float64{93.33, 93.33, 93.33}) {
		t.Errorf("gaps come out of the free space: %v", got)
	}
	s, _ = flexRun(t, "column-gap:10%", item("width:50px;flex:none", "a")+item("width:50px", "a"), 300)
	if got := xs(s[0]); !sameFloats(got, []float64{10, 90}) {
		t.Errorf("a percentage gap is of the container: %v", got)
	}
}

func TestFlexEmptyContainer(t *testing.T) {
	s, _ := flexRun(t, "", "", 300)
	if len(s) != 0 {
		t.Errorf("%d lines", len(s))
	}
}

func TestFlexItemMargins(t *testing.T) {
	s, _ := flexRun(t, "", item("flex:1;margin:0 10px 0 20px", "a")+item("flex:1;margin:0 5px", "a"), 300)
	if got := ws(s[0]); !sameFloats(got, []float64{130, 130}) {
		t.Errorf("margins come out of the free space: %v", got)
	}
	if got := xs(s[0]); !sameFloats(got, []float64{30, 175}) {
		t.Errorf("x %v", got)
	}
}

func flexTenThousand(tb testing.TB) (*run, *box.Box) {
	tb.Helper()
	html := `<div id=c style="display:flex;flex-wrap:wrap">` + strings.Repeat(`<div style="width:30px;flex:none">a</div>`, 10000) + `</div>`
	r := newRun(tb, html, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Boxes] = 1 << 20; l[budget.Glyphs] = 1 << 30 }})
	return r, boxByID(tb, r.root, "c")
}

// BenchmarkFlex10k lays 10 000 items out; subtract BenchmarkFlex10kShapeOnly,
// the shaping of their texts twice, for the layout alone.
func BenchmarkFlex10k(b *testing.B) {
	r, c := flexTenThousand(b)
	b.ReportAllocs()
	for b.Loop() {
		r.pag.l.sizes = nil
		flexStripesOf(b, r.pag.l, c, cbox{w: 300})
	}
}

func BenchmarkFlex10kShapeOnly(b *testing.B) {
	r, c := flexTenThousand(b)
	l := r.pag.l
	b.ReportAllocs()
	for b.Loop() {
		for _, it := range c.Children {
			txt := it.Children[0]
			for range 2 {
				if _, err := l.sh.Shape(txt.Text, fontSpec(txt.Style), nil); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

// Each item text is shaped twice: for the flex base size (max-content) and in
// the layout of the item.
func TestFlexShapesEachItemTextTwice(t *testing.T) {
	const items, glyphs = 200, 5
	html := `<div style="display:flex;flex-wrap:wrap">` + strings.Repeat(`<div style="flex:1">abcde</div>`, items) + `</div>`
	r := newRun(t, html, opts{extra: smallPage, css: flat})
	r.all(t)
	if got := r.b.Used(budget.Glyphs); got != 2*items*glyphs {
		t.Errorf("%d glyphs charged for %d items of %d: want exactly twice the text, %d", got, items, glyphs, 2*items*glyphs)
	}
}

// flexItems returns n items with ids fi0..fi<n-1>, the items flexItemsOn finds.
func flexItems(n int, attrs, content string) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<div id=fi%d %s>%s</div>`, i, attrs, content)
	}
	return sb.String()
}

func flexItemsOn(pages []*Page, pg int) []*frag {
	var out []*frag
	walk(pages[pg].root, func(f *frag) {
		if strings.HasPrefix(f.box.ID, "fi") {
			out = append(out, f)
		}
	})
	return out
}

func TestFlexContainerIsLaidOutInTheFlow(t *testing.T) {
	pages := newRun(t, `<div id=c style="display:flex;width:400px;padding:10px;border:2px solid;background:#eee"><div id=a style="flex:none;width:100px">aa</div><div id=b style="flex:1;margin-left:20px">bb</div></div><p id=n>next</p>`,
		opts{extra: smallPage, css: flat}).all(t)
	c, a, b, n := fragByID(t, pages, "c"), fragByID(t, pages, "a"), fragByID(t, pages, "b"), fragByID(t, pages, "n")
	if !near(a.rect.X, 12) || !near(a.rect.Y, 12) || !near(a.rect.W, 100) {
		t.Errorf("a at %+v", a.rect)
	}
	if !near(b.rect.X, 132) || !near(b.rect.W, 400-100-20) {
		t.Errorf("b at %+v", b.rect)
	}
	if !near(c.rect.H, a.rect.H+24) || !near(n.rect.Y, c.rect.H) {
		t.Errorf("container %v high, next at %v", c.rect.H, n.rect.Y)
	}
	if got := textOf(pages); !strings.Contains(got, "aa") || !strings.Contains(got, "bb") {
		t.Errorf("text %q", got)
	}
}

func TestFlexLinesBreakBetweenPages(t *testing.T) {
	// 30 lines of 100px on pages of 600px: 6 lines a page.
	html := `<div style="display:flex;flex-wrap:wrap;row-gap:0">` + flexItems(30, `class=b style="flex:none;width:100%"`, "x") + `</div>`
	pages := breakRun(t, html)
	if len(pages) != 5 {
		t.Fatalf("%d pages", len(pages))
	}
	for i := range pages {
		items := flexItemsOn(pages, i)
		if len(items) != 6 {
			t.Errorf("page %d: %d lines", i, len(items))
		}
		for _, it := range items {
			if !near(it.rect.H, 100) || !it.first || !it.last {
				t.Errorf("page %d: line %+v split", i, it.rect)
			}
		}
	}
	assertTextKept(t, html, pages)
}

func TestFlexLineTallerThanAPageIsSliced(t *testing.T) {
	// One 2000px line, 100px of content before it.
	html := `<div class=b>before</div><div style="display:flex"><div style="flex:none;width:300px;background:#ccc">` + strings.Repeat(`<div class=b>l</div>`, 20) + `</div><div style="flex:none;width:100px">side</div></div><div class=b>after</div>`
	pages := breakRun(t, html)
	if len(pages) < 4 {
		t.Fatalf("%d pages", len(pages))
	}
	got := textOf(pages)
	if strings.Count(got, "l") != 20 || !strings.Contains(got, "side") || !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("text lost: %q", got)
	}
	assertTextKept(t, html, pages)
	var grey int
	for _, pg := range pages {
		for _, it := range pg.Items {
			if _, ok := it.(display.FillRect); ok {
				grey++
			}
		}
	}
	if grey < 3 {
		t.Errorf("the background of the sliced item is drawn on %d pages only", grey)
	}
}

func TestFlexLineOfExactlyOnePageFitsAFreshPage(t *testing.T) {
	html := `<div class=b>before</div><div style="display:flex"><div id=fi0 style="flex:none;width:100px">` + strings.Repeat(`<div class=b>l</div>`, 6) + `</div></div>`
	pages := breakRun(t, html)
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	if items := flexItemsOn(pages, 1); len(items) != 1 || !near(items[0].rect.Y, 0) || !near(items[0].rect.H, 600) || !items[0].first || !items[0].last {
		t.Errorf("page 2: %d items %+v", len(items), items)
	}
	if len(flexItemsOn(pages, 0)) != 0 {
		t.Error("a line that does not fit leaves a fragment on the first page")
	}
}

func TestFlexNestedAndInAnAtom(t *testing.T) {
	html := `<div style="display:flex"><div style="flex:1"><div style="display:flex"><div style="flex:1">a</div><div style="flex:2">b</div></div></div><span style="display:inline-block"><div style="display:flex"><div>c</div><div>d</div></div></span></div>`
	pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
	assertTextKept(t, html, pages)
}

func TestFlexNestedIsLinear(t *testing.T) {
	work := func(n int) int64 {
		html := strings.Repeat(`<div style="display:flex">`, n) + "x" + strings.Repeat("</div>", n)
		r := newRun(t, html, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Boxes] = 1 << 20; l[budget.Nodes] = 1 << 22 }})
		start := time.Now()
		r.all(t)
		if d := time.Since(start); n == 500 && d > 2*time.Second && !raceEnabled {
			t.Errorf("500 nested flex containers took %v", d)
		}
		return r.b.Used(budget.Work)
	}
	w250, w500 := work(250), work(500)
	if float64(w500) > 2.2*float64(w250) {
		t.Errorf("work %d for 500 levels against %d for 250", w500, w250)
	}
}

const coverCSS = `
body { margin: 0; font: 9.5pt/1.5 "DejaVu Sans", Arial, sans-serif }
.cover { display: flex; align-items: center; justify-content: space-between; gap: 12mm; padding: 8mm 10mm; border-radius: 4px;
  background: linear-gradient(120deg, #e52321 0%, #ea5b0c 45%, #f9c105 100%); color: #fff }
.cover h1 { margin: 0; font-size: 20pt; font-weight: 700 }
.cover .scan-id { margin: 2mm 0 0; font-family: "DejaVu Sans Mono", monospace; font-size: 9pt; opacity: 0.9 }
.cover .verdict { flex: none; padding: 3mm 5mm; border-radius: 999px; background: rgba(255, 255, 255, 0.95); font-weight: 700; font-size: 10pt; white-space: nowrap }
`

// The cover of template 01: a flex row with its text on the left and a pill on
// the right, centered across.
func TestFlexCoverGolden(t *testing.T) {
	html := `<div class=cover><div><h1>Rapport d'analyse</h1><p class=scan-id>a1b2c3d4-e5f6</p></div><div class=verdict>● 5 fichier(s) bloqué(s)</div></div>`
	pages := newRun(t, html, opts{extra: "@page { size: 794.56px 1122.56px; margin: 0 }", css: coverCSS}).all(t)
	golden(t, "flex_cover", pages)
	cover := frags(pages, "div")[0]
	if !near(cover.rect.W, pages[0].Area.W/pages[0].Scale) {
		t.Errorf("cover %+v", cover.rect)
	}
	var verdict *frag
	for _, f := range frags(pages, "div") {
		if f.box.Style.FlexShrink == 0 {
			verdict = f
		}
	}
	if verdict == nil || !near(verdict.rect.Y+verdict.rect.H/2, cover.rect.Y+cover.rect.H/2) {
		t.Errorf("the verdict is not centered across the cover")
	}
}

// A flex line taller than the page is laid out once, not once per page.
func TestFlexSlicedLineIsLaidOutOnce(t *testing.T) {
	para := strings.Repeat("lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor ", 3)
	measure := func(display string) (pages int, work, glyphs int64, text string) {
		html := `<div style="display:` + display + `"><div>` + strings.Repeat("<p>"+para+"</p>", 1000) + `</div></div>`
		r := newRun(t, html, opts{extra: smallPage, css: flat + " p { margin: 0 }", limits: func(l *budget.Limits) { l[budget.Glyphs] = 1 << 30; l[budget.Work] = 1 << 30 }})
		ps := r.all(t)
		return len(ps), r.b.Used(budget.Work), r.b.Used(budget.Glyphs), textOf(ps)
	}
	bp, bw, bg, btext := measure("block")
	fp, fw, fg, ftext := measure("flex")
	if fp < bp-1 || fp > bp+1 || ftext != btext {
		t.Errorf("flex: %d pages, block: %d; same text: %v", fp, bp, ftext == btext)
	}
	if fg > 2*bg {
		t.Errorf("%d glyphs shaped in flex, %d in block: more than twice", fg, bg)
	}
	if float64(fw) > 2.2*float64(bw) {
		t.Errorf("work %d in flex against %d in block", fw, bw)
	}
	t.Logf("pages %d/%d work %d/%d glyphs %d/%d", fp, bp, fw, bw, fg, bg)
}

// Lines pushed to the next page are not laid out, nor shaped, again.
func TestFlexPushedLinesAreNotLaidOutAgain(t *testing.T) {
	html := `<div style="display:flex;flex-wrap:wrap;row-gap:0">` + strings.Repeat(`<div class=b style="flex:none;width:100%">x</div>`, 30) + `</div>`
	r := newRun(t, html, opts{extra: smallPage, css: breaksCSS})
	if n := len(r.all(t)); n != 5 {
		t.Fatalf("%d pages", n)
	}
	if got := r.b.Used(budget.Glyphs); got != 2*30 {
		t.Errorf("%d glyphs for 30 one-glyph items: each must be shaped exactly twice", got)
	}
}

// With flex-shrink factors that sum to less than one the items give up only
// that part of the overflow (Flexbox §9.7 step 4c), as when they grow.
func TestFlexShrinkFactorsUnderOne(t *testing.T) {
	s, _ := flexRun(t, "", item("width:200px;flex:0 0.25 auto", "a")+item("width:200px;flex:0 0.25 auto", "a"), 300)
	if got := ws(s[0]); !sameFloats(got, []float64{175, 175}) {
		t.Errorf("widths %v: 100px of overflow × 0.5 = 50px, 25px each", got)
	}
}

// A flex item that may not break inside keeps the width of its flex line when it
// is measured again.
func TestFlexItemWithBreakInsideAvoidKeepsItsWidth(t *testing.T) {
	html := blocks(5, "p") + `<div style="display:flex"><div id=fi0 style="flex:none;width:250px;break-inside:avoid"><div class=b>a</div></div><div id=fi1 style="flex:1;break-inside:avoid"><div class=b>b</div></div></div>`
	pages := breakRun(t, html)
	for i, pg := range pages {
		for _, f := range flexItemsOn(pages, i) {
			if f.box.ID == "fi0" && !near(f.rect.W, 250) {
				t.Errorf("page %d: #fi0 is %v wide", pg.Number, f.rect.W)
			}
			if f.box.ID == "fi1" && !near(f.rect.W, 800-250) {
				t.Errorf("page %d: #fi1 is %v wide", pg.Number, f.rect.W)
			}
		}
	}
}

// The scaled initial free space of §9.7 step 4b stays the one of step 3 after an
// item froze: A, B and C shrink by 0.3, the overflow is 100px, A stops at its
// min-content (its width is 5px under its basis), the 95px left are scaled from
// the 100px of the start, not from themselves.
func TestFlexShrinkFactorsAfterAFreeze(t *testing.T) {
	long := "WWWWWWWWWWWWWWWWWWWWWWWWWWWWWW"
	minA := widthOf(t, long, "")
	basisA := minA + 5
	items := item(fmt.Sprintf("width:%vpx;flex:0 0.3 auto", basisA), long) + item("width:150px;flex:0 0.3 auto", "b") + item("width:150px;flex:0 0.3 auto", "c")
	avail := basisA + 300 - 100
	s, _ := flexRun(t, "", items, avail)
	got := ws(s[0])
	if !near(got[0], minA) {
		t.Fatalf("A is %v wide, want its min-content %v: %v", got[0], minA, got)
	}
	// Pass 1: free = -100 × 0.9 = -90 over the three bases (scaled shrink): A would
	// lose 90 × basisA / (basisA + 300) and goes under its minimum, so it freezes.
	// Pass 2: the remaining free space is -(100 - 5) = -95 but the scaled initial
	// one is -100 × 0.6 = -60 (two items of 0.3 are left): B and C lose 30 each.
	if !near(got[1], 150-30) || !near(got[2], 150-30) {
		t.Errorf("B and C are %v and %v wide, want 120 each", got[1], got[2])
	}
}
