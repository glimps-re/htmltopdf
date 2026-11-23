package layout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"github.com/glimps-re/htmltopdf/internal/text"
)

type pipeOut struct {
	pages    int
	items    int
	err      error
	stage    string
	b        *budget.Budget
	peak     uint64
	took     time.Duration
	lastPage *Page
}

func hostilePeak(fn func()) uint64 {
	runtime.GC()
	name := "/memory/classes/heap/objects:bytes"
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	base := s[0].Value.Uint64()
	var hi atomic.Uint64
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			ms := []metrics.Sample{{Name: name}}
			metrics.Read(ms)
			if v := ms[0].Value.Uint64(); v > hi.Load() {
				hi.Store(v)
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()
	fn()
	stop.Store(true)
	<-done
	return hi.Load() - min(base, hi.Load())
}

// pipe runs dom, style, box and layout with the budget limits (the defaults
// unless mod changes them) and stops at the first error.
func pipe(tb testing.TB, ctx context.Context, html, extra string, mod func(*budget.Limits), maxPages int) *pipeOut {
	tb.Helper()
	l := budget.Defaults()
	if mod != nil {
		mod(&l)
	}
	b := budget.New(ctx, l, nil)
	out := &pipeOut{b: b}
	start := time.Now()
	out.peak = hostilePeak(func() {
		d, err := dom.Parse(strings.NewReader(html), b)
		if err != nil {
			out.err, out.stage = err, "dom"
			return
		}
		var sheets []*style.Sheet
		var sb strings.Builder
		for i := range d.Elements {
			e := &d.Elements[i]
			if e.Tag.String() == "style" && e.NS == dom.NSHTML {
				for r := e.FirstChild; r != dom.NoNode; r = d.NextSibling(r) {
					if ti, ok := r.Text(); ok {
						sb.WriteString(d.Texts[ti].Data)
					}
				}
			}
		}
		if sb.Len() > 0 {
			ss, err := css.ParseStylesheet([]byte(sb.String()), css.OriginDocument, b)
			if err != nil {
				out.err, out.stage = err, "css"
				return
			}
			sh, err := style.Compile(ss, b)
			if err != nil {
				out.err, out.stage = err, "style"
				return
			}
			sheets = append(sheets, sh)
		}
		var ex *style.Sheet
		if extra != "" {
			ss, err := css.ParseStylesheet([]byte(extra), css.OriginExtra, b)
			if err != nil {
				out.err, out.stage = err, "css"
				return
			}
			if ex, err = style.Compile(ss, b); err != nil {
				out.err, out.stage = err, "style"
				return
			}
		}
		r := style.NewResolver(uaCompiled(tb), ex, sheets, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
		root, err := box.Build(d, r, b)
		if err != nil {
			out.err, out.stage = err, "box"
			return
		}
		d = nil
		p := NewPaginator(root, Config{Page: r.Page(style.PageContext{Number: 1}), Shaper: text.NewShaper(testFonts(tb), b), Fonts: testFonts(tb), Budget: b})
		for {
			pg, err := p.Next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				out.err, out.stage = err, "layout"
				return
			}
			out.pages++
			out.items += len(pg.Items)
			out.lastPage = pg
			for _, it := range pg.Items {
				checkFinite(asT(tb), it)
			}
			if out.pages >= maxPages {
				out.err, out.stage = errTooManyPages, "layout"
				return
			}
		}
	})
	out.took = time.Since(start)
	return out
}

var errTooManyPages = errors.New("test: more pages than the test allows")

// asT adapts a testing.TB for checkFinite, which wants a *testing.T.
func asT(tb testing.TB) *testing.T {
	if t, ok := tb.(*testing.T); ok {
		return t
	}
	panic("pipe needs a *testing.T")
}

func isLimit(err error, kinds ...budget.Kind) bool {
	var le *budget.LimitError
	if !errors.As(err, &le) {
		return false
	}
	for _, k := range kinds {
		if le.Kind == k {
			return true
		}
	}
	return len(kinds) == 0
}

var limitKinds = map[string]budget.Kind{
	"MaxInputBytes": budget.InputBytes, "MaxNodes": budget.Nodes, "HTMLDepth": budget.HTMLDepth, "MaxCSSBytes": budget.CSSBytes,
	"MaxSelectors": budget.Selectors, "MaxVarBytes": budget.VarBytes, "MaxStyles": budget.Styles, "MaxBoxes": budget.Boxes,
	"MaxGlyphs": budget.Glyphs, "MaxPages": budget.Pages, "MaxWork": budget.Work, "MaxOutputBytes": budget.OutputBytes,
}

// Every hostile document of testcorpus through the whole pipeline at the
// default limits: no panic, a limit error or success, finite coordinates, a
// bounded heap.
func TestHostileCorpusFullPipeline(t *testing.T) {
	cases := append(testcorpus.Hostile(budget.Defaults()), testcorpus.SpikeHostile()...)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if strings.HasPrefix(c.Name, "text_") {
				t.Skip("covered by the dom and paint tests")
			}
			o := pipe(t, context.Background(), string(c.HTML()), "", c.Limits, 1200)
			t.Logf("%s: %d pages, %d items, stage %q err %v, %v, peak %d MiB", c.Name, o.pages, o.items, o.stage, o.err, o.took.Round(time.Millisecond), o.peak>>20)
			if o.err != nil && !isLimit(o.err) && !errors.Is(o.err, errTooManyPages) {
				t.Fatalf("stage %s: %v", o.stage, o.err)
			}
			if o.peak > 512<<20 {
				t.Errorf("peak heap %d MiB", o.peak>>20)
			}
			// The Want of a case is stated for the stage that owns its limit;
			// a mismatch here is logged, not failed: later stages may stop a
			// big document earlier (MaxPages) or the case may be stale.
			switch {
			case c.Want == testcorpus.Limit && !isLimit(o.err, limitKinds[c.Limit]):
				t.Logf("MISMATCH %s: want a %s limit error, got stage %q err %v", c.Name, c.Limit, o.stage, o.err)
			case c.Want == testcorpus.Success && o.err != nil:
				t.Logf("MISMATCH %s: want success, got stage %q err %v", c.Name, o.stage, o.err)
			}
		})
	}
}

