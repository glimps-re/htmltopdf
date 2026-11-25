package layout

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func bigAutoTable(cols, rows int) string {
	var sb strings.Builder
	sb.WriteString("<table>")
	for r := range rows {
		sb.WriteString("<tr>")
		for c := range cols {
			fmt.Fprintf(&sb, "<td>%d</td>", (r+c)%7)
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("</table>")
	return sb.String()
}

// An auto table costs a constant number of Work units per cell: 1000 columns by
// 300 rows stay within 3 x the cells + 10 000, under the 1-minute guard.
func autoWork(t *testing.T, cols, rows int, layout string) (work int64, pages int) {
	t.Helper()
	start := time.Now()
	html := strings.Replace(bigAutoTable(cols, rows), "<table>", "<table style='table-layout:"+layout+"'>", 1)
	r := newRun(t, html, opts{extra: "@page { size: 800px 600px; margin: 0 }", limits: func(l *budget.Limits) {
		l[budget.Work] = 1 << 30
		l[budget.Pages] = 5000
		l[budget.Nodes] = 1 << 20
		l[budget.Boxes] = 1 << 20
	}})
	pages = 0
	for {
		if _, err := r.pag.Next(); err != nil {
			break
		}
		pages++
		if time.Since(start) > time.Minute {
			t.Fatal("hang")
		}
	}
	return r.b.Used(budget.Work), pages
}

// Work of an auto table is linear in its cells, and about twice that of the same
// table with fixed widths: each cell is measured (shaped) once more for its
// minimum and maximum width. The 1000 x 300 table renders under the 1-minute
// guard.
func TestAutoTableWorkIsLinear(t *testing.T) {
	small, _ := autoWork(t, 200, 50, "auto")
	big, _ := autoWork(t, 200, 100, "auto")
	twin, _ := autoWork(t, 200, 100, "fixed")
	if float64(big) > 2.1*float64(small) {
		t.Errorf("work %d for 100 rows, %d for 50: not linear", big, small)
	}
	if float64(big) > 2.2*float64(twin) {
		t.Errorf("work %d against %d for the fixed twin", big, twin)
	}
	cols, rows := 1000, 300
	if raceEnabled {
		rows = 60 // the race detector makes the big table slower than the 1-minute guard
	}
	var w int64
	var pages int
	peak := peakHeap(func() { w, pages = autoWork(t, cols, rows, "auto") })
	fixedPeak := peakHeap(func() { autoWork(t, cols, rows, "fixed") })
	t.Logf("1000 x 300: work %d (%d per cell), %d pages, heap-max %d MiB (fixed twin %d MiB)", w, w/int64(cols*rows), pages, peak>>20, fixedPeak>>20)
	// The heap holds the document, the boxes and the fragments of 300 000 cells
	// (the twin pays them too); auto adds the contributions of the cells.
	if float64(peak) > 1.3*float64(fixedPeak) {
		t.Errorf("heap-max %d MiB against %d MiB for the fixed twin", peak>>20, fixedPeak>>20)
	}
}

// Cells that span a thousand columns cost their tracks in Work: a row of 1000
// cells and thousands of rows of colspan=1000 either render within the Work
// limit or end in a LimitError, under the 1-minute guard.
func TestAutoTableColspanWork(t *testing.T) {
	start := time.Now()
	var sb strings.Builder
	sb.WriteString("<table><tr>")
	for range 1000 {
		sb.WriteString("<td>x</td>")
	}
	sb.WriteString("</tr>")
	for range 3000 {
		sb.WriteString("<tr><td colspan=1000>y</td></tr>")
	}
	sb.WriteString("</table>")
	run := func(work int64) (int64, error) {
		r := newRun(t, sb.String(), opts{extra: "@page { size: 800px 600px; margin: 0 }", limits: func(l *budget.Limits) {
			l[budget.Work] = work
			l[budget.Pages] = 5000
			l[budget.Nodes] = 1 << 20
			l[budget.Boxes] = 1 << 20
		}})
		for {
			if _, err := r.pag.Next(); err != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				return r.b.Used(budget.Work), err
			}
		}
	}
	used, err := run(1 << 30)
	if err != nil {
		t.Fatal(err)
	}
	if used < 2*1000*3000 {
		t.Errorf("work %d does not count the 3000 cells of 1000 tracks", used)
	}
	if _, err := run(1_000_000); err == nil {
		t.Error("no limit error at a Work limit of 1e6")
	} else {
		var le *budget.LimitError
		if !errors.As(err, &le) || le.Kind != budget.Work {
			t.Errorf("error %v", err)
		}
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

func BenchmarkAutoTable300(b *testing.B) {
	html := bigAutoTable(8, 300)
	b.ReportAllocs()
	for b.Loop() {
		newRun(b, html, opts{extra: "@page { size: 800px 600px; margin: 0 }"}).all(b)
	}
}
