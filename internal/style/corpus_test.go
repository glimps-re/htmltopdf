package style

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const templatesDir = "../../testdata/templates"

// documentCSS returns the text of the HTML style elements of d.
func documentCSS(d *dom.Document) []string {
	var out []string
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Tag.String() != "style" || e.NS != dom.NSHTML {
			continue
		}
		var sb strings.Builder
		for r := e.FirstChild; r != dom.NoNode; r = d.NextSibling(r) {
			if ti, ok := r.Text(); ok {
				sb.WriteString(d.Texts[ti].Data)
			}
		}
		out = append(out, sb.String())
	}
	return out
}

func loadReport(t testing.TB, name string, size testcorpus.Size) ([]byte, *dom.Document) {
	t.Helper()
	data, err := testcorpus.Report(os.DirFS(templatesDir), name, size)
	if err != nil {
		t.Fatal(err)
	}
	d, err := dom.Parse(bytes.NewReader(data), newBudget())
	if err != nil {
		t.Fatal(err)
	}
	return data, d
}

func compileDoc(t testing.TB, d *dom.Document) ([]*Sheet, *Sheet) {
	t.Helper()
	sheets, extra, _ := compileDocWarnings(t, d)
	return sheets, extra
}

func compileDocWarnings(t testing.TB, d *dom.Document) ([]*Sheet, *Sheet, *budget.Budget) {
	t.Helper()
	b := newBudget()
	var sheets []*Sheet
	for _, src := range documentCSS(d) {
		sheets = append(sheets, compileSheet(t, src, css.OriginDocument, b))
	}
	return sheets, compileSheet(t, testcorpus.HostConnectorPageCSS, css.OriginExtra, b), b
}

func cascade(t testing.TB, d *dom.Document, sheets []*Sheet, extra *Sheet, b *budget.Budget) []*Computed {
	t.Helper()
	r := NewResolver(ua(), extra, sheets, printEnv, b)
	styles := make([]*Computed, len(d.Elements))
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		c, err := r.Element(d, int32(i), parent)
		if err != nil {
			t.Fatal(err)
		}
		styles[i] = c
	}
	return styles
}

func distinct(styles []*Computed) int {
	seen := make(map[*Computed]bool)
	for _, s := range styles {
		seen[s] = true
	}
	return len(seen)
}

// The corpus of the host-connector reports: few distinct styles, no warning
// beyond the documented ones.
func TestCorpusDistinctStyles(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS(templatesDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(name+"/"+string(size), func(t *testing.T) {
				_, d := loadReport(t, name, size)
				sheets, extra := compileDoc(t, d)
				b := newBudget()
				styles := cascade(t, d, sheets, extra, b)
				n := distinct(styles)
				t.Logf("%d elements, %d distinct styles, %d warnings", len(d.Elements), n, func() int { w, _ := b.Warnings(); return len(w) }())
				if name == "00-host-connector" && n > 64 {
					t.Fatalf("%d distinct styles, want at most 64", n)
				}
			})
		}
	}
}

func TestCorpus00Values(t *testing.T) {
	_, d := loadReport(t, "00-host-connector", testcorpus.Small)
	sheets, extra := compileDoc(t, d)
	styles := cascade(t, d, sheets, extra, newBudget())
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Tag.String() == "header" {
			c := styles[i]
			if c.BackgroundImage == nil || len(c.BackgroundImage.Stops) != 2 || c.BackgroundImage.Angle != 90 {
				t.Errorf("header gradient through var(): %+v", c.BackgroundImage)
			}
			if c.Radius[TopLeft].H.Value != 8 || c.Padding[Top].Value != 24 || c.MaxWidth.Value != 900 || c.TextAlign != AlignCenter {
				t.Errorf("header: radius %+v padding %+v max-width %+v", c.Radius[TopLeft], c.Padding[Top], c.MaxWidth)
			}
			return
		}
	}
	t.Fatal("no header")
}

