package htmltopdf

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// M1-15 skeleton-level suites: the hostile corpus, the budgets of the report
// corpus, the combined case, the benchmark with its heap-max and the PDF dump
// the dev scripts (tmp/visual.sh, tmp/perf.sh) use. TestHostileCorpus,
// FuzzRender and TestCorpusReportsRenderAndValidate are M1-14's closure tests.

const (
	suiteHeapCeiling = 256 << 20
	suiteSlowLog     = 2 * time.Second
	// suiteValidateMax is the largest PDF handed to pdfcpu; larger ones are
	// checked by the cheaper own reader (pdftest.Parse).
	suiteValidateMax = 32 << 20
)

// limitsOf converts the budget limits of a corpus case to the public ones.
func limitsOf(l budget.Limits) Limits {
	return Limits{
		MaxInputBytes: l[budget.InputBytes], MaxNodes: int(l[budget.Nodes]), MaxCSSBytes: l[budget.CSSBytes],
		MaxSelectors: int(l[budget.Selectors]), MaxVarBytes: l[budget.VarBytes], MaxStyles: int(l[budget.Styles]),
		MaxBoxes: int(l[budget.Boxes]), MaxGlyphs: int(l[budget.Glyphs]), MaxPages: int(l[budget.Pages]),
		MaxOutputBytes: l[budget.OutputBytes], MaxWork: l[budget.Work],
	}
}

// caseLimits returns the default limits with the case adjustments applied.
func caseLimits(c testcorpus.Case) Limits {
	l := budget.Defaults()
	if c.Limits != nil {
		c.Limits(&l)
	}
	return limitsOf(l)
}

// peakOf runs fn and returns the highest live heap above the starting heap.
func peakOf(fn func()) uint64 {
	runtime.GC()
	const name = "/memory/classes/heap/objects:bytes"
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
			time.Sleep(500 * time.Microsecond)
		}
	}()
	fn()
	stop.Store(true)
	<-done
	return hi.Load() - min(base, hi.Load())
}

type variant struct {
	name string
	opts func(testing.TB) Options
	// expect adjusts the expectation of a case for this variant; ok false
	// means "use the case's own Want".
	expect func(c testcorpus.Case) (testcorpus.Outcome, bool)
}

var variants = []variant{
	{name: "default", opts: func(testing.TB) Options { return Options{} }},
	{name: "host-connector", opts: referenceOptions, expect: func(c testcorpus.Case) (testcorpus.Outcome, bool) {
		// ExtraCSS @page wins over the document: nothing to clamp or warn.
		switch c.Name {
		case "page_size_max_over", "page_size_min_over", "page_margins_over", "page_size_zero", "font_size_1e30":
			return testcorpus.Robust, true
		}
		return 0, false
	}},
}

// robustAnyway lists the cases whose expectation depends on stages that are
// not complete yet; they still run (no panic, bounded heap, valid PDF) and
// their outcome is logged, not asserted. Each entry names what is missing.
var robustAnyway = map[string]string{
	"boxes_pseudo_over": "::before/::after boxes are generated from M3",
}

type suiteOutcome struct {
	res   *Result
	err   error
	out   []byte
	took  time.Duration
	peak  uint64
	input int
}

func renderCase(tb testing.TB, c *Converter, html []byte) suiteOutcome {
	tb.Helper()
	o := suiteOutcome{input: len(html)}
	var buf bytes.Buffer
	o.peak = peakOf(func() {
		start := time.Now()
		o.res, o.err = c.Render(context.Background(), bytes.NewReader(html), &buf)
		o.took = time.Since(start)
	})
	o.out = buf.Bytes()
	return o
}

