package layout

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// atomLiftStyles are the hostile styles that lift a small atom far above the
// baseline of its line, or lower it far below.
var atomLiftStyles = []string{
	"margin-bottom:99999999px", "margin-bottom:-99999999px",
	"margin-top:99999999px", "margin-top:-99999999px",
	"vertical-align:1e8px", "vertical-align:-1e8px",
	"vertical-align:1e8px;margin-top:1e8px;margin-bottom:1e8px",
	"vertical-align:middle;margin-top:1e8px;margin-bottom:1e8px",
	"vertical-align:middle;margin-bottom:1e8px",
	"vertical-align:bottom;margin-top:1e8px",
}

// liftedAtom returns the markup of a small atom of kind svg, img (an svg turned
// into an image box by asImages) or inline-block with the style st.
func liftedAtom(kind, st string) string {
	if kind == "inline-block" {
		return `<span style="display:inline-block;` + st + `">x</span>`
	}
	return `<svg width="10" height="10" style="` + st + `"></svg>`
}

// wordsFullyInArea counts the words drawn with the middle of their x-height
// (baseline minus a quarter of the font size) inside the area of a page.
func wordsFullyInArea(pages []*Page) map[string]int {
	count := map[string]int{}
	for _, pg := range pages {
		h := pg.Area.H / pg.Scale
		for _, it := range pg.Items {
			t, ok := it.(*display.Text)
			if !ok {
				continue
			}
			if mid := t.Baseline - t.Size/4; mid >= 0 && mid <= h {
				for _, w := range strings.Fields(t.Text) {
					count[w]++
				}
			}
		}
	}
	return count
}

// A small atom lifted or lowered by a hostile margin or vertical-align, alone
// or with others, never puts the text of its line outside the page area: on A4
// and on a 400x300 page, in every formatting context, the render succeeds with
// every word drawn once inside a page area, or fails with a LimitError.
func TestAtomLiftKeepsTheLineText(t *testing.T) {
	for _, page := range []struct{ name, css string }{{"A4", ""}, {"400x300", tallAtomPage}} {
		for _, kind := range []string{"svg", "img", "inline-block"} {
			for _, st := range atomLiftStyles {
				for _, c := range tallAtomContexts {
					t.Run(page.name+"/"+kind+"/"+st+"/"+c.name, func(t *testing.T) {
						atomLiftRun(t, kind, page.css, `<p>before</p>`+strings.Replace(c.html, "X", liftedAtom(kind, st), 1))
					})
				}
				t.Run(page.name+"/"+kind+"/"+st+"/alone", func(t *testing.T) {
					atomLiftRun(t, kind, page.css, `<p>a `+liftedAtom(kind, st)+`</p>`)
				})
			}
		}
	}
}

func atomLiftRun(t *testing.T, kind, pageCSS, html string) {
	t.Helper()
	o := opts{extra: pageCSS, limits: func(l *budget.Limits) { l[budget.Pages] = hostileMaxPages }}
	if kind == "img" {
		o.boxes = asImages(imgSource(t))
	}
	r := newRun(t, html, o)
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		var le *budget.LimitError
		if errors.As(err, &le) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, pg)
	}
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if len(pages) > 6 {
		t.Errorf("%d pages, want at most 6", len(pages))
	}
	words := wordsFullyInArea(pages)
	want := []string{"a"}
	if strings.Contains(html, "before") {
		want = append(want, "before")
	}
	if kind == "inline-block" {
		want = append(want, "x")
	}
	for _, w := range want {
		if words[w] != 1 {
			t.Errorf("%q drawn %d times inside a page area", w, words[w])
		}
	}
}

