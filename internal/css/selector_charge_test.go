package css

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func TestAttributeOperators(t *testing.T) {
	doc := parseHTML(t, `<p id=i1 x="Hello World" class="Foo bar"></p><p id=i2 x="en-US"></p><p id=i3 x="  a  b "></p>`+
		"<p id=i4 x=\"K\"></p><p id=i5 x=\"k\"></p><p id=i6></p>")
	tests := []struct {
		sel  string
		want []int
	}{
		{`[x="Hello World"]`, []int{1}},
		{`[x="hello world"]`, []int{}},
		{`[x="hello world" i]`, []int{1}},
		{`[x="HELLO WORLD" i]`, []int{1}},
		{`[x="hello world" s]`, []int{}},
		{`[x~=World]`, []int{1}},
		{`[x~=world]`, []int{}},
		{`[x~=world i]`, []int{1}},
		{`[x~=a]`, []int{3}},
		{`[x~=b]`, []int{3}},
		{`[x~=""]`, []int{}},
		{`[x~="a b"]`, []int{}},
		{`[x|=en]`, []int{2}},
		{`[x|=EN i]`, []int{2}},
		{`[x|=EN]`, []int{}},
		{`[x|=en-US]`, []int{2}},
		{`[x|=e]`, []int{}},
		{`[x^=Hel]`, []int{1}},
		{`[x^=hel]`, []int{}},
		{`[x^=hel i]`, []int{1}},
		{`[x^=""]`, []int{}},
		{`[x$=World]`, []int{1}},
		{`[x$=WORLD i]`, []int{1}},
		{`[x$=orld]`, []int{1}},
		{`[x*="lo Wo"]`, []int{1}},
		{`[x*="LO WO" i]`, []int{1}},
		{`[x*=zzz i]`, []int{}},
		{`[x*=""]`, []int{}},
		// The i flag folds ASCII only: the Kelvin sign is not "k".
		{`[x=k i]`, []int{5}},
		{`[x=K i]`, []int{5}},
		{`[x*=k i]`, []int{5}},
		{`[x^=k i]`, []int{5}},
		{`[x$=k i]`, []int{5}},
		{`[x="\212a" i]`, []int{4}},
		// class attribute, without joining the classes.
		{`[class]`, []int{1}},
		{`[class="Foo bar"]`, []int{1}},
		{`[class~=Foo]`, []int{1}},
		{`[class~=foo]`, []int{}},
		{`[class~=foo i]`, []int{1}},
		{`[class~="Foo bar"]`, []int{}},
		{`[class^=Foo]`, []int{1}},
		{`[class$=bar]`, []int{1}},
		{`[class*="o b"]`, []int{1}},
		{`[class|=Foo]`, []int{}},
		{`[id=i1]`, []int{1}},
		{`[id]`, []int{1, 2, 3, 4, 5, 6}},
		{`[missing]`, []int{}},
		{`[missing=x]`, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.sel, func(t *testing.T) {
			got := matchingIDs(t, doc, "p"+tt.sel)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIgnoreCaseFlagIsASCIIOnlyWhenParsing(t *testing.T) {
	sels, _ := parseSel(t, "[x=K i]")
	if got := sels[0].compounds[0].attrs[0].val; got != "K" {
		t.Fatalf("value folded to %q", got)
	}
	sels, _ = parseSel(t, "[x=AbC i]")
	if got := sels[0].compounds[0].attrs[0].val; got != "abc" {
		t.Fatalf("value = %q", got)
	}
	sels, _ = parseSel(t, "[x|=AbC i]")
	if a := sels[0].compounds[0].attrs[0]; a.dash != "abc-" {
		t.Fatalf("dash = %q", a.dash)
	}
}

// bigAttribute is a document whose only element has a 1 MiB attribute x.
func bigAttribute(tb testing.TB, filler string) *dom.Document {
	tb.Helper()
	return parseHTML(tb, "<p id=i1 x="+strings.Repeat(filler, (1<<20)/len(filler))+"></p>")
}

// elementWithID returns the index of the element with the given id.
func elementWithID(tb testing.TB, d *dom.Document, id string) int32 {
	tb.Helper()
	for i := range d.Elements {
		if d.Elements[i].ID == id {
			return clampIdx(i)
		}
	}
	tb.Fatalf("no element with id %q", id)
	return 0
}

// 1 MiB attribute tested against 5 000 selectors: the scan is charged before it
// happens, so the run ends with MaxWork exceeded or finishes; the time is
// measured by BenchmarkHostileAttribute.
func TestHugeAttributeIsChargedByItsSize(t *testing.T) {
	doc := bigAttribute(t, "a")
	p := elementWithID(t, doc, "i1")
	needle := strings.Repeat("a", 1000) + "b"
	for _, op := range []string{"=", "~=", "|=", "^=", "$=", "*="} {
		for _, flag := range []string{"", " i"} {
			t.Run(op+flag, func(t *testing.T) {
				sels, warn := parseSel(t, "p[x"+op+needle+flag+"]")
				if len(sels) != 1 {
					t.Fatal(warn)
				}
				b := budget.New(context.Background(), budget.Defaults(), nil)
				start := time.Now()
				matches := 0
				var err error
				for range 5000 {
					if _, err = sels[0].Match(doc, p, b); err != nil {
						break
					}
					matches++
				}
				var le *budget.LimitError
				if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
					t.Fatalf("err = %v", err)
				}
				// ~= and *= read the whole megabyte: every completed match paid for it.
				if scans := op == "~=" || op == "*="; scans {
					if min := int64(matches) * ((1 << 20) / 32); b.Used(budget.Work) < min {
						t.Fatalf("%d matches charged %d Work, want at least %d", matches, b.Used(budget.Work), min)
					}
				}
				t.Logf("%d matches, work %d, %v, ended by the limit: %v", matches, b.Used(budget.Work), time.Since(start).Round(time.Millisecond), err != nil)
			})
		}
	}
}

func BenchmarkHostileAttribute(b *testing.B) {
	doc := bigAttribute(b, "a")
	p := elementWithID(b, doc, "i1")
	needle := strings.Repeat("a", 1000) + "b"
	for _, op := range []string{"=", "~=", "|=", "^=", "$=", "*="} {
		for _, flag := range []string{"", " i"} {
			sels, _ := parseSel(b, "p[x"+op+needle+flag+"]")
			b.Run(op+flag, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					bud := budget.New(context.Background(), budget.Defaults(), nil)
					for range 5000 {
						if _, err := sels[0].Match(doc, p, bud); err != nil {
							break
						}
					}
				}
			})
		}
	}
}

func TestManyClassesAreChargedByTheirNumber(t *testing.T) {
	var cls strings.Builder
	for i := range 100_000 {
		fmt.Fprintf(&cls, "c%d ", i)
	}
	doc := parseHTML(t, "<p id=i1 class=\""+cls.String()+"\"></p>")
	// And 1 000 000 duplicates, which the DOM no longer produces.
	dup := &dom.Document{Elements: []dom.Element{{Parent: -1, Classes: make([]string, 1_000_000)}}}
	for i := range dup.Elements[0].Classes {
		dup.Elements[0].Classes[i] = "a"
	}
	for name, tt := range map[string]struct {
		doc *dom.Document
		sel string
	}{"100k classes": {doc, ".c99999.zz"}, "1M duplicates": {dup, ".a.zz"}} {
		t.Run(name, func(t *testing.T) {
			sels, _ := parseSel(t, tt.sel)
			b := budget.New(context.Background(), budget.Defaults(), nil)
			var err error
			matches := 0
			for range 5000 {
				if _, err = sels[0].Match(tt.doc, 0, b); err != nil {
					break
				}
				matches++
			}
			var le *budget.LimitError
			if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
				t.Fatalf("err = %v", err)
			}
			n := len(tt.doc.Elements[0].Classes)
			if min := int64(matches) * int64(n) * 8 / 64; b.Used(budget.Work) < min {
				t.Fatalf("%d matches over %d classes charged %d Work, want at least %d", matches, n, b.Used(budget.Work), min)
			}
		})
	}
}

