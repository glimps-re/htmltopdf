package style

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

// MaxFontSizePx bounds the computed font size: layout and shaping scale with
// it, and no real document needs more.
const MaxFontSizePx = 10000

// computeCtx carries what applying specified values needs.
type computeCtx struct {
	c      *Computed
	parent *Computed
	b      *budget.Budget
	// root is true for the element without parent.
	root bool
	// colorSet tells which border colors were specified, curBorder which of them
	// were currentcolor; curBackground is currentcolor on background-color.
	colorSet      [4]bool
	curBorder     [4]bool
	curBackground bool
}

func clampPx(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-css.MaxLengthPx, math.Min(css.MaxLengthPx, v))
}

// dim resolves a length against the em and rem bases; percentages stay percentages.
func (x *computeCtx) dim(l css.Length, em float64) Dim {
	switch l.Unit {
	case css.UnitEm:
		return Dim{Kind: DimPx, Value: clampPx(l.Value * em)}
	case css.UnitRem:
		return Dim{Kind: DimPx, Value: clampPx(l.Value * x.c.rootFont)}
	case css.UnitPercent:
		return Dim{Kind: DimPercent, Value: clampPx(l.Value)}
	}
	return Dim{Kind: DimPx, Value: clampPx(l.Value)}
}

func (x *computeCtx) px(l css.Length) float64 { return x.dim(l, x.c.FontSize).Value }

// compute builds the computed style from the specified values of the touched
// properties; warnings go to b.
func compute(parent *Computed, specs *[numProps]spec, touched []prop, b *budget.Budget) *Computed {
	c := initialStyle
	x := &computeCtx{c: &c, parent: parent, root: parent == nil, b: b}
	if parent != nil {
		c.inheritFrom(parent)
	}
	// Font size and color first: em lengths and currentcolor depend on them.
	for _, p := range [...]prop{pFontSize, pColor} {
		if specs[p].kind != specNone {
			x.apply(p, &specs[p])
		}
	}
	if x.root {
		c.rootFont = c.FontSize
	}
	for _, p := range touched {
		if p != pFontSize && p != pColor {
			x.apply(p, &specs[p])
		}
	}
	x.finish()
	return &c
}

// finish resolves currentcolor and the border widths.
func (x *computeCtx) finish() {
	c := x.c
	for i := range c.Border {
		if !x.colorSet[i] || x.curBorder[i] {
			c.Border[i].Color = c.Color
		}
		if c.Border[i].Style == BorderNone || c.Border[i].Style == BorderHidden {
			c.Border[i].Width = 0
		}
	}
	if x.curBackground {
		c.BackgroundColor = c.Color
	}
}

func (c *Computed) inheritFrom(p *Computed) {
	c.BorderCollapse, c.BorderSpacing = p.BorderCollapse, p.BorderSpacing
	c.FontFamily, c.FontSize, c.FontWeight, c.FontStyle = p.FontFamily, p.FontSize, p.FontWeight, p.FontStyle
	c.LineHeight, c.Color, c.TextAlign, c.LetterSpacing = p.LineHeight, p.Color, p.TextAlign, p.LetterSpacing
	c.OverflowWrap, c.WhiteSpace, c.ListStyleNone = p.OverflowWrap, p.WhiteSpace, p.ListStyleNone
	c.rootFont, c.vars = p.rootFont, p.vars
}

