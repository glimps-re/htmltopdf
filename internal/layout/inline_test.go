package layout

import (
	"errors"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
)

func firstLine(t *testing.T, html string) *lineFrag {
	t.Helper()
	r := newRun(t, html, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	pages := r.all(t)
	lines := linesOf(pages[0])
	if len(lines) == 0 {
		t.Fatal("no line")
	}
	return lines[0]
}

func lineText(ln *lineFrag) string {
	var sb strings.Builder
	for _, it := range ln.items {
		if it.text != nil {
			sb.WriteString(it.text.Text)
		}
	}
	return sb.String()
}

func textWidth(ln *lineFrag) float64 {
	var w float64
	for _, it := range ln.items {
		if it.kind == itemText {
			w += it.w
		}
	}
	return w
}

func TestTextAlign(t *testing.T) {
	left := firstLine(t, `<p style="width:400px">abc</p>`)
	right := firstLine(t, `<p style="width:400px;text-align:right">abc</p>`)
	center := firstLine(t, `<p style="width:400px;text-align:center">abc</p>`)
	justify := firstLine(t, `<p style="width:400px;text-align:justify">abc def</p>`)
	w := textWidth(left)
	if !near(left.items[0].x, 0) || !near(right.items[0].x, 400-w) || !near(center.items[0].x, (400-w)/2) {
		t.Errorf("x: left %v right %v center %v (width %v)", left.items[0].x, right.items[0].x, center.items[0].x, w)
	}
	if !near(justify.items[0].x, 0) {
		t.Errorf("justify is laid out as start: %v", justify.items[0].x)
	}
}

func TestOverflowingLineIsStartAligned(t *testing.T) {
	ln := firstLine(t, `<p style="width:20px;text-align:center">WWWWWW</p>`)
	if ln.items[0].x != 0 {
		t.Errorf("x = %v", ln.items[0].x)
	}
}

func TestLetterSpacing(t *testing.T) {
	a := firstLine(t, `<p>abcd</p>`)
	b := firstLine(t, `<p style="letter-spacing:3px">abcd</p>`)
	if !near(textWidth(b)-textWidth(a), 12) {
		t.Errorf("widths %v and %v", textWidth(a), textWidth(b))
	}
	var adv float64
	for _, g := range b.items[0].text.Glyphs {
		adv += float64(g.Advance)
	}
	if !near(adv, textWidth(b)) {
		t.Errorf("glyph advances %v vs run %v", adv, textWidth(b))
	}
}

func TestLineHeight(t *testing.T) {
	normal := firstLine(t, `<p>x</p>`)
	number := firstLine(t, `<p style="line-height:2">x</p>`)
	px := firstLine(t, `<p style="line-height:30px">x</p>`)
	small := firstLine(t, `<p style="line-height:1px">x</p>`)
	if !near(normal.rect.H, 18) || !near(number.rect.H, 32) || !near(px.rect.H, 30) || !near(small.rect.H, 1) {
		t.Errorf("heights: normal %v number %v px %v small %v", normal.rect.H, number.rect.H, px.rect.H, small.rect.H)
	}
	// Half-leading: the baseline sits in the middle of the extra space.
	if !near(number.baseline-number.rect.Y, normal.baseline-normal.rect.Y+(32-18)/2) {
		t.Errorf("baseline %v", number.baseline)
	}
}

func TestBiggerInlineFontGrowsTheLine(t *testing.T) {
	ln := firstLine(t, `<p>small <span style="font-size:40px">BIG</span> small</p>`)
	if ln.rect.H < 40 {
		t.Errorf("line height %v", ln.rect.H)
	}
	var sizes []float64
	for _, it := range ln.items {
		if it.text != nil {
			sizes = append(sizes, it.text.Size)
		}
	}
	if len(sizes) != 3 || sizes[0] != 16 || sizes[1] != 40 || sizes[2] != 16 {
		t.Errorf("sizes %v", sizes)
	}
	if got := lineText(ln); got != "small BIG small" {
		t.Errorf("text %q", got)
	}
}

func TestWrapAtSpacesAndStripSpaces(t *testing.T) {
	r := newRun(t, `<p style="width:100px">aaa bbb ccc ddd eee fff</p>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	lines := linesOf(r.all(t)[0])
	if len(lines) < 2 {
		t.Fatalf("%d lines", len(lines))
	}
	for _, ln := range lines {
		txt := lineText(ln)
		if strings.HasPrefix(txt, " ") || strings.HasSuffix(txt, " ") {
			t.Errorf("line %q keeps edge spaces", txt)
		}
		if textWidth(ln) > 100.01 {
			t.Errorf("line %q is %v wide", txt, textWidth(ln))
		}
	}
}

func TestWhiteSpace(t *testing.T) {
	r := newRun(t, `<p style="width:50px;white-space:nowrap">aaa bbb ccc ddd</p><pre style="width:50px;margin:0">one
two  spaced
three</pre>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	pages := r.all(t)
	ps := frags(pages, "p")[0]
	if len(ps.lines) != 1 || ps.lines[0].items[0].w <= 50 {
		t.Errorf("nowrap text stays on one overflowing line: %d lines", len(ps.lines))
	}
	pre := frags(pages, "pre")[0]
	if len(pre.lines) != 3 {
		t.Fatalf("pre: %d lines", len(pre.lines))
	}
	if got := lineText(pre.lines[1]); got != "two  spaced" {
		t.Errorf("pre keeps its spaces: %q", got)
	}
}

func TestLineBreaks(t *testing.T) {
	r := newRun(t, `<p>a<br>b<br><br>c<br></p>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	var got []string
	for _, ln := range linesOf(r.all(t)[0]) {
		got = append(got, lineText(ln))
	}
	// A trailing br ends the line without adding an empty one.
	if strings.Join(got, "|") != "a|b||c" {
		t.Errorf("lines %q", got)
	}
}

func TestUnderlineAndLinks(t *testing.T) {
	r := newRun(t, `<p>see <a href="https://example.com/x">the <b>link</b> text</a> and <u>more</u> <s>gone</s></p>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	pg := r.all(t)[0]
	var under, strike []string
	var links []display.Link
	for _, it := range pg.Items {
		switch v := it.(type) {
		case *display.Text:
			if v.Underline {
				under = append(under, v.Text)
			}
			if v.LineThrough {
				strike = append(strike, v.Text)
			}
		case display.Link:
			links = append(links, v)
		}
	}
	if strings.Join(under, "") != "the link textmore" {
		t.Errorf("underlined: %q", under)
	}
	if strings.Join(strike, "") != "gone" {
		t.Errorf("struck: %q", strike)
	}
	if len(links) != 1 || links[0].URI != "https://example.com/x" || links[0].Rect.W <= 0 || links[0].Rect.H <= 0 {
		t.Errorf("links: %+v", links)
	}
}

func TestLinkWrappedOverTwoLines(t *testing.T) {
	r := newRun(t, `<p style="width:100px"><a href="https://e.com/">aaa bbb ccc ddd eee</a></p>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	var links []display.Link
	for _, it := range r.all(t)[0].Items {
		if l, ok := it.(display.Link); ok {
			links = append(links, l)
		}
	}
	if len(links) < 2 {
		t.Fatalf("%d links", len(links))
	}
	if links[0].Rect.Y >= links[1].Rect.Y {
		t.Errorf("link rects: %+v", links)
	}
}

func TestInlineBoxBackgroundAndPadding(t *testing.T) {
	r := newRun(t, `<p>a<span style="background:#ff0;padding:0 10px;margin:0 5px;border:2px solid red">bc</span>d</p>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	pg := r.all(t)[0]
	var fills []display.FillRect
	var borders []display.Border
	for _, it := range pg.Items {
		switch v := it.(type) {
		case display.FillRect:
			fills = append(fills, v)
		case display.Border:
			borders = append(borders, v)
		}
	}
	if len(fills) != 1 || len(borders) != 1 {
		t.Fatalf("%d fills, %d borders", len(fills), len(borders))
	}
	// The box includes padding and border, not the margin.
	ln := linesOf(pg)[0]
	var tw float64
	for _, it := range ln.items {
		if it.text != nil && it.text.Text == "bc" {
			tw = it.w
		}
	}
	if !near(fills[0].Rect.W, tw+2*10+2*2) {
		t.Errorf("fill width %v, want %v", fills[0].Rect.W, tw+24)
	}
	if borders[0].Sides[0].Width != 2 || borders[0].Sides[3].Width != 2 || borders[0].Sides[1].Width != 2 {
		t.Errorf("borders: %+v", borders[0].Sides)
	}
	// The margins and the paddings push the text.
	var xs []float64
	for _, it := range ln.items {
		if it.text != nil {
			xs = append(xs, it.x)
		}
	}
	if len(xs) != 3 || xs[1]-xs[0] < 5+2+10 {
		t.Errorf("xs %v", xs)
	}
}

func TestInlineBoxSplitAcrossLines(t *testing.T) {
	r := newRun(t, `<p style="width:80px">x <span style="background:#ff0;border:1px solid blue">aaa bbb ccc ddd</span></p>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	pg := r.all(t)[0]
	fills := 0
	for _, it := range pg.Items {
		if _, ok := it.(display.FillRect); ok {
			fills++
		}
	}
	if fills < 2 {
		t.Errorf("%d fills for an inline box over several lines", fills)
	}
}

func TestBlockBackgroundsClipAndPercent(t *testing.T) {
	r := newRun(t, `<div id=o style="width:50%;padding:5%;overflow:hidden;border-radius:10px;background:linear-gradient(red,blue)"><p>clipped text</p></div>`, opts{extra: smallPage, css: "body { margin: 0 } p { margin: 0 }"})
	pg := r.all(t)[0]
	o := fragByID(t, []*Page{pg}, "o")
	if !near(o.rect.W, 400+80) || !near(o.padding[0], 40) {
		t.Errorf("box %+v padding %v", o.rect, o.padding)
	}
	var push, pop, grad int
	for _, it := range pg.Items {
		switch v := it.(type) {
		case display.PushClip:
			push++
			if v.Radii[0].H <= 0 {
				t.Errorf("clip radii %+v", v.Radii)
			}
		case display.PopClip:
			pop++
		case display.FillGradient:
			grad++
		}
	}
	if push != 1 || pop != 1 || grad != 1 {
		t.Errorf("push %d pop %d gradient %d", push, pop, grad)
	}
}

func TestRadiiAreScaledToTheBox(t *testing.T) {
	r := newRun(t, `<div style="width:100px;height:0;padding:10px 0;background:#000;border-radius:999px">x</div>`, opts{extra: smallPage, css: "body { margin: 0 }"})
	pg := r.all(t)[0]
	for _, it := range pg.Items {
		if f, ok := it.(display.FillRect); ok {
			rect := f.Rect
			for i, c := range f.Radii {
				if c.H > rect.W/2+1e-6 || c.V > rect.H+1e-6 {
					t.Errorf("corner %d: %+v for %+v", i, c, rect)
				}
			}
			if f.Radii[0].H+f.Radii[1].H > rect.W+1e-6 {
				t.Errorf("top radii %+v exceed the width %v", f.Radii, rect.W)
			}
		}
	}
}

func TestNegativeAndHugeWidthsStayFinite(t *testing.T) {
	r := newRun(t, `<div style="margin-left:-1e30px;width:1e30px;padding-left:1e30px">x</div><p style="width:-5px;margin-right:-1e9px">y</p>`, opts{extra: smallPage})
	for _, pg := range r.all(t) {
		for _, it := range pg.Items {
			checkFinite(t, it)
		}
	}
}

func TestPagesLimit(t *testing.T) {
	r := newRun(t, `<div style="padding-top:1e7px">x</div>`, opts{extra: "@page { size: 300px 150px; margin: 0 }", limits: func(l *budget.Limits) { l[budget.Pages] = 5 }})
	var err error
	n := 0
	for {
		if _, err = r.pag.Next(); err != nil {
			break
		}
		n++
	}
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Pages || n != 5 {
		t.Errorf("after %d pages: %v", n, err)
	}
}

func TestWorkLimitAndCancellation(t *testing.T) {
	r := newRun(t, `<p>`+paragraphOf(2000)+`</p>`, opts{extra: smallPage, limits: func(l *budget.Limits) { l[budget.Work] = 400 }})
	_, err := r.pag.Next()
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Errorf("got %v", err)
	}
}

func TestEverythingEndsAtThePageBottom(t *testing.T) {
	html := manyParagraphs(60) + `<div style="border:4px solid;padding:20px">` + manyParagraphs(10) + `</div><ul><li>a<li>b</ul>`
	r := newRun(t, html, opts{extra: "@page { size: 400px 300px; margin: 20px }", scale: 0.8})
	pages := r.all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	h := pages[0].Area.H / pages[0].Scale
	for i, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.rect.Bottom() > h+0.01 && f.box.Tag.String() != "html" && f.box.Tag.String() != "body" {
				t.Errorf("page %d: %s ends at %v > %v", i+1, f.box.Tag, f.rect.Bottom(), h)
			}
		})
		for _, ln := range linesOf(pg) {
			if ln.rect.Bottom() > h+0.01 {
				t.Errorf("page %d: line ends at %v > %v", i+1, ln.rect.Bottom(), h)
			}
		}
	}
}

func TestMarginsAreTruncatedAtPageBreaks(t *testing.T) {
	// Each paragraph has a 60px margin; a page break between two of them drops it.
	r := newRun(t, strings.Repeat(`<p style="margin:60px 0">line</p>`, 6), opts{extra: "@page { size: 300px 200px; margin: 0 }", css: "body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) < 2 {
		t.Fatalf("%d pages", len(pages))
	}
	for i, pg := range pages[1:] {
		first := linesOf(pg)[0]
		if !near(first.rect.Y, 0) {
			t.Errorf("page %d starts at y=%v: the margin must be truncated", i+2, first.rect.Y)
		}
	}
	if y := linesOf(pages[0])[0].rect.Y; !near(y, 60) {
		t.Errorf("the first page keeps the top margin: %v", y)
	}
}
