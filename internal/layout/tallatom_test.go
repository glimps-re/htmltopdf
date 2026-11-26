package layout

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const (
	tallAtomPage = "@page { size: 400px 300px; margin: 0 }"
	// hostileMaxPages stops a runaway pagination well before the page budget.
	hostileMaxPages = 200
)

// tallAtomContexts are the formatting contexts an atomic inline can sit in; X
// stands for the atom.
var tallAtomContexts = []struct{ name, html string }{
	{"inline", `<p>a X</p>`},
	{"float", `<div style="float:left"><p>a X</p></div>`},
	{"flex", `<div style="display:flex"><p>a X</p></div>`},
	{"table", `<table><tr><td><p>a X</p></td></tr></table>`},
	{"multicol", `<div style="columns:2"><p>a X</p></div>`},
}

// tallAtom returns the markup of an atom of kind svg, inline-block or img
// (an svg turned into an image box by asImages) of height h, with a red
// background to find it on the pages.
func tallAtom(kind, h string) string {
	switch kind {
	case "svg", "img":
		return `<svg width="50" height="` + h + `" style="background:#f00"></svg>`
	}
	return `<span style="display:inline-block;width:50px;height:` + h + `px;background:#f00">ib</span>`
}

func tallAtomRun(t *testing.T, kind, html string) []*Page {
	t.Helper()
	o := opts{extra: tallAtomPage}
	if kind == "img" {
		o.boxes = asImages(imgSource(t))
	}
	r := newRun(t, html, o)
	pages := r.all(t)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	return pages
}

var tallAtomRed = css.Color{R: 1, A: 1}

// clippedRed returns the number of pages that draw a red background starting
// inside the page area and reaching its bottom: an atom taller than the page,
// clipped by it.
func clippedRed(pages []*Page) int {
	n := 0
	for _, pg := range pages {
		h := pg.Area.H / pg.Scale
		for _, it := range pg.Items {
			if v, ok := it.(display.FillRect); ok && v.Color == tallAtomRed && v.Rect.Y >= 0 && v.Rect.Y < h && v.Rect.Y+v.Rect.H >= h-cutEps {
				n++
				break
			}
		}
	}
	return n
}

// wordsInArea counts the words drawn with their baseline inside the page area,
// over all pages, and returns the page (1-based) each was last seen on.
func wordsInArea(pages []*Page) (count map[string]int, page map[string]int) {
	count, page = map[string]int{}, map[string]int{}
	for i, pg := range pages {
		h := pg.Area.H / pg.Scale
		for _, w := range pageWords(pg) {
			// pageWords reports pt: 0.75 pt per px.
			if y := w.y / 0.75; y >= 0 && y <= h {
				count[w.text]++
				page[w.text] = i + 1
			}
		}
	}
	return count, page
}

// An svg, inline-block or image taller than the page area takes at most one
// page more than the line it is on, in every context: it is placed whole and
// the page clips it, instead of being sliced over one page per page height.
func TestAtomTallerThanThePageIsClipped(t *testing.T) {
	for _, kind := range []string{"svg", "inline-block", "img"} {
		for _, h := range []string{"100000", "99999999"} {
			for _, c := range tallAtomContexts {
				t.Run(kind+"/"+h+"/"+c.name, func(t *testing.T) {
					pages := tallAtomRun(t, kind, strings.Replace(c.html, "X", tallAtom(kind, h), 1))
					if len(pages) > 2 {
						t.Fatalf("%d pages, want at most 2", len(pages))
					}
					if clippedRed(pages) == 0 {
						t.Errorf("the atom is not drawn whole and clipped by a page")
					}
					words, _ := wordsInArea(pages)
					if words["a"] != 1 {
						t.Errorf(`"a" drawn %d times inside a page area`, words["a"])
					}
					if kind == "inline-block" && words["ib"] != 1 {
						t.Errorf(`"ib" drawn %d times inside a page area`, words["ib"])
					}
					if kind != "img" {
						return
					}
					for _, imgs := range imgItems(pages) {
						if len(imgs) > 0 {
							return
						}
					}
					t.Errorf("the image is not drawn")
				})
			}
		}
	}
}

// In a paragraph, a tall atom and the text of its line fit one page: the atom
// takes the room the page leaves its line, under the paragraph margin.
func TestTallAtomInAParagraphTakesOnePage(t *testing.T) {
	for _, kind := range []string{"svg", "inline-block", "img"} {
		t.Run(kind, func(t *testing.T) {
			pages := tallAtomRun(t, kind, `<p>a `+tallAtom(kind, "99999999")+`</p>`)
			if len(pages) != 1 {
				t.Errorf("%d pages, want 1", len(pages))
			}
			if words, _ := wordsInArea(pages); words["a"] != 1 {
				t.Errorf(`"a" drawn %d times inside the page area`, words["a"])
			}
		})
	}
}

