package paint

import (
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

func grad(angle float64, corner css.Corner) css.Gradient {
	return css.Gradient{Angle: angle, Corner: corner, Stops: []css.ColorStop{{Color: red, Pos: 0}, {Color: blue, Pos: 1}}}
}

func TestGradientMatrix(t *testing.T) {
	r := ptRect{x: 10, y: 20, w: 200, h: 100}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
	for name, tc := range map[string]struct {
		angle  float64
		corner css.Corner
		a, b   float64 // direction * length
		e, f   float64 // start
	}{
		"to bottom": {angle: 180, a: 0, b: -100, e: 110, f: 120},
		"to top":    {angle: 0, a: 0, b: 100, e: 110, f: 20},
		"to right":  {angle: 90, a: 200, b: 0, e: 10, f: 70},
		"to left":   {angle: 270, a: -200, b: 0, e: 210, f: 70},
	} {
		m, ok := gradientMatrix(r, tc.angle, tc.corner)
		if !ok {
			t.Fatalf("%s: no matrix", name)
		}
		a, b, e, f := m[0], m[1], m[4], m[5]
		if !near(a, tc.a) || !near(b, tc.b) || !near(e, tc.e) || !near(f, tc.f) {
			t.Errorf("%s: got %v %v %v %v", name, a, b, e, f)
		}
	}
	// 45deg on a square: length is W*sin+H*cos = side*sqrt2.
	sq := ptRect{w: 100, h: 100}
	m, ok := gradientMatrix(sq, 45, css.CornerNone)
	if !ok || !near(math.Hypot(m[0], m[1]), 100*math.Sqrt2) {
		t.Errorf("45deg length %v", math.Hypot(m[0], m[1]))
	}
	// To a corner, the 50% line passes through the other two corners: on a square
	// "to top right" is the 45deg gradient.
	m2, ok := gradientMatrix(sq, 0, css.CornerTopRight)
	if !ok || !near(m2[0], m[0]) || !near(m2[1], m[1]) || !near(m2[4], 50-m[0]/2) || !near(m2[5], 50-m[1]/2) {
		t.Errorf("to top right on a square: %v", m2)
	}
	for _, c := range []css.Corner{css.CornerBottomRight, css.CornerBottomLeft, css.CornerTopLeft} {
		m, ok := gradientMatrix(r, 0, c)
		if !ok || math.IsNaN(m[0]) || math.IsNaN(m[1]) {
			t.Errorf("corner %v: %v", c, m)
		}
	}
	if _, ok := gradientMatrix(ptRect{}, 0, css.CornerTopRight); ok {
		t.Error("empty rect has a gradient line")
	}
	if _, ok := gradientMatrix(r, math.NaN(), css.CornerNone); ok {
		t.Error("NaN angle has a gradient line")
	}
}

func TestFillGradientPainted(t *testing.T) {
	e := newEnv(t)
	radii := display.Radii{{H: 20, V: 10}, {H: 20, V: 10}, {H: 20, V: 10}, {H: 20, V: 10}}
	content, _ := e.page(t, a4, display.List{
		display.FillGradient{Rect: display.Rect{X: 10, Y: 10, W: 300, H: 100}, Radii: radii, Gradient: grad(90, 0)},
		display.FillGradient{Rect: display.Rect{X: 10, Y: 200, W: 300, H: 100}, Gradient: grad(0, css.CornerTopRight)},
	})
	for _, want := range []string{"/Sh1 sh", " cm\n", "W n", " c\n"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in\n%s", want, content)
		}
	}
	if strings.Count(content, "/Sh1 sh") != 2 {
		t.Errorf("one shading should serve both boxes:\n%s", content)
	}
	e.finish(t)
}

func TestFillGradientSkipped(t *testing.T) {
	e := newEnv(t)
	clear := css.Color{}
	transparent := css.Gradient{Stops: []css.ColorStop{{Color: clear}, {Color: clear, Pos: 1}}}
	one := css.Gradient{Stops: []css.ColorStop{{Color: red}}}
	content, _ := e.page(t, a4, display.List{
		display.FillGradient{Rect: display.Rect{W: 0, H: 10}, Gradient: grad(0, 0)},
		display.FillGradient{Rect: display.Rect{X: -5000, Y: 0, W: 10, H: 10}, Gradient: grad(0, 0)},
		display.FillGradient{Rect: display.Rect{W: 10, H: 10}, Gradient: transparent},
		display.FillGradient{Rect: display.Rect{W: 10, H: 10}, Gradient: one},
	})
	if strings.Contains(content, " sh") {
		t.Fatalf("nothing should be painted:\n%s", content)
	}
	e.finish(t)
}

