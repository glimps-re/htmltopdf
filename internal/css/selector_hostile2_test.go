//go:build !race

package css

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// siblingDoc is one <div> holding n <i class=k> followed by a <b>.
func siblingDoc(tb testing.TB, n int) string {
	tb.Helper()
	return "<!doctype html><html><body><div>" + strings.Repeat("<i class=k></i>", n) + "<b class=k></b></div></body></html>"
}

// TestHostileSiblingsDefaultWork: 100 000 siblings against an index of 5 000
// distinct selectors, with the default MaxWork. Every shape ends in a Work
// limit error or completes; the wall time and the units used are logged (the
// target of design §6.2 is 2 s on a quiet machine: see
// BenchmarkHostileSiblings).
func TestHostileSiblingsDefaultWork(t *testing.T) {
	const n, sels = 100_000, 5000
	doc := parseHTML(t, siblingDoc(t, n))
	shapes := map[string]func(k int) string{
		":last-child":       func(int) string { return "i:last-child" },
		":nth-child":        func(k int) string { return fmt.Sprintf("i:nth-child(%dn+1)", k+1) },
		"~ same":            func(int) string { return ".k ~ .k" },
		"~ then tag":        func(k int) string { return fmt.Sprintf("i ~ b.k:nth-child(%dn)", k+2) },
		"+ same":            func(int) string { return ".k + .k" },
		"+ chain":           func(int) string { return ".k + .k + .k + .k" },
		"~ chain":           func(int) string { return ".k ~ .k ~ .k ~ .k" },
		"descendant ~":      func(int) string { return "div .k ~ .k" },
		"nth after ~":       func(k int) string { return fmt.Sprintf(".k ~ i:nth-child(%dn+%d)", k+2, k%7) },
		"backtracking ~ >":  func(int) string { return "div > .k ~ .k ~ .k > b" },
		"~ failing at root": func(int) string { return "u ~ .k ~ .k" },
	}
	for name, gen := range shapes {
		t.Run(name, func(t *testing.T) {
			var ix Index[int]
			b := budget.New(context.Background(), budget.Defaults(), nil)
			for k := range sels {
				ss, _ := parseSel(t, gen(k))
				if len(ss) != 1 {
					t.Fatalf("%q does not parse", gen(k))
				}
				ix.Add(ss[0], k)
			}
			var buf []Entry[int]
			var err error
			matches, tests := 0, 0
			start := time.Now()
		run:
			for e := range doc.Elements {
				if buf, err = ix.Candidates(doc, clampIdx(e), buf[:0], b); err != nil {
					break run
				}
				for _, c := range buf {
					var ok bool
					if ok, err = c.Selector.Match(doc, clampIdx(e), b); err != nil {
						break run
					}
					tests++
					if ok {
						matches++
					}
				}
			}
			took := time.Since(start)
			var le *budget.LimitError
			if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
				t.Fatalf("error %v", err)
			}
			t.Logf("%-18s %8d tests, %8d matches, work %d, %v, err %v", name, tests, matches, b.Used(budget.Work), took.Round(time.Millisecond), err != nil)
		})
	}
}

// A selector whose matching is exponential without the Work bound.
func TestBacktrackingExplosionIsBounded(t *testing.T) {
	doc := parseHTML(t, strings.Repeat("<div class=d>", 400)+"<p id=i1></p>"+strings.Repeat("</div>", 400))
	for _, text := range []string{
		strings.Repeat(".d > .d ", 8) + "p.nope",
		strings.Repeat(".d ", 30) + "p.nope",
		strings.Repeat("div:nth-child(1) > ", 20) + "p.nope",
		strings.Repeat(".d ~ .d > ", 10) + "p.nope",
	} {
		sels, _ := parseSel(t, text)
		if len(sels) != 1 {
			t.Fatalf("%q", text)
		}
		b := budget.New(context.Background(), budget.Defaults(), nil)
		start := time.Now()
		var err error
		for e := range doc.Elements {
			if _, err = sels[0].Match(doc, clampIdx(e), b); err != nil {
				break
			}
		}
		var le *budget.LimitError
		if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
			t.Fatalf("%.40q: %v", text, err)
		}
		t.Logf("%.50q...: work %d, limit hit %v, %v", text, b.Used(budget.Work), err != nil, time.Since(start).Round(time.Millisecond))
	}
}
