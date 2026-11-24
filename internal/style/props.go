package style

import (
	"strings"

	"github.com/glimps-re/htmltopdf/internal/css"
)

// prop identifies a longhand property of the supported set.
type prop uint8

const (
	pCustom prop = iota // a --custom property: only its name matters
	pDisplay
	pBoxSizing
	pMarginTop
	pMarginRight
	pMarginBottom
	pMarginLeft
	pPaddingTop
	pPaddingRight
	pPaddingBottom
	pPaddingLeft
	pWidth
	pMaxWidth
	pBorderTopWidth
	pBorderRightWidth
	pBorderBottomWidth
	pBorderLeftWidth
	pBorderTopStyle
	pBorderRightStyle
	pBorderBottomStyle
	pBorderLeftStyle
	pBorderTopColor
	pBorderRightColor
	pBorderBottomColor
	pBorderLeftColor
	pBorderCollapse
	pBorderSpacing
	pTableLayout
	pRadiusTopLeft
	pRadiusTopRight
	pRadiusBottomRight
	pRadiusBottomLeft
	pBackgroundColor
	pBackgroundImage
	pOverflow
	pFontFamily
	pFontSize
	pFontWeight
	pFontStyle
	pLineHeight
	pColor
	pTextAlign
	pLetterSpacing
	pOverflowWrap
	pWordBreak
	pWhiteSpace
	pTextDecoration
	pVerticalAlign
	pListStyleType
	pContent
	pPageSize
	pTextTransform
	pFontVariantNumeric
	pBreakBefore
	pBreakAfter
	pBreakInside
	pOpacity
	pFlexDirection
	pFlexWrap
	pJustifyContent
	pAlignItems
	pFlexGrow
	pFlexShrink
	pFlexBasis
	pRowGap
	pColumnGap
	pGridColumns
	numProps
)

// inherited marks the inherited properties.
var inherited = [numProps]bool{
	pBorderCollapse: true, pBorderSpacing: true, pFontFamily: true, pFontSize: true, pFontWeight: true,
	pFontStyle: true, pLineHeight: true, pColor: true, pTextAlign: true, pLetterSpacing: true,
	pOverflowWrap: true, pWordBreak: true, pWhiteSpace: true, pListStyleType: true, pTextTransform: true,
	pFontVariantNumeric: true,
}

