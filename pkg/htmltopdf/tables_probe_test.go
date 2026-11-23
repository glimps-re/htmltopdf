//go:build !race

package htmltopdf

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Probes of the M1-16 review (non-race: heap and time are what count). They
// log the numbers; the heap bound of 128 MiB is asserted, the time targets
// (2 s) are logged.
func TestTableReviewProbes(t *testing.T) {
	c := mustNew(t, Options{})
	probe := func(name, html string) suiteOutcome {
		o := renderCase(t, c, []byte(html))
		pages, warns := 0, 0
		if o.res != nil {
			pages, warns = o.res.Pages, len(o.res.Warnings)
		}
		t.Logf("%-34s input %7d B, %v, peak %6.1f MiB, pages %4d, warnings %d, err=%v", name, len(html), o.took.Round(time.Millisecond), float64(o.peak)/(1<<20), pages, warns, o.err)
		return o
	}
	t.Run("B2 colspan=1000 tables", func(t *testing.T) {
		for _, n := range []int{16_000, 17_000, 18_000, 19_000, 20_000} {
			o := probe(fmt.Sprintf("%d x <table><td colspan=1000>", n), strings.Repeat("<table><td colspan=1000></table>", n))
			if o.peak > 128<<20 {
				t.Errorf("%d tables: peak %d MiB > 128", n, o.peak>>20)
			}
		}
	})
	t.Run("B1 thead rows", func(t *testing.T) {
		rows := func(n int) string { return strings.Repeat("<tr><td>h", n) }
		body := func(n int) string { return strings.Repeat("<tr><td>b", n) }
		css := func(lh int) string { return fmt.Sprintf("<style>td{line-height:%dpx}</style>", lh) }
		with := probe("3000 thead + 3000 body, lh 100", css(100)+"<table><thead>"+rows(3000)+"</thead><tbody>"+body(3000)+"</tbody></table>")
		without := probe("3000 body only, lh 100", css(100)+"<table><tbody>"+body(3000)+"</tbody></table>")
		t.Logf("with thead %v vs without %v", with.took.Round(time.Millisecond), without.took.Round(time.Millisecond))
		o := probe("12000 thead + 480 body, lh 900", css(900)+"<table><thead>"+rows(12000)+"</thead><tbody>"+body(480)+"</tbody></table>")
		if o.peak > 128<<20 {
			t.Errorf("peak %d MiB", o.peak>>20)
		}
	})
	t.Run("S1 border-spacing", func(t *testing.T) {
		o := probe("border-spacing 0 2000px", "<table style='border-spacing:0 2000px'><tr><td style='line-height:400px'>x</table>")
		for _, w := range o.res.Warnings {
			t.Logf("warning: %s %q (x%d)", w.Code, w.Detail, w.Count)
		}
	})
}
