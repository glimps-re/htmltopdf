package layout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/style"
)

// The M1-16 golden criterion in one table: 6 columns over exactly 3 pages, the
// header group repeated on each, a colspan cell and a border conflict (a 4 px
// blue header edge against 2 px red cell edges: the wider one wins).
func TestTableCriterionGolden(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<table style="width:600px;border-collapse:collapse"><thead><tr>`)
	for c := range 6 {
		fmt.Fprintf(&sb, `<th style="border:4px solid blue">h%d`, c)
	}
	sb.WriteString(`</thead><tbody>`)
	for i := range 12 {
		sb.WriteString("<tr>")
		if i == 3 {
			sb.WriteString(`<td colspan=3 style="border:2px solid red">span3<td style="border:2px solid red">c3<td style="border:2px solid red">c4<td style="border:2px solid red">c5`)
			continue
		}
		for c := range 6 {
			fmt.Fprintf(&sb, `<td style="border:2px solid red">r%dc%d`, i, c)
		}
	}
	sb.WriteString(`</tbody></table>`)
	r := newRun(t, sb.String(), opts{css: tableDoc + " td,th{line-height:100px;border-spacing:0}"})
	pages := r.all(t)
	if len(pages) != 3 {
		t.Fatalf("%d pages, want 3", len(pages))
	}
	rowFragments := 0
	for i, pg := range pages {
		heads := fragsOf([]*Page{pg}, "thead")
		if len(heads) != 1 {
			t.Errorf("page %d: %d header groups, want the header repeated", i+1, len(heads))
		}
		rows := fragsOf([]*Page{pg}, "tr")
		rowFragments += len(rows)
		for _, row := range rows {
			if row.rect.Y+row.rect.H > 600.01 {
				t.Errorf("page %d: row %v leaves the page", i+1, row.rect)
			}
		}
	}
	if rowFragments != 13+2 {
		t.Errorf("%d row fragments, want 15 (13 rows + 2 repeated headers)", rowFragments)
	}
	// The colspan cell spans three columns of 100 px.
	var span *frag
	for _, td := range fragsOf(pages, "td") {
		if td.box.ColSpan == 3 {
			span = td
		}
	}
	if span == nil || span.rect.W < 299 || span.rect.W > 301 {
		t.Errorf("colspan cell: %+v", span)
	}
	// Border conflict: below a header cell the shared edge is the blue one, 4 px
	// wide, half drawn by each side.
	var th, td *frag
	th = fragsOf(pages[:1], "th")[0]
	td = fragsOf(pages[:1], "td")[0]
	if th.borderColor[style.Bottom].B != 1 || td.borderColor[style.Top].B != 1 || th.border[style.Bottom] != 2 || td.border[style.Top] != 2 {
		t.Errorf("shared edge: th bottom %v %v, td top %v %v", th.border[style.Bottom], th.borderColor[style.Bottom], td.border[style.Top], td.borderColor[style.Top])
	}
	golden(t, "table-criterion", pages)
}
