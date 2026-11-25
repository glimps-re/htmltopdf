package htmltopdf

import (
	"fmt"
	"strings"
	"testing"
)

// A table cell in columns whose lines are 5e5 px tall (more than a page each) keeps every
// word of its text in the PDF: the cell is laid out apart, on a page as tall as
// any author length can make it.
func TestTallColumnsInATableCellKeepTheirTextInThePDF(t *testing.T) {
	var words []string
	for i := range 12 {
		words = append(words, fmt.Sprintf("COLWORD%02d", i))
	}
	o := m2Options(t)
	o.PageSize = PageSize{Width: 19200, Height: 19200}
	o.Limits.MaxPages = 100000
	r := h4Render(t, o, `<table><tr><td style="columns:2;width:1px;line-height:5e5px">`+strings.Join(words, " ")+`</td></tr></table>`, false)
	if r.err != nil {
		t.Fatalf("render: %v", r.err)
	}
	text := pagesText(r.doc)
	for _, w := range words {
		if n := strings.Count(text, w); n != 1 {
			t.Errorf("%q occurs %d times in the PDF text, want once", w, n)
		}
	}
}

// A table cell of a million short lines is refused by a limit (the glyphs or the
// pages of the render) under the default limits: it never ends in a PDF whose
// text lies off the pages.
func TestMillionLinesInATableCellEndInALimit(t *testing.T) {
	if testing.Short() {
		t.Skip("about 2 s and 110 MiB of heap")
	}
	r := h4Render(t, m2Options(t), `<table><tr><td style="width:1px;font-size:8pt">`+strings.Repeat("a ", 1000000)+`</td></tr></table>`, true)
	if !h4IsLimit(r.err, LimitGlyphs) && !h4IsLimit(r.err, LimitPages) {
		t.Fatalf("error %v, want a MaxGlyphs or MaxPages limit", r.err)
	}
	if r.peak > m3HeapCeil {
		t.Errorf("peak heap %d MiB above the %d MiB ceiling of a hostile row", r.peak>>20, m3HeapCeil>>20)
	}
	t.Logf("%v; work %d, glyphs %d, peak heap %d MiB", r.err, r.work, r.glyphs, r.peak>>20)
}
