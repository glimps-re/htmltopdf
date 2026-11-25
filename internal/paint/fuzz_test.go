package paint

import (
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// FuzzPaint paints lists of arbitrary geometry: whatever the numbers, the
// content is balanced, the page is a valid PDF and nothing panics.
func FuzzPaint(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7}, 10.0, 20.0, 30.0, 40.0, 1.0)
	f.Add([]byte{3, 3, 0, 4, 4, 5}, -1e30, math.Inf(1), math.NaN(), 1e300, 0.5)
	f.Add([]byte{3, 0, 1, 2, 4, 3, 4}, 0.0, 0.0, 800.0, 1000.0, 2.0)
	f.Add([]byte{7, 9, 10, 2, 8, 9}, 10.0, 20.0, 300.0, 400.0, 1.0)
	f.Add([]byte{7, 7, 10, 3, 8, 8, 9, 4}, 1e30, math.NaN(), 5.0, math.Inf(-1), 0.5)
	f.Add([]byte{10, 21, 32, 9, 8}, 5.0, 5.0, 100.0, 100.0, 3.0)
	f.Add([]byte{11, 12, 27, 28, 43}, 10.0, 20.0, 300.0, 120.0, 1.0)
	f.Add([]byte{11, 12, 3, 12}, 1e30, math.NaN(), 1e9, math.Inf(-1), 0.5)
	f.Add([]byte{12, 12, 7, 11, 8, 23}, -5.0, 1e7, 1e7, 40.0, 2.0)
	f.Fuzz(func(t *testing.T, ops []byte, x, y, w, h, scale float64) {
		e := newEnv(t)
		rect := display.Rect{X: x, Y: y, W: w, H: h}
		radii := display.Radii{{H: w / 4, V: h / 4}, {H: 3, V: 9}, {H: -2, V: 5}, {H: math.NaN(), V: 1}}
		side := display.BorderSide{Width: math.Abs(w) / 10, Color: red}
		var l display.List
		for _, op := range ops {
			switch op % 13 {
			case 0:
				l = append(l, display.FillRect{Rect: rect, Radii: radii, Color: css.Color{R: x, G: y, B: w, A: h}})
			case 1:
				l = append(l, display.Border{Rect: rect, Sides: [4]display.BorderSide{side, {Width: 2, Color: blue}, side, {}}})
			case 2:
				l = append(l, display.Link{Rect: rect, URI: "https://f.test/"})
			case 3:
				l = append(l, display.PushClip{Rect: rect, Radii: radii})
			case 4:
				l = append(l, display.PopClip{})
			case 5:
				r := e.run(t, "ab", 12)
				r.X, r.Baseline, r.Underline = x, y, op&1 == 0
				l = append(l, &r)
			case 6:
				l = append(l, display.FillGradient{Rect: rect})
			case 7:
				l = append(l, display.PushTransform{M: [6]float64{w / 100, h / 100, -x / 100, y / 100, x, y}})
			case 8:
				l = append(l, display.PopTransform{})
			case 9:
				pt := func(dx float64) display.Point { return display.Point{X: x + dx, Y: y - dx} }
				l = append(l, display.Path{
					Segs:   []display.PathSeg{{Op: display.PathMoveTo, P: [3]display.Point{pt(0)}}, {Op: display.PathCubicTo, P: [3]display.Point{pt(5), pt(h), pt(w)}}, {Op: display.PathLineTo, P: [3]display.Point{pt(1)}}, {Op: display.PathClose}},
					Fill:   css.Color{R: x, G: y, B: w, A: h},
					Stroke: css.Color{B: 1, A: scale}, Width: w / 10, Join: op, Cap: op >> 1, EvenOdd: op&1 == 0,
				})
			case 11:
				d := display.BorderSide{Width: math.Abs(w)/10 + 1, Color: red, Style: display.LineStyle(op>>4%3 + 1), PatternWidth: h / 50, Lead: x / 20, Trail: y / 20}
				l = append(l, display.Border{Rect: rect, Sides: [4]display.BorderSide{d, side, d, {Width: 3, Color: blue, Style: display.LineDotted}}})
			case 12:
				d := display.BorderSide{Width: math.Abs(w)/10 + 1, Color: red, Style: display.LineStyle(op>>4%2 + 1)}
				l = append(l, display.Border{Rect: rect, Radii: radii, Sides: [4]display.BorderSide{d, d, d, d}})
			case 10:
				l = append(l, display.FillGradient{Rect: rect, Radii: radii, Gradient: css.Gradient{
					Shape: css.GradientShape(op%3 + 0), Angle: x,
					Stops: []css.ColorStop{{Color: css.Color{R: 1, A: scale}}, {Color: css.Color{}, Pos: 0.7}, {Color: css.Color{B: 1, A: 1}, Pos: 1}},
				}})
			}
		}
		g := Geometry{Width: 800, Height: 1000, Area: display.Rect{X: x / 10, Y: y / 10, W: 600, H: 800}, Scale: scale}
		c := &pdf.Content{}
		links, err := e.painter.Page(c, g, l)
		if err != nil {
			return // an unbalanced list or the content cap
		}
		balanced(t, string(c.Bytes()))
		if _, err := e.w.AddPage(600, 750, c, false, links); err != nil {
			t.Fatal(err)
		}
		if err := e.w.Close(pdf.Info{}); err != nil {
			t.Fatal(err)
		}
		if _, err := pdftest.Parse(e.buf.Bytes()); err != nil {
			t.Fatal(err)
		}
	})
}
