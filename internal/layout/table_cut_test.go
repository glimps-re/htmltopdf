package layout

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestRowWithCenteredCellsIsCutLikeATopAlignedRow(t *testing.T) {
	// The cells have different line counts and td is vertically centered by
	// default: their line boxes sit half a line apart, so the row only splits
	// if a row that breaks is laid out top-aligned (as Chrome does).
	long := strings.Repeat("alpha ", 15)
	html := `<div style="padding-top:80px"></div>` +
		`<table style="table-layout:fixed;width:300px;border-collapse:collapse"><tr>` +
		`<td style="width:100px;padding:0">` + long + `</td><td style="width:100px;padding:0">a<br>b<br>c<br>d</td><td style="padding:0">x</td></tr></table>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 140px; margin: 10px }", css: "body { margin: 0 } td { font-size: 10px; line-height: 12px }"})
	pages := r.all(t)
	if len(pages) < 2 {
		t.Fatalf("%d pages: the row was not split", len(pages))
	}
	first := linesOf(pages[0])
	if len(first) == 0 {
		t.Fatal("no line of the row on the first page: the table moved whole")
	}
	for _, pg := range pages[1:] {
		for _, ln := range linesOf(pg) {
			if ln.rect.Bottom() > pg.Area.H/pg.Scale+0.01 {
				t.Errorf("line ends at %v below the page area", ln.rect.Bottom())
			}
		}
	}
}

// cutTable is a 5-line cell next to a centered 4-line one, 12 px lines;
// spaceBefore moves the table down the 120 px page area.
func cutTable(t *testing.T, spaceBefore int) *run {
	t.Helper()
	html := `<div style="padding-top:` + strconv.Itoa(spaceBefore) + `px"></div>` +
		`<table style="table-layout:fixed;width:300px;border-collapse:collapse"><tr>` +
		`<td style="width:100px;padding:0">` + strings.Repeat("alpha ", 15) + `</td>` +
		`<td style="width:100px;padding:0">a<br>b<br>c<br>d</td><td style="padding:0">x</td></tr></table>`
	return newRun(t, html, opts{extra: "@page { size: 400px 140px; margin: 10px }", css: "body { margin: 0 } td { font-size: 10px; line-height: 12px }"})
}

func lineYOf(pg *Page, text string) (float64, bool) {
	for _, ln := range linesOf(pg) {
		for _, it := range ln.items {
			if it.text != nil && strings.TrimSpace(it.text.Text) == text {
				return ln.rect.Y, true
			}
		}
	}
	return 0, false
}

func TestSplitRowHasItsCellsTopAlignedOnBothHalves(t *testing.T) {
	pages := cutTable(t, 80).all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	for i, pg := range pages {
		first := linesOf(pg)[0].rect.Y
		for _, ln := range linesOf(pg) {
			if off := ln.rect.Y - first; math.Mod(off, 12) > 0.01 && math.Mod(off, 12) < 11.99 {
				t.Errorf("page %d: a line at +%.1f is not on the 12 px grid of the top-aligned cells", i+1, off)
			}
		}
	}
	if ya, ok := lineYOf(pages[0], "a"); !ok || !near(ya, linesOf(pages[0])[0].rect.Y) {
		t.Errorf("the centered cell starts at %v on the first page, want the row top", ya)
	}
}

func TestRowThatFitsKeepsItsVerticalAlignment(t *testing.T) {
	pages := cutTable(t, 20).all(t)
	if len(pages) != 1 {
		t.Fatalf("%d pages: the row must fit", len(pages))
	}
	ya, ok := lineYOf(pages[0], "a")
	if !ok {
		t.Fatal("no line `a`")
	}
	if top := linesOf(pages[0])[0].rect.Y; !near(ya, top+6) {
		t.Errorf("centered cell starts at %v, want %v (6 px below the row top)", ya, top+6)
	}
}

func TestSplitRowKeepsTheTopAlignedFragmentForTheNextPage(t *testing.T) {
	r := cutTable(t, 80)
	if _, err := r.pag.Next(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range r.pag.l.tables {
		if tl.cur != nil && tl.cur.top && tl.cur.from > 0 {
			found = true
		}
	}
	if !found {
		t.Error("the table layout does not hold the top-aligned row to resume")
	}
}

func TestFittingLastRowWithSpacingStaysCentered(t *testing.T) {
	// The row only "does not fit" once the border spacing after the last row is
	// counted; it is then not cut and must keep its alignment.
	for _, d := range []int{20, 44, 47, 50, 53} {
		html := `<div style="padding-top:` + strconv.Itoa(d) + `px"></div>` +
			`<table style="border-spacing:0 10px"><tr><td>a<br>b<br>c</td><td>x</td></tr></table>`
		r := newRun(t, html, opts{extra: "@page { size: 300px 100px; margin: 0 }", css: "body { margin: 0 } td { padding: 0; font-size: 10px; line-height: 12px }"})
		pages := r.all(t)
		if len(pages) != 1 {
			t.Fatalf("D=%d: %d pages, the row must not be cut", d, len(pages))
		}
		ya, _ := lineYOf(pages[0], "a")
		yx, ok := lineYOf(pages[0], "x")
		if !ok || !near(yx-ya, 12) {
			t.Errorf("D=%d: x at +%.1f from the first line, want 12 (centered)", d, yx-ya)
		}
	}
}

func TestCutRowTopAlignsBottomAlignedCellsToo(t *testing.T) {
	pages := cutTableWith(t, 80, "vertical-align:bottom").all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	ya, ok := lineYOf(pages[0], "a")
	if !ok || !near(ya, linesOf(pages[0])[0].rect.Y) {
		t.Errorf("the bottom-aligned cell starts at %v, want the row top %v", ya, linesOf(pages[0])[0].rect.Y)
	}
}

func cutTableWith(t *testing.T, spaceBefore int, style2 string) *run {
	t.Helper()
	html := `<div style="padding-top:` + strconv.Itoa(spaceBefore) + `px"></div>` +
		`<table style="table-layout:fixed;width:300px;border-collapse:collapse"><tr>` +
		`<td style="width:100px;padding:0">` + strings.Repeat("alpha ", 15) + `</td>` +
		`<td style="width:100px;padding:0;` + style2 + `">a<br>b<br>c<br>d</td><td style="padding:0">x</td></tr></table>`
	return newRun(t, html, opts{extra: "@page { size: 400px 140px; margin: 10px }", css: "body { margin: 0 } td { font-size: 10px; line-height: 12px }"})
}

func TestRowCutOverManyPagesIsLaidOutTopAlignedOnce(t *testing.T) {
	html := `<table style="table-layout:fixed;width:300px;border-collapse:collapse"><tr>` +
		`<td style="width:100px;padding:0">` + strings.Repeat("alpha ", 120) + `</td>` +
		`<td style="width:100px;padding:0">a<br>b<br>c<br>d</td><td style="padding:0">x</td></tr></table>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 80px; margin: 10px }", css: "body { margin: 0 } td { font-size: 10px; line-height: 12px }"})
	pages := r.all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages, the row must span 3 or more", len(pages))
	}
	for i, pg := range pages {
		for _, ln := range linesOf(pg) {
			if math.Mod(ln.rect.Y, 12) > 0.01 && math.Mod(ln.rect.Y, 12) < 11.99 {
				t.Errorf("page %d: line at y=%v is off the top-aligned 12 px grid", i+1, ln.rect.Y)
			}
		}
	}
}

