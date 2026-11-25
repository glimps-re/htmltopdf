package paint

import (
	"bytes"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

var boundsRE = regexp.MustCompile(`/Bounds \[([^\]]*)\]`)

// gradientBounds returns the /Bounds arrays of every stitching function of the
// PDF bytes.
func gradientBounds(tb testing.TB, data []byte) [][]float64 {
	tb.Helper()
	var out [][]float64
	for _, m := range boundsRE.FindAllSubmatch(data, -1) {
		var b []float64
		for _, f := range strings.Fields(string(m[1])) {
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				tb.Fatalf("bounds %q: %v", m[1], err)
			}
			b = append(b, v)
		}
		out = append(out, b)
	}
	return out
}

func stopsAt(c1, c2 css.Color, pos ...float64) css.Gradient {
	g := css.Gradient{}
	for i, p := range pos {
		c := c1
		if i%2 == 1 {
			c = c2
		}
		g.Stops = append(g.Stops, css.ColorStop{Color: c, Pos: p})
	}
	return g
}

func gradientBox(g css.Gradient) display.FillGradient {
	return display.FillGradient{Rect: display.Rect{X: 10, Y: 10, W: 200, H: 100}, Gradient: g}
}

func TestOneShadingForIdenticalGradients(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for i := range 1000 {
		item := gradientBox(stopsAt(red, blue, 0, 1))
		item.Rect.Y = float64(i % 800)
		item.Gradient.Angle = float64(i % 360) // the angle is the matrix, not the shading
		l = append(l, item)
	}
	got, _ := e.page(t, a4, l)
	if n := strings.Count(got, " sh\n"); n != 1000 {
		t.Fatalf("%d shading operators", n)
	}
	balanced(t, got)
	doc := e.finish(t)
	_ = doc
	if n := bytes.Count(e.buf.Bytes(), []byte("/ShadingType 2")); n != 1 {
		t.Fatalf("%d shading objects for 1000 identical gradients, want 1", n)
	}
}

func TestShadingLimitIsClampedWithAWarning(t *testing.T) {
	e := newEnv(t)
	var l display.List
	n := pdf.MaxShadings + 1
	for i := range n {
		pos := 0.001 + float64(i%1000)/2000
		g := stopsAt(css.Color{R: float64(i%16) / 15, A: 1}, css.Color{B: float64(i/16%16) / 15, G: float64(i/256) / 16, A: 1}, 0, pos, 1)
		it := gradientBox(g)
		it.Rect = display.Rect{X: float64(i % 700), Y: float64(i / 7 % 900), W: 20, H: 20}
		l = append(l, it)
	}
	got, _ := e.page(t, a4, l)
	balanced(t, got)
	if shades := strings.Count(got, " sh\n"); shades > pdf.MaxShadings {
		t.Errorf("%d shadings drawn, limit %d", shades, pdf.MaxShadings)
	}
	ws, _ := e.b.Warnings()
	clamped := 0
	for _, w := range ws {
		if w.Code == budget.WarnClamped {
			clamped++
			if !strings.Contains(w.Detail, "4096") && !strings.Contains(w.Detail, "gradient") {
				t.Errorf("warning detail %q", w.Detail)
			}
		}
	}
	if clamped != 1 {
		t.Errorf("%d clamped warnings, want exactly one: %+v", clamped, ws)
	}
	// The gradient that did not fit still paints its box: one more fill than
	// shadings drawn (the background drawn in place).
	if fills := strings.Count(got, "\nf\n"); fills < pdf.MaxShadings+1-1 && strings.Count(got, " sh\n") == pdf.MaxShadings && !strings.Contains(got, " rg\n") {
		t.Errorf("the 4097th gradient leaves its box empty")
	}
	e.finish(t)
}

