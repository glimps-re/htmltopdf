// Package display defines the display list: what layout hands to paint, one
// list per page. Coordinates are CSS px in layout space (origin at the
// top-left of the page area, y down); paint applies the page scale and offset.
package display

import (
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// Rect is an axis-aligned rectangle.
type Rect struct{ X, Y, W, H float64 }

// Right returns the x of the right edge.
func (r Rect) Right() float64 { return r.X + r.W }

// Bottom returns the y of the bottom edge.
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Radii are the horizontal and vertical radii of the four corners (top-left,
// top-right, bottom-right, bottom-left), already clamped so that adjacent
// radii never exceed a side. The zero value is a square rectangle.
type Radii [4]struct{ H, V float64 }

// IsZero reports whether every radius is 0.
func (r Radii) IsZero() bool { return r == Radii{} }

// Item is one drawing operation. Translate returns the item moved by (dx, dy);
// being part of the sealed interface, it is implemented by every item.
type Item interface {
	isItem()
	Translate(dx, dy float64) Item
}

// Translate returns r moved by (dx, dy).
func (r Rect) Translate(dx, dy float64) Rect { r.X, r.Y = r.X+dx, r.Y+dy; return r }

// List is the display list of one page, in painting order.
type List []Item

// FillRect fills the rectangle (rounded when Radii is not zero) with Color.
type FillRect struct {
	Rect  Rect
	Radii Radii
	Color css.Color
}

// FillGradient fills the rectangle with a linear gradient.
type FillGradient struct {
	Rect     Rect
	Radii    Radii
	Gradient css.Gradient
}

// BorderSide is one side of a border: Width 0 means no border.
type BorderSide struct {
	Width float64
	Color css.Color
}

// Border draws the border of Rect, the border box: Sides are indexed top, right,
// bottom, left. A side that does not belong to this fragment (the top of a
// continuation, the bottom of a box that goes on) has Width 0.
type Border struct {
	Rect  Rect
	Radii Radii
	Sides [4]BorderSide
}

// Text draws shaped glyphs on a baseline. It is added to a List as a *Text. Glyph clusters are byte offsets in
// Text; Advance includes letter-spacing.
type Text struct {
	X, Baseline float64
	Face        *text.Face
	Size        float64
	Color       css.Color
	Glyphs      []text.Glyph
	Text        string
	// Underline and LineThrough are the decoration lines, drawn over the advance
	// of the run.
	Underline, LineThrough bool
}

// Width returns the advance of the run.
func (t *Text) Width() float64 {
	var w float64
	for i := range t.Glyphs {
		w += float64(t.Glyphs[i].Advance)
	}
	return w
}

// Link is a clickable rectangle.
type Link struct {
	Rect Rect
	URI  string
}

// PushClip clips the items up to the matching PopClip to Rect (rounded when
// Radii is not zero).
type PushClip struct {
	Rect  Rect
	Radii Radii
}

// PopClip ends the last PushClip.
type PopClip struct{}

// Translate implements Item.
func (v FillRect) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

// Translate implements Item.
func (v FillGradient) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

// Translate implements Item.
func (v Border) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

// Translate implements Item; the text is copied, its glyphs are shared.
func (t *Text) Translate(dx, dy float64) Item {
	c := *t
	c.X, c.Baseline = c.X+dx, c.Baseline+dy
	return &c
}

// Translate implements Item.
func (v Link) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

// Translate implements Item.
func (v PushClip) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

// Translate implements Item.
func (v PopClip) Translate(float64, float64) Item { return v }

func (FillRect) isItem()     {}
func (FillGradient) isItem() {}
func (Border) isItem()       {}
func (*Text) isItem()        {}
func (Link) isItem()         {}
func (PushClip) isItem()     {}
func (PopClip) isItem()      {}
