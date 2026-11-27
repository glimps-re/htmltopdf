package htmltopdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/layout"
)

// FuzzRender crasher da797ee0e395f09a: an absolute box 306720px below its
// inline-block containing block, whose line is sliced on the 96x19124 paper,
// was dropped with no warning. On A4 and on that paper, it is now drawn in a
// page area or reported text-off-page on its side, and no text is lost.
func TestSlicedLineKeepsFarAbsoluteText(t *testing.T) {
	const in = `0+a{ yzBst00!b0<p>0<A stYle="displAY:inline-BloCk;height:19122pX"><B stYle="position:ABsolute;top:19170pC">0000`
	for _, page := range []struct {
		name string
		size PageSize
	}{{"A4", PageSize{}}, {"paper", PageSize{Width: MinPageSide, Height: MaxPageSide}}} {
		t.Run(page.name, func(t *testing.T) {
			c, err := New(Options{PageSize: page.size})
			if err != nil {
				t.Fatal(err)
			}
			var tree *box.Box
			var pages []*layout.Page
			hooked := *c
			hooked.pageHook = func(root *box.Box, pg *layout.Page) { tree = root; pages = append(pages, pg) }
			var buf bytes.Buffer
			res, err := hooked.Render(context.Background(), strings.NewReader(in), &buf)
			if err != nil {
				t.Fatal(err)
			}
			if lost := layout.TextLost(tree, pages); lost != "" {
				t.Fatalf("text lost: %s", lost)
			}
			side, ok := placeOf(pages, "0000")
			if !ok {
				t.Fatalf("%q emitted on no page", "0000")
			}
			if side == "" {
				return
			}
			for _, w := range res.Warnings {
				if w.Code == WarnTextOffPage && w.Detail == side {
					return
				}
			}
			t.Errorf("%q drawn %s the page area with no text-off-page %q warning: %+v", "0000", side, side, res.Warnings)
		})
	}
}

// placeOf returns where the first text item holding word lies: "" when the
// middle of its x-height is inside the area of its page, else the side of the
// area it is past ("above", "below", "left" or "right"); ok is false when no
// page emits word.
func placeOf(pages []*layout.Page, word string) (side string, ok bool) {
	for _, pg := range pages {
		w, h := pg.Area.W/pg.Scale, pg.Area.H/pg.Scale
		for _, it := range pg.Items {
			tx, isText := it.(*display.Text)
			if !isText || !strings.Contains(tx.Text, word) {
				continue
			}
			switch mid := tx.Baseline - tx.Size/4; {
			case mid < 0:
				return "above", true
			case mid > h:
				return "below", true
			case tx.X >= w:
				return "right", true
			case tx.X < 0:
				return "left", true
			}
			return "", true
		}
	}
	return "", false
}