func TestFillGradientTranslucentUsesASoftMask(t *testing.T) {
	e := newEnv(t)
	g := css.Gradient{Stops: []css.ColorStop{{Color: css.Color{R: 1, A: 0.5}}, {Color: blue, Pos: 1}}}
	content, _ := e.page(t, a4, display.List{display.FillGradient{Rect: display.Rect{W: 50, H: 50}, Gradient: g}})
	if !strings.Contains(content, "/SM1 gs\n/Sh1 sh") {
		t.Fatalf("gradient not painted through its soft mask:\n%s", content)
	}
	if ws, _ := e.b.Warnings(); len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	e.finish(t)
}

func TestEllipticalRadiiScaleDown(t *testing.T) {
	s := &pageState{g: Geometry{Scale: 1}}
	r := ptRect{w: 100, h: 100}
	// Radii of 100 px on each side overflow the box: all scale by the same factor.
	c := s.cornersOf(r, display.Radii{{H: 100, V: 50}, {H: 100, V: 50}, {H: 100, V: 50}, {H: 100, V: 50}})
	for i := range c.rx {
		if math.Abs(c.rx[i]-c.rx[0]) > 1e-9 || c.rx[i]+c.rx[(i+1)%4] > 100+1e-9 && i%2 == 0 {
			t.Fatalf("corners %v", c)
		}
	}
	if c.rx[0]*2 > r.w+1e-9 || c.ry[0]*2 > r.h+1e-9 {
		t.Fatalf("radii exceed the box: %v", c)
	}
	in := c.inner([4]float64{10, 10, 10, 10})
	if in.rx[0] != math.Max(0, c.rx[0]-10) || in.ry[0] != math.Max(0, c.ry[0]-10) {
		t.Fatalf("inner radii %v", in)
	}
	hostile := s.cornersOf(r, display.Radii{{H: math.NaN(), V: -4}, {H: math.Inf(1), V: 1e300}})
	for i := range hostile.rx {
		if math.IsNaN(hostile.rx[i]) || math.IsNaN(hostile.ry[i]) || hostile.rx[i] < 0 || hostile.ry[i] < 0 {
			t.Fatalf("hostile radii %v", hostile)
		}
	}
}

