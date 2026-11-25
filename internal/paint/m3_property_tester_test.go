package paint

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
)

type propRNG struct{ s uint64 }

func (r *propRNG) next() uint64 {
	r.s ^= r.s << 13
	r.s ^= r.s >> 7
	r.s ^= r.s << 17
	return r.s
}

func (r *propRNG) n(k int) int {
	v := r.next() % 1_000_000
	return int(v) % max(k, 1)
}
func (r *propRNG) f(lo, hi float64) float64 { return lo + float64(r.next()%1000000)/1e6*(hi-lo) }

// weird returns a float that is sometimes hostile.
func (r *propRNG) weird(lo, hi float64) float64 {
	switch r.n(24) {
	case 0:
		return math.NaN()
	case 1:
		return math.Inf(1)
	case 2:
		return math.Inf(-1)
	case 3:
		return 1e300
	case 4:
		return -1e300
	case 5:
		return 0
	}
	return r.f(lo, hi)
}

var probeColor = css.Color{R: 0.123, G: 0.456, B: 0.789, A: 1}

func (r *propRNG) color() css.Color {
	return css.Color{R: r.f(0, 1), G: r.f(0, 1), B: r.f(0, 1), A: []float64{1, 1, 0.5, 0.2, 0.0196, 0}[r.n(6)]}
}

func (r *propRNG) gradient() css.Gradient {
	g := css.Gradient{Shape: []css.GradientShape{css.GradientLinear, css.GradientCircle, css.GradientEllipse}[r.n(3)], Angle: r.f(0, 360)}
	n := 2 + r.n(4)
	for i := range n {
		g.Stops = append(g.Stops, css.ColorStop{Color: r.color(), Pos: float64(i) / float64(n-1)})
	}
	return g
}

func (r *propRNG) matrix() [6]float64 {
	if r.n(6) == 0 {
		// Non-finite or zero components only: a finite matrix beyond what the content can write is
		// TestHugeMatrixIsNotFatal's case.
		pick := func(lo, hi float64) float64 {
			if v := r.weird(lo, hi); math.Abs(v) < 1e5 || math.IsNaN(v) || math.IsInf(v, 0) {
				return v
			}
			return 0
		}
		return [6]float64{pick(-3, 3), pick(-3, 3), pick(-3, 3), pick(-3, 3), pick(-500, 500), pick(-500, 500)}
	}
	a := r.f(-0.5, 0.5)
	return [6]float64{math.Cos(a), math.Sin(a), -math.Sin(a), math.Cos(a), r.f(-100, 100), r.f(-100, 100)}
}

func (r *propRNG) rect() display.Rect {
	if r.n(8) == 0 {
		return display.Rect{X: r.weird(-1e3, 1e3), Y: r.weird(-1e3, 1e3), W: r.weird(-10, 800), H: r.weird(-10, 600)}
	}
	return display.Rect{X: r.f(-50, 700), Y: r.f(-50, 900), W: r.f(0, 400), H: r.f(0, 300)}
}

func (r *propRNG) item(e *env, depth int, out *display.List) {
	rect := r.rect()
	round := display.Radii{{H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}}
	switch k := r.n(13); k {
	case 0:
		*out = append(*out, display.FillRect{Rect: rect, Color: r.color()})
	case 1:
		*out = append(*out, display.FillRect{Rect: rect, Radii: round, Color: r.color()})
	case 2:
		side := display.BorderSide{Width: r.f(0, 6), Color: r.color()}
		*out = append(*out, display.Border{Rect: rect, Sides: [4]display.BorderSide{side, {Width: 2, Color: r.color()}, side, {}}})
	case 3, 4:
		*out = append(*out, display.FillGradient{Rect: rect, Gradient: r.gradient()})
	case 5:
		*out = append(*out, display.FillGradient{Rect: rect, Radii: round, Gradient: r.gradient()})
	case 6:
		t := e.run(testingT, "alpha", 12)
		t.X, t.Baseline, t.Color = r.f(0, 700), r.f(10, 900), r.color()
		*out = append(*out, &t)
	case 7:
		p := func() display.Point { return display.Point{X: r.weird(-100, 800), Y: r.weird(-100, 900)} }
		*out = append(*out, display.Path{
			Segs:   []display.PathSeg{{Op: display.PathMoveTo, P: [3]display.Point{p()}}, {Op: display.PathCubicTo, P: [3]display.Point{p(), p(), p()}}, {Op: display.PathLineTo, P: [3]display.Point{p()}}, {Op: display.PathClose}},
			Fill:   r.color(),
			Stroke: r.color(), Width: r.f(0, 5), Join: []uint8{0, 1, 2, 3}[r.n(4)], Cap: []uint8{0, 1, 2, 3}[r.n(4)], EvenOdd: r.n(2) == 0,
		})
	case 8:
		*out = append(*out, display.Link{Rect: rect, URI: "https://p.test/"})
	case 9:
		if depth < 6 {
			*out = append(*out, display.PushTransform{M: r.matrix()})
			for range 1 + r.n(4) {
				r.item(e, depth+1, out)
			}
			*out = append(*out, display.PopTransform{})
		}
	case 10:
		if depth < 6 {
			*out = append(*out, display.PushClip{Rect: rect, Radii: round})
			for range 1 + r.n(4) {
				r.item(e, depth+1, out)
			}
			*out = append(*out, display.PopClip{})
		}
	default:
		*out = append(*out, display.FillRect{Rect: rect, Color: r.color()})
	}
}

