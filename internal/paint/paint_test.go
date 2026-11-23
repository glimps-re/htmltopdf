package paint

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

func TestFillRect(t *testing.T) {
	e := newEnv(t)
	got, _ := e.page(t, a4, display.List{display.FillRect{Rect: display.Rect{X: 100, Y: 200, W: 50, H: 30}, Color: red}})
	// 100px -> 75pt; y = (1000 - 230) px -> 577.5pt; 50 x 30 px -> 37.5 x 22.5pt.
	want := lines("1 0 0 rg", "75 577.5 37.5 22.5 re", "f")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	e.finish(t)
}

func TestAreaOffsetAndScale(t *testing.T) {
	e := newEnv(t)
	g := Geometry{Width: 800, Height: 1000, Area: display.Rect{X: 40, Y: 80, W: 720, H: 840}, Scale: 0.5}
	got, _ := e.page(t, g, display.List{display.FillRect{Rect: display.Rect{X: 100, Y: 200, W: 50, H: 30}, Color: blue}})
	// x = (40 + 0.5*100) * .75 = 67.5; top y = (1000 - 80 - 100) * .75 = 615; size 25 x 15 px = 18.75 x 11.25pt;
	// bottom = 615 - 11.25 = 603.75
	want := lines("0 0 1 rg", "67.5 603.75 18.75 11.25 re", "f")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFillSkipped(t *testing.T) {
	e := newEnv(t)
	r := display.Rect{X: 10, Y: 10, W: 10, H: 10}
	for name, it := range map[string]display.Item{
		"transparent": display.FillRect{Rect: r},
		"no width":    display.FillRect{Rect: display.Rect{X: 1, Y: 1, H: 5}, Color: red},
		"no height":   display.FillRect{Rect: display.Rect{X: 1, Y: 1, W: 5}, Color: red},
		"negative":    display.FillRect{Rect: display.Rect{X: 1, Y: 1, W: -5, H: 5}, Color: red},
		"nan size":    display.FillRect{Rect: display.Rect{X: 1, Y: 1, W: math.NaN(), H: 5}, Color: red},
		"left":        display.FillRect{Rect: display.Rect{X: -500, Y: 0, W: 100, H: 100}, Color: red},
		"right":       display.FillRect{Rect: display.Rect{X: 900, Y: 0, W: 100, H: 100}, Color: red},
		"above":       display.FillRect{Rect: display.Rect{X: 0, Y: -500, W: 100, H: 100}, Color: red},
		"below":       display.FillRect{Rect: display.Rect{X: 0, Y: 1100, W: 100, H: 100}, Color: red},
		"gradient":    display.FillGradient{Rect: r},
	} {
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, display.List{it}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(c.Bytes()) != 0 {
			t.Errorf("%s drew %q", name, c.Bytes())
		}
	}
}

func TestPartlyVisibleItemsAreKept(t *testing.T) {
	e := newEnv(t)
	for name, r := range map[string]display.Rect{
		"left edge": {X: -50, Y: 100, W: 100, H: 10}, "right edge": {X: 750, Y: 100, W: 100, H: 10},
		"top edge": {X: 100, Y: -5, W: 10, H: 10}, "bottom edge": {X: 100, Y: 995, W: 10, H: 10},
		"huge": {X: -1e9, Y: -1e9, W: 3e9, H: 3e9},
	} {
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, display.List{display.FillRect{Rect: r, Color: red}}); err != nil || len(c.Bytes()) == 0 {
			t.Errorf("%s: err %v, %q", name, err, c.Bytes())
		}
	}
}

func TestCoordinatesAreClamped(t *testing.T) {
	e := newEnv(t)
	huge := display.List{
		display.FillRect{Rect: display.Rect{X: -1e30, Y: -1e30, W: 2e30, H: 2e30}, Color: red},
		display.FillRect{Rect: display.Rect{X: math.Inf(-1), Y: 0, W: math.Inf(1), H: 1e300}, Color: red},
		display.FillRect{Rect: display.Rect{X: math.NaN(), Y: math.NaN(), W: 10, H: 10}, Color: red},
		display.PushClip{Rect: display.Rect{X: -1e300, Y: -1e300, W: 1e301, H: 1e301}},
		display.FillRect{Rect: display.Rect{X: 1, Y: 1, W: 10, H: 10}, Color: blue},
		display.PopClip{},
	}
	got, _ := e.page(t, a4, huge)
	if !strings.Contains(got, "-1000000") || !strings.Contains(got, "1000000") {
		t.Fatalf("coordinates were not clamped to 1e6 pt:\n%s", got)
	}
	balanced(t, got)
	e.finish(t)
}