func checkOutcome(t *testing.T, tc testcorpus.Case, want testcorpus.Outcome, o suiteOutcome) {
	t.Helper()
	if o.res == nil {
		t.Fatal("nil Result")
	}
	if errors.Is(o.err, ErrInternal) {
		t.Fatalf("recovered panic or internal error: %v", o.err)
	}
	t.Logf("input %d B, output %d B, %v, peak %.1f MiB, err=%v", o.input, len(o.out), o.took.Round(time.Millisecond), float64(o.peak)/(1<<20), o.err)
	if o.took > suiteSlowLog {
		t.Logf("SLOW: %v exceeds %v", o.took, suiteSlowLog)
	}
	if o.peak > suiteHeapCeiling {
		t.Errorf("peak heap %d MiB exceeds 256 MiB", o.peak>>20)
	}
	var le *LimitError
	isLimit := errors.As(o.err, &le)
	if o.err != nil && !isLimit && !errors.Is(o.err, context.DeadlineExceeded) {
		t.Fatalf("undocumented error: %v", o.err)
	}
	if why, ok := robustAnyway[tc.Name]; ok {
		t.Logf("expectation not asserted (%s): want %v got %v", why, want, o.err)
		want = testcorpus.Robust
	}
	switch want {
	case testcorpus.Success:
		if o.err != nil {
			t.Fatalf("want success, got %v", o.err)
		}
	case testcorpus.Limit:
		if !isLimit || string(le.Limit) != tc.Limit {
			t.Fatalf("want a %s LimitError, got %v", tc.Limit, o.err)
		}
	case testcorpus.Degraded:
		if o.err != nil || len(o.res.Warnings) == 0 {
			t.Fatalf("want a degraded success, got err %v with %d warnings", o.err, len(o.res.Warnings))
		}
	case testcorpus.LimitOrDegraded:
		if o.err != nil && !isLimit || o.err == nil && len(o.res.Warnings) == 0 {
			t.Fatalf("want a limit or a degraded success, got err %v with %d warnings", o.err, len(o.res.Warnings))
		}
	case testcorpus.Robust:
	}
	if o.err == nil {
		if o.res.Bytes != int64(len(o.out)) || o.res.Pages < 1 {
			t.Errorf("Result %d pages %d bytes, wrote %d", o.res.Pages, o.res.Bytes, len(o.out))
		}
		var doc *pdftest.Doc
		if len(o.out) <= suiteValidateMax {
			doc = pdftest.Validate(t, o.out)
		} else {
			// Too big for pdfcpu: the own reader still checks the structure.
			var err error
			if doc, err = pdftest.Parse(o.out); err != nil {
				t.Fatalf("PDF of %d bytes does not parse: %v", len(o.out), err)
			}
		}
		if len(doc.Pages) != o.res.Pages {
			t.Errorf("%d pages in the PDF, %d in the Result", len(doc.Pages), o.res.Pages)
		}
	}
}

// Every hostile case and twin of the corpus (spike documents included), through
// the public API, with the default options and with the host-connector options:
// no panic, heap under 256 MiB, the expected outcome, and a valid PDF whenever
// one is produced. Case.Limits adjusts the limits of the twins that raise one.
func TestSuiteHostile(t *testing.T) {
	all := append(testcorpus.SpikeHostile(), testcorpus.Hostile(budget.Defaults())...)
	if len(all) < 60 {
		t.Fatalf("only %d cases", len(all))
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			type timing struct {
				name string
				took time.Duration
				peak uint64
			}
			var timings []timing
			t.Cleanup(func() {
				slices.SortFunc(timings, func(a, b timing) int { return cmp.Compare(b.took, a.took) })
				for _, x := range timings[:min(5, len(timings))] {
					t.Logf("slowest (%s): %-28s %v, peak %.0f MiB", v.name, x.name, x.took.Round(time.Millisecond), float64(x.peak)/(1<<20))
				}
			})
			for _, tc := range all {
				t.Run(tc.Name, func(t *testing.T) {
					opts := v.opts(t)
					opts.Limits = caseLimits(tc)
					c := mustNew(t, opts)
					want := tc.Want
					if v.expect != nil {
						if w, ok := v.expect(tc); ok {
							want = w
						}
					}
					o := renderCase(t, c, tc.HTML())
					timings = append(timings, timing{tc.Name, o.took, o.peak})
					checkOutcome(t, tc, want, o)
				})
			}
		})
	}
}

// reportDocs returns the small and large report of every template.
func reportDocs(tb testing.TB) map[string][]byte {
	tb.Helper()
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		tb.Fatal(err)
	}
	out := map[string][]byte{}
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			data, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
			if err != nil {
				tb.Fatal(err)
			}
			out[testcorpus.FixtureName(name, size)] = data
		}
	}
	return out
}

