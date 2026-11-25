package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// blurSteps is the number of concentric rectangles a blurred shadow is drawn
// with, each at 1/blurSteps of its alpha.
const blurSteps = 4

// maxShadowItems bounds the shadow rectangles of a page (design §5.2.2): a
// page of boxes with 8 blurred shadows each would otherwise build tens of
// times the items of the content. The rest of the page gets no shadow.
const maxShadowItems = 4096

// emitter builds the display list of a page and counts its shadow items.
type emitter struct {
	shadows int
	clamped bool
	// ax and ay are the offset of the relative boxes that contain the fragment
	// being emitted: every item is moved by it once, when it is created.
	ax, ay float64
}

// emit returns the display list of f and its descendants, margin boxes
// included.
func emit(f *frag, out display.List) display.List {
	return (&emitter{}).emit(f, out)
}

// appendShadows adds the outer box-shadows of a box whose border box is r, to
// be drawn under its background, in paint order: the first shadow of the list
// is the topmost, so they are added last to first. A shadow without blur is
// one rectangle; a blur b is blurSteps rounded rectangles stepping from b/2
// outside the spread box to b/2 inside it (an approximation: no smooth
// falloff, and not clipped to the outside of the box, which an opaque
// background hides). Past maxShadowItems on the page nothing more is added.
//
// A fragment that continues on the next page (!last) or from the previous one
// (!first) has no shadow along that cut edge (box-decoration-break: slice): its
// shadow items are clipped to a band open to ±css.MaxLengthPx on the uncut
// sides and ending at the fragment edge on the cut ones.
func (e *emitter) appendShadows(out display.List, st *style.Computed, r display.Rect, rad display.Radii, first, last bool) display.List {
	if r.W <= 0 || r.H <= 0 || len(st.BoxShadow) == 0 {
		return out
	}
	if first && last {
		return e.appendOutsideShadows(out, st, r, rad)
	}
	top, bottom := -css.MaxLengthPx, css.MaxLengthPx
	if !first {
		top = r.Y
	}
	if !last {
		bottom = r.Bottom()
	}
	clip := display.Rect{X: -css.MaxLengthPx, Y: top, W: 2 * css.MaxLengthPx, H: bottom - top}
	out = append(out, display.PushClip{Rect: clip})
	pushed := len(out)
	if out = e.appendOutsideShadows(out, st, r, rad); len(out) == pushed {
		return out[:pushed-1]
	}
	return append(out, display.PopClip{})
}

// appendOutsideShadows adds the shadow rectangles of st clipped to the outside
// of the border box (CSS Backgrounds 3 §7.1: an outer shadow is never painted
// inside it, even when the background is transparent). An opaque background
// hides the inside anyway, so it needs no clip.
func (e *emitter) appendOutsideShadows(out display.List, st *style.Computed, r display.Rect, rad display.Radii) display.List {
	if st.BackgroundColor.A >= 1 {
		return e.appendShadowRects(out, st, r, rad)
	}
	pushed := len(out) + 1
	out = append(out, display.PushClip{Rect: r, Radii: rad, Outside: true})
	if out = e.appendShadowRects(out, st, r, rad); len(out) == pushed {
		return out[:pushed-1]
	}
	return append(out, display.PopClip{})
}

// appendShadowRects adds the shadow rectangles of st, unclipped.
func (e *emitter) appendShadowRects(out display.List, st *style.Computed, r display.Rect, rad display.Radii) display.List {
	for i := len(st.BoxShadow) - 1; i >= 0; i-- {
		s := &st.BoxShadow[i]
		if s.Color.A <= 0 {
			continue
		}
		base := grow(r, s.Spread)
		base.X, base.Y = clampCoord(base.X+s.X), clampCoord(base.Y+s.Y)
		if s.Blur <= 0 {
			if out = e.appendShadowRect(out, base, rad, s.Spread, s.Color); e.clamped {
				return out
			}
			continue
		}
		col := s.Color
		col.A /= blurSteps
		for k := range blurSteps {
			d := s.Blur/2 - float64(k)*s.Blur/(blurSteps-1)
			if out = e.appendShadowRect(out, grow(base, d), rad, s.Spread+d, col); e.clamped {
				return out
			}
		}
	}
	return out
}

// grow returns r moved out by d on every side (in by -d).
func grow(r display.Rect, d float64) display.Rect {
	return display.Rect{X: clampCoord(r.X - d), Y: clampCoord(r.Y - d), W: clampCoord(r.W + 2*d), H: clampCoord(r.H + 2*d)}
}

// appendShadowRect adds one rectangle whose corner radii are those of the box
// grown by grown (they stay square when the box has none, and never go below 0).
func (e *emitter) appendShadowRect(out display.List, r display.Rect, rad display.Radii, grown float64, c css.Color) display.List {
	if r.W <= 0 || r.H <= 0 {
		return out
	}
	if e.shadows >= maxShadowItems {
		e.clamped = true
		return out
	}
	e.shadows++
	var rr display.Radii
	for i := range rad {
		if rad[i].H > 0 && rad[i].V > 0 {
			rr[i].H = math.Max(0, rad[i].H+grown)
			rr[i].V = math.Max(0, rad[i].V+grown)
		}
	}
	return append(out, display.FillRect{Rect: r, Radii: fitRadii(rr, r), Color: c})
}

// fitRadii scales rad down so that adjacent radii never exceed a side of r, as
// display.Radii requires (CSS Backgrounds §5.5).
func fitRadii(rad display.Radii, r display.Rect) display.Radii {
	f := 1.0
	scale := func(side, sum float64) {
		if sum > side {
			f = math.Min(f, side/sum)
		}
	}
	scale(r.W, rad[style.TopLeft].H+rad[style.TopRight].H)
	scale(r.W, rad[style.BottomLeft].H+rad[style.BottomRight].H)
	scale(r.H, rad[style.TopLeft].V+rad[style.BottomLeft].V)
	scale(r.H, rad[style.TopRight].V+rad[style.BottomRight].V)
	if f < 1 {
		for i := range rad {
			rad[i].H *= f
			rad[i].V *= f
		}
	}
	return rad
}
