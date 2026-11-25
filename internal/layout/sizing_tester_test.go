package layout

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

const sizingPage = "@page { size: 800px 600px; margin: 0 } body, p { margin: 0 } div { font-size: 10px; line-height: 20px }"

// sizingRun lays out html to the end; the error is the first non-EOF one.
func sizingRun(t *testing.T, html string) (*run, []*Page, error) {
	t.Helper()
	r := newRun(t, html, opts{extra: sizingPage, limits: func(l *budget.Limits) {
		l[budget.Work] = 1 << 30
	}})
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			return r, pages, nil
		}
		if err != nil {
			return r, pages, err
		}
		pages = append(pages, pg)
	}
}

func allFinite(t *testing.T, name string, pages []*Page) {
	t.Helper()
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			for _, v := range [4]float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H} {
				if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
					t.Fatalf("%s: rect %+v", name, f.rect)
				}
			}
		})
	}
}

// For every height in a set that straddles page multiples, the fragments of
// the box add up to the height and the next sibling starts right after it.
func TestHeightFragmentsSumToTheHeight(t *testing.T) {
	for _, h := range []float64{0, 1, 19, 20, 21, 299, 300, 599, 600, 601, 1199, 1200, 1201, 2999} {
		for _, extra := range []string{"", "padding:7px;box-sizing:border-box;", "border:3px solid #000;", "overflow:hidden;", "margin:11px 0 13px;"} {
			t.Run(fmt.Sprintf("%v/%s", h, extra), func(t *testing.T) {
				_, pages, err := sizingRun(t, fmt.Sprintf(`<div id=pre>x</div><div id=a style="height:%gpx;%s">y</div><div id=b>z</div>`, h, extra))
				if err != nil {
					t.Fatal(err)
				}
				allFinite(t, "doc", pages)
				sum, lastBottom, lastPage := 0.0, 0.0, -1
				for i, pg := range pages {
					walk(pg.root, func(f *frag) {
						if f.box.ID == "a" {
							sum += f.rect.H
							lastBottom, lastPage = f.rect.Bottom(), i
						}
					})
				}
				want := h
				if strings.Contains(extra, "border:3px") {
					want = h + 6
				}
				if strings.Contains(extra, "box-sizing") {
					want = math.Max(h, 14)
				}
				if math.Abs(sum-want) > 0.01 {
					t.Errorf("fragments sum to %v, want %v", sum, want)
				}
				b := findFrag(pages, "b")
				if b == nil {
					t.Fatal("no #b")
				}
				mt := 0.0
				if strings.Contains(extra, "margin:") {
					mt = 13
				}
				pgB := 0
				for i, pg := range pages {
					if pg.root != nil && func() bool {
						found := false
						walk(pg.root, func(f *frag) { found = found || f.box.ID == "b" })
						return found
					}() {
						pgB = i
					}
				}
				if pgB == lastPage && b.rect.Y+0.01 < lastBottom+mt {
					t.Errorf("#b at %v, but #a ends at %v (+%v margin) on the same page", b.rect.Y, lastBottom, mt)
				}
			})
		}
	}
}

// Percentage chains: 400 nested definite-% boxes resolve without recursion
// trouble; each halves its parent.
func TestDeepPercentHeightChain(t *testing.T) {
	const depth = 400
	html := `<div style="height:1000px">` + strings.Repeat(`<div style="height:99%">`, depth) + `<p id=leaf>x</p>` + strings.Repeat("</div>", depth+1)
	start := time.Now()
	_, pages, err := sizingRun(t, html)
	if err != nil {
		var le *budget.LimitError
		if !errors.As(err, &le) {
			t.Fatal(err)
		}
		return
	}
	allFinite(t, "chain", pages)
	if time.Since(start) > 20*time.Second {
		t.Errorf("%v", time.Since(start))
	}
}

// min-width and vertical-align at absurd values: finite rects, bounded work
// against the twin without them.
func TestAbsurdMinWidthAndVerticalAlign(t *testing.T) {
	for name, decl := range map[string]string{
		"min-width 1e9":          "min-width:1e9px",
		"min-width 1e30":         "min-width:1e30px",
		"min-width 100000%":      "min-width:100000%",
		"min-width + width":      "min-width:1e7px;width:1px",
		"va 1e7":                 "display:inline-block;width:5px;height:5px;vertical-align:1e7px",
		"va -1e7":                "display:inline-block;width:5px;height:5px;vertical-align:-1e7px",
		"va 1e30":                "display:inline-block;width:5px;height:5px;vertical-align:1e30px",
		"va -1e30":               "display:inline-block;width:5px;height:5px;vertical-align:-1e30px",
		"va 1e999":               "display:inline-block;width:5px;height:5px;vertical-align:1e999px",
		"va on inline text":      "vertical-align:-1e5px",
		"va on empty inline":     "display:inline;vertical-align:1e4px",
		"height 1e30":            "height:1e30px",
		"height + min-width 1e9": "height:1e3px;min-width:1e9px",
	} {
		t.Run(name, func(t *testing.T) {
			html := strings.Repeat(`<div>text <span style="`+decl+`">y</span> more</div>`, 20)
			_, pages, err := sizingRun(t, html)
			if err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatalf("%v", err)
				}
				return
			}
			allFinite(t, name, pages)
		})
	}
}

