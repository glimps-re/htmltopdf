package pdf

// MoveTo starts a subpath at (x, y) (m).
func (c *Content) MoveTo(x, y float64) { c.move(x, y) }

// LineTo adds a straight segment to (x, y) (l).
func (c *Content) LineTo(x, y float64) { c.line(x, y) }

// ClosePath closes the current subpath (h).
func (c *Content) ClosePath() { c.op("h") }

// CurveTo appends a cubic Bézier segment to (x3, y3) with the control points
// (x1, y1) and (x2, y2) (c).
func (c *Content) CurveTo(x1, y1, x2, y2, x3, y3 float64) { c.curve(x1, y1, x2, y2, x3, y3) }

// LineJoin sets the line join (j): 0 miter, 1 round, 2 bevel; other values are
// an error.
func (c *Content) LineJoin(j int) { c.intOp(j, 2, "j") }

// LineCap sets the line cap (J): 0 butt, 1 round, 2 square; other values are
// an error.
func (c *Content) LineCap(j int) { c.intOp(j, 2, "J") }

func (c *Content) intOp(v, maxV int, op string) {
	if v < 0 || v > maxV {
		c.fail(ErrNumber)
		return
	}
	c.num(float64(v))
	c.op(op)
}

// FillStroke fills then strokes the current path, non-zero rule (B).
func (c *Content) FillStroke() { c.op("B") }

// FillStrokeEvenOdd fills (even-odd rule) then strokes the current path (B*).
func (c *Content) FillStrokeEvenOdd() { c.op("B*") }

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

// Dash sets the dash pattern (d): on units drawn, then off units skipped. They
// are non-negative and, once written, not both 0 (a zero on with a round cap draws
// dots); anything else is an error.
func (c *Content) Dash(on, off float64) {
	if on < 0 || off < 0 || !(on+off > 0) || appendedZero(on) && appendedZero(off) {
		c.fail(ErrNumber)
		return
	}
	c.bracket("[")
	c.num(on)
	c.num(off)
	c.bracket("] 0 ")
	c.op("d")
}

// appendedZero reports whether v is written as 0.
func appendedZero(v float64) bool {
	b, err := appendNum(nil, v)
	return err == nil && (len(b) == 1 && b[0] == '0')
}

func (c *Content) bracket(s string) {
	if c.err != nil {
		return
	}
	prev := len(c.buf)
	c.buf = append(c.buf, s...)
	c.commit(prev)
}
