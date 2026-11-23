package css

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func buildIndex(tb testing.TB, texts []string) (*Index[int], []*Selector) {
	tb.Helper()
	ix := &Index[int]{}
	var all []*Selector
	for i, text := range texts {
		sels, warn := parseSel(tb, text)
		if len(sels) != 1 {
			tb.Fatalf("%q: %v", text, warn)
		}
		ix.Add(sels[0], i)
		all = append(all, sels[0])
	}
	return ix, all
}

func TestIndexCandidatesMatchBruteForce(t *testing.T) {
	doc := parseHTML(t, matchHTML)
	texts := []string{
		"p", "#i3", ".x", ".x.y", "div p", "section > div > p", "p + span", "p ~ b", "[data-k]", "*", ":root", "p:first-child",
		"custom-tag", "div.a", ".a > p", "span ~ p", "li", "#nope", ".nope", "a:hover", "p::before", "body > *", "* > p", "div *",
		"[class]", "p:nth-child(2n)", "P", "CUSTOM-TAG",
	}
	ix, sels := buildIndex(t, texts)
	b := budget.New(context.Background(), budget.Defaults(), nil)
	var cand []Entry[int]
	for e := range doc.Elements {
		cand = mustCandidates(t, ix, doc, clampIdx(e), cand[:0], b)
		var got, want []int
		for _, c := range cand {
			ok, err := c.Selector.Match(doc, int32(e), b)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				got = append(got, c.Value)
			}
		}
		for i, s := range sels {
			ok, err := s.Match(doc, int32(e), b)
			if err != nil {
				t.Fatal(err)
			}
			if ok && !s.NeverMatches() {
				want = append(want, i)
			}
		}
		sort.Ints(got)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("element %d (%s): index gives %v, brute force %v", e, doc.Elements[e].Tag, got, want)
		}
	}
}

func TestIndexSkipsNeverMatching(t *testing.T) {
	ix, _ := buildIndex(t, []string{"a", "a:hover", "b:focus c", "d"})
	if ix.Len() != 2 {
		t.Fatalf("Len = %d, want 2", ix.Len())
	}
}

func TestIndexFilesByRightmostKey(t *testing.T) {
	ix, _ := buildIndex(t, []string{"#a", "p#a", ".c", ".c.d", "x .c", "span", "custom-tag", "*", "[k]", ":first-child"})
	if len(ix.byID["a"]) != 2 || len(ix.byClass["c"]) != 3 || len(ix.byTag["span"]) != 1 || len(ix.byTag["custom-tag"]) != 1 || len(ix.universal) != 3 {
		t.Fatalf("id %d class %d span %d custom-tag %d universal %d", len(ix.byID["a"]), len(ix.byClass["c"]), len(ix.byTag["span"]), len(ix.byTag["custom-tag"]), len(ix.universal))
	}
}

func TestIndexConcurrentReaders(t *testing.T) {
	doc := parseHTML(t, matchHTML)
	ix, _ := buildIndex(t, []string{"p", ".x", "div p", "#i3", "*"})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := budget.New(context.Background(), budget.Defaults(), nil)
			var cand []Entry[int]
			for e := range doc.Elements {
				cand = mustCandidates(t, ix, doc, clampIdx(e), cand[:0], b)
				for _, c := range cand {
					if _, err := c.Selector.Match(doc, int32(e), b); err != nil {
						t.Error(err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

// siblings builds a document of n sibling <i> elements.
func siblings(tb testing.TB, n int) *dom.Document {
	tb.Helper()
	return parseHTML(tb, "<div>"+strings.Repeat("<i class=k></i>", n)+"</div>")
}

func TestSiblingPseudoClassesAreConstantTime(t *testing.T) {
	// One check of :last-child, :nth-child or + on the last of 100 000
	// siblings charges a constant amount of Work.
	doc := siblings(t, 100_000)
	last := clampIdx(len(doc.Elements) - 1)
	for _, text := range []string{"i:last-child", "i:nth-child(3n+2)", ".k + .k", "i:first-child", "i:nth-child(-n+3)"} {
		sels, _ := parseSel(t, text)
		b := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := sels[0].Match(doc, last, b); err != nil {
			t.Fatal(err)
		}
		if used := b.Used(budget.Work); used > workCandidate+workCombinator+workStep+4 { // +4: one unit per class test
			t.Errorf("%q charged %d Work on the last sibling", text, used)
		}
	}
}

// mustCandidates is Candidates failing the test on a budget error.
func mustCandidates[T any](tb testing.TB, ix *Index[T], d *dom.Document, e int32, dst []Entry[T], b *budget.Budget) []Entry[T] {
	tb.Helper()
	dst, err := ix.Candidates(d, e, dst, b)
	if err != nil {
		tb.Fatal(err)
	}
	return dst
}
