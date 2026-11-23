package css

import (
	"context"
	"errors"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

var selectorSeeds = []string{
	"a", "a.b#c[d=e]:first-child::before", "a > b + c ~ d e", "*", "a,b,c", "a,,b", ":nth-child(2n+1)", "x:nth-child(-n+3)",
	"[a=b i]", "[a~=b]", "a:hover", ":root", "#", ".", "a|b", "a[", "a:", "a::", "a:nth-child(", "a:not(b)", "> a", "a >",
	"a\\ b", "a.\\31 23", "\xff\xfe", "p:nth-child(99999999999999999999n+1)", "a:nth-child(2n+)", "a:nth-child(+n-1)",
}

func FuzzSelector(f *testing.F) {
	for _, s := range selectorSeeds {
		f.Add(s)
	}
	doc := parseHTML(f, matchHTML)
	f.Fuzz(func(t *testing.T, text string) {
		l := budget.Defaults()
		l[budget.Selectors] = 1 << 30
		l[budget.Work] = 1 << 20
		b := budget.New(context.Background(), l, nil)
		ss, err := ParseStylesheet([]byte(text+"{x:y}"), OriginDocument, b)
		if err != nil {
			t.Fatal(err)
		}
		var prelude []Token
		if len(ss.Rules) > 0 {
			prelude = ss.Rules[0].Prelude
		}
		sels, err := ParseSelectors(prelude, b)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sels {
			if len(s.compounds) < 1 || len(s.compounds) > MaxCompounds || s.compounds[0].comb != CombNone {
				t.Fatalf("bad compound list in %q", s)
			}
			for i, c := range s.compounds {
				if c.simples() < 1 || c.simples() > MaxSimple || (i > 0 && c.comb == CombNone) {
					t.Fatalf("bad compound %d of %q", i, s)
				}
			}
			_ = s.String()
			_, _ = s.Key()
			for e := range doc.Elements {
				if _, err := s.Match(doc, int32(e), b); err != nil {
					var le *budget.LimitError
					if !errors.As(err, &le) || le.Kind != budget.Work {
						t.Fatal(err)
					}
					return
				}
			}
		}
	})
}
