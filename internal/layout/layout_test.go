package layout

import (
	"errors"
	"flag"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"github.com/glimps-re/htmltopdf/internal/text"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares the dump of pages with testdata/golden/<name>.layout.txt.
func golden(t *testing.T, name string, pages []*Page) {
	t.Helper()
	got := dumpPages(t, pages)
	path := filepath.Join("testdata", "golden", name+".layout.txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from its golden file:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func walk(f *frag, fn func(*frag)) {
	if f == nil {
		return
	}
	fn(f)
	for _, c := range f.children {
		walk(c, fn)
	}
}

// frags returns the fragments of the elements with the given tag, in order, on
// all pages.
func frags(pages []*Page, tag string) []*frag {
	var out []*frag
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Tag.String() == tag {
				out = append(out, f)
			}
		})
	}
	return out
}

func fragByID(t *testing.T, pages []*Page, id string) *frag {
	t.Helper()
	for _, pg := range pages {
		var found *frag
		walk(pg.root, func(f *frag) {
			if f.box.ID == id && found == nil {
				found = f
			}
		})
		if found != nil {
			return found
		}
	}
	t.Fatalf("no element #%s", id)
	return nil
}

func linesOf(pg *Page) []*lineFrag {
	var out []*lineFrag
	walk(pg.root, func(f *frag) { out = append(out, f.lines...) })
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestPageGeometry(t *testing.T) {
	r := newRun(t, `<p>x</p>`, opts{extra: "@page { size: 800px 600px; margin: 50px 10px 20px 30px }", scale: 0.5})
	pg := r.all(t)[0]
	if pg.Width != 800 || pg.Height != 600 || pg.Scale != 0.5 {
		t.Errorf("page: %+v", pg)
	}
	if pg.Area != (display.Rect{X: 30, Y: 50, W: 760, H: 530}) {
		t.Errorf("area: %+v", pg.Area)
	}
	// The page area is laid out at 1/Scale: the html box is 1520 px wide.
	if w := pg.root.rect.W; !near(w, 1520) {
		t.Errorf("layout width = %v", w)
	}
}

func TestScaleBounds(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, 0.01: MinScale, 5: MaxScale, 0.9: 0.9} {
		r := newRun(t, `<p>x</p>`, opts{extra: smallPage, scale: in})
		if got := r.all(t)[0].Scale; got != want {
			t.Errorf("scale %v -> %v, want %v", in, got, want)
		}
	}
}

func TestEmptyDocumentHasOnePage(t *testing.T) {
	for _, html := range []string{``, `<body></body>`, `<div></div>`, `<p> </p>`} {
		r := newRun(t, html, opts{extra: smallPage})
		pages := r.all(t)
		if len(pages) != 1 {
			t.Errorf("%q: %d pages", html, len(pages))
		}
	}
}

func TestAutoMarginsCenter(t *testing.T) {
	html := `<div id=a style="width:200px;margin:0 auto">x</div>
<div id=b style="max-width:300px;margin:0 auto">y</div>
<div id=c style="width:200px;margin-left:auto">z</div>
<div id=d style="width:200px;margin-left:10px;margin-right:5px">w</div>
<div id=e style="width:50%;padding:0 10px;box-sizing:border-box;margin:0 auto">v</div>`
	r := newRun(t, html, opts{extra: smallPage, css: "body { margin: 0 }"})
	pages := r.all(t)
	divs := frags(pages, "div")
	want := []struct{ x, w float64 }{{300, 200}, {250, 300}, {600, 200}, {10, 200}, {200, 400}}
	for i, w := range want {
		if !near(divs[i].rect.X, w.x) || !near(divs[i].rect.W, w.w) {
			t.Errorf("div %d: x=%v w=%v, want x=%v w=%v", i, divs[i].rect.X, divs[i].rect.W, w.x, w.w)
		}
	}
	golden(t, "centering", pages)
}

