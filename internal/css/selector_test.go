package css

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// selectorTokens returns the prelude tokens of the rule "text{x:y}".
func selectorTokens(tb testing.TB, text string) []Token {
	tb.Helper()
	b := budget.New(context.Background(), budget.Defaults(), nil)
	ss, err := ParseStylesheet([]byte(text+"{x:y}"), OriginDocument, b)
	if err != nil || len(ss.Rules) != 1 {
		tb.Fatalf("%q: %d rules, err %v", text, len(ss.Rules), err)
	}
	return ss.Rules[0].Prelude
}

func selBudget(selectors int64) *budget.Budget {
	l := budget.Defaults()
	l[budget.Selectors] = selectors
	return budget.New(context.Background(), l, nil)
}

func parseSel(tb testing.TB, text string) ([]*Selector, []string) {
	tb.Helper()
	b := selBudget(1 << 20)
	sels, err := ParseSelectors(selectorTokens(tb, text), b)
	if err != nil {
		tb.Fatal(err)
	}
	return sels, warnings(b)
}

func TestParseSelectorsAndSpecificity(t *testing.T) {
	tests := []struct {
		in   string
		want string
		spec [3]int
	}{
		{"*", "*", [3]int{0, 0, 0}},
		{"p", "p", [3]int{0, 0, 1}},
		{"P", "p", [3]int{0, 0, 1}},
		{"custom-tag", "custom-tag", [3]int{0, 0, 1}},
		{"#a", "#a", [3]int{1, 0, 0}},
		{".a", ".a", [3]int{0, 1, 0}},
		{"p.a.b#c", "p#c.a.b", [3]int{1, 2, 1}},
		{"*.a", "*.a", [3]int{0, 1, 0}},
		{"[href]", "[href]", [3]int{0, 1, 0}},
		{"[ HREF = 'x' ]", `[href="x"]`, [3]int{0, 1, 0}},
		{"[a=b i]", `[a="b" i]`, [3]int{0, 1, 0}},
		{"[a^=b]", `[a^="b"]`, [3]int{0, 1, 0}},
		{"[a~=b]", `[a~="b"]`, [3]int{0, 1, 0}},
		{"[a|=b]", `[a|="b"]`, [3]int{0, 1, 0}},
		{"[a$=b]", `[a$="b"]`, [3]int{0, 1, 0}},
		{"[a*=b]", `[a*="b"]`, [3]int{0, 1, 0}},
		{":root", ":root", [3]int{0, 1, 0}},
		{"li:first-child", "li:first-child", [3]int{0, 1, 1}},
		{"li:LAST-child", "li:last-child", [3]int{0, 1, 1}},
		{"tr:nth-child(odd)", "tr:nth-child(2n+1)", [3]int{0, 1, 1}},
		{"a:hover", "a:never", [3]int{0, 1, 1}},
		{"a:focus-visible", "a:never", [3]int{0, 1, 1}},
		{"a b", "a b", [3]int{0, 0, 2}},
		{"a   >   b", "a > b", [3]int{0, 0, 2}},
		{"a>b+c~d e", "a > b + c ~ d e", [3]int{0, 0, 5}},
		{".a::before", ".a::before", [3]int{0, 1, 1}},
		{"p:after", "p::after", [3]int{0, 0, 2}},
		{"#a .b p:first-child::after", "#a .b p:first-child::after", [3]int{1, 2, 2}},
		{"  p  ", "p", [3]int{0, 0, 1}},
		{"#a#a", "#a", [3]int{2, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			sels, warn := parseSel(t, tt.in)
			if len(sels) != 1 || len(warn) != 0 {
				t.Fatalf("got %d selectors, warnings %v", len(sels), warn)
			}
			s := sels[0]
			if got := s.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := [3]int{s.Specificity().IDs(), s.Specificity().Classes(), s.Specificity().Types()}; got != tt.spec {
				t.Errorf("specificity = %v, want %v", got, tt.spec)
			}
		})
	}
}

