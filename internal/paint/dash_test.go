package paint

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
)

func TestDashPattern(t *testing.T) {
	const eps = 1e-3
	for _, tt := range []struct {
		name     string
		style    display.LineStyle
		w, l     float64
		closed   bool
		on, off  float64
		wantKind dashKind
	}{
		{"dashed 1 px", display.LineDashed, 1, 122, false, 3, 47.0 / 24, dashButt},
		{"dashed 2 px", display.LineDashed, 2, 124, false, 6, 3.8333, dashButt},
		{"dashed 3 px takes the smaller gap on a tie", display.LineDashed, 3, 46, false, 6, 2, dashButt},
		{"dashed 3 px rounds the gap up", display.LineDashed, 3, 126, false, 6, 42.0 / 13, dashButt},
		{"dashed 4 px", display.LineDashed, 4, 128, false, 8, 4, dashButt},
		{"dashed closed path", display.LineDashed, 2, 315.98, true, 6, 3.874, dashButt},
		{"dotted 1 px is square", display.LineDotted, 1, 122, false, 1, 60.0 / 61, dashButt},
		{"dotted 3 px is square", display.LineDotted, 3, 126, false, 3, 60.0 / 21, dashButt},
		{"dotted 4 px is round", display.LineDotted, 4, 128, false, 0, 7.75, dashRound},
		{"dotted 6 px is round", display.LineDotted, 6, 132, false, 0, 11.4545, dashRound},
		{"two dashes and a gap", display.LineDashed, 2, 15, false, 6, 3, dashButt},
		{"room for two dashes only", display.LineDashed, 2, 13, false, 6, 1, dashButt},
		{"no room for two dashes", display.LineDashed, 2, 12, false, 0, 0, dashSolid},
		{"solid", display.LineSolid, 2, 100, false, 0, 0, dashSolid},
		{"no width", display.LineDashed, 0, 100, false, 0, 0, dashSolid},
		{"no length", display.LineDashed, 1, 0, false, 0, 0, dashSolid},
		{"infinite length", display.LineDashed, 1, math.Inf(1), false, 0, 0, dashSolid},
		{"NaN length", display.LineDashed, 1, math.NaN(), false, 0, 0, dashSolid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			on, off, kind := dashPattern(tt.style, tt.w, tt.l, tt.closed)
			if kind != tt.wantKind || math.Abs(on-tt.on) > eps || math.Abs(off-tt.off) > eps {
				t.Errorf("got %.4f %.4f kind %d, want %.4f %.4f kind %d", on, off, kind, tt.on, tt.off, tt.wantKind)
			}
		})
	}
}

// A closed path that would take more than maxClosedDashes dashes is drawn solid,
// one that takes exactly that many is dashed.
func TestDashPatternBound(t *testing.T) {
	const w, dash, gap = 1.0, 3.0, 2.0
	for _, tt := range []struct {
		name string
		n    float64
		want dashKind
	}{
		{"just under", maxClosedDashes - 1, dashButt},
		{"at the bound", maxClosedDashes, dashButt},
		{"over", maxClosedDashes + 1, dashSolid},
	} {
		if _, _, kind := dashPattern(display.LineDashed, w, tt.n*(dash+gap), true); kind != tt.want {
			t.Errorf("%s: kind %d, want %d", tt.name, kind, tt.want)
		}
	}
}

func dashedSide() display.BorderSide {
	return display.BorderSide{Width: 2, Color: red, Style: display.LineDashed}
}

func TestDashedBorderIsOneStrokePerSide(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 124, H: 44}, Sides: [4]display.BorderSide{d, d, d, d},
	}})
	if n := strings.Count(got, "\nS\n"); n != 4 {
		t.Errorf("%d strokes in\n%s", n, got)
	}
	// Top: 124 px, dashes of 6 px and a gap of 3.8333 px, in pt.
	for _, want := range []string{"0 J", "1.5 w", "[4.5 2.875 ] 0 d", "75 599.25 m", "168 599.25 l"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	balanced(t, got)
	e.finish(t)
}

func TestDottedBorderUsesRoundCapsFromFourPixels(t *testing.T) {
	e := newEnv(t)
	d := display.BorderSide{Width: 4, Color: red, Style: display.LineDotted}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 128, H: 48}, Sides: [4]display.BorderSide{d, d, d, d},
	}})
	for _, want := range []string{"1 J", "[0 5.8125 ] 0 d"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	e.finish(t)
}