// Hard stops and stops 1e-5 apart: every /Bounds array is strictly increasing,
// inside (0, 1).
func TestShadingBoundsStrictlyIncrease(t *testing.T) {
	sets := map[string][]float64{
		"hard 0.5/0.5":       {0, 0.5, 0.5, 1},
		"triple 0.3":         {0, 0.3, 0.3, 0.3, 1},
		"1e-5 apart":         {0, 0.5, 0.5 + 1e-5, 1},
		"below 1e-5":         {0, 0.5, 0.5 + 1e-7, 0.5 + 2e-7, 1},
		"all at 0":           {0, 0, 0},
		"all at 1":           {1, 1, 1},
		"start 0 0 then 1":   {0, 0, 1},
		"1 1 at the end":     {0, 1, 1},
		"decreasing":         {0.8, 0.2, 0.5},
		"out of range":       {-5, 0.5, 7},
		"hard at the end":    {0, 1 - 1e-9, 1},
		"hard at the start":  {1e-9, 1e-9, 1},
		"many equal":         make([]float64, 64),
		"Inf positions":      {math.Inf(-1), 0.5, math.Inf(1)},
		"alternating hard":   {0, 0.25, 0.25, 0.5, 0.5, 0.75, 0.75, 1},
		"dense below 1e-5":   denseStops(64, 1e-7),
		"dense at 1e-5":      denseStops(64, 1e-5),
		"64 on the far end":  denseStops(64, 1e-4),
		"stops at 0.999999x": {0.99999, 0.999995, 1},
	}
	for i := range sets["many equal"] {
		sets["many equal"][i] = 0.5
	}
	for name, pos := range sets {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.page(t, a4, display.List{gradientBox(stopsAt(red, blue, pos...))})
			doc := e.finish(t)
			_ = doc
			all := gradientBounds(t, e.buf.Bytes())
			if len(all) == 0 {
				t.Fatal("no stitching function")
			}
			for _, b := range all {
				for i, v := range b {
					if !(v > 0 && v < 1) || (i > 0 && !(v > b[i-1])) {
						t.Fatalf("bounds %v not strictly increasing inside (0, 1)", b)
					}
				}
			}
		})
	}
}

func denseStops(n int, step float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 0.5 + float64(i)*step
	}
	return out
}

var upstreamBug = map[string]bool{"NaN position": true, "NaN color": true}

