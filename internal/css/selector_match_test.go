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

const matchHTML = `<!doctype html><html><body>
<div id=i1 class="a b"><p id=i2 class=x></p><p id=i3 class="x y"></p><span id=i4></span><p id=i5 data-k=v lang=EN-us title="hello world"></p></div>
<div id=i6><p id=i7></p></div>
<section id=i8><div id=i9 class=a><p id=i10></p><b id=i11></b></div></section>
<custom-tag id=i12 class=x></custom-tag>
</body></html>`

func TestMatching(t *testing.T) {
	doc := parseHTML(t, matchHTML)
	tests := []struct {
		sel  string
		want []int
	}{
		{"p", []int{2, 3, 5, 7, 10}},
		{"P", []int{2, 3, 5, 7, 10}},
		{"custom-tag", []int{12}},
		{"CUSTOM-TAG", []int{12}},
		{"unknown", []int{}},
		{"#i3", []int{3}},
		{"#nope", []int{}},
		{".x", []int{2, 3, 12}},
		{".x.y", []int{3}},
		{"p.x", []int{2, 3}},
		{".a", []int{1, 9}},
		{".b.a", []int{1}},
		{"div p", []int{2, 3, 5, 7, 10}},
		{"section p", []int{10}},
		{"div > p", []int{2, 3, 5, 7, 10}},
		{"section > p", []int{}},
		{"section > div > p", []int{10}},
		{"section div p", []int{10}},
		{"body > div", []int{1, 6}},
		{".x + p", []int{3}},
		{".x + .x", []int{3}},
		{"p + span", []int{4}},
		{"span + p", []int{5}},
		{"p ~ span", []int{4}},
		{"p ~ p", []int{3, 5}},
		{"span ~ p", []int{5}},
		{"span ~ b", []int{}},
		{"p ~ b", []int{11}},
		{"div.a > p ~ b", []int{11}},
		{"[data-k]", []int{5}},
		{"[data-k=v]", []int{5}},
		{`[data-k="v"]`, []int{5}},
		{"[data-k=w]", []int{}},
		{"[DATA-K=v]", []int{5}},
		{"[class]", []int{1, 2, 3, 9, 12}},
		{"[class=x]", []int{2, 12}},
		{`[class="a b"]`, []int{1}},
		{"[id]", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{`[lang|=en]`, []int{}},
		{`[lang|=en i]`, []int{5}},
		{`[lang^=EN]`, []int{5}},
		{`[lang^=en]`, []int{}},
		{`[lang$=US]`, []int{}},
		{`[lang$=us]`, []int{5}},
		{`[title~=world]`, []int{5}},
		{`[title~=hello]`, []int{5}},
		{`[title~="hello world"]`, []int{}},
		{`[title*="lo wo"]`, []int{5}},
		{"[title^='']", []int{}},
		{":root", []int{0}},
		{"html", []int{0}},
		{"body", []int{0}},
		{"p:first-child", []int{2, 7, 10}},
		{"p:last-child", []int{5, 7}},
		{"div:first-child", []int{1, 9}},
		{"b:last-child", []int{11}},
		{"p:nth-child(2)", []int{3}},
		{"p:nth-child(odd)", []int{2, 7, 10}},
		{"div > :first-child", []int{2, 7, 10}},
		{"div > :last-child", []int{5, 7, 11}},
		{"a:hover", []int{}},
		{"p:hover", []int{}},
		{".x:focus", []int{}},
		{"p::before", []int{2, 3, 5, 7, 10}},
		{"#i2::after", []int{2}},
		{"*", nil},
	}
	for _, tt := range tests {
		t.Run(tt.sel, func(t *testing.T) {
			got := matchingIDs(t, doc, tt.sel)
			if tt.want == nil {
				if len(got) != len(doc.Elements) {
					t.Fatalf("* matched %d of %d elements", len(got), len(doc.Elements))
				}
				return
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchingBacktracks(t *testing.T) {
	// The nearest candidate for an ancestor is not always the right one when
	// other combinators are involved.
	doc := parseHTML(t, `<div id=i1 class=a><div id=i2 class=a><p id=i3></p></div><i id=i4></i></div>`+
		`<section id=i5 class=s><ul id=i6><li id=i7 class=k></li></ul></section><section id=i8 class=t><li id=i9 class=k></li></section>`+
		`<span id=i10 class=p></span><b id=i11 class=q><em id=i12></em></b>`)
	tests := []struct {
		sel  string
		want []int
	}{
		{".a > .a > p", []int{3}},
		{".a .a p", []int{3}},
		{".a > p", []int{3}},
		{".s li", []int{7}},
		{".s > li", []int{}},
		{".t > li", []int{9}},
		{".s ul > li", []int{7}},
		{".p + .q em", []int{12}},
		{".p ~ .q > em", []int{12}},
		{".a > i", []int{4}},
		{".a > .a ~ i", []int{4}},
		{".a .a ~ i", []int{4}},
		{".a > .a + i", []int{4}},
	}
	for _, tt := range tests {
		t.Run(tt.sel, func(t *testing.T) {
			if got := matchingIDs(t, doc, tt.sel); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchChargesWork(t *testing.T) {
	doc := parseHTML(t, matchHTML)
	sels, _ := parseSel(t, "section div p")
	b := budget.New(context.Background(), budget.Defaults(), nil)
	for e := range doc.Elements {
		if _, err := sels[0].Match(doc, int32(e), b); err != nil {
			t.Fatal(err)
		}
	}
	if used := b.Used(budget.Work); used < int64(len(doc.Elements)) {
		t.Fatalf("%d elements charged only %d Work", len(doc.Elements), used)
	}
}

func TestMatchWorkLimit(t *testing.T) {
	doc := parseHTML(t, matchHTML)
	sels, _ := parseSel(t, "p")
	const calls = 50
	const perCall = workCandidate // a single compound: no combinator step
	run := func(limit int64) error {
		l := budget.Defaults()
		l[budget.Work] = limit
		b := budget.New(context.Background(), l, nil)
		for range calls {
			if _, err := sels[0].Match(doc, 2, b); err != nil {
				return err
			}
		}
		return nil
	}
	if err := run(calls * perCall); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	var le *budget.LimitError
	if err := run(calls*perCall - 1); !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("over the limit: %v", err)
	}
	if err := run(calls*perCall + 1); err != nil {
		t.Fatalf("just under: %v", err)
	}
}

func TestMatchCancellation(t *testing.T) {
	doc := parseHTML(t, matchHTML)
	sels, _ := parseSel(t, "div p")
	ctx, cancel := context.WithCancel(context.Background())
	b := budget.New(ctx, budget.Defaults(), nil)
	cancel()
	var err error
	for range 3 * budget.WorkPollInterval {
		if _, err = sels[0].Match(doc, 2, b); err != nil {
			break
		}
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// deep builds a document of n nested divs.
func deep(n int) string {
	return strings.Repeat("<div class=d>", n) + "<p id=i1></p>" + strings.Repeat("</div>", n)
}

func TestBacktrackingIsBoundedByWork(t *testing.T) {
	// 400 nested divs (under the parser's depth bound): a selector whose
	// combinators force backtracking tries a number of paths that grows with
	// the depth to the power of its length. The Work budget ends it.
	doc := parseHTML(t, deep(400)+"<p id=i2></p>")
	sels, warn := parseSel(t, strings.Repeat(".d > .d ~ ", 8)+"p")
	if len(sels) != 1 {
		t.Fatal(warn)
	}
	b := budget.New(context.Background(), budget.Defaults(), nil)
	start := time.Now()
	var err error
	for e := range doc.Elements {
		if _, err = sels[0].Match(doc, int32(e), b); err != nil {
			break
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
	var le *budget.LimitError
	if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
		t.Fatalf("err = %v", err)
	}
}

func TestDescendantChainsAreLinear(t *testing.T) {
	// Pure descendant chains match greedily: no combinatorial search.
	doc := parseHTML(t, deep(400))
	sels, _ := parseSel(t, strings.Repeat(".d ", 31)+"p")
	b := budget.New(context.Background(), budget.Defaults(), nil)
	for e := range doc.Elements {
		if _, err := sels[0].Match(doc, int32(e), b); err != nil {
			t.Fatal(err)
		}
	}
	if used := b.Used(budget.Work); used > 40*int64(len(doc.Elements))*10 {
		t.Fatalf("descendant chain charged %d Work", used)
	}
}
