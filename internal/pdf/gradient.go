package pdf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Bounds of the gradients of a document.
const (
	// MaxGradientStops is the largest number of color stops of a gradient.
	MaxGradientStops = 64
	// MaxShadings is the number of distinct gradients a document may hold.
	MaxShadings = 4096
)

// Errors of the gradient API.
var (
	// ErrGradient is returned for stops that make no gradient.
	ErrGradient = errors.New("pdf: invalid gradient")
	// ErrShadingID is returned by Content.Shade for the zero ShadingID.
	ErrShadingID = errors.New("pdf: invalid shading")
	// ErrForeignShading is returned when a content draws a shading of another writer.
	ErrForeignShading = errors.New("pdf: shading of another writer")
	// ErrTooManyGradients is returned past MaxShadings distinct gradients.
	ErrTooManyGradients = errors.New("pdf: too many distinct gradients")
)

// Stop is a color at a position along the gradient line.
type Stop struct {
	Pos   float64
	Color Color
}

// Gradient is a linear gradient in unit space: position 0 is the start of the
// gradient line and 1 its end. It says nothing of the box or the angle: the
// matrix of the drawing places it, so one shading serves every box. The alpha
// of the stops is ignored (alpha gradients need a soft mask, M3).
type Gradient struct {
	Stops []Stop
}

// ShadingID identifies a gradient added with Writer.AddGradient. The zero value
// is invalid.
type ShadingID struct{ e *shadingEntry }

type shadingEntry struct {
	owner *Writer
	index int
	stops []Stop
	// seen is the last content that drew the shading: it spares a scan of the
	// shadings of the content on every Shade.
	seen *Content
}

// shadingSet holds the gradients of a writer, deduplicated by value.
type shadingSet struct {
	entries []*shadingEntry
	byKey   map[string]*shadingEntry
	refs    []nameRef
}

func (e *shadingEntry) name() string { return "Sh" + strconv.Itoa(e.index+1) }

// ticks is the resolution of positions and colors: appendNum writes four
// decimals, so values closer than 1/ticks would print equal.
const ticks = 10000

// qstop is a stop on the 1/ticks grid.
type qstop struct{ pos, r, g, b uint16 }

func quant(v float64) uint16 { return uint16(math.Round(math.Min(1, math.Max(0, v)) * ticks)) }

// quantize puts stops on the grid: positions clamped to [0, 1], never
// decreasing, at least one tick apart (the stitching function needs strictly
// increasing bounds) and, but for the first and last, strictly inside (0, 1);
// colors opaque. Equal gradients give equal results.
func quantize(stops []Stop, out *[MaxGradientStops]qstop) (int, error) {
	n := len(stops)
	if n < 2 || n > MaxGradientStops {
		return 0, fmt.Errorf("%w: %d stops", ErrGradient, n)
	}
	for i, s := range stops {
		c := s.Color
		if math.IsNaN(s.Pos) || math.IsNaN(c.R) || math.IsNaN(c.G) || math.IsNaN(c.B) {
			return 0, fmt.Errorf("%w: not a number", ErrGradient)
		}
		out[i] = qstop{pos: quant(s.Pos), r: quant(c.R), g: quant(c.G), b: quant(c.B)}
	}
	for i := 1; i < n; i++ {
		out[i].pos = max(out[i].pos, out[i-1].pos+1)
	}
	for i := n - 1; i >= 0; i-- {
		out[i].pos = min(out[i].pos, uint16(ticks-(n-1-i)))
	}
	return n, nil
}

func (q qstop) stop() Stop {
	return Stop{Pos: float64(q.pos) / ticks, Color: Color{R: float64(q.r) / ticks, G: float64(q.g) / ticks, B: float64(q.b) / ticks, A: 1}}
}

// AddGradient registers g and returns its identifier; gradients with the same
// stops, whatever their alpha, share one shading object. At most MaxShadings
// distinct gradients are accepted: ErrTooManyGradients.
func (w *Writer) AddGradient(g Gradient) (ShadingID, error) {
	if w.closed {
		return ShadingID{}, ErrClosed
	}
	var q [MaxGradientStops]qstop
	n, err := quantize(g.Stops, &q)
	if err != nil {
		return ShadingID{}, err
	}
	var kb [MaxGradientStops * 8]byte
	key := kb[:0]
	for _, s := range q[:n] {
		key = binary.BigEndian.AppendUint16(key, s.pos)
		key = binary.BigEndian.AppendUint16(key, s.r)
		key = binary.BigEndian.AppendUint16(key, s.g)
		key = binary.BigEndian.AppendUint16(key, s.b)
	}
	if e, ok := w.shadings.byKey[string(key)]; ok {
		return ShadingID{e}, nil
	}
	if len(w.shadings.entries) >= MaxShadings {
		return ShadingID{}, ErrTooManyGradients
	}
	if w.shadings.byKey == nil {
		w.shadings.byKey = map[string]*shadingEntry{}
	}
	e := &shadingEntry{owner: w, index: len(w.shadings.entries), stops: make([]Stop, n)}
	for i, s := range q[:n] {
		e.stops[i] = s.stop()
	}
	w.shadings.entries = append(w.shadings.entries, e)
	w.shadings.byKey[string(key)] = e
	return ShadingID{e}, nil
}

