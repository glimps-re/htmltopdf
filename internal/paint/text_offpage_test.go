package paint

import (
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
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
		{"right edge touches the left of the page", -w, 500, "", true},
		{"just left of the page", -w - 1, 500, "left", false},
		{"left edge touches the right of the page", a4.Width, 500, "", true},
		{"just right of the page", a4.Width + 1, 500, "right", false},
		{"ink bottom just on the top of the page", 100, 1 - ink.descent*100, "", true},
		{"ink off above, run box on the page", 100, -ink.descent*100 - 1, "above", true},
		{"ink off above and run box off", 100, -500, "above", false},
		{"ink top just on the bottom of the page", 100, a4.Height - 1 + ink.ascent*100, "", true},
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
		{"partly on the page", func(r *display.Text) { r.X = a4.Width - 10 }, false},
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