func TestRoundedFillsBordersAndClips(t *testing.T) {
	e := newEnv(t)
	radii := display.Radii{{H: 30, V: 15}, {H: 30, V: 15}, {H: 5, V: 5}, {H: 0, V: 0}}
	side := func(w float64, c css.Color) display.BorderSide { return display.BorderSide{Width: w, Color: c} }
	box := display.Rect{X: 20, Y: 20, W: 300, H: 120}
	content, _ := e.page(t, a4, display.List{
		display.FillRect{Rect: box, Radii: radii, Color: red},
		display.Border{Rect: box, Radii: radii, Sides: [4]display.BorderSide{side(4, black), side(4, black), side(4, black), side(4, black)}},
		display.Border{Rect: display.Rect{X: 20, Y: 200, W: 300, H: 120}, Radii: radii, Sides: [4]display.BorderSide{side(4, black), side(10, red), side(0, blue), side(20, blue)}},
		display.Border{Rect: display.Rect{X: 20, Y: 400, W: 30, H: 30}, Radii: display.Radii{{H: 15, V: 15}, {H: 15, V: 15}, {H: 15, V: 15}, {H: 15, V: 15}}, Sides: [4]display.BorderSide{side(40, black), side(40, black), side(40, black), side(40, black)}},
		display.PushClip{Rect: box, Radii: radii},
		display.FillRect{Rect: box, Color: blue},
		display.PopClip{},
	})
	for _, want := range []string{"W* n", " c\n"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
	e.finish(t)
}

func TestRoundedBorderNoInner(t *testing.T) {
	e := newEnv(t)
	radii := display.Radii{{H: 10, V: 10}, {H: 10, V: 10}, {H: 10, V: 10}, {H: 10, V: 10}}
	s := display.BorderSide{Width: 30, Color: black}
	e.page(t, a4, display.List{display.Border{Rect: display.Rect{W: 40, H: 40}, Radii: radii, Sides: [4]display.BorderSide{s, s, s, s}}})
	e.finish(t)
}

func TestGradientsDedupAcrossPaint(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for i := range 1000 {
		l = append(l, display.FillGradient{Rect: display.Rect{X: float64(i % 700), Y: float64(i % 900), W: 50, H: 50}, Gradient: grad(float64(i%360), 0)})
	}
	e.page(t, a4, l)
	e.finish(t)
	if n := strings.Count(e.buf.String(), "/ShadingType 2"); n != 1 {
		t.Fatalf("%d shading objects for identical stops", n)
	}
}

func TestStandInColor(t *testing.T) {
	stops := []css.ColorStop{{Color: css.Color{A: 1}, Pos: 0}, {Color: css.Color{R: 1, A: 1}, Pos: 1}}
	if c := midColor(stops); math.Abs(c.R-0.5) > 1e-9 || c.A != 1 {
		t.Errorf("mid color %v", c)
	}
	hard := []css.ColorStop{{Color: red}, {Color: red, Pos: 0.5}, {Color: blue, Pos: 0.5}, {Color: blue, Pos: 1}}
	if c := midColor(hard); c.R != 1 && c.B != 1 {
		t.Errorf("hard stop color %v", c)
	}
}

func TestGradientLimitPaintsTheBackgroundColor(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for i := range pdf.MaxShadings + 5 {
		g := css.Gradient{Angle: 90, Stops: []css.ColorStop{{Color: css.Color{R: float64(i%64) / 64, G: float64(i/64) / 70, A: 1}}, {Color: blue, Pos: 1}}}
		l = append(l, display.FillGradient{Rect: display.Rect{X: float64(i % 700), Y: float64(i % 900), W: 20, H: 20}, Gradient: g})
	}
	content, _ := e.page(t, a4, l)
	if got := strings.Count(content, " sh\n"); got != pdf.MaxShadings {
		t.Fatalf("%d shadings drawn", got)
	}
	// Each of the 5 dropped gradients is filled with a color instead.
	if got := strings.Count(content, "f\n"); got < 5 {
		t.Fatalf("%d fills", got)
	}
	ws, _ := e.b.Warnings()
	n := 0
	for _, w := range ws {
		if w.Code == budget.WarnClamped && w.Detail == "gradients > 4096" {
			n++
			if w.Count != 5 {
				t.Errorf("count %d, want 5", w.Count)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d warnings: %v", n, ws)
	}
	e.finish(t)
}

func TestMaxGradientStopsMatchCSS(t *testing.T) {
	if pdf.MaxGradientStops < css.MaxGradientStops {
		t.Fatalf("pdf accepts %d stops, css produces %d", pdf.MaxGradientStops, css.MaxGradientStops)
	}
}

func TestFillGradientIsOpaqueAfterATranslucentFill(t *testing.T) {
	e := newEnv(t)
	content, _ := e.page(t, a4, display.List{
		display.FillRect{Rect: display.Rect{X: 10, Y: 10, W: 50, H: 50}, Color: css.Color{R: 1, A: 0.04}},
		display.FillGradient{Rect: display.Rect{X: 10, Y: 100, W: 300, H: 100}, Gradient: grad(90, 0)},
		display.FillRect{Rect: display.Rect{X: 10, Y: 300, W: 50, H: 50}, Color: css.Color{R: 1, A: 0.04}},
	})
	shade := strings.Index(content, "/Sh1 sh")
	if shade < 0 {
		t.Fatalf("no gradient in\n%s", content)
	}
	before := content[:shade]
	if i := strings.LastIndex(before, "/GT"); i < 0 || !strings.HasPrefix(before[i:], "/GT0 gs") {
		t.Errorf("the gradient is not painted opaque:\n%s", content)
	}
	if strings.Count(content, " gs\n") != 2 {
		t.Errorf("the alpha must come back with the Q, without another gs:\n%s", content)
	}
	e.finish(t)
}
