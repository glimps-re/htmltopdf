// Package paint turns the display list of a page into PDF content operators.
//
// It owns the conversion from layout space (CSS px, y down, relative to the
// page area, scaled) to PDF space (points, y up): coordinates are converted one
// by one instead of through a flipping matrix, because text drawn through a
// mirrored matrix would be mirrored. It also owns the conversion of css and
// text types to the types of package pdf, which imports neither.
//
// Paint is defensive about what upstream guarantees (design §5.2.1): it never
// nests more than MaxClipDepth clips, clamps every coordinate to ±MaxCoordPt,
// skips items that are entirely off the page, and sets the color immediately
// before each fill (the opacity of the graphics state is shared).
package paint

import (
	"errors"
	"fmt"
	"math"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// Limits and constants of the conversion.
const (
	// PxToPt converts CSS px to PDF points.
	PxToPt = 0.75
	// MaxClipDepth is the deepest nesting of clips drawn: deeper ones are dropped
	// and the content stays clipped by the outer ones, which keeps the graphics
	// state stack under pdf.MaxSaveDepth.
	MaxClipDepth = 200
	// MaxCoordPt bounds every coordinate written, in points.
	MaxCoordPt = 1e6
)

// ErrUnbalancedClip is returned when a display list pops a clip it did not push
// or ends with a clip open: a bug upstream.
var ErrUnbalancedClip = errors.New("paint: unbalanced clip")

// glyphChunk is the number of glyphs converted and written at a time: a run of
// millions of glyphs never needs more than one buffer of this size (32 B each).
const glyphChunk = 1 << 16

// Decoration lines, as fractions of the font size (display.Text carries no
// font metrics): the middle of the line above (+) or below (-) the baseline,
// and its thickness.
const (
	underlineCenter = -0.10
	throughCenter   = 0.30
	lineThickness   = 0.07
	// lineBoxHeight is the height of a text run's box, in font sizes.
	lineBoxHeight = 1.4
)

// pollEvery is the number of display items between two context checks.
const pollEvery = 1024

// Geometry places a display list on a page: the page size and the page area
// in CSS px, as layout.Page gives them, and the scale of the content.
type Geometry struct {
	Width, Height float64
	// Area is the page area; item coordinates are relative to its origin.
	Area  display.Rect
	Scale float64
}

// Painter paints the pages of one document. It registers the fonts it meets with
// the writer and is not safe for concurrent use.
type Painter struct {
	w      *pdf.Writer
	shaper *text.Shaper
	b      *budget.Budget
	fonts  map[*text.Face]pdf.FontID
	glyphs []pdf.Glyph
	// lastStops, lastLen and lastID memoize the shading of the last gradient.
	lastStops  *css.ColorStop
	lastLen    int
	lastRadial bool
	lastID     pdf.ShadingID
	// anchors are the destinations of the last page painted.
	anchors []Anchor
	// anchored are the names of anchors, one destination per name and page (an
	// inline box wrapped over lines has an anchor on each).
	anchored map[string]struct{}
}

// Anchor is a destination on a page: the internal target Name, Y points above the
// bottom of the page. It ignores transforms and the offset of relative boxes and
// is unscaled, as Chrome writes it.
type Anchor struct {
	Name string
	Y    float64
}

// Anchors returns the destinations of the page the last call of Page painted; the
// slice is reused by the next call.
func (p *Painter) Anchors() []Anchor { return p.anchors }

// NewPainter returns a Painter that adds fonts to w, reads their metrics through
// s and reports clamped values on b.
func NewPainter(w *pdf.Writer, s *text.Shaper, b *budget.Budget) *Painter {
	return &Painter{w: w, shaper: s, b: b, fonts: map[*text.Face]pdf.FontID{}}
}

// pdfColor converts a css color.
func pdfColor(c css.Color) pdf.Color { return pdf.Color{R: c.R, G: c.G, B: c.B, A: c.A} }

// Page appends the operators of l to c and returns the link annotations of the
// page, in points, to hand to the writer with the content. The error is the
// first error of c (pdf.ErrContentSize among them), a font that cannot be
// embedded, or ErrUnbalancedClip.
func (p *Painter) Page(c *pdf.Content, g Geometry, l display.List) ([]pdf.Link, error) {
	p.anchors = p.anchors[:0]
	clear(p.anchored)
	s := &pageState{p: p, c: c, g: g, ctm: identity}
	if !(g.Scale > 0) || math.IsInf(g.Scale, 0) {
		s.g.Scale = 1
	}
	for i, it := range l {
		if c.Err() != nil || s.err != nil {
			break
		}
		if i%pollEvery == 0 {
			if err := p.b.Poll(); err != nil {
				s.err = err
				break
			}
		}
		switch v := it.(type) {
		case display.FillRect:
			s.fillRect(v)
		case display.Border:
			s.border(v)
		case *display.Text:
			s.text(v)
		case display.Link:
			s.link(v)
		case display.Anchor:
			s.anchor(v)
		case display.PushClip:
			s.pushClip(v)
		case display.PopClip:
			s.popClip()
		case display.PushTransform:
			s.pushTransform(v)
		case display.PopTransform:
			s.popTransform()
		case display.Path:
			s.drawPath(v)
		case display.FillGradient:
			s.fillGradient(v)
		}
	}
	if s.err == nil && len(s.stack) > 0 {
		s.err = ErrUnbalancedClip
	}
	for _, g := range s.stack {
		if g.drawn {
			c.Restore()
		}
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.links, nil
}

type pageState struct {
	p     *Painter
	c     *pdf.Content
	g     Geometry
	links []pdf.Link
	// stack has one entry per open PushClip or PushTransform.
	stack []group
	// ctm is the product of the open transforms, in layout px; xforms counts
	// the drawn ones.
	ctm    [6]float64
	xforms int
	err    error
}

// group is an open clip or transform.
type group struct {
	// drawn is true when a q was written.
	drawn, transform bool
	prev             [6]float64
}

// positive reports whether w and h are both greater than 0 (false for NaN).
func positive(w, h float64) bool { return w > 0 && h > 0 }

func clampPt(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-MaxCoordPt, math.Min(MaxCoordPt, v))
}

// x and y convert layout px to page points (y up).
func (s *pageState) x(v float64) float64 {
	return clampPt((s.g.Area.X + s.g.Scale*v) * PxToPt)
}

func (s *pageState) y(v float64) float64 {
	return clampPt((s.g.Height - s.g.Area.Y - s.g.Scale*v) * PxToPt)
}

// length converts a layout length to points.
func (s *pageState) length(v float64) float64 { return clampPt(v * s.g.Scale * PxToPt) }

// ptRect is a rectangle in points: (x, y) is the bottom-left corner.
type ptRect struct{ x, y, w, h float64 }

func (r ptRect) right() float64 { return r.x + r.w }
func (r ptRect) top() float64   { return r.y + r.h }

// rect converts a layout rectangle; ok is false when it is entirely off the
// page.
func (s *pageState) rect(r display.Rect) (ptRect, bool) {
	x0, x1 := s.x(r.X), s.x(r.X+r.W)
	yTop, yBottom := s.y(r.Y), s.y(r.Y+r.H)
	if s.xforms > 0 {
		return ptRect{x: x0, y: yBottom, w: x1 - x0, h: yTop - yBottom}, true
	}
	return s.onPage(x0, x1, yTop, yBottom)
}

// onPage returns the rectangle with the given edges in points, ok false when it
// is entirely off the page.
func (s *pageState) onPage(x0, x1, yTop, yBottom float64) (ptRect, bool) {
	if x1 < 0 || x0 > s.g.Width*PxToPt || yTop < 0 || yBottom > s.g.Height*PxToPt {
		return ptRect{}, false
	}
	return ptRect{x: x0, y: yBottom, w: x1 - x0, h: yTop - yBottom}, true
}

// path adds the rectangle, with elliptical corners when radii is not zero.
func (s *pageState) path(r ptRect, radii display.Radii) {
	if radii.IsZero() {
		s.c.Rect(r.x, r.y, r.w, r.h)
		return
	}
	s.ellipticPath(r, s.cornersOf(r, radii))
}

func (s *pageState) fillRect(it display.FillRect) {
	if it.Color.A <= 0 || !positive(it.Rect.W, it.Rect.H) {
		return
	}
	r, ok := s.rect(it.Rect)
	if !ok {
		return
	}
	s.c.FillColor(pdfColor(it.Color))
	s.path(r, it.Radii)
	s.c.Fill()
}

func (s *pageState) pushClip(it display.PushClip) {
	if len(s.stack) >= MaxClipDepth {
		s.p.b.Warn(budget.WarnClamped, "clip nesting")
		s.stack = append(s.stack, group{})
		return
	}
	r, ok := s.rect(it.Rect)
	if !ok && it.Outside {
		s.stack = append(s.stack, group{})
		return
	}
	if !ok {
		// Nothing inside can be seen: an empty clip says so.
		r = ptRect{}
	}
	s.c.Save()
	if it.Outside && ok {
		s.c.Rect(-MaxCoordPt, -MaxCoordPt, 2*MaxCoordPt, 2*MaxCoordPt)
		s.path(r, it.Radii)
		s.c.ClipEvenOdd()
	} else {
		s.path(r, it.Radii)
		s.c.Clip()
	}
	s.stack = append(s.stack, group{drawn: true})
}

func (s *pageState) popClip() {
	if g, ok := s.pop(false); ok && g.drawn {
		s.c.Restore()
	}
}

// pop removes the last group, which must be a transform when transform is set.
func (s *pageState) pop(transform bool) (group, bool) {
	n := len(s.stack)
	if n == 0 || s.stack[n-1].transform != transform {
		s.err = ErrUnbalancedClip
		return group{}, false
	}
	g := s.stack[n-1]
	s.stack = s.stack[:n-1]
	return g, true
}

// anchor records the destination of an internal link, clamped to the page. Like
// Chrome, which writes the offset of the box in the page area as if it were
// measured from the top of the page, unscaled: y = page height - offset.
func (s *pageState) anchor(it display.Anchor) {
	if it.Name == "" || math.IsNaN(it.At.Y) {
		return
	}
	if _, dup := s.p.anchored[it.Name]; dup {
		return
	}
	if s.p.anchored == nil {
		s.p.anchored = make(map[string]struct{})
	}
	s.p.anchored[it.Name] = struct{}{}
	pageH := s.g.Height * PxToPt
	s.p.anchors = append(s.p.anchors, Anchor{Name: it.Name, Y: math.Max(0, math.Min(pageH, pageH-it.At.Y*PxToPt))})
}

func (s *pageState) link(it display.Link) {
	if it.URI == "" && it.Dest == "" {
		return
	}
	var r ptRect
	var ok bool
	if s.xforms > 0 {
		x0, x1, y0, y1 := s.mappedBounds(it.Rect)
		r, ok = s.onPage(s.x(x0), s.x(x1), s.y(y0), s.y(y1))
	} else {
		r, ok = s.rect(it.Rect)
	}
	if !ok {
		return
	}
	pageW, pageH := s.g.Width*PxToPt, s.g.Height*PxToPt
	x0, y0 := math.Max(0, r.x), math.Max(0, r.y)
	x1, y1 := math.Min(pageW, r.right()), math.Min(pageH, r.top())
	if !(x1 > x0 && y1 > y0) {
		return
	}
	s.links = append(s.links, pdf.Link{Rect: [4]float64{x0, y0, x1, y1}, URI: it.URI, Dest: it.Dest})
}

func (s *pageState) border(it display.Border) {
	var w [4]float64
	visible := false
	for i, side := range it.Sides {
		if side.Width > 0 && side.Color.A > 0 {
			w[i] = s.length(side.Width)
			visible = visible || w[i] > 0
		}
	}
	if !visible || !positive(it.Rect.W, it.Rect.H) {
		return
	}
	r, ok := s.rect(it.Rect)
	if !ok {
		return
	}
	if !it.Radii.IsZero() {
		var colors [4]pdf.Color
		same := true
		for i, side := range it.Sides {
			colors[i] = pdfColor(side.Color)
			same = same && side.Color == it.Sides[0].Color
		}
		for _, v := range w {
			same = same && v > 0
		}
		s.roundedBorder(r, w, colors, same, it.Radii)
		return
	}
	if uniform(it, w) {
		s.uniformBorder(r, w[0], it.Sides[0].Color)
		return
	}
	// Mitered sides, as polygons of the y-up rectangle.
	top, right, bottom, left := clampedWidths(w, r)
	x0, x1, y0, y1 := r.x, r.right(), r.y, r.top()
	sides := [4][4][2]float64{
		{{x0, y1}, {x1, y1}, {x1 - right, y1 - top}, {x0 + left, y1 - top}},
		{{x1, y1}, {x1, y0}, {x1 - right, y0 + bottom}, {x1 - right, y1 - top}},
		{{x1, y0}, {x0, y0}, {x0 + left, y0 + bottom}, {x1 - right, y0 + bottom}},
		{{x0, y0}, {x0, y1}, {x0 + left, y1 - top}, {x0 + left, y0 + bottom}},
	}
	for i := range sides {
		if w[i] <= 0 {
			continue
		}
		s.fillPolygon(pdfColor(it.Sides[i].Color), sides[i][:])
	}
}

// uniform reports whether the four sides have the same width and color.
func uniform(it display.Border, w [4]float64) bool {
	for i := 1; i < 4; i++ {
		if w[i] != w[0] || it.Sides[i].Color != it.Sides[0].Color || w[i] <= 0 {
			return false
		}
	}
	return w[0] > 0
}

// uniformBorder paints a border of one width and color as a single path: the
// border box and the padding box, filled with the even-odd rule.
func (s *pageState) uniformBorder(r ptRect, w float64, color css.Color) {
	s.c.FillColor(pdfColor(color))
	s.c.Rect(r.x, r.y, r.w, r.h)
	if r.w > 2*w && r.h > 2*w {
		s.c.Rect(r.x+w, r.y+w, r.w-2*w, r.h-2*w)
		s.c.FillEvenOdd()
		return
	}
	s.c.Fill()
}

// font returns the PDF font of a face, adding it to the document on first use.
func (p *Painter) font(f *text.Face) (pdf.FontID, error) {
	if id, ok := p.fonts[f]; ok {
		return id, nil
	}
	info, ok := p.shaper.Info(f)
	if !ok {
		return pdf.FontID{}, fmt.Errorf("paint: cannot read font %q", f.Family)
	}
	id, err := p.w.AddFont(&pdf.FontProgram{
		Data: f.Data(), Name: info.Name, UnitsPerEm: info.UnitsPerEm,
		Ascent: info.Ascent, Descent: info.Descent, CapHeight: info.CapHeight,
		BBox: info.BBox, Italic: info.Italic, Fixed: info.Fixed,
	})
	if err != nil {
		return pdf.FontID{}, err
	}
	p.fonts[f] = id
	return id, nil
}

func (s *pageState) text(it *display.Text) {
	if len(it.Glyphs) == 0 || it.Color.A <= 0 || !(it.Size > 0) || math.IsInf(it.Size, 0) || it.Face == nil {
		return
	}
	width := it.Width()
	// The run box: from the ascent above the baseline to the descent below.
	box := display.Rect{X: it.X, Y: it.Baseline - it.Size, W: width, H: lineBoxHeight * it.Size}
	if _, ok := s.rect(box); !ok {
		return
	}
	id, err := s.p.font(it.Face)
	if err != nil {
		s.err = err
		return
	}
	size := s.length(it.Size)
	s.c.FillColor(pdfColor(it.Color))
	x, y := s.x(it.X), s.y(it.Baseline)
	for i0 := 0; i0 < len(it.Glyphs); i0 += glyphChunk {
		end := min(i0+glyphChunk, len(it.Glyphs))
		if cap(s.p.glyphs) < end-i0 {
			s.p.glyphs = make([]pdf.Glyph, 0, end-i0)
		}
		run := s.p.glyphs[:0]
		adv := 0.0
		for i := i0; i < end; i++ {
			g := it.Glyphs[i]
			pg := pdf.Glyph{ID: g.ID, Advance: s.length(float64(g.Advance))}
			if i == 0 || g.Cluster != it.Glyphs[i-1].Cluster {
				pg.Text = clusterText(it.Text, it.Glyphs, i)
			}
			adv += pg.Advance
			run = append(run, pg)
		}
		s.c.Glyphs(id, size, clampPt(x), y, run)
		x += adv
		s.p.glyphs = run[:0]
	}
	if size <= 0 {
		return
	}
	line := func(center float64) {
		thick := lineThickness * size
		s.c.Rect(s.x(it.X), s.y(it.Baseline)+center*size-thick/2, s.length(width), thick)
		s.c.Fill()
	}
	if it.Underline {
		line(underlineCenter)
	}
	if it.LineThrough {
		line(throughCenter)
	}
}

// clusterText returns the text of the cluster that glyph i starts: from its
// byte offset to the next cluster's, or to the end of the text.
func clusterText(txt string, glyphs []text.Glyph, i int) string {
	start := int(glyphs[i].Cluster)
	if start < 0 || start > len(txt) {
		return ""
	}
	end := len(txt)
	for _, g := range glyphs[i+1:] {
		if g.Cluster != glyphs[i].Cluster {
			end = min(max(int(g.Cluster), start), len(txt))
			break
		}
	}
	return txt[start:end]
}
