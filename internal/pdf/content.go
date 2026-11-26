package pdf

import (
	"errors"
	"math"
	"strconv"
)

// Limits of a Content.
const (
	// MaxSaveDepth is the maximum nesting of Content.Save.
	MaxSaveDepth = 256
	// MaxContentBytes is the maximum size of one uncompressed content stream.
	MaxContentBytes = 16 << 20
)

// Errors recorded by Content and returned by Writer when a body is added.
var (
	ErrSaveDepth   = errors.New("pdf: graphics state nesting too deep")
	ErrUnbalanced  = errors.New("pdf: unbalanced save/restore")
	ErrContentSize = errors.New("pdf: content stream too large")
)

// opacitySet is a set of transparency levels (0-255) used by contents.
type opacitySet [4]uint64

func (s *opacitySet) add(t uint8) { s[t>>6] |= 1 << (t & 63) }

func (s *opacitySet) has(t uint8) bool { return s[t>>6]>>(t&63)&1 != 0 }

func (s *opacitySet) merge(o opacitySet) {
	for i, bits := range o {
		s[i] |= bits
	}
}

// Color is an sRGB color with components in [0, 1]; out-of-range components
// (including infinities) are clamped and NaN is an error.
type Color struct{ R, G, B, A float64 }

const kappa = 0.5522847498307936 // Bézier circle approximation

// Content builds one page content stream. Operators never fail individually:
// the first error is kept and returned by Err, and a Content with an error is
// refused by the Writer. The zero value is ready to use.
type Content struct {
	buf   []byte
	err   error
	depth int
	// transparency is 255 - opacity, so that the zero value is opaque.
	transparency uint8
	stack        [MaxSaveDepth]uint8
	opacities    opacitySet
	fonts        []fontUse
	shadings     []*shadingEntry
	images       []*imageEntry
	tmp          []byte
}

// Bytes returns the operators written so far. It is valid until the next
// call on c.
func (c *Content) Bytes() []byte { return c.buf }

// Err returns the first error recorded by an operator.
func (c *Content) Err() error { return c.err }

// Depth returns the number of unmatched Save calls.
func (c *Content) Depth() int { return c.depth }

func (c *Content) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// commit enforces MaxContentBytes after an append that started at prev.
func (c *Content) commit(prev int) {
	if len(c.buf) > MaxContentBytes {
		c.buf = c.buf[:prev]
		c.fail(ErrContentSize)
	}
}

// num appends an operand; nothing is appended once an error is recorded.
func (c *Content) num(v float64) {
	if c.err != nil {
		return
	}
	prev := len(c.buf)
	var err error
	c.buf, err = appendNum(c.buf, v)
	if err != nil {
		c.fail(err)
		return
	}
	c.buf = append(c.buf, ' ')
	c.commit(prev)
}

func (c *Content) op(name string) {
	if c.err != nil {
		return
	}
	prev := len(c.buf)
	c.buf = append(c.buf, name...)
	c.buf = append(c.buf, '\n')
	c.commit(prev)
}

// Save pushes the graphics state (q).
func (c *Content) Save() {
	if c.err != nil {
		return
	}
	if c.depth >= MaxSaveDepth {
		c.fail(ErrSaveDepth)
		return
	}
	c.stack[c.depth] = c.transparency
	c.depth++
	c.op("q")
}

// Restore pops the graphics state (Q).
func (c *Content) Restore() {
	if c.err != nil {
		return
	}
	if c.depth == 0 {
		c.fail(ErrUnbalanced)
		return
	}
	c.depth--
	c.transparency = c.stack[c.depth]
	c.op("Q")
}

