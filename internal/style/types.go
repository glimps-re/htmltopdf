// Package style computes the CSS style of the elements of a document.
//
// A stylesheet is compiled once (Compile): its selectors are parsed and its
// declarations are turned into typed specified values. Compiled sheets are
// immutable and shared by concurrent renders. A Resolver, one per render,
// builds the rule index for the media of the render and computes one
// *Computed per element: UA, document and ExtraCSS origins (design §9.1),
// inheritance, initial/inherit/unset, custom properties with var(), and
// sharing of identical styles bounded by the Styles limit.
package style

import "github.com/glimps-re/htmltopdf/internal/css"

// DimKind is the kind of a Dim.
type DimKind uint8

// Dimension kinds. DimPx is the zero value so that the zero Dim is 0px.
const (
	DimPx DimKind = iota
	DimPercent
	DimAuto
	DimNone
)

// Dim is a computed length: px, a percentage of a base known to layout, auto
// or none (max-width). Value is in px or in percent.
type Dim struct {
	Kind  DimKind
	Value float64
}

// Display is the computed display type.
type Display uint8

// Display values. List items are blocks: markers are not drawn. Children of a
// flex or grid container are blockified when computed.
const (
	DisplayInline Display = iota
	DisplayBlock
	DisplayNone
	DisplayListItem
	DisplayTable
	DisplayTableRowGroup
	DisplayTableHeaderGroup
	DisplayTableFooterGroup
	DisplayTableRow
	DisplayTableCell
	DisplayTableCaption
	DisplayTableColumn
	DisplayTableColumnGroup
	DisplayInlineBlock
	DisplayFlex
	DisplayGrid
)

// BoxSizing is the computed box-sizing.
type BoxSizing uint8

// Box sizing values.
const (
	ContentBox BoxSizing = iota
	BorderBox
)

// BorderStyle is a computed border style.
type BorderStyle uint8

// Border styles.
const (
	BorderNone BorderStyle = iota
	BorderHidden
	BorderSolid
)

// Border is one side of the border: the width is 0 when the style is none or
// hidden.
type Border struct {
	Width float64
	Style BorderStyle
	Color css.Color
}

// Corner holds the horizontal and vertical radii of one corner.
type Corner struct{ H, V Dim }

// Corners of Computed.Radius.
const (
	TopLeft = iota
	TopRight
	BottomRight
	BottomLeft
)

// Sides of the four-value arrays (CSS order).
const (
	Top = iota
	Right
	Bottom
	Left
)

// FontStyle is the computed font-style.
type FontStyle uint8

// Font styles.
const (
	FontNormal FontStyle = iota
	FontItalic
	FontOblique
)

// LineHeightKind tells how LineHeight.Value is read.
type LineHeightKind uint8

// Line height kinds: normal, a number (times the font size) or a length in px.
const (
	LineHeightNormal LineHeightKind = iota
	LineHeightNumber
	LineHeightPx
)

// LineHeight is the computed line-height.
type LineHeight struct {
	Kind  LineHeightKind
	Value float64
}

// TextAlign is the computed text-align.
type TextAlign uint8

// Text alignments (start and end are resolved to left and right; justify is
// not supported yet).
const (
	AlignLeft TextAlign = iota
	AlignRight
	AlignCenter
)

// OverflowWrap is the computed overflow-wrap (word-wrap is an alias).
type OverflowWrap uint8

// Overflow wrap values.
const (
	WrapNormal OverflowWrap = iota
	WrapBreakWord
	WrapAnywhere
)

// WordBreak is the computed word-break.
type WordBreak uint8

// Word break values: break-all adds a break opportunity between letters and
// digits (CSS Text 3 §5.2).
const (
	WordBreakNormal WordBreak = iota
	WordBreakAll
)

// WhiteSpace is the computed white-space.
type WhiteSpace uint8

// White space values.
const (
	WhiteSpaceNormal WhiteSpace = iota
	WhiteSpacePre
	WhiteSpaceNowrap
)

// TextTransform is the computed text-transform.
type TextTransform uint8

// Text transforms.
const (
	TransformNone TextTransform = iota
	TransformUppercase
	TransformLowercase
)

// Break is a computed break-before, break-after or break-inside value.
type Break uint8

// Break values: BreakPage covers page, left, right, recto and verso and is
// never the value of break-inside.
const (
	BreakAuto Break = iota
	BreakAvoid
	BreakPage
)