func TestMixedBorderKeepsSolidSidesAsPolygons(t *testing.T) {
	e := newEnv(t)
	s := display.BorderSide{Width: 4, Color: red}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect:  display.Rect{X: 100, Y: 200, W: 80, H: 40},
		Sides: [4]display.BorderSide{s, dashedSide(), s, s},
	}})
	if n, f := strings.Count(got, "\nS\n"), strings.Count(got, "\nf\n"); n != 1 || f != 3 {
		t.Errorf("%d strokes, %d fills in\n%s", n, f, got)
	}
	balanced(t, got)
	e.finish(t)
}

// A dashed side next to a side of another color is clipped to its miter, the
// others are not.
func TestDashedSideIsClippedWhereTheColorChanges(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	b := d
	b.Color = blue
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 124, H: 44}, Sides: [4]display.BorderSide{d, d, d, b},
	}})
	if n := strings.Count(got, "W n"); n != 3 {
		t.Errorf("%d clips, want 3 (the left side and its two neighbours)\n%s", n, got)
	}
	e.finish(t)
}

// A side too short for two dashes is drawn solid.
func TestShortDashedSideIsSolid(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 11, H: 40}, Sides: [4]display.BorderSide{d, {}, d, {}},
	}})
	if strings.Contains(got, " d\n") || strings.Contains(got, "\nS\n") {
		t.Errorf("dashes on a side of 11 px:\n%s", got)
	}
	e.finish(t)
}

// A dashed side of 1e7 px or more costs the same content as a short one, and the
// stroked segment is no longer than the page (and a few periods).
func TestDashedHugeSideIsOneClippedStroke(t *testing.T) {
	var sizes []int
	for _, w := range []float64{1e4, 1e7, 1e30, math.MaxFloat64} {
		e := newEnv(t)
		d := dashedSide()
		got, _ := e.page(t, a4, display.List{display.Border{
			Rect: display.Rect{X: -10, Y: 100, W: w, H: 40}, Sides: [4]display.BorderSide{d, d, d, d},
		}})
		sizes = append(sizes, len(got))
		for _, ln := range strings.Split(got, "\n") {
			f := strings.Fields(ln)
			if len(f) == 3 && f[2] == "l" {
				if x, err := strconv.ParseFloat(f[0], 64); err != nil || x > 600+60 {
					t.Errorf("width %g: a point at x = %v (%v)", w, f[0], err)
				}
			}
		}
		if strings.Count(got, "\nS\n") > 4 {
			t.Errorf("width %g: %d strokes", w, strings.Count(got, "\nS\n"))
		}
		balanced(t, got)
		e.finish(t)
	}
	for _, n := range sizes {
		if n > 1200 {
			t.Errorf("content sizes %v", sizes)
		}
	}
}

// Ten thousand dashed boxes cost a fixed content size each.
func TestManyDashedBoxes(t *testing.T) {
	size := func(n int) int {
		e := newEnv(t)
		d := dashedSide()
		var l display.List
		for i := 0; i < n; i++ {
			l = append(l, display.Border{Rect: display.Rect{X: float64(i % 600), Y: float64(i % 900), W: 60, H: 30}, Sides: [4]display.BorderSide{d, d, d, d}})
		}
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, l); err != nil {
			t.Fatal(err)
		}
		return len(c.Bytes())
	}
	a, b := size(10000), size(20000)
	if a > pdf.MaxContentBytes {
		t.Errorf("%d bytes for 10000 boxes", a)
	}
	if ratio := float64(b) / float64(a); ratio < 1.9 || ratio > 2.1 {
		t.Errorf("content grows by %.2f for twice the boxes", ratio)
	}
}

func TestRoundedDashedBorder(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	radii := display.Radii{{H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 124, H: 44}, Radii: radii, Sides: [4]display.BorderSide{d, d, d, d},
	}})
	if n := strings.Count(got, "\nS\n"); n != 1 {
		t.Errorf("%d strokes, want 1 closed path\n%s", n, got)
	}
	if n := strings.Count(got, " c\n"); n != 4 {
		t.Errorf("%d curves, want 4", n)
	}
	if !strings.Contains(got, "h\nS") {
		t.Errorf("the path is not closed:\n%s", got)
	}
	balanced(t, got)
	e.finish(t)
}

// Sides that differ in width or style around a rounded corner are painted as the
// solid ring.
func TestRoundedBorderWithMixedSidesStaysSolid(t *testing.T) {
	e := newEnv(t)
	d, s := dashedSide(), display.BorderSide{Width: 2, Color: red}
	radii := display.Radii{{H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}, {H: 8, V: 8}}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 124, H: 44}, Radii: radii, Sides: [4]display.BorderSide{d, s, d, s},
	}})
	if strings.Contains(got, "\nS\n") {
		t.Errorf("a stroke in\n%s", got)
	}
	e.finish(t)
}

