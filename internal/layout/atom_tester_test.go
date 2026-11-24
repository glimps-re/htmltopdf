package layout

import (
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
)

func finiteRect(r display.Rect) bool {
	for _, v := range []float64{r.X, r.Y, r.W, r.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func itemRects(it display.Item) []display.Rect {
	switch v := it.(type) {
	case display.FillRect:
		return []display.Rect{v.Rect}
	case *display.FillRect:
		return []display.Rect{v.Rect}
	case display.FillGradient:
		return []display.Rect{v.Rect}
	case display.Border:
		return []display.Rect{v.Rect}
	case display.Link:
		return []display.Rect{v.Rect}
	case display.PushClip:
		return []display.Rect{v.Rect}
	case *display.Text:
		return []display.Rect{{X: v.X, Y: v.Baseline, W: v.Width(), H: v.Size}}
	}
	return nil
}

// drain paginates r. A budget or context error ends it without failing; any
// other error fails. It returns the pages and the stop error.
func (r *run) drain(t testing.TB) ([]*Page, error) {
	t.Helper()
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			return pages, nil
		}
		if err != nil {
			var le *budget.LimitError
			if errors.As(err, &le) {
				return pages, err
			}
			t.Fatalf("unexpected error: %v", err)
		}
		pages = append(pages, pg)
		if len(pages) > 3000 {
			t.Fatal("runaway pagination")
		}
	}
}

func assertFinite(t testing.TB, pages []*Page) {
	t.Helper()
	for _, pg := range pages {
		for _, it := range pg.Items {
			for _, r := range itemRects(it) {
				if !finiteRect(r) {
					t.Fatalf("page %d: non finite geometry %+v in %T", pg.Number, r, it)
				}
			}
		}
	}
}

func TestM203bHostileGeometryIsFinite(t *testing.T) {
	ib := func(st, inner string) string {
		return `<p>a <span style="display:inline-block;` + st + `">` + inner + `</span> b</p>`
	}
	tests := map[string]string{
		"percent width":            `<div style="width:400px">` + ib("width:50%", "word word word") + `</div>`,
		"percent over 100":         `<div style="width:400px">` + ib("width:250%", "word word") + `</div>`,
		"percent in shrink-to-fit": `<div style="display:inline-block;width:50%"><span style="display:inline-block;width:50%">x</span></div>`,
		"percent of zero width":    `<div style="width:0">` + ib("width:50%", "word") + `</div>`,
		"negative margins":         ib("margin:-50px -80px", "word word"),
		"huge negative margin":     ib("margin-left:-1e9px", "word"),
		"huge positive margin":     ib("margin:0 1e9px", "word"),
		"huge width":               ib("width:1e9px", "word"),
		"huge padding":             ib("padding:1e9px", "word"),
		"huge max-width":           ib("max-width:1e12px", "word"),
		"zero max-width":           ib("max-width:0", "word word"),
		"width 0":                  ib("width:0", "word word"),
		"empty":                    ib("", ""),
		"only a br":                ib("", "<br>"),
		"table inside":             ib("", `<table><tr><td>a</td><td>b b b</td></tr></table>`),
		"table percent":            ib("", `<table style="width:100%"><tr><td style="width:30%">a</td><td>b</td></tr></table>`),
		"table fixed":              ib("", `<table style="table-layout:fixed;width:300px"><tr><td>a</td><td>b</td></tr></table>`),
		"table in table in atom":   ib("", `<table><tr><td><span style="display:inline-block"><table><tr><td>deep</td></tr></table></span></td></tr></table>`),
		"atom in table cell":       `<table><tr><td><span style="display:inline-block;width:50%">x</span></td><td><span style="display:inline-block">y y y</span></td></tr></table>`,
		"table with nowrap":        ib("", `<table><tr><td style="white-space:nowrap">`+strings.Repeat("long ", 200)+`</td></tr></table>`),
		"atom taller than page":    ib("", strings.Repeat("line<br>", 200)),
		"atom taller, in a table":  `<table><tr><td><span style="display:inline-block">` + strings.Repeat("line<br>", 200) + `</span></td></tr></table>`,
		"link in atom":             ib("", `<a href="https://example.test/">link link</a>`),
		"overflow hidden atom":     ib("overflow:hidden;width:20px;border-radius:50%", "word word word"),
		"atom with float margins":  ib("margin:1e-9px 1e-9px", "x"),
		"letter spacing":           ib("letter-spacing:1e6px", "word"),
		"nowrap in atom":           ib("white-space:nowrap", strings.Repeat("word ", 100)),
		"nowrap atom wide":         `<div style="white-space:nowrap">` + strings.Repeat(`<span style="display:inline-block;width:300px">x</span>`, 5) + `</div>`,
		"vertical-align variants": ib("vertical-align:top", "a<br>b") + ib("vertical-align:middle", "a<br>b") +
			ib("vertical-align:bottom", "a<br>b"),
		"atom inside flex":       `<div style="display:flex"><span style="display:inline-block;width:50%">a</span><span>b</span></div>`,
		"atom inside grid":       `<div style="display:grid;grid-template-columns:1fr 1fr"><span style="display:inline-block">a</span><span>b</span></div>`,
		"atom in atom in nowrap": `<p style="white-space:nowrap"><span style="display:inline-block"><span style="display:inline-block">a b</span> c d</span> e f</p>`,
	}
	for name, html := range tests {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			r := newRun(t, html, opts{extra: smallPage, css: flat})
			pages, _ := r.drain(t)
			assertFinite(t, pages)
			assertWorkBound(t, r, html, 130)
			if d := time.Since(start); d > time.Minute {
				t.Errorf("took %v", d)
			}
		})
	}
}