// Justify is the computed justify-content.
type Justify uint8

// Justify values; start and left map to JustifyStart, end and right to JustifyEnd.
const (
	JustifyStart Justify = iota
	JustifyEnd
	JustifyCenter
	JustifySpaceBetween
	JustifySpaceAround
	JustifySpaceEvenly
)

// ItemAlign is the computed align-items.
type ItemAlign uint8

// Item alignments.
const (
	ItemStretch ItemAlign = iota
	ItemStart
	ItemEnd
	ItemCenter
	ItemBaseline
)

// TrackKind is the kind of a grid Track.
type TrackKind uint8

// Track kinds.
const (
	TrackPx TrackKind = iota
	TrackPercent
	TrackFr
	TrackAuto
	TrackMinContent
	TrackMaxContent
)

// Track is one column track of grid-template-columns. Value is in px, percent
// or fr, and 0 for the keyword kinds.
type Track struct {
	Kind  TrackKind
	Value float64
}

// MaxGridTracks bounds the tracks of grid-template-columns once repeat() is
// expanded.
const MaxGridTracks = 1000

// Shadow is one outer box-shadow, lengths in px.
type Shadow struct {
	X, Y, Blur, Spread float64
	// Color is the used colour, opacity applied.
	Color css.Color
}

// MaxShadows bounds the shadows kept from a box-shadow list.
const MaxShadows = 8

// TextDecoration is a set of decoration lines (not inherited).
type TextDecoration uint8

// Decoration lines.
const (
	DecorationUnderline TextDecoration = 1 << iota
	DecorationLineThrough
)

// VerticalAlign is the computed vertical-align.
type VerticalAlign uint8

// Vertical alignments. VAlignLength is vertical-align: <length>, the offset being
// Computed.VAlignOffset.
const (
	VAlignBaseline VerticalAlign = iota
	VAlignTop
	VAlignMiddle
	VAlignBottom
	VAlignLength
)

// Position is the computed position. Only the containing block of its
// descendants and the layer of an absolute box are implemented: a relative box
// has no offset (design §5.2.3).
type Position uint8

// Positions; fixed and sticky are invalid values.
const (
	PositionStatic Position = iota
	PositionRelative
	PositionAbsolute
)

// CounterFormat is the style of a counter() in a content value.
type CounterFormat uint8

// Counter formats.
const (
	CounterDecimal CounterFormat = iota
	CounterDecimalLeadingZero
)

// CounterOp is one entry of counter-reset or counter-increment: the reset value
// or the increment, with saturating int32 arithmetic.
type CounterOp struct {
	Name  string
	Value int32
}

// MaxCounterOps bounds the entries kept from a counter-reset or counter-increment.
const MaxCounterOps = 32

// MaxIdentBytes bounds a counter or page name.
const MaxIdentBytes = 256