// Shade paints the current clip with the gradient, in the current user space
// where the gradient line runs from (0, 0) to (1, 0): set the matrix with
// Concat first, inside a Save. It records ErrShadingID for an invalid id.
func (c *Content) Shade(id ShadingID) {
	if c.err != nil {
		return
	}
	if id.e == nil {
		c.fail(ErrShadingID)
		return
	}
	if id.e.seen != c {
		id.e.seen = c
		c.shadings = append(c.shadings, id.e)
	}
	prev := len(c.buf)
	c.buf = append(c.buf, '/')
	c.buf = append(c.buf, id.e.name()...)
	c.buf = append(c.buf, " sh\n"...)
	c.commit(prev)
}

// checkShadings refuses a content that draws a gradient of another writer.
func (w *Writer) checkShadings(c *Content) error {
	for _, e := range c.shadings {
		if e.owner != w {
			return ErrForeignShading
		}
	}
	return nil
}

// closeShadings writes the shading objects.
func (w *Writer) closeShadings() error {
	var body []byte
	for _, e := range w.shadings.entries {
		ref := w.reserve()
		var err error
		if body, err = shadingDict(body[:0], e.stops); err != nil {
			return err
		}
		if err := w.emit(ref, body); err != nil {
			return err
		}
		w.shadings.refs = append(w.shadings.refs, nameRef{name: e.name(), ref: ref})
	}
	return nil
}

// appendRGB appends "[r g b]".
func appendRGB(dst []byte, c Color) ([]byte, error) {
	dst = append(dst, '[')
	var err error
	if dst, err = appendNum(dst, c.R); err != nil {
		return dst, err
	}
	dst = append(dst, ' ')
	if dst, err = appendNum(dst, c.G); err != nil {
		return dst, err
	}
	dst = append(dst, ' ')
	if dst, err = appendNum(dst, c.B); err != nil {
		return dst, err
	}
	return append(dst, ']'), nil
}

// colorSegment is a function of the stitching: a color ramp over [from, to].
type colorSegment struct {
	from, to float64
	c0, c1   Color
}

func appendRamp(dst []byte, s colorSegment) ([]byte, error) {
	dst = append(dst, "<< /FunctionType 2 /Domain [0 1] /N 1 /C0 "...)
	dst, err := appendRGB(dst, s.c0)
	if err != nil {
		return dst, err
	}
	dst = append(dst, " /C1 "...)
	if dst, err = appendRGB(dst, s.c1); err != nil {
		return dst, err
	}
	return append(dst, " >>"...), nil
}

// shadingDict writes an axial shading of the unit segment (0,0)-(1,0), colored
// by a stitching function through the stops, which quantize made strictly
// increasing.
func shadingDict(dst []byte, stops []Stop) ([]byte, error) {
	dst = append(dst, "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] /Extend [true true] /Function "...)
	// A constant ramp before the first stop and after the last, when they are
	// not at 0 and 1.
	segs := make([]colorSegment, 0, len(stops)+1)
	if first := stops[0]; first.Pos > 0 {
		segs = append(segs, colorSegment{0, first.Pos, first.Color, first.Color})
	}
	for i := 1; i < len(stops); i++ {
		segs = append(segs, colorSegment{stops[i-1].Pos, stops[i].Pos, stops[i-1].Color, stops[i].Color})
	}
	if last := stops[len(stops)-1]; last.Pos < 1 {
		segs = append(segs, colorSegment{last.Pos, 1, last.Color, last.Color})
	}
	var err error
	if len(segs) == 1 {
		if dst, err = appendRamp(dst, segs[0]); err != nil {
			return dst, err
		}
		return append(dst, " >>"...), nil
	}
	dst = append(dst, "<< /FunctionType 3 /Domain [0 1] /Functions ["...)
	for i, s := range segs {
		if i > 0 {
			dst = append(dst, ' ')
		}
		if dst, err = appendRamp(dst, s); err != nil {
			return dst, err
		}
	}
	dst = append(dst, "] /Bounds ["...)
	for i := 1; i < len(segs); i++ {
		if i > 1 {
			dst = append(dst, ' ')
		}
		if dst, err = appendNum(dst, segs[i].from); err != nil {
			return dst, err
		}
	}
	dst = append(dst, "] /Encode ["...)
	for i := range segs {
		if i > 0 {
			dst = append(dst, ' ')
		}
		dst = append(dst, "0 1"...)
	}
	return append(dst, "] >> >>"...), nil
}