// longhands maps a property name to the longhand properties it sets: one for a
// longhand, several for a shorthand.
var longhands = map[string][]prop{
	"display": {pDisplay}, "box-sizing": {pBoxSizing},
	"margin-top": {pMarginTop}, "margin-right": {pMarginRight}, "margin-bottom": {pMarginBottom}, "margin-left": {pMarginLeft},
	"padding-top": {pPaddingTop}, "padding-right": {pPaddingRight}, "padding-bottom": {pPaddingBottom}, "padding-left": {pPaddingLeft},
	"margin":  {pMarginTop, pMarginRight, pMarginBottom, pMarginLeft},
	"padding": {pPaddingTop, pPaddingRight, pPaddingBottom, pPaddingLeft},
	"width":   {pWidth}, "max-width": {pMaxWidth},
	"border-top-width": {pBorderTopWidth}, "border-right-width": {pBorderRightWidth},
	"border-bottom-width": {pBorderBottomWidth}, "border-left-width": {pBorderLeftWidth},
	"border-top-style": {pBorderTopStyle}, "border-right-style": {pBorderRightStyle},
	"border-bottom-style": {pBorderBottomStyle}, "border-left-style": {pBorderLeftStyle},
	"border-top-color": {pBorderTopColor}, "border-right-color": {pBorderRightColor},
	"border-bottom-color": {pBorderBottomColor}, "border-left-color": {pBorderLeftColor},
	"border-width":  {pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth},
	"border-style":  {pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle},
	"border-color":  {pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor},
	"border-top":    {pBorderTopWidth, pBorderTopStyle, pBorderTopColor},
	"border-right":  {pBorderRightWidth, pBorderRightStyle, pBorderRightColor},
	"border-bottom": {pBorderBottomWidth, pBorderBottomStyle, pBorderBottomColor},
	"border-left":   {pBorderLeftWidth, pBorderLeftStyle, pBorderLeftColor},
	"border": {
		pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth,
		pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle,
		pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor,
	},
	"border-collapse": {pBorderCollapse}, "border-spacing": {pBorderSpacing}, "table-layout": {pTableLayout},
	"border-radius":              {pRadiusTopLeft, pRadiusTopRight, pRadiusBottomRight, pRadiusBottomLeft},
	"border-top-left-radius":     {pRadiusTopLeft},
	"border-top-right-radius":    {pRadiusTopRight},
	"border-bottom-right-radius": {pRadiusBottomRight},
	"border-bottom-left-radius":  {pRadiusBottomLeft},
	"background-color":           {pBackgroundColor},
	"background-image":           {pBackgroundImage},
	"background":                 {pBackgroundColor, pBackgroundImage},
	"overflow":                   {pOverflow},
	"font-family":                {pFontFamily},
	"font-size":                  {pFontSize},
	"font-weight":                {pFontWeight},
	"font-style":                 {pFontStyle},
	"line-height":                {pLineHeight},
	"color":                      {pColor},
	"text-align":                 {pTextAlign},
	"letter-spacing":             {pLetterSpacing},
	"overflow-wrap":              {pOverflowWrap},
	"word-wrap":                  {pOverflowWrap},
	"word-break":                 {pWordBreak},
	"white-space":                {pWhiteSpace},
	"text-decoration":            {pTextDecoration},
	"text-decoration-line":       {pTextDecoration},
	"vertical-align":             {pVerticalAlign},
	"list-style-type":            {pListStyleType},
	"list-style":                 {pListStyleType},
	"text-transform":             {pTextTransform},
	"font-variant-numeric":       {pFontVariantNumeric},
	"break-before":               {pBreakBefore},
	"break-after":                {pBreakAfter},
	"break-inside":               {pBreakInside},
	"opacity":                    {pOpacity},
	"font":                       {pFontStyle, pFontWeight, pFontSize, pLineHeight, pFontFamily, pFontVariantNumeric},
	"flex-direction":             {pFlexDirection}, "flex-wrap": {pFlexWrap}, "justify-content": {pJustifyContent},
	"align-items": {pAlignItems}, "flex-grow": {pFlexGrow}, "flex-shrink": {pFlexShrink}, "flex-basis": {pFlexBasis},
	"flex": {pFlexGrow, pFlexShrink, pFlexBasis}, "row-gap": {pRowGap}, "column-gap": {pColumnGap},
	"gap": {pRowGap, pColumnGap}, "grid-template-columns": {pGridColumns},
}

// declCtx tells where a declaration appears: properties differ between
// elements, margin boxes and @page rules.
type declCtx uint8

const (
	ctxElement declCtx = iota
	ctxMarginBox
	ctxPage
)

// marginBoxOnly lists the properties valid in margin boxes only.
var marginBoxOnly = map[string][]prop{"content": {pContent}}

// pageOnly lists the descriptors valid in @page rules only; the margin
// properties of longhands are valid there too.
var pageOnly = map[string][]prop{"size": {pPageSize}}

// propsOf returns the longhands the property name sets in ctx.
func propsOf(name string, ctx declCtx) ([]prop, bool) {
	switch ctx {
	case ctxMarginBox:
		if p, ok := marginBoxOnly[name]; ok {
			return p, true
		}
	case ctxPage:
		if p, ok := pageOnly[name]; ok {
			return p, true
		}
		p, ok := longhands[name]
		if !ok || !isMarginProp(p[0]) {
			return nil, false
		}
		return p, true
	}
	p, ok := longhands[name]
	return p, ok
}

func isMarginProp(p prop) bool { return p >= pMarginTop && p <= pMarginLeft }

type specKind uint8

const (
	specNone specKind = iota
	specValue
	specInherit
	specInitial
	specUnset
)

// spec is the specified value of one longhand, parsed when the sheet is
// compiled (or, after var() substitution, when the style is computed).
type spec struct {
	kind  specKind
	len   css.Length
	len2  css.Length
	auto  bool
	none  bool
	n     int32
	f     float64
	color css.Color
	cur   bool // currentcolor
	grad  *css.Gradient
	list  []string
	items []ContentItem
	grid  []gridGroup
}

type propSpec struct {
	prop prop
	spec spec
}

// parseStatus is the outcome of parsing a declaration.
type parseStatus uint8

