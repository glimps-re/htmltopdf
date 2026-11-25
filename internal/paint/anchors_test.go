package paint

import (
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// An internal link carries the id of its target, an anchor becomes a destination
// at the top of its box, and the anchors of a page are those of the last Page.
func TestInternalLinkAndAnchor(t *testing.T) {
	e := newEnv(t)
	c := &pdf.Content{}
	list := display.List{
		display.Anchor{Name: "a", At: display.Point{X: 40, Y: 100}},
		display.Link{Rect: display.Rect{X: 10, Y: 10, W: 50, H: 10}, Dest: "b"},
		display.Anchor{Name: "", At: display.Point{Y: 5}},
		display.Anchor{Name: "far", At: display.Point{Y: 1e9}},
		display.Anchor{Name: "nan", At: display.Point{Y: math.NaN()}},
	}
	links, err := e.painter.Page(c, a4, list)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Dest != "b" || links[0].URI != "" {
		t.Fatalf("links %+v", links)
	}
	anchors := e.painter.Anchors()
	if len(anchors) != 2 || anchors[0].Name != "a" || anchors[1].Name != "far" {
		t.Fatalf("anchors %+v", anchors)
	}
	// y is the page height less the offset, unscaled and from the page top (as
	// Chrome writes it), clamped to the page.
	if want := (a4.Height - 100) * PxToPt; math.Abs(anchors[0].Y-want) > 1e-6 {
		t.Errorf("a at %v, want %v", anchors[0].Y, want)
	}
	if anchors[1].Y != 0 {
		t.Errorf("far at %v, want the clamp 0", anchors[1].Y)
	}
	ref, err := e.w.AddPage(600, 750, c, false, links)
	if err != nil {
		t.Fatal(err)
	}
	for _, an := range anchors {
		if err := e.w.SetDest(an.Name, ref, 0, an.Y); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.painter.Page(&pdf.Content{}, a4, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(e.painter.Anchors()); n != 0 {
		t.Errorf("%d anchors left from the previous page", n)
	}
	if err := e.w.Close(pdf.Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, e.buf.Bytes())
	if len(doc.Dests) != 3 || len(doc.Pages[0].Links) != 1 {
		t.Errorf("dests %+v links %+v", doc.Dests, doc.Pages[0].Links)
	}
}

// One destination per name and page, whatever the number of anchors of an inline
// box wrapped over lines.
func TestAnchorsAreDeduplicated(t *testing.T) {
	e := newEnv(t)
	list := display.List{
		display.Anchor{Name: "w", At: display.Point{Y: 10}},
		display.Anchor{Name: "w", At: display.Point{Y: 30}},
		display.Anchor{Name: "v", At: display.Point{Y: 50}},
	}
	if _, err := e.painter.Page(&pdf.Content{}, a4, list); err != nil {
		t.Fatal(err)
	}
	if a := e.painter.Anchors(); len(a) != 2 || a[0].Name != "w" || a[1].Name != "v" {
		t.Errorf("%+v", a)
	}
	if _, err := e.painter.Page(&pdf.Content{}, a4, list[:1]); err != nil {
		t.Fatal(err)
	}
	if a := e.painter.Anchors(); len(a) != 1 {
		t.Errorf("next page: %+v", a)
	}
}
