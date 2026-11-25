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
	// MaxShadings is the number of shading objects a document may hold; a
	// translucent gradient needs two (color and alpha).
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

// Gradient is a gradient in unit space. A linear one runs from (0, 0), position
// 0, to (1, 0), position 1; a radial one is the circle of radius 1 centered on
// the origin, position 0 at the center. It says nothing of the box or the
// angle: the matrix of the drawing places it, so one shading serves every box.
// Stops whose alpha is below 1 make the gradient translucent: it is then drawn
// through a luminosity soft mask of the alphas.
type Gradient struct {
	Stops  []Stop
	Radial bool
}

// ShadingID identifies a gradient added with Writer.AddGradient. The zero value
// is invalid.
type ShadingID struct{ e *shadingEntry }

type shadingEntry struct {
	owner *Writer
	index int
	stops []Stop
	// radial and translucent tell the kind of shading; the alpha of a
	// translucent gradient is the A of its stops.
	radial, translucent bool
	// seen is the last content that drew the shading: it spares a scan of the
	// shadings of the content on every Shade.
	seen *Content
}

// shadingSet holds the gradients of a writer, deduplicated by value.
type shadingSet struct {
	entries []*shadingEntry
	byKey   map[string]*shadingEntry
	refs    []nameRef
	// objects counts the shading objects: 2 per translucent gradient.
	objects int
	// masks are the soft-mask graphics states of the translucent gradients.
	masks []nameRef
}

func (e *shadingEntry) name() string { return "Sh" + strconv.Itoa(e.index+1) }

func (e *shadingEntry) maskName() string { return "SM" + strconv.Itoa(e.index+1) }

// ticks is the resolution of positions and colors: appendNum writes four
// decimals, so values closer than 1/ticks would print equal.
const ticks = 10000

// qstop is a stop on the 1/ticks grid.
type qstop struct{ pos, r, g, b, a uint16 }

func quant(v float64) uint16 { return uint16(math.Round(math.Min(1, math.Max(0, v)) * ticks)) }

// quantize puts stops on the grid: positions clamped to [0, 1], never
// decreasing, at least one tick apart (the stitching function needs strictly
// increasing bounds) and, but for the first and last, strictly inside (0, 1).
// translucent tells whether any alpha is below 1; equal gradients give equal
// results.
func quantize(stops []Stop, out *[MaxGradientStops]qstop) (n int, translucent bool, err error) {
	n = len(stops)
	if n < 2 || n > MaxGradientStops {
		return 0, false, fmt.Errorf("%w: %d stops", ErrGradient, n)
	}
	for i, s := range stops {
		c := s.Color
		if math.IsNaN(s.Pos) || math.IsNaN(c.R) || math.IsNaN(c.G) || math.IsNaN(c.B) || math.IsNaN(c.A) {
			return 0, false, fmt.Errorf("%w: not a number", ErrGradient)
		}
		out[i] = qstop{pos: quant(s.Pos), r: quant(c.R), g: quant(c.G), b: quant(c.B), a: quant(c.A)}
		translucent = translucent || out[i].a < ticks
	}
	for i := 1; i < n; i++ {
		out[i].pos = max(out[i].pos, out[i-1].pos+1)
	}
	for i := n - 1; i >= 0; i-- {
		out[i].pos = min(out[i].pos, uint16(ticks-(n-1-i)))
	}
	return n, translucent, nil
}

func (q qstop) stop(translucent bool) Stop {
	a := 1.0
	if translucent {
		a = float64(q.a) / ticks
	}
	return Stop{Pos: float64(q.pos) / ticks, Color: Color{R: float64(q.r) / ticks, G: float64(q.g) / ticks, B: float64(q.b) / ticks, A: a}}
}

// AddGradient registers g and returns its identifier; equal gradients (stops
// on the 1/10000 grid, shape, and alpha when translucent) share their shading
// objects. At most MaxShadings shading objects are accepted, a translucent
// gradient taking two: ErrTooManyGradients.
func (w *Writer) AddGradient(g Gradient) (ShadingID, error) {
	if w.closed {
		return ShadingID{}, ErrClosed
	}
	var q [MaxGradientStops]qstop
	n, translucent, err := quantize(g.Stops, &q)
	if err != nil {
		return ShadingID{}, err
	}
	var kb [1 + MaxGradientStops*10]byte
	key := kb[:1]
	if g.Radial {
		key[0] |= 1
	}
	if translucent {
		key[0] |= 2
	}
	for _, s := range q[:n] {
		key = binary.BigEndian.AppendUint16(key, s.pos)
		key = binary.BigEndian.AppendUint16(key, s.r)
		key = binary.BigEndian.AppendUint16(key, s.g)
		key = binary.BigEndian.AppendUint16(key, s.b)
		if translucent {
			key = binary.BigEndian.AppendUint16(key, s.a)
		}
	}
	if e, ok := w.shadings.byKey[string(key)]; ok {
		return ShadingID{e}, nil
	}
	cost := 1
	if translucent {
		cost = 2
	}
	if w.shadings.objects+cost > MaxShadings {
		return ShadingID{}, ErrTooManyGradients
	}
	if w.shadings.byKey == nil {
		w.shadings.byKey = map[string]*shadingEntry{}
	}
	e := &shadingEntry{owner: w, index: len(w.shadings.entries), stops: make([]Stop, n), radial: g.Radial, translucent: translucent}
	for i, s := range q[:n] {
		e.stops[i] = s.stop(translucent)
	}
	w.shadings.objects += cost
	w.shadings.entries = append(w.shadings.entries, e)
	w.shadings.byKey[string(key)] = e
	return ShadingID{e}, nil
}