func TestScaleOutOfRange(t *testing.T) {
	e := newEnv(t)
	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		g := a4
		g.Scale = scale
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, g, display.List{display.FillRect{Rect: display.Rect{X: 100, Y: 200, W: 50, H: 30}, Color: red}}); err != nil {
			t.Fatal(err)
		}
		if want := lines("1 0 0 rg", "75 577.5 37.5 22.5 re", "f"); string(c.Bytes()) != want {
			t.Errorf("scale %v: %q", scale, c.Bytes())
		}
	}
}

func TestRoundedFill(t *testing.T) {
	e := newEnv(t)
	r := display.Radii{{H: 8, V: 8}, {H: 8, V: 4}, {H: 0, V: 0}, {H: 100, V: 100}}
	got, _ := e.page(t, a4, display.List{display.FillRect{Rect: display.Rect{X: 100, Y: 200, W: 50, H: 30}, Radii: r, Color: red}})
	if strings.Count(got, " c\n") != 4 || !strings.HasSuffix(got, "h\nf\n") {
		t.Fatalf("rounded fill:\n%s", got)
	}
	e.finish(t)
}

func TestUniformBorder(t *testing.T) {
	e := newEnv(t)
	side := display.BorderSide{Width: 4, Color: red}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 80, H: 40}, Sides: [4]display.BorderSide{side, side, side, side},
	}})
	// outer 60 x 30 pt at (75, 570); inner inset by 3 pt.
	want := lines("1 0 0 rg", "75 570 60 30 re", "78 573 54 24 re", "f*")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	e.finish(t)
}

func TestUniformBorderThickerThanTheBox(t *testing.T) {
	e := newEnv(t)
	side := display.BorderSide{Width: 40, Color: red}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 50, H: 30}, Sides: [4]display.BorderSide{side, side, side, side},
	}})
	if want := lines("1 0 0 rg", "75 577.5 37.5 22.5 re", "f"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBorderSides(t *testing.T) {
	e := newEnv(t)
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 80, H: 40},
		Sides: [4]display.BorderSide{
			{Width: 4, Color: red}, {Width: 0, Color: blue}, {Width: 8, Color: blue}, {Width: 4, Color: black},
		},
	}})
	// box (pt): x 75..135, y 570..600. top 3, bottom 6, left 3.
	want := lines(
		"1 0 0 rg", "75 600 m", "135 600 l", "135 597 l", "78 597 l", "h", "f",
		"0 0 1 rg", "135 570 m", "75 570 l", "78 576 l", "135 576 l", "h", "f",
		"0 0 0 rg", "75 570 m", "75 600 l", "78 597 l", "78 576 l", "h", "f")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	e.finish(t)
}

func TestBorderNothingToDraw(t *testing.T) {
	e := newEnv(t)
	r := display.Rect{X: 100, Y: 200, W: 80, H: 40}
	for name, it := range map[string]display.Border{
		"no sides":          {Rect: r},
		"transparent sides": {Rect: r, Sides: [4]display.BorderSide{{Width: 4}, {Width: 4}, {Width: 4}, {Width: 4}}},
		"empty box":         {Rect: display.Rect{X: 1, Y: 1}, Sides: [4]display.BorderSide{{Width: 4, Color: red}}},
		"off page":          {Rect: display.Rect{X: 5000, Y: 5000, W: 10, H: 10}, Sides: [4]display.BorderSide{{Width: 1, Color: red}}},
	} {
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, display.List{it}); err != nil || len(c.Bytes()) != 0 {
			t.Errorf("%s: err %v, %q", name, err, c.Bytes())
		}
	}
}

func TestLinks(t *testing.T) {
	e := newEnv(t)
	_, links := e.page(t, a4, display.List{
		display.Link{Rect: display.Rect{X: 100, Y: 200, W: 80, H: 20}, URI: "https://example.test/a"},
		display.Link{Rect: display.Rect{X: 5000, Y: 5000, W: 80, H: 20}, URI: "https://off.test/"},
		display.Link{Rect: display.Rect{X: 1, Y: 1, W: 8, H: 8}},
	})
	want := []pdf.Link{{Rect: [4]float64{75, 585, 135, 600}, URI: "https://example.test/a"}}
	if len(links) != 1 || links[0] != want[0] {
		t.Fatalf("links = %+v, want %+v", links, want)
	}
	doc := e.finish(t)
	if len(doc.Pages[0].Links) != 1 || doc.Pages[0].Links[0].URI != "https://example.test/a" {
		t.Fatalf("links in the PDF: %+v", doc.Pages[0].Links)
	}
}