// After a page that already holds content, the tall atom moves to the next page
// once, and what follows it comes on the page after: at most 4 pages, one for
// "before", one or two for the atom, one for "after".
func TestTallAtomMovesOnce(t *testing.T) {
	for _, kind := range []string{"svg", "inline-block", "img"} {
		for _, c := range tallAtomContexts {
			t.Run(kind+"/"+c.name, func(t *testing.T) {
				html := `<p>before</p>` + strings.Replace(c.html, "X", tallAtom(kind, "99999999"), 1) + `<p>after</p>`
				pages := tallAtomRun(t, kind, html)
				if len(pages) > 4 {
					t.Fatalf("%d pages, want at most 4", len(pages))
				}
				words, _ := wordsInArea(pages)
				for _, w := range []string{"before", "a", "after"} {
					if words[w] != 1 {
						t.Errorf("%q drawn %d times inside a page area", w, words[w])
					}
				}
			})
		}
	}
}

// The text of a tall inline-block stays on the pages: the atom is sliced over
// its 60 lines (about 1100 px: page 3 or later for the last one), and only its
// empty height is clipped.
func TestTallInlineBlockKeepsItsText(t *testing.T) {
	const n = 60
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<span style="display:block">w%d</span>`, i)
	}
	html := `<div>a <span style="display:inline-block;height:99999999px">` + sb.String() + `</span></div><div>after</div>`
	pages := tallAtomRun(t, "inline-block", html)
	words, page := wordsInArea(pages)
	for i := range n {
		if w := fmt.Sprintf("w%d", i); words[w] != 1 {
			t.Errorf("%s drawn %d times inside a page area", w, words[w])
		}
	}
	if last := page[fmt.Sprintf("w%d", n-1)]; last < 3 {
		t.Errorf("the last line is on page %d: the atom is not sliced over its text", last)
	}
	if words["after"] != 1 || len(pages) > page[fmt.Sprintf("w%d", n-1)]+1 {
		t.Errorf("%d pages, %q drawn %d times: the empty height is not clipped", len(pages), "after", words["after"])
	}
}

// An absolute box below the capped height of an inline-block is brought up
// inside it, so its text lands on the page.
func TestTallAtomLiftsItsAbsoluteBoxes(t *testing.T) {
	for _, pos := range []string{"top:99999990px", "bottom:0"} {
		t.Run(pos, func(t *testing.T) {
			html := `<p>a <span style="display:inline-block;position:relative;height:99999999px"><span style="position:absolute;` + pos + `">absz</span></span></p>`
			pages := tallAtomRun(t, "inline-block", html)
			if len(pages) > 2 {
				t.Errorf("%d pages, want at most 2", len(pages))
			}
			if words, _ := wordsInArea(pages); words["absz"] != 1 {
				t.Errorf(`"absz" drawn %d times inside a page area`, words["absz"])
			}
		})
	}
}

func TestCapAtom(t *testing.T) {
	const page = 300.0
	fixed := &style.Computed{Height: style.Dim{Kind: style.DimPx, Value: 1e8}}
	cases := []struct {
		name     string
		h        float64
		children []*frag
		lines    []*lineFrag
		mt, mb   float64
		rise     float64
		capped   bool
		wantH    float64
	}{
		{name: "fits", h: 250, wantH: 250},
		{name: "empty", h: 1e8, capped: true, wantH: page},
		{name: "full of text", h: 500, lines: []*lineFrag{{rect: display.Rect{Y: 480, H: 20}, baseline: 490, items: []lineItem{{text: &display.Text{Baseline: 490, Size: 16}}}}}, wantH: 500},
		{name: "text below the page", h: 1e8, lines: []*lineFrag{{rect: display.Rect{Y: 480, H: 20}, baseline: 495, items: []lineItem{{text: &display.Text{Baseline: 495, Size: 16}}}}}, capped: true, wantH: 511},
		{name: "huge line-height", h: 1e8, lines: []*lineFrag{{rect: display.Rect{H: 1e8}, baseline: 5e7, items: []lineItem{{text: &display.Text{Baseline: 5e7, Size: 16}}}}}, capped: true, wantH: page},
		{name: "replaced child", h: 1e8, children: []*frag{{box: &box.Box{Kind: box.KindSVG}, rect: display.Rect{Y: 500, H: 1e8 - 500}}}, capped: true, wantH: 500},
		{name: "fixed-height child", h: 1e8, children: []*frag{{box: &box.Box{Kind: box.KindBlock, Style: fixed}, rect: display.Rect{H: 1e8}}}, wantH: 1e8},
		// The rise comes in clamped by clampRise: ±¼ of the page.
		{name: "huge margins and rise", h: 1e8, mt: 1e8, mb: -1e8, rise: -page / 4, capped: true, wantH: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &frag{box: &box.Box{Kind: box.KindInlineBlock}, rect: display.Rect{H: c.h}, children: c.children, lines: c.lines}
			a := &atomBox{f: f, mt: c.mt, mb: c.mb, rise: c.rise}
			a.capAtom(page, 0, 0, false)
			if a.capped != c.capped || math.Abs(a.h-c.wantH) > 1e-6 {
				t.Errorf("capped %v h %v, want %v %v", a.capped, a.h, c.capped, c.wantH)
			}
			if a.base > a.h+a.mb+1e-6 {
				t.Errorf("baseline %v below the capped box %v + %v", a.base, a.h, a.mb)
			}
			for _, v := range []float64{a.mt, a.mb} {
				if math.Abs(v) > page {
					t.Errorf("margin %v not clamped to ±%v", v, page)
				}
			}
		})
	}
}

func TestCapAtomWithoutPage(t *testing.T) {
	a := &atomBox{f: &frag{rect: display.Rect{H: 1e8}}, mt: 1e8}
	a.capAtom(0, 0, 0, false)
	if a.capped || a.h != 1e8 || a.mt != 1e8 {
		t.Errorf("no page height: capped %v h %v mt %v", a.capped, a.h, a.mt)
	}
}

// A tall image next to an inline-block too tall to fit the page keeps its
// capped height: the line is sliced, the image with it (on the baseline, below
// the top of the line), and shrinking it would leave a strip of it.
func TestTallImageBesideATallInlineBlockKeepsItsHeight(t *testing.T) {
	html := `<div>a <svg width="50" height="99999999" style="background:#f00"></svg><span style="display:inline-block">` +
		strings.Repeat(`<span style="display:block">w</span>`, 60) + `</span> b</div>`
	pages := tallAtomRun(t, "svg", html)
	drawn := 0.0
	for _, pg := range pages {
		for _, it := range pg.Items {
			if v, ok := it.(display.FillRect); ok && v.Color == tallAtomRed {
				drawn += v.Rect.H
			}
		}
	}
	// The cap: a page area less the strut of the line.
	if drawn < 250 {
		t.Errorf("the image is drawn %v px tall over the pages, want about a page", drawn)
	}
}

// A line-height leaving pages of empty leading around a tall inline-block is
// sliced over the inline-block only: its 60 lines in 4 pages, and the text of
// the line kept, wherever its baseline is.
func TestTallLeadingAroundATallInlineBlock(t *testing.T) {
	for _, valign := range []string{"baseline", "top"} {
		t.Run(valign, func(t *testing.T) {
			html := `<p style="line-height:99999999px">a <span style="display:inline-block;line-height:normal;vertical-align:` + valign + `">` +
				strings.Repeat(`<span style="display:block">w</span>`, 60) + `</span></p>`
			pages := tallAtomRun(t, "inline-block", html)
			if len(pages) > 5 {
				t.Errorf("%d pages, want at most 5", len(pages))
			}
			if words, _ := wordsInArea(pages); words["w"] != 60 {
				t.Errorf("%d lines of the inline-block drawn inside a page area, want 60", words["w"])
			}
		})
	}
}

// An inline-block that the leading of its line pushes below the page is sliced
// onto the pages, or, capped, raised onto the page: its text is never placed
// past the page area. With a 400 px font, its 10 lines of 460 px take 18 pages,
// as before the cap.
func TestAtomPushedBelowThePageByTheLeading(t *testing.T) {
	ib := `<span style="display:inline-block;line-height:normal">` + strings.Repeat(`<span style="display:block">w</span>`, 10) + `</span>`
	capped := `<span style="display:inline-block;height:99999999px;line-height:normal">` + strings.Repeat(`<span style="display:block">w</span>`, 10) + `</span>`
	cases := []struct {
		name, html string
		maxPages   int
	}{
		{"line-height 900px", `<p style="line-height:900px">a ` + ib + `</p>`, 5},
		{"font-size 400px", `<p style="font-size:400px;line-height:1">a ` + ib + `</p>`, 18},
		{"line-height 99999999", `<p style="line-height:99999999px">a ` + ib + `</p>`, 5},
		{"capped, line-height 900px", `<p style="line-height:900px">a ` + capped + `</p>`, 5},
		{"capped, line-height 99999999", `<p style="line-height:99999999px">a ` + capped + `</p>`, 5},
		{"capped, vertical-align bottom", `<p style="line-height:900px">a ` + strings.Replace(capped, "line-height:normal", "line-height:normal;vertical-align:bottom", 1) + `</p>`, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pages := tallAtomRun(t, "inline-block", c.html)
			if len(pages) > c.maxPages {
				t.Errorf("%d pages, want at most %d", len(pages), c.maxPages)
			}
			if words, _ := wordsInArea(pages); words["w"] != 10 {
				t.Errorf("%d lines of the inline-block drawn inside a page area, want 10", words["w"])
			}
		})
	}
}

// One-tag hostile documents render through the whole pipeline with the default
// budget, in a bounded number of pages.
func TestTallAtomHostileDocuments(t *testing.T) {
	for _, d := range testcorpus.TallAtomDocs() {
		t.Run(d.Name, func(t *testing.T) {
			o := pipe(t, context.Background(), d.HTML, tallAtomPage, nil, hostileMaxPages)
			if o.err != nil {
				t.Fatalf("%s: %v", o.stage, o.err)
			}
			if o.pages > d.MaxPages {
				t.Errorf("%d pages, want at most %d", o.pages, d.MaxPages)
			}
		})
	}
}
