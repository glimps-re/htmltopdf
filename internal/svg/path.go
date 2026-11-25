package svg

import (
	"errors"
	"math"
	"strconv"
)

// Op is a path operation.
type Op uint8

// Path operations. MoveTo and LineTo use P[0]; CubicTo uses P[0..2] (two
// control points, then the end point); Close uses none.
const (
	MoveTo Op = iota
	LineTo
	CubicTo
	Close
)

// Point is a point in viewBox units.
type Point struct{ X, Y float64 }

// Seg is one absolute path segment.
type Seg struct {
	Op Op
	P  [3]Point
}

// MaxCoord bounds the absolute value of every number read from the document,
// so that no later arithmetic can overflow.
const MaxCoord = 1e7

// MaxPathBytes bounds the bytes of one d (or points) attribute; a longer
// attribute drops its shape.
const MaxPathBytes = 65536

func clamp(v float64) float64 {
	switch {
	case v != v:
		return 0
	case v > MaxCoord:
		return MaxCoord
	case v < -MaxCoord:
		return -MaxCoord
	}
	return v
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// scanner reads the numbers of a path or points attribute.
type scanner struct {
	s string
	i int
}

func (sc *scanner) skipSpace() {
	for sc.i < len(sc.s) && isSpace(sc.s[sc.i]) {
		sc.i++
	}
}

// skipSep skips whitespace and at most one comma, the separator between
// numbers (SVG 2 §9.3.9).
func (sc *scanner) skipSep() {
	sc.skipSpace()
	if sc.i < len(sc.s) && sc.s[sc.i] == ',' {
		sc.i++
		sc.skipSpace()
	}
}

func (sc *scanner) atNumber() bool {
	if sc.i >= len(sc.s) {
		return false
	}
	c := sc.s[sc.i]
	return c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+'
}

// number reads one number: sign? (digits [. digits*] | . digits) (e sign? digits)?.
// The value is clamped to ±MaxCoord. On failure the position is unchanged.
func (sc *scanner) number() (float64, bool) {
	s, start := sc.s, sc.i
	i := start
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		frac := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			frac++
		}
		if frac == 0 {
			// Chrome rejects "5.": no digit after the dot.
			return 0, false
		}
		digits += frac
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '-' || s[j] == '+') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	v, err := strconv.ParseFloat(s[start:i], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	sc.i = i
	return clamp(v), true
}

// flag reads an arc flag: a single 0 or 1, not necessarily followed by a
// separator ("a1 1 0 0110 10" is valid).
func (sc *scanner) flag() (bool, bool) {
	if sc.i >= len(sc.s) {
		return false, false
	}
	switch sc.s[sc.i] {
	case '0':
		sc.i++
		return false, true
	case '1':
		sc.i++
		return true, true
	}
	return false, false
}

// argCount is the number of arguments of a path command (upper case).
func argCount(c byte) int {
	switch c {
	case 'M', 'L', 'T':
		return 2
	case 'H', 'V':
		return 1
	case 'C':
		return 6
	case 'S', 'Q':
		return 4
	case 'A':
		return 7
	}
	return 0
}

func isCommand(c byte) bool {
	switch c | 0x20 {
	case 'm', 'l', 'h', 'v', 'c', 's', 'q', 't', 'a', 'z':
		return true
	}
	return false
}

// pathState is the current point and the data the smooth commands reflect.
type pathState struct {
	segs       []Seg
	cur, start Point
	// ctrl is the last control point of the previous command and ctrlKind
	// tells whether it is a cubic ('C') or quadratic ('Q') one, 0 for none.
	ctrl      Point
	ctrlKind  byte
	afterZ    bool
	haveStart bool
}

func (p *pathState) lineTo(pt Point) {
	p.beginDraw()
	p.segs = append(p.segs, Seg{Op: LineTo, P: [3]Point{pt}})
	p.cur = pt
}

