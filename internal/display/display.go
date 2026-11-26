// Package display defines the display list: what layout hands to paint, one
// list per page. Coordinates are CSS px in layout space (origin at the
// top-left of the page area, y down); paint applies the page scale and offset.
package display

import (
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/raster"
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

// LineStyle is how a border side is drawn.
type LineStyle uint8

// Line styles: the zero value is a solid line.
const (
	LineSolid LineStyle = iota
	LineDashed
	LineDotted
)

// BorderSide is one side of a border.
//
// A cell of a collapsed table draws half of the border it shares, which is
// right for a solid line; a dashed or dotted one is drawn whole, as a single
// stroke along the edge: PatternWidth is then its full width and the stroke is
// centered on the edge of Rect instead of inside it.
type BorderSide struct {
	// Width is the width drawn inside the border box; 0 means no border.
	Width float64
	Color css.Color
	Style LineStyle
	// PatternWidth, when positive, is the full width of a collapsed edge shared
	// with the next cell.
	PatternWidth float64
	// Lead and Trail move the start and the end of the stroke along the side
	// (px, in the direction of the stroke: left to right, top to bottom).
	Lead, Trail float64
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
	// Thickness is the thickness in px of the decoration lines; 0 is the one of
	// the font.
	Thickness float64
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
	// Dest is the id of the target of an internal link, set instead of URI.
	Dest string
}

// Anchor is the destination of the internal links to Name: the top of the first
// fragment of the box with that id. Only At.Y is used, the destination being the
// left edge of the page (Chrome).
type Anchor struct {
	Name string
	At   Point
}

// PushClip clips the items up to the matching PopClip to Rect (rounded when
// Radii is not zero), or with Outside to everything but Rect.
type PushClip struct {
	Rect    Rect
	Radii   Radii
	Outside bool
}

// PopClip ends the last PushClip.
type PopClip struct{}

// Point is a point in layout px.
type Point struct{ X, Y float64 }

// PushTransform applies the affine matrix M = [a b c d e f] (x' = a·x + c·y + e,
// y' = b·x + d·y + f, in layout px, y down) to the items up to the matching
// PopTransform.
type PushTransform struct{ M [6]float64 }

// PopTransform ends the last PushTransform.
type PopTransform struct{}

// Path operations of a PathSeg.
const (
	PathMoveTo uint8 = iota
	PathLineTo
	PathCubicTo
	PathClose
)

// PathSeg is one segment of a Path: MoveTo and LineTo use P[0], CubicTo
// P[0..2] (control points, then end point), Close none.
type PathSeg struct {
	Op uint8
	P  [3]Point
}

// Path fills and strokes a path given in layout px. A Fill or Stroke with A == 0
// is not drawn; EvenOdd selects the fill rule; Width, Join and Cap are the
// stroke width in px and the PDF line join (0 miter, 1 round, 2 bevel) and cap
// (0 butt, 1 round, 2 square) codes.
type Path struct {
	Segs         []PathSeg
	Fill, Stroke css.Color
	EvenOdd      bool
	Width        float64
	Join, Cap    uint8
}

// Image draws Src scaled to Rect, the content box of its box, with the opacity
// Opacity in [0, 1]. A nil Src draws nothing.
type Image struct {
	Rect    Rect
	Src     *raster.Source
	Opacity float64
}

// Translate implements Item.
func (v Image) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

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
func (v Anchor) Translate(dx, dy float64) Item { v.At.X, v.At.Y = v.At.X+dx, v.At.Y+dy; return v }

// Translate implements Item.
func (v PushClip) Translate(dx, dy float64) Item { v.Rect = v.Rect.Translate(dx, dy); return v }

// Translate implements Item.
func (v PopClip) Translate(float64, float64) Item { return v }

// Translate implements Item: the matrix becomes the same transformation in the
// moved frame.
func (v PushTransform) Translate(dx, dy float64) Item {
	m := v.M
	m[4] += dx - m[0]*dx - m[2]*dy
	m[5] += dy - m[1]*dx - m[3]*dy
	return PushTransform{M: m}
}

// Translate implements Item.
func (v PopTransform) Translate(float64, float64) Item { return v }

// Translate implements Item; the segments are copied.
func (v Path) Translate(dx, dy float64) Item {
	segs := make([]PathSeg, len(v.Segs))
	for i, sg := range v.Segs {
		for k := range sg.P {
			sg.P[k].X += dx
			sg.P[k].Y += dy
		}
		segs[i] = sg
	}
	v.Segs = segs
	return v
}

func (FillRect) isItem()      {}
func (FillGradient) isItem()  {}
func (Border) isItem()        {}
func (*Text) isItem()         {}
func (Link) isItem()          {}
func (Anchor) isItem()        {}
func (PushClip) isItem()      {}
func (PopClip) isItem()       {}
func (PushTransform) isItem() {}
func (PopTransform) isItem()  {}
func (Path) isItem()          {}
func (Image) isItem()         {}