const (
	parseOK parseStatus = iota
	parseInvalid
	parseUnsupported
)

// parseDecl parses the value of the property name into longhand specs. Names
// are lowercase except custom properties, handled by the caller.
func parseDecl(name string, toks []css.Token, ctx declCtx) ([]propSpec, parseStatus) {
	props, ok := propsOf(name, ctx)
	if !ok {
		return nil, parseUnsupported
	}
	toks = trim(toks)
	if len(toks) == 0 {
		return nil, parseInvalid
	}
	if kw, ok := ident(toks); ok {
		var k specKind
		switch kw {
		case "inherit":
			k = specInherit
		case "initial":
			k = specInitial
		case "unset":
			k = specUnset
		}
		if k != specNone {
			out := make([]propSpec, len(props))
			for i, p := range props {
				out[i] = propSpec{p, spec{kind: k}}
			}
			return out, parseOK
		}
	}
	out, ok := parseValue(name, props, toks)
	if !ok {
		return nil, parseInvalid
	}
	return out, parseOK
}

func one(p prop, s spec) []propSpec {
	s.kind = specValue
	return []propSpec{{p, s}}
}

func parseValue(name string, props []prop, toks []css.Token) ([]propSpec, bool) {
	switch name {
	case "margin":
		return boxValues(props, toks, true, true)
	case "padding":
		return boxValues(props, toks, false, false)
	case "border-width":
		return sideValues(props, toks, parseBorderWidth)
	case "border-style":
		return sideValues(props, toks, parseBorderStyle)
	case "border-color":
		return sideValues(props, toks, parseColorSpec)
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		return borderShorthand(props, toks)
	case "border-radius":
		return radiusShorthand(props, toks)
	case "background":
		return backgroundShorthand(toks)
	case "list-style":
		return listStyleShorthand(toks)
	case "font":
		return fontShorthand(toks)
	case "flex":
		return flexShorthand(toks)
	case "gap":
		return gapShorthand(toks)
	}
	p := props[0]
	s, ok := parseLonghand(p, toks)
	if !ok {
		return nil, false
	}
	return one(p, s), true
}

func parseLonghand(p prop, toks []css.Token) (spec, bool) {
	switch p {
	case pDisplay:
		return keyword(toks, displayKeywords)
	case pBoxSizing:
		return keyword(toks, boxSizingKeywords)
	case pMarginTop, pMarginRight, pMarginBottom, pMarginLeft:
		return parseLengthPercentage(toks, true, true)
	case pPaddingTop, pPaddingRight, pPaddingBottom, pPaddingLeft:
		return parseLengthPercentage(toks, false, false)
	case pWidth:
		return parseSize(toks, "auto")
	case pMaxWidth:
		return parseSize(toks, "none")
	case pBorderTopWidth, pBorderRightWidth, pBorderBottomWidth, pBorderLeftWidth:
		return parseBorderWidth(toks)
	case pBorderTopStyle, pBorderRightStyle, pBorderBottomStyle, pBorderLeftStyle:
		return parseBorderStyle(toks)
	case pBorderTopColor, pBorderRightColor, pBorderBottomColor, pBorderLeftColor, pColor, pBackgroundColor:
		return parseColorSpec(toks)
	case pBorderCollapse:
		return keyword(toks, collapseKeywords)
	case pBorderSpacing:
		return parseBorderSpacing(toks)
	case pTableLayout:
		return keyword(toks, layoutKeywords)
	case pRadiusTopLeft, pRadiusTopRight, pRadiusBottomRight, pRadiusBottomLeft:
		return parseRadius(toks)
	case pBackgroundImage:
		return parseBackgroundImage(toks)
	case pOverflow:
		return parseOverflow(toks)
	case pFontFamily:
		return parseFontFamily(toks)
	case pFontSize:
		return parseFontSize(toks)
	case pFontWeight:
		return parseFontWeight(toks)
	case pFontStyle:
		return keyword(toks, fontStyleKeywords)
	case pLineHeight:
		return parseLineHeight(toks)
	case pTextAlign:
		return keyword(toks, textAlignKeywords)
	case pLetterSpacing:
		return parseLetterSpacing(toks)
	case pOverflowWrap:
		return keyword(toks, overflowWrapKeywords)
	case pWordBreak:
		return keyword(toks, wordBreakKeywords)
	case pWhiteSpace:
		return keyword(toks, whiteSpaceKeywords)
	case pTextDecoration:
		return parseTextDecoration(toks)
	case pVerticalAlign:
		return keyword(toks, verticalAlignKeywords)
	case pListStyleType:
		return parseListStyleType(toks)
	case pContent:
		return parseContent(toks)
	case pPageSize:
		return parsePageSize(toks)
	case pTextTransform:
		return keyword(toks, textTransformKeywords)
	case pFontVariantNumeric:
		return keyword(toks, variantNumericKeywords)
	case pBreakBefore, pBreakAfter:
		return keyword(toks, breakKeywords)
	case pBreakInside:
		return keyword(toks, breakInsideKeywords)
	case pOpacity:
		return parseOpacity(toks)
	case pFlexDirection:
		return keyword(toks, flexDirectionKeywords)
	case pFlexWrap:
		return keyword(toks, flexWrapKeywords)
	case pJustifyContent:
		return keyword(toks, justifyKeywords)
	case pAlignItems:
		return keyword(toks, alignItemsKeywords)
	case pFlexGrow, pFlexShrink:
		return parseFlexFactor(toks)
	case pFlexBasis:
		return parseFlexBasis(toks)
	case pRowGap, pColumnGap:
		return parseGap(toks)
	case pGridColumns:
		return parseGridColumns(toks)
	}
	return spec{}, false
}

