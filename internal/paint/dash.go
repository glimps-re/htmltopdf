package paint

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

// maxClosedDashes bounds the dashes of a closed path around rounded corners, and
// of a side under a transform, which are not clipped to the page: a longer one is drawn solid, so that a viewer
// never has to draw millions of them. A straight side is clipped to the page and
// has no bound.
const maxClosedDashes = 1 << 12

// minPattern is the shortest period, in pt, that the content stream can write.
const minPattern = 1e-3

type dashKind uint8

const (
	dashSolid dashKind = iota
	dashButt
	dashRound
)

// dashPattern returns the dash pattern Chrome draws on a side of width w and
// length l (layout px): dashes of 3w and gaps of 2w below 3 px (2w and w from
// 3 px), dots as wide as the border, round from 4 px; the gap is adjusted so
// that the pattern fills the side with a dash at both ends (a closed path has
// no ends: it is filled by a whole number of periods). on is 0 for dots.
func dashPattern(style display.LineStyle, w, l float64, closed bool) (on, off float64, kind dashKind) {
	if !(w > 0) || !(l > 0) || math.IsInf(l, 0) || style == display.LineSolid {
		return 0, 0, dashSolid
	}
	dash, gap := w, w
	if style == display.LineDashed {
		dash, gap = 3*w, 2*w
		if w >= 3 {
			dash, gap = 2*w, w
		}
	}
	if l <= 2*dash {
		return 0, 0, dashSolid
	}
	if l <= 2*dash+gap {
		gap = l - 2*dash
	} else {
		gap = bestDashGap(l, dash, gap, closed)
	}
	if closed && l/(dash+gap) > maxClosedDashes+1e-9 {
		return 0, 0, dashSolid
	}
	if style == display.LineDotted && w > 3 {
		return 0, dash + gap, dashRound
	}
	return dash, gap, dashButt
}

// bestDashGap returns the gap closest to gap that makes a whole number of dashes
// fill a stroke of the given length.
func bestDashGap(length, dash, gap float64, closed bool) float64 {
	avail := length + gap
	gaps := -1.0
	if closed {
		avail, gaps = length, 0
	}
	lo := math.Floor(avail / (dash + gap))
	hi := lo + 1
	minGap := math.Inf(1)
	if lo+gaps > 0 {
		minGap = (length - lo*dash) / (lo + gaps)
	}
	maxGap := (length - hi*dash) / (hi + gaps)
	if maxGap <= 0 || math.Abs(minGap-gap) < math.Abs(maxGap-gap) {
		return minGap
	}
	return maxGap
}

// dashedBorder paints a border without radii where some side is dashed or
// dotted: the solid sides as mitered polygons, the others as a stroked line along
// the whole side, corners included, as Chrome draws them.
func (s *pageState) dashedBorder(it display.Border, r ptRect, w [4]float64) {
	top, right, bottom, left := clampedWidths(w, r)
	cw := [4]float64{top, right, bottom, left}
	x0, x1, y0, y1 := r.x, r.right(), r.y, r.top()
	polys := [4][4][2]float64{
		{{x0, y1}, {x1, y1}, {x1 - right, y1 - top}, {x0 + left, y1 - top}},
		{{x1, y1}, {x1, y0}, {x1 - right, y0 + bottom}, {x1 - right, y1 - top}},
		{{x1, y0}, {x0, y0}, {x0 + left, y0 + bottom}, {x1 - right, y0 + bottom}},
		{{x0, y0}, {x0, y1}, {x0 + left, y1 - top}, {x0 + left, y0 + bottom}},
	}
	lengths := [4]float64{it.Rect.W, it.Rect.H, it.Rect.W, it.Rect.H}
	for i, side := range it.Sides {
		if w[i] <= 0 || cw[i] <= 0 {
			continue
		}
		color := pdfColor(side.Color)
		pw := side.Width
		if side.PatternWidth > 0 {
			pw = side.PatternWidth
		}
		on, off, kind := dashPattern(side.Style, pw, lengths[i]+side.Trail-side.Lead, false)
		k := s.length(1)
		if (on+off)*k < minPattern || s.xforms > 0 && (lengths[i]+side.Trail-side.Lead)/(on+off) > maxClosedDashes {
			// Under a transform the stroke is not clipped to the page: it is bounded
			// like a closed path.
			kind = dashSolid
		}
		shared := side.PatternWidth > 0
		thick := cw[i]
		if shared {
			thick = s.length(pw)
		}
		if kind == dashSolid {
			if shared {
				s.fillBand(i, r, thick, color)
			} else {
				s.fillPolygon(color, polys[i][:])
			}
			continue
		}
		clip := false
		for _, j := range [2]int{(i + 1) % 4, (i + 3) % 4} {
			clip = clip || !shared && w[j] > 0 && it.Sides[j].Color != side.Color
		}
		s.c.Save()
		if clip {
			s.c.MoveTo(polys[i][0][0], polys[i][0][1])
			for _, p := range polys[i][1:] {
				s.c.LineTo(p[0], p[1])
			}
			s.c.ClosePath()
			s.c.Clip()
		}
		s.dashLine(i, r, thick, side, color, on*k, off*k, kind)
		s.c.Restore()
	}
}

