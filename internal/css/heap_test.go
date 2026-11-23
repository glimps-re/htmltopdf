//go:build !race

package css

import (
	"runtime"
	"strings"
	"testing"
)

// The token arena is sized once at one token per byte (32 B per source byte)
// and never regrows. What a parse of the largest allowed sheet allocates on top
// is the source copies and the rule and declaration tables (doubled while they
// grow): 1 B, then at most 96 B per source byte in the worst sheet below
// (measured: 1, 70 and 40 B per byte). Cumulative allocation bounds the peak
// heap, whatever the garbage collector does.
func TestAllocationAtCSSLimit(t *testing.T) {
	const size = 512 << 10
	sheets := map[string]string{
		"one token per byte": strings.Repeat("a,", size/2),
		"rules":              strings.Repeat("a{b:c}", size/6),
		"declarations":       "a{" + strings.Repeat("b:c;", size/4-2) + "}",
	}
	for name, src := range sheets {
		t.Run(name, func(t *testing.T) {
			src = src[:min(len(src), size)]
			b := newBudget(size)
			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)
			ss, err := ParseStylesheet([]byte(src), OriginDocument, b)
			runtime.ReadMemStats(&m1)
			if err != nil {
				t.Fatal(err)
			}
			total := m1.TotalAlloc - m0.TotalAlloc
			arena := uint64(size+1) * 32
			t.Logf("%d source bytes: %d MiB allocated (arena %d MiB)", len(src), total>>20, arena>>20)
			if total > arena+uint64(size)*96 {
				t.Fatalf("allocated %d MiB, want about the arena (%d MiB) plus 96 B per source byte", total>>20, arena>>20)
			}
			runtime.KeepAlive(ss)
		})
	}
}
