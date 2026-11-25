package layout

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
)

const flat = "body { margin: 0 } p, div { margin: 0 } span { font-size: 16px }"

func boxByID(t testing.TB, b *box.Box, id string) *box.Box {
	t.Helper()
	var find func(*box.Box) *box.Box
	find = func(b *box.Box) *box.Box {
		if b.ID == id {
			return b
		}
		for _, c := range b.Children {
			if f := find(c); f != nil {
				return f
			}
		}
		return nil
	}
	if f := find(b); f != nil {
		return f
	}
	t.Fatalf("no box #%s", id)
	return nil
}

// atoms returns the atomic inlines of the lines of the pages, in order.
func atomsOf(pages []*Page) []*atomBox {
	var out []*atomBox
	var visit func(f *frag)
	visit = func(f *frag) {
		for _, c := range f.children {
			visit(c)
		}
		for _, ln := range f.lines {
			for _, it := range ln.items {
				if it.atom != nil {
					out = append(out, it.atom)
					visit(it.atom.f)
				}
			}
		}
	}
	for _, pg := range pages {
		visit(pg.root)
	}
	return out
}

func lineOfAtom(pages []*Page, a *atomBox) *lineFrag {
	var found *lineFrag
	var visit func(f *frag)
	visit = func(f *frag) {
		for _, c := range f.children {
			visit(c)
		}
		for _, ln := range f.lines {
			for _, it := range ln.items {
				if it.atom == a {
					found = ln
				}
			}
		}
	}
	for _, pg := range pages {
		visit(pg.root)
	}
	return found
}

func TestPillsWrapWithoutSplitting(t *testing.T) {
	pill := `<span style="display:inline-block;padding:2px 6px;background:#eee;border-radius:9px;margin:0 4px 3px 0">%s</span> `
	var sb strings.Builder
	for _, w := range []string{"Trojan", "Emotet", "Ransomware", "Dropper", "Backdoor", "Worm"} {
		sb.WriteString(strings.Replace(pill, "%s", w, 1))
	}
	r := newRun(t, `<div id=c style="width:150px">`+sb.String()+`</div>`, opts{extra: smallPage, css: flat})
	pages := r.all(t)
	as := atomsOf(pages)
	if len(as) != 6 {
		t.Fatalf("%d atoms", len(as))
	}
	lines := map[*lineFrag]int{}
	for _, a := range as {
		if len(a.f.lines) != 1 {
			t.Errorf("a pill was split over %d lines", len(a.f.lines))
		}
		ln := lineOfAtom(pages, a)
		lines[ln]++
		if a.x < -0.01 || a.x+a.f.rect.W > 150.01 && lines[ln] > 1 {
			t.Errorf("pill at %v..%v leaves the 150px box", a.x, a.x+a.f.rect.W)
		}
		if h := a.f.rect.H; !near(h, a.f.lines[0].rect.H+4) {
			t.Errorf("pill height %v, line %v + 2 × 2px padding", h, a.f.lines[0].rect.H)
		}
		if ln.rect.H < a.f.rect.H+3-0.01 {
			t.Errorf("the margin-bottom is not in the line height: line %v, pill %v + 3", ln.rect.H, a.f.rect.H)
		}
		if !near(a.f.rect.Y, ln.baseline-a.base) {
			t.Errorf("pill top %v, baseline %v, base %v", a.f.rect.Y, ln.baseline, a.base)
		}
	}
	if len(lines) < 3 {
		t.Errorf("%d lines for six pills of 150px", len(lines))
	}
	var fills, radii int
	for _, it := range pages[0].Items {
		if f, ok := it.(display.FillRect); ok && f.Color.A > 0 && f.Rect.W < 150 {
			fills++
			if f.Radii[0].H > 0 {
				radii++
			}
		}
	}
	if fills != 6 || radii != 6 {
		t.Errorf("%d pill backgrounds, %d rounded", fills, radii)
	}
}