// testingT lets the item generator build text runs with the test's reporter.
var testingT testing.TB

type opState struct {
	alpha float64
	ctm   [6]float64
}

// replay walks the content stream with the q/Q stack, tracking the fill alpha
// (gs) and the CTM (cm), and returns the state at every probe fill: the f
// that follows the probe colour.
func replay(t testing.TB, content string, alpha map[string]float64) (probes []opState, depthMax int) {
	t.Helper()
	cur := opState{alpha: 1, ctm: identity}
	var stack []opState
	probe := strconv.FormatFloat(probeColor.R, 'f', -1, 64) + " " + strconv.FormatFloat(probeColor.G, 'f', -1, 64) + " " + strconv.FormatFloat(probeColor.B, 'f', -1, 64) + " rg"
	armed := false
	for _, l := range strings.Split(content, "\n") {
		switch {
		case l == "q":
			stack = append(stack, cur)
			depthMax = max(depthMax, len(stack))
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
			cur.alpha = a
		case strings.HasSuffix(l, " cm"):
			var m [6]float64
			f := strings.Fields(strings.TrimSuffix(l, " cm"))
			if len(f) != 6 {
				t.Fatalf("bad cm %q", l)
			}
			for i := range m {
				v, err := strconv.ParseFloat(f[i], 64)
				if err != nil {
					t.Fatalf("bad cm %q", l)
				}
				m[i] = v
			}
			cur.ctm = mul(cur.ctm, m)
		case l == probe:
			armed = true
		case l == "f" && armed:
			probes = append(probes, cur)
			armed = false
		}
	}
	if len(stack) != 0 {
		t.Fatalf("%d unclosed q", len(stack))
	}
	return probes, depthMax
}

// Random display lists mixing every item kind with transforms, clips, paths
// and linear, radial and translucent gradients (hostile numbers included):
// the content is balanced, the PDF valid, and after every top-level item an
// opaque probe rectangle runs at alpha 1 under the identity matrix: nothing
// leaks out of an item or a group.
func TestRandomListsLeakNothing(t *testing.T) {
	testingT = t
	for seed := uint64(1); seed <= 150; seed++ {
		rng := &propRNG{s: seed * 0x9E3779B97F4A7C15}
		e := newEnv(t)
		var l display.List
		top := 0
		for range 6 + rng.n(14) {
			rng.item(e, 0, &l)
			l = append(l, display.FillRect{Rect: display.Rect{X: 1, Y: 2, W: 3, H: 4}, Color: probeColor})
			top++
		}
		content, _ := e.page(t, a4, l)
		doc := e.finish(t)
		if len(doc.Pages) != 1 {
			t.Fatalf("seed %d: %d pages", seed, len(doc.Pages))
		}
		balanced(t, content)
		probes, _ := replay(t, content, extGStates(t, e.buf.Bytes()))
		if len(probes) != top {
			t.Fatalf("seed %d: %d probes drawn, want %d\n%s", seed, len(probes), top, content)
		}
		for i, p := range probes {
			if p.alpha != 1 {
				t.Errorf("seed %d probe %d: alpha %v leaked", seed, i, p.alpha)
			}
			for k := range p.ctm {
				if math.Abs(p.ctm[k]-identity[k]) > 1e-9 {
					t.Errorf("seed %d probe %d: CTM %v leaked", seed, i, p.ctm)
					break
				}
			}
		}
		if !strings.Contains(content, "\n") {
			t.Errorf("seed %d: empty content", seed)
		}
	}
}

// A finite matrix too large for the content stream is hidden with a warning,
// like a non-finite one: the page and the render survive and the error is not
// the misleading "unbalanced clip".
func TestHugeMatrixIsNotFatal(t *testing.T) {
	r := display.Rect{X: 1, Y: 1, W: 50, H: 50}
	for name, m := range map[string][6]float64{
		"scale 1e10":       {1e10, 0, 0, 1, 0, 0},
		"skew 1e100":       {1, 0, 1e100, 1, 0, 0},
		"translate 1e300":  {1, 0, 0, 1, 1e300, 0},
		"all 1e300":        {1e300, 1e300, -1e300, 1e300, 1e300, -1e300},
		"translate 1e7 px": {1, 0, 0, 1, 1e7, 1e7},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			l := display.List{display.PushTransform{M: m}, display.FillRect{Rect: r, Color: red}, display.PopTransform{}, display.FillRect{Rect: r, Color: blue}}
			content, _ := e.page(t, a4, l)
			e.finish(t)
			balanced(t, content)
			if !strings.Contains(content, "0 0 1 rg") {
				t.Errorf("the item after the transform is not painted:\n%s", content)
			}
		})
	}
}
