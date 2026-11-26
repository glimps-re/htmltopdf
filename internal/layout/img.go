package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// imgIntrinsic is the intrinsic size of the image of b, one image pixel per CSS
// px; ok is false for a box without a valid image, which then has no size and
// no ratio.
func imgIntrinsic(b *box.Box) (w, h float64, ok bool) {
	if b.Img == nil || b.Img.Src == nil || b.Img.Src.Width < 1 || b.Img.Src.Height < 1 {
		return 0, 0, false
	}
	return float64(b.Img.Src.Width), float64(b.Img.Src.Height), true
}

// imgHeightHint is the content height that CSS or the height attribute give to
// the image box b in cb, whose padding and border are those of g; ok is false
// when the height is auto. A percentage resolves only against a definite height.
func imgHeightHint(b *box.Box, g geom, cb cbox) (h float64, ok bool) {
	if h, ok := usedHeight(b.Style, g, cb); ok {
		return h, true
	}
	if b.Style.Height.Kind != style.DimAuto || b.Img == nil {
		return 0, false
	}
	return heightAttr(b.Img.Height, cb)
}

// imgAutoWidth is the content width of an image box whose CSS width is auto:
// its width attribute, else its height hint through the ratio, else its
// intrinsic width. The attributes are presentational hints that a CSS value
// other than auto overrides. min-height and max-height are not supported by
// the style package: the ratio is never kept against them.
func imgAutoWidth(b *box.Box, g geom, cb cbox) float64 {
	if b.Img != nil {
		if v, ok := attrPx(b.Img.Width, cb.w); ok {
			return v
		}
	}
	iw, ih, ok := imgIntrinsic(b)
	if !ok {
		return 0
	}
	if h, ok := imgHeightHint(b, g, cb); ok {
		return clampCoord(h * iw / ih)
	}
	return iw
}

// imgAutoHeight is the content height of an image box of content width w whose
// used height is auto: its height attribute, else w through the ratio. A CSS
// percentage of an auto height overrides the attribute and behaves as auto.
func imgAutoHeight(b *box.Box, w float64, cb cbox) float64 {
	if b.Img != nil && b.Style.Height.Kind == style.DimAuto {
		if v, ok := heightAttr(b.Img.Height, cb); ok {
			return v
		}
	}
	iw, ih, ok := imgIntrinsic(b)
	if !ok {
		return 0
	}
	return clampCoord(w * ih / iw)
}

// imgGeom gives an image box with an auto CSS width its used width: min-width
// and max-width apply and the auto margins share the rest.
func imgGeom(b *box.Box, g geom, cb cbox) geom {
	if b.Style.Width.Kind != style.DimAuto {
		return g
	}
	extra := g.pl + g.pr + g.bl + g.br
	return geometryW(b.Style, cb, capWidth(b.Style, imgAutoWidth(b, g, cb), cb.w, extra))
}

// imgSizes are the intrinsic contributions of the content of an image box: its
// width, or {0, ∞} for a percentage width attribute, which depends on the
// container.
func imgSizes(b *box.Box) sizes {
	if b.Img != nil && b.Img.Width.Set && b.Img.Width.Percent {
		return sizes{0, math.MaxFloat32}
	}
	cb := cbox{}
	w := imgAutoWidth(b, geometry(b.Style, cb), cb)
	return sizes{w, w}
}

// imgAtomWidth is the border box width of an image box with an auto CSS width
// laid out as an atomic inline in a containing block of width cbw.
func imgAtomWidth(b *box.Box, cbw float64) (float64, bool) {
	if b.Kind != box.KindImage || b.Style.Width.Kind != style.DimAuto {
		return 0, false
	}
	cb := cbox{w: cbw}
	extra := borderPadding(b.Style, cbw)
	return capWidth(b.Style, imgAutoWidth(b, geometry(b.Style, cb), cb), cbw, extra) + extra, true
}

// emitImage draws the image of f in its content box, clipped to the inner
// border radius when the box has one, as Chrome clips replaced content.
func emitImage(f *frag, out display.List) display.List {
	b := f.box
	if b.Img == nil || b.Img.Src == nil {
		return out
	}
	opacity := b.Style.UsedOpacity()
	content := contentBox(f)
	if !(content.W > 0 && content.H > 0) || !(opacity > 0) {
		return out
	}
	img := display.Image{Rect: content, Src: b.Img.Src, Opacity: opacity}
	rad := radiiOf(b.Style, f.rect, f.first, f.last)
	if rad.IsZero() {
		return append(out, img)
	}
	return append(out,
		display.PushClip{Rect: paddingBox(f), Radii: innerRadii(rad, f)},
		img,
		display.PopClip{})
}

// contentBox is the content box of the fragment f.
func contentBox(f *frag) display.Rect {
	r := f.rect
	return display.Rect{
		X: r.X + f.border[style.Left] + f.padding[style.Left],
		Y: r.Y + f.border[style.Top] + f.padding[style.Top],
		W: r.W - f.border[style.Left] - f.border[style.Right] - f.padding[style.Left] - f.padding[style.Right],
		H: r.H - f.border[style.Top] - f.border[style.Bottom] - f.padding[style.Top] - f.padding[style.Bottom],
	}
}