func TestMarginCollapsing(t *testing.T) {
	r := newRun(t, `<div id=a style="margin:10px 0 20px">A</div>
<div id=b style="margin:30px 0 5px">B</div>
<div id=c style="margin-top:-8px">C</div>
<div id=e style="margin:40px 0"></div>
<div id=f style="margin:0">F</div>
<div id=g style="overflow:hidden;margin:0"><div style="margin:25px 0">G</div></div>
<div id=h style="padding-top:1px;margin:0"><div style="margin-top:12px">H</div></div>
<div id=i style="margin:0"><div style="margin-top:50px">I</div></div>`, opts{extra: smallPage, css: "body { margin: 0 }"})
	pages := r.all(t)
	div := func(id string) *frag { return fragByID(t, pages, id) }
	lh := 18.0
	// A: top margin 10 (body has none, root is a BFC).
	if !near(div("a").rect.Y, 10) {
		t.Errorf("A.y = %v", div("a").rect.Y)
	}
	// B follows A: 20 and 30 collapse to 30.
	if !near(div("b").rect.Y, 10+lh+30) {
		t.Errorf("B.y = %v, want %v", div("b").rect.Y, 10+lh+30)
	}
	// C: B's bottom margin 5 and C's top -8 collapse to -3.
	if !near(div("c").rect.Y, div("b").rect.Y+lh-3) {
		t.Errorf("C.y = %v, want %v", div("c").rect.Y, div("b").rect.Y+lh-3)
	}
	// e is empty: its margins collapse through, with C's bottom (0) and f's top (0): 40.
	cBottom := div("c").rect.Y + lh
	if !near(div("f").rect.Y, cBottom+40) {
		t.Errorf("F.y = %v, want %v", div("f").rect.Y, cBottom+40)
	}
	// g clips: the margin of its child stays inside: g.y = f bottom, child 25 below.
	fb := div("f").rect.Y + lh
	if !near(div("g").rect.Y, fb) || !near(div("g").rect.H, 25+lh+25) {
		t.Errorf("g: y=%v h=%v, want y=%v h=%v", div("g").rect.Y, div("g").rect.H, fb, 25+lh+25)
	}
	// h has padding: the child's margin stays inside.
	if !near(div("h").rect.H, 1+12+lh) {
		t.Errorf("h.h = %v, want %v", div("h").rect.H, 1+12+lh)
	}
	// i has neither: the child's margin collapses through to i's top.
	gb := div("g").rect.Y + div("g").rect.H
	hb := div("h").rect.Y + div("h").rect.H
	if !near(div("i").rect.Y, hb+50) || hb < gb {
		t.Errorf("i.y = %v, want %v", div("i").rect.Y, hb+50)
	}
	golden(t, "collapsing", pages)
}

func TestLongWord(t *testing.T) {
	word := strings.Repeat("W", 60)
	css := "body { margin: 0 } div { width: 100px; margin: 0 }"
	r := newRun(t, `<div>`+word+`</div>`, opts{extra: smallPage, css: css})
	pages := r.all(t)
	if n := len(linesOf(pages[0])); n != 1 {
		t.Errorf("without overflow-wrap: %d lines, want 1", n)
	}
	if w := linesOf(pages[0])[0].items[0].w; w <= 100 {
		t.Errorf("the word must overflow: %v", w)
	}
	golden(t, "longword-overflow", pages)

	r = newRun(t, `<div>`+word+`</div>`, opts{extra: smallPage, css: css + " div { overflow-wrap: break-word }"})
	pages = r.all(t)
	lines := linesOf(pages[0])
	if len(lines) < 5 {
		t.Fatalf("break-word: %d lines", len(lines))
	}
	var rebuilt strings.Builder
	for _, ln := range lines {
		if ln.items[0].w > 100+0.01 {
			t.Errorf("a broken line is %v px wide", ln.items[0].w)
		}
		for _, it := range ln.items {
			rebuilt.WriteString(it.text.Text)
		}
	}
	if rebuilt.String() != word {
		t.Errorf("text lost: %q", rebuilt.String())
	}
	golden(t, "longword-break", pages)

	// word-wrap is the same property.
	r = newRun(t, `<div>`+word+`</div>`, opts{extra: smallPage, css: css + " div { word-wrap: break-word }"})
	if n := len(linesOf(r.all(t)[0])); n != len(lines) {
		t.Errorf("word-wrap: %d lines, want %d", n, len(lines))
	}
}

func paragraphOf(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("word")
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteByte(' ')
	}
	return sb.String()
}

