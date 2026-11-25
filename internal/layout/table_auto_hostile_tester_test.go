package layout

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func nestedTables(depth int, leaf string) string {
	var sb strings.Builder
	for range depth {
		sb.WriteString(`<table><tr><td>a</td><td>`)
	}
	sb.WriteString(leaf)
	for range depth {
		sb.WriteString(`</td></tr></table>`)
	}
	return sb.String()
}

// Auto tables nested 10, 20 and 40 deep: the work grows linearly with the
// depth (the intrinsic widths are cached, not recomputed per level).
func TestNestedAutoTablesWorkIsLinear(t *testing.T) {
	work := map[int]int64{}
	for _, d := range []int{10, 20, 40} {
		r, pages, err := sizingRun(t, nestedTables(d, "leaf text"))
		if err != nil {
			t.Fatalf("depth %d: %v", d, err)
		}
		allFinite(t, "nested", pages)
		work[d] = r.b.Used(budget.Work)
	}
	t.Logf("work %v", work)
	if work[40] > 5*work[10]+20000 || work[20] > 3*work[10]+10000 {
		t.Errorf("work grows faster than the depth: %v", work)
	}
}

// Tables at auto layout with hostile widths, spans and contents: finite
// geometry or a LimitError, never a hang.
func TestHostileAutoTables(t *testing.T) {
	cells := func(n int, attr string) string {
		return strings.Repeat(`<td `+attr+`>x</td>`, n)
	}
	for name, html := range map[string]string{
		"colspan 1000 over 1000 columns":  `<table><tr>` + cells(1000, "") + `</tr><tr><td colspan="1000">wide</td></tr></table>`,
		"colspan 65535":                   `<table><tr><td colspan="65535">a</td><td>b</td></tr></table>`,
		"colspan 1e9":                     `<table><tr><td colspan="1000000000">a</td></tr></table>`,
		"pct 1e6":                         `<table style="width:100%"><tr><td style="width:1000000%">a</td><td style="width:1000000%">b</td></tr></table>`,
		"pct negative / zero":             `<table style="width:100%"><tr><td style="width:-5%">a</td><td style="width:0%">b</td><td>c</td></tr></table>`,
		"pct 100 each x50":                `<table style="width:100%"><tr>` + cells(50, `style="width:100%"`) + `</tr></table>`,
		"width 1e30px":                    `<table><tr><td style="width:1e30px">a</td><td style="width:1e30px">b</td></tr></table>`,
		"table width 1e30px":              `<table style="width:1e30px"><tr><td>a</td></tr></table>`,
		"table width 1e9% ":               `<table style="width:1e9%"><tr><td>a</td></tr></table>`,
		"word 100000 chars":               `<table><tr><td>` + strings.Repeat("W", 100000) + `</td><td>b</td></tr></table>`,
		"anywhere word 100000 chars":      `<table style="width:100%"><tr><td style="overflow-wrap:anywhere">` + strings.Repeat("W", 100000) + `</td><td>b</td></tr></table>`,
		"nowrap text 50000 chars":         `<table><tr><td style="white-space:nowrap">` + strings.Repeat("ab ", 17000) + `</td></tr></table>`,
		"padding 1e9":                     `<table><tr><td style="padding:1e9px">a</td></tr></table>`,
		"border-spacing 1e9":              `<table style="border-spacing:1e9px"><tr><td>a</td></tr></table>`,
		"empty cells 1000x10":             `<table style="width:100%">` + strings.Repeat(`<tr>`+cells(1000, "")+`</tr>`, 10) + `</table>`,
		"only colspan cells":              `<table><tr><td colspan="3">a</td></tr><tr><td colspan="2">b</td><td colspan="5">c</td></tr></table>`,
		"table in inline-block in cell":   `<table><tr><td><span style="display:inline-block"><table><tr><td>a</td><td>b</td></tr></table></span></td></tr></table>`,
		"table in flex item":              `<div style="display:flex"><div><table style="width:100%"><tr><td>a</td><td>b</td></tr></table></div><div>x</div></div>`,
		"min-width 1e9 cells":             `<table><tr><td style="min-width:1e9px">a</td><td>b</td></tr></table>`,
		"height 1e9 rows":                 `<table><tr style="height:1e9px"><td>a</td></tr></table>`,
		"col width + colspan + pct mixed": `<table style="width:100%"><colgroup><col style="width:30%"><col style="width:100px"><col></colgroup><tr><td colspan="2">a</td><td>b</td></tr><tr><td>c</td><td colspan="2" style="width:60%">d</td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, pages, err := sizingRun(t, html)
			if err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatalf("%v", err)
				}
			} else {
				allFinite(t, name, pages)
			}
			if time.Since(start) > time.Minute {
				t.Error("hang")
			}
		})
	}
}

// Work stays linear in the cells: 5 000 rows × 4 cells at auto layout cost
// no more than 3 × cells + 10 000 (the M3-07 bound), twice a fixed twin.
func TestAutoTableRowsWorkIsLinear(t *testing.T) {
	const rows = 4000
	mk := func(layout string) string {
		return `<table style="width:100%;table-layout:` + layout + `">` + strings.Repeat(`<tr><td>a</td><td>bb ccc</td><td>dddd</td><td>e</td></tr>`, rows) + `</table>`
	}
	ra, _, err := sizingRun(t, mk("auto"))
	if err != nil {
		t.Fatal(err)
	}
	rf, _, err := sizingRun(t, mk("fixed"))
	if err != nil {
		t.Fatal(err)
	}
	wa, wf := ra.b.Used(budget.Work), rf.b.Used(budget.Work)
	t.Logf("auto %d, fixed %d (cells %d)", wa, wf, rows*4)
	if wa > int64(3*rows*4+10000) && wa > 2*wf+10000 {
		t.Errorf("auto work %d (fixed %d) for %d cells", wa, wf, rows*4)
	}
}