func TestAtomBaseline(t *testing.T) {
	t.Run("with a line", func(t *testing.T) {
		pages := newRun(t, `<p>ab<span style="display:inline-block;font-size:30px;padding:4px">X</span>cd</p>`, opts{extra: smallPage, css: flat}).all(t)
		as := atomsOf(pages)
		if len(as) != 1 {
			t.Fatal(len(as))
		}
		outer := frags(pages, "p")[0].lines[0]
		inner := as[0].f.lines[0]
		if !near(inner.baseline, outer.baseline) {
			t.Errorf("inner baseline %v, outer %v", inner.baseline, outer.baseline)
		}
		if outer.rect.H < 30 {
			t.Errorf("the line is %v high: it holds a 30px atom", outer.rect.H)
		}
	})
	t.Run("without a line", func(t *testing.T) {
		pages := newRun(t, `<p>ab<span style="display:inline-block;padding:10px 3px;margin-bottom:5px"></span>cd</p>`, opts{extra: smallPage, css: flat}).all(t)
		a := atomsOf(pages)[0]
		ln := frags(pages, "p")[0].lines[0]
		if !near(a.f.rect.Y+a.f.rect.H+5, ln.baseline) {
			t.Errorf("bottom margin edge %v, baseline %v", a.f.rect.Y+a.f.rect.H+5, ln.baseline)
		}
	})
	t.Run("clipping", func(t *testing.T) {
		pages := newRun(t, `<p>ab<span style="display:inline-block;overflow:hidden;padding:4px">X</span>cd</p>`, opts{extra: smallPage, css: flat}).all(t)
		a := atomsOf(pages)[0]
		ln := frags(pages, "p")[0].lines[0]
		if !near(a.f.rect.Y+a.f.rect.H, ln.baseline) {
			t.Errorf("a clipping atom sits on its bottom edge: %v, baseline %v", a.f.rect.Y+a.f.rect.H, ln.baseline)
		}
	})
}

func TestAtomVerticalAlign(t *testing.T) {
	for _, tt := range []struct {
		align string
		check func(a *atomBox, ln *lineFrag) float64
	}{
		{"top", func(a *atomBox, ln *lineFrag) float64 { return a.f.rect.Y - (ln.rect.Y + 2) }},
		{"bottom", func(a *atomBox, ln *lineFrag) float64 { return a.f.rect.Y + a.f.rect.H + 2 - (ln.rect.Y + ln.rect.H) }},
		{"middle", func(a *atomBox, ln *lineFrag) float64 {
			return a.f.rect.Y + a.f.rect.H/2 - (ln.baseline - 4)
		}},
		{"baseline", func(a *atomBox, ln *lineFrag) float64 { return a.f.rect.Y - (ln.baseline - a.base) }},
	} {
		t.Run(tt.align, func(t *testing.T) {
			html := `<p>a<span style="display:inline-block;font-size:30px;margin:2px;vertical-align:` + tt.align + `">X</span>b<span style="display:inline-block;font-size:30px;margin:2px">Y</span></p>`
			pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
			as := atomsOf(pages)
			ln := frags(pages, "p")[0].lines[0]
			if d := tt.check(as[0], ln); math.Abs(d) > 0.01 {
				t.Errorf("offset %v", d)
			}
		})
	}
}

func TestAtomsAreNotBrokenInNowrap(t *testing.T) {
	html := `<div style="width:60px;white-space:nowrap"><span style="display:inline-block">aaaa</span><span style="display:inline-block">bbbb</span> <span style="display:inline-block">cccc</span> dddd eeee</div>`
	pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
	d := frags(pages, "div")[0]
	if len(d.lines) != 1 {
		t.Fatalf("%d lines in a nowrap box", len(d.lines))
	}
	if textWidth(d.lines[0]) <= 0 || d.lines[0].rect.W != 60 {
		t.Errorf("line %+v", d.lines[0].rect)
	}
	var w float64
	for _, it := range d.lines[0].items {
		w += it.w
	}
	if w <= 60 {
		t.Errorf("the nowrap line is %v wide: it must overflow its 60px box", w)
	}
	normal := newRun(t, strings.Replace(html, ";white-space:nowrap", "", 1), opts{extra: smallPage, css: flat}).all(t)
	if n := len(frags(normal, "div")[0].lines); n < 3 {
		t.Errorf("without nowrap: %d lines", n)
	}
}

