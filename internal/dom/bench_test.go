package dom

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"golang.org/x/net/html"
)

const templatesDir = "../../testdata/templates"

// report returns the generated report of a corpus template.
func report(tb testing.TB, name string, size testcorpus.Size) []byte {
	tb.Helper()
	data, err := testcorpus.Report(os.DirFS(templatesDir), name, size)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

const largeReport = "00-host-connector"

func BenchmarkParse(b *testing.B) {
	data := report(b, largeReport, testcorpus.Large)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		bg := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := Parse(bytes.NewReader(data), bg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvert(b *testing.B) {
	data := report(b, largeReport, testcorpus.Large)
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		bg := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := Convert(root, bg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEstimate(b *testing.B) {
	data := report(b, largeReport, testcorpus.Large)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := estimateNodes(data, noLimit, noPoll); err != nil {
			b.Fatal(err)
		}
	}
}

func TestLargeReportEstimateTight(t *testing.T) {
	data := report(t, largeReport, testcorpus.Large)
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	real := countCost(root)
	est, err := estimateNodes(data, noLimit, noPoll)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("00-large: estimate %d, actual %d (limit %d)", est, real, budget.Defaults()[budget.Nodes])
	if est < real || est > 2*real {
		t.Fatalf("estimate %d not within [%d, %d]", est, real, 2*real)
	}
}

func TestReportsParse(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS(templatesDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(name+"/"+string(size), func(t *testing.T) {
				data := report(t, name, size)
				b := budget.New(context.Background(), budget.Defaults(), nil)
				doc, err := Parse(bytes.NewReader(data), b)
				if err != nil {
					t.Fatal(err)
				}
				est, _ := estimateNodes(data, noLimit, noPoll)
				t.Logf("%d bytes: %d elements, %d texts, nodes %d, estimate %d, title %q",
					len(data), len(doc.Elements), len(doc.Texts), b.Used(budget.Nodes), est, doc.Title)
				assertCovers(t, string(data))
			})
		}
	}
}