func TestM203bNestedAtomsDepth(t *testing.T) {
	for _, n := range []int{1000, 3000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			start := time.Now()
			b := testBudget(func(l *budget.Limits) { l[budget.Nodes] = 1 << 22 })
			html := "<p>" + nested(n) + "</p>"
			d, err := dom.Parse(strings.NewReader(html), b)
			if err != nil {
				return
			}
			r := style.NewResolver(uaCompiled(t), nil, nil, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
			if _, err := box.Build(d, r, b); err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatalf("build: %v", err)
				}
				return
			}
			rr := newRun(t, html, opts{extra: smallPage, css: flat})
			pages, _ := rr.drain(t)
			assertFinite(t, pages)
			if d := time.Since(start); d > time.Minute {
				t.Errorf("took %v", d)
			}
		})
	}
}

// peakHeap samples the live heap while f runs and returns its maximum.
func peakHeap(f func()) uint64 {
	runtime.GC()
	var stop atomic.Bool
	var peak atomic.Uint64
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for !stop.Load() {
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	f()
	stop.Store(true)
	<-done
	return peak.Load()
}

func TestM203bHundredThousandAtomsHeap(t *testing.T) {
	html := "<p>" + strings.Repeat(`<span style="display:inline-block">k</span> `, 100000) + "</p>"
	var gotErr error
	var elapsed time.Duration
	peak := peakHeap(func() {
		start := time.Now()
		b := budget.New(t.Context(), func() budget.Limits {
			l := budget.Defaults()
			l[budget.Nodes] = 1 << 22
			l[budget.InputBytes] = 1 << 26
			l[budget.CSSBytes] = 1 << 24
			return l
		}(), nil)
		d, err := dom.Parse(strings.NewReader(html), b)
		if err != nil {
			gotErr = err
			return
		}
		r := style.NewResolver(uaCompiled(t), nil, nil, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
		_, gotErr = box.Build(d, r, b)
		elapsed = time.Since(start)
	})
	var le *budget.LimitError
	if !errors.As(gotErr, &le) {
		t.Fatalf("100000 sibling atoms with the default limits: %v, want a LimitError", gotErr)
	}
	t.Logf("%v after %v, peak heap %d MiB", le.Kind, elapsed, peak>>20)
	if peak > peakHeapCap() {
		t.Errorf("peak heap %d MiB, limit 256", peak>>20)
	}
	if elapsed > time.Minute {
		t.Errorf("took %v", elapsed)
	}
}

func TestM203bNowrapHugeText(t *testing.T) {
	tests := map[string]string{
		"300k chars no space":    strings.Repeat("x", 300000),
		"60k words":              strings.Repeat("word ", 60000),
		"60k words with br":      strings.Repeat("word word<br>", 20000),
		"60k nbsp":               strings.Repeat("a ", 60000),
		"zero width and combin.": strings.Repeat("e"+string(rune(0x301))+string(rune(0x200b)), 50000),
		"cjk":                    strings.Repeat("漢字", 50000),
		"rtl":                    strings.Repeat("שלום ", 20000),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			r := newRun(t, `<div style="width:200px;white-space:nowrap">`+content+`</div>`, opts{extra: smallPage, css: flat})
			pages, _ := r.drain(t)
			assertFinite(t, pages)
			assertWorkBound(t, r, content, 130)
			if d := time.Since(start); d > time.Minute {
				t.Errorf("took %v", d)
			}
		})
	}
	t.Run("one line, overflow kept", func(t *testing.T) {
		r := newRun(t, `<div id="d" style="width:100px;white-space:nowrap">`+strings.Repeat("word ", 50)+`</div>`, opts{extra: smallPage, css: flat})
		pages, err := r.drain(t)
		if err != nil {
			t.Fatal(err)
		}
		lines, maxX := 0, 0.0
		for _, it := range pages[0].Items {
			if tx, ok := it.(*display.Text); ok {
				lines++
				maxX = math.Max(maxX, tx.X+tx.Width())
			}
		}
		if len(pages) != 1 {
			t.Errorf("%d pages", len(pages))
		}
		ys := map[float64]bool{}
		for _, it := range pages[0].Items {
			if tx, ok := it.(*display.Text); ok {
				ys[tx.Baseline] = true
			}
		}
		if len(ys) != 1 {
			t.Errorf("%d lines in a nowrap block without br", len(ys))
		}
		if maxX <= 100 {
			t.Errorf("text ends at %v: a nowrap line wider than its box must overflow", maxX)
		}
		for _, it := range pages[0].Items {
			if _, ok := it.(display.PushClip); ok {
				t.Error("nowrap overflow must not be clipped")
			}
		}
	})
	t.Run("each br makes a line", func(t *testing.T) {
		r := newRun(t, `<div style="width:100px;white-space:nowrap">aaaa bbbb<br>cccc dddd<br><br>eeee</div>`, opts{extra: smallPage, css: flat})
		pages, _ := r.drain(t)
		ys := map[float64]bool{}
		for _, it := range pages[0].Items {
			if tx, ok := it.(*display.Text); ok {
				ys[tx.Baseline] = true
			}
		}
		if len(ys) != 3 {
			t.Errorf("%d text lines, want 3 (the blank line draws nothing)", len(ys))
		}
	})
}