var (
	boxSizingKeywords = map[string]int32{"content-box": int32(ContentBox), "border-box": int32(BorderBox)}
	collapseKeywords  = map[string]int32{"collapse": 1, "separate": 0}
	layoutKeywords    = map[string]int32{"fixed": 1, "auto": 0}
	fontStyleKeywords = map[string]int32{"normal": int32(FontNormal), "italic": int32(FontItalic), "oblique": int32(FontOblique)}
	textAlignKeywords = map[string]int32{
		"left": int32(AlignLeft), "start": int32(AlignLeft), "right": int32(AlignRight), "end": int32(AlignRight),
		"center": int32(AlignCenter),
	}
	overflowWrapKeywords  = map[string]int32{"normal": int32(WrapNormal), "break-word": int32(WrapBreakWord), "anywhere": int32(WrapAnywhere)}
	wordBreakKeywords     = map[string]int32{"normal": int32(WordBreakNormal), "break-all": int32(WordBreakAll)}
	whiteSpaceKeywords    = map[string]int32{"normal": int32(WhiteSpaceNormal), "pre": int32(WhiteSpacePre), "nowrap": int32(WhiteSpaceNowrap)}
	verticalAlignKeywords = map[string]int32{
		"baseline": int32(VAlignBaseline), "top": int32(VAlignTop), "middle": int32(VAlignMiddle), "bottom": int32(VAlignBottom),
	}
	borderStyleKeywords = map[string]int32{"none": int32(BorderNone), "hidden": int32(BorderHidden), "solid": int32(BorderSolid)}
)

var displayKeywords = map[string]int32{
	"none": int32(DisplayNone), "block": int32(DisplayBlock), "inline": int32(DisplayInline),
	"list-item": int32(DisplayListItem), "table": int32(DisplayTable),
	"table-row-group": int32(DisplayTableRowGroup), "table-header-group": int32(DisplayTableHeaderGroup),
	"table-footer-group": int32(DisplayTableFooterGroup), "table-row": int32(DisplayTableRow),
	"table-cell": int32(DisplayTableCell), "table-caption": int32(DisplayTableCaption),
	"table-column": int32(DisplayTableColumn), "table-column-group": int32(DisplayTableColumnGroup),
	"inline-block": int32(DisplayInlineBlock), "flex": int32(DisplayFlex), "grid": int32(DisplayGrid),
}

var (
	textTransformKeywords  = map[string]int32{"none": int32(TransformNone), "uppercase": int32(TransformUppercase), "lowercase": int32(TransformLowercase)}
	variantNumericKeywords = map[string]int32{"normal": 0, "tabular-nums": 1}
	breakInsideKeywords    = map[string]int32{"auto": int32(BreakAuto), "avoid": int32(BreakAvoid), "avoid-page": int32(BreakAvoid)}
	breakKeywords          = map[string]int32{
		"auto": int32(BreakAuto), "avoid": int32(BreakAvoid), "avoid-page": int32(BreakAvoid), "page": int32(BreakPage),
		"left": int32(BreakPage), "right": int32(BreakPage), "recto": int32(BreakPage), "verso": int32(BreakPage),
	}
)