func TestSpecificityOrder(t *testing.T) {
	order := []string{"*", "p", "p p", ".a", "[x]", "p.a", "p:first-child", ".a.b", "#a", "#a p", "#a.b", "#a#b"}
	var prev Specificity
	for i, text := range order {
		sels, _ := parseSel(t, text)
		sp := sels[0].Specificity()
		if i > 0 && sp < prev {
			t.Errorf("%q (%d) sorts below its predecessor (%d)", text, sp, prev)
		}
		prev = sp
	}
	// Fields saturate instead of carrying into the next one.
	sels, _ := parseSel(t, strings.Repeat(".a", 30)+" "+strings.Repeat(".a", 30))
	if sels[0].Specificity().Classes() != 60 || sels[0].Specificity().IDs() != 0 {
		t.Errorf("sixty classes: %d ids, %d classes", sels[0].Specificity().IDs(), sels[0].Specificity().Classes())
	}
	var s Specificity
	for range 2000 {
		s = s.add(1)
	}
	if s.Classes() != 1023 || s.IDs() != 0 || s.Types() != 0 {
		t.Errorf("saturated specificity = %d/%d/%d", s.IDs(), s.Classes(), s.Types())
	}
}

func TestSelectorList(t *testing.T) {
	sels, warn := parseSel(t, "h1, .a > b ,  #c")
	if len(sels) != 3 || len(warn) != 0 || sels[1].String() != ".a > b" {
		t.Fatalf("got %d selectors %v", len(sels), warn)
	}
}

func TestUnsupportedSelectorsDropTheWholeList(t *testing.T) {
	for _, in := range []string{
		"", " ", ",", "a,", ",a", "a,,b", "a, ", "> a", "a >", "a > > b", "a +", "~ a", "a ,, b",
		"a:not(b)", "a:is(b)", "a:first-of-type", "a:nth-child()", "a:nth-child(foo)", "a:nth-child(2n+)", "a:nth-child(2 n)",
		"a:nth-child(1.5)", "a:nth-child(99999999999)", "a:nth-last-child(1)", "a:lang(en)", ":", "a:", "a::", "a::first-line", "a::marker",
		"a::before::after", "a::before b", "a::before:hover", "a:hover::before b", "#123", "#", ".", ". a", "a.", "a|b", "*|a", "ns|*",
		"a[b!=c]", "a[b=]", "a[=b]", "a[b=c d]", "a[b=c z]", "a[]", "a[b=1]", "a;b", "a!b", "a%b", "a&b",
		"a b c d e f g h i j k l m n o p q r s t u v w x y z a1 b1 c1 d1 e1 f1 g1 h1",
		"p a:hover:nonsense", "a b > :foo", "a, b:unknown", "good, bad::before x",
	} {
		t.Run(in, func(t *testing.T) {
			sels, warn := parseSel(t, in)
			if sels != nil || len(warn) != 1 || !strings.HasPrefix(warn[0], "unsupported-selector:") {
				t.Fatalf("got %v, warnings %v", sels, warn)
			}
		})
	}
}

func TestNthChildArguments(t *testing.T) {
	tests := []struct {
		in   string
		a, b int
	}{
		{"odd", 2, 1},
		{"even", 2, 0},
		{"ODD", 2, 1},
		{"3", 0, 3},
		{"0", 0, 0},
		{"-2", 0, -2},
		{"n", 1, 0},
		{"2n", 2, 0},
		{"2n+1", 2, 1},
		{"2n-1", 2, -1},
		{"2n + 1", 2, 1},
		{"2n - 1", 2, -1},
		{"2n +1", 2, 1},
		{"2n -1", 2, -1},
		{"-n+3", -1, 3},
		{"-n + 3", -1, 3},
		{"+n", 1, 0},
		{"-n", -1, 0},
		{"n+3", 1, 3},
		{"n-3", 1, -3},
		{"+n+3", 1, 3},
		{"+n-3", 1, -3},
		{"-n-3", -1, -3},
		{"3n", 3, 0},
		{"  2n+1  ", 2, 1},
		{"10n+10", 10, 10},
		{"N+2", 1, 2},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			sels, warn := parseSel(t, "x:nth-child("+tt.in+")")
			if len(sels) != 1 {
				t.Fatalf("rejected: %v", warn)
			}
			p := sels[0].compounds[0].pseudos[0]
			if p.kind != pseudoNth || p.a != tt.a || p.b != tt.b {
				t.Fatalf("a=%d b=%d, want %d %d", p.a, p.b, tt.a, tt.b)
			}
		})
	}
}

