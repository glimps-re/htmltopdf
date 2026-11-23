package pdf

// MoveTo starts a subpath at (x, y) (m).
func (c *Content) MoveTo(x, y float64) { c.move(x, y) }

// LineTo adds a straight segment to (x, y) (l).
func (c *Content) LineTo(x, y float64) { c.line(x, y) }

// ClosePath closes the current subpath (h).
func (c *Content) ClosePath() { c.op("h") }

// FillEvenOdd fills the current path with the even-odd rule (f*): a rectangle
// drawn inside another leaves a hole, which paints a uniform border in one
// operation.
func (c *Content) FillEvenOdd() { c.op("f*") }

// ClipEvenOdd intersects the clip with the current path, read with the
// even-odd rule, and ends the path (W* n).
func (c *Content) ClipEvenOdd() { c.op("W* n") }

// EllipticRect appends a closed rectangle path with elliptical corners. rx and
// ry are the horizontal and vertical radii of the corners (x,y), (x+w,y),
// (x+w,y+h) and (x,y+h) in that order; each is clamped to [0, w] and [0, h].
// Callers scale the radii so that adjacent ones fit their side (CSS Backgrounds
// 3 section 5.5): radii that overlap draw a self-crossing path. A rectangle
// with a non-positive side falls back to Rect.
func (c *Content) EllipticRect(x, y, w, h float64, rx, ry [4]float64) {
	if !(w > 0 && h > 0) {
		c.Rect(x, y, w, h)
		return
	}
	for i := range rx {
		if !(rx[i] > 0) {
			rx[i] = 0
		}
		if !(ry[i] > 0) {
			ry[i] = 0
		}
		rx[i], ry[i] = min(rx[i], w), min(ry[i], h)
	}
	kx, ky := [4]float64{}, [4]float64{}
	for i := range kx {
		kx[i], ky[i] = kappa*rx[i], kappa*ry[i]
	}
	c.move(x+rx[0], y)
	c.line(x+w-rx[1], y)
	c.curve(x+w-rx[1]+kx[1], y, x+w, y+ry[1]-ky[1], x+w, y+ry[1])
	c.line(x+w, y+h-ry[2])
	c.curve(x+w, y+h-ry[2]+ky[2], x+w-rx[2]+kx[2], y+h, x+w-rx[2], y+h)
	c.line(x+rx[3], y+h)
	c.curve(x+rx[3]-kx[3], y+h, x, y+h-ry[3]+ky[3], x, y+h-ry[3])
	c.line(x, y+ry[0])
	c.curve(x, y+ry[0]-ky[0], x+rx[0]-kx[0], y, x+rx[0], y)
	c.op("h")
}