// copyProp copies the value of property p from src, for inherit and initial.
func (x *computeCtx) copyProp(p prop, src *Computed) {
	c := x.c
	switch p {
	case pDisplay:
		c.Display = src.Display
	case pBoxSizing:
		c.BoxSizing = src.BoxSizing
	case pMarginTop, pMarginRight, pMarginBottom, pMarginLeft:
		c.Margin[p-pMarginTop] = src.Margin[p-pMarginTop]
	case pPaddingTop, pPaddingRight, pPaddingBottom, pPaddingLeft:
		c.Padding[p-pPaddingTop] = src.Padding[p-pPaddingTop]
	case pWidth:
		c.Width = src.Width
	case pMaxWidth:
		c.MaxWidth = src.MaxWidth
	case pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth:
		c.Border[p-pBorderTopWidth].Width = src.Border[p-pBorderTopWidth].Width
	case pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle:
		c.Border[p-pBorderTopStyle].Style = src.Border[p-pBorderTopStyle].Style
	case pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor:
		i := p - pBorderTopColor
		if src == &initialStyle {
			x.colorSet[i] = false
			break
		}
		c.Border[i].Color = src.Border[i].Color
		x.colorSet[i], x.curBorder[i] = true, false
	case pBorderCollapse:
		c.BorderCollapse = src.BorderCollapse
	case pBorderSpacing:
		c.BorderSpacing = src.BorderSpacing
	case pTableLayout:
		c.TableLayoutFixed = src.TableLayoutFixed
	case pRadiusTopLeft, pRadiusTopRight, pRadiusBottomRight, pRadiusBottomLeft:
		c.Radius[p-pRadiusTopLeft] = src.Radius[p-pRadiusTopLeft]
	case pBackgroundColor:
		c.BackgroundColor, x.curBackground = src.BackgroundColor, false
	case pBackgroundImage:
		c.BackgroundImage = src.BackgroundImage
	case pOverflow:
		c.OverflowHidden = src.OverflowHidden
	case pFontFamily:
		c.FontFamily = src.FontFamily
	case pFontSize:
		c.FontSize = src.FontSize
	case pFontWeight:
		c.FontWeight = src.FontWeight
	case pFontStyle:
		c.FontStyle = src.FontStyle
	case pLineHeight:
		c.LineHeight = src.LineHeight
	case pColor:
		c.Color = src.Color
	case pTextAlign:
		c.TextAlign = src.TextAlign
	case pLetterSpacing:
		c.LetterSpacing = src.LetterSpacing
	case pOverflowWrap:
		c.OverflowWrap = src.OverflowWrap
	case pWhiteSpace:
		c.WhiteSpace = src.WhiteSpace
	case pTextDecoration:
		c.TextDecoration = src.TextDecoration
	case pVerticalAlign:
		c.VerticalAlign = src.VerticalAlign
	case pListStyleType:
		c.ListStyleNone = src.ListStyleNone
	}
}

// apply applies the specified value s of property p.
func (x *computeCtx) apply(p prop, s *spec) {
	switch s.kind {
	case specInherit:
		if x.parent != nil {
			x.copyProp(p, x.parent)
		} else {
			x.copyProp(p, &initialStyle)
		}
	case specInitial:
		x.copyProp(p, &initialStyle)
	case specUnset:
		if inherited[p] && x.parent != nil {
			x.copyProp(p, x.parent)
		} else {
			x.copyProp(p, &initialStyle)
		}
	case specValue:
		x.applyValue(p, s)
	}
}