func TestClipsAreBalanced(t *testing.T) {
	e := newEnv(t)
	r := display.Rect{X: 10, Y: 10, W: 100, H: 100}
	got, _ := e.page(t, a4, display.List{
		display.PushClip{Rect: r},
		display.FillRect{Rect: r, Color: red},
		display.PushClip{Rect: r, Radii: display.Radii{{H: 5, V: 5}, {H: 5, V: 5}, {H: 5, V: 5}, {H: 5, V: 5}}},
		display.FillRect{Rect: r, Color: blue},
		display.PopClip{},
		display.PopClip{},
		display.FillRect{Rect: r, Color: black},
	})
	if saves := balanced(t, got); saves != 2 || strings.Count(got, "W n") != 2 {
		t.Fatalf("saves %d:\n%s", saves, got)
	}
	e.finish(t)
}

func TestClipNestingIsBounded(t *testing.T) {
	e := newEnv(t)
	var l display.List
	const depth = MaxClipDepth + 30
	for range depth {
		l = append(l, display.PushClip{Rect: display.Rect{X: 0, Y: 0, W: 500, H: 500}})
	}
	l = append(l, display.FillRect{Rect: display.Rect{X: 1, Y: 1, W: 10, H: 10}, Color: red})
	for range depth {
		l = append(l, display.PopClip{})
	}
	got, _ := e.page(t, a4, l)
	if saves := balanced(t, got); saves != MaxClipDepth {
		t.Fatalf("%d saves, want %d", saves, MaxClipDepth)
	}
	ws, _ := e.b.Warnings()
	if len(ws) != 1 || ws[0].Code != budget.WarnClamped || ws[0].Count != 30 {
		t.Fatalf("warnings = %+v", ws)
	}
	e.finish(t)
}

func TestClipDepthAtTheLimit(t *testing.T) {
	for _, tt := range []struct {
		depth int
		warn  bool
	}{{MaxClipDepth - 1, false}, {MaxClipDepth, false}, {MaxClipDepth + 1, true}} {
		e := newEnv(t)
		var l display.List
		for range tt.depth {
			l = append(l, display.PushClip{Rect: display.Rect{W: 10, H: 10}})
		}
		for range tt.depth {
			l = append(l, display.PopClip{})
		}
		got, _ := e.page(t, a4, l)
		ws, _ := e.b.Warnings()
		if saves := balanced(t, got); saves != min(tt.depth, MaxClipDepth) || (len(ws) != 0) != tt.warn {
			t.Errorf("depth %d: %d saves, warnings %v", tt.depth, saves, ws)
		}
	}
}

func TestUnbalancedClips(t *testing.T) {
	e := newEnv(t)
	r := display.Rect{W: 10, H: 10}
	for name, l := range map[string]display.List{
		"pop without push": {display.PopClip{}},
		"push left open":   {display.PushClip{Rect: r}, display.FillRect{Rect: r, Color: red}},
		"two pops":         {display.PushClip{Rect: r}, display.PopClip{}, display.PopClip{}},
	} {
		c := &pdf.Content{}
		_, err := e.painter.Page(c, a4, l)
		if !errors.Is(err, ErrUnbalancedClip) {
			t.Errorf("%s: err = %v", name, err)
		}
		balanced(t, string(c.Bytes()))
	}
}

func TestContentCapIsReported(t *testing.T) {
	e := newEnv(t)
	item := display.FillRect{Rect: display.Rect{X: 1, Y: 1, W: 5, H: 5}, Color: red}
	// Each item writes "1 0 0 rg\n0.75 749.25 3.75 3.75 re\nf\n" (about 35 bytes).
	n := pdf.MaxContentBytes/30 + 1000
	var it display.Item = item
	l := make(display.List, n)
	for i := range l {
		l[i] = it
	}
	c := &pdf.Content{}
	_, err := e.painter.Page(c, a4, l)
	if !errors.Is(err, pdf.ErrContentSize) {
		t.Fatalf("err = %v", err)
	}
	if len(c.Bytes()) > pdf.MaxContentBytes {
		t.Fatalf("content of %d bytes", len(c.Bytes()))
	}
}

func TestZeroSizeBoxesCostNothing(t *testing.T) {
	// 200 000 zero-size bordered boxes were 50 MB of operators.
	e := newEnv(t)
	l := make(display.List, 200_000)
	var it display.Item = display.Border{Rect: display.Rect{X: 5, Y: 5}, Sides: [4]display.BorderSide{{Width: 1, Color: red}}}
	for i := range l {
		l[i] = it
	}
	c := &pdf.Content{}
	if _, err := e.painter.Page(c, a4, l); err != nil || len(c.Bytes()) != 0 {
		t.Fatalf("err %v, %d bytes", err, len(c.Bytes()))
	}
}
