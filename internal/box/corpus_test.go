package box

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

var corpusSizes = []testcorpus.Size{testcorpus.Small, testcorpus.Large}

func corpusNames(tb testing.TB) []string {
	tb.Helper()
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		tb.Fatal(err)
	}
	return names
}

func parseReport(tb testing.TB, name string, size testcorpus.Size, b *budget.Budget) *dom.Document {
	tb.Helper()
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
	if err != nil {
		tb.Fatal(err)
	}
	return parseDoc(tb, string(html), b)
}

func newResolver(tb testing.TB, d *dom.Document, ua *style.Sheet, b *budget.Budget) *style.Resolver {
	tb.Helper()
	env := css.MediaEnv{Print: true, Width: 794, Height: 1123}
	return style.NewResolver(ua, nil, documentSheets(tb, d, b), env, b)
}

func testGC() { runtime.GC() }

// BenchmarkBuild reports the time to build the box tree of each report, styles
// included (design: 00-large under 15 ms).
func BenchmarkBuild(b *testing.B) {
	ua, err := style.CompileUA(budget.New(context.Background(), budget.Defaults(), nil))
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range corpusNames(b) {
		for _, size := range corpusSizes {
			b.Run(name+"/"+string(size), func(b *testing.B) {
				setup := testBudget(nil)
				d := parseReport(b, name, size, setup)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					bud := testBudget(nil)
					st := &testStyler{Resolver: newResolver(b, d, ua, bud)}
					if _, err := Build(d, st, bud); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkBuildCachedStyles is BenchmarkBuild with the styles already
// computed by an earlier pass over the same resolver: it isolates the cost of
// the box tree itself.
func BenchmarkBuildCachedStyles(b *testing.B) {
	ua, err := style.CompileUA(budget.New(context.Background(), budget.Defaults(), nil))
	if err != nil {
		b.Fatal(err)
	}
	for _, name := range corpusNames(b) {
		b.Run(name+"/large", func(b *testing.B) {
			bud := testBudget(func(l *budget.Limits) { l[budget.Boxes] = 1 << 40; l[budget.Work] = 1 << 40 })
			d := parseReport(b, name, testcorpus.Large, bud)
			st := &testStyler{Resolver: newResolver(b, d, ua, bud)}
			if _, err := Build(d, st, bud); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Build(d, st, bud); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