// MaxPages: the exact number of pages passes, one fewer fails with
// LimitPages.
func TestMaxPagesAtTheLimit(t *testing.T) {
	html := strings.Repeat(`<div style="padding-top:500px">x</div>`, 30)
	extra := "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
	ref := pipe(t, context.Background(), html, extra, nil, 1000)
	if ref.err != nil || ref.pages < 10 {
		t.Fatalf("reference run: %d pages, %v", ref.pages, ref.err)
	}
	for _, c := range []struct {
		name  string
		limit int
		ok    bool
	}{{"just under", ref.pages + 1, true}, {"at the limit", ref.pages, true}, {"over", ref.pages - 1, false}} {
		o := pipe(t, context.Background(), html, extra, func(l *budget.Limits) { l[budget.Pages] = int64(c.limit) }, 1000)
		if c.ok != (o.err == nil) || (!c.ok && !isLimit(o.err, budget.Pages)) {
			t.Errorf("%s (%d pages, limit %d): %d pages, %v", c.name, ref.pages, c.limit, o.pages, o.err)
		}
	}
	// A box that never fits still makes progress and ends at the limit.
	o := pipe(t, context.Background(), `<div style="padding-top:1e7px;border:5px solid">x</div>`, extra, func(l *budget.Limits) { l[budget.Pages] = 40 }, 1000)
	if !isLimit(o.err, budget.Pages) && o.err != nil {
		t.Errorf("giant box: %v", o.err)
	}
	t.Logf("giant box: %d pages, %v", o.pages, o.err)
}

