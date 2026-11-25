package paint

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// matrixOf returns the operands of the first cm operator of content.
func matrixOf(t testing.TB, content string) [6]float64 {
	t.Helper()
	for _, l := range strings.Split(content, "\n") {
		if f, ok := strings.CutSuffix(l, " cm"); ok {
			var m [6]float64
			parts := strings.Fields(f)
			if len(parts) != 6 {
				t.Fatalf("cm with %d operands", len(parts))
			}
			for i, p := range parts {
				v, err := strconv.ParseFloat(p, 64)
				if err != nil {
					t.Fatal(err)
				}
				m[i] = v
			}
			return m
		}
	}
	t.Fatalf("no cm in:\n%s", content)
	return [6]float64{}
}

func paintList(t testing.TB, l display.List) (string, error) {
	t.Helper()
	e := newEnv(t)
	c := &pdf.Content{}
	_, err := e.painter.Page(c, a4, l)
	return string(c.Bytes()), err
}

func TestTransformOperators(t *testing.T) {
	box := display.FillRect{Rect: display.Rect{X: 10, Y: 20, W: 30, H: 40}, Color: red}
	tests := []struct {
		name string
		m    [6]float64
		want [6]float64
	}{
		{"identity", [6]float64{1, 0, 0, 1, 0, 0}, [6]float64{1, 0, 0, 1, 0, 0}},
		{"translate: y flips", [6]float64{1, 0, 0, 1, 10, 20}, [6]float64{1, 0, 0, 1, 7.5, -15}},
		{"scale about the page origin", [6]float64{2, 0, 0, 3, 0, 0}, [6]float64{2, 0, 0, 3, 0, -750 * 2}},
		{"quarter turn", [6]float64{0, 1, -1, 0, 0, 0}, [6]float64{0, -1, 1, 0, -750, 750}},
	}
	for _, tt := range tests {
		content, err := paintList(t, display.List{display.PushTransform{M: tt.m}, box, display.PopTransform{}})
		if err != nil {
			t.Fatal(err)
		}
		got := matrixOf(t, content)
		for i := range got {
			if !approx(got[i], tt.want[i]) {
				t.Errorf("%s: cm %v, want %v", tt.name, got, tt.want)
				break
			}
		}
		if !strings.HasPrefix(content, "q\n") || !strings.HasSuffix(content, "Q\n") || strings.Count(content, " cm\n") != 1 {
			t.Errorf("%s: content\n%s", tt.name, content)
		}
	}
}

func TestTransformConjugationMapsPoints(t *testing.T) {
	s := &pageState{g: Geometry{Width: 800, Height: 1000, Area: display.Rect{X: 13, Y: 7, W: 700, H: 900}, Scale: 1.5}}
	m := [6]float64{0.8, 0.3, -0.2, 1.1, 40, -25}
	pm := s.pdfMatrix(m)
	for _, p := range [][2]float64{{0, 0}, {100, 50}, {-30, 400}} {
		wantX, wantY := s.toPt(m[0]*p[0]+m[2]*p[1]+m[4], m[1]*p[0]+m[3]*p[1]+m[5])
		x, y := s.toPt(p[0], p[1])
		gotX, gotY := pm[0]*x+pm[2]*y+pm[4], pm[1]*x+pm[3]*y+pm[5]
		if !approx(gotX, wantX) || !approx(gotY, wantY) {
			t.Errorf("(%v,%v): got (%v,%v), want (%v,%v)", p[0], p[1], gotX, gotY, wantX, wantY)
		}
	}
}

