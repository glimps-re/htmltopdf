package paint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/text"
)

func TestTextRun(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "Bonjour, déjà vu !", 16)
	run.X, run.Baseline = 100, 200
	got, _ := e.page(t, a4, display.List{&run})
	if !strings.HasPrefix(got, "0 0 0 rg\nBT\n/F1 12 Tf\n75 600 Td\n[<") || !strings.HasSuffix(got, ">] TJ\nET\n") {
		t.Fatalf("content:\n%s", got)
	}
	doc := e.finish(t)
	if len(doc.Pages[0].Text) != 1 || doc.Pages[0].Text[0] != "Bonjour, déjà vu !" {
		t.Fatalf("extracted %q", doc.Pages[0].Text)
	}
}

func TestExactTextForClusters(t *testing.T) {
	e := newEnv(t)
	const (
		french = "Été à Noël : «Où êtes-vous ?» — ça coûte 12,50 € …"
		arrows = "↳ … — « » ⟨RLO⟩"
	)
	var l display.List
	for i, txt := range []string{french, arrows} {
		for _, r := range e.runs(t, txt, 14) {
			r.X, r.Baseline = 10, 100+float64(i)*40
			l = append(l, &r)
		}
	}
	e.page(t, a4, l)
	doc := e.finish(t)
	// One block per face run; the runs, joined, are the lines in order.
	got := strings.Join(doc.Pages[0].Text, "")
	if want := french + arrows; got != want {
		t.Fatalf("extracted %q, want %q", got, want)
	}
}

func TestFontsAreAddedOnce(t *testing.T) {
	e := newEnv(t)
	var l display.List
	for i := 0; i < 5; i++ {
		r := e.run(t, "abc", 12)
		r.X, r.Baseline = 10, 50+float64(i)*20
		l = append(l, &r)
	}
	arrow := e.run(t, "↳", 12)
	arrow.X, arrow.Baseline = 10, 300
	l = append(l, &arrow)
	e.page(t, a4, l)
	doc := e.finish(t)
	if len(doc.Fonts) != 2 {
		t.Fatalf("%d fonts, want 2 (Liberation and DejaVu)", len(doc.Fonts))
	}
}

func TestTextScale(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "ab", 20)
	run.X, run.Baseline = 100, 200
	g := Geometry{Width: 800, Height: 1000, Area: display.Rect{X: 40, Y: 80, W: 720, H: 840}, Scale: 0.5}
	got, _ := e.page(t, g, display.List{&run})
	// size 20px * 0.5 * 0.75 = 7.5pt; x = (40+50)*.75 = 67.5; y = (1000-80-100)*.75 = 615.
	if !strings.Contains(got, "/F1 7.5 Tf\n67.5 615 Td\n") {
		t.Fatalf("content:\n%s", got)
	}
	e.finish(t)
}

func TestDecorations(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "ab", 20)
	run.X, run.Baseline = 100, 200
	plain, _ := e.page(t, a4, display.List{&run})
	run.Underline, run.LineThrough = true, true
	got, _ := e.page(t, a4, display.List{&run})
	extra := strings.TrimPrefix(got, plain)
	if extra == got || strings.Count(extra, "re\nf\n") != 2 {
		t.Fatalf("decorations:\n%s", extra)
	}
	balanced(t, got)
	e.finish(t)
}

func TestUnderlineGeometry(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "ab", 16)
	run.X, run.Baseline, run.Underline = 100, 200, true
	got, _ := e.page(t, a4, display.List{&run})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	re := lines[len(lines)-2]
	var x, y, w, h float64
	if _, err := fmt.Sscanf(re, "%g %g %g %g re", &x, &y, &w, &h); err != nil {
		t.Fatalf("%q: %v", re, err)
	}
	size := 12.0 // 16 px in points
	baseline := 600.0
	if x != 75 || h < size*0.05 || h > size*0.09 || y > baseline || y < baseline-size*0.2 {
		t.Fatalf("underline %v %v %v %v, baseline %v size %v", x, y, w, h, baseline, size)
	}
	if want := run.Width() * 0.75; w < want-0.01 || w > want+0.01 {
		t.Fatalf("underline width %v, run width %v pt", w, want)
	}
}

func TestTextSkipped(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "abc", 12)
	run.X, run.Baseline = 10, 50
	for name, it := range map[string]display.Text{
		"no glyphs":   {Face: run.Face, Size: 12, Color: black},
		"transparent": {Face: run.Face, Size: 12, Glyphs: run.Glyphs, Text: "abc"},
		"zero size":   {Face: run.Face, Size: 0, Color: black, Glyphs: run.Glyphs, Text: "abc"},
		"no face":     {Size: 12, Color: black, Glyphs: run.Glyphs, Text: "abc"},
		"off page":    {Face: run.Face, Size: 12, Color: black, Glyphs: run.Glyphs, Text: "abc", X: 10, Baseline: 5000},
		"far left":    {Face: run.Face, Size: 12, Color: black, Glyphs: run.Glyphs, Text: "abc", X: -5000, Baseline: 50},
	} {
		c := &pdf.Content{}
		if _, err := e.painter.Page(c, a4, display.List{&it}); err != nil || len(c.Bytes()) != 0 {
			t.Errorf("%s: err %v, %q", name, err, c.Bytes())
		}
	}
}

func TestUnreadableFontIsAnError(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "abc", 12)
	run.Face = &text.Face{Family: "broken"}
	run.X, run.Baseline = 10, 50
	if _, err := e.painter.Page(&pdf.Content{}, a4, display.List{&run}); err == nil {
		t.Fatal("a face that cannot be read was accepted")
	}
}

func TestGlyphOutOfTheFontIsAnError(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "abc", 12)
	run.Glyphs = append([]text.Glyph(nil), run.Glyphs...)
	run.Glyphs[1].ID = 65535
	run.X, run.Baseline = 10, 50
	if _, err := e.painter.Page(&pdf.Content{}, a4, display.List{&run}); err == nil {
		t.Fatal("a glyph beyond the font was accepted")
	}
}

func TestClusterText(t *testing.T) {
	g := func(clusters ...int32) []text.Glyph {
		out := make([]text.Glyph, len(clusters))
		for i, c := range clusters {
			out[i].Cluster = c
		}
		return out
	}
	tests := []struct {
		txt      string
		glyphs   []text.Glyph
		i        int
		want     string
		wantNext string
	}{
		{"abc", g(0, 1, 2), 0, "a", "b"},
		{"abc", g(0, 1, 2), 2, "c", ""},
		{"ffi!", g(0, 0, 0, 3), 0, "ffi", "!"},
		{"é!", g(0, 2), 0, "é", "!"},
		{"éx", g(0, 0, 3), 0, "é", "x"},
		{"abc", g(5), 0, "", ""},
		{"abc", g(-1), 0, "", ""},
		{"abc", g(2, 1), 0, "", ""},
	}
	for _, tt := range tests {
		if got := clusterText(tt.txt, tt.glyphs, tt.i); got != tt.want {
			t.Errorf("%q glyph %d: %q, want %q", tt.txt, tt.i, got, tt.want)
		}
	}
}
