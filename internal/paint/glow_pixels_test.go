package paint

import (
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

// The glow of the cover of template 02: a radial gradient of the accent to
// transparent at 70 %, at opacity .55 (folded into the stop alphas), over the
// navy background, at Scale 0.9 (hence the 0.9 of the 180 mm box). The
// expected pixels are those of the headless Chrome render (tmp/spike/out/
// chrome09, report_02-executive_small, page 1, rasterized at 60 dpi,
// 497 x 702), sampled along a horizontal ray from the center of the glow. A
// rasterizer is not available to the tests, so the gradient is evaluated as a
// PDF viewer does: the cm of the content maps the unit circle, the stops are
// those handed to the writer, the alpha is the soft mask's.
func TestRadialGlowMatchesChrome(t *testing.T) {
	const (
		pageW, pageH = 595.92 / PxToPt, 841.92 / PxToPt
		dpi          = 60
		px           = 96.0 / dpi // CSS px per output pixel
		cx, cy       = 431.5 * px, 565 * px
		side         = 0.9 * 180 / 25.4 * 96
	)
	accent := css.Color{R: 0xea / 255.0, G: 0x5b / 255.0, B: 0x0c / 255.0}
	withAlpha := func(a float64) css.Color { c := accent; c.A = a; return c }
	glow := css.Gradient{Shape: css.GradientCircle, Stops: []css.ColorStop{{Color: withAlpha(0.55)}, {Color: withAlpha(0), Pos: 0.7}}}
	half := side / 2
	round := display.Radii{{H: half, V: half}, {H: half, V: half}, {H: half, V: half}, {H: half, V: half}}
	e := newEnv(t)
	g := Geometry{Width: pageW, Height: pageH, Area: display.Rect{W: pageW, H: pageH}, Scale: 1}
	content, _ := e.page(t, g, display.List{display.FillGradient{Rect: display.Rect{X: cx - half, Y: cy - half, W: side, H: side}, Radii: round, Gradient: glow}})
	m := matrixOf(t, content)
	stops := pdfStops(glow.Stops)
	navy := [3]float64{0x14, 0x21, 0x3d}
	at := func(outX float64) [3]int {
		t := math.Abs(outX*72/dpi-m[4]) / m[0] // the center is (m[4], m[5]), the radius m[0]
		return composite(stops, t, navy)
	}
	radius := m[0] * dpi / 72 // in output pixels
	samples := []struct {
		name string
		x    float64
		want [3]int // Chrome
		ours [3]int // pdftoppm of our PDF at 60 dpi (dev-only, recorded)
	}{
		{"center", cx / px, [3]int{136, 64, 33}, [3]int{137, 64, 34}},
		{"35 % of the radius", cx/px - 0.35*radius, [3]int{78, 48, 47}, [3]int{78, 48, 47}},
		{"70 % of the radius", cx/px - 0.70*radius, [3]int{20, 33, 61}, [3]int{20, 33, 61}},
	}
	for _, s := range samples {
		got := at(math.Round(s.x))
		for i := range got {
			if d := got[i] - s.want[i]; d < -4 || d > 4 {
				t.Errorf("%s at x=%.0f: got %v, Chrome has %v", s.name, s.x, got, s.want)
				break
			}
			if d := got[i] - s.ours[i]; d < -1 || d > 1 {
				t.Errorf("%s at x=%.0f: got %v, a rasterizer gives %v", s.name, s.x, got, s.ours)
				break
			}
		}
	}
}

// composite returns the color at unit radius t of the gradient stops drawn over bg
// (the colors of the stops interpolated, their alpha as a soft mask).
func composite(stops []pdf.Stop, t float64, bg [3]float64) [3]int {
	var c [4]float64
	switch {
	case t <= stops[0].Pos:
		c = [4]float64{stops[0].Color.R, stops[0].Color.G, stops[0].Color.B, stops[0].Color.A}
	case t >= stops[len(stops)-1].Pos:
		l := stops[len(stops)-1].Color
		c = [4]float64{l.R, l.G, l.B, l.A}
	default:
		for i := 1; i < len(stops); i++ {
			if t <= stops[i].Pos {
				a, b := stops[i-1], stops[i]
				f := (t - a.Pos) / (b.Pos - a.Pos)
				c = [4]float64{a.Color.R + f*(b.Color.R-a.Color.R), a.Color.G + f*(b.Color.G-a.Color.G), a.Color.B + f*(b.Color.B-a.Color.B), a.Color.A + f*(b.Color.A-a.Color.A)}
				break
			}
		}
	}
	var out [3]int
	for i, v := range [3]float64{c[0], c[1], c[2]} {
		out[i] = int(math.Round(bg[i]*(1-c[3]) + v*255*c[3]))
	}
	return out
}
