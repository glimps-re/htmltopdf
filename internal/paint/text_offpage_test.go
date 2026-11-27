package paint

import (
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/text"
)

func TestTextOffPageSides(t *testing.T) {
	tests := []struct {
		name string
		x, y float64
		want string
	}{
		{"left", -500, 100, "left"},
		{"right", 5000, 100, "right"},
		{"above", 100, -500, "above"},
		{"below", 100, 5000, "below"},
		{"right and below is right", 5000, 5000, "right"},
		{"left and above is left", -500, -500, "left"},
		{"above and right is right", 5000, -500, "right"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "abc", 16)
			run.X, run.Baseline = tc.x, tc.y
			content, _ := e.page(t, a4, display.List{&run})
			if content != "" {
				t.Fatalf("content drawn:\n%s", content)
			}
			ws, _ := e.b.Warnings()
			if want := []budget.Warning{{Code: budget.WarnTextOffPage, Detail: tc.want, Count: 1}}; !slices.Equal(ws, want) {
				t.Fatalf("warnings %+v, want %+v", ws, want)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageInkBoundaries(t *testing.T) {
	e := newEnv(t)
	probe := e.run(t, "H", 100)
	ink := e.painter.inkOf(probe.Face)
	w := probe.Width()
	tests := []struct {
		name     string
		x, y     float64
		wantSide string
		drawn    bool
	}{
		{"right edge on the left of the page", -w, 500, "left", true},
		{"just left of the page", -w - 1, 500, "left", false},
		{"left edge on the right of the page", a4.Width, 500, "right", true},
		{"just right of the page", a4.Width + 1, 500, "right", false},
		{"ink bottom just on the top of the page", 100, 1 - ink.descent*100, "above", true},
		{"ink off above, run box on the page", 100, -ink.descent*100 - 1, "above", true},
		{"ink off above and run box off", 100, -500, "above", false},
		{"ink top just on the bottom of the page", 100, a4.Height - 1 + ink.ascent*100, "below", true},
		{"ink off below, run box on the page", 100, a4.Height + ink.ascent*100 + 1, "below", true},
		{"ink off below and run box off", 100, a4.Height + 500, "below", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "H", 100)
			run.X, run.Baseline = tc.x, tc.y
			content, _ := e.page(t, a4, display.List{&run})
			if drawn := strings.Contains(content, " Tf"); drawn != tc.drawn {
				t.Fatalf("drawn %v, want %v:\n%s", drawn, tc.drawn, content)
			}
			ws, _ := e.b.Warnings()
			var want []budget.Warning
			if tc.wantSide != "" {
				want = []budget.Warning{{Code: budget.WarnTextOffPage, Detail: tc.wantSide, Count: 1}}
			}
			if !slices.Equal(ws, want) {
				t.Fatalf("warnings %+v, want %+v", ws, want)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageNotWarned(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*display.Text)
		transform bool
	}{
		{"on the page", func(*display.Text) {}, false},
		{"transparent", func(r *display.Text) { r.X = 5000; r.Color.A = 0 }, false},
		{"size 0", func(r *display.Text) { r.X = 5000; r.Size = 0 }, false},
		{"transformed", func(r *display.Text) { r.X = 5000 }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "abc", 16)
			run.X, run.Baseline = 100, 200
			tc.mutate(&run)
			l := display.List{&run}
			if tc.transform {
				l = display.List{display.PushTransform{M: [6]float64{1, 0, 0, 1, 0, 0}}, &run, display.PopTransform{}}
			}
			e.page(t, a4, l)
			if ws, _ := e.b.Warnings(); len(ws) != 0 {
				t.Fatalf("warnings %+v", ws)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageCountsEveryRunOnEveryPage(t *testing.T) {
	e := newEnv(t)
	for range 3 {
		var l display.List
		for range 4 {
			run := e.run(t, "abc", 16)
			run.X, run.Baseline = 5000, 100
			l = append(l, &run)
		}
		e.page(t, a4, l)
	}
	ws, _ := e.b.Warnings()
	if want := []budget.Warning{{Code: budget.WarnTextOffPage, Detail: "right", Count: 12}}; !slices.Equal(ws, want) {
		t.Fatalf("warnings %+v, want %+v", ws, want)
	}
}

func TestOnlyTextWarnsOffPage(t *testing.T) {
	e := newEnv(t)
	far := display.Rect{X: 5000, Y: 5000, W: 10, H: 10}
	e.page(t, a4, display.List{
		display.FillRect{Rect: far, Color: red},
		display.Border{Rect: far, Sides: [4]display.BorderSide{{Width: 2, Color: black}, {Width: 2, Color: black}, {Width: 2, Color: black}, {Width: 2, Color: black}}},
	})
	if ws, _ := e.b.Warnings(); len(ws) != 0 {
		t.Fatalf("warnings %+v", ws)
	}
}

func TestTextOffPagePartly(t *testing.T) {
	probe := newEnv(t)
	ref := probe.run(t, "Hello", 40)
	ink := probe.painter.inkOf(ref.Face)
	w, size := ref.Width(), ref.Size
	tol := textEdgeTolerance * size
	tests := []struct {
		name string
		x, y float64
		want string
	}{
		{"right, clearly", a4.Width - w/2, 500, "right"},
		{"right, just over", a4.Width - w + 1.1*tol, 500, "right"},
		{"right, just under", a4.Width - w + 0.9*tol, 500, ""},
		{"left, clearly", -w / 2, 500, "left"},
		{"left, just over", -1.1 * tol, 500, "left"},
		{"left, just under", -0.9 * tol, 500, ""},
		{"above, clearly", 100, ink.cap*size - size, "above"},
		{"above, just over", 100, ink.cap*size - 1.1*tol, "above"},
		{"above, just under", 100, ink.cap*size - 0.9*tol, ""},
		{"below, clearly", 100, a4.Height - ink.descent*size + size, "below"},
		{"below, just over", 100, a4.Height - ink.descent*size + 1.1*tol, "below"},
		{"below, just under", 100, a4.Height - ink.descent*size + 0.9*tol, ""},
		{"right and below is right", a4.Width - w/2, a4.Height, "right"},
		{"left and above is left", -w / 2, 0, "left"},
		{"on the page", 100, 500, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "Hello", 40)
			run.X, run.Baseline = tc.x, tc.y
			content, _ := e.page(t, a4, display.List{&run})
			if !strings.Contains(content, " Tf") {
				t.Fatalf("a run partly on the page must be drawn:\n%s", content)
			}
			ws, _ := e.b.Warnings()
			var want []budget.Warning
			if tc.want != "" {
				want = []budget.Warning{{Code: budget.WarnTextOffPage, Detail: tc.want, Count: 1}}
			}
			if !slices.Equal(ws, want) {
				t.Fatalf("warnings %+v, want %+v", ws, want)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageToleranceScalesWithSize(t *testing.T) {
	for _, size := range []float64{8, 80} {
		e := newEnv(t)
		run := e.run(t, "Hello", size)
		run.X, run.Baseline = a4.Width-run.Width()+0.5*textEdgeTolerance*size, 500
		e.page(t, a4, display.List{&run})
		if ws, _ := e.b.Warnings(); len(ws) != 0 {
			t.Fatalf("size %v: warnings %+v", size, ws)
		}
		e.finish(t)
	}
}

func TestTextOffPageLongNowrapRun(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, strings.Repeat("word ", 400), 16)
	run.X, run.Baseline = 50, 200
	if run.Width() < 4*a4.Width {
		t.Fatalf("fixture too short: %v", run.Width())
	}
	content, _ := e.page(t, a4, display.List{&run})
	if !strings.Contains(content, " Tf") {
		t.Fatalf("run not drawn:\n%s", content)
	}
	ws, _ := e.b.Warnings()
	if want := []budget.Warning{{Code: budget.WarnTextOffPage, Detail: "right", Count: 1}}; !slices.Equal(ws, want) {
		t.Fatalf("warnings %+v, want %+v", ws, want)
	}
	e.finish(t)
}

func TestTextOffPagePartlyUnderTransformIsSilent(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "Hello", 40)
	run.X, run.Baseline = a4.Width-run.Width()/2, 500
	e.page(t, a4, display.List{display.PushTransform{M: [6]float64{1, 0, 0, 1, 0, 0}}, &run, display.PopTransform{}})
	if ws, _ := e.b.Warnings(); len(ws) != 0 {
		t.Fatalf("warnings %+v", ws)
	}
	e.finish(t)
}

func spaced(r display.Text, px float64) display.Text {
	r.Glyphs = append([]text.Glyph(nil), r.Glyphs...)
	for i := range r.Glyphs {
		r.Glyphs[i].Advance += float32(px)
	}
	return r
}

func TestTextOffPageNegativeLetterSpacing(t *testing.T) {
	e := newEnv(t)
	probe := spaced(e.run(t, "Hello", 40), -12)
	last := probe.Glyphs[len(probe.Glyphs)-1]
	reach := probe.Width() - float64(last.Advance) + float64(e.shaper.GlyphAdvance(probe.Face, last.ID, 40))
	tol := textEdgeTolerance * 40
	tests := []struct {
		name string
		x    float64
		want string
	}{
		{"last glyph reaches past the edge", a4.Width + 3*tol - reach, "right"},
		{"last glyph within the tolerance", a4.Width + 0.5*tol - reach, ""},
		{"inverted box in the middle of the page", 400, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := spaced(e.run(t, "Hello", 40), -12)
			if tc.name == "inverted box in the middle of the page" {
				run = spaced(e.run(t, "AAAAAAAAAA", 20), -20)
				if run.Width() >= 0 {
					t.Fatalf("box not inverted: %v", run.Width())
				}
			}
			run.X, run.Baseline = tc.x, 500
			e.page(t, a4, display.List{&run})
			ws, _ := e.b.Warnings()
			var want []budget.Warning
			if tc.want != "" {
				want = []budget.Warning{{Code: budget.WarnTextOffPage, Detail: tc.want, Count: 1}}
			}
			if !slices.Equal(ws, want) {
				t.Fatalf("warnings %+v, want %+v", ws, want)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageCapitalsDecideTheTop(t *testing.T) {
	probe := newEnv(t)
	ref := probe.run(t, "TITLE", 96)
	ink := probe.painter.inkOf(ref.Face)
	if ink.cap >= ink.ascent {
		t.Fatalf("cap %v not under ascent %v", ink.cap, ink.ascent)
	}
	tests := []struct {
		name     string
		baseline float64
		want     string
	}{
		{"capitals on the page, ascent above it", ink.cap*96 + 1, ""},
		{"capitals cut", ink.cap*96 - 20, "above"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "TITLE", 96)
			run.X, run.Baseline = 100, tc.baseline
			e.page(t, a4, display.List{&run})
			ws, _ := e.b.Warnings()
			var want []budget.Warning
			if tc.want != "" {
				want = []budget.Warning{{Code: budget.WarnTextOffPage, Detail: tc.want, Count: 1}}
			}
			if !slices.Equal(ws, want) {
				t.Fatalf("warnings %+v, want %+v", ws, want)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageToleranceIsCapped(t *testing.T) {
	tests := []struct {
		name string
		size float64
		x    float64
		want string
	}{
		{"400px glyph cut by 30pt", 400, -40, "left"},
		{"400px glyph under the cap", 400, -4, ""},
		{"400px glyph just over the cap", 400, -textEdgeCap/PxToPt - 1, "left"},
		{"12px run under a tenth of its size", 12, -0.9 * textEdgeTolerance * 12, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "l", tc.size)
			run.X, run.Baseline = tc.x, 500
			e.page(t, a4, display.List{&run})
			ws, _ := e.b.Warnings()
			var want []budget.Warning
			if tc.want != "" {
				want = []budget.Warning{{Code: budget.WarnTextOffPage, Detail: tc.want, Count: 1}}
			}
			if !slices.Equal(ws, want) {
				t.Fatalf("warnings %+v, want %+v", ws, want)
			}
			e.finish(t)
		})
	}
}

func TestTextOffPageEntirelyBeyondBeatsCrossed(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "Hello", 40)
	ink := e.painter.inkOf(run.Face)
	run.X, run.Baseline = a4.Width-run.Width()/2, a4.Height+ink.ascent*40+50
	e.page(t, a4, display.List{&run})
	ws, _ := e.b.Warnings()
	if want := []budget.Warning{{Code: budget.WarnTextOffPage, Detail: "below", Count: 1}}; !slices.Equal(ws, want) {
		t.Fatalf("warnings %+v, want %+v", ws, want)
	}
	e.finish(t)
}

func TestTextOffPageInvertedBoxIsDrawnOnItsInk(t *testing.T) {
	e := newEnv(t)
	run := spaced(e.run(t, "AAAAAAAAAA", 20), -20)
	run.X, run.Baseline = 400, 500
	content, _ := e.page(t, a4, display.List{&run})
	if !strings.Contains(content, " Tf") {
		t.Fatalf("run with an inverted advance box not drawn:\n%s", content)
	}
	if ws, _ := e.b.Warnings(); len(ws) != 0 {
		t.Fatalf("warnings %+v", ws)
	}
	far := spaced(e.run(t, "AAAAAAAAAA", 20), -20)
	far.X, far.Baseline = a4.Width+500, 500
	content, _ = e.page(t, a4, display.List{&far})
	if strings.Contains(content, " Tf") {
		t.Fatalf("run off the page drawn:\n%s", content)
	}
	ws, _ := e.b.Warnings()
	if want := []budget.Warning{{Code: budget.WarnTextOffPage, Detail: "right", Count: 1}}; !slices.Equal(ws, want) {
		t.Fatalf("warnings %+v, want %+v", ws, want)
	}
}