// Shade paints the current clip with the gradient, in the current user space
// of the gradient (see Gradient): set the matrix with Concat first, inside a
// Save, which also ends the soft mask a translucent gradient sets. It records
// ErrShadingID for an invalid id.
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
	if id.e.translucent {
		c.buf = append(c.buf, '/')
		c.buf = append(c.buf, id.e.maskName()...)
		c.buf = append(c.buf, " gs\n"...)
	}
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

// softMaskBBox is the bounding box, in the unit space of the gradient, of the
// form of a soft mask. The clip of a gradient lies within [0, 1] along a linear
// gradient line and within the unit circle of a radial one, and within a box
// side across the line: 10^4 covers any box a page can show. It must stay
// small: poppler renders nothing with a mask form of 10^8 units. A box far longer
// than its gradient line (hostile) can leave the form, which then shows no
// color there.
const softMaskBBox = "[-10000 -10000 10000 10000]"

// closeShadings writes the shading objects and, for translucent gradients, the
// soft-mask form and its graphics state.
func (w *Writer) closeShadings() error {
	var body []byte
	for _, e := range w.shadings.entries {
		ref := w.reserve()
		var err error
		if body, err = shadingDict(body[:0], e.stops, e.radial, false); err != nil {
			return err
		}
		if err := w.emit(ref, body); err != nil {
			return err
		}
		w.shadings.refs = append(w.shadings.refs, nameRef{name: e.name(), ref: ref})
		if !e.translucent {
			continue
		}
		if err := w.closeSoftMask(e); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) closeSoftMask(e *shadingEntry) error {
	alphaRef := w.reserve()
	body, err := shadingDict(nil, e.stops, e.radial, true)
	if err != nil {
		return err
	}
	if err := w.emit(alphaRef, body); err != nil {
		return err
	}
	formRef := w.reserve()
	extra := "/Type /XObject /Subtype /Form /BBox " + softMaskBBox +
		" /Group << /S /Transparency /CS /DeviceGray >> /Resources << /Shading << /A " + refString(alphaRef) + " >> >> "
	if err := w.emitStream(formRef, []byte("/A sh\n"), extra); err != nil {
		return err
	}
	gsRef := w.reserve()
	gs := "<< /Type /ExtGState /SMask << /Type /Mask /S /Luminosity /G " + refString(formRef) + " >> >>"
	if err := w.emit(gsRef, []byte(gs)); err != nil {
		return err
	}
	w.shadings.masks = append(w.shadings.masks, nameRef{name: e.maskName(), ref: gsRef})
	return nil
}

func refString(r Ref) string { return string(appendRef(nil, r)) }

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

// appendGray appends "[a]", the alpha of c as a gray level.
func appendGray(dst []byte, c Color) ([]byte, error) {
	dst = append(dst, '[')
	dst, err := appendNum(dst, c.A)
	return append(dst, ']'), err
}

func appendRamp(dst []byte, s colorSegment, alpha bool) ([]byte, error) {
	enc := appendRGB
	if alpha {
		enc = appendGray
	}
	dst = append(dst, "<< /FunctionType 2 /Domain [0 1] /N 1 /C0 "...)
	dst, err := enc(dst, s.c0)
	if err != nil {
		return dst, err
	}
	dst = append(dst, " /C1 "...)
	if dst, err = enc(dst, s.c1); err != nil {
		return dst, err
	}
	return append(dst, " >>"...), nil
}

// shadingDict writes the shading of the unit gradient (an axial one on the
// segment (0,0)-(1,0), or a radial one on the unit circle), colored by a
// stitching function through the stops, which quantize made strictly
// increasing. With alpha the shading is a gray level, the alpha of the stops.
func shadingDict(dst []byte, stops []Stop, radial, alpha bool) ([]byte, error) {
	dst = append(dst, "<< /ShadingType "...)
	if radial {
		dst = append(dst, "3"...)
	} else {
		dst = append(dst, "2"...)
	}
	if alpha {
		dst = append(dst, " /ColorSpace /DeviceGray"...)
	} else {
		dst = append(dst, " /ColorSpace /DeviceRGB"...)
	}
	if radial {
		dst = append(dst, " /Coords [0 0 0 0 0 1]"...)
	} else {
		dst = append(dst, " /Coords [0 0 1 0]"...)
	}
	dst = append(dst, " /Extend [true true] /Function "...)
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
		if dst, err = appendRamp(dst, segs[0], alpha); err != nil {
			return dst, err
		}
		return append(dst, " >>"...), nil
	}
	dst = append(dst, "<< /FunctionType 3 /Domain [0 1] /Functions ["...)
	for i, s := range segs {
		if i > 0 {
			dst = append(dst, ' ')
		}
		if dst, err = appendRamp(dst, s, alpha); err != nil {
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