func TestBreakInsideParagraph(t *testing.T) {
	r := newRun(t, `<p>`+paragraphOf(400)+`</p>`, opts{extra: "@page { size: 300px 120px; margin: 10px }", css: "body { margin: 0 } p { margin: 0 }"})
	pages := r.all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	total := ""
	for i, pg := range pages {
		lines := linesOf(pg)
		if len(lines) == 0 {
			t.Fatalf("page %d is empty", i+1)
		}
		last := lines[len(lines)-1]
		if last.rect.Bottom() > 100+0.01 {
			t.Errorf("page %d: line ends at %v, past the page area", i+1, last.rect.Bottom())
		}
		if !near(lines[0].rect.Y, 0) {
			t.Errorf("page %d starts at %v", i+1, lines[0].rect.Y)
		}
		for _, ln := range lines {
			for _, it := range ln.items {
				total += it.text.Text
			}
			total += " "
		}
	}
	// Every word appears once, in order.
	if got, want := strings.Fields(total), strings.Fields(paragraphOf(400)); len(got) != len(want) {
		t.Fatalf("%d words, want %d", len(got), len(want))
	} else {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("word %d = %q, want %q", i, got[i], want[i])
			}
		}
	}
	// The paragraph is fragmented: more on the first, cont on the others.
	ps := frags(pages, "p")
	if !ps[0].first || ps[0].last || ps[len(ps)-1].first || !ps[len(ps)-1].last {
		t.Errorf("fragment flags: %v %v / %v %v", ps[0].first, ps[0].last, ps[len(ps)-1].first, ps[len(ps)-1].last)
	}
	golden(t, "paragraph-break", pages[:3])
}

func TestBorderedBlockOverThreePages(t *testing.T) {
	html := `<div id=box>` + paragraphOf(300) + `</div>`
	css := "body { margin: 0 } #box { border: 3px solid #123456; padding: 5px; background: #eef; border-radius: 6px }"
	r := newRun(t, html, opts{extra: "@page { size: 300px 150px; margin: 0 }", css: css})
	pages := r.all(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	boxes := frags(pages, "div")
	if len(boxes) != len(pages) {
		t.Fatalf("%d fragments on %d pages", len(boxes), len(pages))
	}
	for i, f := range boxes {
		wantFirst, wantLast := i == 0, i == len(boxes)-1
		if f.first != wantFirst || f.last != wantLast {
			t.Errorf("fragment %d: first=%v last=%v", i, f.first, f.last)
		}
	}
	// Decorations: top border and radii on the first fragment, bottom on the last,
	// side borders and the background on every one.
	for i, pg := range pages {
		var fill, border int
		var b display.Border
		var rad display.Radii
		for _, it := range pg.Items {
			switch v := it.(type) {
			case display.FillRect:
				fill++
				rad = v.Radii
			case display.Border:
				border++
				b = v
			}
		}
		if fill != 1 || border != 1 {
			t.Fatalf("page %d: %d fills and %d borders", i+1, fill, border)
		}
		first, last := i == 0, i == len(pages)-1
		if (b.Sides[0].Width > 0) != first || (b.Sides[2].Width > 0) != last || b.Sides[1].Width != 3 || b.Sides[3].Width != 3 {
			t.Errorf("page %d borders: %+v", i+1, b.Sides)
		}
		if (rad[0].H > 0) != first || (rad[1].H > 0) != first || (rad[2].H > 0) != last || (rad[3].H > 0) != last {
			t.Errorf("page %d radii: %+v", i+1, rad)
		}
	}
	golden(t, "bordered-block", pages)
}

func TestDecorationsAreSlicedWhenTheyDoNotFit(t *testing.T) {
	// 1 000 px of padding on a 150 px page: spread over pages, bounded.
	r := newRun(t, `<div style="padding-top:1000px;padding-bottom:300px">x</div>`, opts{extra: "@page { size: 300px 150px; margin: 0 }", css: "body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) < 8 || len(pages) > 11 {
		t.Errorf("%d pages for 1300 px of padding", len(pages))
	}
	var total float64
	for _, f := range frags(pages, "div") {
		total += f.rect.H
	}
	if !near(total, 1000+18+300) {
		t.Errorf("fragment heights sum to %v, want %v", total, 1000+18+300)
	}
}

func TestHugeMarginMakesAtMostOneBlankPage(t *testing.T) {
	r := newRun(t, `<p style="margin-top:1e7px">x</p><p>y</p>`, opts{extra: "@page { size: 300px 150px; margin: 0 }"})
	pages := r.all(t)
	if len(pages) > 3 {
		t.Errorf("%d pages", len(pages))
	}
	total := 0
	for _, pg := range pages {
		total += len(linesOf(pg))
	}
	if total != 2 {
		t.Errorf("%d lines", total)
	}
}

func TestHostileHugePageIsClamped(t *testing.T) {
	var src []byte
	for _, c := range testcorpus.SpikeHostile() {
		if c.Name == "hostile_huge_page" {
			src = c.HTML()
		}
	}
	if src == nil {
		t.Fatal("no hostile_huge_page case")
	}
	r := newRun(t, string(src), opts{})
	pages := r.all(t)
	if len(pages) < 1 || len(pages) > 10 {
		t.Fatalf("%d pages", len(pages))
	}
	for _, pg := range pages {
		if pg.Width != style.MaxPageSidePx || pg.Height != style.MaxPageSidePx {
			t.Errorf("page %vx%v, want the 200 in clamp", pg.Width, pg.Height)
		}
		for _, it := range pg.Items {
			checkFinite(t, it)
		}
	}
	if w, _ := r.b.Warnings(); len(w) == 0 {
		t.Error("no warning for the clamps")
	}
}

func checkFinite(t *testing.T, it display.Item) {
	t.Helper()
	ok := func(vs ...float64) {
		for _, v := range vs {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
				t.Fatalf("non-finite or absurd coordinate %v in %+v", v, it)
			}
		}
	}
	switch v := it.(type) {
	case display.FillRect:
		ok(v.Rect.X, v.Rect.Y, v.Rect.W, v.Rect.H)
	case display.FillGradient:
		ok(v.Rect.X, v.Rect.Y, v.Rect.W, v.Rect.H)
	case display.Border:
		ok(v.Rect.X, v.Rect.Y, v.Rect.W, v.Rect.H)
	case *display.Text:
		ok(v.X, v.Baseline, v.Size)
	case display.Link:
		ok(v.Rect.X, v.Rect.Y, v.Rect.W, v.Rect.H)
	case display.PushClip:
		ok(v.Rect.X, v.Rect.Y, v.Rect.W, v.Rect.H)
	}
}

func manyParagraphs(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("<p>")
		sb.WriteString(paragraphOf(30 + i%7))
		sb.WriteString("</p>")
	}
	return sb.String()
}