func TestRepeatedHeaderOfACutTableStaysCentered(t *testing.T) {
	html := `<table style="table-layout:fixed;width:300px;border-collapse:collapse">` +
		`<thead><tr><th style="width:100px;padding:0">h1<br>h2</th><th style="width:100px;padding:0">h3</th><th style="padding:0">h4</th></tr></thead><tbody>` +
		strings.Repeat(`<tr><td style="padding:0">r<br>r<br>r</td><td style="padding:0">x</td><td style="padding:0">y</td></tr>`, 12) + `</tbody></table>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 100px; margin: 10px }", css: "body { margin: 0 } td, th { font-size: 10px; line-height: 12px }"})
	pages := r.all(t)
	if len(pages) < 2 {
		t.Fatalf("%d pages", len(pages))
	}
	h1, ok1 := lineYOf(pages[1], "h1")
	h3, ok3 := lineYOf(pages[1], "h3")
	if !ok1 || !ok3 || !near(h3-h1, 6) {
		t.Errorf("repeated header: h3 at +%.1f from h1, want 6 (centered)", h3-h1)
	}
}

func TestCutRowTopAlignsBaselineCellsToo(t *testing.T) {
	// Chrome probe (a row cut at the page bottom, baseline cells of 16px and
	// 30px text): the first lines start at the same top, not on a shared baseline.
	html := `<div style="padding-top:80px"></div>` +
		`<table style="border-collapse:collapse"><tr>` +
		`<td style="vertical-align:baseline;padding:0;font-size:10px;line-height:12px;width:100px">` + strings.Repeat("alpha ", 15) + `</td>` +
		`<td style="vertical-align:baseline;padding:0;font-size:20px;line-height:24px">B1<br>B2<br>B3</td></tr></table>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 140px; margin: 10px }", css: "body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	var tops []float64
	for _, ln := range linesOf(pages[0]) {
		tops = append(tops, ln.rect.Y)
	}
	yb, ok := lineYOf(pages[0], "B1")
	if !ok || !near(yb, tops[0]) {
		t.Errorf("B1 line at %v, want the row top %v (all tops: %v)", yb, tops[0], tops)
	}
}

func TestTopAlignedRowThatFitsIsPlacedWhenTheFirstLayoutDoesNot(t *testing.T) {
	// Baseline cells make the row taller than its top-aligned layout: the
	// 33 px row does not fit the 30 px left, the 24 px one does.
	html := `<div style="padding-top:90px"></div>` +
		`<table style="border-collapse:collapse"><tr>` +
		`<td style="vertical-align:baseline;padding:0;font-size:20px;line-height:24px;width:100px">A</td>` +
		`<td style="vertical-align:baseline;padding:0;font-size:10px;line-height:12px">b<br>c</td></tr></table>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 140px; margin: 10px }", css: "body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) != 1 {
		t.Fatalf("%d pages: the top-aligned row fits", len(pages))
	}
	h := pages[0].Area.H / pages[0].Scale
	for _, ln := range linesOf(pages[0]) {
		if ln.rect.Bottom() > h+0.01 {
			t.Errorf("line ends at %v below the page area %v", ln.rect.Bottom(), h)
		}
	}
}