func TestCorpusIsWarningFreeExceptBoxShadow(t *testing.T) {
	_, d := loadReport(t, "00-host-connector", testcorpus.Large)
	sheets, extra, b := compileDocWarnings(t, d)
	cascade(t, d, sheets, extra, b)
	w, _ := b.Warnings()
	if len(w) != 1 {
		t.Errorf("want the box-shadow warning only, got %+v", w)
	}
	for _, v := range w {
		if v.Code == budget.WarnUnsupportedProperty && v.Detail == "box-shadow" {
			continue
		}
		t.Errorf("unexpected warning %+v", v)
	}
}

func BenchmarkCascade00Large(b *testing.B) {
	data, err := testcorpus.Report(os.DirFS(templatesDir), "00-host-connector", testcorpus.Large)
	if err != nil {
		b.Fatal(err)
	}
	d, err := dom.Parse(bytes.NewReader(data), newBudget())
	if err != nil {
		b.Fatal(err)
	}
	sheets, extra := compileDoc(b, d)
	b.ReportAllocs()
	for b.Loop() {
		bg := newBudget()
		r := NewResolver(ua(), extra, sheets, printEnv, bg)
		styles := make([]*Computed, len(d.Elements))
		for i := range d.Elements {
			var parent *Computed
			if p := d.Elements[i].Parent; p >= 0 {
				parent = styles[p]
			}
			c, err := r.Element(d, int32(i), parent)
			if err != nil {
				b.Fatal(err)
			}
			styles[i] = c
		}
	}
}

func uniqueStyleDoc(t testing.TB, n int) *dom.Document {
	t.Helper()
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<p style="margin-left:%dpx">`, i)
	}
	l := budget.Defaults()
	l[budget.InputBytes] = 64 << 20
	l[budget.Nodes] = 10 << 20
	d, err := dom.Parse(strings.NewReader(sb.String()), budget.New(context.Background(), l, nil))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func resolveAll(d *dom.Document, b *budget.Budget) error {
	r := NewResolver(ua(), nil, nil, printEnv, b)
	styles := make([]*Computed, len(d.Elements))
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		c, err := r.Element(d, int32(i), parent)
		if err != nil {
			return err
		}
		styles[i] = c
	}
	return nil
}

// html, head? and body take 2 styles; each unique style attribute adds one.
func TestStylesLimit(t *testing.T) {
	limit := int(budget.Defaults()[budget.Styles])
	// html and body: 2 styles. A p opened inside a p closes it, so all p share
	// the body as parent: n unique p cost n styles.
	atLimit := uniqueStyleDoc(t, limit-2-1) // minus html, body and head
	if err := resolveAll(atLimit, newBudget()); err != nil {
		t.Fatalf("twin at the limit: %v", err)
	}
	over := uniqueStyleDoc(t, limit)
	err := resolveAll(over, newBudget())
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Styles || le.Max != int64(limit) {
		t.Fatalf("over the limit: %v", err)
	}
	huge := uniqueStyleDoc(t, 100_000)
	if err := resolveAll(huge, newBudget()); !errors.As(err, &le) || le.Kind != budget.Styles {
		t.Fatalf("100000 unique style attributes: %v", err)
	}
}

func TestSharedSheetsAcrossRenders(t *testing.T) {
	_, d := loadReport(t, "00-host-connector", testcorpus.Small)
	sheets, extra := compileDoc(t, d)
	var wg sync.WaitGroup
	results := make([][]*Computed, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = cascade(t, d, sheets, extra, newBudget())
		}()
	}
	wg.Wait()
	for i := range results {
		for j := range results[i] {
			a, b := results[0][j], results[i][j]
			if a.FontSize != b.FontSize || a.Color != b.Color || a.Margin != b.Margin || a.Display != b.Display {
				t.Fatalf("render %d element %d differs", i, j)
			}
		}
	}
}