func TestNowrapKeepsForcedBreaks(t *testing.T) {
	pages := newRun(t, `<div style="width:30px;white-space:nowrap">aaa bbb<br>ccc ddd</div>`, opts{extra: smallPage, css: flat}).all(t)
	if n := len(frags(pages, "div")[0].lines); n != 2 {
		t.Errorf("%d lines", n)
	}
}

func TestNowrapInATableCell(t *testing.T) {
	pages := newRun(t, `<table style="width:100%"><tr><td id=a style="white-space:nowrap;width:40px">alpha beta gamma delta</td><td id=b>x y z</td></tr></table>`,
		opts{extra: smallPage, css: flat}).all(t)
	if n := len(fragByID(t, pages, "a").lines); n != 1 {
		t.Errorf("nowrap cell has %d lines", n)
	}
}

func contributions(t *testing.T, html, id string) sizes {
	t.Helper()
	r := newRun(t, html, opts{extra: smallPage, css: flat})
	s, err := r.pag.l.intrinsic(boxByID(t, r.root, id))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func widthOf(t *testing.T, text, st string) float64 {
	t.Helper()
	pages := newRun(t, `<p style="`+st+`">`+text+`</p>`, opts{extra: smallPage, css: flat}).all(t)
	return textWidth(frags(pages, "p")[0].lines[0])
}

func TestIntrinsicSizes(t *testing.T) {
	word, whole := widthOf(t, "bbbbbb", ""), widthOf(t, "aaa bbbbbb cc", "")
	t.Run("text", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block">aaa bbbbbb cc</div>`, "a")
		if !near(s.min, word) || !near(s.max, whole) {
			t.Errorf("%+v, want %v %v", s, word, whole)
		}
	})
	t.Run("box edges and margins", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block;padding:0 3px;border:2px solid;margin:0 7px">aaa bbbbbb cc</div>`, "a")
		if !near(s.min, word+10+14) || !near(s.max, whole+10+14) {
			t.Errorf("%+v", s)
		}
	})
	t.Run("specified width and max-width", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block;width:50px;padding:0 3px">aaa bbbbbb cc</div>`, "a")
		if !near(s.min, 56) || !near(s.max, 56) {
			t.Errorf("%+v", s)
		}
		s = contributions(t, `<div id=a style="display:inline-block;max-width:40px">aaa bbbbbb cc</div>`, "a")
		if !near(s.max, 40) || s.min > 40 {
			t.Errorf("%+v", s)
		}
		s = contributions(t, `<div id=a style="display:inline-block;width:50%">aaa</div>`, "a")
		if s.max != widthOf(t, "aaa", "") {
			t.Errorf("a percentage width counts as auto: %+v", s)
		}
	})
	t.Run("overflow-wrap and word-break", func(t *testing.T) {
		cluster := widthOf(t, "W", "")
		anywhere := contributions(t, `<div id=a style="display:inline-block;overflow-wrap:anywhere">iiiW iiii</div>`, "a")
		if !near(anywhere.min, cluster) {
			t.Errorf("anywhere: min %v, widest cluster %v", anywhere.min, cluster)
		}
		breakAll := contributions(t, `<div id=a style="display:inline-block;word-break:break-all">abcdef</div>`, "a")
		if breakAll.min >= widthOf(t, "abcdef", "")/2 {
			t.Errorf("break-all: min %v", breakAll.min)
		}
		bw := contributions(t, `<div id=a style="display:inline-block;overflow-wrap:break-word">abcdef</div>`, "a")
		if !near(bw.min, widthOf(t, "abcdef", "")) {
			t.Errorf("break-word does not lower min-content: %v", bw.min)
		}
	})
	t.Run("letter-spacing", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block;letter-spacing:2px">abc de</div>`, "a")
		if !near(s.max, widthOf(t, "abc de", "letter-spacing:2px")) || !near(s.min, widthOf(t, "abc", "letter-spacing:2px")) {
			t.Errorf("%+v", s)
		}
	})
	t.Run("nested inline-blocks", func(t *testing.T) {
		s := contributions(t, `<div id=o style="display:inline-block"><span id=i style="display:inline-block;padding:0 5px">xx</span> yy</div>`, "o")
		want := widthOf(t, "xx yy", "") + 10
		if !near(s.max, want) {
			t.Errorf("max %v, want %v", s.max, want)
		}
		if !near(s.min, math.Max(widthOf(t, "xx", "")+10, widthOf(t, "yy", ""))) {
			t.Errorf("min %v", s.min)
		}
	})
	t.Run("inline box edges", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block">aa<span style="padding:0 4px">bb</span>cc</div>`, "a")
		if !near(s.max, widthOf(t, "aabbcc", "")+8) || !near(s.min, s.max) {
			t.Errorf("%+v", s)
		}
	})
	t.Run("nowrap", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block;white-space:nowrap">aaa bbbbbb cc</div>`, "a")
		if !near(s.min, whole) || !near(s.max, whole) {
			t.Errorf("%+v", s)
		}
	})
	t.Run("blocks and br", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block"><div>aaa bbbbbb</div><div>cc</div></div>`, "a")
		if !near(s.max, widthOf(t, "aaa bbbbbb", "")) || !near(s.min, word) {
			t.Errorf("%+v", s)
		}
		s = contributions(t, `<div id=a style="display:inline-block">aaa<br>bbbbbb cc</div>`, "a")
		if !near(s.max, widthOf(t, "bbbbbb cc", "")) {
			t.Errorf("br: %+v", s)
		}
	})
	t.Run("table", func(t *testing.T) {
		s := contributions(t, `<div id=a style="display:inline-block"><table id=t><tr><td>aaa</td></tr></table></div>`, "t")
		// One word: its width, the padding of the cell and the spacing of the table.
		if want := widthOf(t, "aaa", "") + 2*1 + 2*2; !near(s.min, want) || !near(s.max, want) {
			t.Errorf("a table without width: %+v, want %v", s, want)
		}
		s = contributions(t, `<div id=a style="display:inline-block"><table id=t style="width:100px"><tr><td>aaa</td></tr></table></div>`, "t")
		if !near(s.min, 100) || !near(s.max, 100) {
			t.Errorf("a table with a width: %+v", s)
		}
	})
}

