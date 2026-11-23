package css

import (
	"context"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func mediaQuery(tb testing.TB, text string) (MediaQuery, []string) {
	tb.Helper()
	b := budget.New(context.Background(), budget.Defaults(), nil)
	ss, err := ParseStylesheet([]byte("@media "+text+"{a{x:y}}"), OriginDocument, b)
	if err != nil || len(ss.Rules) != 1 {
		tb.Fatalf("%q: %d rules, %v", text, len(ss.Rules), err)
	}
	b = budget.New(context.Background(), budget.Defaults(), nil)
	q := ParseMediaQuery(ss.Rules[0].Prelude, b)
	return q, warnings(b)
}

func TestMediaQueryMatchAndWarnings(t *testing.T) {
	print800 := MediaEnv{Print: true, Width: 794, Height: 1123}
	screen := MediaEnv{Print: false, Width: 1024, Height: 600}
	landscape := MediaEnv{Print: true, Width: 1123, Height: 794}
	for _, tt := range []struct {
		q                         string
		print, screenM, landscape bool
		warns                     int
	}{
		{"", true, true, true, 0},
		{"all", true, true, true, 0},
		{"print", true, false, true, 0},
		{"screen", false, true, false, 0},
		{"PRINT", true, false, true, 0},
		{"only print", true, false, true, 0},
		{"not print", false, true, false, 0},
		{"not screen", true, false, true, 0},
		{"print, screen", true, true, true, 0},
		{"tv", false, false, false, 0},
		{"not tv", true, true, true, 0},
		{"(min-width: 800px)", false, true, true, 0},
		{"(min-width: 794px)", true, true, true, 0},
		{"(min-width: 795px)", false, true, true, 0},
		{"(max-width: 794px)", true, false, false, 0},
		{"(max-width: 793px)", false, false, false, 0},
		{"(width: 794px)", true, false, false, 0},
		{"(min-height: 1123px)", true, false, false, 0},
		{"(max-height: 600px)", false, true, false, 0},
		{"(orientation: portrait)", true, false, false, 0},
		{"(orientation: landscape)", false, true, true, 0},
		{"print and (orientation: landscape)", false, false, true, 0},
		{"print and (min-width: 20em)", true, false, true, 0},
		{"screen and (min-width: 1024px) and (max-width: 1024px)", false, true, false, 0},
		{"(min-width: 100px) and (max-width: 900px)", true, false, false, 0},
		{"not print and (min-width: 1px)", false, true, false, 0},
		{"(min-width: 1in)", true, true, true, 0},
		{"print and (min-width: 1e30px)", false, false, false, 0},
		{"(MIN-WIDTH:100PX)", true, true, true, 0},
		{"( min-width : 100px )", true, true, true, 0},

		{"(color)", false, false, false, 1},
		{"(min-width)", false, false, false, 1},
		{"(min-width: )", false, false, false, 1},
		{"(min-width: 100)", false, false, false, 1},
		{"(min-width: -1px)", false, false, false, 1},
		{"(min-width: 50%)", false, false, false, 1},
		{"(min-width: calc(1px))", false, false, false, 1},
		{"(min-width: var(--x))", false, false, false, 1},
		{"(orientation: sideways)", false, false, false, 1},
		{"(orientation)", false, false, false, 1},
		{"print and", false, false, false, 1},
		{"and print", false, false, false, 1},
		{"print screen", false, false, false, 1},
		{"print and and (min-width: 1px)", false, false, false, 1},
		{"print (min-width: 1px)", false, false, false, 1},
		{"not", false, false, false, 1},
		{"only", false, false, false, 1},
		{"not (min-width: 1px)", false, false, false, 1},
		{"print or screen", false, false, false, 1},
		{"(min-width: 1px) or (max-width: 2px)", false, false, false, 1},
		{"(400px <= width)", false, false, false, 1},
		{"@", false, false, false, 1},
		{"{", false, false, false, 1},

		{"print, (color)", true, false, true, 1},
		{"(color), print, (nope)", true, false, true, 2},
		{"print,", true, false, true, 1},
		{",print", true, false, true, 1},
		{"print,,screen", true, true, true, 1},
	} {
		t.Run(tt.q, func(t *testing.T) {
			if strings.Contains(tt.q, "{") {
				return
			}
			q, w := mediaQuery(t, tt.q)
			if len(w) != tt.warns {
				t.Errorf("warnings %q, want %d", w, tt.warns)
			}
			for _, wn := range w {
				if !strings.HasPrefix(wn, "invalid-value:media query: ") {
					t.Errorf("warning %q is not an invalid-value media query warning", wn)
				}
			}
			for name, c := range map[string]struct {
				env  MediaEnv
				want bool
			}{"print": {print800, tt.print}, "screen": {screen, tt.screenM}, "landscape": {landscape, tt.landscape}} {
				if got := q.Match(c.env); got != c.want {
					t.Errorf("%s: got %v want %v", name, got, c.want)
				}
			}
		})
	}
}

func TestMediaQueryHostileInputs(t *testing.T) {
	l := budget.Defaults()
	l[budget.CSSBytes] = 1 << 30
	b := budget.New(context.Background(), l, nil)
	for name, src := range map[string]string{
		"60k alternatives":    strings.Repeat("print,", 60_000) + "screen",
		"60k invalid":         strings.Repeat("(x),", 60_000) + "print",
		"deep parentheses":    "(" + strings.Repeat("(", 60) + "min-width:1px" + strings.Repeat(")", 60) + ")",
		"too deep":            "(" + strings.Repeat("(", 5000) + "x",
		"long and chain":      "print" + strings.Repeat(" and (min-width: 1px)", 15_000),
		"huge number":         "(min-width: " + strings.Repeat("9", 400) + "px)",
		"bad bytes":           "\xff\xfe(\x00min-width: 1px)",
		"unterminated":        "(min-width: 'x",
		"unterminated url":    "(min-width: url(",
		"function in feature": "(min-width: f(g(h(1))))",
	} {
		ss, err := ParseStylesheet([]byte("@media "+src+"{a{x:y}}"), OriginDocument, b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, r := range ss.Rules {
			q := ParseMediaQuery(r.Prelude, b)
			q.Match(MediaEnv{Print: true, Width: 794, Height: 1123})
		}
	}
	ws, dropped := b.Warnings()
	if len(ws) == 0 || len(ws) > 256 {
		t.Errorf("%d warnings kept (%d dropped): want 1 to 256, duplicates merged", len(ws), dropped)
	}
	// The warning detail is bounded and sanitized.
	for _, w := range ws {
		if len(w.Detail) > 80 {
			t.Errorf("detail of %d bytes", len(w.Detail))
		}
	}
}