// NaN and infinite angles, positions and boxes, radii bigger than the box,
// borders wider than the box: valid output or a documented warning, never a
// panic or a broken file.
func TestHostileGradientGeometry(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	rect := display.Rect{X: 10, Y: 10, W: 200, H: 100}
	bigRadii := display.Radii{{H: 1e6, V: 1e6}, {H: 1e6, V: 1e6}, {H: 1e6, V: 1e6}, {H: 1e6, V: 1e6}}
	odd := display.Radii{{H: 150, V: 10}, {H: 150, V: 90}, {H: 10, V: 150}, {H: 90, V: 150}}
	g := stopsAt(red, blue, 0, 1)
	cases := map[string]display.List{
		"NaN angle":            {display.FillGradient{Rect: rect, Gradient: css.Gradient{Angle: nan, Stops: g.Stops}}},
		"Inf angle":            {display.FillGradient{Rect: rect, Gradient: css.Gradient{Angle: inf, Stops: g.Stops}}},
		"huge angle":           {display.FillGradient{Rect: rect, Gradient: css.Gradient{Angle: 1e300, Stops: g.Stops}}},
		"NaN position":         {gradientBox(css.Gradient{Stops: []css.ColorStop{{Color: red, Pos: nan}, {Color: blue, Pos: 1}}})},
		"Inf position":         {gradientBox(css.Gradient{Stops: []css.ColorStop{{Color: red, Pos: -inf}, {Color: blue, Pos: inf}}})},
		"NaN color":            {gradientBox(css.Gradient{Stops: []css.ColorStop{{Color: css.Color{R: nan, A: 1}}, {Color: blue, Pos: 1}}})},
		"zero width":           {display.FillGradient{Rect: display.Rect{X: 1, Y: 1, W: 0, H: 50}, Gradient: g}},
		"zero height":          {display.FillGradient{Rect: display.Rect{X: 1, Y: 1, W: 50, H: 0}, Gradient: g}},
		"negative size":        {display.FillGradient{Rect: display.Rect{X: 1, Y: 1, W: -50, H: -5}, Gradient: g}},
		"NaN rect":             {display.FillGradient{Rect: display.Rect{X: nan, Y: 1, W: 50, H: 5}, Gradient: g}},
		"huge rect":            {display.FillGradient{Rect: display.Rect{X: -1e30, Y: -1e30, W: 2e30, H: 2e30}, Gradient: g}},
		"tiny rect":            {display.FillGradient{Rect: display.Rect{X: 1, Y: 1, W: 1e-12, H: 1e-12}, Gradient: g}},
		"radii over the box":   {display.FillGradient{Rect: rect, Radii: bigRadii, Gradient: g}},
		"odd radii":            {display.FillGradient{Rect: rect, Radii: odd, Gradient: g}},
		"NaN radii":            {display.FillGradient{Rect: rect, Radii: display.Radii{{H: nan, V: 3}, {H: 3, V: nan}, {H: -4, V: 4}, {H: inf, V: inf}}, Gradient: g}},
		"fill radii":           {display.FillRect{Rect: rect, Radii: bigRadii, Color: red}},
		"border wider":         {display.Border{Rect: rect, Sides: [4]display.BorderSide{{Width: 500, Color: red}, {Width: 500, Color: blue}, {Width: 500, Color: red}, {Width: 500, Color: blue}}}},
		"border wider rounded": {display.Border{Rect: rect, Radii: bigRadii, Sides: [4]display.BorderSide{{Width: 300, Color: red}, {Width: 1, Color: blue}, {Width: 300, Color: red}, {Width: 1, Color: blue}}}},
		"border 0 and huge":    {display.Border{Rect: rect, Radii: odd, Sides: [4]display.BorderSide{{Width: 1e30, Color: red}, {}, {Width: 1e-30, Color: red}, {Width: 3, Color: blue}}}},
		"clip radii over":      {display.PushClip{Rect: rect, Radii: bigRadii}, display.FillGradient{Rect: rect, Radii: odd, Gradient: g}, display.PopClip{}},
		"gradient in clips":    nil,
	}
	var nested display.List
	for range MaxClipDepth + 10 {
		nested = append(nested, display.PushClip{Rect: display.Rect{W: 700, H: 900}, Radii: odd})
		nested = append(nested, gradientBox(g))
	}
	for range MaxClipDepth + 10 {
		nested = append(nested, display.PopClip{})
	}
	cases["gradient in clips"] = nested
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			c := &pdf.Content{}
			links, err := e.painter.Page(c, a4, l)
			if upstreamBug[name] {
				// css gradients are finite by construction: a NaN position or
				// color reaching paint is an upstream bug and fails loudly.
				if !errors.Is(err, pdf.ErrGradient) && !errors.Is(err, pdf.ErrNumber) {
					t.Fatalf("err = %v, want pdf.ErrGradient or pdf.ErrNumber", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Page: %v", err)
			}
			got := string(c.Bytes())
			balanced(t, got)
			if strings.ContainsAny(got, "\x00") || strings.Contains(got, "NaN") || strings.Contains(got, "Inf") || strings.Contains(got, "e+") {
				t.Fatalf("bad number in the content:\n%.400s", got)
			}
			if _, err := e.w.AddPage(600, 750, c, false, links); err != nil {
				t.Fatalf("AddPage: %v", err)
			}
			e.finish(t)
		})
	}
}

// 200 clips with gradients inside: q and Q balance, pdfcpu validates.
func TestGradientsInsideTheDeepestClips(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for i := range MaxClipDepth {
		l = append(l, display.PushClip{Rect: display.Rect{X: float64(i), Y: float64(i), W: 700, H: 800}})
		l = append(l, display.FillGradient{
			Rect:     display.Rect{X: float64(i), Y: float64(i), W: 100, H: 100},
			Radii:    display.Radii{{H: 10, V: 10}, {H: 10, V: 10}, {H: 10, V: 10}, {H: 10, V: 10}},
			Gradient: css.Gradient{Angle: float64(i), Stops: stopsAt(red, blue, 0, 0.5, 1).Stops},
		})
	}
	for range MaxClipDepth {
		l = append(l, display.PopClip{})
	}
	got, _ := e.page(t, a4, l)
	if saves := balanced(t, got); saves < MaxClipDepth {
		t.Errorf("%d saves", saves)
	}
	e.finish(t)
}