func BenchmarkThousandParagraphs(b *testing.B) {
	r := newRun(b, manyParagraphs(1000), opts{extra: "@page { size: 800px 1000px; margin: 20px }"})
	cfg := r.pag.cfg
	cfg.KeepTree = false
	b.ReportAllocs()
	for b.Loop() {
		cfg.Budget = testBudget(nil)
		cfg.Shaper = text.NewShaper(testFonts(b), cfg.Budget)
		p := NewPaginator(r.root, cfg)
		for {
			if _, err := p.Next(); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				b.Fatal(err)
			}
		}
	}
}

var sinkBreaks []text.Break

// BenchmarkShapeOnly is the floor of BenchmarkThousandParagraphs: shaping and
// breaking the same texts without any layout.
func BenchmarkShapeOnly(b *testing.B) {
	texts := make([]string, 1000)
	for i := range texts {
		texts[i] = paragraphOf(30 + i%7)
	}
	spec := text.FontSpec{Families: []string{"Liberation Sans"}, Weight: 400, Size: 16}
	b.ReportAllocs()
	for b.Loop() {
		sh := text.NewShaper(testFonts(b), testBudget(nil))
		for _, s := range texts {
			if _, err := sh.Shape(s, spec, nil); err != nil {
				b.Fatal(err)
			}
			sinkBreaks = text.Breaks(s, sinkBreaks[:0])
		}
	}
}

func corpusRun(tb testing.TB, name string, size testcorpus.Size) *run {
	tb.Helper()
	data, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
	if err != nil {
		tb.Fatal(err)
	}
	return newRun(tb, string(data), opts{extra: testcorpus.HostConnectorPageCSS, scale: 0.9})
}

func TestCorpusReportsPaginate(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(name+"/"+string(size), func(t *testing.T) {
				pages := corpusRun(t, name, size).all(t)
				items := 0
				for _, pg := range pages {
					items += len(pg.Items)
					for _, it := range pg.Items {
						checkFinite(t, it)
					}
				}
				t.Logf("%d pages, %d display items", len(pages), items)
				if len(pages) < 1 || items == 0 {
					t.Fatal("empty rendering")
				}
			})
		}
	}
}

func BenchmarkLayout00Large(b *testing.B) {
	r := corpusRun(b, "00-host-connector", testcorpus.Large)
	cfg := r.pag.cfg
	cfg.KeepTree = false
	b.ReportAllocs()
	for b.Loop() {
		cfg.Budget = testBudget(nil)
		cfg.Shaper = text.NewShaper(testFonts(b), cfg.Budget)
		p := NewPaginator(r.root, cfg)
		for {
			if _, err := p.Next(); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				b.Fatal(err)
			}
		}
	}
}

