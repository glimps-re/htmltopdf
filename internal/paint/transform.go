package paint

import (
	"math"
	"slices"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

var identity = [6]float64{1, 0, 0, 1, 0, 0}

// mul returns the matrix that applies b, then a.
func mul(a, b [6]float64) [6]float64 {
	return [6]float64{
		a[0]*b[0] + a[2]*b[1], a[1]*b[0] + a[3]*b[1],
		a[0]*b[2] + a[2]*b[3], a[1]*b[2] + a[3]*b[3],
		a[0]*b[4] + a[2]*b[5] + a[4], a[1]*b[4] + a[3]*b[5] + a[5],
	}
}

// Bounds of a usable matrix: the linear part within maxLinear, the translation
// within what the PDF number formatter writes, and a determinant that is not
// nearly singular (a singular cm hides everything it encloses anyway).
const (
	maxLinear = 1e6
	minDet    = 1e-6
)

func usable(m [6]float64) bool {
	for i, v := range m {
		limit := maxLinear
		if i >= 4 {
			limit = pdf.MaxCoord
		}
		if !(math.Abs(v) <= limit) {
			return false
		}
	}
	// The determinant of the matrix as the formatter writes it, with four decimals.
	r := func(v float64) float64 { return math.Round(v*1e4) / 1e4 }
	return math.Abs(r(m[0])*r(m[3])-r(m[1])*r(m[2])) >= minDet
}

// toPt and fromPt convert between layout px and points without clamping.
func (s *pageState) toPt(x, y float64) (float64, float64) {
	return (s.g.Area.X + s.g.Scale*x) * PxToPt, (s.g.Height - s.g.Area.Y - s.g.Scale*y) * PxToPt
}

func (s *pageState) fromPt(x, y float64) (float64, float64) {
	return (x/PxToPt - s.g.Area.X) / s.g.Scale, (s.g.Height - s.g.Area.Y - y/PxToPt) / s.g.Scale
}

// pdfMatrix converts a layout-px matrix to the equivalent matrix of the page
// space in points (conjugation by the px to pt map).
func (s *pageState) pdfMatrix(m [6]float64) [6]float64 {
	image := func(x, y float64) (float64, float64) {
		lx, ly := s.fromPt(x, y)
		return s.toPt(m[0]*lx+m[2]*ly+m[4], m[1]*lx+m[3]*ly+m[5])
	}
	e, f := image(0, 0)
	ax, ay := image(1, 0)
	cx, cy := image(0, 1)
	return [6]float64{ax - e, ay - f, cx - e, cy - f, e, f}
}

// mappedBounds returns the bounding box of r mapped by the current transform.
func (s *pageState) mappedBounds(r display.Rect) (x0, x1, y0, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	m := s.ctm
	for _, c := range [4][2]float64{{r.X, r.Y}, {r.Right(), r.Y}, {r.Right(), r.Bottom()}, {r.X, r.Bottom()}} {
		x, y := m[0]*c[0]+m[2]*c[1]+m[4], m[1]*c[0]+m[3]*c[1]+m[5]
		x0, x1 = math.Min(x0, x), math.Max(x1, x)
		y0, y1 = math.Min(y0, y), math.Max(y1, y)
	}
	return x0, x1, y0, y1
}

func (s *pageState) pushTransform(it display.PushTransform) {
	prev := s.ctm
	total := mul(prev, it.M)
	pm := s.pdfMatrix(it.M)
	switch {
	case len(s.stack) >= MaxClipDepth:
		s.p.b.Warn(budget.WarnClamped, "transform nesting")
	case !usable(total) || !usable(pm):
		s.p.b.Warn(budget.WarnClamped, "transform")
	default:
		s.c.Save()
		s.c.Concat(pm[0], pm[1], pm[2], pm[3], pm[4], pm[5])
		s.stack = append(s.stack, group{drawn: true, transform: true, prev: prev})
		s.ctm = total
		s.xforms++
		return
	}
	// The transform cannot be applied (too deep, or unusable): what it encloses is
	// drawn without it, never hidden.
	s.stack = append(s.stack, group{transform: true, prev: prev})
}

func (s *pageState) popTransform() {
	g, ok := s.pop(true)
	if !ok {
		return
	}
	s.ctm = g.prev
	if g.drawn {
		s.xforms--
		s.c.Restore()
	}
}

// drawPath draws a display.Path: fill and stroke of its segments, inside a
// q/Q so that its stroke parameters and opacity do not leak.
func (s *pageState) drawPath(it display.Path) {
	fill, stroke := it.Fill.A > 0, it.Stroke.A > 0 && it.Width > 0
	if !fill && !stroke || len(it.Segs) < 2 || !slices.ContainsFunc(it.Segs, func(sg display.PathSeg) bool { return sg.Op == display.PathMoveTo }) {
		return
	}
	s.c.Save()
	defer s.c.Restore()
	if stroke {
		s.c.LineWidth(s.length(it.Width))
		s.c.LineJoin(int(min(it.Join, 2)))
		s.c.LineCap(int(min(it.Cap, 2)))
	}
	// One painting operator when both paints share an alpha (the opacity is one
	// graphics-state value); otherwise two passes over the same path.
	if fill && stroke && it.Fill.A == it.Stroke.A {
		s.c.FillColor(pdfColor(it.Fill))
		s.c.StrokeColor(pdfColor(it.Stroke))
		s.segs(it.Segs)
		if it.EvenOdd {
			s.c.FillStrokeEvenOdd()
		} else {
			s.c.FillStroke()
		}
		return
	}
	if fill {
		s.c.FillColor(pdfColor(it.Fill))
		s.segs(it.Segs)
		if it.EvenOdd {
			s.c.FillEvenOdd()
		} else {
			s.c.Fill()
		}
	}
	if stroke {
		s.c.StrokeColor(pdfColor(it.Stroke))
		s.segs(it.Segs)
		s.c.Stroke()
	}
}

// segs appends the path of segs; segments before the first MoveTo are dropped,
// as a path must start with one.
func (s *pageState) segs(segs []display.PathSeg) {
	started := false
	for _, sg := range segs {
		switch sg.Op {
		case display.PathMoveTo:
			s.c.MoveTo(s.x(sg.P[0].X), s.y(sg.P[0].Y))
			started = true
		case display.PathLineTo:
			if started {
				s.c.LineTo(s.x(sg.P[0].X), s.y(sg.P[0].Y))
			}
		case display.PathCubicTo:
			if started {
				s.c.CurveTo(s.x(sg.P[0].X), s.y(sg.P[0].Y), s.x(sg.P[1].X), s.y(sg.P[1].Y), s.x(sg.P[2].X), s.y(sg.P[2].Y))
			}
		case display.PathClose:
			if started {
				s.c.ClosePath()
			}
		}
	}
}