func TestNthChildMatching(t *testing.T) {
	var li strings.Builder
	for i := range 10 {
		fmt.Fprintf(&li, "<li id=i%d></li>", i+1)
	}
	doc := parseHTML(t, "<ul>"+li.String()+"</ul>")
	tests := map[string][]int{
		"odd": {1, 3, 5, 7, 9}, "even": {2, 4, 6, 8, 10}, "3": {3}, "2n+3": {3, 5, 7, 9}, "-n+3": {1, 2, 3},
		"n+8": {8, 9, 10}, "3n": {3, 6, 9}, "3n-1": {2, 5, 8}, "-2n+7": {1, 3, 5, 7}, "0n+4": {4}, "n": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		"0": {}, "-3": {}, "-n": {}, "11": {}, "10n": {10},
	}
	for arg, want := range tests {
		t.Run(arg, func(t *testing.T) {
			got := matchingIDs(t, doc, "li:nth-child("+arg+")")
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestSelectorLimits(t *testing.T) {
	compounds := func(n int) string { return strings.TrimSpace(strings.Repeat("a ", n)) }
	simples := func(n int) string { return "a" + strings.Repeat(".c", n-1) }
	for _, tt := range []struct {
		name string
		text string
		ok   bool
	}{
		{"compounds just under", compounds(MaxCompounds - 1), true},
		{"compounds at the limit", compounds(MaxCompounds), true},
		{"compounds over", compounds(MaxCompounds + 1), false},
		{"simple selectors just under", simples(MaxSimple - 1), true},
		{"simple selectors at the limit", simples(MaxSimple), true},
		{"simple selectors over", simples(MaxSimple + 1), false},
		{"child combinators at the limit", strings.TrimSuffix(strings.Repeat("a>", MaxCompounds), ">") + ">a", false},
		{"all limits at once", strings.TrimSpace(strings.Repeat(simples(MaxSimple)+" ", MaxCompounds)), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sels, warn := parseSel(t, tt.text)
			if tt.ok != (len(sels) == 1) || tt.ok != (len(warn) == 0) {
				t.Fatalf("selectors %d, warnings %v", len(sels), warn)
			}
		})
	}
	// A hostile selector is refused after 32 compounds, not after reading it all.
	for _, text := range []string{strings.Repeat("a ", 10_000), strings.Repeat("a>", 10_000) + "a", "a" + strings.Repeat(".c", 10_000)} {
		sels, warn := parseSel(t, text)
		if sels != nil || len(warn) != 1 {
			t.Fatalf("hostile selector: %v %v", sels, warn)
		}
	}
}

func TestSelectorsBudget(t *testing.T) {
	list := func(n int) []Token { return selectorTokens(t, strings.TrimSuffix(strings.Repeat("a,", n), ",")) }
	for _, tt := range []struct {
		name  string
		count int
		limit int64
		ok    bool
	}{{"just under", 4, 5, true}, {"at the limit", 5, 5, true}, {"over", 6, 5, false}} {
		t.Run(tt.name, func(t *testing.T) {
			sels, err := ParseSelectors(list(tt.count), selBudget(tt.limit))
			var le *budget.LimitError
			if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.Selectors)) || (tt.ok && len(sels) != tt.count) {
				t.Fatalf("selectors %d, err %v", len(sels), err)
			}
		})
	}
	// Every member counts, even in a list that is dropped, and across rules.
	b := selBudget(5)
	if _, err := ParseSelectors(selectorTokens(t, "a, b:bogus, c"), b); err != nil || b.Used(budget.Selectors) != 3 {
		t.Fatalf("dropped list: err %v, used %d", err, b.Used(budget.Selectors))
	}
	if _, err := ParseSelectors(selectorTokens(t, "a, b, c"), b); err == nil {
		t.Fatal("budget shared by two rules was not exhausted")
	}
}

func TestHostileSelectorList512KiB(t *testing.T) {
	src := strings.Repeat("a,", (512<<10)/2)
	b := budget.New(context.Background(), budget.Defaults(), nil)
	ds, err := ParseDeclarations([]byte("x:"+src[:len(src)-1]), b)
	if err != nil {
		t.Skipf("the CSS size limit comes first: %v", err)
	}
	_, err = ParseSelectors(ds[0].Value, b)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Selectors || le.Max != 5000 {
		t.Fatalf("err = %v", err)
	}
	if got := b.Used(budget.Selectors); got != 5001 {
		t.Errorf("stopped after %d selectors, want 5001", got)
	}
}

func TestKey(t *testing.T) {
	tests := []struct {
		in   string
		kind KeyKind
		key  string
	}{
		{"#a", KeyID, "a"},
		{"p#a.c", KeyID, "a"},
		{".a.b", KeyClass, "a"},
		{"p.b", KeyClass, "b"},
		{"p", KeyTag, "p"},
		{"a b > P", KeyTag, "p"},
		{"*", KeyUniversal, ""},
		{"[x]", KeyUniversal, ""},
		{":first-child", KeyUniversal, ""},
		{"x y .k", KeyClass, "k"},
		{"custom-tag", KeyTag, "custom-tag"},
	}
	for _, tt := range tests {
		sels, _ := parseSel(t, tt.in)
		if kind, key := sels[0].Key(); kind != tt.kind || key != tt.key {
			t.Errorf("%q: key %d %q, want %d %q", tt.in, kind, key, tt.kind, tt.key)
		}
	}
}

func TestPseudoElementField(t *testing.T) {
	for in, want := range map[string]PseudoElement{"a": PseudoNone, "a::before": PseudoBefore, "a:after": PseudoAfter} {
		sels, _ := parseSel(t, in)
		if sels[0].Pseudo() != want {
			t.Errorf("%q: pseudo %d, want %d", in, sels[0].Pseudo(), want)
		}
	}
}

func TestNeverMatches(t *testing.T) {
	for in, want := range map[string]bool{"a:hover": true, "a:focus b": true, "a b:active": true, "a:first-child": false, "a": false, "#x#y": true} {
		sels, _ := parseSel(t, in)
		if sels[0].NeverMatches() != want {
			t.Errorf("%q: NeverMatches = %v", in, !want)
		}
	}
}

func parseHTML(tb testing.TB, html string) *dom.Document {
	tb.Helper()
	l := budget.Defaults()
	l[budget.Nodes] = 1 << 22
	l[budget.InputBytes] = 1 << 26
	d, err := dom.Parse(strings.NewReader(html), budget.New(context.Background(), l, nil))
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

// matchingIDs returns the numeric suffix of the ids ("i3" gives 3, no id gives
// 0) of the elements that match text.
func matchingIDs(tb testing.TB, d *dom.Document, text string) []int {
	tb.Helper()
	sels, warn := parseSel(tb, text)
	if len(sels) != 1 {
		tb.Fatalf("%q rejected: %v", text, warn)
	}
	b := budget.New(context.Background(), budget.Defaults(), nil)
	out := []int{}
	for e := range d.Elements {
		ok, err := sels[0].Match(d, int32(e), b)
		if err != nil {
			tb.Fatal(err)
		}
		if ok {
			n := 0 // elements without an id count as 0
			if id := d.Elements[e].ID; id != "" {
				if _, err := fmt.Sscanf(id, "i%d", &n); err != nil {
					tb.Fatalf("matched element %q without a numeric id", id)
				}
			}
			out = append(out, n)
		}
	}
	return out
}

func TestUnclosedBlocksInSelectors(t *testing.T) {
	for _, text := range []string{"a[b", "a b[c", "a:nth-child(2n+1", "a[b=c"} {
		ds, err := ParseDeclarations([]byte("x:"+text), budget.New(context.Background(), budget.Defaults(), nil))
		if err != nil || len(ds) != 1 {
			t.Fatal(text, err)
		}
		b := selBudget(100)
		sels, err := ParseSelectors(ds[0].Value, b)
		if err != nil || sels != nil || len(warnings(b)) != 1 {
			t.Errorf("%q: %v %v %v", text, sels, err, warnings(b))
		}
	}
}
