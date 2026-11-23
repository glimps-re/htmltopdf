package css

import (
	"context"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestMediaQueries(t *testing.T) {
	print794 := MediaEnv{Print: true, Width: 794, Height: 1123}
	landscape := MediaEnv{Print: true, Width: 1123, Height: 794}
	screen := MediaEnv{Width: 794, Height: 1123}
	tests := []struct {
		q    string
		env  MediaEnv
		want bool
	}{
		{"", print794, true},
		{"all", print794, true},
		{"print", print794, true},
		{"PRINT", print794, true},
		{"screen", print794, false},
		{"screen", screen, true},
		{"print", screen, false},
		{"not print", print794, false},
		{"not screen", print794, true},
		{"only print", print794, true},
		{"only screen", print794, false},
		{"not all", print794, false},
		{"print and (min-width: 600px)", print794, true},
		{"print and (min-width: 800px)", print794, false},
		{"screen and (min-width: 600px)", print794, false},
		{"(max-width: 600px)", print794, false},
		{"(max-width: 794px)", print794, true},
		{"(max-width: 793.9px)", print794, false},
		{"(min-width: 794px)", print794, true},
		{"(width: 794px)", print794, true},
		{"(width: 795px)", print794, false},
		{"(min-width: 40em)", print794, true},
		{"(min-width: 50em)", print794, false},
		{"(max-width: 50rem)", print794, true},
		{"(min-width: 10in)", print794, false},
		{"(min-width: 100pt)", print794, true},
		{"(min-height: 1000px)", print794, true},
		{"(max-height: 1000px)", print794, false},
		{"(height: 1123px)", print794, true},
		{"(orientation: portrait)", print794, true},
		{"(orientation: landscape)", print794, false},
		{"(orientation: landscape)", landscape, true},
		{"(orientation: PORTRAIT)", print794, true},
		{"(min-width: 600px) and (max-width: 900px)", print794, true},
		{"(min-width: 600px) and (max-width: 700px)", print794, false},
		{"print and (min-width: 600px) and (orientation: portrait)", print794, true},
		{"print, screen", screen, true},
		{"screen, (max-width: 100px)", print794, false},
		{"screen, print", print794, true},
		{"bar, print", print794, true},
		{"(max-width: 100px), (min-width: 700px)", print794, true},
		{"unknown", print794, false},
		{"not unknown", print794, true},
		{"only (min-width: 1px)", print794, false},
		{"only", print794, false},
		{"(foo: 1)", print794, false},
		{"(color)", print794, false},
		{"print and", print794, false},
		{"and (min-width: 1px)", print794, false},
		{"print (min-width: 1px)", print794, false},
		{"print and (min-width: 1px) (max-width: 9px)", print794, false},
		{"(min-width: -1px)", print794, false},
		{"(min-width: 50%)", print794, false},
		{"(min-width: 10)", print794, false},
		{"(min-width: 0)", print794, true},
		{"(min-width)", print794, false},
		{"not (min-width: 1px)", print794, false},
		{"(width >= 600px)", print794, false},
		{"print,", print794, true},
		{",print", print794, true},
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			q := ParseMediaQuery(selectorTokens2(t, tt.q), budget.New(context.Background(), budget.Defaults(), nil))
			if got := q.Match(tt.env); got != tt.want {
				t.Fatalf("Match = %v, want %v", got, tt.want)
			}
		})
	}
}

// selectorTokens2 returns the prelude of "@media <text> {}" .
func selectorTokens2(tb testing.TB, text string) []Token {
	tb.Helper()
	l := budget.Defaults()
	l[budget.CSSBytes] = 1 << 26
	l[budget.Work] = 1 << 40
	ss, err := ParseStylesheet([]byte("@media "+text+"{a{x:y}}"), OriginDocument, budget.New(context.Background(), l, nil))
	if err != nil {
		tb.Fatal(err)
	}
	if len(ss.Rules) != 1 || ss.Rules[0].Kind != RuleMedia {
		tb.Fatalf("%q: %d rules", text, len(ss.Rules))
	}
	return ss.Rules[0].Prelude
}

func TestMediaQueryZeroValueMatchesEverything(t *testing.T) {
	var q MediaQuery
	if !q.Match(MediaEnv{}) || !q.Match(MediaEnv{Print: true, Width: 1, Height: 1}) {
		t.Fatal("zero value does not match")
	}
}

func TestMediaQueryHostile(t *testing.T) {
	for _, text := range []string{
		"print" + repeatStr(", print", 50_000),
		repeatStr("(min-width: 1px) and ", 50_000) + "print",
		repeatStr("(", 60) + repeatStr(")", 60),
		repeatStr("not ", 10_000) + "print",
	} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ParseMediaQuery(selectorTokens2(t, text), b).Match(MediaEnv{Print: true, Width: 1, Height: 1})
	}
}

func repeatStr(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

// A query that evaluates to false because it is malformed or unsupported is
// reported; a valid one, including one that simply does not match, is not.
func TestMediaQueryWarnings(t *testing.T) {
	tests := []struct {
		q    string
		warn []string
	}{
		{"", nil},
		{"print", nil},
		{"screen", nil},
		{"unknown", nil},
		{"not unknown", nil},
		{"print and (min-width: 600px)", nil},
		{"(max-width: 1px), print", nil},
		{"(color)", []string{"invalid-value:media query: (color)"}},
		{"(width >= 600px)", []string{"invalid-value:media query: (width >= 600px)"}},
		{"(foo: 1)", []string{"invalid-value:media query: (foo: 1)"}},
		{"print and", []string{"invalid-value:media query: print and"}},
		{"only (min-width: 1px)", []string{"invalid-value:media query: only (min-width: 1px)"}},
		{"(min-width: -1px)", []string{"invalid-value:media query: (min-width: -1px)"}},
		{"print, (foo: 1), screen, bar baz", []string{
			"invalid-value:media query: (foo: 1)", "invalid-value:media query: bar baz",
		}},
		{"print,", []string{"invalid-value:media query: "}},
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			b := budget.New(context.Background(), budget.Defaults(), nil)
			ParseMediaQuery(selectorTokens2(t, tt.q), b)
			if got := warnings(b); len(got) != len(tt.warn) {
				t.Fatalf("warnings %q, want %q", got, tt.warn)
			} else {
				for i := range got {
					if got[i] != tt.warn[i] {
						t.Errorf("warning %d = %q, want %q", i, got[i], tt.warn[i])
					}
				}
			}
		})
	}
}