// The repro documents of the task, through the whole pipeline with the default
// budget: one or two pages, never a page with the text below its area.
func TestAtomLiftHostileDocuments(t *testing.T) {
	const png = `data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAAAAAA6fptVAAAADElEQVR4nGNgYGAAAAAEAAH2FzhVAAAAAElFTkSuQmCC`
	docs := []struct{ name, html string }{
		{"img margin-bottom", `<p>a <img height="10" src="` + png + `" style="margin-bottom:99999999px"></p>`},
		{"img vertical-align", `<p>a <img height="10" src="` + png + `" style="vertical-align:1e8px"></p>`},
		{"svg margin-bottom", `<p>a <svg height="10" style="margin-bottom:99999999px"></svg></p>`},
		{"inline-block vertical-align", `<p>a <span style="display:inline-block;vertical-align:1e8px">x</span></p>`},
	}
	for _, page := range []struct{ name, css string }{{"A4", ""}, {"400x300", tallAtomPage}} {
		for _, d := range docs {
			t.Run(page.name+"/"+d.name, func(t *testing.T) {
				o := pipe(t, t.Context(), d.html, page.css, nil, hostileMaxPages)
				if o.err != nil {
					t.Fatalf("%s: %v", o.stage, o.err)
				}
				if o.pages > 2 {
					t.Errorf("%d pages, want at most 2", o.pages)
				}
			})
		}
	}
}

func TestLowerLift(t *testing.T) {
	const page, strut = 300.0, 4.0
	cases := []struct {
		name               string
		valign             style.VerticalAlign
		mt, mb, rise, base float64
		wantMt, wantMb     float64
		wantRise           float64
	}{
		{name: "fits", mt: 100, mb: 50, rise: 50, base: 10, wantMt: 100, wantMb: 50, wantRise: 50},
		{name: "rise first", rise: 300, base: 10, wantRise: 286},
		{name: "then the top margin", mt: 300, rise: 100, base: 10, wantMt: 286},
		{name: "bottom margin under no baseline", mb: 300, base: 310, wantMb: 286},
		{name: "bottom margin under a baseline", mt: 300, mb: 300, base: 5, wantMt: 291, wantMb: 300},
		{name: "all three", mt: 300, mb: 300, rise: 300, base: 310, wantMb: 286},
		{name: "negative ones stay", mt: -300, mb: -300, rise: -300, base: -290, wantMt: -300, wantMb: -300, wantRise: -300},
		{name: "middle", valign: style.VAlignMiddle, mt: 300, mb: 300, base: 310, wantMt: 282, wantMb: 300},
		{name: "bottom", valign: style.VAlignBottom, mt: 300, mb: 300, base: 310, wantMt: 0, wantMb: 290},
		{name: "top", valign: style.VAlignTop, mt: 300, mb: 300, base: 310, wantMt: 300, wantMb: 300},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.valign == style.VAlignBaseline && c.rise != 0 {
				c.valign = style.VAlignLength
			}
			a := &atomBox{h: 10, mt: c.mt, mb: c.mb, rise: c.rise, base: c.base, valign: c.valign}
			a.lowerLift(page, strut)
			if math.Abs(a.mt-c.wantMt) > 1e-9 || math.Abs(a.mb-c.wantMb) > 1e-9 || math.Abs(a.rise-c.wantRise) > 1e-9 {
				t.Errorf("mt %v mb %v rise %v, want %v %v %v", a.mt, a.mb, a.rise, c.wantMt, c.wantMb, c.wantRise)
			}
			if a.base > a.h+a.mb+1e-9 {
				t.Errorf("baseline %v below the box %v + %v", a.base, a.h, a.mb)
			}
			if above, _ := a.extents(); c.valign != style.VAlignTop && c.valign != style.VAlignBottom && above > page-strut+1e-9 {
				t.Errorf("extent above the baseline %v, want at most %v", above, page-strut)
			}
		})
	}
}

// leadingContexts are the formatting contexts a paragraph can sit in; X stands
// for the paragraph.
var leadingContexts = []struct{ name, html string }{
	{"inline", `X`},
	{"float", `<div style="float:left">X</div>`},
	{"flex", `<div style="display:flex">X</div>`},
	{"grid", `<div style="display:grid"><div>X</div></div>`},
	{"table", `<table><tr><td>X</td></tr></table>`},
	{"multicol", `<div style="columns:2">X</div>`},
	{"inline-block", `<div style="display:inline-block">X</div>`},
	{"padded flex", `<div style="display:flex;padding-top:220px"><div>X</div></div>`},
	{"padded cell", `<table><tr><td style="padding-top:220px">X</td></tr></table>`},
	{"flex padded 100", `<div style="display:flex;padding-top:100px"><div>X</div></div>`},
	{"cell padded 100", `<table><tr><td style="padding-top:100px">X</td></tr></table>`},
}

