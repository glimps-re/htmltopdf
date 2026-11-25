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

func TestTransformItemsTranslate(t *testing.T) {
	// A rotation of 90° about the origin, then the frame moved by (10, 20): the
	// image of a moved point is the moved image of the point.
	m := [6]float64{0, 1, -1, 0, 3, 4}
	moved := PushTransform{M: m}.Translate(10, 20).(PushTransform)
	apply := func(m [6]float64, x, y float64) (float64, float64) {
		return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
	}
	for _, p := range [][2]float64{{0, 0}, {5, 7}, {-3, 100}} {
		wantX, wantY := apply(m, p[0], p[1])
		gotX, gotY := apply(moved.M, p[0]+10, p[1]+20)
		if gotX != wantX+10 || gotY != wantY+20 {
			t.Errorf("(%v,%v): got (%v,%v), want (%v,%v)", p[0], p[1], gotX, gotY, wantX+10, wantY+20)
		}
	}
	if (PopTransform{}).Translate(1, 2) != (PopTransform{}) {
		t.Error("PopTransform changed")
	}
}

func TestPathTranslateCopiesItsSegments(t *testing.T) {
	p := Path{Segs: []PathSeg{
		{Op: PathMoveTo, P: [3]Point{{X: 1, Y: 2}}},
		{Op: PathCubicTo, P: [3]Point{{X: 3, Y: 4}, {X: 5, Y: 6}, {X: 7, Y: 8}}},
		{Op: PathClose},
	}, Width: 2}
	q, ok := p.Translate(10, 100).(Path)
	if !ok {
		t.Fatal("not a Path")
	}
	if q.Segs[0].P[0] != (Point{X: 11, Y: 102}) || q.Segs[1].P[2] != (Point{X: 17, Y: 108}) || q.Width != 2 {
		t.Errorf("%+v", q)
	}
	if p.Segs[0].P[0] != (Point{X: 1, Y: 2}) {
		t.Error("Translate modified the original")
	}
}

func TestNewItemsImplementItem(t *testing.T) {
	l := List{PushTransform{}, PopTransform{}, Path{}}
	if len(l) != 3 {
		t.Fatal("list")
	}
}