// dashLine strokes side i (top, right, bottom, left) of r along its center, from
// the corner where Chrome starts: left to right for the horizontal sides, top to
// bottom for the vertical ones.
func (s *pageState) dashLine(i int, r ptRect, w float64, side display.BorderSide, color pdf.Color, on, off float64, kind dashKind) {
	in := w / 2
	if side.PatternWidth > 0 {
		in = 0
	}
	var ax, ay, bx, by float64
	switch i {
	case 0:
		ax, ay, bx, by = r.x, r.top()-in, r.right(), r.top()-in
	case 1:
		ax, ay, bx, by = r.right()-in, r.top(), r.right()-in, r.y
	case 2:
		ax, ay, bx, by = r.x, r.y+in, r.right(), r.y+in
	default:
		ax, ay, bx, by = r.x+in, r.top(), r.x+in, r.y
	}
	if side.Lead != 0 || side.Trail != 0 {
		d := math.Hypot(bx-ax, by-ay)
		if d > 0 {
			ux, uy := (bx-ax)/d, (by-ay)/d
			lead, trail := s.length(side.Lead), s.length(side.Trail)
			ax, ay, bx, by = ax+ux*lead, ay+uy*lead, bx+ux*trail, by+uy*trail
		}
	}
	if kind == dashRound {
		// The first dot touches the corner: its center is half a dot in, and the
		// line ends a quarter period after the last one so that it is drawn.
		d := math.Hypot(bx-ax, by-ay)
		if !(d > w) {
			return
		}
		ux, uy := (bx-ax)/d, (by-ay)/d
		ax, ay = ax+ux*w/2, ay+uy*w/2
		end := math.Round((d-w)/off)*off + off/4
		bx, by = ax+ux*end, ay+uy*end
		s.c.LineCap(1)
	} else {
		s.c.LineCap(0)
	}
	ax, ay, bx, by, ok := s.clipToPage(ax, ay, bx, by, on+off, w)
	if !ok {
		return
	}
	s.c.StrokeColor(color)
	s.c.LineWidth(w)
	s.c.Dash(on, off)
	s.c.MoveTo(ax, ay)
	s.c.LineTo(bx, by)
	s.c.Stroke()
}

// hasDashes reports whether a visible side of it is dashed or dotted.
func hasDashes(it display.Border, w [4]float64) bool {
	for i, side := range it.Sides {
		if w[i] > 0 && side.Style != display.LineSolid {
			return true
		}
	}
	return false
}

// roundedDashed paints a dashed or dotted border of one width, color and style
// around rounded corners as Chrome does: one closed path along the middle of the
// border, from the top edge after the top-left corner, clockwise. It reports
// false for anything else (the caller paints the ring solid).
func (s *pageState) roundedDashed(it display.Border, r ptRect, w [4]float64, radii display.Radii) bool {
	first := it.Sides[0]
	for i, side := range it.Sides {
		if w[i] <= 0 || side.Width != first.Width || side.Color != first.Color || side.Style != first.Style || side.Style == display.LineSolid {
			return false
		}
	}
	wp := math.Min(w[0], math.Min(r.w, r.h)/2)
	if !(wp > 0) || 2*wp >= math.Min(r.w, r.h) {
		return false
	}
	c := s.cornersOf(r, radii)
	cr := ptRect{x: r.x + wp/2, y: r.y + wp/2, w: r.w - wp, h: r.h - wp}
	var rx, ry [4]float64
	for i := range c.rx {
		rx[i], ry[i] = math.Max(0, c.rx[i]-wp/2), math.Max(0, c.ry[i]-wp/2)
	}
	// pdf order of the corners: bottom-left, bottom-right, top-right, top-left.
	bl, br, tr, tl := 0, 1, 2, 3
	length := cr.w - rx[tl] - rx[tr] + cr.w - rx[br] - rx[bl] + cr.h - ry[tr] - ry[br] + cr.h - ry[bl] - ry[tl]
	for i := range rx {
		length += quarterEllipse(rx[i], ry[i])
	}
	k := s.length(1)
	on, off, kind := dashPattern(first.Style, first.Width, length/k, true)
	if kind == dashSolid || (on+off)*k < minPattern {
		return false
	}
	x0, x1, y0, y1 := cr.x, cr.right(), cr.y, cr.top()
	s.c.Save()
	s.c.StrokeColor(pdfColor(first.Color))
	s.c.LineWidth(wp)
	s.c.LineCap(0)
	if kind == dashRound {
		s.c.LineCap(1)
	}
	s.c.Dash(on*k, off*k)
	s.c.MoveTo(x0+rx[tl], y1)
	s.c.LineTo(x1-rx[tr], y1)
	cornerArc(s.c, x1-rx[tr], y1, x1, y1-ry[tr], rx[tr], ry[tr], true)
	s.c.LineTo(x1, y0+ry[br])
	cornerArc(s.c, x1, y0+ry[br], x1-rx[br], y0, rx[br], ry[br], false)
	s.c.LineTo(x0+rx[bl], y0)
	cornerArc(s.c, x0+rx[bl], y0, x0, y0+ry[bl], rx[bl], ry[bl], true)
	s.c.LineTo(x0, y1-ry[tl])
	cornerArc(s.c, x0, y1-ry[tl], x0+rx[tl], y1, rx[tl], ry[tl], false)
	s.c.ClosePath()
	s.c.Stroke()
	s.c.Restore()
	return true
}

