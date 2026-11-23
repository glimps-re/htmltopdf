package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// geom holds the used box model values of a block, in layout px.
type geom struct {
	mt, mb, ml, mr float64
	bt, br, bb, bl float64
	pt, pr, pb, pl float64
	w              float64 // content width
	borderW        float64 // border-box width
}

func (g *geom) pre() float64  { return g.bt + g.pt }
func (g *geom) post() float64 { return g.pb + g.bb }

// px resolves a computed dimension against the containing block width.
func px(d style.Dim, base float64) float64 {
	switch d.Kind {
	case style.DimPx:
		return d.Value
	case style.DimPercent:
		return d.Value / 100 * base
	}
	return 0
}

func clampCoord(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-css.MaxLengthPx, math.Min(css.MaxLengthPx, v))
}

// padPx resolves a padding: never negative.
func padPx(d style.Dim, base float64) float64 {
	return clampCoord(math.Max(0, px(d, base)))
}

// geometry resolves the margins, padding, borders and width of a block in the
// containing block cb (CSS 2.1 §10.3.3: auto margins, over-constrained width).
func geometry(st *style.Computed, cb cbox) geom {
	var g geom
	g.mt = clampCoord(px(st.Margin[style.Top], cb.w))
	g.mb = clampCoord(px(st.Margin[style.Bottom], cb.w))
	g.pt, g.pr = padPx(st.Padding[style.Top], cb.w), padPx(st.Padding[style.Right], cb.w)
	g.pb, g.pl = padPx(st.Padding[style.Bottom], cb.w), padPx(st.Padding[style.Left], cb.w)
	g.bt, g.br = st.Border[style.Top].Width, st.Border[style.Right].Width
	g.bb, g.bl = st.Border[style.Bottom].Width, st.Border[style.Left].Width

	ml, mr := st.Margin[style.Left], st.Margin[style.Right]
	autoL, autoR := ml.Kind == style.DimAuto, mr.Kind == style.DimAuto
	g.ml, g.mr = clampCoord(px(ml, cb.w)), clampCoord(px(mr, cb.w))
	extra := g.pl + g.pr + g.bl + g.br // padding and border: content-box sizing adds them
	if st.Width.Kind == style.DimAuto {
		g.w = math.Max(0, cb.w-g.ml-g.mr-extra)
	} else {
		g.w = contentWidth(st, px(st.Width, cb.w), extra)
	}
	if st.MaxWidth.Kind != style.DimNone {
		g.w = math.Min(g.w, contentWidth(st, px(st.MaxWidth, cb.w), extra))
	}
	g.borderW = g.w + extra
	free := cb.w - g.borderW - g.ml - g.mr
	switch {
	case autoL && autoR:
		g.ml = math.Max(0, free) / 2
		g.mr = math.Max(0, free) / 2
	case autoL:
		g.ml = math.Max(0, free)
	case autoR:
		g.mr = math.Max(0, free)
	default:
		// Over-constrained: the right margin absorbs what is left.
		g.mr += free
	}
	return g
}

func contentWidth(st *style.Computed, specified, extra float64) float64 {
	if st.BoxSizing == style.BorderBox {
		return math.Max(0, specified-extra)
	}
	return math.Max(0, specified)
}
