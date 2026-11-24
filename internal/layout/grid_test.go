package layout

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const gridCSS = "body, p { margin: 0 } .g { display: grid } .g > div { line-height: 20px; font-size: 10px }"

func gridRun(t testing.TB, html string) []*Page {
	t.Helper()
	return newRun(t, html, opts{extra: smallPage, css: gridCSS}).all(t)
}

func gridFrag(t testing.TB, pages []*Page) *frag {
	t.Helper()
	var g *frag
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			if g == nil && f.box.Style.Display == style.DisplayGrid {
				g = f
			}
		})
	}
	if g == nil {
		t.Fatal("no grid container")
	}
	return g
}

// cells returns the x and width of the items of the first grid container.
func gridCells(t testing.TB, html string) (xs, ws []float64) {
	t.Helper()
	g := gridFrag(t, gridRun(t, html))
	for _, c := range g.children {
		xs = append(xs, c.rect.X)
		ws = append(ws, c.rect.W)
	}
	return xs, ws
}

func gridDivs(n int, content string) string {
	return strings.Repeat("<div>"+content+"</div>", n)
}

func TestGridTrackSizing(t *testing.T) {
	tests := []struct {
		name    string
		css     string
		items   string
		wantX   []float64
		wantW   []float64
		gapCols float64
	}{
		{"fixed px", "grid-template-columns: 100px 200px", gridDivs(2, "a"), []float64{0, 100}, []float64{100, 200}, 0},
		{"percentages", "grid-template-columns: 25% 50%", gridDivs(2, "a"), []float64{0, 200}, []float64{200, 400}, 0},
		{"fr share", "grid-template-columns: 1fr 3fr", gridDivs(2, "a"), []float64{0, 200}, []float64{200, 600}, 0},
		{"fr and px", "grid-template-columns: 100px 1fr", gridDivs(2, "a"), []float64{0, 100}, []float64{100, 700}, 0},
		{"fr below 1 takes that fraction", "grid-template-columns: 0.5fr 0.25fr", gridDivs(2, "a"), []float64{0, 400}, []float64{400, 200}, 0},
		{"auto columns share the space", "grid-template-columns: auto auto", gridDivs(2, "a"), []float64{0, 400}, []float64{400, 400}, 0},
		{"auto stretches only up to... all of it", "grid-template-columns: auto 100px", gridDivs(2, "a"), []float64{0, 700}, []float64{700, 100}, 0},
		{"gaps", "grid-template-columns: 1fr 1fr; column-gap: 20px", gridDivs(2, "a"), []float64{0, 410}, []float64{390, 390}, 0},
		{"gap shorthand", "grid-template-columns: 100px 100px; gap: 10px 30px", gridDivs(2, "a"), []float64{0, 130}, []float64{100, 100}, 0},
		{"no template: one auto column", "", gridDivs(2, "a"), []float64{0, 0}, []float64{800, 800}, 0},
		{"more items than columns wrap to rows", "grid-template-columns: 100px 100px", gridDivs(3, "a"), []float64{0, 100, 0}, []float64{100, 100, 100}, 0},
		{"one item", "grid-template-columns: 1fr 1fr", gridDivs(1, "a"), []float64{0}, []float64{400}, 0},
		{"gaps larger than the container", "grid-template-columns: 100px 100px; column-gap: 2000px", gridDivs(2, "a"), []float64{0, 2100}, []float64{100, 100}, 0},
		{"item margins shrink the stretched item", "grid-template-columns: 200px 200px", `<div style="margin:0 10px 0 20px">a</div><div>b</div>`, []float64{20, 200}, []float64{170, 200}, 0},
		{"item with a width keeps it", "grid-template-columns: 200px", `<div style="width:50px">a</div>`, []float64{0}, []float64{50}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := `<div class=g style="` + tt.css + `">` + tt.items + `</div>`
			xs, ws := gridCells(t, html)
			if !sameFloats(xs, tt.wantX) || !sameFloats(ws, tt.wantW) {
				t.Errorf("x %v w %v, want x %v w %v", xs, ws, tt.wantX, tt.wantW)
			}
		})
	}
}

func TestGridItemPercentagesRefertoTheirColumn(t *testing.T) {
	g := gridFrag(t, gridRun(t, `<div class=g style="grid-template-columns:repeat(4,200px)"><div style="padding:0 5%;margin-left:10%">x</div></div>`))
	item := g.children[0]
	if !near(item.padding[style.Left], 10) || !near(item.rect.X, 20) {
		t.Errorf("padding %v, x %v: percentages must resolve against the 200px column (want 10 and 20)", item.padding[style.Left], item.rect.X)
	}
}

