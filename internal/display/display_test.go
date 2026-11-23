package display

import (
	"testing"

	"github.com/glimps-re/htmltopdf/internal/text"
)

func TestRect(t *testing.T) {
	r := Rect{X: 1, Y: 2, W: 3, H: 4}
	if r.Right() != 4 || r.Bottom() != 6 {
		t.Errorf("right %v bottom %v", r.Right(), r.Bottom())
	}
}

func TestRadiiIsZero(t *testing.T) {
	var r Radii
	if !r.IsZero() {
		t.Error("zero radii")
	}
	r[2].V = 1
	if r.IsZero() {
		t.Error("non-zero radii")
	}
}

func TestTextWidth(t *testing.T) {
	tx := &Text{Glyphs: []text.Glyph{{Advance: 1.5}, {Advance: 2}}}
	if got := tx.Width(); got != 3.5 {
		t.Errorf("width = %v", got)
	}
}

func TestItemsImplementItem(t *testing.T) {
	l := List{FillRect{}, FillGradient{}, Border{}, &Text{}, Link{}, PushClip{}, PopClip{}}
	if len(l) != 7 {
		t.Fatal("list")
	}
}
