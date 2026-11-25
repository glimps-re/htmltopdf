// Package svg parses the subset of SVG that reduces to filled and stroked
// paths: path, rect, circle, ellipse, line, polyline, polygon and g.
package svg

import (
	"context"
	"math"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// MaxElements bounds the shapes of one document; the excess is dropped.
const MaxElements = 10000

// maxPaintBytes bounds an attribute parsed as a color.
const maxPaintBytes = 128

// Join and cap codes, those of the PDF j and J operators.
const (
	JoinMiter uint8 = iota
	JoinRound
	JoinBevel
)

// Cap codes.
const (
	CapButt uint8 = iota
	CapRound
	CapSquare
)

// Shape is one path with its paint, in viewBox units.
type Shape struct {
	Segs []Seg
	// Fill and Stroke have A == 0 for none. When FillCurrent (StrokeCurrent)
	// is set the paint is currentColor and the color is an opaque black
	// placeholder the caller replaces.
	Fill, Stroke               css.Color
	FillCurrent, StrokeCurrent bool
	FillEvenOdd                bool
	StrokeWidth                float64
	Join, Cap                  uint8
}

// Length is a width or height attribute: a number of px (unitless or px) or a
// percentage. A negative number is 0, as Chrome sizes it; anything else is
// unset.
type Length struct {
	Value   float64
	Percent bool
	Set     bool
}

// Image is a parsed svg element.
type Image struct {
	// ViewBox is min-x, min-y, width, height; a width or height ≤ 0 means no
	// viewBox.
	ViewBox [4]float64
	// Width and Height are the width and height attributes.
	Width, Height Length
	Shapes        []Shape
}

type paint struct {
	c       css.Color
	current bool
}

// state is the inherited presentation of an element.
type state struct {
	fill, stroke paint
	width        float64
	join, cap    uint8
	evenOdd      bool
}

type frame struct {
	next dom.NodeRef
	st   state
}

// MaxSegments bounds the path segments of a document, counted after the
// conversion of arcs: a shape that would exceed it, and every later one, is
// dropped.
const MaxSegments = 1 << 18

// maxColors bounds the cache of parsed paints; further colors are parsed each
// time.
const maxColors = 4096

// Parser parses the svg elements of one document, which share the MaxSegments
// bound and a cache of colors.
type Parser struct {
	d      *dom.Document
	b      *budget.Budget
	colors map[string]paintResult
	// pb parses colors with its own budget, whose ceilings cannot be reached
	// (a color attribute is at most maxPaintBytes): attributes must not eat the
	// CSS budget of the document. Each parse charges 1 Work to b.
	pb       *budget.Budget
	names    map[string]string
	segments int
	full     bool
}

// NewParser returns a Parser for the elements of d.
func NewParser(d *dom.Document, b *budget.Budget) *Parser {
	var lim budget.Limits
	lim[budget.CSSBytes] = int64(maxColorsParsed) * (maxPaintBytes + 2)
	lim[budget.Work] = 1 << 40
	// context.Background: the private budget cannot be cancelled, but each parse
	// charges 1 Work to b, which polls the real context.
	return &Parser{d: d, b: b, colors: map[string]paintResult{}, pb: budget.New(context.Background(), lim, nil)}
}

// maxColorsParsed is the number of color attributes a document can hold at
// most: two per shape of at most MaxElements shapes in each of 2^16 svg
// elements, far beyond any input that fits the node limit.
const maxColorsParsed = 1 << 26

type paintResult struct {
	p  paint
	ok bool
}

// Parse reads the svg element e and its descendants (the nesting is bounded by
// the HTML depth limit, and the walk is iterative). Elements other than the
// shapes and g are ignored with one warning per name, as are the transform and
// style attributes; the shapes of a path stop at the first error of its data.
// Presentation attributes (fill, stroke, stroke-width, stroke-linejoin,
// stroke-linecap, fill-rule) are inherited. It charges 1 Work per segment.
func Parse(d *dom.Document, e int32, b *budget.Budget) (*Image, error) {
	return NewParser(d, b).Parse(e)
}

// Parse reads the svg element e as the package function does, sharing the
// segment bound of the document with the earlier calls.
func (p *Parser) Parse(e int32) (*Image, error) {
	d, b := p.d, p.b
	root := &d.Elements[e]
	img := &Image{ViewBox: viewBox(root), Width: sizeAttr(root, "width"), Height: sizeAttr(root, "height")}
	st := p.apply(state{fill: paint{c: css.Color{A: 1}}, width: 1}, root)
	stack := []frame{{next: root.FirstChild, st: st}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		ref := top.next
		if ref == dom.NoNode {
			stack = stack[:len(stack)-1]
			continue
		}
		top.next = d.NextSibling(ref)
		i, isEl := ref.Element()
		if !isEl {
			continue
		}
		el := &d.Elements[i]
		if el.NS != dom.NSSVG {
			continue
		}
		name := elementName(el)
		if v, ok := el.Attr("display"); ok && strings.TrimSpace(v) == "none" {
			continue
		}
		switch name {
		case "g":
			stack = append(stack, frame{next: el.FirstChild, st: p.apply(top.st, el)})
		case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
			if p.full {
				return img, nil
			}
			if len(img.Shapes) >= MaxElements {
				b.Warn(budget.WarnClamped, "svg elements")
				return img, nil
			}
			sh, ok, err := p.shape(name, el, top.st)
			if err != nil {
				return nil, err
			}
			if ok {
				img.Shapes = append(img.Shapes, sh)
			}
		case "title", "desc", "metadata":
		default:
			b.Warn(budget.WarnUnsupportedElement, p.unsupported(name))
		}
	}
	return img, nil
}

// unsupported returns the warning detail of an element name, built once per
// name.
func (p *Parser) unsupported(name string) string {
	if p.names == nil {
		p.names = map[string]string{}
	}
	d, ok := p.names[name]
	if !ok {
		d = "svg " + name
		if len(p.names) < maxColors {
			p.names[name] = d
		}
	}
	return d
}

func elementName(e *dom.Element) string {
	if e.Tag != 0 {
		return e.Tag.String()
	}
	return e.Name
}

// sizeAttr reads a width or height attribute.
func sizeAttr(e *dom.Element, name string) Length {
	v, ok := e.Attr(name)
	if !ok {
		return Length{}
	}
	v = strings.TrimSpace(v)
	pct := strings.HasSuffix(v, "%")
	if pct {
		v = strings.TrimSuffix(v, "%")
	}
	n, ok := parseLength(v)
	if !ok {
		return Length{}
	}
	return Length{Value: math.Max(0, n), Percent: pct, Set: true}
}

func viewBox(e *dom.Element) [4]float64 {
	v, ok := e.Attr("viewbox")
	if !ok {
		if v, ok = e.Attr("viewBox"); !ok {
			return [4]float64{}
		}
	}
	sc := scanner{s: v}
	var vb [4]float64
	sc.skipSpace()
	for k := range vb {
		n, ok := sc.number()
		if !ok {
			return [4]float64{}
		}
		vb[k] = n
		sc.skipSep()
	}
	if vb[2] <= 0 || vb[3] <= 0 {
		return [4]float64{}
	}
	return vb
}

// apply returns st overridden by the presentation attributes of e.
func (p *Parser) apply(st state, e *dom.Element) state {
	if _, ok := e.Attr("transform"); ok {
		p.b.Warn(budget.WarnUnsupportedProperty, "svg transform")
	}
	if e.Style != "" {
		p.b.Warn(budget.WarnUnsupportedProperty, "svg style")
	}
	if v, ok := e.Attr("fill"); ok {
		if pt, ok := p.paint(v); ok {
			st.fill = pt
		}
	}
	if v, ok := e.Attr("stroke"); ok {
		if pt, ok := p.paint(v); ok {
			st.stroke = pt
		}
	}
	if v, ok := e.Attr("stroke-width"); ok {
		if w, ok := parseLength(v); ok && w >= 0 {
			st.width = w
		}
	}
	if v, ok := e.Attr("stroke-linejoin"); ok {
		switch strings.TrimSpace(v) {
		case "miter", "miter-clip", "arcs":
			st.join = JoinMiter
		case "round":
			st.join = JoinRound
		case "bevel":
			st.join = JoinBevel
		}
	}
	if v, ok := e.Attr("stroke-linecap"); ok {
		switch strings.TrimSpace(v) {
		case "butt":
			st.cap = CapButt
		case "round":
			st.cap = CapRound
		case "square":
			st.cap = CapSquare
		}
	}
	if v, ok := e.Attr("fill-rule"); ok {
		switch strings.TrimSpace(v) {
		case "evenodd":
			st.evenOdd = true
		case "nonzero":
			st.evenOdd = false
		}
	}
	return st
}

// paint parses a fill or stroke value; ok is false for an invalid one, which
// leaves the inherited paint. A url() reference is none: gradients and
// patterns are not supported.
func (p *Parser) paint(v string) (paint, bool) {
	v = strings.TrimSpace(v)
	if len(v) > maxPaintBytes {
		return paint{}, false
	}
	if r, ok := p.colors[v]; ok {
		return r.p, r.ok
	}
	r := p.parsePaint(v)
	if len(p.colors) < maxColors {
		p.colors[v] = r
	}
	return r.p, r.ok
}

func (p *Parser) parsePaint(v string) paintResult {
	switch l := strings.ToLower(v); {
	case l == "none", strings.HasPrefix(l, "url("):
		return paintResult{ok: true}
	case l == "currentcolor":
		return paintResult{p: paint{c: css.Color{A: 1}, current: true}, ok: true}
	}
	decls, err := css.ParseDeclarations([]byte("c:"+v), p.pb)
	if err != nil || len(decls) != 1 {
		return paintResult{}
	}
	c, ok := css.ParseColor(decls[0].Value)
	return paintResult{p: paint{c: c}, ok: ok}
}

// parseLength reads a number optionally followed by px.
func parseLength(v string) (float64, bool) {
	sc := scanner{s: strings.TrimSpace(v)}
	n, ok := sc.number()
	if !ok {
		return 0, false
	}
	if rest := sc.s[sc.i:]; rest != "" && rest != "px" {
		return 0, false
	}
	return n, true
}

func attrLength(e *dom.Element, name string) (float64, bool) {
	v, ok := e.Attr(name)
	if !ok {
		return 0, false
	}
	return parseLength(v)
}

// shape builds the shape of element e, charging its segments to Work. ok is
// false for an element that draws nothing.
func (p *Parser) shape(name string, e *dom.Element, inherited state) (Shape, bool, error) {
	st := p.apply(inherited, e)
	if st.fill.c.A <= 0 && st.stroke.c.A <= 0 {
		return Shape{}, false, nil
	}
	remaining := MaxSegments - p.segments
	var segs []Seg
	switch name {
	case "path":
		v, _ := e.Attr("d")
		if len(v) > MaxPathBytes {
			p.b.Warn(budget.WarnClamped, "svg path")
			break
		}
		segs = parsePath(v, remaining)
	case "rect":
		segs = rectSegs(e)
	case "circle":
		r, _ := attrLength(e, "r")
		cx, _ := attrLength(e, "cx")
		cy, _ := attrLength(e, "cy")
		segs = ellipseSegs(cx, cy, r, r)
	case "ellipse":
		rx, okX := attrLength(e, "rx")
		ry, okY := attrLength(e, "ry")
		okX, okY = okX && rx >= 0, okY && ry >= 0
		switch {
		case !okX && okY:
			rx = ry
		case okX && !okY:
			ry = rx
		}
		cx, _ := attrLength(e, "cx")
		cy, _ := attrLength(e, "cy")
		segs = ellipseSegs(cx, cy, rx, ry)
	case "line":
		x1, _ := attrLength(e, "x1")
		y1, _ := attrLength(e, "y1")
		x2, _ := attrLength(e, "x2")
		y2, _ := attrLength(e, "y2")
		segs = []Seg{{Op: MoveTo, P: [3]Point{{X: x1, Y: y1}}}, {Op: LineTo, P: [3]Point{{X: x2, Y: y2}}}}
	case "polyline", "polygon":
		v, _ := e.Attr("points")
		if len(v) > MaxPathBytes {
			p.b.Warn(budget.WarnClamped, "svg path")
			break
		}
		segs = pointsSegs(v, name == "polygon", remaining)
	}
	if len(segs) == 0 {
		return Shape{}, false, nil
	}
	if len(segs) > remaining {
		p.full = true
		p.b.Warn(budget.WarnClamped, "svg segments")
		return Shape{}, false, nil
	}
	if err := p.b.Charge(budget.Work, int64(len(segs))); err != nil {
		return Shape{}, false, err
	}
	p.segments += len(segs)
	return Shape{
		Segs: segs, Fill: st.fill.c, FillCurrent: st.fill.current, FillEvenOdd: st.evenOdd,
		Stroke: st.stroke.c, StrokeCurrent: st.stroke.current,
		StrokeWidth: math.Min(st.width, MaxCoord), Join: st.join, Cap: st.cap,
	}, true, nil
}

// pointsSegs reads a points list of at most max segments, one more being the
// signal of an overflow. A syntax error before the end draws nothing; an odd
// trailing number is ignored.
func pointsSegs(v string, closed bool, limit int) []Seg {
	sc := scanner{s: v}
	var segs []Seg
	sc.skipSpace()
	for sc.i < len(sc.s) {
		x, ok := sc.number()
		if !ok {
			return nil
		}
		sc.skipSep()
		if sc.i >= len(sc.s) {
			break
		}
		y, ok := sc.number()
		if !ok {
			return nil
		}
		sc.skipSep()
		op := LineTo
		if len(segs) == 0 {
			op = MoveTo
		}
		segs = append(segs, Seg{Op: op, P: [3]Point{{X: x, Y: y}}})
		if len(segs) > limit {
			return segs
		}
	}
	if len(segs) < 2 {
		return nil
	}
	if closed {
		segs = append(segs, Seg{Op: Close})
	}
	return segs
}

func rectSegs(e *dom.Element) []Seg {
	x, _ := attrLength(e, "x")
	y, _ := attrLength(e, "y")
	w, okW := attrLength(e, "width")
	h, okH := attrLength(e, "height")
	if !okW || !okH || w <= 0 || h <= 0 {
		return nil
	}
	rx, okX := attrLength(e, "rx")
	ry, okY := attrLength(e, "ry")
	okX, okY = okX && rx >= 0, okY && ry >= 0
	switch {
	case !okX && !okY:
		rx, ry = 0, 0
	case !okX:
		rx = ry
	case !okY:
		ry = rx
	}
	rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
	r, b := clamp(x+w), clamp(y+h)
	if rx == 0 || ry == 0 {
		return []Seg{
			{Op: MoveTo, P: [3]Point{{X: x, Y: y}}},
			{Op: LineTo, P: [3]Point{{X: r, Y: y}}},
			{Op: LineTo, P: [3]Point{{X: r, Y: b}}},
			{Op: LineTo, P: [3]Point{{X: x, Y: b}}},
			{Op: Close},
		}
	}
	l, t := clamp(x+rx), clamp(y+ry)
	rIn, bIn := clamp(r-rx), clamp(b-ry)
	ps := pathState{start: Point{X: l, Y: y}}
	ps.cur = ps.start
	ps.segs = []Seg{{Op: MoveTo, P: [3]Point{ps.start}}}
	ps.lineTo(Point{X: rIn, Y: y})
	ps.arcTo(rx, ry, 0, false, true, Point{X: r, Y: t})
	ps.lineTo(Point{X: r, Y: bIn})
	ps.arcTo(rx, ry, 0, false, true, Point{X: rIn, Y: b})
	ps.lineTo(Point{X: l, Y: b})
	ps.arcTo(rx, ry, 0, false, true, Point{X: x, Y: bIn})
	ps.lineTo(Point{X: x, Y: t})
	ps.arcTo(rx, ry, 0, false, true, ps.start)
	return append(ps.segs, Seg{Op: Close})
}

func ellipseSegs(cx, cy, rx, ry float64) []Seg {
	if rx <= 0 || ry <= 0 {
		return nil
	}
	start := Point{X: clamp(cx + rx), Y: cy}
	ps := pathState{cur: start, start: start}
	ps.segs = []Seg{{Op: MoveTo, P: [3]Point{start}}}
	ps.arcTo(rx, ry, 0, false, true, Point{X: cx, Y: clamp(cy + ry)})
	ps.arcTo(rx, ry, 0, false, true, Point{X: clamp(cx - rx), Y: cy})
	ps.arcTo(rx, ry, 0, false, true, Point{X: cx, Y: clamp(cy - ry)})
	ps.arcTo(rx, ry, 0, false, true, start)
	return append(ps.segs, Seg{Op: Close})
}