func keyword(toks []css.Token, table map[string]int32) (spec, bool) {
	kw, ok := ident(toks)
	if !ok {
		return spec{}, false
	}
	n, ok := table[kw]
	return spec{n: n}, ok
}

// parseLengthPercentage parses a length or percentage; auto when allowed.
func parseLengthPercentage(toks []css.Token, allowAuto, allowNegative bool) (spec, bool) {
	toks = trim(toks)
	if allowAuto {
		if kw, ok := ident(toks); ok && kw == "auto" {
			return spec{auto: true}, true
		}
	}
	l, ok := css.ParseLength(toks)
	if !ok || (!allowNegative && l.Value < 0) {
		return spec{}, false
	}
	return spec{len: l}, true
}

func parseSize(toks []css.Token, word string) (spec, bool) {
	if kw, ok := ident(toks); ok && kw == word {
		if word == "none" {
			return spec{none: true}, true
		}
		return spec{auto: true}, true
	}
	return parseLengthPercentage(toks, false, false)
}

var borderWidthKeywords = map[string]float64{"thin": 1, "medium": 3, "thick": 5}

func parseBorderWidth(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok {
		w, ok := borderWidthKeywords[kw]
		return spec{len: css.Length{Value: w, Unit: css.UnitPx}}, ok
	}
	l, ok := css.ParseLength(toks)
	if !ok || l.Value < 0 || l.Unit == css.UnitPercent {
		return spec{}, false
	}
	return spec{len: l}, true
}

func parseBorderStyle(toks []css.Token) (spec, bool) {
	return keyword(toks, borderStyleKeywords)
}

func parseColorSpec(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok && kw == "currentcolor" {
		return spec{cur: true}, true
	}
	c, ok := css.ParseColor(toks)
	return spec{color: c}, ok
}

func parseBorderSpacing(toks []css.Token) (spec, bool) {
	fs := fields(trim(toks))
	if len(fs) < 1 || len(fs) > 2 {
		return spec{}, false
	}
	h, ok := css.ParseLength(fs[0])
	if !ok || h.Value < 0 || h.Unit == css.UnitPercent {
		return spec{}, false
	}
	v := h
	if len(fs) == 2 {
		if v, ok = css.ParseLength(fs[1]); !ok || v.Value < 0 || v.Unit == css.UnitPercent {
			return spec{}, false
		}
	}
	return spec{len: h, len2: v}, true
}

func parseRadius(toks []css.Token) (spec, bool) {
	fs := fields(trim(toks))
	if len(fs) < 1 || len(fs) > 2 {
		return spec{}, false
	}
	h, ok := css.ParseLength(fs[0])
	if !ok || h.Value < 0 {
		return spec{}, false
	}
	v := h
	if len(fs) == 2 {
		if v, ok = css.ParseLength(fs[1]); !ok || v.Value < 0 {
			return spec{}, false
		}
	}
	return spec{len: h, len2: v}, true
}

func parseBackgroundImage(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok && kw == "none" {
		return spec{none: true}, true
	}
	g, ok := css.ParseGradient(toks)
	if !ok {
		return spec{}, false
	}
	return spec{grad: &g}, true
}

func parseOverflow(toks []css.Token) (spec, bool) {
	fs := fields(trim(toks))
	if len(fs) < 1 || len(fs) > 2 {
		return spec{}, false
	}
	hidden := int32(0)
	for _, f := range fs {
		kw, ok := ident(f)
		if !ok {
			return spec{}, false
		}
		switch kw {
		case "hidden", "clip":
			hidden = 1
		case "visible", "auto", "scroll":
		default:
			return spec{}, false
		}
	}
	return spec{n: hidden}, true
}

var genericFamilies = map[string]bool{
	"serif": true, "sans-serif": true, "monospace": true, "cursive": true, "fantasy": true, "system-ui": true,
}

// MaxFontFamilies bounds the families kept from a font-family list.
const MaxFontFamilies = 32

