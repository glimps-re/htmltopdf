package box

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// testStyler is the real resolver, counting the anonymous styles it is asked
// for.
type testStyler struct {
	*style.Resolver
	anonymous int
}

func (s *testStyler) Anonymous(parent *style.Computed, display style.Display) (*style.Computed, error) {
	s.anonymous++
	return s.Resolver.Anonymous(parent, display)
}

func testBudget(mod func(*budget.Limits)) *budget.Budget {
	l := budget.Defaults()
	l[budget.Nodes] = 1 << 22
	l[budget.InputBytes] = 1 << 26
	l[budget.CSSBytes] = 1 << 24
	if mod != nil {
		mod(&l)
	}
	return budget.New(context.Background(), l, nil)
}

func parseDoc(tb testing.TB, html string, b *budget.Budget) *dom.Document {
	tb.Helper()
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

// documentSheets compiles the style elements of d.
func documentSheets(tb testing.TB, d *dom.Document, b *budget.Budget) []*style.Sheet {
	tb.Helper()
	var sheets []*style.Sheet
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
		ss, err := css.ParseStylesheet([]byte(sb.String()), css.OriginDocument, b)
		if err != nil {
			tb.Fatal(err)
		}
		sh, err := style.Compile(ss, b)
		if err != nil {
			tb.Fatal(err)
		}
		sheets = append(sheets, sh)
	}
	return sheets
}

func newStyler(tb testing.TB, d *dom.Document, b *budget.Budget) *testStyler {
	tb.Helper()
	ua, err := style.CompileUA(budget.New(context.Background(), budget.Defaults(), nil))
	if err != nil {
		tb.Fatal(err)
	}
	env := css.MediaEnv{Print: true, Width: 794, Height: 1123}
	return &testStyler{Resolver: style.NewResolver(ua, nil, documentSheets(tb, d, b), env, b)}
}

// buildHTML builds the box tree of html and returns its dump without the html
// and body lines, de-indented, with the budget (for the warnings).
func buildHTML(tb testing.TB, html string) (string, *budget.Budget) {
	tb.Helper()
	b := testBudget(nil)
	d := parseDoc(tb, html, b)
	root, err := Build(d, newStyler(tb, d, b), b)
	if err != nil {
		tb.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Dump(&buf, root); err != nil {
		tb.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) < 2 || lines[0] != "block html" || lines[1] != "  block body" {
		tb.Fatalf("unexpected root:\n%s", buf.String())
	}
	var out []string
	for _, l := range lines[2:] {
		out = append(out, strings.TrimPrefix(l, "    "))
	}
	return strings.Join(out, "\n"), b
}

func warnings(b *budget.Budget) []string {
	ws, _ := b.Warnings()
	var out []string
	for _, w := range ws {
		out = append(out, w.Code+":"+w.Detail)
	}
	return out
}

var emptyDoc dom.Document

func testBudgetCancelled() *budget.Budget {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return budget.New(ctx, budget.Defaults(), nil)
}