// Concat multiplies the current matrix by [a b c d e f] (cm).
func (c *Content) Concat(a, b, cc, d, e, f float64) {
	c.num(a)
	c.num(b)
	c.num(cc)
	c.num(d)
	c.num(e)
	c.num(f)
	c.op("cm")
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

func (c *Content) color(col Color, op string) {
	if math.IsNaN(col.R) || math.IsNaN(col.G) || math.IsNaN(col.B) || math.IsNaN(col.A) {
		c.fail(ErrNumber)
		return
	}
	if c.err != nil {
		return
	}
	t := 255 - uint8(math.Round(clamp01(col.A)*255))
	if t != c.transparency {
		prev := len(c.buf)
		c.buf = append(c.buf, "/GT"...)
		c.buf = strconv.AppendUint(c.buf, uint64(t), 10)
		c.buf = append(c.buf, " gs\n"...)
		if c.commit(prev); c.err != nil {
			return
		}
		c.transparency = t
		c.opacities.add(t)
	}
	c.num(clamp01(col.R))
	c.num(clamp01(col.G))
	c.num(clamp01(col.B))
	c.op(op)
}

// FillColor sets the fill color (rg) and the opacity from col.A. Opacity is
// shared by fill and stroke: the last color set wins until the next Restore.
func (c *Content) FillColor(col Color) { c.color(col, "rg") }

// StrokeColor sets the stroke color (RG); see FillColor for the opacity.
func (c *Content) StrokeColor(col Color) { c.color(col, "RG") }

// LineWidth sets the stroke width (w); a negative width is an error.
func (c *Content) LineWidth(w float64) {
	if w < 0 {
		c.fail(ErrNumber)
		return
	}
	c.num(w)
	c.op("w")
}

// Rect appends a rectangle path (re).
func (c *Content) Rect(x, y, w, h float64) {
	c.num(x)
	c.num(y)
	c.num(w)
	c.num(h)
	c.op("re")
}

// RoundedRect appends a closed rounded-rectangle path. radii are the circular
// radii of the corners (x,y), (x+w,y), (x+w,y+h), (x,y+h) in that order; each is
// clamped to [0, min(w,h)/2]. A rectangle with a non-positive side falls back
// to Rect.
func (c *Content) RoundedRect(x, y, w, h float64, radii [4]float64) {
	if !(w > 0 && h > 0) {
		c.Rect(x, y, w, h)
		return
	}
	limit := min(w, h) / 2
	for i, r := range radii {
		if !(r > 0) {
			r = 0
		}
		radii[i] = min(r, limit)
	}
	r0, r1, r2, r3 := radii[0], radii[1], radii[2], radii[3]
	c.move(x+r0, y)
	c.line(x+w-r1, y)
	c.curve(x+w-r1+kappa*r1, y, x+w, y+r1-kappa*r1, x+w, y+r1)
	c.line(x+w, y+h-r2)
	c.curve(x+w, y+h-r2+kappa*r2, x+w-r2+kappa*r2, y+h, x+w-r2, y+h)
	c.line(x+r3, y+h)
	c.curve(x+r3-kappa*r3, y+h, x, y+h-r3+kappa*r3, x, y+h-r3)
	c.line(x, y+r0)
	c.curve(x, y+r0-kappa*r0, x+r0-kappa*r0, y, x+r0, y)
	c.op("h")
}

func (c *Content) move(x, y float64) {
	c.num(x)
	c.num(y)
	c.op("m")
}

func (c *Content) line(x, y float64) {
	c.num(x)
	c.num(y)
	c.op("l")
}

func (c *Content) curve(x1, y1, x2, y2, x3, y3 float64) {
	c.num(x1)
	c.num(y1)
	c.num(x2)
	c.num(y2)
	c.num(x3)
	c.num(y3)
	c.op("c")
}

// Fill fills the current path (f).
func (c *Content) Fill() { c.op("f") }

// Clip intersects the clip with the current path and ends it (W n).
func (c *Content) Clip() { c.op("W n") }

// Stroke strokes the current path (S).
func (c *Content) Stroke() { c.op("S") }

// DrawImage paints the image in the rectangle with lower-left corner x, y and
// size wPt x hPt, in points. A zero id, or a non-finite or non-positive size,
// records ErrImage.
func (c *Content) DrawImage(id ImageID, x, y, wPt, hPt float64) {
	if c.err != nil {
		return
	}
	if id.e == nil || !(wPt > 0 && hPt > 0) || !validNum(wPt) || !validNum(hPt) || !validNum(x) || !validNum(y) {
		c.fail(ErrImage)
		return
	}
	if id.e.seen != c {
		id.e.seen = c
		c.images = append(c.images, id.e)
	}
	c.Save()
	c.Concat(wPt, 0, 0, hPt, x, y)
	if c.err != nil {
		return
	}
	prev := len(c.buf)
	c.buf = append(c.buf, '/')
	c.buf = append(c.buf, id.e.name()...)
	c.buf = append(c.buf, " Do\n"...)
	c.commit(prev)
	c.Restore()
}
