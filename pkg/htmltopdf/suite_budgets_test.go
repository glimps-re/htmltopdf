//go:build !race

package htmltopdf

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// corpusHeapCeiling is the heap budget of one corpus report: the baseline
// peaks at 4-25 MiB, so 64 MiB is a regression alarm well under the 256 MiB of
// the hostile suite.
const corpusHeapCeiling = 64 << 20

// TestCorpusBudgets applies the default limits (and each case's adjustments) to
// the report corpus and the hostile corpus: every report renders within every
// budget with room to spare, and the twin cases keep their margins. The time
// and heap are logged, not asserted (a heap ceiling of 256 MiB is).
func TestCorpusBudgets(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	for name, html := range reportDocs(t) {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			var res *Result
			var b *budget.Budget
			var err error
			var took time.Duration
			peak := peakOf(func() {
				start := time.Now()
				res, b, err = c.render(context.Background(), bytes.NewReader(html), &buf)
				took = time.Since(start)
			})
			if err != nil {
				t.Fatal(err)
			}
			work, maxWork := b.Used(budget.Work), budget.Defaults()[budget.Work]
			t.Logf("%d pages, %d bytes, %v, peak %.1f MiB, work %d (%.2f%% of MaxWork), %d warnings", res.Pages, res.Bytes, took.Round(time.Millisecond), float64(peak)/(1<<20), work, 100*float64(work)/float64(maxWork), len(res.Warnings))
			if peak > corpusHeapCeiling {
				t.Errorf("peak heap %d MiB exceeds the corpus ceiling of %d MiB", peak>>20, corpusHeapCeiling>>20)
			}
			if work > maxWork/20 {
				t.Errorf("work %d exceeds MaxWork/20 = %d: the default would not leave 20x headroom", work, maxWork/20)
			}
			if buf.Len() <= suiteValidateMax {
				pdftest.Validate(t, buf.Bytes())
			}
		})
	}
	// Twins apply their own limits: the stress twin raises only MaxPages.
	for _, tc := range testcorpus.Hostile(budget.Defaults()) {
		if tc.Limits == nil {
			continue
		}
		t.Run("limits/"+tc.Name, func(t *testing.T) {
			opts := referenceOptions(t)
			opts.Limits = caseLimits(tc)
			o := renderCase(t, mustNew(t, opts), tc.HTML())
			if o.err != nil {
				t.Fatalf("the twin with its adjusted limits fails: %v", o.err)
			}
			t.Logf("%d pages, %v, peak %.1f MiB", o.res.Pages, o.took.Round(time.Millisecond), float64(o.peak)/(1<<20))
		})
	}
}