// The index charges the lookups and the appends; Match charges the candidate:
// a candidate is paid once.
func TestCandidatesAreChargedBeforeTheyAreAppended(t *testing.T) {
	ix := &Index[int]{}
	for i := range 3 {
		sels, _ := parseSel(t, ".a")
		ix.Add(sels[0], i)
	}
	doc := &dom.Document{Elements: []dom.Element{{Parent: -1, Classes: []string{"a", "a"}}}}
	// Two class lookups, each workStep + 1 unit for its 3 entries; then the tag
	// and the universal list (workStep each, both empty).
	const exact = 2*(workStep+1) + 2*workStep
	run := func(limit int64) ([]Entry[int], error) {
		l := budget.Defaults()
		l[budget.Work] = limit
		return ix.Candidates(doc, 0, nil, budget.New(context.Background(), l, nil))
	}
	for _, tt := range []struct {
		limit int64
		ok    bool
	}{{exact + 1, true}, {exact, true}, {exact - 1, false}} {
		got, err := run(tt.limit)
		var le *budget.LimitError
		if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.Work)) || (tt.ok && len(got) != 6) {
			t.Errorf("limit %d: %d entries, err %v", tt.limit, len(got), err)
		}
	}
	// One element with N candidates: the index charges the lookups and appends,
	// then each Match its own workCandidate.
	b := budget.New(context.Background(), budget.Defaults(), nil)
	cand, err := ix.Candidates(doc, 0, nil, b)
	if err != nil || b.Used(budget.Work) != exact {
		t.Fatalf("Candidates charged %d, want %d (err %v)", b.Used(budget.Work), exact, err)
	}
	before := b.Used(budget.Work)
	for _, c := range cand {
		if _, err := c.Selector.Match(doc, 0, b); err != nil {
			t.Fatal(err)
		}
	}
	// Each Match: workCandidate plus the class test (1 unit for 1 class).
	if got, want := b.Used(budget.Work)-before, int64(len(cand))*(workCandidate+1); got != want {
		t.Fatalf("%d matches charged %d, want %d", len(cand), got, want)
	}
}

