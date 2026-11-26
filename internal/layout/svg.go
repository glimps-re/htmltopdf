package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/svg"
)

// Default size of a replaced SVG without width, height or viewBox ratio (CSS 2.1
// §10.3.2, as HTML specifies it).
const (
	svgDefaultWidth  = 300
	svgDefaultHeight = 150
)

// minViewBox is the smallest viewBox side taken into account: a smaller one is
// ignored as degenerate, as Chrome does.
const minViewBox = 1e-6

// svgRatio is the height over the width of the viewBox of an svg box.
func svgRatio(b *box.Box) (r float64, ok bool) {
	vb := b.SVG.ViewBox
	if vb[2] >= minViewBox && vb[3] >= minViewBox {
		return vb[3] / vb[2], true
	}
	return 0, false
}

// attrPx resolves a width or height attribute against base: ok is false when it
// is unset.
func attrPx(a svg.Length, base float64) (v float64, ok bool) {
	if !a.Set {
		return 0, false
	}
	if a.Percent {
		return clampCoord(a.Value / 100 * base), true
	}
	return clampCoord(a.Value), true
}

// heightAttr resolves a height attribute in cb: ok is false when it is unset,
// or a percentage of an indefinite height.
func heightAttr(a svg.Length, cb cbox) (v float64, ok bool) {
	if a.Percent && !cb.hdef {
		return 0, false
	}
	return attrPx(a, cb.h)
}

// svgSized returns the width and height that CSS (px) or the attributes (px) give
// to the box: -1 for one that is not given.
func svgSized(b *box.Box) (w, h float64) {
	st := b.Style
	w, h = -1, -1
	if st.Width.Kind == style.DimPx {
		w = st.Width.Value
	} else if v, ok := attrPx(b.SVG.Width, 0); ok && !b.SVG.Width.Percent {
		w = v
	}
	if st.Height.Kind == style.DimPx {
		h = st.Height.Value
	} else if v, ok := attrPx(b.SVG.Height, 0); ok && !b.SVG.Height.Percent {
		h = v
	}
	return w, h
}

// svgFills reports whether the svg box takes the width of its container: a
// viewBox ratio and no size, as Chrome does.
func svgFills(b *box.Box) bool {
	w, h := svgSized(b)
	_, ratio := svgRatio(b)
	return w < 0 && h < 0 && ratio && b.Style.Width.Kind == style.DimAuto &&
		!b.SVG.Width.Set && !b.SVG.Height.Set && b.Style.Height.Kind == style.DimAuto
}

// svgAutoWidth is the content width of an svg box whose CSS width is auto: its
// width attribute, else the height through the viewBox ratio, else 300. A
// percentage attribute resolves against cbw.
func svgAutoWidth(b *box.Box, cbw float64) float64 {
	if v, ok := attrPx(b.SVG.Width, cbw); ok {
		return v
	}
	_, h := svgSized(b)
	if r, ok := svgRatio(b); ok && h > 0 {
		return clampCoord(h / r)
	}
	return svgDefaultWidth
}

// svgAutoHeight is the content height of an svg box of content width w whose CSS
// height is auto: its height attribute, else through the viewBox ratio, else 150.
func svgAutoHeight(b *box.Box, w float64, cb cbox) float64 {
	if v, ok := heightAttr(b.SVG.Height, cb); ok {
		return v
	}
	if r, ok := svgRatio(b); ok {
		return clampCoord(w * r)
	}
	return svgDefaultHeight
}

// svgGeom gives an svg box with an auto CSS width its intrinsic one, as a replaced
// element: min-width and max-width apply and the auto margins share the rest. A
// box that fills its container keeps the geometry of a block.
func svgGeom(b *box.Box, g geom, cb cbox) geom {
	if b.Style.Width.Kind != style.DimAuto || svgFills(b) {
		return g
	}
	extra := g.pl + g.pr + g.bl + g.br
	return geometryW(b.Style, cb, capWidth(b.Style, svgAutoWidth(b, cb.w), cb.w, extra))
}