// A hostile line-height, or the vertical-align of text spans, never puts the
// glyphs of a line outside the page area: on A4 and on a 400x300 page, alone,
// after a paragraph or pushed down the page, in every formatting context, every
// word is drawn once with its x-height inside a page area, or the render fails
// with a LimitError.
func TestHugeLeadingKeepsTheLineText(t *testing.T) {
	const (
		down = `<span style="vertical-align:-1e8px;line-height:1e8px">b</span>`
		up   = `<span style="vertical-align:1e8px;line-height:1e8px">c</span>`
	)
	docs := []struct{ name, html string }{
		{"line-height px", `<p style="line-height:99999999px">a b</p>`},
		{"line-height number", `<p style="line-height:1e7">a b</p>`},
		{"line-height in a span", `<p>a <span style="line-height:99999999px">b</span></p>`},
		{"line-height in an inline-block", `<p>a <span style="display:inline-block;line-height:99999999px">b</span></p>`},
		{"one-pixel inline-block", `<p><span style="display:inline-block;width:1px;line-height:1e7px">a b</span></p>`},
		{"huge font", `<p style="font-size:1e9px;line-height:1e9px">a</p>`},
		{"raised span", `<p>a <span style="vertical-align:1e8px">b</span></p>`},
		{"lowered span", `<p>a <span style="vertical-align:-1e8px">b</span></p>`},
		{"raised and lowered", `<p>a <span style="vertical-align:-1e9px">b<span style="vertical-align:1e9px">c</span></span></p>`},
		{"sibling spans", `<p>a ` + down + ` ` + up + `</p>`},
		{"line-height and nested spans", `<p style="line-height:1e7px">a <span style="vertical-align:-1e9px">b<span style="vertical-align:1e9px">c</span></span></p>`},
		{"200 px font, lowered and raised spans", `<p style="font-size:200px;line-height:1">a <span style="vertical-align:-1e8px">b</span> <span style="vertical-align:1e8px">c</span></p>`},
		{"200 px font and a lowered span", `<p style="font-size:200px;line-height:1">a <span style="vertical-align:-1e8px">b</span></p>`},
		{"moderate nested spans", `<p>a <span style="vertical-align:-40px">b<span style="vertical-align:80px">c</span></span></p>`},
		{"lowered 70 px", `<p>a <span style="vertical-align:-70px">b</span></p>`},
		{"margin and a lowered span", `<p style="margin-top:250px">a <span style="vertical-align:-1e9px">b</span></p>`},
	}
	preambles := []struct{ name, html string }{{"alone", ""}, {"after a paragraph", `<p>before</p>`}, {"pushed", `<div style="height:100px"></div>`}}
	for _, page := range []struct{ name, css string }{{"A4", ""}, {"400x300", tallAtomPage}} {
		for _, d := range docs {
			for _, c := range leadingContexts {
				for _, pre := range preambles {
					t.Run(page.name+"/"+d.name+"/"+c.name+"/"+pre.name, func(t *testing.T) {
						leadingRun(t, page.css, pre.html+strings.Replace(c.html, "X", d.html, 1))
					})
				}
			}
		}
	}
}

// On a small named page, the spans of a line stay on it.
func TestHugeLeadingOnASmallNamedPage(t *testing.T) {
	leadingRun(t, tallAtomPage+` @page small { size: 200px 120px; margin: 0 }`, `<p>before</p><p style="page:small">a <span style="vertical-align:-1e8px;line-height:1e8px">b</span> <span style="vertical-align:1e8px;line-height:1e8px">c</span></p>`)
}

// Stacked lowered spans and a 1e8 px pile of capped lines in a flex item: the
// cuts always progress, and the render ends in a success or a LimitError.
func TestHugeLeadingProgresses(t *testing.T) {
	docs := map[string]string{
		"stacked spans": `<p>a ` + strings.Repeat(`<span style="vertical-align:-1e8px">b `, 200) + `</p>`,
		"pile in flex":  `<div style="display:flex"><div style="line-height:1e8px">` + strings.Repeat(`w <span style="vertical-align:-1e8px">v</span><br>`, 3000) + `</div></div>`,
	}
	for name, html := range docs {
		t.Run(name, func(t *testing.T) {
			// The default budget bounds the run: Pages and Work end a runaway.
			o := pipe(t, t.Context(), html, "", nil, math.MaxInt)
			var le *budget.LimitError
			if o.err != nil && !errors.As(o.err, &le) {
				t.Fatalf("%s: %v", o.stage, o.err)
			}
		})
	}
}