// quarterEllipse approximates the length of a quarter of an ellipse of radii a
// and b (Ramanujan).
func quarterEllipse(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return math.Pi / 4 * (3*(a+b) - math.Sqrt((3*a+b)*(a+3*b)))
}

// cornerArc appends the quarter ellipse from (x0, y0) to (x1, y1), as one Bézier
// curve, leaving along x when horizontal and along y otherwise.
func cornerArc(c *pdf.Content, x0, y0, x1, y1, rx, ry float64, horizontal bool) {
	const kappa = 0.5522847498307936
	if rx <= 0 || ry <= 0 {
		c.LineTo(x1, y1)
		return
	}
	sx, sy := math.Copysign(1, x1-x0), math.Copysign(1, y1-y0)
	if horizontal {
		c.CurveTo(x0+sx*kappa*rx, y0, x1, y1-sy*kappa*ry, x1, y1)
		return
	}
	c.CurveTo(x0, y0+sy*kappa*ry, x1-sx*kappa*rx, y1, x1, y1)
}

// fillBand fills the band of width w centered on side i of r.
func (s *pageState) fillBand(i int, r ptRect, w float64, color pdf.Color) {
	s.c.FillColor(color)
	switch i {
	case 0:
		s.c.Rect(r.x, r.top()-w/2, r.w, w)
	case 1:
		s.c.Rect(r.right()-w/2, r.y, w, r.h)
	case 2:
		s.c.Rect(r.x, r.y-w/2, r.w, w)
	default:
		s.c.Rect(r.x-w/2, r.y, w, r.h)
	}
	s.c.Fill()
}

// clipToPage shortens the axis-aligned line from (ax, ay) to (bx, by) to the part
// that can be seen (the page and a margin of one period and the width), cut at a
// whole number of periods from its start so that the dashes keep their place;
// ok is false when none of it is on the page.
func (s *pageState) clipToPage(ax, ay, bx, by, period, w float64) (cx, cy, dx, dy float64, ok bool) {
	d := math.Hypot(bx-ax, by-ay)
	if s.xforms > 0 || !(period > 0) {
		return ax, ay, bx, by, d > 0
	}
	if !(d > 0) {
		return ax, ay, bx, by, false
	}
	ux, uy := (bx-ax)/d, (by-ay)/d
	margin := period + w
	lo, hi := -margin, s.g.Width*PxToPt+margin
	a, u := ax, ux
	cross, crossMax := ay, s.g.Height*PxToPt
	if uy != 0 {
		lo, hi = -margin, s.g.Height*PxToPt+margin
		a, u = ay, uy
		cross, crossMax = ax, s.g.Width*PxToPt
	}
	if cross < -margin || cross > crossMax+margin {
		return ax, ay, ax, ay, false
	}
	t0, t1 := lo-a, hi-a
	if u < 0 {
		t0, t1 = a-hi, a-lo
	}
	t0 = math.Floor(math.Max(0, t0)/period) * period
	t1 = math.Min(d, t1)
	if !(t1 > t0) {
		return ax, ay, ax, ay, false
	}
	return ax + ux*t0, ay + uy*t0, ax + ux*t1, ay + uy*t1, true
}