func TestM203bPercentAndNegativeMarginSemantics(t *testing.T) {
	r := newRun(t, `<div style="width:400px"><span id="a" style="display:inline-block;width:50%;background:#000">x</span></div>`, opts{extra: smallPage, css: flat})
	pages := r.all(t)
	a := atomsOf(pages)
	if len(a) != 1 {
		t.Fatalf("%d atoms", len(a))
	}
	var w float64
	for _, it := range pages[0].Items {
		if f, ok := it.(display.FillRect); ok {
			w = f.Rect.W
		}
	}
	if !near(w, 200) {
		t.Errorf("width:50%% of a 400px block gives %v, want 200", w)
	}

	r = newRun(t, `<div style="width:400px">aa<span style="display:inline-block;width:50px;margin-left:-20px;background:#000">x</span></div>`, opts{extra: smallPage, css: flat})
	pages = r.all(t)
	var x float64
	var tw float64
	for _, it := range pages[0].Items {
		switch v := it.(type) {
		case display.FillRect:
			x = v.Rect.X
		case *display.Text:
			if tw == 0 {
				tw = v.X + v.Width()
			}
		}
	}
	if !near(x, tw-20) {
		t.Errorf("a margin-left of -20px puts the atom at %v, want %v (text ends at %v)", x, tw-20, tw)
	}
}

var tagRE = regexp.MustCompile(`<[^>]*>`)

// nonSpace counts the runes that are not white space (including U+200B and NBSP,
// which layout may drop or keep).
func nonSpace(s string) map[rune]int {
	m := map[rune]int{}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', 0x200b, 0xa0:
			continue
		}
		m[r]++
	}
	return m
}

// assertTextKept fails when the text drawn on the pages is not the text of the
// source: no rune may be lost or invented, whatever the geometry.
func assertTextKept(t testing.TB, html string, pages []*Page) {
	t.Helper()
	var sb strings.Builder
	for _, pg := range pages {
		for _, it := range pg.Items {
			if tx, ok := it.(*display.Text); ok {
				sb.WriteString(tx.Text)
			}
		}
	}
	for _, pg := range pages {
		scale := pg.Scale
		if scale <= 0 {
			scale = 1
		}
		bottom := pg.Area.H/scale + 1
		for _, it := range pg.Items {
			if tx, ok := it.(*display.Text); ok && (tx.Baseline < 0 || tx.Baseline-tx.Size > bottom || tx.Baseline > bottom+tx.Size) {
				t.Errorf("page %d: the text %.20q is drawn at y = %.1f, outside the page area (height %.1f): invisible in the PDF", pg.Number, tx.Text, tx.Baseline, bottom)
				break
			}
		}
	}
	want, got := nonSpace(tagRE.ReplaceAllString(html, "")), nonSpace(sb.String())
	for r, n := range want {
		if got[r] != n {
			t.Errorf("rune %q: %d in the source, %d drawn (%d pages)", r, n, got[r], len(pages))
		}
	}
	for r, n := range got {
		if want[r] == 0 {
			t.Errorf("rune %q drawn %d times, absent from the source", r, n)
		}
	}
}