func TestTransformMapsLinkToItsBoundingBox(t *testing.T) {
	e := newEnv(t)
	l := display.List{
		display.PushTransform{M: [6]float64{0, 1, -1, 0, 300, 0}},
		display.Link{Rect: display.Rect{X: 100, Y: 20, W: 40, H: 10}, URI: "https://t.test/"},
		display.PopTransform{},
		display.Link{Rect: display.Rect{X: 100, Y: 20, W: 40, H: 10}, URI: "https://u.test/"},
	}
	_, links := e.page(t, a4, l)
	if len(links) != 2 {
		t.Fatalf("%d links", len(links))
	}
	// The rectangle (100..140, 20..30) turned by 90°: x' = 300-y, y' = x.
	want := [4]float64{(300 - 30) * 0.75, (1000 - 140) * 0.75, (300 - 20) * 0.75, (1000 - 100) * 0.75}
	for i := range want {
		if !approx(links[0].Rect[i], want[i]) {
			t.Errorf("link rect %v, want %v", links[0].Rect, want)
			break
		}
	}
	if want2 := [4]float64{75, 750 - 22.5, 105, 750 - 15}; links[1].Rect != want2 {
		t.Errorf("link after the transform %v, want %v", links[1].Rect, want2)
	}
}

func TestTransformedItemsAreNotCulledByTheirUntransformedPosition(t *testing.T) {
	far := display.FillRect{Rect: display.Rect{X: 5000, Y: 20, W: 30, H: 40}, Color: red}
	content, err := paintList(t, display.List{display.PushTransform{M: [6]float64{1, 0, 0, 1, -5000, 0}}, far, display.PopTransform{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "re\nf\n") {
		t.Errorf("item moved onto the page is culled:\n%s", content)
	}
}

func TestUnbalancedGroups(t *testing.T) {
	tests := map[string]display.List{
		"pop transform alone":         {display.PopTransform{}},
		"pop clip alone":              {display.PopClip{}},
		"clip closed as transform":    {display.PushClip{Rect: display.Rect{W: 5, H: 5}}, display.PopTransform{}},
		"transform closed as clip":    {display.PushTransform{M: identityM}, display.PopClip{}},
		"transform left open":         {display.PushTransform{M: identityM}},
		"hidden transform left open":  {display.PushTransform{M: [6]float64{math.NaN()}}},
		"crossed":                     {display.PushTransform{M: identityM}, display.PushClip{Rect: display.Rect{W: 5, H: 5}}, display.PopTransform{}, display.PopClip{}},
		"pop of a hidden transform×2": {display.PushTransform{M: [6]float64{math.Inf(1)}}, display.PopTransform{}, display.PopTransform{}},
	}
	for name, l := range tests {
		if _, err := paintList(t, l); !errors.Is(err, ErrUnbalancedClip) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

var identityM = [6]float64{1, 0, 0, 1, 0, 0}

func TestNonFiniteTransformDrawsItsContentUntransformed(t *testing.T) {
	e := newEnv(t)
	box := display.FillRect{Rect: display.Rect{W: 30, H: 40}, Color: red}
	l := display.List{
		display.PushTransform{M: [6]float64{math.NaN(), 0, 0, 1, 0, 0}},
		box,
		display.PushClip{Rect: display.Rect{W: 5, H: 5}},
		box,
		display.PopClip{},
		display.PushTransform{M: identityM},
		box,
		display.PopTransform{},
		display.Link{Rect: display.Rect{W: 5, H: 5}, URI: "https://h.test/"},
		display.PopTransform{},
		display.FillRect{Rect: display.Rect{X: 100, Y: 100, W: 30, H: 40}, Color: blue},
	}
	content, links := e.page(t, a4, l)
	e.finish(t)
	if n := strings.Count(content, "re\nf\n"); n != 4 || len(links) != 1 {
		t.Errorf("%d fills, %d links:\n%s", n, len(links), content)
	}
	if ws, _ := e.b.Warnings(); len(ws) != 1 || ws[0].Detail != "transform" {
		t.Errorf("warnings %+v", ws)
	}
}

func TestNestedTransformsAreBounded(t *testing.T) {
	for _, depth := range []int{MaxClipDepth - 1, MaxClipDepth, MaxClipDepth + 1, 512} {
		e := newEnv(t)
		var l display.List
		for range depth {
			l = append(l, display.PushTransform{M: [6]float64{1, 0, 0, 1, 1, 0}})
		}
		l = append(l, display.FillRect{Rect: display.Rect{X: 5, Y: 5, W: 30, H: 40}, Color: red})
		for range depth {
			l = append(l, display.PopTransform{})
		}
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, l); err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		if c.Depth() != 0 {
			t.Errorf("depth %d: %d unmatched q", depth, c.Depth())
		}
		saves := balanced(t, string(c.Bytes()))
		if saves > MaxClipDepth+2 {
			t.Errorf("depth %d: %d saves", depth, saves)
		}
		drawn := strings.Contains(string(c.Bytes()), "re\nf\n")
		ws, _ := e.b.Warnings()
		// Too deep to nest: the content is drawn untransformed, never hidden.
		if want := depth <= MaxClipDepth; !drawn || (len(ws) == 0) != want {
			t.Errorf("depth %d: drawn=%v warnings=%v", depth, drawn, ws)
		}
	}
}

func TestMixedClipAndTransformDepthIsShared(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for i := range 2 * MaxClipDepth {
		if i%2 == 0 {
			l = append(l, display.PushClip{Rect: display.Rect{W: 700, H: 900}})
		} else {
			l = append(l, display.PushTransform{M: identityM})
		}
	}
	for i := 2*MaxClipDepth - 1; i >= 0; i-- {
		if i%2 == 0 {
			l = append(l, display.PopClip{})
		} else {
			l = append(l, display.PopTransform{})
		}
	}
	c := &pdf.Content{}
	if _, err := e.painter.Page(c, a4, l); err != nil {
		t.Fatal(err)
	}
	if saves := balanced(t, string(c.Bytes())); saves != MaxClipDepth {
		t.Errorf("%d saves, want %d", saves, MaxClipDepth)
	}
}

func TestPathOperatorSequences(t *testing.T) {
	segs := []display.PathSeg{
		{Op: display.PathMoveTo, P: [3]display.Point{{X: 10, Y: 10}}},
		{Op: display.PathLineTo, P: [3]display.Point{{X: 50, Y: 10}}},
		{Op: display.PathCubicTo, P: [3]display.Point{{X: 60, Y: 10}, {X: 60, Y: 40}, {X: 50, Y: 40}}},
		{Op: display.PathClose},
	}
	const geometry = "7.5 742.5 m\n37.5 742.5 l\n45 742.5 45 720 37.5 720 c\nh\n"
	half := css.Color{R: 1, A: 0.5}
	tests := []struct {
		name string
		p    display.Path
		want string
	}{
		{"fill", display.Path{Segs: segs, Fill: red}, geometry + "f\n"},
		{"fill even-odd", display.Path{Segs: segs, Fill: red, EvenOdd: true}, geometry + "f*\n"},
		{"stroke", display.Path{Segs: segs, Stroke: blue, Width: 4, Join: display.PathMoveTo + 1, Cap: 2}, geometry + "S\n"},
		{"fill and stroke, same alpha", display.Path{Segs: segs, Fill: half, Stroke: css.Color{B: 1, A: 0.5}, Width: 2}, geometry + "B\n"},
		{"fill and stroke even-odd", display.Path{Segs: segs, Fill: half, Stroke: css.Color{B: 1, A: 0.5}, Width: 2, EvenOdd: true}, geometry + "B*\n"},
		{"fill and stroke, two alphas", display.Path{Segs: segs, Fill: half, Stroke: blue, Width: 2}, geometry + "f\n"},
	}
	for _, tt := range tests {
		got, err := paintList(t, display.List{tt.p})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, "q\n") || !strings.HasSuffix(got, "Q\n") || !strings.Contains(got, tt.want) {
			t.Errorf("%s:\n%s\nwant it to contain\n%s", tt.name, got, tt.want)
		}
	}
	got, _ := paintList(t, display.List{display.Path{Segs: segs, Fill: half, Stroke: blue, Width: 2}})
	if strings.Count(got, geometry) != 2 || !strings.Contains(got, "S\n") {
		t.Errorf("two passes expected:\n%s", got)
	}
	got, _ = paintList(t, display.List{display.Path{Segs: segs, Stroke: blue, Width: 4, Join: 1, Cap: 2}})
	for _, want := range []string{"3 w\n", "1 j\n", "2 J\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestPathEdgeCases(t *testing.T) {
	mv := display.PathSeg{Op: display.PathMoveTo, P: [3]display.Point{{X: 1, Y: 1}}}
	ln := display.PathSeg{Op: display.PathLineTo, P: [3]display.Point{{X: 9, Y: 9}}}
	for name, p := range map[string]display.Path{
		"invisible":     {Segs: []display.PathSeg{mv, ln}, Fill: css.Color{}, Stroke: css.Color{A: 1}, Width: 0},
		"one segment":   {Segs: []display.PathSeg{mv}, Fill: red},
		"empty":         {Fill: red},
		"transparent":   {Segs: []display.PathSeg{mv, ln}, Fill: css.Color{R: 1}},
		"zero width":    {Segs: []display.PathSeg{mv, ln}, Stroke: red},
		"only a close":  {Segs: []display.PathSeg{{Op: display.PathClose}, {Op: display.PathClose}}, Fill: red},
		"before moveto": {Segs: []display.PathSeg{ln, ln}, Fill: red},
	} {
		got, err := paintList(t, display.List{p})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.ContainsAny(strings.ReplaceAll(got, "q\nQ\n", ""), "mlcf") && strings.Contains(got, "\nf\n") {
			t.Errorf("%s: drew:\n%s", name, got)
		}
	}
	got, _ := paintList(t, display.List{display.Path{Segs: []display.PathSeg{ln, mv, ln}, Fill: red}})
	if strings.Count(got, " m\n") != 1 || strings.Count(got, " l\n") != 1 {
		t.Errorf("segments before the first moveto are kept:\n%s", got)
	}
	e := newEnv(t)
	hostile := display.Path{Segs: []display.PathSeg{
		{Op: display.PathMoveTo, P: [3]display.Point{{X: math.NaN(), Y: math.Inf(1)}}},
		{Op: display.PathLineTo, P: [3]display.Point{{X: 1e300, Y: -1e300}}},
		{Op: 9},
		{Op: display.PathCubicTo},
	}, Fill: red, Stroke: blue, Width: math.Inf(1), Join: 200, Cap: 200}
	c := &pdf.Content{}
	if _, err := e.painter.Page(c, a4, display.List{hostile}); err != nil {
		t.Fatalf("hostile path: %v", err)
	}
	balanced(t, string(c.Bytes()))
}

func TestRadialMatrices(t *testing.T) {
	r := ptRect{x: 10, y: 20, w: 80, h: 60}
	m, ok := radialMatrix(r, css.GradientCircle)
	if !ok || !approx(m[0], 50) || !approx(m[3], 50) || m[1] != 0 || m[2] != 0 || !approx(m[4], 50) || !approx(m[5], 50) {
		t.Errorf("circle %v", m)
	}
	m, ok = radialMatrix(r, css.GradientEllipse)
	if !ok || !approx(m[0], 40*math.Sqrt2) || !approx(m[3], 30*math.Sqrt2) {
		t.Errorf("ellipse %v", m)
	}
	if _, ok := radialMatrix(ptRect{w: 0, h: 10}, css.GradientEllipse); ok {
		t.Error("an ellipse of width 0")
	}
	if _, ok := radialMatrix(ptRect{w: math.Inf(1), h: 10}, css.GradientCircle); ok {
		t.Error("an infinite circle")
	}
}

func stops(cs ...css.Color) []css.ColorStop {
	out := make([]css.ColorStop, len(cs))
	for i, c := range cs {
		out[i] = css.ColorStop{Color: c, Pos: float64(i) / float64(len(cs)-1)}
	}
	return out
}

func TestPremultipliedStops(t *testing.T) {
	orange := css.Color{R: 0.9, G: 0.3, B: 0.05, A: 0.5}
	clear := css.Color{}
	tests := []struct {
		name  string
		in    []css.ColorStop
		wantC []pdf.Color
	}{
		{"fade out takes the neighbour's color", stops(orange, clear), []pdf.Color{{R: 0.9, G: 0.3, B: 0.05, A: 0.5}, {R: 0.9, G: 0.3, B: 0.05}}},
		{"fade in takes the neighbour's color", stops(clear, orange), []pdf.Color{{R: 0.9, G: 0.3, B: 0.05}, {R: 0.9, G: 0.3, B: 0.05, A: 0.5}}},
		{"between two colors: split in two", stops(red, clear, blue), []pdf.Color{{R: 1, A: 1}, {R: 1}, {B: 1}, {B: 1, A: 1}}},
		{"opaque stops untouched", stops(red, blue), []pdf.Color{{R: 1, A: 1}, {B: 1, A: 1}}},
		{"all transparent untouched", stops(clear, clear), []pdf.Color{{}, {}}},
		{"two transparent in a row", stops(red, clear, clear, blue), []pdf.Color{{R: 1, A: 1}, {R: 1}, {B: 1}, {R: 1}, {B: 1}, {B: 1, A: 1}}},
	}
	for _, tt := range tests {
		got := pdfStops(tt.in)
		if len(got) != len(tt.wantC) {
			t.Errorf("%s: %d stops, want %d: %+v", tt.name, len(got), len(tt.wantC), got)
			continue
		}
		for i := range got {
			if got[i].Color != tt.wantC[i] {
				t.Errorf("%s: stop %d is %+v, want %+v", tt.name, i, got[i].Color, tt.wantC[i])
			}
		}
	}
}

func TestPremultipliedStopsStayUnderTheStopLimit(t *testing.T) {
	var in []css.ColorStop
	for i := range pdf.MaxGradientStops {
		c := red
		if i%2 == 1 {
			c = css.Color{}
		}
		in = append(in, css.ColorStop{Color: c, Pos: float64(i) / pdf.MaxGradientStops})
	}
	in[0].Color, in[len(in)-1].Color = red, blue
	if got := pdfStops(in); len(got) != pdf.MaxGradientStops {
		t.Errorf("%d stops: a split that does not fit must keep the nearest color", len(got))
	}
	half := in[:pdf.MaxGradientStops/2]
	half[0].Color, half[len(half)-1].Color = red, blue
	got := pdfStops(half)
	if len(got) <= len(half) || len(got) > pdf.MaxGradientStops {
		t.Errorf("%d stops from %d", len(got), len(half))
	}
}

func TestRadialAndTranslucentGradientPage(t *testing.T) {
	e := newEnv(t)
	glow := css.Gradient{Shape: css.GradientCircle, Stops: []css.ColorStop{{Color: css.Color{R: 0.9, G: 0.35, B: 0.05, A: 0.5}}, {Color: css.Color{}, Pos: 0.7}}}
	ell := css.Gradient{Shape: css.GradientEllipse, Stops: stops(red, blue)}
	l := display.List{
		display.FillGradient{Rect: display.Rect{X: 10, Y: 10, W: 200, H: 100}, Gradient: glow},
		display.FillGradient{Rect: display.Rect{X: 10, Y: 200, W: 200, H: 100}, Radii: display.Radii{{H: 10, V: 10}, {H: 10, V: 10}, {H: 10, V: 10}, {H: 10, V: 10}}, Gradient: ell},
		display.PushTransform{M: [6]float64{0.9, 0.2, -0.2, 0.9, 5, 5}},
		display.FillGradient{Rect: display.Rect{X: 300, Y: 10, W: 100, H: 100}, Gradient: glow},
		display.Path{Segs: []display.PathSeg{
			{Op: display.PathMoveTo, P: [3]display.Point{{X: 10, Y: 10}}},
			{Op: display.PathCubicTo, P: [3]display.Point{{X: 50, Y: 10}, {X: 50, Y: 50}, {X: 10, Y: 50}}},
			{Op: display.PathClose},
		}, Fill: css.Color{R: 1, A: 0.4}, Stroke: blue, Width: 3, Join: 1, Cap: 1},
		display.Link{Rect: display.Rect{X: 10, Y: 10, W: 50, H: 20}, URI: "https://all.test/"},
		display.PopTransform{},
		display.FillRect{Rect: display.Rect{X: 5, Y: 5, W: 5, H: 5}, Color: red},
	}
	content, links := e.page(t, a4, l)
	doc := e.finish(t)
	if len(doc.Pages) != 1 || len(links) != 1 {
		t.Fatalf("%d pages, %d links", len(doc.Pages), len(links))
	}
	if ws, _ := e.b.Warnings(); len(ws) != 0 {
		t.Errorf("warnings %+v", ws)
	}
	if n := strings.Count(content, "/SM1 gs\n/Sh1 sh\n"); n != 2 {
		t.Errorf("%d soft-masked shadings:\n%s", n, content)
	}
	if strings.Contains(strings.ToLower(content), "gradient alpha") {
		t.Error("the alpha warning is gone")
	}
}

func TestTranslucentGradientLimit(t *testing.T) {
	grad := func(i int) display.Item {
		g := css.Gradient{Stops: []css.ColorStop{{Color: css.Color{R: float64(i) / 10000, A: 0.5}}, {Color: blue, Pos: 1}}}
		return display.FillGradient{Rect: display.Rect{X: 1, Y: 1, W: 20, H: 20}, Gradient: g}
	}
	for _, n := range []int{pdf.MaxShadings / 2, pdf.MaxShadings/2 + 1} {
		e := newEnv(t)
		var l display.List
		for i := range n {
			l = append(l, grad(i))
		}
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, l); err != nil {
			t.Fatal(err)
		}
		shaded := strings.Count(string(c.Bytes()), " sh\n")
		ws, _ := e.b.Warnings()
		clamped := 0
		for _, w := range ws {
			if w.Code == budget.WarnClamped && strings.HasPrefix(w.Detail, "gradients >") {
				clamped += w.Count
			}
		}
		if n == pdf.MaxShadings/2 && (shaded != n || clamped != 0) {
			t.Errorf("at the limit: %d shaded, %d clamped", shaded, clamped)
		}
		if n > pdf.MaxShadings/2 && (shaded != n-1 || clamped != 1) {
			t.Errorf("over the limit: %d shaded, %d clamped; the last one must be its mid color", shaded, clamped)
		}
	}
}

// opacities replays the graphics-state stack of content and returns the fill
// alpha at every painting operator. Soft-mask states leave the alpha alone.
func opacities(t testing.TB, content string, alpha map[string]float64) []float64 {
	t.Helper()
	cur := 1.0
	var stack []float64
	var out []float64
	for _, l := range strings.Split(content, "\n") {
		switch {
		case l == "q":
			stack = append(stack, cur)
		case l == "Q":
			if len(stack) == 0 {
				t.Fatal("Q without q")
			}
			cur, stack = stack[len(stack)-1], stack[:len(stack)-1]
		case strings.HasSuffix(l, " gs"):
			name := strings.TrimPrefix(strings.TrimSuffix(l, " gs"), "/")
			if strings.HasPrefix(name, "SM") {
				continue
			}
			a, ok := alpha[name]
			if !ok {
				t.Fatalf("unknown ExtGState %s", name)
			}
			cur = a
		case l == "f", l == "f*", l == "S", l == "B", l == "B*", strings.HasSuffix(l, " sh"), strings.HasSuffix(l, " TJ"), strings.HasSuffix(l, " Tj"):
			out = append(out, cur)
		}
	}
	return out
}

// Transforms, paths and soft masks do not break the paint state invariant of
// M2-01c: every operator runs at the alpha of its own item.
func TestNoAlphaLeaksAroundTransformsAndSoftMasks(t *testing.T) {
	rect := display.Rect{X: 20, Y: 30, W: 300, H: 120}
	round := display.Radii{{H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}}
	col := func(a float64) css.Color { return css.Color{R: 0.2, G: 0.4, B: 0.6, A: a} }
	side := func(a float64) display.BorderSide { return display.BorderSide{Width: 3, Color: col(a)} }
	segs := []display.PathSeg{
		{Op: display.PathMoveTo, P: [3]display.Point{{X: 30, Y: 40}}},
		{Op: display.PathCubicTo, P: [3]display.Point{{X: 90, Y: 40}, {X: 90, Y: 90}, {X: 30, Y: 90}}},
		{Op: display.PathClose},
	}
	txt := newEnv(t).run(t, "alpha", 14)
	type item struct {
		name  string
		it    func() display.Item
		alpha []float64
	}
	var items []item
	for _, a := range []float64{1, 0.3} {
		items = append(items,
			item{"rect", func() display.Item { return display.FillRect{Rect: rect, Color: col(a)} }, []float64{a}},
			item{"rounded rect", func() display.Item { return display.FillRect{Rect: rect, Radii: round, Color: col(a)} }, []float64{a}},
			item{"text", func() display.Item { t := txt; t.Color = col(a); return &t }, []float64{a}},
			item{"underlined text", func() display.Item { t := txt; t.Color, t.Underline = col(a), true; return &t }, []float64{a}},
			item{"uniform border", func() display.Item {
				return display.Border{Rect: rect, Sides: [4]display.BorderSide{side(a), side(a), side(a), side(a)}}
			}, []float64{a}},
			item{"rounded border", func() display.Item {
				return display.Border{Rect: rect, Radii: round, Sides: [4]display.BorderSide{side(a), side(a), side(a), side(a)}}
			}, []float64{a}},
			item{"path fill", func() display.Item { return display.Path{Segs: segs, Fill: col(a)} }, []float64{a}},
			item{"path stroke", func() display.Item { return display.Path{Segs: segs, Stroke: col(a), Width: 2} }, []float64{a}},
			item{"path fill and stroke", func() display.Item { return display.Path{Segs: segs, Fill: col(a), Stroke: col(a), Width: 2} }, []float64{a}},
			item{"path fill, other stroke", func() display.Item { return display.Path{Segs: segs, Fill: col(a), Stroke: col(1), Width: 2} }, []float64{a, 1}},
		)
	}
	items = append(items,
		item{"linear gradient", func() display.Item {
			return display.FillGradient{Rect: rect, Gradient: css.Gradient{Angle: 90, Stops: stops(red, blue)}}
		}, []float64{1}},
		item{"radial gradient", func() display.Item {
			return display.FillGradient{Rect: rect, Gradient: css.Gradient{Shape: css.GradientCircle, Stops: stops(red, blue)}}
		}, []float64{1}},
		item{"translucent gradient", func() display.Item {
			return display.FillGradient{Rect: rect, Gradient: css.Gradient{Stops: stops(col(0.5), blue)}}
		}, []float64{1}},
		item{"rounded translucent radial", func() display.Item {
			return display.FillGradient{Rect: rect, Radii: round, Gradient: css.Gradient{Shape: css.GradientEllipse, Stops: stops(col(0.5), css.Color{})}}
		}, []float64{1}},
	)
	wraps := map[string]func(display.Item, display.Item) display.List{
		"plain": func(dirty, it display.Item) display.List { return display.List{dirty, it} },
		"in a transform": func(dirty, it display.Item) display.List {
			return display.List{dirty, display.PushTransform{M: [6]float64{0.9, 0.2, -0.2, 0.9, 5, 5}}, it, display.PopTransform{}}
		},
		"after a soft mask": func(dirty, it display.Item) display.List {
			mask := display.FillGradient{Rect: rect, Gradient: css.Gradient{Stops: stops(col(0.2), col(0.9))}}
			return display.List{dirty, mask, it}
		},
		"transform around the dirty item": func(dirty, it display.Item) display.List {
			return display.List{display.PushTransform{M: identityM}, dirty, display.PopTransform{}, it}
		},
	}
	for _, dirty := range []float64{0.04, 0.5, 0.99} {
		for wname, wrap := range wraps {
			for _, tc := range items {
				t.Run(fmt.Sprintf("%s/%s/after alpha %v", tc.name, wname, dirty), func(t *testing.T) {
					e := newEnv(t)
					content, _ := e.page(t, a4, wrap(display.FillRect{Rect: display.Rect{W: 10, H: 10}, Color: col(dirty)}, tc.it()))
					e.finish(t)
					got := opacities(t, content, extGStates(t, e.buf.Bytes()))
					if len(got) < 2 {
						t.Fatalf("%d painting operators", len(got))
					}
					last := got[len(got)-1:]
					if wname == "after a soft mask" {
						last = got[2:]
					}
					for _, a := range last {
						ok := false
						for _, w := range tc.alpha {
							ok = ok || math.Abs(a-w) < 0.01
						}
						if !ok {
							t.Errorf("an operator of the item runs at alpha %.3f, it asks for %v (all: %v)", a, tc.alpha, got)
							return
						}
					}
				})
			}
		}
	}
}

func TestOutsideClipExcludesTheBox(t *testing.T) {
	box := display.Rect{X: 10, Y: 10, W: 100, H: 50}
	content, err := paintList(t, display.List{
		display.PushClip{Rect: box, Outside: true},
		display.FillRect{Rect: display.Rect{W: 300, H: 300}, Color: red},
		display.PopClip{},
		display.PushClip{Rect: display.Rect{X: 5000, W: 10, H: 10}, Outside: true},
		display.FillRect{Rect: display.Rect{W: 30, H: 30}, Color: red},
		display.PopClip{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "re\n7.5 ") || !strings.Contains(content, "W* n\n") {
		t.Errorf("no even-odd clip:\n%s", content)
	}
	if strings.Count(content, "W* n") != 1 || strings.Count(content, "re\nf\n") != 2 {
		t.Errorf("the off-page box must not clip the fill:\n%s", content)
	}
	balanced(t, content)
}

func TestUnusableTransformsDrawTheirContentUntransformed(t *testing.T) {
	box := display.FillRect{Rect: display.Rect{X: 5, Y: 5, W: 30, H: 40}, Color: red}
	big := [6]float64{1e5, 0, 0, 1e5, 0, 0}
	tests := map[string][]display.Item{
		"scale 1e10":      {display.PushTransform{M: [6]float64{1e10, 0, 0, 1e10, 0, 0}}},
		"shift 2e9":       {display.PushTransform{M: [6]float64{1, 0, 0, 1, 2e9, 0}}},
		"near singular":   {display.PushTransform{M: [6]float64{1e-7, 0, 0, 1, 0, 0}}},
		"singular":        {display.PushTransform{M: [6]float64{1, 1, 1, 1, 0, 0}}},
		"nested overflow": {display.PushTransform{M: big}, display.PushTransform{M: big}, display.PushTransform{M: big}},
	}
	for name, pushes := range tests {
		e := newEnv(t)
		var l display.List
		for _, p := range pushes {
			l = append(l, p)
		}
		l = append(l, box)
		for range pushes {
			l = append(l, display.PopTransform{})
		}
		l = append(l, display.FillRect{Rect: display.Rect{X: 100, Y: 100, W: 30, H: 40}, Color: blue})
		content, _ := e.page(t, a4, l)
		e.finish(t)
		if strings.Count(content, "re\nf\n") != 2 || strings.Contains(content, "0 0 0 0") {
			t.Errorf("%s:\n%s", name, content)
		}
		ws, _ := e.b.Warnings()
		if len(ws) == 0 || ws[0].Code != budget.WarnClamped {
			t.Errorf("%s: warnings %+v", name, ws)
		}
	}
}

func TestUsableTransformAtTheBounds(t *testing.T) {
	for _, m := range [][6]float64{{maxLinear, 0, 0, 1, 0, 0}, {1, 0, 0, 1, pdf.MaxCoord, -pdf.MaxCoord}, {1e-3, 0, 0, 1e-3, 0, 0}} {
		if !usable(m) {
			t.Errorf("%v rejected", m)
		}
	}
	for _, m := range [][6]float64{{maxLinear * 1.01, 0, 0, 1, 0, 0}, {1, 0, 0, 1, pdf.MaxCoord * 1.01, 0}, {1e-3, 0, 0, 1e-4, 0, 0}, {4e-5, 0, 0, 1, 0, 0}} {
		if usable(m) {
			t.Errorf("%v accepted", m)
		}
	}
}
