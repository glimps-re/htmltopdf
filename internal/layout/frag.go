package layout

import (
	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
)

// frag is a block fragment: the part of a block box on one page. Coordinates
// are layout px relative to the page area.
type frag struct {
	box *box.Box
	// rect is the border box of this fragment.
	rect display.Rect
	yset bool
	// border and padding are the used widths (top, right, bottom, left); the
	// top ones belong to the first fragment only, the bottom ones to the last.
	border, padding [4]float64
	// first and last tell whether this is the first and last fragment of the box.
	first, last bool
	borderColor *[4]css.Color
	borderLine  *lines
	children    []*frag
	lines       []*lineFrag
	// abs are the layers of the absolute boxes whose containing block is this
	// fragment, dx and dy its relative offset and rot its rotation.
	abs []*layer
	// rules are the column rules of a multi-column container, in the page
	// coordinates of its content.
	rules  []display.Rect
	dx, dy float64
	rot    float64
}

// lineFrag is a line box.
type lineFrag struct {
	// rect is the line box; baseline is the absolute y of the baseline.
	rect     display.Rect
	baseline float64
	items    []lineItem
}

type itemKind uint8

const (
	itemText itemKind = iota
	// itemOpen and itemClose delimit an inline box on the line: they carry its
	// padding, border and margin and its decoration.
	itemOpen
	itemClose
	// itemAtom is an atomic inline: its fragment is laid out as a block.
	itemAtom
)

// lineItem is a piece of a line: a text run or an inline box edge.
type lineItem struct {
	kind itemKind
	// x is the left edge, w the advance.
	x, w float64
	// text is the display item of a text run (X and Baseline are filled when
	// the line is placed).
	text *display.Text
	box  *box.Box
	href string
	// ascent and descent are the content area of the run or inline box.
	ascent, descent float64
	// rise is the vertical-align offset of the run, up: its baseline is rise
	// above that of the line (ascent and descent include it).
	rise float64
	// margin is the horizontal margin of an inline box edge, which the
	// background and borders do not cover.
	margin float64
	// atom is set for an itemAtom.
	atom *atomBox
}