func TestShrinkToFit(t *testing.T) {
	html := `<div style="width:100px"><span id=s style="display:inline-block">aaa</span> <span id=l style="display:inline-block">aaa bbb ccc ddd eee fff ggg hhh</span> <span id=w style="display:inline-block;margin:0 10px">wwwwwwwwwwwwwwwwwwwwwwwww</span></div>`
	pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
	byID := map[string]*atomBox{}
	for _, a := range atomsOf(pages) {
		if len(a.f.lines) > 0 {
			byID[a.f.box.ID] = a
		}
	}
	if s := byID["s"]; !near(s.f.rect.W, widthOf(t, "aaa", "")) {
		t.Errorf("short: %v", s.f.rect.W)
	}
	if l := byID["l"]; !near(l.f.rect.W, 100) || len(l.f.lines) < 2 {
		t.Errorf("long: %v wide, %d lines", l.f.rect.W, len(l.f.lines))
	}
	if w := byID["w"]; !near(w.f.rect.W, widthOf(t, "wwwwwwwwwwwwwwwwwwwwwwwww", "")) || w.ml != 10 {
		t.Errorf("a word wider than the line is its min-content: %v", w.f.rect.W)
	}
}

func TestNestedAtomsAndLinks(t *testing.T) {
	html := `<p><a href="https://a.test/x"><span style="display:inline-block;padding:2px"><b>one</b> <span style="display:inline-block">two</span></span></a></p>`
	pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
	links := 0
	var text []string
	for _, it := range pages[0].Items {
		switch v := it.(type) {
		case display.Link:
			links++
		case *display.Text:
			text = append(text, v.Text)
		}
	}
	if links < 2 || strings.Join(text, "") != "one two" {
		t.Errorf("%d links, text %q", links, text)
	}
}

