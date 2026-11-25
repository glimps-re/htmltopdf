package layout

import (
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

// zeroLine is a line of line-height 0: its box is empty, its baseline is at y.
func zeroLine(y float64) *lineFrag {
	return &lineFrag{rect: display.Rect{Y: y}, baseline: y}
}

// The skip tests compare bounds stored relative to the fragment and added
// back: with a top of 1.1, the bottom 6.3 comes back as 6.299999999999999.
// A line whose baseline is the cut position exactly must still be in the
// piece below it.
func TestCutKeepsALineOnTheCutAfterRounding(t *testing.T) {
	top, cut := 1.1, 6.3
	if (cut-top)+top >= cut {
		t.Fatal("the coordinates no longer round down: pick others")
	}
	indexed := &frag{rect: display.Rect{Y: top}}
	for i := range minIndexed {
		indexed.lines = append(indexed.lines, zeroLine(top+0.25*float64(i)))
	}
	indexed.lines = append(indexed.lines, zeroLine(cut), zeroLine(7), zeroLine(8))
	if got := len(cutFrag(indexed, cut, 100).lines); got != 3 {
		t.Errorf("indexed lines: %d in the piece, want 3", got)
	}
	overflow := &frag{children: []*frag{{rect: display.Rect{Y: top}, lines: []*lineFrag{zeroLine(cut)}}}}
	if piece := cutFrag(overflow, cut, 100); len(piece.children) != 1 || len(piece.children[0].lines) != 1 {
		t.Errorf("the overflow line of a box above the cut is not in the piece")
	}
}