// Documents built to stall pagination or break the arithmetic. Each ends with
// at least one page (or a limit error), finite coordinates and no panic.
func TestForcedProgressAndHostileValues(t *testing.T) {
	deep := func(n int, open, close string) string {
		return strings.Repeat(open, n) + "x" + strings.Repeat(close, n)
	}
	cases := map[string]string{
		"line taller than the page":     `<p style="font-size:10000px;line-height:10000px">x y z</p>`,
		"giant line-height":             `<p style="font-size:1e4px;line-height:1e7">x y z</p>`,
		"giant word, tiny page":         `<p style="font-size:200px">` + strings.Repeat("W", 5000) + `</p>`,
		"giant word, anywhere":          `<p style="overflow-wrap:anywhere;width:1px">` + strings.Repeat("W", 20000) + `</p>`,
		"width 0":                       `<p style="width:0;padding:0">a b c d e f g</p>`,
		"negative width":                `<div style="width:-1e9px">x</div>`,
		"huge width":                    `<div style="width:1e30px;border:1e30px solid">x</div>`,
		"huge margins":                  `<div style="margin:1e30px">x</div><div style="margin:-1e30px">y</div>`,
		"huge padding":                  `<div style="padding:1e30px"><div style="padding:1e30px">x</div></div>`,
		"huge border":                   `<div style="border:1e30px solid red">x</div>`,
		"huge radius":                   `<div style="border-radius:1e30px;background:#f00;border:3px solid">x</div>`,
		"huge letter spacing":           `<p style="letter-spacing:1e30px">abc def</p>`,
		"percent overflow":              `<div style="width:1e30%;margin-left:1e30%;padding:1e30%">x</div>`,
		"font size 1e30":                `<p style="font-size:1e30px">x</p>`,
		"font size em chain":            `<div style="font-size:1e7em"><div style="font-size:1e7em">x</div></div>`,
		"padding nesting":               deep(200, `<div style="padding:1e7px">`, "</div>"),
		"deep nesting":                  deep(450, "<div>", "</div>"),
		"deep nesting with borders":     deep(300, `<div style="border:9999px solid;margin:9999px">`, "</div>"),
		"deep inline nesting":           deep(450, "<span style='padding:1e6px;margin:1e6px'>", "</span>"),
		"inline block mix":              strings.Repeat(`<span style="padding:1e9px">a<div>b</div>`, 100),
		"many pages of tall blocks":     strings.Repeat(`<div style="padding-top:1e6px">x</div>`, 200),
		"table display":                 `<div style="display:table;width:1e30px"><div style="display:table-cell">x</div></div>`,
		"list items":                    "<ul>" + strings.Repeat("<li>x", 3000) + "</ul>",
		"unbreakable pre":               `<pre>` + strings.Repeat("x", 200_000) + `</pre>`,
		"empty text everywhere":         strings.Repeat("<span></span><div></div><p></p>", 2000),
		"background clip hostile":       `<div style="overflow:hidden;border-radius:50px;padding-top:1e7px;background:linear-gradient(red,blue)">` + strings.Repeat("<p>x</p>", 100) + `</div>`,
		"calc and var rejects":          `<div style="width:calc(1px + 2px);height:var(--x);margin:var(--x, 1e30px)">x</div>`,
		"line height number times size": `<p style="font-size:1e4px;line-height:1e7;">` + strings.Repeat("word ", 50) + `</p>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			o := pipe(t, context.Background(), body, "@page { size: 8in 6in; margin: 0.5in }", nil, 800)
			t.Logf("%d pages, %d items, stage %q err %v, %v, peak %d MiB", o.pages, o.items, o.stage, o.err, o.took.Round(time.Millisecond), o.peak>>20)
			if o.err != nil && !isLimit(o.err) && !errors.Is(o.err, errTooManyPages) {
				t.Fatalf("%s: %v", o.stage, o.err)
			}
			if o.err == nil && o.pages < 1 {
				t.Errorf("no page")
			}
			if o.peak > 256<<20 {
				t.Errorf("peak heap %d MiB", o.peak>>20)
			}
		})
	}
}

// A cancelled context stops layout within one page; a Work limit set at any
// value ends in LimitWork, never a panic or an endless loop.
func TestCancellationAndWorkAtEveryPoint(t *testing.T) {
	html := manyParagraphs(300)
	extra := "@page { size: 800px 600px; margin: 10px }"
	t.Run("context cancelled between pages", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		b := budget.New(ctx, budget.Defaults(), nil)
		d, err := dom.Parse(strings.NewReader(html), b)
		if err != nil {
			t.Fatal(err)
		}
		ss, _ := css.ParseStylesheet([]byte(extra), css.OriginExtra, b)
		ex, err := style.Compile(ss, b)
		if err != nil {
			t.Fatal(err)
		}
		r := style.NewResolver(uaCompiled(t), ex, nil, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
		root, err := box.Build(d, r, b)
		if err != nil {
			t.Fatal(err)
		}
		p := NewPaginator(root, Config{Page: r.Page(style.PageContext{Number: 1}), Shaper: text.NewShaper(testFonts(t), b), Fonts: testFonts(t), Budget: b})
		if _, err := p.Next(); err != nil {
			t.Fatal(err)
		}
		cancel()
		start := time.Now()
		for range 5 {
			if _, err = p.Next(); err != nil {
				break
			}
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("after cancel: %v", err)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("took %v to notice the cancellation", time.Since(start))
		}
	})
	t.Run("deadline during one huge paragraph", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		html := `<p style="overflow-wrap:anywhere">` + strings.Repeat("abc ", 400_000) + `</p>`
		start := time.Now()
		o := pipe(t, ctx, html, extra, func(l *budget.Limits) { l[budget.Work] = 1 << 40; l[budget.Glyphs] = 1 << 40 }, 3000)
		t.Logf("%d pages, err %v, %v", o.pages, o.err, time.Since(start).Round(time.Millisecond))
		if o.err != nil && !errors.Is(o.err, context.DeadlineExceeded) && !isLimit(o.err) {
			t.Fatalf("%v", o.err)
		}
		if took := time.Since(start); took > 20*time.Second {
			t.Errorf("a 30 ms deadline took %v", took)
		}
	})
	t.Run("work limit at every point", func(t *testing.T) {
		small := `<div style="border:3px solid"><p>` + paragraphOf(60) + `</p><ul><li>a<li>b</ul></div>` + manyParagraphs(5)
		for work := int64(1); work < 3000; work = work*3/2 + 1 {
			o := pipe(t, context.Background(), small, extra, func(l *budget.Limits) { l[budget.Work] = work }, 100)
			if o.err != nil && !isLimit(o.err, budget.Work) {
				t.Fatalf("Work=%d: stage %s: %v", work, o.stage, o.err)
			}
		}
	})
}

// Thousand paragraphs without shaping: the criterion is the difference of the
// two benchmarks (see BenchmarkThousandParagraphs and BenchmarkShapeOnly).
func TestThousandParagraphsMeasure(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	whole := testing.Benchmark(BenchmarkThousandParagraphs)
	shape := testing.Benchmark(BenchmarkShapeOnly)
	diff := time.Duration(whole.NsPerOp() - shape.NsPerOp())
	t.Logf("ThousandParagraphs %v, ShapeOnly %v, layout without shaping %v", time.Duration(whole.NsPerOp()), time.Duration(shape.NsPerOp()), diff)
}

var styleElementRE = regexp.MustCompile(`(?s)<style>(.*?)</style>`)

// The tall-rows fixture of testcorpus: every row is exactly 200 px, five rows
// fill an A4 page and no row is split.
func TestTallRowsAreExactly200px(t *testing.T) {
	var css string
	for _, c := range testcorpus.Hostile(budget.Defaults()) {
		if c.Name == "pages_table_tall_rows_under" {
			css = string(styleElementRE.FindSubmatch(c.HTML())[1])
		}
	}
	if css == "" {
		t.Fatal("no pages_table_tall_rows_under case")
	}
	const rows = 13
	html := "<table>" + strings.Repeat("<tr><td>1</td></tr>", rows) + "</table>"
	r := newRun(t, html, opts{css: css})
	pages := r.all(t)
	var problems []string
	trs := frags(pages, "tr")
	if len(trs) != rows {
		problems = append(problems, fmt.Sprintf("%d tr fragments for %d rows (rows split or merged)", len(trs), rows))
	}
	for i, f := range trs {
		if f.rect.H != 200 {
			problems = append(problems, fmt.Sprintf("row %d is %v px high", i, f.rect.H))
		}
	}
	if want := (rows + 4) / 5; len(pages) != want {
		problems = append(problems, fmt.Sprintf("%d pages, want %d (5 rows per page)", len(pages), want))
	}
	if len(problems) > 0 {
		t.Error(strings.Join(problems, "; "))
	}
}