// familiesCut is the n of a font-family spec whose list was cut at
// MaxFontFamilies.
const familiesCut = 1

func parseFontFamily(toks []css.Token) (spec, bool) {
	var list []string
	var cut int32
	for _, part := range splitOn(trim(toks), isComma) {
		if len(part) == 0 {
			return spec{}, false
		}
		var name strings.Builder
		for i, t := range part {
			switch {
			case t.Type == css.TString && len(part) == 1:
				name.WriteString(t.Text)
			case t.Type == css.TIdent:
				if i > 0 {
					name.WriteByte(' ')
				}
				name.WriteString(t.Text)
			case t.Type == css.TWhitespace:
			default:
				return spec{}, false
			}
		}
		n := name.String()
		if len(part) == 1 && part[0].Type == css.TIdent && genericFamilies[lowerASCII(n)] {
			n = lowerASCII(n)
		}
		if len(list) < MaxFontFamilies {
			list = append(list, n)
		} else {
			cut = familiesCut
		}
	}
	return spec{list: list, n: cut}, len(list) > 0
}

var fontSizeKeywords = map[string]float64{
	"xx-small": 9, "x-small": 10, "small": 13, "medium": 16, "large": 18, "x-large": 24, "xx-large": 32, "xxx-large": 48,
}

// Font size specs: n == sizeSmaller or sizeLarger for the relative keywords.
const (
	sizeSmaller = 1
	sizeLarger  = 2
)

func parseFontSize(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok {
		switch kw {
		case "smaller":
			return spec{n: sizeSmaller}, true
		case "larger":
			return spec{n: sizeLarger}, true
		}
		px, ok := fontSizeKeywords[kw]
		return spec{len: css.Length{Value: px, Unit: css.UnitPx}}, ok
	}
	return parseLengthPercentage(toks, false, false)
}

// Font weight specs: n is the weight, or weightBolder and weightLighter.
const (
	weightBolder  = -1
	weightLighter = -2
)

func parseFontWeight(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok {
		switch kw {
		case "normal":
			return spec{n: 400}, true
		case "bold":
			return spec{n: 700}, true
		case "bolder":
			return spec{n: weightBolder}, true
		case "lighter":
			return spec{n: weightLighter}, true
		}
		return spec{}, false
	}
	if len(toks) == 1 && toks[0].Type == css.TNumber && toks[0].Num >= 1 && toks[0].Num <= 1000 {
		return spec{n: int32(toks[0].Num)}, true
	}
	return spec{}, false
}

func parseLineHeight(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok {
		return spec{none: true}, kw == "normal"
	}
	if len(toks) == 1 && toks[0].Type == css.TNumber {
		return spec{f: float64(toks[0].Num), n: 1}, toks[0].Num >= 0
	}
	s, ok := parseLengthPercentage(toks, false, false)
	return s, ok
}

func parseLetterSpacing(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok {
		return spec{none: true}, kw == "normal"
	}
	l, ok := css.ParseLength(toks)
	if !ok || l.Unit == css.UnitPercent {
		return spec{}, false
	}
	return spec{len: l}, true
}

func parseTextDecoration(toks []css.Token) (spec, bool) {
	var set int32
	for _, f := range fields(trim(toks)) {
		kw, ok := ident(f)
		if !ok {
			return spec{}, false
		}
		switch kw {
		case "none":
		case "underline":
			set |= int32(DecorationUnderline)
		case "line-through":
			set |= int32(DecorationLineThrough)
		case "solid":
		default:
			return spec{}, false
		}
	}
	return spec{n: set}, true
}

func parseListStyleType(toks []css.Token) (spec, bool) {
	kw, ok := ident(toks)
	if !ok {
		return spec{}, false
	}
	switch kw {
	case "none":
		return spec{n: 1}, true
	case "disc", "circle", "square", "decimal":
		return spec{}, true
	}
	return spec{}, false
}

// boxValues expands margin and padding: one to four values.
func boxValues(props []prop, toks []css.Token, allowAuto, allowNegative bool) ([]propSpec, bool) {
	return sideValues(props, toks, func(t []css.Token) (spec, bool) {
		return parseLengthPercentage(t, allowAuto, allowNegative)
	})
}