func (x *computeCtx) applyValue(p prop, s *spec) {
	c := x.c
	switch p {
	case pDisplay:
		c.Display = Display(u8(s.n))
	case pBoxSizing:
		c.BoxSizing = BoxSizing(u8(s.n))
	case pMarginTop, pMarginRight, pMarginBottom, pMarginLeft:
		c.Margin[p-pMarginTop] = x.lengthOrAuto(s)
	case pPaddingTop, pPaddingRight, pPaddingBottom, pPaddingLeft:
		c.Padding[p-pPaddingTop] = x.dim(s.len, c.FontSize)
	case pWidth:
		c.Width = x.lengthOrAuto(s)
	case pMaxWidth:
		if s.none {
			c.MaxWidth = Dim{Kind: DimNone}
		} else {
			c.MaxWidth = x.dim(s.len, c.FontSize)
		}
	case pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth:
		c.Border[p-pBorderTopWidth].Width = x.px(s.len)
	case pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle:
		c.Border[p-pBorderTopStyle].Style = BorderStyle(u8(s.n))
	case pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor:
		i := p - pBorderTopColor
		x.colorSet[i], x.curBorder[i] = true, s.cur
		c.Border[i].Color = s.color
	case pBorderCollapse:
		c.BorderCollapse = s.n == 1
	case pBorderSpacing:
		c.BorderSpacing = [2]float64{x.px(s.len), x.px(s.len2)}
	case pTableLayout:
		c.TableLayoutFixed = s.n == 1
	case pRadiusTopLeft, pRadiusTopRight, pRadiusBottomRight, pRadiusBottomLeft:
		c.Radius[p-pRadiusTopLeft] = Corner{H: x.dim(s.len, c.FontSize), V: x.dim(s.len2, c.FontSize)}
	case pBackgroundColor:
		x.curBackground = s.cur
		c.BackgroundColor = s.color
	case pBackgroundImage:
		c.BackgroundImage = s.grad
	case pOverflow:
		c.OverflowHidden = s.n == 1
	case pFontFamily:
		c.FontFamily = s.list
	case pFontSize:
		c.FontSize = x.fontSize(s)
	case pFontWeight:
		c.FontWeight = x.fontWeight(s.n)
	case pFontStyle:
		c.FontStyle = FontStyle(u8(s.n))
	case pLineHeight:
		c.LineHeight = x.lineHeight(s)
	case pColor:
		if s.cur {
			if x.parent != nil {
				c.Color = x.parent.Color
			} else {
				c.Color = initialStyle.Color
			}
		} else {
			c.Color = s.color
		}
	case pTextAlign:
		c.TextAlign = TextAlign(u8(s.n))
	case pLetterSpacing:
		c.LetterSpacing = 0
		if !s.none {
			c.LetterSpacing = x.px(s.len)
		}
	case pOverflowWrap:
		c.OverflowWrap = OverflowWrap(u8(s.n))
	case pWhiteSpace:
		c.WhiteSpace = WhiteSpace(u8(s.n))
	case pTextDecoration:
		c.TextDecoration = TextDecoration(u8(s.n))
	case pVerticalAlign:
		c.VerticalAlign = VerticalAlign(u8(s.n))
	case pListStyleType:
		c.ListStyleNone = s.n == 1
	}
}

func (x *computeCtx) lengthOrAuto(s *spec) Dim {
	if s.auto {
		return Dim{Kind: DimAuto}
	}
	return x.dim(s.len, x.c.FontSize)
}

func (x *computeCtx) parentFontSize() float64 {
	if x.parent != nil {
		return x.parent.FontSize
	}
	return initialFontSize
}

func (x *computeCtx) fontSize(s *spec) float64 {
	parent := x.parentFontSize()
	var v float64
	switch {
	case s.n == sizeSmaller:
		v = parent / 1.2
	case s.n == sizeLarger:
		v = parent * 1.2
	case s.len.Unit == css.UnitPercent:
		v = s.len.Value / 100 * parent
	case s.len.Unit == css.UnitEm:
		v = s.len.Value * parent
	case s.len.Unit == css.UnitRem:
		root := float64(initialFontSize)
		if !x.root {
			root = x.c.rootFont
		}
		v = s.len.Value * root
	default:
		v = s.len.Value
	}
	clamped := math.Max(0, math.Min(MaxFontSizePx, clampPx(v)))
	if clamped != v && x.b != nil {
		x.b.Warn(budget.WarnClamped, "font-size")
	}
	return clamped
}

func (x *computeCtx) fontWeight(n int32) int {
	parent := 400
	if x.parent != nil {
		parent = x.parent.FontWeight
	}
	switch n {
	case weightBolder:
		switch {
		case parent < 350:
			return 400
		case parent < 550:
			return 700
		}
		return 900
	case weightLighter:
		switch {
		case parent < 550:
			return 100
		case parent < 750:
			return 400
		}
		return 700
	}
	return int(n)
}

func (x *computeCtx) lineHeight(s *spec) LineHeight {
	switch {
	case s.none:
		return LineHeight{Kind: LineHeightNormal}
	case s.n == 1:
		// times a font size of at most MaxFontSizePx: the product stays within the length bound
		return LineHeight{Kind: LineHeightNumber, Value: math.Min(s.f, css.MaxLengthPx/MaxFontSizePx)}
	case s.len.Unit == css.UnitPercent:
		return LineHeight{Kind: LineHeightPx, Value: clampPx(s.len.Value / 100 * x.c.FontSize)}
	}
	return LineHeight{Kind: LineHeightPx, Value: x.px(s.len)}
}

// u8 narrows an enum value stored in a spec: parsers only store small values.
func u8(n int32) uint8 { return uint8(min(max(n, 0), math.MaxUint8)) }
