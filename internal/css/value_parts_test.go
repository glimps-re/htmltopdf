package css

import (
	"runtime"
	"strings"
	"testing"
)

func TestSplitTopAndFieldsAreBounded(t *testing.T) {
	args := func(n int, sep string) []Token { return valueTokens(t, strings.Repeat("1"+sep, n)+"1") }
	for _, tt := range []struct {
		parts int // commas + 1
		want  int
	}{
		{maxParts - 1, maxParts - 1},
		{maxParts, maxParts},
		{maxParts + 1, maxParts + 1},
		{10 * maxParts, maxParts + 1},
		{200_000, maxParts + 1},
	} {
		if got := len(splitTop(args(tt.parts-1, ","))); got != tt.want {
			t.Errorf("splitTop of %d parts gives %d", tt.parts, got)
		}
		if got := len(fields(args(tt.parts-1, " "))); got != tt.want {
			t.Errorf("fields of %d parts gives %d", tt.parts, got)
		}
	}
}

func TestHostileArgumentCounts(t *testing.T) {
	huge := 100_000
	for name, src := range map[string]string{
		"rgb commas": "rgb(" + strings.Repeat("1,", huge) + "1)",
		"rgb spaces": "rgb(" + strings.Repeat("1 ", huge) + "1)",
		"gradient":   "linear-gradient(" + strings.Repeat("red,", huge) + "blue)",
		"stop":       "linear-gradient(red" + strings.Repeat(" 1%", huge) + ", blue)",
	} {
		t.Run(name, func(t *testing.T) {
			toks := valueTokens(t, src)
			var m0, m1 runtime.MemStats
			runtime.ReadMemStats(&m0)
			_, okC := ParseColor(toks)
			_, okG := ParseGradient(toks)
			runtime.ReadMemStats(&m1)
			if okC || okG {
				t.Fatal("accepted")
			}
			if d := m1.TotalAlloc - m0.TotalAlloc; d > 1<<20 {
				t.Fatalf("allocated %d KiB", d>>10)
			}
		})
	}
}