// sideValues expands a one to four value shorthand over the four sides.
func sideValues(props []prop, toks []css.Token, parse func([]css.Token) (spec, bool)) ([]propSpec, bool) {
	fs := fields(toks)
	if len(fs) < 1 || len(fs) > 4 {
		return nil, false
	}
	var v [4]spec
	for i, f := range fs {
		s, ok := parse(f)
		if !ok {
			return nil, false
		}
		v[i] = s
	}
	switch len(fs) {
	case 1:
		v[1], v[2], v[3] = v[0], v[0], v[0]
	case 2:
		v[2], v[3] = v[0], v[1]
	case 3:
		v[3] = v[1]
	}
	out := make([]propSpec, 4)
	for i := range out {
		v[i].kind = specValue
		out[i] = propSpec{props[i], v[i]}
	}
	return out, true
}

// borderShorthand parses border and border-<side>: a width, a style and a
// color in any order; omitted components take their initial value.
func borderShorthand(props []prop, toks []css.Token) ([]propSpec, bool) {
	var w, st, c *spec
	for _, f := range fields(toks) {
		if s, ok := parseBorderStyle(f); ok && st == nil {
			st = &s
		} else if s, ok := parseBorderWidth(f); ok && w == nil {
			w = &s
		} else if s, ok := parseColorSpec(f); ok && c == nil {
			c = &s
		} else {
			return nil, false
		}
	}
	width := spec{kind: specInitial}
	style := spec{kind: specInitial}
	color := spec{kind: specInitial}
	if w != nil {
		width = *w
		width.kind = specValue
	}
	if st != nil {
		style = *st
		style.kind = specValue
	}
	if c != nil {
		color = *c
		color.kind = specValue
	}
	n := len(props) / 3
	out := make([]propSpec, 0, len(props))
	for i := range n {
		out = append(out, propSpec{props[i], width}, propSpec{props[n+i], style}, propSpec{props[2*n+i], color})
	}
	return out, true
}

func radiusShorthand(props []prop, toks []css.Token) ([]propSpec, bool) {
	halves := splitOn(toks, isSlash)
	if len(halves) < 1 || len(halves) > 2 {
		return nil, false
	}
	expand := func(t []css.Token) ([4]css.Length, bool) {
		fs := fields(t)
		var out [4]css.Length
		if len(fs) < 1 || len(fs) > 4 {
			return out, false
		}
		for i, f := range fs {
			l, ok := css.ParseLength(f)
			if !ok || l.Value < 0 {
				return out, false
			}
			out[i] = l
		}
		switch len(fs) {
		case 1:
			out[1], out[2], out[3] = out[0], out[0], out[0]
		case 2:
			out[2], out[3] = out[0], out[1]
		case 3:
			out[3] = out[1]
		}
		return out, true
	}
	h, ok := expand(halves[0])
	if !ok {
		return nil, false
	}
	v := h
	if len(halves) == 2 {
		if v, ok = expand(halves[1]); !ok {
			return nil, false
		}
	}
	out := make([]propSpec, 4)
	for i := range out {
		out[i] = propSpec{props[i], spec{kind: specValue, len: h[i], len2: v[i]}}
	}
	return out, true
}

func backgroundShorthand(toks []css.Token) ([]propSpec, bool) {
	color := spec{kind: specInitial}
	image := spec{kind: specInitial}
	var haveColor, haveImage bool
	for _, f := range fields(toks) {
		if s, ok := parseColorSpec(f); ok && !haveColor {
			color, haveColor = s, true
			color.kind = specValue
		} else if s, ok := parseBackgroundImage(f); ok && !haveImage {
			image, haveImage = s, true
			image.kind = specValue
		} else {
			return nil, false
		}
	}
	return []propSpec{{pBackgroundColor, color}, {pBackgroundImage, image}}, true
}

func listStyleShorthand(toks []css.Token) ([]propSpec, bool) {
	out := spec{kind: specInitial}
	for _, f := range fields(toks) {
		kw, ok := ident(f)
		if !ok {
			return nil, false
		}
		switch kw {
		case "inside", "outside":
		default:
			s, ok := parseListStyleType(f)
			if !ok {
				return nil, false
			}
			out, out.kind = s, specValue
		}
	}
	return []propSpec{{pListStyleType, out}}, true
}