// emitSVG draws the shapes of the svg box of f, mapped from the viewBox to the
// content box (preserveAspectRatio xMidYMid meet) and clipped to it. A
// currentColor paint takes the color of the box.
func emitSVG(f *frag, out display.List) display.List {
	b := f.box
	if b.SVG == nil || len(b.SVG.Shapes) == 0 {
		return out
	}
	content := contentBox(f)
	if !(content.W > 0 && content.H > 0) {
		return out
	}
	scale, tx, ty := 1.0, content.X, content.Y
	if vb := b.SVG.ViewBox; vb[2] >= minViewBox && vb[3] >= minViewBox {
		scale = math.Min(content.W/vb[2], content.H/vb[3])
		tx = content.X + (content.W-scale*vb[2])/2 - scale*vb[0]
		ty = content.Y + (content.H-scale*vb[3])/2 - scale*vb[1]
	}
	cur := b.Style.Color
	// currentColor already carries the opacity; explicit paints get it here.
	opacity := b.Style.UsedOpacity()
	out = append(out, display.PushClip{Rect: content})
	for i := range b.SVG.Shapes {
		sh := &b.SVG.Shapes[i]
		fill, stroke := sh.Fill, sh.Stroke
		if sh.FillCurrent {
			fill = cur
		} else {
			fill.A *= opacity
		}
		if sh.StrokeCurrent {
			stroke = cur
		} else {
			stroke.A *= opacity
		}
		p := display.Path{
			Segs: make([]display.PathSeg, len(sh.Segs)),
			Fill: fill, Stroke: stroke, EvenOdd: sh.FillEvenOdd,
			Width: clampCoord(sh.StrokeWidth * scale), Join: sh.Join, Cap: sh.Cap,
		}
		for j, sg := range sh.Segs {
			ps := display.PathSeg{Op: uint8(sg.Op)}
			for k := range sg.P {
				ps.P[k] = display.Point{X: clampCoord(tx + scale*sg.P[k].X), Y: clampCoord(ty + scale*sg.P[k].Y)}
			}
			p.Segs[j] = ps
		}
		out = append(out, p)
	}
	return append(out, display.PopClip{})
}

// emitReplaced draws the content of a replaced box: its drawing or its image.
func emitReplaced(f *frag, out display.List) display.List {
	switch f.box.Kind {
	case box.KindSVG:
		return emitSVG(f, out)
	case box.KindImage:
		return emitImage(f, out)
	}
	return out
}

// replacedGeom is the geometry of the replaced box b: svgGeom or imgGeom.
func replacedGeom(b *box.Box, g geom, cb cbox) geom {
	switch b.Kind {
	case box.KindSVG:
		return svgGeom(b, g, cb)
	case box.KindImage:
		return imgGeom(b, g, cb)
	}
	return g
}

// replacedAutoHeight is the content height of the replaced box b of content
// width w whose CSS height is auto.
func replacedAutoHeight(b *box.Box, w float64, cb cbox) float64 {
	switch b.Kind {
	case box.KindSVG:
		return svgAutoHeight(b, w, cb)
	case box.KindImage:
		return imgAutoHeight(b, w, cb)
	}
	return 0
}

// replacedSizes are the intrinsic contributions of the content of a replaced box.
func replacedSizes(b *box.Box) sizes {
	switch b.Kind {
	case box.KindSVG:
		if b.SVG.Width.Set && b.SVG.Width.Percent || svgFills(b) {
			return sizes{0, math.MaxFloat32}
		}
		w := svgAutoWidth(b, 0)
		return sizes{w, w}
	case box.KindImage:
		return imgSizes(b)
	}
	return sizes{}
}

// blockLevel reports whether c is a block-level child: a block, or a replaced
// box whose display is not inline-level.
func blockLevel(c *box.Box) bool {
	if c.Replaced() {
		return c.Style.Display != style.DisplayInline && c.Style.Display != style.DisplayInlineBlock
	}
	return c.Kind == box.KindBlock
}

// replacedAtomWidth is the border box width of an atomic replaced box whose
// width does not come from its contributions: an image with an auto CSS width,
// or an svg whose width is a percentage attribute.
func replacedAtomWidth(b *box.Box, cbw float64) (float64, bool) {
	if w, ok := imgAtomWidth(b, cbw); ok {
		return w, true
	}
	return svgPercentWidth(b, cbw)
}

// svgPercentWidth is the border box width of an atomic svg whose width is a
// percentage attribute, resolved against its container.
func svgPercentWidth(b *box.Box, cbw float64) (float64, bool) {
	if b.Kind != box.KindSVG || b.Style.Width.Kind != style.DimAuto || !b.SVG.Width.Set || !b.SVG.Width.Percent {
		return 0, false
	}
	extra := borderPadding(b.Style, cbw)
	return capWidth(b.Style, svgAutoWidth(b, cbw), cbw, extra) + extra, true
}
