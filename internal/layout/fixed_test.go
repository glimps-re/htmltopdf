package layout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
)

const fixedSmallPage = "@page{size:200px 100px;margin:0} body{margin:0} p{margin:0;height:100px}"

func countText(pg *Page, s string) int {
	n := 0
	for _, it := range pg.Items {
		if t, ok := it.(*display.Text); ok && t.Text == s {
			n++
		}
	}
	return n
}

func TestFixedIsOnEveryPage(t *testing.T) {
	r := newRun(t, `<div style="position:fixed;top:0;left:0">MARK</div>`+strings.Repeat("<p>x</p>", 5), opts{extra: fixedSmallPage})
	pages := r.all(t)
	if len(pages) != 5 {
		t.Fatalf("%d pages", len(pages))
	}
	for i, pg := range pages {
		if n := countText(pg, "MARK"); n != 1 {
			t.Errorf("page %d: %d fixed boxes", i+1, n)
		}
	}
}

// A fixed box without a vertical inset is on the pages from its static position's.
func TestFixedWithoutVerticalInsetStartsAtItsStaticPage(t *testing.T) {
	r := newRun(t, `<p>x</p><p>x</p><div style="position:fixed;left:0">MARK</div><p>x</p><p>x</p>`, opts{extra: fixedSmallPage})
	var got []int
	for _, pg := range r.all(t) {
		got = append(got, countText(pg, "MARK"))
	}
	if fmt.Sprint(got) != "[0 0 1 1]" {
		t.Errorf("fixed box per page %v, want [0 0 1 1]", got)
	}
}

// A fixed box with a vertical inset is on page 1 even when its element comes
// late in the document (probe: Chrome, top:10px after 1500 px of flow).
func TestFixedWithVerticalInsetIsOnPageOneWhateverItsPlace(t *testing.T) {
	r := newRun(t, strings.Repeat("<p>x</p>", 3)+`<div style="position:fixed;top:0;left:0">MARK</div><p>x</p><p>x</p>`, opts{extra: fixedSmallPage})
	var got []int
	for _, pg := range r.all(t) {
		got = append(got, countText(pg, "MARK"))
	}
	if fmt.Sprint(got) != "[1 1 1 1 1]" {
		t.Errorf("fixed box per page %v, want [1 1 1 1 1]", got)
	}
}

// A box laid out several times (an avoid box that moves to the next page) registers
// its fixed boxes once.
func TestFixedInAnAvoidBoxIsPlacedOnce(t *testing.T) {
	html := `<p>x</p><div style="break-inside:avoid;height:60px"><div style="position:fixed;left:0">MARK</div></div><p>x</p>`
	r := newRun(t, html, opts{extra: fixedSmallPage})
	total := 0
	for _, pg := range r.all(t) {
		if n := countText(pg, "MARK"); n > 1 {
			t.Errorf("%d copies on one page", n)
		}
		total += countText(pg, "MARK")
	}
	if total == 0 {
		t.Error("the fixed box is lost")
	}
}