func leadingRun(t *testing.T, pageCSS, html string) {
	t.Helper()
	r := newRun(t, html, opts{extra: pageCSS, limits: func(l *budget.Limits) { l[budget.Pages] = hostileMaxPages }})
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		var le *budget.LimitError
		if errors.As(err, &le) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, pg)
	}
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if len(pages) > 6 && !strings.Contains(html, "font-size:1e9px") {
		t.Errorf("%d pages, want at most 6", len(pages))
	}
	if strings.Contains(html, "font-size:1e9px") {
		// A glyph taller than the page cannot be inside it: TextLost counts it
		// when it covers the page area.
		return
	}
	words := wordsFullyInArea(pages)
	for _, w := range []string{"before", "a", "b", "c", "x"} {
		if strings.Contains(html, ">"+w+"<") || strings.Contains(html, ">"+w+" ") || strings.Contains(html, " "+w+"<") {
			if words[w] != 1 {
				t.Errorf("%q drawn %d times inside a page area", w, words[w])
			}
		}
	}
}

func TestLineHeightPartsCapsTheLeading(t *testing.T) {
	px := func(v float64) *style.Computed {
		return &style.Computed{FontSize: 16, LineHeight: style.LineHeight{Kind: style.LineHeightPx, Value: v}}
	}
	cases := []struct {
		name            string
		st              *style.Computed
		page            float64
		wantTop, wantBo float64
	}{
		{"normal leading", px(20), 300, 16, 4},
		{"capped to the page", px(1e8), 300, 14 + 142, 2 + 142},
		{"no page", px(1e8), 0, 14 + (1e8-16)/2, 2 + (1e8-16)/2},
		{"negative leading kept", px(4), 300, 8, -4},
		{"content taller than the page", px(1e8), 10, 14, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			top, bottom := lineHeightParts(c.st, 14, 2, 0, c.page)
			if math.Abs(top-c.wantTop) > 1e-6 || math.Abs(bottom-c.wantBo) > 1e-6 {
				t.Errorf("top %v bottom %v, want %v %v", top, bottom, c.wantTop, c.wantBo)
			}
			if c.page > 16 && top+bottom > c.page+1e-6 {
				t.Errorf("line box %v taller than the page %v", top+bottom, c.page)
			}
		})
	}
}

func TestClampRise(t *testing.T) {
	l := &layouter{pageH: 400}
	for _, c := range []struct{ in, want float64 }{{50, 50}, {1e9, 100}, {-1e9, -100}, {-100, -100}} {
		if got := l.clampRise(c.in); got != c.want {
			t.Errorf("clampRise(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := (&layouter{}).clampRise(1e30); got != clampCoord(1e30) {
		t.Errorf("without a page: %v, want the length clamp", got)
	}
}

// Tall atoms of the hostile corpus raised or lowered past the page: the text of
// their line is seen on a page.
func TestTallAtomDocumentsKeepTheirText(t *testing.T) {
	docs := map[string]string{
		"tall and lowered":        `<p>a <svg width="10" height="99999999" style="vertical-align:-1e8px"></svg></p>`,
		"negative margin-bottom":  `<p>a <svg width="10" height="99999999" style="margin-bottom:-99999999px"></svg> b</p>`,
		"line-height around atom": `<p style="line-height:99999999px">a <span style="display:inline-block;line-height:normal"><span style="display:block">x</span>` + strings.Repeat(`<span style="display:block">w</span>`, 59) + `</span></p>`,
	}
	for name, html := range docs {
		t.Run(name, func(t *testing.T) {
			leadingRun(t, tallAtomPage, html)
		})
	}
}

func TestTextCutEnd(t *testing.T) {
	// ascent is the height of the glyphs above the baseline of the line.
	text := func(baseline, ascent float64) lineItem {
		return lineItem{text: &display.Text{Baseline: baseline, Size: 16}, ascent: ascent}
	}
	line := func(y, h, baseline float64, items ...lineItem) *lineFrag {
		return &lineFrag{rect: display.Rect{Y: y, H: h}, baseline: baseline, items: items}
	}
	cases := []struct {
		name     string
		lines    []*lineFrag
		from, to float64
		want     float64
	}{
		{"nothing astride", []*lineFrag{line(0, 20, 15, text(15, 12))}, 0, 300, 300},
		{"text astride", []*lineFrag{line(280, 40, 295, text(305, 12))}, 0, 300, 283},
		{"baseline above the end", []*lineFrag{line(280, 40, 295, text(295, 12))}, 0, 300, 300},
		{"cut between line boxes", []*lineFrag{line(300, 20, 312, text(312, 14))}, 0, 300, 300},
		{"glyph taller than the window", []*lineFrag{line(0, 400, 350, text(350, 340))}, 0, 300, 300},
		{"no progress", []*lineFrag{line(0, 40, 20, text(310, 310))}, 0, 300, 300},
		{"chained", []*lineFrag{line(250, 80, 270, text(290, 0), text(305, -15))}, 0, 300, 270},
		{"not above the middle", []*lineFrag{line(100, 220, 140, text(160, 10), text(305, -15))}, 0, 300, 155},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := textCutEnd(&frag{lines: c.lines}, c.from, c.to, c.from+(c.to-c.from)/2, 300)
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("end %v, want %v", got, c.want)
			}
			if got <= c.from+cutEps {
				t.Errorf("end %v does not progress from %v", got, c.from)
			}
		})
	}
}

