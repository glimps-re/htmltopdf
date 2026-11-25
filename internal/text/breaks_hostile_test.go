package text

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// Breaks on 2 MiB of hostile text finishes (a one-minute guard), grows linearly in the number of breaks and
// in the bytes allocated from half the text to the full text (counts, not wall time), with a bounded
// allocation, and its offsets are valid.
func TestBreaksHostileTwoMiB(t *testing.T) {
	size := 2 << 20
	if raceDetectorOn || testing.Short() {
		size = 256 << 10
	}
	const punct = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	cases := map[string]func(n int) string{
		"punctuation only":   func(n int) string { return strings.Repeat(punct, n/len(punct)+1)[:n] },
		"alternating a-1":    func(n int) string { return strings.Repeat("a-1", n/3+1)[:n] },
		"alternating 1-a":    func(n int) string { return strings.Repeat("1-a", n/3+1)[:n] },
		"spaces and hyphens": func(n int) string { return strings.Repeat("- ", n/2) },
		"a-1 with spaces":    func(n int) string { return strings.Repeat("a-1 ", n/4) },
		"hyphen digits":      func(n int) string { return strings.Repeat("?-1", n/3) },
		"one punctuation":    func(n int) string { return strings.Repeat("/", n) },
		"urls":               func(n int) string { return strings.Repeat("https://e.x/a/b?c=d&e=f#g ", n/26+1)[:n] },
		"latin and cjk":      func(n int) string { return strings.Repeat("é-日1-a", n/9+1)[:n] },
	}
	for name, gen := range cases {
		t.Run(name, func(t *testing.T) {
			timeOf := func(n int) (time.Duration, int, uint64) {
				s := gen(n)
				var m0, m1 runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&m0)
				start := time.Now()
				bs := Breaks(s, nil)
				d := time.Since(start)
				runtime.ReadMemStats(&m1)
				prev := 0
				for _, b := range bs {
					if b.Offset <= prev || b.Offset >= len(s) {
						t.Fatalf("break at %d after %d (len %d)", b.Offset, prev, len(s))
					}
					prev = b.Offset
				}
				return d, len(bs), m1.TotalAlloc - m0.TotalAlloc
			}
			best := func(n int) (time.Duration, int, uint64) {
				d1, c, a := timeOf(n)
				d2, _, _ := timeOf(n)
				return min(d1, d2), c, a
			}
			half, nHalf, allocHalf := best(size / 2)
			full, n, alloc := best(size)
			t.Logf("%d breaks, %v for %d KiB (%v for half), %d MiB allocated", n, full.Round(time.Millisecond), size>>10, half.Round(time.Millisecond), alloc>>20)
			if full > time.Minute {
				t.Errorf("%v for the full text", full)
			}
			// Linearity by counts: twice the text gives at most 2.2 times the breaks and the bytes allocated.
			if float64(n) > 2.2*float64(nHalf)+16 {
				t.Errorf("not linear: %d breaks for the half text, %d for the full text", nHalf, n)
			}
			if float64(alloc) > 2.2*float64(allocHalf)+1<<20 {
				t.Errorf("not linear: %d KiB allocated for the half text, %d KiB for the full text", allocHalf>>10, alloc>>10)
			}
			if alloc > 128*uint64(max(size, 0)) {
				t.Errorf("%d MiB allocated for %d KiB of text", alloc>>20, size>>10)
			}
		})
	}
}

func BenchmarkBreaksHostile(b *testing.B) {
	for name, s := range map[string]string{
		"punct": strings.Repeat("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", 1<<11),
		"a-1":   strings.Repeat("a-1", 1<<15),
	} {
		b.Run(name, func(b *testing.B) {
			var dst []Break
			b.SetBytes(int64(len(s)))
			for b.Loop() {
				dst = Breaks(s, dst[:0])
			}
		})
	}
}