func TestTranslucentGradientsShareMasks(t *testing.T) {
	e := newEnv(t)
	half := css.Color{R: 1, A: 0.5}
	got, _ := e.page(t, a4, display.List{
		gradientBox(stopsAt(half, blue, 0, 1)),
		gradientBox(stopsAt(half, blue, 0, 1)),
		gradientBox(stopsAt(red, css.Color{}, 0, 1)),
	})
	balanced(t, got)
	if n := strings.Count(got, " gs\n/Sh"); n != 3 {
		t.Errorf("%d soft masks set, want 3:\n%s", n, got)
	}
	if ws, _ := e.b.Warnings(); len(ws) != 0 {
		t.Errorf("warnings %+v", ws)
	}
	e.finish(t)
}

// Random gradient items among other items: valid output every time.
func TestRandomGradientLists(t *testing.T) {
	rounds := 400
	if testing.Short() {
		rounds = 60
	}
	r := prng(0xD1B54A32D192ED03)
	failed := 0
	defer func() { t.Logf("%d of %d lists failed loudly on a NaN stop", failed, rounds) }()
	for round := range rounds {
		e := newEnv(t)
		var l display.List
		for range 1 + r.n(12) {
			var stops []css.ColorStop
			for range 2 + r.n(6) {
				stops = append(stops, css.ColorStop{Color: css.Color{R: r.c(), G: r.c(), B: r.c(), A: []float64{0, 0.5, 1}[r.n(3)]}, Pos: r.f()})
			}
			l = append(l, display.FillGradient{
				Rect:     display.Rect{X: r.f(), Y: r.f(), W: r.f(), H: r.f()},
				Radii:    display.Radii{{H: r.f(), V: r.f()}, {H: r.f(), V: r.f()}, {H: r.f(), V: r.f()}, {H: r.f(), V: r.f()}},
				Gradient: css.Gradient{Angle: r.f(), Corner: []css.Corner{css.CornerNone, css.CornerTopRight, css.CornerBottomRight, css.CornerBottomLeft, css.CornerTopLeft}[r.n(5)], Stops: stops},
			})
		}
		c := &pdf.Content{}
		links, err := e.painter.Page(c, a4, l)
		if err != nil {
			// Only a NaN stop may fail the page, loudly (a degenerate box or an
			// invisible gradient is skipped before its stops are looked at).
			if !hasNaNStop(l) || (!errors.Is(err, pdf.ErrGradient) && !errors.Is(err, pdf.ErrNumber)) {
				t.Fatalf("round %d: %v", round, err)
			}
			failed++
			continue
		}
		balanced(t, string(c.Bytes()))
		if _, err := e.w.AddPage(600, 750, c, false, links); err != nil {
			t.Fatalf("round %d: AddPage: %v", round, err)
		}
		e.finish(t)
		for _, b := range gradientBounds(t, e.buf.Bytes()) {
			for i, v := range b {
				if !(v > 0 && v < 1) || (i > 0 && !(v > b[i-1])) {
					t.Fatalf("round %d: bounds %v", round, b)
				}
			}
		}
	}
}

// hasNaNStop reports whether a gradient of the list has a NaN position or a
// NaN color component that paint draws (a visible, non-degenerate gradient).
func hasNaNStop(l display.List) bool {
	for _, it := range l {
		g, ok := it.(display.FillGradient)
		if !ok {
			continue
		}
		for _, st := range g.Gradient.Stops {
			if math.IsNaN(st.Pos) || math.IsNaN(st.Color.R) || math.IsNaN(st.Color.G) || math.IsNaN(st.Color.B) {
				return true
			}
		}
	}
	return false
}
