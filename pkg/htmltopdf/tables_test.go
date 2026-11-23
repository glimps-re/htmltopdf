package htmltopdf

import (
	"errors"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// M1-16 criteria measured through the public API, with the default options and
// with the host-connector options. Times and heaps are logged; the bounds the
// criterion states (2 s, 128 MiB) are asserted on the heap only.
func TestTablesDoneCriterion(t *testing.T) {
	cases := map[string]testcorpus.Case{}
	for _, c := range append(testcorpus.Hostile(budget.Defaults()), testcorpus.SpikeHostile()...) {
		cases[c.Name] = c
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			run := func(name string) suiteOutcome {
				tc := cases[name]
				opts := v.opts(t)
				opts.Limits = caseLimits(tc)
				o := renderCase(t, mustNew(t, opts), tc.HTML())
				t.Logf("%s: %d B in, %v, peak %.1f MiB, pages %d, err %v", name, o.input, o.took.Round(time.Millisecond), float64(o.peak)/(1<<20), o.res.Pages, o.err)
				if o.peak > 128<<20 {
					t.Errorf("%s: peak heap %d MiB > 128", name, o.peak>>20)
				}
				return o
			}
			// 2 505 rows → LimitPages (Max 500); the 2 495-row twin → 499 pages.
			if o := run("pages_table_tall_rows_over"); !isLimitOf(o.err, LimitPages, 500) {
				t.Errorf("2 505 rows: %v", o.err)
			}
			if o := run("pages_table_tall_rows_under"); o.err != nil || o.res.Pages != 499 {
				t.Errorf("2 495 rows: pages %d err %v, want 499 pages", o.res.Pages, o.err)
			}
			// 30 000 rows: LimitPages; the MaxPages=1000 twin renders every page.
			if o := run("pages_table_rows_over"); !isLimitOf(o.err, LimitPages, 500) {
				t.Errorf("30 000 rows: %v", o.err)
			}
			o := run("pages_table_rows_under")
			if o.err != nil || o.res.Pages <= 500 || o.res.Pages > 1000 {
				t.Errorf("30 000 rows, MaxPages 1000: pages %d err %v", o.res.Pages, o.err)
			}
			// The spike's colspan bomb: a warning and no failure.
			o = run("hostile_colspan")
			found := false
			for _, w := range o.res.Warnings {
				found = found || w.Code == WarnClamped
			}
			if o.err != nil || !found {
				t.Errorf("hostile_colspan: err %v, warnings %+v", o.err, o.res.Warnings)
			}
			// The spike's 50k rows hit the input limit first.
			if o := run("hostile_50k_rows"); !isLimitOf(o.err, LimitInputBytes, 0) && !isLimitOf(o.err, LimitNodes, 0) {
				t.Errorf("50k rows: %v", o.err)
			}
			// More columns than the limit, and colspan over the clamp, warn.
			for _, name := range []string{"colspan_over", "table_columns_over"} {
				o := run(name)
				clamped := false
				for _, w := range o.res.Warnings {
					clamped = clamped || w.Code == WarnClamped
				}
				if o.err != nil || !clamped {
					t.Errorf("%s: err %v, warnings %+v", name, o.err, o.res.Warnings)
				}
			}
		})
	}
}

// isLimitOf reports whether err is a *LimitError of kind l (and of maximum max
// when max is not 0).
func isLimitOf(err error, l Limit, max int64) bool {
	var le *LimitError
	return errors.As(err, &le) && le.Limit == l && (max == 0 || le.Max == max)
}
