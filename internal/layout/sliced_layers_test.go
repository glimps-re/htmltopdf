package layout

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
)

// Geometry of the sliced containers, in px: a container of tallH holds the
// containing block (cbH) at its top, or at its bottom after nestedSpacer; a
// clip of clipH holds 40 fillers of fillerH and, at its bottom, the containing
// block after clipSpacer.
const (
	tallH        = 3000
	cbH          = 10
	nestedSpacer = tallH - 2*cbH // 2980
	clipH        = tallH - cbH   // 2990
	fillerH      = 20
	clipSpacer   = clipH - 40*fillerH - 2*cbH // 2170
)

// An absolute box whose containing block is sliced over pages (a line taller
// than the page, a flex or grid band, a table row), or lies in a box of it that
// another page holds, is emitted exactly once, even when its top lies above the
// first slice or below the last one: it is never silently dropped, and paint
// reports it off the page. The tops of each case reach the branches of the cut:
// the window of the containing block, another window, above the first slice,
// below the last, and inside or outside a clip.
func TestSlicedContainerKeepsItsLayers(t *testing.T) {
	const word = "zq"
	px := func(v int) string { return strconv.Itoa(v) + "px" }
	containers := []struct{ name, html string }{
		{"line", `<p>a <span style="display:inline-block;height:Hpx">X</span></p>`},
		{"flex", `<div style="display:flex"><div style="height:Hpx;position:relative">X</div></div>`},
		{"grid", `<div style="display:grid"><div style="height:Hpx;position:relative">X</div></div>`},
		{"table", `<table><tr><td style="height:Hpx">X</td></tr></table>`},
	}
	const layer = `b<i style="position:absolute;top:T">zq</i>`
	cb := `<div style="position:relative;height:` + px(cbH) + `">` + layer + `</div>`
	filler := strings.Repeat(`<div style="height:`+px(fillerH)+`">y</div>`, 40)
	clip := `<div style="overflow:hidden;height:` + px(clipH) + `">`
	// inside are the tops that put the layer inside the clip of an overflow:
	// hidden ancestor, where it must be drawn; outside it, the clip may hide it.
	inners := []struct {
		name, html   string
		tops, inside []string
	}{
		{name: "direct", html: layer, tops: []string{"-50000px", "-400px", "1500px", "50000px"}},
		{name: "nested at top", html: cb, tops: []string{"-400px", "150px", "1500px", "50000px"}},
		{
			name: "nested at bottom", html: `<div style="height:` + px(nestedSpacer) + `"></div>` + cb,
			tops: []string{"-50000px", "-2500px", "-1500px", "50000px"},
		},
		{
			name: "hidden, CB at top", html: clip + cb + filler + `</div>`,
			tops: []string{"-400px", "700px", "1500px", "50000px"}, inside: []string{"700px", "1500px"},
		},
		{
			name: "hidden, CB at bottom", html: clip + filler + `<div style="height:` + px(clipSpacer) + `"></div>` + cb + `</div>`,
			tops: []string{"-50000px", "-1500px", "-400px", "50000px"}, inside: []string{"-1500px", "-400px"},
		},
	}
	type page struct{ name, css string }
	a4, small := page{"A4", ""}, page{"400x300", tallAtomPage}
	for _, c := range containers {
		pages := []page{small}
		if c.name == "table" {
			pages = append(pages, a4)
		}
		for _, pg := range pages {
			for _, h := range []int{290, tallH} {
				for _, in := range inners {
					for _, top := range in.tops {
						t.Run(pg.name+"/"+c.name+"/"+px(h)+"/"+in.name+"/"+top, func(t *testing.T) {
							html := strings.NewReplacer("Hpx", px(h), "X", strings.Replace(in.html, "top:T", "top:"+top, 1)).Replace(c.html)
							root, pages := slicedLayersRun(t, pg.css, html)
							if lost := TextLost(root, pages); lost != "" {
								t.Errorf("text lost: %s", lost)
							}
							n := countDrawn(pages, word)
							switch {
							case in.inside != nil && !slices.Contains(in.inside, top):
								if n > 1 {
									t.Errorf("%q emitted %d times, want at most 1", word, n)
								}
							case n != 1:
								t.Errorf("%q emitted %d times, want 1", word, n)
							}
						})
					}
				}
			}
		}
	}
}

// A sliced table row whose containing block is on its first page and whose
// absolute box is on the second emits that box once. Named regression of
// TestSlicedContainerKeepsItsLayers (A4/table/3000px/nested at top/1500px),
// with the slicing over 3 pages checked.
func TestSlicedRowKeepsLayerOfEarlierBox(t *testing.T) {
	root, pages := slicedLayersRun(t, "", `<table><tr><td style="height:3000px"><div style="position:relative;height:10px">b<i style="position:absolute;top:1500px">zq</i></div></td></tr></table>`)
	if len(pages) < 3 {
		t.Fatalf("%d pages, want the row sliced over 3", len(pages))
	}
	if lost := TextLost(root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if n := countDrawn(pages, "zq"); n != 1 {
		t.Errorf("%q emitted %d times, want 1", "zq", n)
	}
}

// slicedLayersRun lays out html on every page; a limit fails the test.
func slicedLayersRun(t *testing.T, pageCSS, html string) (*box.Box, []*Page) {
	t.Helper()
	r := newRun(t, html, opts{extra: pageCSS, limits: func(l *budget.Limits) { l[budget.Pages] = hostileMaxPages }})
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			return r.root, pages
		}
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, pg)
	}
}

// countDrawn counts the text items of the pages that hold word.
func countDrawn(pages []*Page, word string) int {
	n := 0
	for _, pg := range pages {
		for _, it := range pg.Items {
			if tx, ok := it.(*display.Text); ok && strings.Contains(tx.Text, word) {
				n++
			}
		}
	}
	return n
}