// overlapGlyphs is a flex item of n lines whose glyphs overlap the lines above
// (a line-height of 0.4 at 30 px), so that every page break goes through a
// pile of glyphs.
func overlapGlyphs(n int) string {
	var sb strings.Builder
	sb.WriteString(`<div style="display:flex"><div style="font-size:30px;line-height:0.4">`)
	for i := range n {
		fmt.Fprintf(&sb, "w%d<br>", i)
	}
	sb.WriteString(`</div></div>`)
	return sb.String()
}

// The end of a window moves up over a pile of overlapping glyphs in one sweep,
// and never above its middle: Work and pages stay linear in the lines.
func TestTextCutEndIsLinear(t *testing.T) {
	const (
		// unlimitedWork lets the budget measure the Work of the runs, not stop them.
		unlimitedWork = 1 << 40
		// linearRatio: doubling the input may at most double the work and the
		// pages, plus 10% for page-boundary effects.
		linearRatio = 2.2
	)
	run := func(n int) (work int64, pages int) {
		r := newRun(t, overlapGlyphs(n), opts{extra: tallAtomPage, limits: func(l *budget.Limits) { l[budget.Pages] = 100000; l[budget.Work] = unlimitedWork }})
		pages = len(r.all(t))
		return r.b.Used(budget.Work), pages
	}
	w1, p1 := run(2000)
	w2, p2 := run(4000)
	if float64(w2) > linearRatio*float64(w1) {
		t.Errorf("work %d for 4000 lines against %d for 2000", w2, w1)
	}
	if float64(p2) > linearRatio*float64(p1)+1 {
		t.Errorf("%d pages for 4000 lines against %d for 2000", p2, p1)
	}
	// 12 px a line on 300 px pages, at least half a page each: at most 2 × 80.
	if p1 > 2*2000*12/300+2 {
		t.Errorf("%d pages for 2000 lines of 12 px: the windows shrink", p1)
	}
}

// A table row cut between lines whose glyphs overhang their line boxes
// (line-height 0.5): the cut index finds the lines by their glyph tops, every
// line is drawn once and seen on a page.
func TestTableCutThroughOverhangingGlyphs(t *testing.T) {
	var sb strings.Builder
	for i := range 60 {
		fmt.Fprintf(&sb, "<div>r%d</div>", i)
	}
	r := newRun(t, `<table><tr><td style="line-height:0.5">`+sb.String()+`</td></tr></table>`, opts{extra: "@page { size: 300px 120px; margin: 0 }"})
	pages := r.all(t)
	if lost := TextLost(r.root, pages); lost != "" {
		t.Errorf("text lost: %s", lost)
	}
	if len(pages) < 2 {
		t.Fatalf("%d pages: the row is not cut", len(pages))
	}
	words := wordsFullyInArea(pages)
	for i := range 60 {
		if w := fmt.Sprintf("r%d", i); words[w] != 1 {
			t.Errorf("%s drawn %d times inside a page area", w, words[w])
		}
	}
}