// 10 000 blocks with a height, min-width and vertical-align: work is linear
// against the twin.
func TestSizingWorkIsLinear(t *testing.T) {
	const n = 10000
	with := strings.Repeat(`<div style="height:5px;min-width:3px"><span style="display:inline-block;height:2px;vertical-align:-1px">x</span></div>`, n)
	without := strings.Repeat(`<div><span style="display:inline-block">x</span></div>`, n)
	rw, pw, err := sizingRun(t, with)
	if err != nil {
		t.Fatal(err)
	}
	rt, pt, err := sizingRun(t, without)
	if err != nil {
		t.Fatal(err)
	}
	ww, wt := rw.b.Used(budget.Work), rt.b.Used(budget.Work)
	t.Logf("work %d vs twin %d, pages %d vs %d", ww, wt, len(pw), len(pt))
	if ww > 2*wt+10000 {
		t.Errorf("work %d against %d for the twin", ww, wt)
	}
}

// Content taller than its fixed height overflows without creating pages
// beyond the twin without the height, and a huge height on cells, rows,
// inline-blocks and flex items ends in the pages limit or renders.
func TestHeightOverflowAndContainers(t *testing.T) {
	tall := strings.Repeat("x<br>", 400)
	_, withH, err := sizingRun(t, `<div style="height:30px">`+tall+`</div><p>after</p>`)
	if err != nil {
		t.Fatal(err)
	}
	_, noH, err := sizingRun(t, `<div>`+tall+`</div><p>after</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(withH) > len(noH) {
		t.Errorf("%d pages with the height, %d without", len(withH), len(noH))
	}
	for name, html := range map[string]string{
		"td":           `<table><tr><td style="height:1e9px">x</td></tr></table>`,
		"tr":           `<table><tr style="height:1e9px"><td>x</td></tr></table>`,
		"table":        `<table style="height:1e9px"><tr><td>x</td></tr></table>`,
		"inline-block": `<div><span style="display:inline-block;height:1e9px">x</span></div>`,
		"flex":         `<div style="display:flex;height:1e9px"><div>x</div></div>`,
		"flex item":    `<div style="display:flex"><div style="height:1e9px">x</div></div>`,
		"nested":       `<div style="height:1e9px"><div style="height:50%">x</div></div>`,
		"negative":     `<div style="height:-5px">x</div>`,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, pages, err := sizingRun(t, html)
			if err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatalf("%v", err)
				}
			} else {
				allFinite(t, name, pages)
			}
			if time.Since(start) > time.Minute {
				t.Error("hang")
			}
		})
	}
}

// Overflow rulings of a fixed height: overflow hidden clips (50px box with
// 2000px of content is one page, its lines are clipped, the rest follows the
// box); overflow visible paginates the content without extending the box (all
// lines present, the fragments of the box add up to its height).
func TestFixedHeightOverflowRulings(t *testing.T) {
	const lines = 100 // 20px each: 2000px
	content := strings.Repeat("x<br>", lines-1) + "x"
	countLines := func(pages []*Page) int {
		n := 0
		for _, pg := range pages {
			n += len(linesOf(pg))
		}
		return n
	}
	_, hidden, err := sizingRun(t, `<div id=a style="height:50px;overflow:hidden">`+content+`</div><div id=b>after</div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 {
		t.Errorf("hidden: %d pages, want 1", len(hidden))
	}
	if a, b := findFrag(hidden, "a"), findFrag(hidden, "b"); a == nil || b == nil || !near(a.rect.H, 50) || !near(b.rect.Y, 50) {
		t.Errorf("hidden: box %+v, next %+v", a, b)
	}
	if n := countLines(hidden); n > lines+1 {
		t.Errorf("hidden: %d lines", n)
	}

	_, visible, err := sizingRun(t, `<div id=a style="height:50px">`+content+`</div><div id=b>after</div>`)
	if err != nil {
		t.Fatal(err)
	}
	if n := countLines(visible); n < lines+1 {
		t.Errorf("visible: %d lines on the pages, want all %d of the box and #b", n, lines+1)
	}
	sum := 0.0
	walkPages(visible, func(f *frag) {
		if f.box.ID == "a" {
			sum += f.rect.H
		}
	})
	if !near(sum, 50) {
		t.Errorf("visible: fragments sum to %v, want 50", sum)
	}
	for _, ov := range []string{"overflow:hidden", ""} {
		_, pages, err := sizingRun(t, `<div style="height:580px"></div><div id=a style="height:50px;`+ov+`">`+content+`</div>`)
		if err != nil {
			t.Fatal(err)
		}
		sum := 0.0
		walkPages(pages, func(f *frag) {
			if f.box.ID == "a" {
				sum += f.rect.H
			}
		})
		if !near(sum, 50) {
			t.Errorf("%q straddling a cut: fragments sum to %v", ov, sum)
		}
		if ov != "" && countLines(pages) > lines+1 {
			t.Errorf("hidden straddling a cut: %d lines", countLines(pages))
		}
	}
}
