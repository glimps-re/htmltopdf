package layout

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
)

type svgRes struct {
	frag  *frag
	paths []display.Path
	clip  []display.PushClip
}

func svgLayout(t testing.TB, htmlStr string) svgRes {
	t.Helper()
	r := newRun(t, htmlStr, opts{extra: smallPage})
	pages := r.all(t)
	var res svgRes
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Kind == box.KindSVG && res.frag == nil {
				res.frag = f
			}
		})
		for _, it := range pg.Items {
			switch v := it.(type) {
			case display.Path:
				res.paths = append(res.paths, v)
			case display.PushClip:
				res.clip = append(res.clip, v)
			}
		}
	}
	return res
}

func bbox(p display.Path) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, s := range p.Segs {
		n := map[uint8]int{display.PathMoveTo: 1, display.PathLineTo: 1, display.PathCubicTo: 3}[s.Op]
		for k := range n {
			x0, x1 = math.Min(x0, s.P[k].X), math.Max(x1, s.P[k].X)
			y0, y1 = math.Min(y0, s.P[k].Y), math.Max(y1, s.P[k].Y)
		}
	}
	return
}

func TestSVGSizes(t *testing.T) {
	const vb = `viewBox="0 0 24 12"><path d="M0 0 L24 12" stroke="red"/>`
	tests := []struct {
		name, css, attrs string
		w, h             float64
	}{
		{"both", "svg{width:60px;height:40px}", vb, 60, 40},
		{"width and the ratio", "svg{width:60px}", vb, 60, 30},
		{"height and the ratio", "svg{height:30px}", vb, 60, 30},
		{"viewBox only: the container's width", "", vb, 784, 392},
		{"nothing: 300x150", "", `><path d="M0 0 L9 9" stroke="red"/>`, 300, 150},
		{"width and height attributes", "", `width="16" height="12"><path d="M0 0 L9 9" stroke="red"/>`, 16, 12},
		{"CSS wins per axis", "svg{width:20px}", `width="50" height="50"><path d="M0 0 L9 9" stroke="red"/>`, 20, 50},
		{"width attribute and the ratio", "", `width="48" viewBox="0 0 24 12"><path d="M0 0 L9 9" stroke="red"/>`, 48, 24},
		{"percentage attribute", "p{width:400px}", `width="50%" height="10"><path d="M0 0 L9 9" stroke="red"/>`, 200, 10},
		{"width 0", "", `width="0" height="10"><path d="M0 0 L9 9" stroke="red"/>`, 0, 10},
		{"degenerate viewBox is ignored", "", `width="50" viewBox="0 0 1e-9 1e-9"><path d="M0 0 L9 9" stroke="red"/>`, 50, 150},
		{"max-width and auto margins on a block", "svg{display:block;margin:0 auto;max-width:100px}", `width="200" height="40"><path d="M0 0 L9 9" stroke="red"/>`, 100, 40},
		{"width, no viewBox: 150 high", "svg{width:60px}", `><path d="M0 0 L9 9" stroke="red"/>`, 60, 150},
		{"height, no viewBox: 300 wide", "svg{height:20px}", `><path d="M0 0 L9 9" stroke="red"/>`, 300, 20},
		{"padding and border are outside", "svg{width:60px;height:30px;padding:5px;border:2px solid red}", vb, 74, 44},
		{"display block keeps its size", "svg{display:block;width:60px}", vb, 60, 30},
		{"flex item", "div{display:flex} svg{width:60px}", vb, 60, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `<p>a <svg ` + tt.attrs + `</svg> b</p>`
			if strings.Contains(tt.css, "flex") {
				body = `<div><svg ` + tt.attrs + `</svg></div>`
			}
			res := svgLayout(t, `<style>`+tt.css+`</style>`+body)
			if res.frag == nil {
				t.Fatal("no svg fragment")
			}
			if r := res.frag.rect; math.Abs(r.W-tt.w) > 1e-6 || math.Abs(r.H-tt.h) > 1e-6 {
				t.Errorf("border box %vx%v, want %vx%v", r.W, r.H, tt.w, tt.h)
			}
		})
	}
}

func TestSVGInlineBaselineIsTheBottomMarginEdge(t *testing.T) {
	r := newRun(t, `<style>svg{width:40px;height:40px;margin-bottom:6px}</style><p>text <svg viewBox="0 0 1 1"><path d="M0 0 L1 1" stroke="red"/></svg> text</p>`, opts{extra: smallPage})
	seen := false
	for _, pg := range r.all(t) {
		walk(pg.root, func(f *frag) {
			for _, ln := range f.lines {
				for _, it := range ln.items {
					if it.atom != nil && it.atom.f.box.Kind == box.KindSVG {
						seen = true
						if got, want := it.atom.f.rect.Bottom()+6, ln.baseline; math.Abs(got-want) > 1e-6 {
							t.Errorf("margin edge at %v, baseline %v", got, want)
						}
					}
				}
			}
		})
	}
	if !seen {
		t.Fatal("svg not on a line")
	}
}

