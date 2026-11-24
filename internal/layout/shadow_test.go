package layout

import (
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func shadowStyle(sh ...style.Shadow) *style.Computed { return &style.Computed{BoxShadow: sh} }

func fills(t *testing.T, l display.List) []display.FillRect {
	t.Helper()
	out := make([]display.FillRect, len(l))
	for i, it := range l {
		f, ok := it.(display.FillRect)
		if !ok {
			t.Fatalf("item %d is %T", i, it)
		}
		out[i] = f
	}
	return out
}

func TestShadowWithoutBlur(t *testing.T) {
	col := css.Color{R: 1, A: 0.5}
	box := display.Rect{X: 10, Y: 20, W: 100, H: 50}
	got := fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{X: 3, Y: 4, Spread: 2, Color: col}), box, display.Radii{}))
	want := display.Rect{X: 11, Y: 22, W: 104, H: 54}
	if len(got) != 1 || got[0].Rect != want || got[0].Color != col || !got[0].Radii.IsZero() {
		t.Errorf("%+v", got)
	}
}

func TestShadowBlurIsConcentric(t *testing.T) {
	col := css.Color{A: 0.8}
	box := display.Rect{X: 0, Y: 0, W: 100, H: 100}
	got := fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Blur: 12, Color: col}), box, display.Radii{}))
	if len(got) != blurSteps {
		t.Fatalf("%d rectangles", len(got))
	}
	wantGrow := []float64{6, 2, -2, -6}
	for i, f := range got {
		if f.Rect != grow(box, wantGrow[i]) {
			t.Errorf("rectangle %d: %+v", i, f.Rect)
		}
		if f.Color.A != 0.2 {
			t.Errorf("rectangle %d alpha %v", i, f.Color.A)
		}
	}
}

func TestShadowRadiiGrowAndFit(t *testing.T) {
	box := display.Rect{W: 40, H: 40}
	var rad display.Radii
	for i := range rad {
		rad[i].H, rad[i].V = 10, 10
	}
	got := fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Spread: 5, Color: css.Color{A: 1}}), box, rad))
	if got[0].Radii[0].H != 15 {
		t.Errorf("radius %v, want 15", got[0].Radii[0].H)
	}
	got = fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Spread: 100, Color: css.Color{A: 1}}), box, rad))
	r := got[0].Radii
	if r[0].H+r[1].H > got[0].Rect.W+1e-9 || r[0].V+r[3].V > got[0].Rect.H+1e-9 {
		t.Errorf("radii %+v exceed %+v", r, got[0].Rect)
	}
	got = fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Spread: -100, Color: css.Color{A: 1}}), box, rad))
	if len(got) != 0 {
		t.Errorf("a shadow shrunk to nothing is drawn: %+v", got)
	}
	got = fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Color: css.Color{A: 1}}), box, display.Radii{}))
	if !got[0].Radii.IsZero() {
		t.Error("a square box gets a rounded shadow")
	}
}

func TestShadowOrderAndSkips(t *testing.T) {
	a, b := css.Color{R: 1, A: 1}, css.Color{G: 1, A: 1}
	box := display.Rect{W: 10, H: 10}
	got := fills(t, (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Color: a}, style.Shadow{Color: css.Color{}}, style.Shadow{Color: b}), box, display.Radii{}))
	if len(got) != 2 || got[0].Color != b || got[1].Color != a {
		t.Errorf("first shadow must be painted last: %+v", got)
	}
	if got := (&emitter{}).appendShadows(nil, shadowStyle(style.Shadow{Color: a}), display.Rect{W: 0, H: 10}, display.Radii{}); len(got) != 0 {
		t.Errorf("an empty box has a shadow: %v", got)
	}
	if got := (&emitter{}).appendShadows(nil, &style.Computed{}, box, display.Radii{}); got != nil {
		t.Errorf("no shadow: %v", got)
	}
}

func BenchmarkAppendShadows(b *testing.B) {
	sh := make([]style.Shadow, style.MaxShadows)
	for i := range sh {
		sh[i] = style.Shadow{X: 1, Y: 2, Blur: 8, Spread: 1, Color: css.Color{A: 0.3}}
	}
	st := shadowStyle(sh...)
	r := display.Rect{W: 100, H: 50}
	var out display.List
	b.ReportAllocs()
	for b.Loop() {
		out = (&emitter{}).appendShadows(out[:0], st, r, display.Radii{})
	}
}

