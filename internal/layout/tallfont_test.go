package layout

import (
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

// tallPage is a page area of 280x180 px, which a font of 170pc (2720 px) and
// spans of 150 px raised by a quarter page exceed.
const tallPage = "@page { size: 300px 200px; margin: 10px }"

// A font taller than the page area, or spans whose rises together exceed it,
// must not push the other text of their line below the page; the tall glyphs
// show their bottom part.
func TestTallFontKeepsLineTextOnPage(t *testing.T) {
	for _, tc := range []struct{ name, html string }{
		{"fuzz minimised", "<style>p{margin:3px}</style>0<a style='font-size:170pc;margin:auto'>0</a>"},
		{"just over the page", "<p>abc</p>0<span style='font-size:300px'>0</span>"},
		{"small line-height", "0<span style='font-size:170pc;line-height:100px'>0</span>"},
		{"tall inline box around small text", "<span style='font-size:170pc'>a<span style='font-size:16px'>0</span></span>"},
		{"tall font first", "<span style='font-size:170pc'>X</span> y z"},
		{"in columns", "<div style='columns:2'>0<span style='font-size:170pc'>0</span></div>"},
		{"in a table cell", "<table><tr><td>0<span style='font-size:170pc'>0</span></td></tr></table>"},
		{"in a flex item", "<div style='display:flex'><div>0<span style='font-size:170pc'>0</span></div></div>"},
		{"line with an atom", "<div style='width:2000px'>0<span style='font-size:170pc'>0</span><b style='display:inline-block'>x</b></div>"},
		{"below the page size", "0<span style='font-size:150px'>0</span>"},
		{"raised span over the page", "0<span style='font-size:155px;vertical-align:45px'>0</span>"},
		{"raised and lowered spans", "0<span style='font-size:155px;vertical-align:45px'>0</span>q<span style='font-size:155px;vertical-align:-45px'>0</span>"},
		{"opposite rises and a tall font", "0<span style='font-size:150px;vertical-align:45px'>0</span><span style='font-size:150px;vertical-align:-45px'>1</span><span style='font-size:900px'>2</span>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRun(t, tc.html, opts{extra: tallPage})
			pages := r.all(t)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			if len(pages) > 2 {
				t.Errorf("%d pages, want at most 2", len(pages))
			}
			for _, pg := range pages {
				h := pg.Area.H
				for _, it := range pg.Items {
					tx, ok := it.(*display.Text)
					if !ok {
						continue
					}
					lo := 0.0
					if tx.Size <= h {
						lo = tx.Size * xMidFraction
					}
					if tx.Baseline < lo || tx.Baseline > h {
						t.Errorf("page %d: %q (size %v) has its baseline at %v, want in [%v, %v]", pg.Number, tx.Text, tx.Size, tx.Baseline, lo, h)
					}
				}
			}
		})
	}
}

func TestTallFontAloneShowsItsBottom(t *testing.T) {
	r := newRun(t, "<span style='font-size:170pc'>X</span>", opts{extra: tallPage + " body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) != 1 || countText(pages[0], "X") != 1 {
		t.Fatalf("want X once on 1 page, got %d pages", len(pages))
	}
	for _, it := range pages[0].Items {
		if tx, ok := it.(*display.Text); ok {
			if want := tallBaselineFraction * pages[0].Area.H; math.Abs(tx.Baseline-want) > 0.01 {
				t.Errorf("baseline at %v, want %v", tx.Baseline, want)
			}
		}
	}
}

// The background and border of an inline box that sticks out of a line cut
// to a page area stay in that line: painted after the lines above, they would
// hide them.
func TestTallInlineBoxDoesNotCoverLinesAbove(t *testing.T) {
	const above = "<p>first</p><p>second</p><p>third</p>"
	for _, tc := range []struct{ name, html string }{
		{"tall font", above + "0<span style='font-size:170pc;background:white;border:2px solid red'>0</span>"},
		{"raised and lowered spans", above + "0<span style='font-size:155px;vertical-align:45px;background:white'>0</span>q<span style='font-size:155px;vertical-align:-45px;background:white'>0</span>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRun(t, tc.html, opts{extra: tallPage + " body, p { margin: 0 }"})
			pages := r.all(t)
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			if len(pages) > 2 {
				t.Fatalf("%d pages, want at most 2", len(pages))
			}
			pg := pages[len(pages)-1]
			ln := lastLine(pg.root)
			if ln == nil {
				t.Fatal("no line on the last page")
			}
			var rects []display.Rect
			for _, it := range pg.Items {
				switch v := it.(type) {
				case display.FillRect:
					rects = append(rects, v.Rect)
				case display.Border:
					rects = append(rects, v.Rect)
				}
			}
			if len(rects) == 0 {
				t.Fatal("no background drawn")
			}
			for _, rc := range rects {
				if rc.Y < ln.rect.Y-0.01 {
					t.Errorf("rect %v starts above the top of its line (%v)", rc, ln.rect.Y)
				}
			}
		})
	}
}

// lastLine returns the last line box under f in tree order.
func lastLine(f *frag) *lineFrag {
	if f == nil {
		return nil
	}
	for i := len(f.children) - 1; i >= 0; i-- {
		if ln := lastLine(f.children[i]); ln != nil {
			return ln
		}
	}
	if len(f.lines) > 0 {
		return f.lines[len(f.lines)-1]
	}
	return nil
}

// The link of an inline box taller than the page stays in its line: it must
// not take the clicks of the links above.
func TestTallLinkDoesNotCoverLinksAbove(t *testing.T) {
	r := newRun(t, "<p><a href='https://a'>first</a></p><a href='https://b' style='font-size:9000px'>x</a>",
		opts{extra: tallPage + " body, p { margin: 0 }"})
	pages := r.all(t)
	var first, tall []display.Rect
	for _, pg := range pages {
		for _, it := range pg.Items {
			if l, ok := it.(display.Link); ok {
				switch l.URI {
				case "https://a":
					first = append(first, l.Rect)
				case "https://b":
					tall = append(tall, l.Rect)
				}
			}
		}
	}
	if len(first) != 1 || len(tall) == 0 {
		t.Fatalf("links: %v first, %v tall", first, tall)
	}
	for _, rc := range tall {
		if rc.Y < first[0].Bottom() && rc.Bottom() > first[0].Y && rc.X < first[0].Right() && rc.Right() > first[0].X {
			t.Errorf("tall link %v covers the first link %v", rc, first[0])
		}
	}
}
