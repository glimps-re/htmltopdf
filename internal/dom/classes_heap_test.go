//go:build !race

package dom

import (
	"runtime"
	"strings"
	"testing"
)

func TestClassBombHeap(t *testing.T) {
	src := `<p class="` + strings.Repeat("a ", (2<<20-30)/2) + `">`
	d := parse(t, src)
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	runtime.KeepAlive(d)
	if m.HeapAlloc > 16<<20 {
		t.Fatalf("heap after parsing 1M repeated classes: %d MiB", m.HeapAlloc>>20)
	}
}