func TestTemplate00DrawsItsShadows(t *testing.T) {
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "00-host-connector", testcorpus.Small)
	if err != nil {
		t.Fatal(err)
	}
	count := func(extra string) (pages, fillRects int) {
		r := newRun(t, string(html), opts{extra: testcorpus.HostConnectorPageCSS + extra})
		for _, pg := range r.all(t) {
			pages++
			for _, it := range pg.Items {
				if _, ok := it.(display.FillRect); ok {
					fillRects++
				}
			}
		}
		return pages, fillRects
	}
	pagesOn, fillsOn := count("")
	pagesOff, fillsOff := count("* { box-shadow: none !important }")
	if pagesOn != pagesOff {
		t.Errorf("shadows change the page count: %d and %d", pagesOn, pagesOff)
	}
	if fillsOn <= fillsOff {
		t.Errorf("no shadow drawn: %d fills with, %d without", fillsOn, fillsOff)
	}
	t.Logf("%d pages; %d fills with shadows, %d without", pagesOn, fillsOn, fillsOff)
}

func manyShadowBoxes(n int) string {
	sh := strings.Repeat("1px 2px 8px 1px rgba(0,0,0,.3),", 7) + "0 0 4px red"
	return strings.Repeat(`<div class=b style="box-shadow:`+sh+`">x</div>`, n)
}

func TestManyShadowedBoxesStayBounded(t *testing.T) {
	r := newRun(t, manyShadowBoxes(1000), opts{extra: smallPage, css: "body { margin: 0 } .b { line-height: 20px }"})
	fills := 0
	for _, pg := range r.all(t) {
		for _, it := range pg.Items {
			if _, ok := it.(display.FillRect); ok {
				fills++
			}
		}
	}
	if want := 1000 * 8 * blurSteps; fills != want {
		t.Errorf("%d fills, want %d", fills, want)
	}
}

func BenchmarkShadowedBoxes(b *testing.B) {
	html := manyShadowBoxes(1000)
	b.ReportAllocs()
	for b.Loop() {
		r := newRun(b, html, opts{extra: smallPage, css: "body { margin: 0 } .b { line-height: 20px }"})
		r.all(b)
	}
}

const eightBlurred = "1px 1px 2px red,1px 1px 2px red,1px 1px 2px red,1px 1px 2px red,1px 1px 2px red,1px 1px 2px red,1px 1px 2px red,1px 1px 2px red"

func shadowFills(t *testing.T, html string) (perPage []int, work int64, warned int) {
	t.Helper()
	r := newRun(t, html, opts{extra: smallPage, css: "body { margin: 0 } .b { line-height: 4px; font-size: 4px; box-shadow: " + eightBlurred + " }"})
	for _, pg := range r.all(t) {
		n := 0
		for _, it := range pg.Items {
			if _, ok := it.(display.FillRect); ok {
				n++
			}
		}
		perPage = append(perPage, n)
	}
	ws, _ := r.b.Warnings()
	for _, w := range ws {
		if w.Code == budget.WarnClamped && w.Detail == "box-shadow items per page" {
			warned++
		}
	}
	return perPage, r.b.Used(budget.Work), warned
}

func TestShadowItemsPerPageBound(t *testing.T) {
	boxes := func(n int) string { return strings.Repeat(`<div class=b>x</div>`, n) }
	full, work, warned := shadowFills(t, boxes(128))
	if len(full) != 1 || full[0] != maxShadowItems || warned != 0 {
		t.Errorf("128 boxes: %v fills, %d warnings", full, warned)
	}
	_, baseWork, _ := shadowFills(t, boxes(0))
	if work-baseWork < maxShadowItems {
		t.Errorf("work %d (empty %d): one unit per shadow item expected", work, baseWork)
	}
	over, _, warned := shadowFills(t, boxes(129))
	if len(over) != 1 || over[0] != maxShadowItems || warned != 1 {
		t.Errorf("129 boxes: %v fills, %d warnings", over, warned)
	}
	two, _, warned := shadowFills(t, boxes(310))
	if len(two) < 2 || two[0] != maxShadowItems || two[1] != maxShadowItems || warned != 1 {
		t.Errorf("the counter must restart on each page: %v fills, %d warnings", two, warned)
	}
}
