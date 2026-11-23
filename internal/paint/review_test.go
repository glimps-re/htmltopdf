package paint

import (
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/text"
)

func TestLinkRectClampedToPage(t *testing.T) {
	e := newEnv(t)
	_, links := e.page(t, a4, display.List{
		display.Link{Rect: display.Rect{X: -500, Y: -500, W: 600, H: 600}, URI: "https://a.example/"},
		display.Link{Rect: display.Rect{X: 700, Y: 900, W: 1e9, H: 1e9}, URI: "https://b.example/"},
	})
	if len(links) != 2 {
		t.Fatalf("%d links", len(links))
	}
	for _, l := range links {
		r := l.Rect
		if r[0] < 0 || r[1] < 0 || r[2] > 600 || r[3] > 750 || r[2] <= r[0] || r[3] <= r[1] {
			t.Fatalf("rect %v leaves the page", r)
		}
	}
	e.finish(t)
}

func TestPollEveryItems(t *testing.T) {
	e := newEnv(t)
	l := make(display.List, 3000)
	for i := range l {
		l[i] = display.FillRect{Rect: display.Rect{W: 1, H: 1}, Color: red}
	}
	e.page(t, a4, l)
}

func TestNestedClipsBeyondLimitStayClipped(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for range MaxClipDepth + 20 {
		l = append(l, display.PushClip{Rect: display.Rect{X: 10, Y: 10, W: 100, H: 100}})
	}
	l = append(l, display.FillRect{Rect: display.Rect{W: 800, H: 1000}, Color: red})
	for range MaxClipDepth + 20 {
		l = append(l, display.PopClip{})
	}
	content, _ := e.page(t, a4, l)
	if strings.Count(content, "W n") != MaxClipDepth {
		t.Fatalf("%d clips drawn", strings.Count(content, "W n"))
	}
	if strings.Index(content, " rg") < strings.LastIndex(content, "W n") {
		t.Fatal("the fill is not inside the outer clips")
	}
	e.finish(t)
}

func TestShaperReturnsCanonicalFaces(t *testing.T) {
	e := newEnv(t)
	spec := text.FontSpec{Families: []string{"sans-serif"}, Weight: 400, Size: 16}
	a, err := e.shaper.Shape("abc", spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec.Size = 30
	b, err := e.shaper.Shape("xyz", spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Face != b[0].Face {
		t.Fatal("the same face is returned as two pointers: a font would be embedded twice")
	}
}