func TestAtomsMoveWithTheirRow(t *testing.T) {
	html := `<table style="width:100%"><tr><td style="vertical-align:bottom">a<br>b<br>c</td><td style="vertical-align:bottom"><span style="display:inline-block">pill</span></td></tr></table>`
	pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
	a := atomsOf(pages)[0]
	ln := lineOfAtom(pages, a)
	if !near(a.f.lines[0].baseline, ln.baseline) || !near(a.f.rect.Y, ln.baseline-a.base) {
		t.Errorf("the atom was not moved with its cell: top %v, line baseline %v, inner baseline %v", a.f.rect.Y, ln.baseline, a.f.lines[0].baseline)
	}
	for _, it := range pages[0].Items {
		if tx, ok := it.(*display.Text); ok && tx.Text == "pill" && !near(tx.Baseline, ln.baseline) {
			t.Errorf("pill text at %v, line %v", tx.Baseline, ln.baseline)
		}
	}
}

func nested(n int) string {
	return strings.Repeat(`<span style="display:inline-block">word `, n) + "x" + strings.Repeat("</span>", n)
}

func TestNestedAtomsAreLinear(t *testing.T) {
	work := func(n int) (int64, int64) {
		r := newRun(t, "<p>"+nested(n)+"</p>", opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Boxes] = 1 << 20 }})
		r.all(t)
		return r.b.Used(budget.Work), r.b.Used(budget.Glyphs)
	}
	w250, _ := work(250)
	w500, g500 := work(500)
	if float64(w500) > 2.2*float64(w250) {
		t.Errorf("work %d for 500 levels against %d for 250", w500, w250)
	}
	if limit := int64(2 * (5*500 + 1)); g500 > limit {
		t.Errorf("%d glyphs shaped, text has %d", g500, limit/2)
	}
}

func BenchmarkPills(b *testing.B) {
	var sb strings.Builder
	for range 200 {
		sb.WriteString(`<span style="display:inline-block;padding:2px 6px;background:#eee;border-radius:9px;margin:0 4px 3px 0">Trojan</span> `)
	}
	html := `<div style="width:300px">` + sb.String() + `</div>`
	b.ReportAllocs()
	for b.Loop() {
		newRun(b, html, opts{extra: smallPage, css: flat}).all(b)
	}
}

func TestSiblingAtomsHitTheBoxLimit(t *testing.T) {
	html := "<p>" + strings.Repeat(`<span style="display:inline-block">k</span> `, 100000) + "</p>"
	start := time.Now()
	limits := func(l *budget.Limits) { l[budget.Boxes] = 50000 }
	b := testBudget(limits)
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		t.Fatal(err)
	}
	r := style.NewResolver(uaCompiled(t), nil, nil, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
	_, err = box.Build(d, r, b)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Boxes {
		t.Fatalf("got %v, want the Boxes limit", err)
	}
	if time.Since(start) > time.Minute {
		t.Errorf("took %v", time.Since(start))
	}
	twin := "<p>" + strings.Repeat(`<span style="display:inline-block">k</span> `, 20000) + "</p>"
	rr := newRun(t, twin, opts{extra: smallPage, css: flat})
	rr.all(t)
	if w := rr.b.Used(budget.Work); w > 4_700_000 {
		t.Errorf("the 20000-atom twin charged %d work units (116 per atom measured: linear, 2x margin)", w)
	}
}

func textOf(pages []*Page) string {
	var sb strings.Builder
	for _, pg := range pages {
		for _, it := range pg.Items {
			if t, ok := it.(*display.Text); ok {
				sb.WriteString(t.Text)
			}
		}
	}
	return sb.String()
}