func TestGlyphsAreReleasedBehindTheBreakToken(t *testing.T) {
	// One text box per span: the units before the cursor drop their shaping.
	var sb strings.Builder
	sb.WriteString("<p>")
	for i := range 600 {
		sb.WriteString("<span>word" + string(rune('a'+i%26)) + " </span>")
	}
	sb.WriteString("</p>")
	r := newRun(t, sb.String(), opts{extra: "@page { size: 300px 120px; margin: 10px }", css: "body { margin: 0 } p { margin: 0 }"})
	if _, err := r.pag.Next(); err != nil {
		t.Fatal(err)
	}
	p := r.pag.l.para
	if p == nil || p.released == 0 {
		t.Fatalf("paragraph %+v after the first page", p)
	}
	for i := range p.released {
		if p.units[i].st != nil {
			t.Fatalf("unit %d still holds its shaping", i)
		}
	}
	// The rest still lays out, and the paragraph is dropped at its end.
	for {
		if _, err := r.pag.Next(); err != nil {
			break
		}
	}
	if r.pag.l.para != nil {
		t.Error("the paragraph is kept after its end")
	}
	for i, sg := range r.pag.l.segs[:cap(r.pag.l.segs)] {
		if sg.un != nil {
			t.Fatalf("segment %d still points at a unit after the paragraph ended", i)
		}
	}
}

func TestRunsBehindTheCursorAreReleased(t *testing.T) {
	// The shaping of a text box is cut in runs by face: release works run by run.
	st := &shapedText{runs: make([]text.Run, 3), clusters: []int32{0, 10, 20}}
	for i := range st.runs {
		st.runs[i].Glyphs = make([]text.Glyph, 2)
	}
	st.releaseBefore(5)
	if st.released != 0 || st.runs[0].Glyphs == nil {
		t.Error("the run at the cursor must stay")
	}
	st.releaseBefore(10)
	if st.released != 1 || st.runs[0].Glyphs != nil || st.runs[1].Glyphs == nil {
		t.Errorf("released %d", st.released)
	}
	st.releaseBefore(1000)
	if st.released != 2 || st.runs[2].Glyphs == nil {
		t.Errorf("the last run is never released: %d", st.released)
	}
}

func TestGlyphsAreChargedExactly(t *testing.T) {
	for _, n := range []int{1, 2, 10, 1000} {
		html := "<p>" + strings.Repeat("A", n) + "</p>"
		limits := func(l *budget.Limits) { l[budget.Glyphs] = int64(n) }
		if _, err := newRun(t, html, opts{extra: smallPage, limits: limits}).pag.Next(); err != nil {
			t.Errorf("%d glyphs at a limit of %d: %v", n, n, err)
		}
		over := func(l *budget.Limits) { l[budget.Glyphs] = int64(n - 1) }
		_, err := newRun(t, html, opts{extra: smallPage, limits: over}).pag.Next()
		var le *budget.LimitError
		if !errors.As(err, &le) || le.Kind != budget.Glyphs {
			t.Errorf("%d glyphs at a limit of %d: %v", n, n-1, err)
		}
	}
}

func TestNothingFitsRestoresTheFlushState(t *testing.T) {
	// The paragraph's first line does not fit below the spacer: the margins of
	// the nested divs belong to the break, no fragment gets a negative height
	// and the paragraph starts the next page at its top.
	html := `<p style="margin:0;padding-top:70px">a</p>` +
		`<div style="margin-top:20px"><div style="margin-top:20px;border-top:0 solid"><p style="margin:0">next</p></div></div>`
	r := newRun(t, html, opts{extra: "@page { size: 300px 100px; margin: 0 }", css: "body { margin: 0 }"})
	pages := r.all(t)
	if len(pages) != 2 {
		t.Fatalf("%d pages, want 2", len(pages))
	}
	for i, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.rect.H < 0 || f.rect.Y < 0 {
				t.Errorf("page %d: %s has rect %+v", i+1, f.box.Tag, f.rect)
			}
		})
	}
	if y := linesOf(pages[1])[0].rect.Y; !near(y, 0) {
		t.Errorf("the paragraph starts the next page at y=%v, want 0", y)
	}
	walk(pages[0].root, func(f *frag) {
		if f.box.Tag.String() == "div" {
			t.Errorf("the div that did not fit left a fragment on page 1: %+v", f.rect)
		}
	})
}
