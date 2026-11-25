package layout

import (
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

func bordersOf(items display.List) (out []display.Border) {
	for _, it := range items {
		if b, ok := it.(display.Border); ok {
			out = append(out, b)
		}
	}
	return out
}

func TestBorderStyleReachesTheDisplayList(t *testing.T) {
	html := `<div style="width:100px;height:20px;border:2px solid red;border-bottom:1px dotted blue;border-left:3px dashed green"></div>`
	bs := bordersOf(newRun(t, html, opts{}).all(t)[0].Items)
	if len(bs) != 1 {
		t.Fatalf("%d borders", len(bs))
	}
	want := [4]display.LineStyle{display.LineSolid, display.LineSolid, display.LineDotted, display.LineDashed}
	for i, s := range bs[0].Sides {
		if s.Style != want[i] {
			t.Errorf("side %d: style %d, want %d", i, s.Style, want[i])
		}
	}
}

// A collapsed dashed edge is drawn whole by the cell before it (or above), which
// runs to the middle of the border at each end; the other cell draws the edges
// it shares with its neighbours no more.
func TestCollapsedDashedEdges(t *testing.T) {
	html := `<table style="border-collapse:collapse"><tr><td style="border:2px dashed red">a</td><td style="border:2px dashed red">b</td></tr></table>`
	bs := bordersOf(newRun(t, html, opts{}).all(t)[0].Items)
	if len(bs) != 2 {
		t.Fatalf("%d borders", len(bs))
	}
	first, second := bs[0].Sides, bs[1].Sides
	if s := first[style.Right]; s.PatternWidth != 2 || s.Width != 1 || s.Style != display.LineDashed {
		t.Errorf("right side of the first cell: %+v", s)
	}
	if first[style.Top].Width != 2 || first[style.Top].Trail != 1 || first[style.Top].Lead != 0 {
		t.Errorf("top side of the first cell: %+v", first[style.Top])
	}
	if second[style.Left].Width != 0 {
		t.Errorf("the second cell draws the edge it shares: %+v", second[style.Left])
	}
	if second[style.Top].Lead != 1 || second[style.Top].Trail != 0 {
		t.Errorf("top side of the second cell: %+v", second[style.Top])
	}
}

// A solid collapsed edge is still drawn by half in each cell.
func TestCollapsedSolidEdgesAreUnchanged(t *testing.T) {
	html := `<table style="border-collapse:collapse"><tr><td style="border:2px solid red">a</td><td style="border:2px solid red">b</td></tr></table>`
	bs := bordersOf(newRun(t, html, opts{}).all(t)[0].Items)
	for i, b := range bs {
		for s, side := range b.Sides {
			if side.Lead != 0 || side.Trail != 0 || side.PatternWidth != 0 {
				t.Errorf("cell %d side %d: %+v", i, s, side)
			}
		}
	}
	if bs[0].Sides[style.Right].Width != 1 || bs[1].Sides[style.Left].Width != 1 {
		t.Errorf("%+v %+v", bs[0].Sides[style.Right], bs[1].Sides[style.Left])
	}
}

func TestDecorationThickness(t *testing.T) {
	for _, tt := range []struct {
		name, html string
		want       float64
	}{
		{"auto", `<u style="font-size:20px">x</u>`, 0},
		{"from-font", `<u style="text-decoration-thickness:from-font">x</u>`, 0},
		{"px", `<u style="text-decoration-thickness:3px">x</u>`, 3},
		{"at least 1 px", `<u style="text-decoration-thickness:0.2px">x</u>`, 1},
		{"rounded", `<u style="text-decoration-thickness:2.4px">x</u>`, 2},
		{"rounded up", `<u style="text-decoration-thickness:2.5px">x</u>`, 3},
		{"percentage of the font", `<u style="font-size:40px;text-decoration-thickness:10%">x</u>`, 4},
		{"percentage rounded", `<u style="font-size:18.67px;text-decoration-thickness:10%">x</u>`, 2},
		{"em", `<u style="font-size:20px;text-decoration-thickness:0.5em">x</u>`, 10},
		{"zero is 1 px, as Chrome draws it", `<u style="text-decoration-thickness:0">x</u>`, 1},
		{"inherited by the children of the decorated box", `<u style="text-decoration-thickness:3px"><span>x</span></u>`, 3},
		{"a nested decoration has its own", `<u style="text-decoration-thickness:3px"><span style="text-decoration:underline;text-decoration-thickness:5px">x</span></u>`, 5},
		{"a block that declares it", `<p style="text-decoration:underline;text-decoration-thickness:4px">x</p>`, 4},
		{"without a decoration", `<p style="text-decoration-thickness:4px">x</p>`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got []float64
			for _, tx := range texts(newRun(t, tt.html, opts{}).all(t)[0].Items) {
				if tx.Underline {
					got = append(got, tx.Thickness)
				}
			}
			if len(got) != 1 && tt.name != "without a decoration" || len(got) > 0 && math.Abs(got[0]-tt.want) > 1e-9 {
				t.Errorf("thickness %v, want %v", got, tt.want)
			}
		})
	}
}

// The decoration thickness of a huge value is clamped, not propagated.
func TestDecorationThicknessHostile(t *testing.T) {
	for _, v := range []string{"1e30px", "99999999999em", "1e9%", "calc(1px)", "-1px", "NaN"} {
		html := `<u style="text-decoration-thickness:` + v + `">x</u>`
		for _, tx := range texts(newRun(t, html, opts{}).all(t)[0].Items) {
			if tx.Thickness < 0 || math.IsNaN(tx.Thickness) || tx.Thickness > 1e7 {
				t.Errorf("%s: thickness %v", v, tx.Thickness)
			}
		}
	}
}

func TestMarginBoxBorderIsDashed(t *testing.T) {
	items, _ := marginItems(t, `@page{margin:40px;@top-center{content:"T";border-bottom:1px dashed red;padding:2px}}`, 1, 1)
	var found bool
	for _, b := range bordersOf(items) {
		found = found || b.Sides[style.Bottom].Style == display.LineDashed
	}
	if !found {
		t.Error("no dashed border in the margin box")
	}
}