func fixedBoxes(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<div style="position:fixed;top:0;left:0;width:3px;height:3px;background:#c00;font-size:1px">F%d</div>`, i)
	}
	return sb.String()
}

func TestFixedItemsPerPageBound(t *testing.T) {
	const perBox = 2 // background and text
	at := maxFixedItems / perBox
	for _, tc := range []struct {
		n    int
		warn bool
	}{{at, false}, {at + 1, true}} {
		r := newRun(t, fixedBoxes(tc.n)+strings.Repeat("<p>x</p>", 3), opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Nodes], l[budget.Boxes] = 1<<20, 1<<20 }})
		pages := r.all(t)
		ws, _ := r.b.Warnings()
		warned := false
		for _, w := range ws {
			warned = warned || w.Detail == "fixed boxes"
		}
		if warned != tc.warn {
			t.Errorf("%d boxes: warned=%v", tc.n, warned)
		}
		last := fmt.Sprintf("F%d", tc.n-1)
		for i, pg := range pages {
			want := 1
			if tc.warn && i > 0 {
				want = 0 // past the bound: on the first page only
			}
			if n := countText(pg, last); n != want {
				t.Errorf("%d boxes, page %d: last box %d times, want %d", tc.n, i+1, n, want)
			}
		}
	}
}

// Each repeat of fixed text is written again: it is charged to Glyphs, and when the
// repeats would take more than a quarter of them the text stays on its first page.
func TestFixedRepeatsAreChargedToGlyphs(t *testing.T) {
	const max = 5000
	html := `<div style="position:fixed;top:0;left:0;width:150px;font-size:1px">` + strings.Repeat("word ", 400) + `</div>` + strings.Repeat("<p>x</p>", 50)
	r := newRun(t, html, opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Glyphs] = max }})
	pages := r.all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	words := func(pg *Page) (n int) {
		for _, tx := range texts(pg.Items) {
			n += len(tx.Glyphs)
		}
		return n
	}
	if first, second := words(pages[0]), words(pages[1]); first < 1500 || second > 50 {
		t.Errorf("glyphs on page 1: %d, on page 2: %d", first, second)
	}
	if ws, _ := r.b.Warnings(); len(ws) == 0 || ws[0].Detail != "fixed boxes" {
		t.Errorf("warnings %v", ws)
	}
	if used := r.b.Used(budget.Glyphs); used > max {
		t.Errorf("%d glyphs charged, max %d", used, max)
	}
}

// A fixed box without a vertical inset inside a fixed box is on the same pages as
// its parent, once on each.
func TestNestedFixedBoxIsOnTheParentsPages(t *testing.T) {
	const html = `<div style="position:fixed;top:0">OUTERMARK<div style="position:fixed;left:200px">INNERMARK</div></div>`
	for _, flow := range []int{3, 150} {
		pages := newRun(t, html+strings.Repeat("<p>x</p>", flow), opts{extra: fixedSmallPage}).all(t)
		if flow > 3 && len(pages) < 3 {
			t.Fatalf("%d pages", len(pages))
		}
		for i, pg := range pages {
			if o, n := countText(pg, "OUTERMARK"), countText(pg, "INNERMARK"); o != 1 || n != 1 {
				t.Errorf("%d lines, page %d: outer %d, inner %d", flow, i+1, o, n)
			}
		}
	}
}

// The repeats that the bound drops are not charged to Glyphs: the first page
// has every fixed text once and each later page writes at most the bound.
func TestFixedPastTheBoundIsNotChargedForItsRepeats(t *testing.T) {
	const boxes, glyphs = 5000, 10
	html := strings.Repeat(`<p style="position:fixed;left:0">ABCDEFGHIJ</p>`, boxes) + strings.Repeat("<p>x</p>", 40)
	r := newRun(t, html, opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Nodes], l[budget.Boxes] = 1<<20, 1<<20 }})
	pages := r.all(t)
	if len(pages) < 4 {
		t.Fatalf("%d pages", len(pages))
	}
	if n := countText(pages[0], "ABCDEFGHIJ"); n != boxes {
		t.Errorf("%d fixed texts on the first page", n)
	}
	if ws, _ := r.b.Warnings(); len(ws) == 0 {
		t.Error("no warning")
	}
	flow := int64(40 + 8)
	if used, max := r.b.Used(budget.Glyphs), flow+2*boxes*glyphs+int64(len(pages)-1)*maxFixedItems*glyphs; used > max {
		t.Errorf("%d glyphs charged, at most %d expected", used, max)
	}
}

// Empty fixed boxes count as an item each toward the bound and to Work.
func TestEmptyFixedBoxesAreBoundedToo(t *testing.T) {
	html := strings.Repeat(`<div style="position:fixed;top:0;left:0"></div>`, maxFixedItems+10) + strings.Repeat("<p>x</p>", 20)
	r := newRun(t, html, opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Nodes], l[budget.Boxes] = 1<<20, 1<<20 }})
	pages := r.all(t)
	ws, _ := r.b.Warnings()
	warned := false
	for _, w := range ws {
		warned = warned || w.Detail == "fixed boxes"
	}
	if !warned || len(pages) < 3 {
		t.Errorf("warned %v, %d pages", warned, len(pages))
	}
}

// A positional fixed box is laid out again for a page area of another size.
func TestPositionalFixedBoxFollowsThePageSize(t *testing.T) {
	html := `<p>x</p><div style="position:fixed;right:0;width:20px;height:5px;background:#f00"></div><p style="page:wide;break-before:page">y</p><p style="page:wide">y</p>`
	r := newRun(t, html, opts{extra: `@page{size:300px 200px;margin:0} @page wide{size:600px 200px;margin:0}`})
	var xs []float64
	for _, pg := range r.all(t) {
		for _, rc := range fixedRects(pg.Items, "#ff0000") {
			xs = append(xs, rc.X)
		}
	}
	if len(xs) < 2 || xs[0] != 280 || xs[len(xs)-1] != 580 {
		t.Errorf("x of the box per page: %v", xs)
	}
}

// A watermark on 500 pages costs its items on each page and nothing else: the
// Work of the document is that of its twin without the box, plus the items of
// the box on every page.
func TestFixedWatermarkWorkIsLinear(t *testing.T) {
	const pages = 500
	flow := strings.Repeat(`<p style="break-after:page">x</p>`, pages)
	work := func(html string) (int64, int) {
		r := newRun(t, html, opts{extra: fixedSmallPage})
		n := len(r.all(t))
		return r.b.Used(budget.Work), n
	}
	twin, n := work(flow)
	with, m := work(`<div style="position:fixed;top:0;left:0;width:40px;height:10px;background:#c00;font-size:4px">WM</div>` + flow)
	const items = 2 // background and text
	if n != pages || m != pages {
		t.Fatalf("%d and %d pages", n, m)
	}
	if limit := twin + twin/10 + pages*items; with > limit {
		t.Errorf("Work %d with the box, %d without: limit %d", with, twin, limit)
	}
}

func BenchmarkFixedLayers(b *testing.B) {
	for _, tc := range []struct {
		name, box string
		pages     int
	}{
		{"watermark on 500 pages", `<div style="position:fixed;top:0;left:0;width:40px;height:10px;background:#c00;font-size:4px">WM</div>`, 500},
		{"4096 items on 20 pages", fixedBoxes(maxFixedItems / 2), 20},
	} {
		b.Run(tc.name, func(b *testing.B) {
			html := tc.box + strings.Repeat(`<p style="break-after:page">x</p>`, tc.pages)
			b.ReportAllocs()
			for range b.N {
				r := newRun(b, html, opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Nodes], l[budget.Boxes] = 1<<20, 1<<20 }})
				r.all(b)
			}
		})
	}
}

// The repeats of fixed text may take a quarter of MaxGlyphs: at that cap every page
// has the text and nothing is warned; one page more and the repeat that would pass
// it is dropped, with the warning; the text is in the document in any case.
func TestFixedTextRepeatsAtTheGlyphCap(t *testing.T) {
	const glyphs, max = 100, 2000 // a repeat costs 100: the cap, 500, is 5 repeats = 6 pages
	box := `<div style="position:fixed;top:0;left:0;width:300px;font-size:1px">` + strings.Repeat("a", glyphs) + `</div>`
	for _, tc := range []struct {
		pages int
		warn  bool
	}{{5, false}, {6, false}, {7, true}, {12, true}} {
		html := box + strings.Repeat(`<p style="break-after:page">x</p>`, tc.pages)
		r := newRun(t, html, opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Glyphs] = max }})
		pages := r.all(t)
		if len(pages) != tc.pages {
			t.Fatalf("%d pages, want %d", len(pages), tc.pages)
		}
		with := 0
		for _, pg := range pages {
			if countText(pg, strings.Repeat("a", glyphs)) == 1 {
				with++
			}
		}
		want := min(tc.pages, 6)
		if with != want {
			t.Errorf("%d pages: the text is on %d, want %d", tc.pages, with, want)
		}
		warned := false
		ws, _ := r.b.Warnings()
		for _, w := range ws {
			warned = warned || w.Detail == "fixed boxes"
		}
		if warned != tc.warn {
			t.Errorf("%d pages: warned %v", tc.pages, warned)
		}
	}
}

// Beyond the cap every fixed text is still in the document: on its first page.
func TestFixedTextsBeyondTheGlyphCapAppearOnce(t *testing.T) {
	var sb strings.Builder
	for i := range 30 {
		fmt.Fprintf(&sb, `<div style="position:fixed;top:%dpx;left:0;font-size:1px">T%02d%s</div>`, i, i, strings.Repeat("b", 60))
	}
	r := newRun(t, sb.String()+strings.Repeat(`<p style="break-after:page">x</p>`, 20), opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Glyphs] = 10000 }})
	pages := r.all(t)
	for i := range 30 {
		mark := fmt.Sprintf("T%02d%s", i, strings.Repeat("b", 60))
		n := 0
		for _, pg := range pages {
			n += countText(pg, mark)
		}
		if n < 1 {
			t.Errorf("text %d is not in the document", i)
		}
	}
	if used := r.b.Used(budget.Glyphs); used > 10000 {
		t.Errorf("%d glyphs", used)
	}
}

// Fixed boxes met out of order (one below the page, then one above it, by a
// negative margin) are each on their start page and on none before it, with or
// without the bound.
func TestFixedBoxesMetOutOfOrderKeepTheirStartPages(t *testing.T) {
	html := `<div style="height:300px;break-inside:avoid"></div><div style="position:fixed;left:0">AAA</div><div style="margin-top:-250px"><div style="position:fixed;left:50px">BBB</div></div><p>y</p><p>z</p>`
	for _, over := range []bool{false, true} {
		h := html
		if over {
			h = strings.Repeat(`<div style="position:fixed;top:0;left:0;width:1px;height:1px;background:#c00"></div>`, maxFixedItems) + html
		}
		pages := newRun(t, h, opts{extra: fixedSmallPage, limits: func(l *budget.Limits) { l[budget.Nodes], l[budget.Boxes] = 1<<20, 1<<20 }}).all(t)
		// The static position of AAA is below the document: it is on the last
		// page; BBB is pulled up to the third page by the negative margin.
		for i, pg := range pages {
			wantA, wantB := 0, 0
			if i == len(pages)-1 {
				wantA = 1
			}
			if i >= 2 {
				wantB = 1
			}
			if a, b := countText(pg, "AAA"), countText(pg, "BBB"); !over && (a != wantA || b != wantB) || over && (a > 1 || b > 1) {
				t.Errorf("over=%v page %d: AAA %d (want %d), BBB %d (want %d)", over, i+1, a, wantA, b, wantB)
			}
		}
		for _, mark := range []string{"AAA", "BBB"} {
			n := 0
			for _, pg := range pages {
				n += countText(pg, mark)
			}
			if n < 1 {
				t.Errorf("over=%v: %s is not in the document", over, mark)
			}
		}
	}
}

// Once a fixed box is refused a repeat it is not laid out again for each page size:
// the Work of 40 page sizes is not much more than that of 10.
func TestDroppedFixedBoxIsNotLaidOutAgain(t *testing.T) {
	work := func(sizes int) int64 {
		var css, body strings.Builder
		css.WriteString("@page{margin:0} body{margin:0} p{margin:0;height:50px}")
		body.WriteString(`<div style="position:fixed;top:0">` + strings.Repeat(`<div style="position:fixed;left:0"></div>`, 3*maxFixedItems) + `</div>`)
		for i := range sizes {
			fmt.Fprintf(&css, "@page s%d{size:%dpx 100px}.s%d{page:s%d;break-before:page}", i, 200+i, i, i)
			fmt.Fprintf(&body, `<p class="s%d">x</p>`, i)
		}
		r := newRun(t, body.String(), opts{extra: css.String(), limits: func(l *budget.Limits) { l[budget.Nodes], l[budget.Boxes] = 1<<20, 1<<20 }})
		r.all(t)
		return r.b.Used(budget.Work)
	}
	w10, w40 := work(10), work(40)
	t.Logf("Work %d for 10 sizes, %d for 40", w10, w40)
	if w40 > w10+30*int64(maxFixedItems) {
		t.Errorf("Work %d for 40 page sizes, %d for 10", w40, w10)
	}
}

// A fixed box whose static position is below the bottom of the last page stays on
// that page: its text is not dropped with the page that does not exist.
func TestFixedBoxBelowTheLastPageStaysOnIt(t *testing.T) {
	pages := newRun(t, `<div style="height:100px"></div><div style="position:fixed;left:0">BELOW</div>`, opts{extra: fixedSmallPage}).all(t)
	if len(pages) != 1 {
		t.Fatalf("%d pages", len(pages))
	}
	if n := countText(pages[0], "BELOW"); n != 1 {
		t.Errorf("the text is %d times on the page", n)
	}
}
