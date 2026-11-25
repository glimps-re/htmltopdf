package layout

import (
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
)

const namedPagesCSS = `@page { size: 800px 600px; margin: 20px } @page cover { size: 600px 400px; margin: 0 }
body { margin: 0 } p { margin: 0 }`

// A box with a page name starts a page of that name, with the size and area of
// its @page rule; the siblings around it use the default page.
func TestNamedPagesGetTheirOwnPageStyle(t *testing.T) {
	r := newRun(t, `<div style="page:cover" id=c>Cover</div><div id=a>after</div><p style="page:cover">again</p><p>last</p>`, opts{css: namedPagesCSS})
	pages := r.all(t)
	if len(pages) != 4 {
		t.Fatalf("%d pages", len(pages))
	}
	want := []struct {
		name string
		w, h float64
	}{{"cover", 600, 400}, {"", 800, 600}, {"cover", 600, 400}, {"", 800, 600}}
	for i, w := range want {
		pg := pages[i]
		if pg.Name != w.name || pg.Width != w.w || pg.Height != w.h {
			t.Errorf("page %d: name %q %vx%v, want %q %vx%v", i+1, pg.Name, pg.Width, pg.Height, w.name, w.w, w.h)
		}
		if w.name == "cover" && (pg.Area.X != 0 || pg.Area.W != 600) {
			t.Errorf("page %d area %+v", i+1, pg.Area)
		}
		if w.name == "" && (pg.Area.X != 20 || pg.Area.W != 760) {
			t.Errorf("page %d area %+v", i+1, pg.Area)
		}
	}
	// The width of the area follows the page: a block fills its page.
	if c := fragByID(t, pages, "c"); math.Abs(c.rect.W-600) > 0.01 {
		t.Errorf("cover block %v wide", c.rect.W)
	}
	if a := fragByID(t, pages, "a"); math.Abs(a.rect.W-760) > 0.01 {
		t.Errorf("default block %v wide", a.rect.W)
	}
}

// The name is inherited by the descendants, and a first box deep in the tree
// names the first page.
func TestUsedPageNameIsInherited(t *testing.T) {
	r := newRun(t, `<section style="page:cover"><div><p>in</p></div></section><section><div><p>out</p></div></section>`, opts{css: namedPagesCSS})
	pages := r.all(t)
	if len(pages) != 2 || pages[0].Name != "cover" || pages[1].Name != "" {
		t.Errorf("%d pages, names %q %q", len(pages), pages[0].Name, pages[len(pages)-1].Name)
	}
}

// Without a named page rule every page is the default one.
func TestNamedPageWithoutRuleIsDefault(t *testing.T) {
	r := newRun(t, `<div style="page:nobody">x</div><p>y</p>`, opts{css: namedPagesCSS})
	pages := r.all(t)
	if len(pages) != 2 || pages[0].Width != 800 || pages[0].Name != "nobody" {
		t.Errorf("%d pages, first %q %v", len(pages), pages[0].Name, pages[0].Width)
	}
}

func alternating(n int) string {
	var sb strings.Builder
	for i := range n {
		if i%2 == 0 {
			sb.WriteString("<div style='page:cover'>c</div>")
		} else {
			sb.WriteString("<div>d</div>")
		}
	}
	return sb.String()
}

// Siblings that alternate two page names make a page each: 600 of them reach
// LimitPages at 500, the 499-page twin renders; 1-minute guard.
func TestAlternatingPageNamesAreBoundedByPages(t *testing.T) {
	start := time.Now()
	run := func(n int) (int, error) {
		r := newRun(t, alternating(n), opts{css: namedPagesCSS, limits: func(l *budget.Limits) { l[budget.Pages] = 500 }})
		pages := 0
		for {
			if _, err := r.pag.Next(); err != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				return pages, err
			}
			pages++
		}
	}
	if n, err := run(499); err != nil || n != 499 {
		t.Errorf("499 siblings: %d pages, %v", n, err)
	}
	_, err := run(600)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Pages {
		t.Errorf("600 siblings: %v", err)
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

// Two names with the same page size but different heights lay the same blocks
// out in their own page areas.
func TestNamedPagesOfOneWidthKeepTheirHeight(t *testing.T) {
	css := "@page { size: 800px 600px; margin: 0 } @page tall { size: 800px 1000px; margin: 0 } body { margin: 0 } p { margin: 0; height: 450px }"
	pages := newRun(t, `<p style="page:tall">a</p><p style="page:tall">b</p><p>c</p><p>d</p>`, opts{css: css}).all(t)
	if len(pages) != 3 || pages[0].Height != 1000 || pages[1].Height != 600 || pages[2].Height != 600 {
		heights := []float64{}
		for _, pg := range pages {
			heights = append(heights, pg.Height)
		}
		t.Errorf("pages %v", heights)
	}
}

// A margin box with the same text but another style on a named page is not
// served from the cache of the default page.
func TestMarginBoxStyleFollowsThePageName(t *testing.T) {
	css := `@page { size: 800px 600px; margin: 50px; @top-center { content: "head"; font-size: 10px } }
@page big { margin: 50px; @top-center { content: "head"; font-size: 30px } }
body { margin: 0 }`
	r := newRun(t, `<p>a</p><p style="page:big">b</p><p>c</p>`, opts{css: css})
	pages := r.all(t)
	if len(pages) != 3 {
		t.Fatalf("%d pages", len(pages))
	}
	size := func(pg *Page) float64 {
		items, err := r.pag.MarginBoxes(pg, len(pages))
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if tx, ok := it.(*display.Text); ok {
				return tx.Size
			}
		}
		t.Fatalf("page %d: no margin text", pg.Number)
		return 0
	}
	if a, b, c := size(pages[0]), size(pages[1]), size(pages[2]); a != 10 || b != 30 || c != 10 {
		t.Errorf("sizes %v %v %v, want 10 30 10", a, b, c)
	}
}