func TestMediaQueryListIsCapped(t *testing.T) {
	list := func(n int) string { return strings.TrimSuffix(strings.Repeat("print,", n), ",") }
	for _, tt := range []struct {
		n    int
		warn int
	}{{maxMediaQueries - 1, 0}, {maxMediaQueries, 0}, {maxMediaQueries + 1, 1}, {50_000, 1}} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		q := ParseMediaQuery(selectorTokens2(t, list(tt.n)), b)
		if len(q.alts) > maxMediaQueries || len(warnings(b)) != tt.warn {
			t.Errorf("%d queries: %d kept, warnings %v", tt.n, len(q.alts), warnings(b))
		}
	}
	// 100 000 empty queries cost one warning each... deduplicated, and only
	// the first 32 are even parsed.
	b := budget.New(context.Background(), budget.Defaults(), nil)
	ParseMediaQuery(selectorTokens2(t, strings.Repeat(",", 100_000)), b)
	if ws, _ := b.Warnings(); len(ws) > 3 {
		t.Fatalf("%d distinct warnings", len(ws))
	}
}

func TestDescribeTruncatesBeforeCopying(t *testing.T) {
	huge := strings.Repeat("a", 100_000)
	toks := selectorTokens(t, huge+"."+huge+"[x="+huge+"]")
	if got := describe(toks); len(got) > budget.MaxDetailBytes+budget.MaxDetailBytes {
		t.Fatalf("describe returned %d bytes", len(got))
	}
	if allocs := testing.AllocsPerRun(5, func() { describe(toks) }); allocs > 10 {
		t.Fatalf("%v allocations", allocs)
	}
}

func TestCommaRanges(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"a", "[a]"},
		{"a,b,c", "[a][b][c]"},
		{"a,", "[a][]"},
		{",a", "[][a]"},
		{",", "[][]"},
		{"f(a,b),c", "[f(a,b)][c]"},
		{"[a,b],c", "[[a,b]][c]"},
		{"a,,b", "[a][][b]"},
	}
	for _, tt := range tests {
		toks := valueTokens(t, tt.in)
		var parts []string
		for lo, hi := range commaRanges(toks) {
			parts = append(parts, "["+strings.ReplaceAll(dumpTokens(toks[lo:hi]), "_", " ")+"]")
		}
		if got := strings.Join(parts, ""); got != tt.want {
			t.Errorf("%q: %s, want %s", tt.in, got, tt.want)
		}
	}
	if n := 0; true {
		for range commaRanges(nil) {
			n++
		}
		if n != 1 {
			t.Errorf("no tokens: %d ranges, want 1", n)
		}
	}
}