func TestSVGPathsMapViewBoxToContentBox(t *testing.T) {
	// The 02 logo: viewBox 24, 22 mm wide (83.1 px), a path from (3.5, 2) to (12, 11.7).
	res := svgLayout(t, `<style>svg{width:22mm;height:22mm}</style><svg viewBox="0 0 24 24" fill="none" stroke="#f9c105" stroke-width="1.6"><path d="M3.5 2 H12 V11.7"/></svg>`)
	if res.frag == nil || len(res.paths) != 1 {
		t.Fatalf("%v, %d paths", res.frag, len(res.paths))
	}
	r := res.frag.rect
	s := r.W / 24
	x0, y0, x1, y1 := bbox(res.paths[0])
	for i, c := range [][2]float64{{x0, r.X + 3.5*s}, {y0, r.Y + 2*s}, {x1, r.X + 12*s}, {y1, r.Y + 11.7*s}} {
		if math.Abs(c[0]-c[1]) > 0.7/0.75 {
			t.Errorf("edge %d at %v, want %v", i, c[0], c[1])
		}
	}
	if p := res.paths[0]; math.Abs(p.Width-1.6*s) > 1e-9 || p.Fill.A != 0 || p.Stroke.A != 1 {
		t.Errorf("paint %+v", p)
	}
	if len(res.clip) == 0 || res.clip[len(res.clip)-1].Rect != r {
		t.Errorf("clip %+v, box %+v", res.clip, r)
	}
}

func TestSVGMeetCentersTheViewBox(t *testing.T) {
	res := svgLayout(t, `<style>svg{width:100px;height:50px}</style><svg viewBox="0 0 10 10"><path d="M0 0 L10 10" stroke="red"/></svg>`)
	r := res.frag.rect
	x0, y0, x1, y1 := bbox(res.paths[0])
	if math.Abs(x0-(r.X+25)) > 1e-6 || math.Abs(x1-(r.X+75)) > 1e-6 || math.Abs(y0-r.Y) > 1e-6 || math.Abs(y1-(r.Y+50)) > 1e-6 {
		t.Errorf("bbox (%v,%v)-(%v,%v) in %+v", x0, y0, x1, y1, r)
	}
}

func TestSVGCurrentColorTakesTheBoxColor(t *testing.T) {
	res := svgLayout(t, `<style>svg{color:rgb(255,0,0);width:20px;height:20px}</style>`+
		`<svg viewBox="0 0 1 1" fill="currentColor" stroke="currentColor"><path d="M0 0 L1 1 L0 1z"/></svg>`+
		`<svg viewBox="0 0 1 1"><path d="M0 0 L1 1 L0 1z"/></svg>`)
	if len(res.paths) != 2 {
		t.Fatalf("%d paths", len(res.paths))
	}
	red := css.Color{R: 1, A: 1}
	if p := res.paths[0]; p.Fill != red || p.Stroke != red {
		t.Errorf("currentColor painted %+v / %+v, want the box color", p.Fill, p.Stroke)
	}
	if p := res.paths[1]; p.Fill != (css.Color{A: 1}) || p.Stroke.A != 0 {
		t.Errorf("default paint %+v / %+v: black fill, no stroke", p.Fill, p.Stroke)
	}
	for _, p := range res.paths {
		if p.Fill == (css.Color{A: 1}) && p.Stroke == (css.Color{A: 1}) {
			t.Error("the black placeholder leaked")
		}
	}
}

func TestSVGWithoutShapesDrawsNothing(t *testing.T) {
	res := svgLayout(t, `<svg width=10><text>t</text></svg>`)
	if res.frag == nil || len(res.paths) != 0 || len(res.clip) != 0 {
		t.Errorf("%v %d %d", res.frag, len(res.paths), len(res.clip))
	}
}

func TestSVGWorkScalesLinearly(t *testing.T) {
	icons := func(n int) string {
		return `<style>svg{width:10px;height:10px}</style><p>` + strings.Repeat(`<svg viewBox="0 0 24 24"><path d="M0 0 L24 24" stroke="red"/></svg> `, n) + `</p>`
	}
	work := func(n int) int64 {
		r := newRun(t, icons(n), opts{extra: smallPage, limits: func(l *budget.Limits) {
			l[budget.Nodes] = 1 << 20
			l[budget.Boxes] = 1 << 20
			l[budget.Work] = 1 << 30
			l[budget.InputBytes] = 64 << 20
		}})
		r.all(t)
		return r.b.Used(budget.Work)
	}
	small, big := work(5000), work(10000)
	if big > 2*small {
		t.Errorf("10 000 icons: Work %d, 5 000: %d", big, small)
	}
}

