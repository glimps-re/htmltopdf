//go:build !race

package dom

import (
	"bytes"
	"runtime"
	"testing"
)

// TestReadBoundedAlloc pins that readBounded allocates in proportion to the
// document, not to the InputBytes limit.
func TestReadBoundedAlloc(t *testing.T) {
	const doc = 70 << 10
	data := bytes.Repeat([]byte{'x'}, doc)
	for kind, mk := range readerKinds {
		for _, limit := range []int64{2 << 20, 512 << 20} {
			// Doubling allocates under twice the final buffer, itself under
			// twice the document; 16 KiB covers the small allocations.
			const want = 4*doc + 16<<10
			b := inputBudget(limit)
			src := mk(data)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			got, err := readBounded(src, b)
			runtime.ReadMemStats(&after)
			if err != nil || len(got) != doc {
				t.Fatalf("%s/%d: %d bytes, %v", kind, limit, len(got), err)
			}
			if alloc := after.TotalAlloc - before.TotalAlloc; alloc > want {
				t.Errorf("%s/%d MiB: allocated %d bytes for a %d-byte document, want <= %d",
					kind, limit>>20, alloc, doc, want)
			}
		}
	}
}
