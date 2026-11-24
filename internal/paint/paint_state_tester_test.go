package paint

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
)

var extGStateRE = regexp.MustCompile(`/(GT\d+) << /Type /ExtGState /ca ([0-9.]+) /CA ([0-9.]+) >>`)

// paintedAlphas returns the fill alpha in effect at every painting operator of
// content (fill, shading, text), in order. alpha maps the ExtGState names of the
// document to their /ca.
func paintedAlphas(t testing.TB, content string, alpha map[string]float64) []float64 {
	t.Helper()
	cur := 1.0
	var out []float64
	for _, l := range strings.Split(content, "\n") {
		if name, ok := strings.CutSuffix(l, " gs"); ok {
			a, known := alpha[strings.TrimPrefix(name, "/")]
			if !known {
				t.Fatalf("%s is not an ExtGState with /ca", name)
			}
			cur = a
			continue
		}
		switch {
		case l == "f", l == "f*", l == "S", l == "B", strings.HasSuffix(l, " sh"), strings.HasSuffix(l, " TJ"), strings.HasSuffix(l, " Tj"):
			out = append(out, cur)
		}
	}
	return out
}

func extGStates(t testing.TB, pdfBytes []byte) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, m := range extGStateRE.FindAllSubmatch(pdfBytes, -1) {
		a, err := strconv.ParseFloat(string(m[2]), 64)
		if err != nil {
			t.Fatal(err)
		}
		out[string(m[1])] = a
	}
	return out
}

// Paint state invariant: no fill, shading or text runs under an alpha its own
// item does not ask for. Every kind of item is painted right after a
// translucent rectangle (at several alphas) and after an opaque one, and each
// of its painting operators must run at the item's own alpha.
func TestNoAlphaLeaksBetweenItems(t *testing.T) {
	rect := display.Rect{X: 20, Y: 30, W: 300, H: 120}
	round := display.Radii{{H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}}
	col := func(a float64) css.Color { return css.Color{R: 0.2, G: 0.4, B: 0.6, A: a} }
	e := newEnv(t)
	txt := e.run(t, "alpha", 14)
	side := func(a float64) display.BorderSide { return display.BorderSide{Width: 3, Color: col(a)} }
	type item struct {
		name  string
		it    func() display.Item
		alpha []float64 // alphas the item may paint with
	}
	var items []item
	for _, a := range []float64{1, 0.3} {
		items = append(items,
			item{"rect", func() display.Item { return display.FillRect{Rect: rect, Color: col(a)} }, []float64{a}},
			item{"rounded rect", func() display.Item { return display.FillRect{Rect: rect, Radii: round, Color: col(a)} }, []float64{a}},
			item{"text", func() display.Item { t := txt; t.Color = col(a); return &t }, []float64{a}},
			item{"underlined text", func() display.Item { t := txt; t.Color, t.Underline, t.LineThrough = col(a), true, true; return &t }, []float64{a}},
			item{"uniform border", func() display.Item {
				return display.Border{Rect: rect, Sides: [4]display.BorderSide{side(a), side(a), side(a), side(a)}}
			}, []float64{a}},
			item{"rounded border", func() display.Item {
				return display.Border{Rect: rect, Radii: round, Sides: [4]display.BorderSide{side(a), side(a), side(a), side(a)}}
			}, []float64{a}},
			item{"mixed border", func() display.Item {
				return display.Border{Rect: rect, Sides: [4]display.BorderSide{side(a), side(1), side(a), {}}}
			}, []float64{a, 1}},
		)
	}
	items = append(items,
		item{"gradient", func() display.Item { return display.FillGradient{Rect: rect, Gradient: grad(90, 0)} }, []float64{1}},
		item{"rounded gradient", func() display.Item { return display.FillGradient{Rect: rect, Radii: round, Gradient: grad(45, 0)} }, []float64{1}},
		item{"corner gradient", func() display.Item {
			return display.FillGradient{Rect: rect, Gradient: grad(0, css.CornerTopRight)}
		}, []float64{1}},
	)
	for _, dirty := range []float64{0.04, 0.5, 0.99} {
		for _, tc := range items {
			t.Run(tc.name+"/after alpha "+strconv.FormatFloat(dirty, 'f', -1, 64), func(t *testing.T) {
				e := newEnv(t)
				l := display.List{display.FillRect{Rect: display.Rect{W: 10, H: 10}, Color: col(dirty)}, tc.it()}
				content, _ := e.page(t, a4, l)
				e.finish(t)
				got := paintedAlphas(t, content, extGStates(t, e.buf.Bytes()))
				if len(got) < 2 {
					t.Fatalf("%d painting operators", len(got))
				}
				for _, a := range got[1:] {
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

func TestPaintedAlphasSelfCheck(t *testing.T) {
	content := "/GT1 gs\n0 0 10 10 re\nf\n/Sh1 sh\n/GT0 gs\n0 0 1 1 re\nf\nBT\n[<01>] TJ\nET\n"
	got := paintedAlphas(t, content, map[string]float64{"GT1": 0.04, "GT0": 1})
	want := []float64{0.04, 0.04, 1, 1}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("operator %d at %v, want %v", i, got[i], want[i])
		}
	}
}
