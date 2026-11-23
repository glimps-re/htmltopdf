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

// Display values. List items are blocks: markers are not drawn.
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
)

// WhiteSpace is the computed white-space.
type WhiteSpace uint8

// White space values.
const (
	WhiteSpaceNormal WhiteSpace = iota
	WhiteSpacePre
)

// TextDecoration is a set of decoration lines (not inherited).
type TextDecoration uint8

// Decoration lines.
const (
	DecorationUnderline TextDecoration = 1 << iota
	DecorationLineThrough
)

// VerticalAlign is the computed vertical-align.
type VerticalAlign uint8

// Vertical alignments.
const (
	VAlignBaseline VerticalAlign = iota
	VAlignTop
	VAlignMiddle
	VAlignBottom
)

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
	// BackgroundImage is the linear gradient painted over the background color.
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
	WhiteSpace     WhiteSpace
	TextDecoration TextDecoration
	VerticalAlign  VerticalAlign

	// ListStyleNone is true for list-style-type: none.
	ListStyleNone bool

	rootFont float64
	vars     *varSet
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
		rootFont:         initialFontSize,
	}
	// medium; the width is dropped by finish while the style is none.
	for i := range c.Border {
		c.Border[i].Width = 3
	}
	return c
}

var initialStyle = newInitial()
