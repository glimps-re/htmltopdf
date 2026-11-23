package css

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

var hostileStyle = regexp.MustCompile(`(?s)<style>(.*?)</style>`)

// Every selector hostile case of testcorpus goes through the stylesheet parser
// and ParseSelectors with the default limits: the member limit is a budget
// error, the compound and simple-selector limits are a dropped list with a
// warning, and the twins pass silently.
func TestHostileCorpusSelectors(t *testing.T) {
	seen := 0
	for _, c := range testcorpus.Hostile(budget.Defaults()) {
		if !strings.HasPrefix(c.Name, "selector") {
			continue
		}
		seen++
		t.Run(c.Name, func(t *testing.T) {
			m := hostileStyle.FindSubmatch(c.HTML())
			if m == nil {
				t.Fatal("no style element")
			}
			b := budget.New(context.Background(), budget.Defaults(), nil)
			ss, err := ParseStylesheet(m[1], OriginDocument, b)
			if err != nil {
				t.Fatal(err)
			}
			if len(ss.Rules) != 1 {
				t.Fatalf("%d rules", len(ss.Rules))
			}
			start := time.Now()
			sels, err := ParseSelectors(ss.Rules[0].Prelude, b)
			took := time.Since(start)
			w := warnText2(b)
			switch c.Limit {
			case "MaxSelectors":
				var le *budget.LimitError
				if c.Want == testcorpus.Limit {
					if !errors.As(err, &le) || le.Kind != budget.Selectors {
						t.Fatalf("got %v, want a MaxSelectors limit error", err)
					}
				} else if err != nil || len(sels) == 0 || w != "" {
					t.Fatalf("under the limit: %d selectors, err %v, warnings %q", len(sels), err, w)
				}
			case "compounds", "simple-selectors":
				if err != nil {
					t.Fatal(err)
				}
				if c.Want == testcorpus.Success {
					if len(sels) != 1 || w != "" {
						t.Fatalf("at the limit: %d selectors, warnings %q", len(sels), w)
					}
				} else if len(sels) != 0 || !strings.Contains(w, WarnUnsupportedSelector) {
					t.Fatalf("over the limit: %d selectors, warnings %q", len(sels), w)
				}
			}
			t.Logf("%d selectors, %v, err %v", len(sels), took.Round(time.Microsecond), err)
		})
	}
	if seen < 7 {
		t.Fatalf("%d selector cases", seen)
	}
}

func warnText2(b *budget.Budget) string {
	ws, _ := b.Warnings()
	var sb strings.Builder
	for _, w := range ws {
		sb.WriteString(w.Code + ":" + w.Detail + "\n")
	}
	return sb.String()
}

// The 00 stylesheet applied to the 00-large DOM.
func TestRules00OnLargeDOM(t *testing.T) {
	root := os.DirFS("../../testdata/templates")
	rep, err := testcorpus.Report(root, "00-host-connector", testcorpus.Large)
	if err != nil {
		t.Fatal(err)
	}
	b := budget.New(context.Background(), budget.Defaults(), nil)
	d, err := dom.Parse(strings.NewReader(string(rep)), b)
	if err != nil {
		t.Fatal(err)
	}
	small, err := testcorpus.Report(root, "00-host-connector", testcorpus.Small)
	if err != nil {
		t.Fatal(err)
	}
	var css strings.Builder
	for _, m := range hostileStyle.FindAllSubmatch(small, -1) {
		css.Write(m[1])
	}
	ss, err := ParseStylesheet([]byte(css.String()), OriginDocument, b)
	if err != nil {
		t.Fatal(err)
	}
	var ix Index[int]
	n := 0
	var walk func([]Rule)
	walk = func(rs []Rule) {
		for _, r := range rs {
			switch r.Kind {
			case RuleStyle:
				sels, err := ParseSelectors(r.Prelude, b)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range sels {
					ix.Add(s, n)
					n++
				}
			case RuleMedia:
				walk(r.Rules)
			}
		}
	}
	walk(ss.Rules)
	if ws, _ := b.Warnings(); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	run := func(b *budget.Budget) (matches int) {
		var buf []Entry[int]
		for e := range d.Elements {
			buf = mustCandidates(t, &ix, d, clampIdx(e), buf[:0], b)
			for _, c := range buf {
				ok, err := c.Selector.Match(d, clampIdx(e), b)
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					matches++
				}
			}
		}
		return
	}
	mb := budget.New(context.Background(), budget.Defaults(), nil)
	start := time.Now()
	matches := run(mb)
	took := time.Since(start)
	used := mb.Used(budget.Work)
	t.Logf("%d selectors, %d elements: %d matches, %v, %d work units (%.3f%% of MaxWork)",
		n, len(d.Elements), matches, took, used, 100*float64(used)/float64(budget.Defaults()[budget.Work]))
	if matches == 0 {
		t.Fatal("no rule matched")
	}
	if used > budget.Defaults()[budget.Work]/50 {
		t.Errorf("%d work units: more than 2%% of MaxWork for a real report", used)
	}
}

func BenchmarkRules00OnLargeDOM(b *testing.B) {
	root := os.DirFS("../../testdata/templates")
	rep, err := testcorpus.Report(root, "00-host-connector", testcorpus.Large)
	if err != nil {
		b.Fatal(err)
	}
	bud := budget.New(context.Background(), budget.Defaults(), nil)
	d, err := dom.Parse(strings.NewReader(string(rep)), bud)
	if err != nil {
		b.Fatal(err)
	}
	small, _ := testcorpus.Report(root, "00-host-connector", testcorpus.Small)
	var css strings.Builder
	for _, m := range hostileStyle.FindAllSubmatch(small, -1) {
		css.Write(m[1])
	}
	ss, err := ParseStylesheet([]byte(css.String()), OriginDocument, bud)
	if err != nil {
		b.Fatal(err)
	}
	var ix Index[int]
	for _, r := range ss.Rules {
		if r.Kind != RuleStyle {
			continue
		}
		sels, _ := ParseSelectors(r.Prelude, bud)
		for _, s := range sels {
			ix.Add(s, 0)
		}
	}
	l := budget.Defaults()
	l[budget.Work] = 1 << 62
	mb := budget.New(context.Background(), l, nil)
	b.ReportAllocs()
	b.ResetTimer()
	var buf []Entry[int]
	for b.Loop() {
		for e := range d.Elements {
			buf = mustCandidates(b, &ix, d, clampIdx(e), buf[:0], mb)
			for _, c := range buf {
				if _, err := c.Selector.Match(d, clampIdx(e), mb); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}
