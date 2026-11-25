package svg

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func newBudget(mod func(*budget.Limits)) *budget.Budget {
	l := budget.Defaults()
	if mod != nil {
		mod(&l)
	}
	return budget.New(context.Background(), l, nil)
}

// parseSVG parses html, then its first svg element.
func parseSVG(t testing.TB, html string, b *budget.Budget) (*Image, error) {
	t.Helper()
	if b == nil {
		b = newBudget(nil)
	}
	d, err := dom.Parse(strings.NewReader(html), newBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	for i := range d.Elements {
		if d.Elements[i].NS == dom.NSSVG && elementName(&d.Elements[i]) == "svg" {
			return Parse(d, int32(i), b)
		}
	}
	t.Fatal("no svg element")
	return nil, nil
}

func mustSVG(t testing.TB, html string) *Image {
	t.Helper()
	img, err := parseSVG(t, html, nil)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

const eps = 1e-9

func near(a, b float64) bool { return math.Abs(a-b) <= eps }

func pt(x, y float64) Point { return Point{x, y} }

func mv(x, y float64) Seg  { return Seg{Op: MoveTo, P: [3]Point{pt(x, y)}} }
func ln(x, y float64) Seg  { return Seg{Op: LineTo, P: [3]Point{pt(x, y)}} }
func cu(a, b, c Point) Seg { return Seg{Op: CubicTo, P: [3]Point{a, b, c}} }
func closeSeg() Seg        { return Seg{Op: Close} }

func segsEqual(a, b []Seg) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Op != b[i].Op {
			return false
		}
		n := 0
		switch a[i].Op {
		case MoveTo, LineTo:
			n = 1
		case CubicTo:
			n = 3
		}
		for k := range n {
			if !near(a[i].P[k].X, b[i].P[k].X) || !near(a[i].P[k].Y, b[i].P[k].Y) {
				return false
			}
		}
	}
	return true
}
