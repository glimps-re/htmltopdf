//go:build unix

package raster

import (
	"syscall"
	"testing"
	"time"
)

// maxNsPerWork is the CPU one Work unit may stand for: MaxWork × 20 ns bounds
// a render to about two seconds.
const maxNsPerWork = 20

// cpuTime returns the user and system CPU time of the process. Unlike the
// wall clock it does not grow while other processes (parallel test
// packages) hold the CPUs; tests of one package run one at a time.
func cpuTime(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// TestWorkPerPixelCalibration checks that the Work of Decode keeps decode plus
// re-encode near maxNsPerWork of CPU per unit on every variant at the largest
// size the caps accept and on the shapes whose cost is not in the pixels. The
// best of three measures filters noise; GC work is included. It logs above the
// target and fails only above twice the target, so a slower machine passes.
func TestWorkPerPixelCalibration(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing calibration: not under -short or -race")
	}
	for _, c := range append(append(benchCases(), costCases()...), shapeCases()...) {
		t.Run(c.name, func(t *testing.T) {
			s, err := NewSet(newBudget()).Add(c.src(t))
			if err != nil {
				t.Fatal(err)
			}
			best := time.Duration(1 << 62)
			for range 3 {
				best = min(best, cpuPerRun(t, s))
			}
			ns := float64(best.Nanoseconds()) / float64(s.work())
			t.Logf("%d×%d, %d payload bytes: %v CPU, %d units, %.1f ns/unit", s.Width, s.Height, len(s.payload), best, s.work(), ns)
			if ns > maxNsPerWork {
				t.Logf("%.1f ns per Work unit, above the %d ns target", ns, maxNsPerWork)
			}
			if ns > 2*maxNsPerWork {
				t.Errorf("%.1f ns per Work unit > %d", ns, 2*maxNsPerWork)
			}
		})
	}
}

// minCPU is the CPU a measure accumulates: getrusage is too coarse to time a
// single decode of a small image.
const minCPU = 50 * time.Millisecond

// cpuPerRun returns the mean CPU time of decoding and re-encoding s.
func cpuPerRun(t *testing.T, s *Source) time.Duration {
	t.Helper()
	start := cpuTime(t)
	for n := 1; ; n++ {
		d, err := Decode(s, newBudget())
		if err != nil {
			t.Fatal(err)
		}
		if err := reencode(d, s.Format == JPEG); err != nil {
			t.Fatal(err)
		}
		if spent := cpuTime(t) - start; spent >= minCPU {
			return spent / time.Duration(n)
		}
	}
}