func TestM203bNoTextIsLost(t *testing.T) {
	tall := func(inner string) string {
		return `<p>a <span style="display:inline-block">` + inner + `</span> b</p>`
	}
	tests := map[string]string{
		"20000 x in a tall atom":        tall(strings.Repeat("x", 20000)),
		"20000 b leaves in a tall atom": tall(strings.Repeat("<b>x</b> ", 20000)),
		"20000 leaves, depth 3":         tall(`<span style="display:inline-block"><span style="display:inline-block">` + strings.Repeat("<b>x</b> ", 20000) + `</span></span>`),
		"20000 leaves, plain inline":    `<p>` + strings.Repeat("<b>x</b> ", 20000) + `</p>`,
		"20000 x words in a tall atom":  tall(strings.Repeat("xx ", 20000)),
		"200 lines in an atom":          tall(strings.Repeat("line<br>", 200)),
		"tall atom in a table cell":     `<table><tr><td><span style="display:inline-block">` + strings.Repeat("line<br>", 200) + `</span></td><td>z</td></tr></table>`,
		"tall atom in an atom":          tall(`<span style="display:inline-block">` + strings.Repeat("line<br>", 200) + `</span>`),
		"tall atom after text":          `<p>` + strings.Repeat("before ", 50) + `<span style="display:inline-block">` + strings.Repeat("line<br>", 200) + `</span> after</p>`,
		"two tall atoms":                tall(strings.Repeat("l1<br>", 150)) + tall(strings.Repeat("l2<br>", 150)),
		"tall atom with a table":        tall(`<table>` + strings.Repeat("<tr><td>cell</td></tr>", 150) + `</table>`),
		"tall atom with a link":         tall(`<a href="https://e.test/">` + strings.Repeat("link<br>", 200) + `</a>`),
		"tall atom, nowrap":             `<p style="white-space:nowrap">` + tall(strings.Repeat("nw ", 3000)) + `</p>`,
		"tall atom with avoid":          `<div style="break-inside:avoid">` + tall(strings.Repeat("line<br>", 200)) + `</div>`,
		"tall atom, overflow hidden":    tall(`<div style="overflow:hidden">` + strings.Repeat("line<br>", 200) + `</div>`),
		"60k words nowrap":              `<div style="width:200px;white-space:nowrap">` + strings.Repeat("word ", 20000) + `</div>`,
		"nested atoms":                  "<p>" + nested(300) + "</p>",
		"pills wrapping":                `<div style="width:120px">` + strings.Repeat(`<span style="display:inline-block;padding:2px">Trojan</span> `, 300) + `</div>`,
		"negative margins":              `<p>a <span style="display:inline-block;margin:-50px -80px">word word</span> b</p>`,
		"percent width":                 `<div style="width:400px"><span style="display:inline-block;width:50%">word word word</span></div>`,
		"table in atom":                 `<p>a <span style="display:inline-block"><table><tr><td>a</td><td>b b b</td></tr></table></span> b</p>`,
	}
	for name, html := range tests {
		t.Run(name, func(t *testing.T) {
			r := newRun(t, html, opts{extra: smallPage, css: flat})
			pages, err := r.drain(t)
			if err != nil {
				t.Skipf("stopped by a limit: %v", err)
			}
			assertTextKept(t, html, pages)
		})
	}
}

// peakHeapCap is the peak-heap bound of the 256 MiB criteria, in bytes. The
// sampled HeapAlloc includes garbage the collector has not swept yet, and the
// race detector multiplies the footprint, so the bound doubles under -race.
func peakHeapCap() uint64 {
	if raceEnabled {
		return 512 << 20
	}
	return 256 << 20
}

// assertWorkBound fails when the document charged more than perByte units of
// Work per byte of HTML (plus a fixed 20 000 for the page set-up). The factor is
// the largest measured one times at least four: it catches a super-linear cost
// that the one-minute guard of a slow machine would not.
func assertWorkBound(t testing.TB, r *run, html string, perByte int64) {
	t.Helper()
	w, limit := r.b.Used(budget.Work), perByte*int64(len(html))+20_000
	if w > limit {
		t.Errorf("%d work units for %d bytes of HTML, at most %d expected (%d per byte)", w, len(html), limit, perByte)
	}
	if per := float64(w) / float64(len(html)); per > float64(perByte)/4 {
		t.Logf("work per byte %.1f is above a quarter of the bound (%d)", per, perByte)
	}
}
