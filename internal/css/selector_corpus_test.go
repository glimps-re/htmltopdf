package css_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// styleSelectors returns the selectors of every style rule of the sheets of a
// report, with the number of rules.
func styleSelectors(tb testing.TB, name string) ([]*css.Selector, int) {
	tb.Helper()
	b := budget.New(context.Background(), budget.Defaults(), nil)
	ss, err := css.ParseStylesheet([]byte(styleSheets(tb, name)), css.OriginDocument, b)
	if err != nil {
		tb.Fatal(err)
	}
	var sels []*css.Selector
	rules := 0
	var walk func([]css.Rule)
	walk = func(rs []css.Rule) {
		for _, r := range rs {
			switch r.Kind {
			case css.RuleStyle:
				rules++
				list, err := css.ParseSelectors(r.Prelude, b)
				if err != nil {
					tb.Fatal(err)
				}
				sels = append(sels, list...)
			case css.RuleMedia:
				walk(r.Rules)
			}
		}
	}
	walk(ss.Rules)
	if ws, _ := b.Warnings(); len(ws) != 0 {
		tb.Fatalf("%s: warnings %v", name, ws)
	}
	return sels, rules
}

func reportDOM(tb testing.TB, name string, size testcorpus.Size) *dom.Document {
	tb.Helper()
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
	if err != nil {
		tb.Fatal(err)
	}
	d, err := dom.Parse(bytes.NewReader(html), budget.New(context.Background(), budget.Defaults(), nil))
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

// Every selector of the four corpus stylesheets is supported.
func TestCorpusSelectorsParse(t *testing.T) {
	want := map[string]int{ // style rules
		"00-host-connector": 19, "01-modern": 54, "02-executive": 61, "03-analyst": 57,
	}
	for name := range want {
		sels, rules := styleSelectors(t, name)
		t.Logf("%s: %d style rules, %d selectors", name, rules, len(sels))
		if rules != want[name] || len(sels) < rules {
			t.Errorf("%s: %d rules (want %d), %d selectors", name, rules, want[name], len(sels))
		}
	}
}

// allRulesMatch runs the whole rule set of 00 against every element of the
// DOM through the index and returns the number of (element, selector) matches.
func allRulesMatch(tb testing.TB, ix *css.Index[int], d *dom.Document, b *budget.Budget) int {
	var cand []css.Entry[int]
	matches := 0
	for e := range d.Elements {
		var err error
		if cand, err = ix.Candidates(d, int32(e), cand[:0], b); err != nil {
			tb.Fatal(err)
		}
		for _, c := range cand {
			ok, err := c.Selector.Match(d, int32(e), b)
			if err != nil {
				tb.Fatal(err)
			}
			if ok {
				matches++
			}
		}
	}
	return matches
}

func rulesIndex(tb testing.TB) *css.Index[int] {
	sels, _ := styleSelectors(tb, "00-host-connector")
	ix := &css.Index[int]{}
	for i, s := range sels {
		ix.Add(s, i)
	}
	return ix
}

func TestAllRulesOnLargeDOM(t *testing.T) {
	d := reportDOM(t, "00-host-connector", testcorpus.Large)
	ix := rulesIndex(t)
	b := budget.New(context.Background(), budget.Defaults(), nil)
	start := time.Now()
	n := allRulesMatch(t, ix, d, b)
	t.Logf("%d elements, %d selectors, %d matches in %v, %d Work", len(d.Elements), ix.Len(), n, time.Since(start), b.Used(budget.Work))
	if n == 0 {
		t.Fatal("no selector matched the report")
	}
	// M1-14 calibrates MaxWork at 20 times the corpus maximum.
	if used, limit := b.Used(budget.Work), budget.Defaults()[budget.Work]; used > limit/20 {
		t.Fatalf("matching charged %d Work, more than 1/20 of MaxWork (%d)", used, limit)
	}
}

func BenchmarkAllRulesOnLargeDOM(b *testing.B) {
	d := reportDOM(b, "00-host-connector", testcorpus.Large)
	ix := rulesIndex(b)
	l := budget.Defaults()
	l[budget.Work] = 1 << 62
	bud := budget.New(context.Background(), l, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		allRulesMatch(b, ix, d, bud)
	}
}
