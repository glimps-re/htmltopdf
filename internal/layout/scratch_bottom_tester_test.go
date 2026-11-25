package layout

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
)

// scratchRun lays html out on 19200px pages with the page and work limits
// raised: the boxes below are as tall as the length clamp allows.
func scratchRun(t *testing.T, html string) (*run, []*Page) {
	t.Helper()
	r := newRun(t, html, opts{extra: "@page { size: 19200px 19200px; margin: 0 }", limits: func(l *budget.Limits) {
		l[budget.Pages] = 100000
		l[budget.Work] = 1 << 31
	}})
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			return r, pages
		}
		if err != nil {
			t.Fatalf("the render fails: %v", err)
		}
		pages = append(pages, pg)
	}
}

// textOnPages is the text of the items whose baseline lies inside the area of
// their page: text below it is not drawn.
func textOnPages(pages []*Page) string {
	var sb strings.Builder
	for _, pg := range pages {
		for _, it := range pg.Items {
			if x, ok := it.(*display.Text); ok && x.Baseline >= 0 && x.Baseline <= pg.Area.H/pg.Scale {
				sb.WriteString(x.Text)
			}
		}
	}
	return sb.String()
}

// wordsOf lists the words of the concatenated item texts of a document, whose
// words are "w0".."w11" (item boundaries are word boundaries).
func wordsOf(pages []*Page, onPage bool) []string {
	var out []string
	for _, pg := range pages {
		for _, it := range pg.Items {
			if x, ok := it.(*display.Text); ok && (!onPage || (x.Baseline >= 0 && x.Baseline <= pg.Area.H/pg.Scale)) {
				out = append(out, x.Text)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Everything laid out apart (cells, flex and grid items, atoms, absolute boxes,
// columns) is laid out whole whatever the author's lengths: a box one pixel wide
// whose line height is at the length clamp keeps every word of its text in the
// fragment tree and emits it in order. A line box taller than its page is the
// documented author exception: its place on a page is not checked (twelve lines
// of 1e7 px are more than the pages of a render can hold).
func TestTallLinesKeepEveryWordLaidOutApart(t *testing.T) {
	tallLines(t, "1e7px", false)
}

// With lines shorter than the page (5e3 px on 19200 px pages) every word is also
// drawn inside the area of a page. An absolute box is not paginated: its text
// is emitted wherever it is.
func TestShortTallLinesAreDrawnOnAPage(t *testing.T) {
	tallLines(t, "5e3px", true)
}

func tallLines(t *testing.T, lineHeight string, onPage bool) {
	t.Helper()
	var words []string
	for i := range 12 {
		words = append(words, fmt.Sprintf("w%d", i))
	}
	text := strings.Join(words, " ")
	for _, avoid := range []string{"", ";break-inside:avoid"} {
		tall := "width:1px;line-height:" + lineHeight + avoid
		cases := []struct {
			name, html string
			paginated  bool
		}{
			{"absolute box", `<div style="position:relative"><div style="position:absolute;` + tall + `">` + text + `</div></div>`, false},
			{"table cell", `<table><tr><td style="` + tall + `">` + text + `</td></tr></table>`, true},
			{"flex item", `<div style="display:flex"><div style="` + tall + `">` + text + `</div></div>`, true},
			{"grid item", `<div style="display:grid"><div style="` + tall + `">` + text + `</div></div>`, true},
			{"inline-block", `<p><span style="display:inline-block;` + tall + `">` + text + `</span></p>`, true},
			{"columns in a table cell", `<table><tr><td style="columns:2;` + tall + `">` + text + `</td></tr></table>`, true},
		}
		for _, tc := range cases {
			t.Run(tc.name+avoid, func(t *testing.T) {
				r, pages := scratchRun(t, tc.html)
				if lost := TextLost(r.root, pages); lost != "" {
					t.Errorf("text lost: %s", lost)
				}
				want := slices.Clone(words)
				slices.Sort(want)
				if got := wordsOf(pages, false); !slices.Equal(got, want) {
					t.Errorf("the words emitted are %v, want each of %v once", got, want)
				}
				if got := wordsOf(pages, true); onPage && tc.paginated && !slices.Equal(got, want) {
					t.Errorf("the words drawn inside a page area are %v, want each of %v once", got, want)
				}
			})
		}
	}
}

// Eight inline-blocks of the maximum height in a box one pixel wide are all kept
// (an absolute box is not paginated: its text is emitted wherever it is).
func TestTallInlineBlocksInANarrowAbsoluteBoxAreAllKept(t *testing.T) {
	var sb strings.Builder
	for i := range 8 {
		fmt.Fprintf(&sb, `<span style="display:inline-block;height:1e7px">BLOCK%d</span> `, i)
	}
	r, pages := scratchRun(t, `<div style="position:relative"><div style="position:absolute;width:1px">`+sb.String()+`</div></div>`)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if got := textOf(pages); strings.Count(got, "BLOCK") != 8 {
		t.Errorf("not all 8 blocks are emitted: %q", got)
	}
}

// Nested boxes whose bottom padding and border are at the length clamp, in a
// table cell, do not push the text that follows them below the page.
func TestTallBottomEdgesInATableCellKeepTheTextAfterThem(t *testing.T) {
	const edge = `padding-bottom:1e7px;border-bottom:1e7px solid red`
	const depth = 8
	r, pages := scratchRun(t, `<table><tr><td>`+strings.Repeat(`<div style="`+edge+`">`, depth)+`INNER`+strings.Repeat("</div>", depth)+`SIBLING</td></tr></table>`)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if got := textOnPages(pages); !strings.Contains(got, "INNER") || !strings.Contains(got, "SIBLING") {
		t.Errorf("INNER and SIBLING are not both drawn on a page: %q", got)
	}
}

// An atom whose padding, border, height and vertical-align chain to the length
// clamp, in a table cell, keeps itself and the text after it.
func TestTallAtomInATableCellKeepsTheTextAfterIt(t *testing.T) {
	const tall = "padding:1e7px 0;border:1e7px solid red;height:1e7px;vertical-align:-1e7px"
	r, pages := scratchRun(t, `<table><tr><td><p>before <span style="display:inline-block;`+tall+`">ATOM</span> ENDTEXT</p></td></tr></table>`)
	got := textOnPages(pages)
	for _, w := range []string{"before", "ATOM", "ENDTEXT"} {
		if !strings.Contains(got, w) {
			t.Errorf("%q is not drawn on a page: %q", w, got)
		}
	}
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
}

// A flex item of 1100 lines of 1e4 px (shorter than the page) draws every line
// inside a page area.
func TestManyShortLinesInAFlexItemAreDrawnOnAPage(t *testing.T) {
	var words []string
	for i := range 1100 {
		words = append(words, fmt.Sprintf("L%04d", i))
	}
	r, pages := scratchRun(t, `<div style="display:flex"><div style="width:1px;line-height:1e4px">`+strings.Join(words, " ")+`</div></div>`)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	want := slices.Clone(words)
	slices.Sort(want)
	if got := wordsOf(pages, true); !slices.Equal(got, want) {
		t.Errorf("%d of %d lines drawn inside a page area (%d emitted)", len(got), len(want), len(wordsOf(pages, false)))
	}
}

// Nine blocks of the maximum height in three columns of a table cell, then a
// last paragraph: all of them, ENDTEXT included, are drawn on a page.
func TestTallBlocksInColumnsOfATableCellAreDrawnOnAPage(t *testing.T) {
	var sb strings.Builder
	var words []string
	for i := range 9 {
		fmt.Fprintf(&sb, `<div style="height:1e7px">BLOCK%d</div>`, i)
		words = append(words, fmt.Sprintf("BLOCK%d", i))
	}
	words = append(words, "ENDTEXT")
	r, pages := scratchRun(t, `<table><tr><td style="columns:3">`+sb.String()+`<p>ENDTEXT</p></td></tr></table>`)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	want := slices.Clone(words)
	slices.Sort(want)
	if got := wordsOf(pages, true); !slices.Equal(got, want) {
		t.Errorf("drawn inside a page area: %v, want %v (emitted %v)", got, want, wordsOf(pages, false))
	}
}

// Text after tall boxes in a table cell stays inside the page: a paragraph that
// avoids a break after three blocks of the maximum height, and a paragraph after
// a bottom padding and a bottom border at the maximum, with normal lines and with
// lines taller than the page (the author exception: only emitted, not placed).
func TestTextAfterTallBoxesInACellIsDrawnOnAPage(t *testing.T) {
	cases := []struct {
		name, html string
		words      []string
		tallLines  bool
	}{
		{"avoid paragraph after three tall blocks", `<table><tr><td><div style="height:1e7px">TALLA</div><div style="height:1e7px">TALLB</div><div style="height:1e7px">TALLC</div><p style="break-inside:avoid">PARA</p></td></tr></table>`, []string{"TALLA", "TALLB", "TALLC", "PARA"}, false},
		{"avoid paragraph of tall lines after three tall blocks", `<table><tr><td><div style="height:1e7px">TALLA</div><div style="height:1e7px">TALLB</div><div style="height:1e7px">TALLC</div><p style="break-inside:avoid;line-height:1e7px">PARA</p></td></tr></table>`, []string{"TALLA", "TALLB", "TALLC", "PARA"}, true},
		{"paragraph after a bottom padding and border", `<table><tr><td><div style="padding-bottom:1e7px;border-bottom:1e7px solid red">BOX</div><p>PARA</p></td></tr></table>`, []string{"BOX", "PARA"}, false},
		{"paragraph of tall lines after a bottom padding and border", `<table><tr><td><div style="padding-bottom:1e7px;border-bottom:1e7px solid red">BOX</div><p style="line-height:1e7px">PARA</p></td></tr></table>`, []string{"BOX", "PARA"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, pages := scratchRun(t, tc.html)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			want := slices.Clone(tc.words)
			slices.Sort(want)
			if got := wordsOf(pages, !tc.tallLines); !slices.Equal(got, want) {
				t.Errorf("drawn: %v, want %v (emitted %v)", got, want, wordsOf(pages, false))
			}
		})
	}
}

// Forty words in boxes whose height is fixed and whose columns are narrow keep
// every word on a page: the columns overflow into more pages, never into
// nothing.
func TestFixedHeightColumnsKeepEveryWordOnAPage(t *testing.T) {
	var words []string
	for i := range 40 {
		words = append(words, fmt.Sprintf("c%02d", i))
	}
	text := strings.Join(words, " ")
	const box = "height:40px;columns:2;width:60px"
	cases := []struct{ name, html string }{
		{"table cell", `<table><tr><td style="` + box + `">` + text + `</td></tr></table>`},
		{"flex item", `<div style="display:flex"><div style="` + box + `">` + text + `</div></div>`},
		{"inline-block", `<p><span style="display:inline-block;` + box + `">` + text + `</span></p>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, pages := scratchRun(t, tc.html)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			want := slices.Clone(words)
			slices.Sort(want)
			if got := wordsOf(pages, true); !slices.Equal(got, want) {
				t.Errorf("%d of %d words drawn inside a page area (%d emitted)", len(got), len(want), len(wordsOf(pages, false)))
			}
		})
	}
}

// Columns whose lines are taller than the page (3e6 px on 19200 px pages) are the
// author exception: nothing is lost in the fragment tree, their place on a page
// is not checked.
func TestTallLinesInThreeColumnsOfACellAreNotLost(t *testing.T) {
	var words []string
	for i := range 40 {
		words = append(words, fmt.Sprintf("t%02d", i))
	}
	r, pages := scratchRun(t, `<table><tr><td><div style="columns:3;line-height:3e6px;width:30px">`+strings.Join(words, " ")+`</div></td></tr></table>`)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	want := slices.Clone(words)
	slices.Sort(want)
	if got := wordsOf(pages, false); !slices.Equal(got, want) {
		t.Errorf("%d of %d words emitted", len(got), len(want))
	}
}