func TestBlockSVGIsNeverSliced(t *testing.T) {
	body := func(h int) string {
		return fmt.Sprintf(`<div style="height:500px"></div><svg style="display:block;width:100px;height:%dpx" viewBox="0 0 1 1"><path d="M0 0 L1 1" stroke="red"/></svg><p>after</p>`, h)
	}
	page := "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
	pages := newRun(t, body(200), opts{extra: page}).all(t)
	var onPage []int
	for i, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Kind == box.KindSVG {
				onPage = append(onPage, i)
				if f.rect.H != 200 || f.rect.Y != 0 {
					t.Errorf("svg fragment %+v on page %d: it moves whole to the next page", f.rect, i+1)
				}
			}
		})
	}
	if len(onPage) != 1 || onPage[0] != 1 {
		t.Errorf("svg fragments on pages %v, want one on page 2", onPage)
	}
	// Taller than a page: it stays whole where it starts and the page clips it.
	pages = newRun(t, body(2000), opts{extra: page}).all(t)
	count := 0
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Kind == box.KindSVG {
				count++
				if f.rect.H != 2000 {
					t.Errorf("svg height %v", f.rect.H)
				}
			}
		})
	}
	if count != 1 {
		t.Errorf("%d svg fragments for a box taller than a page", count)
	}
}

func TestSVGOpacityFadesExplicitPaintsOnce(t *testing.T) {
	res := svgLayout(t, `<style>svg{opacity:.5;color:rgb(255,0,0);width:20px;height:20px}</style>`+
		`<svg viewBox="0 0 1 1" fill="#00f" stroke="currentColor"><path d="M0 0 L1 1 L0 1z"/></svg>`)
	if len(res.paths) != 1 {
		t.Fatalf("%d paths", len(res.paths))
	}
	p := res.paths[0]
	if math.Abs(p.Fill.A-0.5) > 1e-9 {
		t.Errorf("explicit fill alpha %v, want 0.5", p.Fill.A)
	}
	if math.Abs(p.Stroke.A-0.5) > 1e-9 {
		t.Errorf("currentColor stroke alpha %v: faded once by the color itself, not twice", p.Stroke.A)
	}
}

// The logo of the 02 cover, in the page of the Chrome probe (chrome_positioned.txt,
// "02 stamp: cover padding-box check with long meta"): its box and the bounding
// box of its path within 0.5 px of Chrome's box and the viewBox mapping.
func TestSVGLogoPathMatchesChromeProbe(t *testing.T) {
	var c sizingCase
	for _, k := range loadChromeCases(t, "testdata/chrome_positioned.txt") {
		if strings.HasPrefix(k.Name, "02 stamp: cover padding-box") {
			c = k
		}
	}
	if c.Name == "" {
		t.Fatal("probe case not found")
	}
	page := fmt.Sprintf("@page { size: %fpx 1200px; margin: 0 } body { margin: 0 }", c.W)
	pages := newRun(t, `<div style="overflow:hidden">`+c.HTML+`</div>`, opts{extra: page, css: templateCSS(t) + "\n" + c.CSS}).all(t)
	var f *frag
	for _, pg := range pages {
		walk(pg.root, func(x *frag) {
			if x.box.Kind == box.KindSVG && f == nil {
				f = x
			}
		})
	}
	if f == nil {
		t.Fatal("no svg")
	}
	want := c.M["logo"]
	got := []float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 0.5 {
			t.Fatalf("logo box %.3f, Chrome %.3f", got, want)
		}
	}
	var path display.Path
	for _, pg := range pages {
		for _, it := range pg.Items {
			if p, ok := it.(display.Path); ok && path.Segs == nil {
				path = p
			}
		}
	}
	x0, y0, x1, y1 := bbox(path)
	s := want[2] / 24
	// d="M12 2 3.5 5.5v6.2": x 3.5..12, y 2..11.7 in the 24 px viewBox.
	for i, e := range [][2]float64{{x0, want[0] + 3.5*s}, {y0, want[1] + 2*s}, {x1, want[0] + 12*s}, {y1, want[1] + 11.7*s}} {
		if math.Abs(e[0]-e[1]) > 0.5 {
			t.Errorf("path edge %d at %.3f, want %.3f", i, e[0], e[1])
		}
	}
}
