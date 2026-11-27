package layout

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// pile is n lines of "y" 1 px apart in a font of size px: each glyph overlaps
// the next ones, so no page end between glyphs exists.
func pile(n int, size string) string {
	return strings.Repeat(`<div style="height:1px;font-size:`+size+`">y</div>`, n)
}

// pileContainers wrap a pile in the containers sliced by the shared cutter.
var pileContainers = []struct{ name, open, close string }{
	{"table cell", `<table><tr><td>`, `</td></tr></table>`},
	{"table with repeated header", `<table><thead><tr><th>H</th></tr></thead><tr><td>`, `</td></tr></table>`},
	{"flex band", `<div style="display:flex"><div>`, `</div></div>`},
	{"grid band", `<div style="display:grid"><div>`, `</div></div>`},
	{"table in flex", `<div style="display:flex"><div><table><tr><td>`, `</td></tr></table></div></div>`},
	{"flex in table", `<table><tr><td><div style="display:flex"><div>`, `</div></div></td></tr></table>`},
}

// glyphsOffPage counts the text items of pg whose glyphs start above its top or
// whose baseline is below its bottom, and the text items it holds.
func glyphsOffPage(pg *Page) (off, texts int) {
	height := pg.Area.H / pg.Scale
	var walk func(f *frag)
	walk = func(f *frag) {
		for _, ln := range f.lines {
			for i := range ln.items {
				it := &ln.items[i]
				if it.atom != nil {
					walk(it.atom.f)
				}
				if it.text == nil {
					continue
				}
				texts++
				if ln.baseline-it.ascent < -cutEps || it.text.Baseline > height+cutEps {
					off++
				}
			}
		}
		for _, c := range f.children {
			walk(c)
		}
	}
	if pg.root != nil {
		walk(pg.root)
	}
	return off, texts
}

// maxPilePages bounds the pages of a pile of lines px tall on pages with room
// px for it: at least half of the room per page (the floor of textCutEnd),
// plus the pages of the glyphs of the last line.
func maxPilePages(lines, room int) int {
	return 2*lines/room + 3
}

// headerRoom is the room of a 300 px page under a repeated table header, at most.
const headerRoom = 250

// A pile of overlapping lines sliced over pages in a table row or a flex or
// grid band: the end of a window cuts through glyphs (its floor stops it at the
// middle of the window), and the next window starts below the tops of the
// glyphs cut, so every line is drawn once, whole, on a page, and the pages stay
// linear in the lines. Glyphs deeper than half the page (250px) are not moved
// down, so that the window keeps half of it: their tops may stay cut, but no
// text is lost.
func TestPileCutKeepsGlyphsOnPage(t *testing.T) {
	sizes := []struct {
		size        string
		lines       int
		wholeGlyphs bool
	}{
		{"16px", 1000, true},
		{"120px", 400, true},
		{"250px", 400, false},
	}
	for _, c := range pileContainers {
		for _, sz := range sizes {
			t.Run(c.name+"/"+sz.size, func(t *testing.T) {
				checkPile(t, c.open+pile(sz.lines, sz.size)+c.close, sz.lines, sz.lines, sz.wholeGlyphs)
			})
		}
	}
}

// A pile whose second text of each line is raised or lowered by vertical-align:
// the straddling glyph tops come from the rise of the text, not its line.
func TestPileCutRaisedText(t *testing.T) {
	const lines = 600
	for _, c := range []struct{ name, open, close string }{pileContainers[0], pileContainers[2]} {
		for _, rise := range []string{"120px", "-120px"} {
			t.Run(c.name+"/"+rise, func(t *testing.T) {
				line := `<div style="height:1px">y<span style="vertical-align:` + rise + `">y</span></div>`
				checkPile(t, c.open+strings.Repeat(line, lines)+c.close, lines, 2*lines, true)
			})
		}
	}
}

// checkPile lays out html, a pile of lines lines holding texts "y" in all, on
// 400x300 pages: no text is lost, each is drawn once, the pages are bounded and,
// with wholeGlyphs, no glyph is cut by the page edge.
func checkPile(t *testing.T, html string, lines, texts int, wholeGlyphs bool) {
	t.Helper()
	root, pages := slicedLayersRun(t, tallAtomPage, html)
	if lost := TextLost(root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if maxPages := maxPilePages(lines, headerRoom); len(pages) > maxPages {
		t.Errorf("%d pages, want at most %d", len(pages), maxPages)
	}
	for _, pg := range pages {
		if off, n := glyphsOffPage(pg); off > 0 && wholeGlyphs {
			t.Errorf("page %d: %d of %d texts cut by the page edge", pg.Number, off, n)
		}
	}
	if n := countDrawn(pages, "y"); n != texts {
		t.Errorf("%d texts drawn, want %d", n, texts)
	}
}

// hostilePile is 10 000 overlapping lines in nested sliced containers.
var hostilePile = `<table><tr><td><div style="display:flex"><div><table><tr><td>` + pile(10000, "16px") + `</td></tr></table></div></div></td></tr></table>`

// Hostile: 10 000 overlapping lines in nested sliced containers end in bounded
// pages with no text lost, and half the Work they take stops them with a
// LimitError instead of paginating on.
func TestPileCutHostileIsBounded(t *testing.T) {
	r := newRun(t, hostilePile, opts{extra: tallAtomPage, limits: func(l *budget.Limits) { l[budget.Pages] = hostileMaxPages; l[budget.Work] = 1 << 40 }})
	pages := r.all(t)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if maxPages := maxPilePages(10000, 300); len(pages) > maxPages {
		t.Errorf("%d pages, want at most %d", len(pages), maxPages)
	}
	for _, pg := range pages {
		if off, n := glyphsOffPage(pg); off > 0 {
			t.Errorf("page %d: %d of %d texts cut by the page edge", pg.Number, off, n)
		}
	}

	limit := r.b.Used(budget.Work) / 2
	r = newRun(t, hostilePile, opts{extra: tallAtomPage, limits: func(l *budget.Limits) { l[budget.Pages] = hostileMaxPages; l[budget.Work] = limit }})
	var err error
	for n := 0; err == nil; n++ {
		if n > hostileMaxPages {
			t.Fatal("runaway pagination")
		}
		_, err = r.pag.Next()
	}
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("err = %v under a Work limit of %d, want a Work LimitError", err, limit)
	}
}

func BenchmarkPileCut(b *testing.B) {
	for _, c := range []struct{ name, open, close string }{pileContainers[0], pileContainers[2]} {
		html := c.open + pile(10000, "16px") + c.close
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r := newRun(b, html, opts{extra: tallAtomPage, limits: func(l *budget.Limits) { l[budget.Pages] = hostileMaxPages }})
				for {
					if _, err := r.pag.Next(); errors.Is(err, io.EOF) {
						break
					} else if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
