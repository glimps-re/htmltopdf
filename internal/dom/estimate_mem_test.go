//go:build !race

package dom

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

const estimatorHeapBudget = 16 << 20

// estimatorRetained returns the heap the estimator holds after it ran over src.
func estimatorRetained(t *testing.T, src string) uint64 {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	e, err := runEstimator([]byte(src), 4*100_000, noPoll)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(e)
	// The buffer copy is not part of the estimator.
	return after.HeapAlloc - before.HeapAlloc - uint64(len(src))
}

func TestEstimatorMemory(t *testing.T) {
	var distinct strings.Builder
	for i := range 300_000 {
		fmt.Fprintf(&distinct, "<x%d>", i)
	}
	cases := map[string]string{
		"300k distinct unknown tags": distinct.String(),
		"400k nested divs":           strings.Repeat("<div>", 400_000),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := estimatorRetained(t, src)
			t.Logf("estimator holds %.1f MiB for %d input bytes", float64(got)/(1<<20), len(src))
			if got > estimatorHeapBudget {
				t.Fatalf("estimator holds %d MiB > %d MiB", got>>20, estimatorHeapBudget>>20)
			}
		})
	}
}