func TestGridIntrinsicTracks(t *testing.T) {
	html := func(cols string) string {
		return `<div class=g style="grid-template-columns:` + cols + `"><div>aaaa bb</div><div>x</div><div>aa</div><div>yyyy zzzzzz</div></div>`
	}
	_, mx := gridCells(t, html("max-content max-content"))
	_, mn := gridCells(t, html("min-content min-content"))
	_, mf := gridCells(t, html("max-content 1fr"))
	if mx[0] <= mn[0] || mx[1] <= mn[1] {
		t.Errorf("max-content %v is not wider than min-content %v", mx, mn)
	}
	if mn[0] >= mx[0] || mx[0] != mx[2] || mx[1] != mx[3] {
		t.Errorf("items of a column must share its width: %v", mx)
	}
	if math.Abs(mf[0]-mx[0]) > 0.01 || math.Abs(mf[0]+mf[1]-800) > 0.01 {
		t.Errorf("max-content + 1fr: %v (max-content alone %v)", mf, mx)
	}
}

func TestGridRowsAndAlignment(t *testing.T) {
	html := func(align string) string {
		return `<div class=g style="grid-template-columns:1fr 1fr;align-items:` + align + `"><div>a<br>b<br>c</div><div>x</div></div>`
	}
	for align, want := range map[string][2]float64{
		"stretch":    {0, 60},
		"flex-start": {0, 20},
		"center":     {20, 20},
		"flex-end":   {40, 20},
	} {
		g := gridFrag(t, gridRun(t, html(align)))
		second := g.children[1]
		if !near(second.rect.Y-g.rect.Y, want[0]) || !near(second.rect.H, want[1]) {
			t.Errorf("%s: second item y %v h %v, want %v", align, second.rect.Y-g.rect.Y, second.rect.H, want)
		}
		if !near(g.rect.H, 60) {
			t.Errorf("%s: container height %v, want 60", align, g.rect.H)
		}
	}
	g := gridFrag(t, gridRun(t, `<div class=g style="grid-template-columns:1fr;row-gap:7px">`+gridDivs(3, "a")+`</div>`))
	if !near(g.rect.H, 3*20+2*7) {
		t.Errorf("rows with gaps: height %v", g.rect.H)
	}
}

func TestGridEmptyAndOneItem(t *testing.T) {
	g := gridFrag(t, gridRun(t, `<div class=g style="grid-template-columns:1fr 1fr"></div>`))
	if g.rect.H != 0 || len(g.children) != 0 {
		t.Errorf("empty grid: %+v", g.rect)
	}
	g = gridFrag(t, gridRun(t, `<div class=g style="grid-template-columns:1fr 1fr">text only</div>`))
	if len(g.children) != 1 {
		t.Errorf("a text run is one item, got %d", len(g.children))
	}
}

func TestGridFragmentation(t *testing.T) {
	pages := gridRun(t, `<div class=g style="grid-template-columns:1fr 1fr">`+gridDivs(400, "r")+`</div>`)
	if len(pages) != 7 {
		t.Fatalf("%d pages, want 7 (200 rows of 20px, 30 per 600px page)", len(pages))
	}
	rows := 0
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Style.Display != style.DisplayGrid {
				return
			}
			for _, c := range f.children {
				if !c.first || !c.last {
					t.Errorf("item split between pages: %+v", c.rect)
				}
				if c.rect.Y+c.rect.H > 600+1e-6 {
					t.Errorf("item overflows its page: %+v", c.rect)
				}
			}
			rows += len(f.children) / 2
		})
	}
	if rows != 200 {
		t.Errorf("%d rows placed, want 200", rows)
	}
	tall := gridRun(t, `<div class=g style="grid-template-columns:1fr"><div style="line-height:100px">`+strings.Repeat("x<br>", 11)+`x</div></div>`)
	if len(tall) != 2 {
		t.Errorf("a 1200px row on 600px pages: %d pages, want 2", len(tall))
	}
	text := ""
	for _, ln := range lineTexts(tall) {
		text += ln
	}
	if strings.Count(text, "x") != 12 {
		t.Errorf("a sliced row lost text: %q", text)
	}
}

func TestGridRowSlicedOverPagesIsLaidOutOnce(t *testing.T) {
	html := `<div class=g style="grid-template-columns:1fr 1fr"><div style="line-height:100px">` + strings.Repeat("word<br>", 17) + `x</div><div>y</div></div>`
	glyphs := func(extra string) (pages int, used int64) {
		r := newRun(t, html, opts{extra: extra, css: gridCSS})
		return len(r.all(t)), r.b.Used(budget.Glyphs)
	}
	pages, sliced := glyphs(smallPage)
	onePage, whole := glyphs("@page { size: 800px 100000px; margin: 0 }")
	if pages != 3 || onePage != 1 {
		t.Fatalf("%d pages sliced, %d whole", pages, onePage)
	}
	if sliced != whole {
		t.Errorf("%d glyphs shaped for the sliced row, %d for the same row on one page", sliced, whole)
	}
}

