package paint

import (
	"errors"
	"math"
	"strconv"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

// corners holds the elliptical radii of a box in points, in the order of
// pdf.Content.EllipticRect: the corners (x,y), (x+w,y), (x+w,y+h) and (x,y+h)
// of the y-up rectangle, that is bottom-left, bottom-right, top-right and
// top-left.
type corners struct{ rx, ry [4]float64 }

// pdfCorner maps a display corner (TL, TR, BR, BL) to its index in corners.
var pdfCorner = [4]int{3, 2, 1, 0}

// cornersOf converts and scales radii for r: when the radii of two adjacent
// corners exceed their side, all are scaled by the same factor (CSS Backgrounds
// 3 section 5.5), so neighbouring curves never overlap.
func (s *pageState) cornersOf(r ptRect, radii display.Radii) corners {
	var c corners
	for i := range radii {
		j := pdfCorner[i]
		c.rx[j] = math.Max(0, s.length(radii[i].H))
		c.ry[j] = math.Max(0, s.length(radii[i].V))
	}
	f := 1.0
	fit := func(side, sum float64) {
		if sum > side && sum > 0 {
			f = math.Min(f, side/sum)
		}
	}
	fit(r.w, c.rx[0]+c.rx[1])
	fit(r.w, c.rx[3]+c.rx[2])
	fit(r.h, c.ry[0]+c.ry[3])
	fit(r.h, c.ry[1]+c.ry[2])
	if f < 1 {
		for i := range c.rx {
			c.rx[i] *= f
			c.ry[i] *= f
		}
	}
	return c
}

func (s *pageState) ellipticPath(r ptRect, c corners) {
	s.c.EllipticRect(r.x, r.y, r.w, r.h, c.rx, c.ry)
}

// inner returns the corners of the padding box: each radius shrinks by the
// width of the adjacent side, down to 0. w is top, right, bottom, left.
func (c corners) inner(w [4]float64) corners {
	var o corners
	// pdf order: bottom-left, bottom-right, top-right, top-left.
	horiz := [4]float64{w[3], w[1], w[1], w[3]}
	vert := [4]float64{w[2], w[2], w[0], w[0]}
	for i := range c.rx {
		o.rx[i] = math.Max(0, c.rx[i]-horiz[i])
		o.ry[i] = math.Max(0, c.ry[i]-vert[i])
	}
	return o
}

// roundedBorder paints a border whose box has rounded corners: the ring between
// the outer and inner paths is clipped, then filled uniformly or side by side.
func (s *pageState) roundedBorder(r ptRect, w [4]float64, colors [4]pdf.Color, uniformColor bool, radii display.Radii) {
	outer := s.cornersOf(r, radii)
	top, right, bottom, left := clampedWidths(w, r)
	in := ptRect{x: r.x + left, y: r.y + bottom, w: r.w - left - right, h: r.h - top - bottom}
	hasInner := in.w > 0 && in.h > 0
	s.c.Save()
	s.ellipticPath(r, outer)
	if hasInner {
		s.ellipticPath(in, outer.inner([4]float64{top, right, bottom, left}))
		s.c.ClipEvenOdd()
	} else {
		s.c.Clip()
	}
	if uniformColor {
		s.c.FillColor(colors[0])
		s.c.Rect(r.x, r.y, r.w, r.h)
		s.c.Fill()
		s.c.Restore()
		return
	}
	cx, cy := r.x+r.w/2, r.y+r.h/2
	x0, x1, y0, y1 := r.x, r.right(), r.y, r.top()
	// Each side is the trapezoid between the outer edge and the diagonals
	// through the corners, extended to the center so the rounded inner corners
	// are covered.
	sides := [4][4][2]float64{
		{{x0, y1}, {x1, y1}, {cx, cy}, {cx, cy}},
		{{x1, y1}, {x1, y0}, {cx, cy}, {cx, cy}},
		{{x1, y0}, {x0, y0}, {cx, cy}, {cx, cy}},
		{{x0, y0}, {x0, y1}, {cx, cy}, {cx, cy}},
	}
	miter := [4][2]float64{
		{x0 + left, y1 - top}, {x1 - right, y1 - top}, {x1 - right, y0 + bottom}, {x0 + left, y0 + bottom},
	}
	// ext extends the miter line past the inner corner of the rectangle, up to the
	// center: the rounded inner corner leaves a nibble inside the rectangle's
	// inner corner, which a side must cover too.
	ext := func(corner, inner [2]float64) [2]float64 {
		p := [2]float64{corner[0] + 2*(inner[0]-corner[0]), corner[1] + 2*(inner[1]-corner[1])}
		return [2]float64{math.Max(x0, math.Min(x1, p[0])), math.Max(y0, math.Min(y1, p[1]))}
	}
	tl, tr, br, bl := ext([2]float64{x0, y1}, miter[0]), ext([2]float64{x1, y1}, miter[1]), ext([2]float64{x1, y0}, miter[2]), ext([2]float64{x0, y0}, miter[3])
	sides[0][2], sides[0][3] = tr, tl
	sides[1][2], sides[1][3] = br, tr
	sides[2][2], sides[2][3] = bl, br
	sides[3][2], sides[3][3] = tl, bl
	for i := range sides {
		if w[i] <= 0 {
			continue
		}
		s.fillPolygon(colors[i], sides[i][:])
	}
	s.c.Restore()
}

// gradientMatrix returns the unit-space matrix of a gradient over r: the unit
// u axis lies on the CSS gradient line (CSS Images 3 section 3.1.1), which
// crosses the center of r and is long enough for the corners to take the end
// colors. ok is false when the line has no direction or length.
func gradientMatrix(r ptRect, angle float64, corner css.Corner) (m [6]float64, ok bool) {
	var dx, dy float64 // direction in y-up space
	switch corner {
	case css.CornerTopRight:
		dx, dy = r.h, r.w
	case css.CornerBottomRight:
		dx, dy = r.h, -r.w
	case css.CornerBottomLeft:
		dx, dy = -r.h, -r.w
	case css.CornerTopLeft:
		dx, dy = -r.h, r.w
	default:
		rad := angle * math.Pi / 180
		dx, dy = math.Sin(rad), math.Cos(rad)
	}
	n := math.Hypot(dx, dy)
	if !(n > 0) || math.IsInf(n, 0) {
		return m, false
	}
	dx, dy = dx/n, dy/n
	length := math.Abs(r.w*dx) + math.Abs(r.h*dy)
	if !(length > 0) {
		return m, false
	}
	cx, cy := r.x+r.w/2, r.y+r.h/2
	return [6]float64{dx * length, dy * length, -dy, dx, cx - dx*length/2, cy - dy*length/2}, true
}

// midColor is the color halfway along the gradient line: what stands in for a
// gradient that cannot be drawn.
func midColor(stops []css.ColorStop) css.Color {
	for i := 1; i < len(stops); i++ {
		lo, hi := stops[i-1], stops[i]
		if hi.Pos >= 0.5 || i == len(stops)-1 {
			t := 0.0
			if hi.Pos > lo.Pos {
				t = math.Min(1, math.Max(0, (0.5-lo.Pos)/(hi.Pos-lo.Pos)))
			}
			mix := func(a, b float64) float64 { return a + (b-a)*t }
			return css.Color{R: mix(lo.Color.R, hi.Color.R), G: mix(lo.Color.G, hi.Color.G), B: mix(lo.Color.B, hi.Color.B), A: mix(lo.Color.A, hi.Color.A)}
		}
	}
	return css.Color{}
}

// shading returns the shading of g, reusing the last one for a run of fills
// that share their stops.
func (p *Painter) shading(g *css.Gradient) (pdf.ShadingID, error) {
	n := len(g.Stops)
	if n > 0 && p.lastStops == &g.Stops[0] && p.lastLen == n {
		return p.lastID, nil
	}
	stops := make([]pdf.Stop, n)
	for i, st := range g.Stops {
		stops[i] = pdf.Stop{Pos: st.Pos, Color: pdfColor(st.Color)}
	}
	id, err := p.w.AddGradient(pdf.Gradient{Stops: stops})
	if err == nil && n > 0 {
		p.lastStops, p.lastLen, p.lastID = &g.Stops[0], n, id
	}
	return id, err
}

func (s *pageState) fillGradient(it display.FillGradient) {
	if !positive(it.Rect.W, it.Rect.H) || len(it.Gradient.Stops) < 2 {
		return
	}
	r, ok := s.rect(it.Rect)
	if !ok || !positive(r.w, r.h) {
		return
	}
	visible, translucent := false, false
	for _, st := range it.Gradient.Stops {
		visible = visible || st.Color.A > 0
		translucent = translucent || (st.Color.A > 0 && st.Color.A < 1)
	}
	if !visible {
		return
	}
	m, ok := gradientMatrix(r, it.Gradient.Angle, it.Gradient.Corner)
	if !ok {
		s.p.b.Warn(budget.WarnClamped, "gradient line")
		return
	}
	if translucent {
		s.p.b.Warn(budget.WarnUnsupportedProperty, "gradient alpha")
	}
	id, err := s.p.shading(&it.Gradient)
	switch {
	case errors.Is(err, pdf.ErrTooManyGradients):
		s.p.b.Warn(budget.WarnClamped, "gradients > "+strconv.Itoa(pdf.MaxShadings))
		s.c.FillColor(pdfColor(midColor(it.Gradient.Stops)))
		s.path(r, it.Radii)
		s.c.Fill()
		return
	case err != nil:
		s.err = err
		return
	}
	s.c.Save()
	s.path(r, it.Radii)
	s.c.Clip()
	s.c.Concat(m[0], m[1], m[2], m[3], m[4], m[5])
	s.c.Shade(id)
	s.c.Restore()
}

// clampedWidths returns the border widths (top, right, bottom, left) that fit r.
func clampedWidths(w [4]float64, r ptRect) (top, right, bottom, left float64) {
	return math.Min(w[0], r.h), math.Min(w[1], r.w), math.Min(w[2], r.h), math.Min(w[3], r.w)
}

// fillPolygon fills the polygon pts with color.
func (s *pageState) fillPolygon(color pdf.Color, pts [][2]float64) {
	s.c.FillColor(color)
	s.c.MoveTo(pts[0][0], pts[0][1])
	for _, pt := range pts[1:] {
		s.c.LineTo(pt[0], pt[1])
	}
	s.c.ClosePath()
	s.c.Fill()
}