// Computed is the computed style of an element. It is immutable and shared by
// every element with the same style: never modify it.
type Computed struct {
	Display   Display
	BoxSizing BoxSizing

	// Margin and Padding are indexed by Top, Right, Bottom and Left.
	Margin, Padding [4]Dim
	Width, MaxWidth Dim
	Border          [4]Border
	Radius          [4]Corner

	// BorderCollapse is true for border-collapse: collapse.
	BorderCollapse bool
	// BorderSpacing is the horizontal and vertical border-spacing in px.
	BorderSpacing [2]float64
	// TableLayoutFixed is true for table-layout: fixed.
	TableLayoutFixed bool

	BackgroundColor css.Color
	// BackgroundImage is the gradient painted over the background color; the
	// opacity of the element and its ancestors is folded into its stop alphas.
	BackgroundImage *css.Gradient
	// OverflowHidden is true when the box clips its content.
	OverflowHidden bool

	// FontFamily lists the families in order, generic names included.
	FontFamily []string
	FontSize   float64
	FontWeight int
	FontStyle  FontStyle
	LineHeight LineHeight
	Color      css.Color

	TextAlign      TextAlign
	LetterSpacing  float64
	OverflowWrap   OverflowWrap
	WordBreak      WordBreak
	WhiteSpace     WhiteSpace
	TextDecoration TextDecoration
	VerticalAlign  VerticalAlign
	// VAlignOffset is the offset of VAlignLength in px, positive up.
	VAlignOffset Dim

	// ListStyleNone is true for list-style-type: none.
	ListStyleNone bool

	// TextTransform is inherited.
	TextTransform TextTransform
	// TabularNums is font-variant-numeric: tabular-nums, inherited. It has no
	// effect: the reference fonts have tabular figures.
	TabularNums bool

	BreakBefore, BreakAfter, BreakInside Break

	// FlexWrap is true for flex-wrap: wrap.
	FlexWrap       bool
	JustifyContent Justify
	AlignItems     ItemAlign
	FlexGrow       float64
	FlexShrink     float64
	FlexBasis      Dim
	RowGap         Dim
	ColumnGap      Dim
	// GridColumns is nil for none. It is shared between styles: never modify it.
	GridColumns []Track
	// BoxShadow is nil for none. It is shared between styles: never modify it.
	BoxShadow []Shadow
	// shadowSrc is the specified box-shadow, kept so that inherit resolves it
	// again against the colour and opacity of the inheriting element.
	shadowSrc []shadowSpec

	// Position is not inherited. Inset is top, right, bottom and left.
	Position Position
	Inset    [4]Dim
	// ZIndex is used unless ZAuto.
	ZIndex int32
	ZAuto  bool
	// Rotate is transform: rotate() in radians in [0, 2π), 0 for none.
	Rotate float64
	// Height is auto, px or a percentage that layout resolves.
	Height Dim
	// MinWidth is auto (the automatic minimum of a flex item, 0 elsewhere), px or a
	// percentage.
	MinWidth Dim
	// Page is the named page of the box, "" for auto. It is not inherited.
	Page string
	// Content is the content of a ::before or ::after box, nil when it has none:
	// it is only set on the style of a pseudo-element. It is shared: never modify it.
	Content []ContentItem
	// CounterReset and CounterIncrement are nil for none. They are shared: never
	// modify them.
	CounterReset, CounterIncrement []CounterOp

	// ownFade is 1 − opacity and fade is 1 − the product of the opacities of the
	// element and its ancestors, so that the zero value is opaque. When fade is
	// not 0, raw holds the colours before the product was applied.
	ownFade, fade float64
	raw           *unfaded

	rootFont float64
	vars     *varSet
}

// unfaded holds the colours of a style before opacity was applied: currentColor
// and inheritance use them.
type unfaded struct {
	color, background css.Color
	border            [4]css.Color
	gradient          *css.Gradient
}

func (c *Computed) rawGradient() *css.Gradient {
	if c.raw != nil {
		return c.raw.gradient
	}
	return c.BackgroundImage
}

// UsedOpacity is the product of the opacities of the element and its ancestors.
func (c *Computed) UsedOpacity() float64 { return 1 - c.fade }

func (c *Computed) rawColor() css.Color {
	if c.raw != nil {
		return c.raw.color
	}
	return c.Color
}

func (c *Computed) rawBackground() css.Color {
	if c.raw != nil {
		return c.raw.background
	}
	return c.BackgroundColor
}

func (c *Computed) rawBorder(i int) css.Color {
	if c.raw != nil {
		return c.raw.border[i]
	}
	return c.Border[i].Color
}

const initialFontSize = 16

func newInitial() Computed {
	c := Computed{
		Display:          DisplayInline,
		Width:            Dim{Kind: DimAuto},
		MaxWidth:         Dim{Kind: DimNone},
		Margin:           [4]Dim{},
		BorderSpacing:    [2]float64{},
		TableLayoutFixed: false,
		BackgroundColor:  css.Color{},
		FontFamily:       []string{"serif"},
		FontSize:         initialFontSize,
		FontWeight:       400,
		LineHeight:       LineHeight{Kind: LineHeightNormal},
		Color:            css.Color{A: 1},
		FlexShrink:       1,
		FlexBasis:        Dim{Kind: DimAuto},
		Height:           Dim{Kind: DimAuto},
		MinWidth:         Dim{Kind: DimAuto},
		Inset:            [4]Dim{{Kind: DimAuto}, {Kind: DimAuto}, {Kind: DimAuto}, {Kind: DimAuto}},
		ZAuto:            true,
		rootFont:         initialFontSize,
	}
	// medium; the width is dropped by finish while the style is none.
	for i := range c.Border {
		c.Border[i].Width = 3
	}
	return c
}

var initialStyle = newInitial()