func (p *pathState) cubicTo(c1, c2, pt Point) {
	p.beginDraw()
	p.segs = append(p.segs, Seg{Op: CubicTo, P: [3]Point{c1, c2, pt}})
	p.cur = pt
}

// beginDraw starts a new subpath at the start point of the closed one, as a
// drawing command after a closepath does (SVG 2 §9.3.4).
func (p *pathState) beginDraw() {
	if p.afterZ {
		p.segs = append(p.segs, Seg{Op: MoveTo, P: [3]Point{p.cur}})
		p.afterZ = false
	}
}

// ParsePath parses SVG path data (SVG 2 §9.3): every command, relative forms,
// implicit repeats, arcs as at most 4 cubics and quadratics as cubics. The
// path ends at its first error and the segments before it are kept. ok is false
// when d is longer than MaxPathBytes, which drops the path.
func ParsePath(d string) (segs []Seg, ok bool) {
	if len(d) > MaxPathBytes {
		return nil, false
	}
	return parsePath(d, math.MaxInt), true
}

// parsePath reads path data and stops as soon as it holds more than limit
// segments, which the caller reads as an overflow.
func parsePath(d string, limit int) []Seg {
	sc := scanner{s: d}
	var p pathState
	var cmd byte
	var a [7]float64
	sc.skipSpace()
	for sc.i < len(sc.s) {
		c := sc.s[sc.i]
		switch {
		case isCommand(c):
			sc.i++
			cmd = c
			if !p.haveStart && cmd|0x20 != 'm' {
				return p.segs
			}
			sc.skipSpace()
			if cmd|0x20 == 'z' {
				p.closePath()
				continue
			}
		case cmd != 0 && cmd|0x20 != 'z' && sc.atNumber():
			// Implicit repeat; after a moveto the repeats are linetos.
			switch cmd {
			case 'M':
				cmd = 'L'
			case 'm':
				cmd = 'l'
			}
		default:
			return p.segs
		}
		upper := cmd &^ 0x20
		n := argCount(upper)
		for k := 0; k < n; k++ {
			if upper == 'A' && (k == 3 || k == 4) {
				f, ok := sc.flag()
				if !ok {
					return p.segs
				}
				a[k] = 0
				if f {
					a[k] = 1
				}
			} else {
				v, ok := sc.number()
				if !ok {
					return p.segs
				}
				a[k] = v
			}
			if k < n-1 {
				sc.skipSep()
			} else {
				sc.skipSpace()
			}
		}
		p.apply(cmd, &a)
		if len(p.segs) > limit {
			return p.segs
		}
	}
	return p.segs
}

func (p *pathState) closePath() {
	if p.afterZ {
		return
	}
	p.segs = append(p.segs, Seg{Op: Close})
	p.cur = p.start
	p.afterZ = true
	p.ctrlKind = 0
}

func (p *pathState) apply(cmd byte, a *[7]float64) {
	rel := cmd >= 'a'
	var ox, oy float64
	if rel {
		ox, oy = p.cur.X, p.cur.Y
	}
	pt := func(i int) Point { return Point{clamp(a[i] + ox), clamp(a[i+1] + oy)} }
	kind := byte(0)
	switch cmd &^ 0x20 {
	case 'M':
		p.cur = pt(0)
		p.start = p.cur
		p.haveStart = true
		p.afterZ = false
		p.segs = append(p.segs, Seg{Op: MoveTo, P: [3]Point{p.cur}})
	case 'L':
		p.lineTo(pt(0))
	case 'H':
		p.lineTo(Point{clamp(a[0] + ox), p.cur.Y})
	case 'V':
		p.lineTo(Point{p.cur.X, clamp(a[0] + oy)})
	case 'C':
		p.cubicTo(pt(0), pt(2), pt(4))
		p.ctrl, kind = pt(2), 'C'
	case 'S':
		c1 := p.cur
		if p.ctrlKind == 'C' {
			c1 = Point{clamp(2*p.cur.X - p.ctrl.X), clamp(2*p.cur.Y - p.ctrl.Y)}
		}
		p.cubicTo(c1, pt(0), pt(2))
		p.ctrl, kind = pt(0), 'C'
	case 'Q':
		p.quadTo(pt(0), pt(2))
		p.ctrl, kind = pt(0), 'Q'
	case 'T':
		q := p.cur
		if p.ctrlKind == 'Q' {
			q = Point{clamp(2*p.cur.X - p.ctrl.X), clamp(2*p.cur.Y - p.ctrl.Y)}
		}
		p.quadTo(q, pt(0))
		p.ctrl, kind = q, 'Q'
	case 'A':
		p.arcTo(a[0], a[1], a[2], a[3] != 0, a[4] != 0, pt(5))
	}
	p.ctrlKind = kind
}

