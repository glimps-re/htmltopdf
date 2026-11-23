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
	f.Fuzz(func(t *testing.T, ops []byte, x, y, w, h, scale float64) {
		e := newEnv(t)
		rect := display.Rect{X: x, Y: y, W: w, H: h}
		radii := display.Radii{{H: w / 4, V: h / 4}, {H: 3, V: 9}, {H: -2, V: 5}, {H: math.NaN(), V: 1}}
		side := display.BorderSide{Width: math.Abs(w) / 10, Color: red}
		var l display.List
		for _, op := range ops {
			switch op % 7 {
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