// A collapsed table edge shared with the next cell is one stroke centered on the
// edge of the cell, moved by Lead and Trail.
func TestSharedDashedEdge(t *testing.T) {
	e := newEnv(t)
	d := display.BorderSide{Width: 1, Color: red, Style: display.LineDashed, PatternWidth: 2, Lead: 1, Trail: 2}
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 80, H: 40}, Sides: [4]display.BorderSide{{}, {}, d, {}},
	}})
	// Bottom edge at y = 240 px (570 pt), from x = 101 px to 182 px.
	for _, want := range []string{"1.5 w", "75.75 570 m", "136.5 570 l"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	e.finish(t)
}

func BenchmarkDashedBorders(b *testing.B) {
	e := newEnv(b)
	d := dashedSide()
	radii := display.Radii{{H: 4, V: 4}, {H: 4, V: 4}, {H: 4, V: 4}, {H: 4, V: 4}}
	var l display.List
	for i := 0; i < 1000; i++ {
		it := display.Border{Rect: display.Rect{X: float64(i % 700), Y: float64(i % 900), W: 60, H: 30}, Sides: [4]display.BorderSide{d, d, d, d}}
		if i%2 == 0 {
			it.Radii = radii
		}
		l = append(l, it)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, l); err != nil {
			b.Fatal(err)
		}
	}
}

// A pattern that the content stream would write as zeros is drawn solid.
func TestTinyDashPatternIsSolid(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	d.Width = 1e-3
	g := a4
	g.Scale = 1e-3
	got, _ := e.page(t, g, display.List{display.Border{
		Rect: display.Rect{X: 100, Y: 200, W: 1e5, H: 1e5}, Sides: [4]display.BorderSide{d, d, d, d},
	}})
	if strings.Contains(got, "[0 0 ]") {
		t.Errorf("a zero pattern in\n%s", got)
	}
	e.finish(t)
}

// A long dashed side is stroked only where the page is, at a whole number of
// periods from its start so that the dashes stay where they are.
func TestDashedLineIsClippedToThePage(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	got, _ := e.page(t, a4, display.List{display.Border{
		Rect: display.Rect{X: -1e5, Y: 100, W: 2e5, H: 40}, Sides: [4]display.BorderSide{d, {}, d, {}},
	}})
	var xs []float64
	for _, ln := range strings.Split(got, "\n") {
		f := strings.Fields(ln)
		if len(f) == 3 && (f[2] == "m" || f[2] == "l") {
			v, err := strconv.ParseFloat(f[0], 64)
			if err != nil {
				t.Fatal(err)
			}
			xs = append(xs, v)
		}
	}
	if len(xs) != 4 {
		t.Fatalf("%d points in\n%s", len(xs), got)
	}
	period := 4.5 + 2.875 // the pattern of 6 px dashes on 2e5 px, in pt (approximately)
	for _, x := range xs {
		if x < -2*period-30 || x > 600+2*period+30 {
			t.Errorf("a point at x = %v is far outside the page", x)
		}
	}
	e.finish(t)
}

// A dashed side that is entirely off the page draws nothing.
func TestDashedLineOffThePageIsNotStroked(t *testing.T) {
	e := newEnv(t)
	d := dashedSide()
	c := &pdf.Content{}
	if _, err := e.painter.Page(c, a4, display.List{display.Border{
		Rect: display.Rect{X: 5000, Y: 100, W: 2e5, H: 40}, Sides: [4]display.BorderSide{d, {}, d, {}},
	}}); err != nil || strings.Contains(string(c.Bytes()), "S") {
		t.Errorf("%v %q", err, c.Bytes())
	}
}

// Under a transform the stroke cannot be clipped to the page: a side with more than
// maxClosedDashes dashes is drawn solid, one with fewer is dashed.
func TestDashedSideUnderATransformIsBounded(t *testing.T) {
	for _, tt := range []struct {
		w       float64
		strokes int // the vertical sides of 40 px are dashed in any case
	}{{1000, 4}, {1e7, 2}, {1e30, 2}} {
		e := newEnv(t)
		d := dashedSide()
		got, _ := e.page(t, a4, display.List{
			display.PushTransform{M: [6]float64{0.98, 0.17, -0.17, 0.98, 0, 0}},
			display.Border{Rect: display.Rect{X: 10, Y: 100, W: tt.w, H: 40}, Sides: [4]display.BorderSide{d, d, d, d}},
			display.PopTransform{},
		})
		if n := strings.Count(got, "\nS\n"); n != tt.strokes {
			t.Errorf("width %g: %d strokes, want %d", tt.w, n, tt.strokes)
		}
		if len(got) > 2000 {
			t.Errorf("width %g: %d bytes", tt.w, len(got))
		}
		e.finish(t)
	}
}
