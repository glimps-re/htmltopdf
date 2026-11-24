package layout

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const breaksCSS = "body, p, h2, h3 { margin: 0 } .b { line-height: 100px; font-size: 10px } .r { line-height: 200px } .t { line-height: 600px } .s { line-height: 2px; font-size: 2px } .w { line-height: 80px; font-size: 10px } .n { line-height: 90px; font-size: 10px }"

func breakRun(t *testing.T, html string) []*Page {
	t.Helper()
	return newRun(t, html, opts{extra: smallPage, css: breaksCSS}).all(t)
}

// pageOf returns the 0-based page of the first fragment with the id and its top.
func pageOf(t *testing.T, pages []*Page, id string) (int, float64) {
	t.Helper()
	for i, pg := range pages {
		var found *frag
		walk(pg.root, func(f *frag) {
			if f.box.ID == id && found == nil {
				found = f
			}
		})
		if found != nil {
			return i, found.rect.Y
		}
	}
	t.Fatalf("no element #%s", id)
	return 0, 0
}

func blocks(n int, prefix string) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<div class="b" id="%s%d">x</div>`, prefix, i)
	}
	return sb.String()
}

func TestForcedBreaks(t *testing.T) {
	tests := []struct {
		name  string
		html  string
		pages int
		at    map[string]int
	}{
		{"break-before between siblings", `<div class=b id=a>a</div><div class=b id=b style="break-before:page">b</div>`, 2, map[string]int{"a": 0, "b": 1}},
		{"break-after between siblings", `<div class=b id=a style="break-after:page">a</div><div class=b id=b>b</div>`, 2, map[string]int{"a": 0, "b": 1}},
		{"left and right are pages", `<div class=b id=a style="break-after:right">a</div><div class=b id=b style="break-before:left">b</div><div class=b id=c>c</div>`, 2, map[string]int{"a": 0, "b": 1, "c": 1}},
		{"not before the first child", `<div class=b id=a style="break-before:page">a</div><div class=b id=b>b</div>`, 1, map[string]int{"a": 0, "b": 0}},
		{"not after the last child", `<div class=b id=a>a</div><div class=b id=b style="break-after:page">b</div>`, 1, map[string]int{"b": 0}},
		{"nested, between siblings of a div", `<div><div class=b id=a>a</div><div class=b id=b style="break-before:page">b</div></div><div class=b id=c>c</div>`, 2, map[string]int{"a": 0, "b": 1, "c": 1}},
		{"two breaks do not leave an empty page", `<div class=b id=a style="break-after:page">a</div><div id=e style="break-after:page"></div><div class=b id=b>b</div>`, 2, map[string]int{"a": 0, "b": 1}},
		{"avoid is not forced", `<div class=b id=a style="break-after:avoid">a</div><div class=b id=b>b</div>`, 1, map[string]int{"b": 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := breakRun(t, tt.html)
			if len(pages) != tt.pages {
				t.Fatalf("%d pages, want %d", len(pages), tt.pages)
			}
			for id, want := range tt.at {
				if got, _ := pageOf(t, pages, id); got != want {
					t.Errorf("#%s on page %d, want %d", id, got, want)
				}
			}
		})
	}
}

func TestForcedBreakKeepsMargins(t *testing.T) {
	forced := breakRun(t, `<div class=b id=a>a</div><div class=b id=b style="break-before:page;margin-top:40px">b</div>`)
	if _, y := pageOf(t, forced, "b"); y != 40 {
		t.Errorf("after a forced break y = %v, want 40", y)
	}
	natural := breakRun(t, blocks(6, "p")+`<div class=b id=b style="margin-top:40px">b</div>`)
	if pg, y := pageOf(t, natural, "b"); pg != 1 || y != 0 {
		t.Errorf("after an unforced break page %d y = %v, want 1 and 0", pg, y)
	}
}

func TestAvoidBetweenSiblings(t *testing.T) {
	tests := []struct {
		name string
		html string
		at   map[string]int
	}{
		{"h2 moves with its paragraph", blocks(5, "p") + `<h2 class=b id=h style="break-after:avoid">h</h2><div class=b id=n>n</div>`, map[string]int{"p4": 0, "h": 1, "n": 1}},
		{"break-before on the next", blocks(5, "p") + `<h2 class=b id=h>h</h2><div class=b id=n style="break-before:avoid">n</div>`, map[string]int{"p4": 0, "h": 1, "n": 1}},
		{"chain h2 h3", blocks(4, "p") + `<h2 class=b id=h2 style="break-after:avoid">h</h2><h3 class=b id=h3 style="break-after:avoid">h</h3><div class=b id=n>n</div>`, map[string]int{"p3": 0, "h2": 1, "h3": 1, "n": 1}},
		{"chain backs up to the first one that is not first on the page", `<div class=b id=a>a</div><h2 class=b id=h2 style="break-after:avoid">h</h2>` + blocks(3, "p") + `<h3 class=b id=h3 style="break-after:avoid">h</h3><div class=b id=n>n</div>`, map[string]int{"a": 0, "h2": 0, "p2": 0, "h3": 1, "n": 1}},
		{"satisfied avoid changes nothing", `<h2 class=b id=h style="break-after:avoid">h</h2><div class=b id=n>n</div>`, map[string]int{"h": 0, "n": 0}},
		{"top of the page ignored", `<h2 class=t id=h style="break-after:avoid">h</h2><div class=b id=n>n</div>`, map[string]int{"h": 0, "n": 1}},
		{"first line is enough", blocks(4, "p") + `<h2 class=b id=h style="break-after:avoid">h</h2><div class=b id=n>n<br>m</div>`, map[string]int{"h": 0, "n": 0}},
		{"forced wins over avoid", `<h2 class=b id=h style="break-after:avoid;break-before:page">h</h2><div class=b id=n style="break-before:page">n</div>`, map[string]int{"h": 0, "n": 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := breakRun(t, tt.html)
			for id, want := range tt.at {
				if got, _ := pageOf(t, pages, id); got != want {
					t.Errorf("#%s on page %d, want %d", id, got, want)
				}
			}
		})
	}
}

func TestBreakInsideAvoid(t *testing.T) {
	two := func(attrs string) string {
		return `<div id=m ` + attrs + `><div class=b id=m1>1</div><div class=b id=m2>2</div><div class=b id=m3>3</div></div>`
	}
	t.Run("moved whole", func(t *testing.T) {
		pages := breakRun(t, blocks(4, "p")+two(`style="break-inside:avoid"`))
		if pg, y := pageOf(t, pages, "m1"); pg != 1 || y != 0 {
			t.Errorf("m1 page %d y %v", pg, y)
		}
		if got := len(frags(pages, "div")); got == 0 {
			t.Fatal("no fragments")
		}
		if pg, _ := pageOf(t, pages, "m3"); pg != 1 {
			t.Errorf("m3 page %d", pg)
		}
	})
	t.Run("control splits", func(t *testing.T) {
		pages := breakRun(t, blocks(4, "p")+two(""))
		if pg, _ := pageOf(t, pages, "m3"); pg != 1 {
			t.Errorf("m3 page %d", pg)
		}
		if pg, _ := pageOf(t, pages, "m1"); pg != 0 {
			t.Errorf("m1 page %d", pg)
		}
	})
	t.Run("fits whole", func(t *testing.T) {
		pages := breakRun(t, blocks(2, "p")+two(`style="break-inside:avoid"`))
		if pg, _ := pageOf(t, pages, "m3"); pg != 0 || len(pages) != 1 {
			t.Errorf("m3 page %d of %d", pg, len(pages))
		}
	})
	t.Run("starting the page it is sliced", func(t *testing.T) {
		tall := `<div style="break-inside:avoid">` + blocks(8, "q") + `</div>`
		pages := breakRun(t, tall)
		if len(pages) != 2 {
			t.Fatalf("%d pages", len(pages))
		}
		if pg, _ := pageOf(t, pages, "q7"); pg != 1 {
			t.Errorf("q7 page %d", pg)
		}
	})
	t.Run("taller than a page is not moved", func(t *testing.T) {
		pages := breakRun(t, blocks(1, "p")+`<div style="break-inside:avoid">`+blocks(8, "q")+`</div>`)
		if pg, y := pageOf(t, pages, "q0"); pg != 0 || y != 100 {
			t.Errorf("q0 page %d y %v, want page 0 right after p0", pg, y)
		}
		if pg, _ := pageOf(t, pages, "q7"); pg != 1 {
			t.Errorf("q7 page %d", pg)
		}
	})
	t.Run("heading chained to a box taller than a page stays with it", func(t *testing.T) {
		pages := breakRun(t, blocks(1, "p")+`<h2 class=b id=h style="break-after:avoid">h</h2><div style="break-inside:avoid">`+blocks(8, "q")+`</div>`)
		if pg, _ := pageOf(t, pages, "h"); pg != 0 {
			t.Errorf("h page %d", pg)
		}
		if pg, _ := pageOf(t, pages, "q0"); pg != 0 {
			t.Errorf("q0 page %d", pg)
		}
	})
	t.Run("exactly a page tall fits whole on a fresh page", func(t *testing.T) {
		pages := breakRun(t, blocks(1, "p")+`<div style="break-inside:avoid">`+blocks(6, "q")+`</div>`)
		if len(pages) != 2 {
			t.Fatalf("%d pages", len(pages))
		}
		if pg, y := pageOf(t, pages, "q0"); pg != 1 || y != 0 {
			t.Errorf("q0 page %d y %v", pg, y)
		}
		if pg, _ := pageOf(t, pages, "q5"); pg != 1 {
			t.Errorf("q5 page %d", pg)
		}
	})
	t.Run("paragraph lines", func(t *testing.T) {
		pages := breakRun(t, blocks(5, "p")+`<p class=b id=pp style="break-inside:avoid">a<br>b<br>c</p>`)
		if pg, _ := pageOf(t, pages, "pp"); pg != 1 {
			t.Errorf("paragraph on page %d", pg)
		}
		if lines := linesOf(pages[0]); len(lines) != 5 {
			t.Errorf("%d lines on page 0", len(lines))
		}
	})
}

func tableHTML(attr string) string {
	var sb strings.Builder
	sb.WriteString(`<table style="border-spacing:0;border-collapse:separate">`)
	for i := range 3 {
		fmt.Fprintf(&sb, `<tr %s><td class="w" id="r%d">a<br>b<br>c</td></tr>`, attr, i)
	}
	sb.WriteString(`</table>`)
	return sb.String()
}

func TestRowBreakInsideAvoid(t *testing.T) {
	rows := func(pages []*Page) int { return len(frags(pages, "tr")) }
	avoid := breakRun(t, tableHTML(`style="break-inside:avoid"`))
	plain := breakRun(t, tableHTML(""))
	if len(avoid) != 2 || len(plain) != 2 {
		t.Fatalf("pages: %d and %d", len(avoid), len(plain))
	}
	if got, want := rows(avoid), 3; got != want {
		t.Errorf("avoid: %d row fragments, want %d", got, want)
	}
	if got := rows(plain); got != 4 {
		t.Errorf("control: %d row fragments, want 4 (a row cut)", got)
	}
	if pg, _ := pageOf(t, avoid, "r2"); pg != 1 {
		t.Errorf("last row on page %d", pg)
	}
}

func TestAvoidBounded(t *testing.T) {
	const n = 10000
	html := func(attr string) string {
		return strings.Repeat(`<h2 class=s `+attr+`>h</h2>`, n) + `<div class=t>t</div>`
	}
	work := func(attr string) int64 {
		r := newRun(t, html(attr), opts{extra: smallPage, css: breaksCSS})
		r.all(t)
		return r.b.Used(budget.Work)
	}
	base := work("")
	got := work(`style="break-after:avoid"`)
	if float64(got) > 2*float64(base)*1.01 {
		t.Errorf("work %d, twin %d: more than twice", got, base)
	}
}

func TestForcedBreaksLimit(t *testing.T) {
	html := func(n int) string {
		return strings.Repeat(`<div class=b style="break-after:page">x</div>`, n) + `<div class=b>x</div>`
	}
	limit := func(l *budget.Limits) { l[budget.Pages] = 500 }
	var le *budget.LimitError
	_, err := newRun(t, html(10000), opts{extra: smallPage, css: breaksCSS, limits: limit}).pag.drain()
	if !errors.As(err, &le) || le.Kind != budget.Pages {
		t.Fatalf("err = %v", err)
	}
	if n, err := newRun(t, html(499), opts{extra: smallPage, css: breaksCSS, limits: limit}).pag.drain(); err != nil || n != 500 {
		t.Fatalf("twin: %d pages, err %v", n, err)
	}
}

func TestTemplate01Breaks(t *testing.T) {
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "01-modern", testcorpus.Large)
	if err != nil {
		t.Fatal(err)
	}
	pages := newRun(t, string(html), opts{extra: testcorpus.ChromePaperCSS, scale: 0.9}).all(t)
	rows := 0
	for _, pg := range pages {
		var last *frag
		walk(pg.root, func(f *frag) {
			if len(f.lines) > 0 {
				last = f
			}
		})
		if last != nil && (last.box.Tag.String() == "h2" || last.box.Tag.String() == "h3") {
			t.Errorf("page %d ends with a %s", pg.Number, last.box.Tag)
		}
	}
	for _, f := range frags(pages, "tr") {
		if !f.first || !f.last {
			rows++
		}
	}
	t.Logf("%d pages, %d split rows", len(pages), rows)
	if rows > 0 {
		t.Errorf("%d split rows", rows)
	}
}

// drain lays every page out and returns their number.
func (p *Paginator) drain() (int, error) {
	n := 0
	for {
		_, err := p.Next()
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
	}
}

func goldenBreaks(t *testing.T, name, html string) {
	t.Helper()
	golden(t, name, breakRun(t, html))
}

func TestAvoidGoldens(t *testing.T) {
	lines := func(n int, sep string) string { return strings.Repeat("x"+sep, n) }
	t.Run("1.5 pages breaks where it starts", func(t *testing.T) {
		goldenBreaks(t, "avoid_1_5_pages", blocks(1, "p")+`<div class=b style="break-inside:avoid">`+lines(9, "<br>")+`</div>`)
	})
	t.Run("0.9 page moves whole", func(t *testing.T) {
		goldenBreaks(t, "avoid_0_9_page", blocks(1, "p")+`<div class=n style="break-inside:avoid">`+lines(6, "<br>")+`</div>`)
	})
	t.Run("exactly a page moves whole", func(t *testing.T) {
		goldenBreaks(t, "avoid_1_page", blocks(1, "p")+`<div class=b style="break-inside:avoid">`+lines(6, "<br>")+`</div>`)
	})
}

func TestMeasureIsBounded(t *testing.T) {
	const depth = 508
	html := func(attr string) string {
		return `<div class=b>x</div>` + strings.Repeat(`<div `+attr+`>`, depth) + strings.Repeat("a<br>", 300) + strings.Repeat("</div>", depth)
	}
	work := func(attr string) (int64, int) {
		r := newRun(t, html(attr), opts{extra: smallPage, css: breaksCSS + " .b { line-height: 20px }"})
		seen := map[*box.Box]int{}
		r.pag.l.onMeasure = func(b *box.Box) { seen[b]++ }
		r.all(t)
		for b, n := range seen {
			if n > 1 {
				t.Errorf("a box measured %d times (%p)", n, b)
			}
		}
		return r.b.Used(budget.Work), len(seen)
	}
	twin, _ := work("")
	got, measured := work(`style="break-inside:avoid"`)
	if measured == 0 {
		t.Error("nothing was measured")
	}
	if got > 10*twin {
		t.Errorf("work %d, twin %d: more than 10x", got, twin)
	}
}

func TestMeasureCapPerPage(t *testing.T) {
	// 40 avoid boxes taller than a page, each after content: at most 8 are
	// measured per page, the others break where they are.
	html := strings.Repeat(`<div class=b>x</div><div style="break-inside:avoid">`+strings.Repeat("a<br>", 14)+`</div>`, 40)
	r := newRun(t, html, opts{extra: smallPage, css: breaksCSS})
	perPage, maxPer := 0, 0
	r.pag.l.onMeasure = func(*box.Box) { perPage++; maxPer = max(maxPer, perPage) }
	for {
		perPage = 0
		if _, err := r.pag.Next(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if maxPer == 0 || maxPer > maxMeasures {
		t.Errorf("%d measurements on one page, want 1 to %d", maxPer, maxMeasures)
	}
}

func TestSnapshotsAreIndependent(t *testing.T) {
	f1, f2 := &frag{}, &frag{}
	f1.rect.Y, f2.rect.Y = 10, 20
	l := &layouter{}
	var snap breakSnap
	l.waiting = []*frag{f1}
	l.saveBreak(&snap)
	l.waiting = []*frag{f2}
	l.saveBreak(&snap)
	f2.rect.Y = 99
	l.restoreBreak(&snap)
	if f2.rect.Y != 20 || len(l.waiting) != 1 || l.waiting[0] != f2 {
		t.Errorf("a reused snapshot restored the first save: y = %v", f2.rect.Y)
	}
}

func TestTwoChainsInOneParent(t *testing.T) {
	html := `<div id=par>` + blocks(2, "p") + `<h2 class=b id=h1 style="break-after:avoid">h</h2><div class=b id=a>a</div><div class=b id=m>m</div>` +
		`<h2 class=b id=h2 style="break-after:avoid">h</h2><div class=b id=z>z</div></div>`
	pages := breakRun(t, html)
	if pg, _ := pageOf(t, pages, "h2"); pg != 1 {
		t.Errorf("second heading on page %d, want 1", pg)
	}
	if pg, y := pageOf(t, pages, "z"); pg != 1 || y != 100 {
		t.Errorf("z on page %d y %v", pg, y)
	}
	if pg, _ := pageOf(t, pages, "a"); pg != 0 {
		t.Errorf("a on page %d, want 0", pg)
	}
}

func TestMeasureTruncatesTheTopMargin(t *testing.T) {
	// A box 580px tall with a 40px top margin, after content: it fits a fresh
	// page (the margin is truncated there), so it moves whole.
	html := blocks(1, "p") + `<div style="break-inside:avoid;margin-top:40px;padding-bottom:40px" class=n id=c>` + strings.Repeat("a<br>", 5) + `a</div>`
	pages := breakRun(t, html)
	if pg, y := pageOf(t, pages, "c"); pg != 1 || y != 0 {
		t.Errorf("box on page %d y %v, want the top of page 1", pg, y)
	}
	if len(pages) != 2 {
		t.Errorf("%d pages", len(pages))
	}
}

func TestTemplatesAvoidCost(t *testing.T) {
	for _, tpl := range []string{"01-modern", "02-executive"} {
		html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), tpl, testcorpus.Large)
		if err != nil {
			t.Fatal(err)
		}
		work := func(extra string) int64 {
			r := newRun(t, string(html), opts{extra: testcorpus.ChromePaperCSS + extra, scale: 0.9})
			r.all(t)
			return r.b.Used(budget.Work)
		}
		with, without := work(""), work("* { break-inside: auto !important }")
		t.Logf("%s: work %d, without break-inside %d", tpl, with, without)
		if with > 2*without {
			t.Errorf("%s: work %d > 2 x %d", tpl, with, without)
		}
	}
}

func TestRowAvoidFirstRowOfATableMidPage(t *testing.T) {
	pages := breakRun(t, blocks(4, "p")+tableHTML(`style="break-inside:avoid"`))
	for _, f := range frags(pages, "tr") {
		if !f.first || !f.last {
			t.Errorf("row %v is split", f.rect)
		}
	}
	if pg, y := pageOf(t, pages, "r0"); pg != 1 || y != 0 {
		t.Errorf("first row on page %d y %v, want the top of page 1", pg, y)
	}
}

func TestTallAvoidRowIsCutWhereItStarts(t *testing.T) {
	cell := strings.Repeat("x<br>", 8) + "x"
	layout := func(attr string) []*frag {
		html := blocks(2, "p") + `<table style="border-spacing:0;border-collapse:separate"><tr ` + attr + `><td class=b id=tall>` + cell + `</td></tr></table>`
		pages := breakRun(t, html)
		if pg, y := pageOf(t, pages, "tall"); pg != 0 || y != 200 {
			t.Errorf("%q: row starts on page %d at y=%v, want page 0 at 200", attr, pg, y)
		}
		return frags(pages, "tr")
	}
	avoid, plain := layout(`style="break-inside:avoid"`), layout("")
	if len(avoid) != len(plain) || len(avoid) < 2 {
		t.Fatalf("%d row fragments with avoid, %d without", len(avoid), len(plain))
	}
	for i := range avoid {
		if avoid[i].rect != plain[i].rect || avoid[i].first != plain[i].first || avoid[i].last != plain[i].last {
			t.Errorf("fragment %d differs from the plain table: %+v", i, avoid[i].rect)
		}
	}
}

func TestForcedBreaksInUnboundedContentKeepTheText(t *testing.T) {
	cell := func(attrs string) string {
		return `<div>AAA</div><div ` + attrs + `>BBB</div><div>CCC</div>`
	}
	for name, html := range map[string]string{
		"td break-before":    `<table><tr><td>` + cell(`style="break-before:page"`) + `</td></tr></table>`,
		"td break-after":     `<table><tr><td><div>AAA</div><div style="break-after:page">BBB</div><div>CCC</div></td></tr></table>`,
		"td avoid chain":     `<table><tr><td><h2 style="break-after:avoid">AAA</h2><p>BBB</p><p>CCC</p></td></tr></table>`,
		"td avoid inside":    `<table><tr><td><div>AAA</div><div style="break-inside:avoid">BBB</div><div>CCC</div></td></tr></table>`,
		"inline-block":       `<p>x <span style="display:inline-block">` + cell(`style="break-before:page"`) + `</span> y</p>`,
		"inline-block after": `<p><span style="display:inline-block"><div style="break-after:page">AAA</div><div>BBB</div><div>CCC</div></span></p>`,
		"nested td":          `<table><tr><td><table><tr><td>` + cell(`style="break-before:page"`) + `</td></tr></table></td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(lineTexts(breakRun(t, html)), "")
			for _, w := range []string{"AAA", "BBB", "CCC"} {
				if !strings.Contains(got, w) {
					t.Errorf("%q lost: %q", w, got)
				}
			}
		})
	}
	pages := breakRun(t, `<table><tr><td>`+cell(`style="break-before:page"`)+`</td></tr></table>`)
	if len(pages) != 1 {
		t.Errorf("%d pages: a forced break inside a cell must not break the page", len(pages))
	}
}

func TestAvoidChainWhenOnlyTheBottomDecorationDoesNotFit(t *testing.T) {
	html := blocks(5, "p") + `<h2 class=b id=h style="break-after:avoid;border-bottom:5px solid;line-height:98px">h</h2><div class=b id=n>n</div>`
	pages := breakRun(t, html)
	h, _ := pageOf(t, pages, "h")
	n, _ := pageOf(t, pages, "n")
	if h != n {
		t.Errorf("h on page %d and its next sibling on page %d", h, n)
	}
	for _, f := range frags(pages, "h2") {
		if !f.first || !f.last {
			t.Errorf("heading split: %+v", f.rect)
		}
	}
}