func TestGridBounded(t *testing.T) {
	maxTracks := style.MaxGridTracks
	type bound struct {
		name    string
		tracks  string
		items   int
		columns int
	}
	for _, tt := range []bound{
		{"1000 tracks", fmt.Sprintf("repeat(%d, 1fr)", maxTracks), 1000, 0},
		{"1001 tracks are clamped", fmt.Sprintf("repeat(%d, 1fr)", maxTracks+1), 1001, maxTracks},
		{"100000 tracks are clamped", "repeat(100000, 1fr)", 1001, maxTracks},
		{"10000 items in 4 columns", "repeat(4, 1fr)", 10000, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			items := gridDivs(tt.items, "x")
			r := newRun(t, `<div class=g style="grid-template-columns:`+tt.tracks+`">`+items+`</div>`, opts{extra: smallPage, css: gridCSS})
			pages := r.all(t)
			g := gridFrag(t, pages)
			if tt.columns > 0 {
				first := 0
				for _, c := range g.children {
					if c.rect.Y == g.children[0].rect.Y {
						first++
					}
				}
				if first != tt.columns {
					t.Errorf("%d columns, want %d", first, tt.columns)
				}
			}
			placed := 0
			for _, pg := range pages {
				walk(pg.root, func(f *frag) {
					if f.box.Style.Display == style.DisplayGrid {
						placed += len(f.children)
					}
				})
			}
			if placed != tt.items {
				t.Errorf("%d items placed, want %d", placed, tt.items)
			}
			twin := newRun(t, `<div>`+items+`</div>`, opts{extra: smallPage, css: gridCSS})
			twin.all(t)
			if got, ref := r.b.Used(budget.Work), twin.b.Used(budget.Work); got > 4*ref+int64(tt.items) {
				t.Errorf("work %d against %d for the same items in a block", got, ref)
			}
		})
	}
}

// Positions measured on chrome09 (Chrome at the production parameters) for the
// first page of 01-modern small, in PDF points, as pdftotext -bbox reports them.
func TestGridMatchesChromeOn01(t *testing.T) {
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "01-modern", testcorpus.Small)
	if err != nil {
		t.Fatal(err)
	}
	r := newRun(t, string(html), opts{extra: testcorpus.ChromePaperCSS, scale: 0.9})
	pg := r.all(t)[0]
	pt := func(x float64) float64 { return (pg.Area.X + x*pg.Scale) * 0.75 }
	var grids []*frag
	walk(pg.root, func(f *frag) {
		if f.box.Style.Display == style.DisplayGrid {
			grids = append(grids, f)
		}
	})
	if len(grids) < 2 {
		t.Fatalf("%d grid containers on page 1", len(grids))
	}
	firstText := func(f *frag) float64 {
		x := math.NaN()
		walk(f, func(c *frag) {
			if math.IsNaN(x) && len(c.lines) > 0 && len(c.lines[0].items) > 0 && c.lines[0].items[0].text != nil {
				x = c.lines[0].items[0].text.X
			}
		})
		return x
	}
	kpis := grids[0]
	wantKPI := []float64{50.623826, 182.259367, 313.905456, 445.551544}
	if len(kpis.children) != 4 {
		t.Fatalf("%d KPI boxes", len(kpis.children))
	}
	for i, c := range kpis.children {
		if got := pt(firstText(c)); math.Abs(got-wantKPI[i]) > 0.5 {
			t.Errorf("KPI %d value at %.2f pt, Chrome %.2f", i, got, wantKPI[i])
		}
		if !near(c.rect.H, kpis.children[0].rect.H) {
			t.Errorf("KPI %d is %v tall, the first %v", i, c.rect.H, kpis.children[0].rect.H)
		}
	}
	facts := grids[1]
	if got, want := pt(firstText(facts.children[0])), 39.749998; math.Abs(got-want) > 0.5 {
		t.Errorf("first dt at %.2f pt, Chrome %.2f", got, want)
	}
	if got, want := pt(firstText(facts.children[1])), 183.683199; math.Abs(got-want) > 0.5 {
		t.Errorf("first dd at %.2f pt, Chrome %.2f", got, want)
	}
}

func BenchmarkGrid10000Items(b *testing.B) {
	html := `<div class=g style="grid-template-columns:repeat(4,1fr)">` + gridDivs(10000, "x") + `</div>`
	b.ReportAllocs()
	for b.Loop() {
		newRun(b, html, opts{extra: smallPage, css: gridCSS}).all(b)
	}
}

func BenchmarkGrid1000Tracks(b *testing.B) {
	html := `<div class=g style="grid-template-columns:repeat(1000,1fr)">` + gridDivs(1000, "x") + `</div>`
	b.ReportAllocs()
	for b.Loop() {
		newRun(b, html, opts{extra: smallPage, css: gridCSS}).all(b)
	}
}