func TestAtomTallerThanAPageIsSliced(t *testing.T) {
	var words, lines strings.Builder
	var wantWords, wantLines []string
	for i := range 3000 {
		fmt.Fprintf(&words, "w%04d ", i)
		wantWords = append(wantWords, fmt.Sprintf("w%04d", i))
	}
	for i := range 400 {
		fmt.Fprintf(&lines, "<div>l%03d</div>", i)
		wantLines = append(wantLines, fmt.Sprintf("l%03d", i))
	}
	for name, tt := range map[string]struct {
		html string
		want []string
	}{
		"wrapped text": {`<p>before <span style="display:inline-block">` + words.String() + `</span> after</p>`, wantWords},
		"block lines":  {`<p>before <span style="display:inline-block">` + lines.String() + `</span> after</p>`, wantLines},
		"nowrap":       {`<p style="white-space:nowrap">before <span style="display:inline-block">` + lines.String() + `</span> after</p>`, wantLines},
		"nested": {`<p>before <span style="display:inline-block"><span style="display:inline-block;border:1px solid;background:#eee">` +
			lines.String() + `</span></span> after</p>`, wantLines},
		"top aligned": {`<p>before <span style="display:inline-block;vertical-align:top">` + lines.String() + `</span> after</p>`, wantLines},
	} {
		t.Run(name, func(t *testing.T) {
			pages := newRun(t, tt.html, opts{extra: smallPage, css: flat}).all(t)
			if len(pages) < 3 {
				t.Fatalf("%d pages: the atom is taller than 2 pages", len(pages))
			}
			got := textOf(pages)
			for _, w := range append([]string{"before", "after"}, tt.want...) {
				if !strings.Contains(got, w) {
					t.Fatalf("%q lost on %d pages", w, len(pages))
				}
			}
			for _, pg := range pages {
				for _, it := range pg.Items {
					if tx, ok := it.(*display.Text); ok && tx.Baseline > pg.Area.H/pg.Scale+1 {
						t.Fatalf("text %q below the page area: baseline %v", tx.Text, tx.Baseline)
					}
				}
			}
		})
	}
}

func TestAtomSlicingIsBoundedByMaxPages(t *testing.T) {
	html := `<p><span style="display:inline-block">` + strings.Repeat("<div>x</div>", 3000) + `</span></p>`
	r := newRun(t, html, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Pages] = 5 }})
	var err error
	for range 50 {
		if _, err = r.pag.Next(); err != nil {
			break
		}
	}
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Pages {
		t.Errorf("got %v, want the Pages limit", err)
	}
}

