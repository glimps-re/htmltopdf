//go:build !race

package dom

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

const heapBudget = 64 << 20

func TestHostileHeapMax(t *testing.T) {
	old := debug.SetGCPercent(100)
	defer debug.SetGCPercent(old)
	for name, unit := range hostileUnits {
		t.Run(name, func(t *testing.T) {
			src := repeatTo(unit, inputMax)
			runtime.GC()
			var peak uint64
			b := budget.New(t.Context(), budget.Defaults(), func(string) {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				peak = max(peak, m.HeapAlloc)
			})
			_, err := Parse(strings.NewReader(src), b)
			if err == nil {
				t.Fatal("expected a limit error")
			}
			t.Logf("heap-max %.1f MiB (input %d bytes included)", float64(peak)/(1<<20), len(src))
			if peak > heapBudget {
				t.Fatalf("heap-max %d MiB > %d MiB", peak>>20, heapBudget>>20)
			}
		})
	}
}