// The combined hostile case of the architect: a 512 KiB stylesheet of thousands
// of rules and markup close to MaxBoxes. It ends in a limit error or a PDF
// within 256 MiB; the time is logged (target 2 s).
func TestSuiteCombinedStylesheetAndMarkup(t *testing.T) {
	l := budget.Defaults()
	var css strings.Builder
	css.WriteString("@page{size:A4;margin:10mm}")
	for i := 0; i < int(l[budget.Selectors])/2-2; i++ {
		fmt.Fprintf(&css, ".c%d,.d%d>i:nth-child(%dn+1){color:#%06x;margin:%dpx;padding:1px;background:linear-gradient(red,blue)}\n", i, i, i%9+1, i*37%0xffffff, i%7)
	}
	if pad := int(l[budget.CSSBytes]) - 64 - css.Len(); pad > 4 {
		css.WriteString("/*" + strings.Repeat(" ", pad-4) + "*/")
	}
	// Close to MaxBoxes with few nodes: 60 nested spans around 3 100 blocks; each
	// block re-opens the 60 inline boxes (about 190 000 boxes, 6 000 nodes).
	body := strings.Repeat("<span class=c1>", 60) + strings.Repeat("<div class=d2><i>x</i></div>", 3100) + strings.Repeat("</span>", 60)
	html := "<style>" + css.String() + "</style>" + body
	t.Logf("stylesheet %d B, markup %d B", css.Len(), len(body))
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			opts := v.opts(t)
			c := mustNew(t, opts)
			o := renderCase(t, c, []byte(html))
			checkOutcome(t, testcorpus.Case{Name: "combined"}, testcorpus.Robust, o)
			var le *LimitError
			if o.err != nil && !errors.As(o.err, &le) {
				t.Fatalf("err %v", o.err)
			}
		})
	}
}

// BenchmarkRender reports the time and the heap-max of rendering each report
// of the corpus with the host-connector options (the baseline of the task
// closure note).
func BenchmarkRender(b *testing.B) {
	docs := reportDocs(b)
	c := mustNew(b, referenceOptions(b))
	for name, html := range docs {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			var maxPeak uint64
			b.SetBytes(int64(len(html)))
			for b.Loop() {
				var buf bytes.Buffer
				peak := peakOf(func() {
					if _, err := c.Render(context.Background(), bytes.NewReader(html), &buf); err != nil {
						b.Fatal(err)
					}
				})
				maxPeak = max(maxPeak, peak+uint64(len(html)))
			}
			b.ReportMetric(float64(maxPeak)/(1<<20), "heap-max-MiB")
		})
	}
}

// TestSuiteWriteCorpusPDFs renders every report of the corpus (small and
// large) into $HTMLTOPDF_OUT, with a .txt of the text pdftest extracts from
// each: tmp/visual.sh compares it with poppler's pdftotext and makes the
// montages. It does nothing without the variable.
func TestSuiteWriteCorpusPDFs(t *testing.T) {
	if os.Getenv("HTMLTOPDF_OUT") == "" {
		t.Skip("HTMLTOPDF_OUT is not set")
	}
	out, err := os.OpenRoot(os.Getenv("HTMLTOPDF_OUT"))
	if err != nil {
		t.Fatalf("HTMLTOPDF_OUT must be an existing directory: %v", err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			t.Error(err)
		}
	}()
	c := mustNew(t, referenceOptions(t))
	for name, html := range reportDocs(t) {
		var buf bytes.Buffer
		res, err := c.Render(context.Background(), bytes.NewReader(html), &buf)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		doc := pdftest.Validate(t, buf.Bytes())
		if err := out.WriteFile(name+".pdf", buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		var txt strings.Builder
		for _, p := range doc.Pages {
			for _, s := range p.Text {
				txt.WriteString(s)
				txt.WriteByte('\n')
			}
			txt.WriteString("\f")
		}
		if err := out.WriteFile(name+".pdftest.txt", []byte(txt.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d pages, %d bytes, %d warnings", name, res.Pages, res.Bytes, len(res.Warnings))
	}
}