func TestForcedBreaksInsideAnAtomKeepItWhole(t *testing.T) {
	inner := `<div style="break-after:page">AAA</div><div>BBB</div><div style="break-before:page">CCC</div><div>DDD</div>`
	for name, html := range map[string]string{
		"atom":   `<section>a<span style="display:inline-block">` + inner + `</span>z</section>`,
		"nested": `<section>a<span style="display:inline-block"><span style="display:inline-block">` + inner + `</span></span>z</section>`,
		"cell":   `<table><tr><td>a<span style="display:inline-block">` + inner + `</span>z</td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
			if len(pages) != 1 {
				t.Fatalf("%d pages: a forced break inside an atom must not break it", len(pages))
			}
			got := textOf(pages)
			for _, w := range []string{"AAA", "BBB", "CCC", "DDD", "a", "z"} {
				if !strings.Contains(got, w) {
					t.Errorf("%q lost", w)
				}
			}
		})
	}
	r := newRun(t, `<section>a<span style="display:inline-block">`+inner+`</span>z</section><p id=n>next</p>`, opts{extra: smallPage, css: flat})
	r.all(t)
	if r.pag.l.forced || r.pag.l.unbounded {
		t.Errorf("layMono leaks state: forced %v, unbounded %v", r.pag.l.forced, r.pag.l.unbounded)
	}
}

func TestAtomPrimaryCacheIsBounded(t *testing.T) {
	html := "<p>" + strings.Repeat(`<span style="display:inline-block">k</span> `, 3000) + "</p>"
	r := newRun(t, html, opts{extra: smallPage, css: flat})
	r.all(t)
	if n := len(r.pag.l.primaries); n > 8 {
		t.Errorf("%d primary faces cached for 3000 atoms of one style", n)
	}
}

func TestAtomPercentPaddingRefersToTheContainingBlock(t *testing.T) {
	pages := newRun(t, `<div style="width:800px"><span style="display:inline-block;padding:0 10%">x</span></div>`, opts{extra: smallPage, css: flat}).all(t)
	a := atomsOf(pages)[0]
	if want := widthOf(t, "x", "") + 160; !near(a.f.rect.W, want) {
		t.Errorf("atom is %v wide, want %v: 10%% of the 800px block on each side", a.f.rect.W, want)
	}
	if !near(a.f.padding[style.Left], 80) || !near(a.f.padding[style.Right], 80) {
		t.Errorf("padding laid out as %v", a.f.padding)
	}
}

// Laid out as wide as its min-content, a block has no line wider than it; as
// wide as its max-content, it has one line.
func TestIntrinsicSizesMatchLineBreaking(t *testing.T) {
	for _, tt := range []struct{ name, css, html string }{
		{"words", "", "aaa bbbbbb cc dddd eeeeeeeee f"},
		{"spaces", "", "  aaa    bbb  cc  "},
		{"letter-spacing", "letter-spacing:2px", "aaa bbbbbb cc"},
		{"break-all", "word-break:break-all", "abcdefgh ij"},
		{"anywhere", "overflow-wrap:anywhere", "abcdefgh ij"},
		{"edges", "", `aa<span style="padding:0 6px;border:2px solid">bbb cc</span>dd`},
		{"atoms", "", `aa <span style="display:inline-block;padding:0 4px">bbbb ccc</span> dd <span style="display:inline-block">e</span>`},
		{"br", "", "aaa bb<br>cccccc dd"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			html := `<div id=d style="` + tt.css + `">` + tt.html + `</div>`
			s := contributions(t, html, "d")
			lay := func(w float64) *frag {
				doc := `<div id=d style="width:` + fmt.Sprint(w) + `px;` + tt.css + `">` + tt.html + `</div>`
				return fragByID(t, newRun(t, doc, opts{extra: smallPage, css: flat}).all(t), "d")
			}
			lineW := func(ln *lineFrag) float64 {
				var w float64
				for _, it := range ln.items {
					w += it.w
				}
				return w
			}
			for _, ln := range lay(math.Ceil(s.min*100) / 100).lines {
				if lineW(ln) > s.min+0.05 {
					t.Errorf("at min-content %v a line is %v wide", s.min, lineW(ln))
				}
			}
			if n := len(lay(math.Ceil(s.max*100) / 100).lines); n > 1 && tt.name != "br" {
				t.Errorf("at max-content %v: %d lines", s.max, n)
			}
			if tt.name == "br" {
				if n := len(lay(math.Ceil(s.max*100) / 100).lines); n != 2 {
					t.Errorf("at max-content %v: %d lines, want 2", s.max, n)
				}
			}
		})
	}
}

func TestLayMono(t *testing.T) {
	r := newRun(t, `<div id=d style="margin:7px 9px;padding:3px;border:2px solid;width:10px;max-width:5px">aaa bbb ccc ddd</div>`, opts{extra: smallPage, css: flat})
	l := r.pag.l
	l.y, l.pend = 123, margins{pos: 4}
	f, err := l.layMono(boxByID(t, r.root, "d"), 150, 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.rect.X != 0 || f.rect.Y != 0 || !near(f.rect.W, 150) || f.box.ID != "d" {
		t.Errorf("fragment %+v: x = y = 0, border box 150 wide whatever the specified width and margins", f.rect)
	}
	if f.rect.H < 10+2*(3+2) {
		t.Errorf("height %v", f.rect.H)
	}
	if l.y != 123 || l.pend != (margins{pos: 4}) || l.root != r.root || l.unbounded {
		t.Errorf("layMono leaks state: y %v pend %+v unbounded %v", l.y, l.pend, l.unbounded)
	}
}
