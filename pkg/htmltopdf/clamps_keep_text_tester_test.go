package htmltopdf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
)

// requireOnce fails the test when text occurs other than once in the PDF text.
func requireOnce(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if n := strings.Count(text, want); n != 1 {
			t.Errorf("%q occurs %d times in the PDF text, want once", want, n)
		}
	}
}

// Cells past column 1000 keep their text: each text of the row is in the PDF
// exactly once, with the clamping warning. The fixed 100%-wide layout keeps the
// kept columns on the page: a cell past the page edge is off the page, not
// clamped.
func TestClampsKeepTableCellText(t *testing.T) {
	var thousandOne strings.Builder
	var want1001 []string
	for i := range 1001 {
		fmt.Fprintf(&thousandOne, "<td>CELL%04d</td>", i)
		want1001 = append(want1001, fmt.Sprintf("CELL%04d", i))
	}
	const fixed = `<style>table.t{table-layout:fixed;width:100%;border-spacing:0}td{padding:0}</style>`
	tests := []struct {
		name, html string
		texts      []string
	}{
		{"decoy colspan 1000 then one cell", `<table class=t><tr><td colspan=1000>decoy</td><td>PHISHTEXT</td></tr></table>`, []string{"decoy", "PHISHTEXT"}},
		{"1001 single cells", "<table class=t><tr>" + thousandOne.String() + "</tr></table>", want1001},
		{"colspan 1000 then 5 cells", `<table class=t><tr><td colspan=1000>decoy</td><td>PHISHA</td><td>PHISHB</td><td>PHISHC</td><td>PHISHD</td><td>PHISHE</td></tr></table>`, []string{"decoy", "PHISHA", "PHISHB", "PHISHC", "PHISHD", "PHISHE"}},
		{"nested table in a moved cell", `<table class=t><tr><td colspan=1000>decoy</td><td><table class=t><tr><td>NESTEDTEXT</td></tr></table></td></tr></table>`, []string{"decoy", "NESTEDTEXT"}},
		{"moved cell with a colspan", `<table class=t><tr><td colspan=1000>decoy</td><td colspan=3>SPANNED</td><td>AFTER</td></tr></table>`, []string{"decoy", "SPANNED", "AFTER"}},
		{"second row keeps its cells", `<table class=t><tr><td colspan=1000>decoy</td><td>MOVEDA</td></tr><tr><td>ROW2A</td><td>ROW2B</td></tr></table>`, []string{"decoy", "MOVEDA", "ROW2A", "ROW2B"}},
		{"moved cell with an inline absolute box", `<table class=t><tr><td colspan=1000>decoy</td><td>CELLTEXT<span style="position:absolute;top:5px">PHISHABS</span></td></tr></table>`, []string{"decoy", "CELLTEXT", "PHISHABS"}},
		{"moved cell holding only an inline absolute box", `<table class=t><tr><td colspan=1000>decoy</td><td><span style="position:absolute;top:5px">PHISHONLY</span></td></tr></table>`, []string{"decoy", "PHISHONLY"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := m3Render(t, fixed+tc.html)
			requireOnce(t, pagesText(r.doc), tc.texts...)
			if !m3Clamped(r.res, box.ClampedColumns) {
				t.Errorf("no %q clamping warning: %+v", box.ClampedColumns, r.res.Warnings)
			}
			r.balanced(t)
		})
	}
}

// Fixed boxes past the per-page cap stop repeating on the next pages and stay on
// their first page: every text is still in the PDF.
func TestClampsKeepFixedBoxText(t *testing.T) {
	var fixed strings.Builder
	for i := range 4100 {
		fmt.Fprintf(&fixed, `<div style="position:fixed;top:0;left:0">FIX%04d</div>`, i)
	}
	r := m3Render(t, `<style>body{margin:0}</style><div style="height:3000px">flow</div>`+fixed.String())
	text := pagesText(r.doc)
	for i := range 4100 {
		if want := fmt.Sprintf("FIX%04d", i); !strings.Contains(text, want) {
			t.Fatalf("%q is not in the PDF text", want)
		}
	}
	if !m3Clamped(r.res, "fixed boxes") {
		t.Errorf("no \"fixed boxes\" clamping warning: %+v", r.res.Warnings)
	}
	r.balanced(t)
}

// A font size beyond the bound is clamped and a tiny one is kept: the text is
// drawn either way.
func TestClampsKeepTextOfOutOfRangeFontSizes(t *testing.T) {
	huge := m3Render(t, `<p style="font-size:1e9px">HUGETEXT</p>`)
	requireOnce(t, pagesText(huge.doc), "HUGETEXT")
	if !m3Clamped(huge.res, "font-size") {
		t.Errorf("no \"font-size\" clamping warning: %+v", huge.res.Warnings)
	}
	tiny := m3Render(t, `<p style="font-size:0.001px">TINYTEXT</p>`)
	requireOnce(t, pagesText(tiny.doc), "TINYTEXT")
}

// A text of more than 65 536 script changes is shaped to its end.
func TestClampsKeepTextOfManyScriptChanges(t *testing.T) {
	r := m3Render(t, `<p style="word-break:break-all">`+strings.Repeat("aα", 35000)+`</p>`)
	text := pagesText(r.doc)
	if a, g := strings.Count(text, "a"), strings.Count(text, "α"); a != 35000 || g != 35000 {
		t.Errorf("%d latin and %d greek letters in the PDF text, want 35000 each", a, g)
	}
	if !m3Clamped(r.res, "spans") {
		t.Errorf("no \"spans\" clamping warning: %+v", r.res.Warnings)
	}
}

// The tracks of a grid beyond the 1000 kept go on in further rows: no item is
// lost. Tracks of 0.4px keep the first row on the page.
func TestClampsKeepGridItems(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<style>body{margin:0}.g{display:grid;grid-template-columns:repeat(1500,0.4px)}.g>div{font-size:1px}</style><div class=g>`)
	for i := range 1500 {
		fmt.Fprintf(&sb, "<div>ITEM%04d</div>", i)
	}
	sb.WriteString("</div>")
	r := m3Render(t, sb.String())
	text := pagesText(r.doc)
	for i := range 1500 {
		requireOnce(t, text, fmt.Sprintf("ITEM%04d", i))
	}
	if !m3Clamped(r.res, "grid tracks") {
		t.Errorf("no \"grid tracks\" clamping warning: %+v", r.res.Warnings)
	}
}