func (p *pathState) quadTo(q, pt Point) {
	p0 := p.cur
	p.cubicTo(
		Point{clamp(p0.X + 2.0/3*(q.X-p0.X)), clamp(p0.Y + 2.0/3*(q.Y-p0.Y))},
		Point{clamp(pt.X + 2.0/3*(q.X-pt.X)), clamp(pt.Y + 2.0/3*(q.Y-pt.Y))},
		pt,
	)
}

// arcTo appends the elliptical arc from the current point to p1 (SVG 2
// §B.2.4 endpoint to center conversion): no segment when the endpoints are
// equal, a line when a radius is 0, radii too small for the endpoints scaled
// up, at most 4 cubics otherwise.
func (p *pathState) arcTo(rx, ry, rotDeg float64, large, sweep bool, p1 Point) {
	p0 := p.cur
	p1 = Point{clamp(p1.X), clamp(p1.Y)}
	if p0 == p1 {
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.lineTo(p1)
		return
	}
	phi := rotDeg * math.Pi / 180
	sinP, cosP := math.Sincos(phi)
	dx, dy := (p0.X-p1.X)/2, (p0.Y-p1.Y)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	rx2, ry2 := rx*rx, ry*ry
	den := rx2*y1p*y1p + ry2*x1p*x1p
	if den == 0 || math.IsInf(den, 0) || math.IsInf(rx2*ry2, 0) {
		p.lineTo(p1)
		return
	}
	co := math.Sqrt(math.Max(0, (rx2*ry2-den)/den))
	if large == sweep {
		co = -co
	}
	cxp := co * rx * y1p / ry
	cyp := -co * ry * x1p / rx
	cx := cosP*cxp - sinP*cyp + (p0.X+p1.X)/2
	cy := sinP*cxp + cosP*cyp + (p0.Y+p1.Y)/2
	th1 := math.Atan2((y1p-cyp)/ry, (x1p-cxp)/rx)
	th2 := math.Atan2((-y1p-cyp)/ry, (-x1p-cxp)/rx)
	dth := th2 - th1
	if math.IsNaN(dth) || math.IsInf(dth, 0) {
		p.lineTo(p1)
		return
	}
	switch {
	case sweep && dth < 0:
		dth += 2 * math.Pi
	case !sweep && dth > 0:
		dth -= 2 * math.Pi
	}
	n := min(4, max(1, int(math.Ceil(math.Abs(dth)/(math.Pi/2)-1e-9))))
	delta := dth / float64(n)
	t := 4.0 / 3 * math.Tan(delta/4)
	at := func(ux, uy float64) Point {
		return Point{clamp(cx + rx*ux*cosP - ry*uy*sinP), clamp(cy + rx*ux*sinP + ry*uy*cosP)}
	}
	for k := range n {
		s1, c1 := math.Sincos(th1 + float64(k)*delta)
		s2, c2 := math.Sincos(th1 + float64(k+1)*delta)
		end := at(c2, s2)
		if k == n-1 {
			end = p1
		}
		p.cubicTo(at(c1-t*s1, s1+t*c1), at(c2+t*s2, s2-t*c2), end)
	}
}
