package css

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// A "}" ends the item being collected inside a block: the item is dropped and
// the block goes on (CSS Syntax §5).
func TestCloseBraceEndsItemsInsideBlocks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		warn []string
	}{
		{"at-rule in a style rule", "p{@foo} q{color:red}", "q{color:red}", []string{"unsupported-at-rule:@foo"}},
		{"at-rule in a page rule", "@page{@bottom-left} q{color:red}", "@page {} q{color:red}", []string{"unsupported-at-rule:@bottom-left"}},
		{"at-rule in a media rule", "@media screen{ @foo } q{color:red}", "@media screen{} q{color:red}", []string{"unsupported-at-rule:@foo"}},
		{
			"selector without block in a media rule", "@media screen{ a } q{color:red} p{color:blue}",
			"@media screen{} q{color:red} p{color:blue}",
			[]string{"invalid-value:rule without a block"},
		},
		{"at-rule with a prelude in a media rule", "@media screen{ @foo bar } q{top:0}", "@media screen{} q{top:0}", []string{"unsupported-at-rule:@foo"}},
		{"valid rule before the closing brace", "@media screen{ a{top:1} b } q{top:0}", "@media screen{a{top:1}} q{top:0}", []string{"invalid-value:rule without a block"}},
		{"nested at-rule with a block still skipped", "p{@foo{x{y}} top:0} q{top:1}", "p{top:0} q{top:1}", []string{"unsupported-at-rule:@foo"}},
		{"page margin box without a block", "@page{@bottom-left;margin:0}", "@page {margin:0}", []string{"unsupported-at-rule:@bottom-left"}},
		{"top level keeps joining the prelude", "a } b{top:0}", "a_}_b{top:0}", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warn := parseSheet(t, tt.src)
			if got != tt.want || strings.Join(warn, "|") != strings.Join(tt.warn, "|") {
				t.Fatalf("got  %q %q\nwant %q %q", got, warn, tt.want, tt.warn)
			}
		})
	}
}

func TestHashFlag(t *testing.T) {
	ds, err := ParseDeclarations([]byte("a: #fff #123 #-x #1a #_"), newBudget(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var got []bool
	for _, tok := range ds[0].Value {
		if tok.Type == THash {
			got = append(got, tok.Flags&FlagID != 0)
			if tok.Unit != UnitNone {
				t.Errorf("hash token has Unit %d", tok.Unit)
			}
		}
	}
	want := []bool{true, false, true, false, true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("flags %v, want %v", got, want)
		}
	}
}

func TestEscapedUnit(t *testing.T) {
	ds, err := ParseDeclarations([]byte(`a: 5p\78  6\50\58`), newBudget(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var units []Unit
	for _, tok := range ds[0].Value {
		if tok.Type == TDimension {
			units = append(units, tok.Unit)
		}
	}
	if len(units) != 2 || units[0] != UnitPx || units[1] != UnitPx {
		t.Fatalf("units = %v", units)
	}
}

func TestResultsCannotBeAppendedInPlace(t *testing.T) {
	ss, err := ParseStylesheet([]byte("a{x:1;y:2} @media print{b{x:1}} @page{margin:0;@top-left{x:y}}"), OriginDocument, newBudget(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(rs []Rule)
	walk = func(rs []Rule) {
		if cap(rs) != len(rs) {
			t.Errorf("rules: cap %d, len %d", cap(rs), len(rs))
		}
		for _, r := range rs {
			if cap(r.Decls) != len(r.Decls) {
				t.Errorf("decls: cap %d, len %d", cap(r.Decls), len(r.Decls))
			}
			walk(r.Rules)
		}
	}
	walk(ss.Rules)
}

func TestSourceSizeLimit(t *testing.T) {
	// The int32 token indexes bound the source size: the limit is reached by a
	// budget error, never by an overflow. A slice that big is not allocated, so
	// only the error path is checked.
	l := budget.Defaults()
	l[budget.CSSBytes] = math.MaxInt64
	b := budget.New(context.Background(), l, nil)
	huge := make([]byte, 0)
	if _, err := newParser(huge, b); err != nil {
		t.Fatalf("empty source: %v", err)
	}
}

// tokenSheet is a source of n lexer tokens: "a" then "," pairs.
func tokenSheet(n int) []byte {
	return []byte(strings.Repeat("a,", n/2) + strings.Repeat("a", n%2))
}

func workBudget(max int64) *budget.Budget {
	l := budget.Defaults()
	l[budget.Work] = max
	return budget.New(context.Background(), l, nil)
}

func TestWorkChargedPerToken(t *testing.T) {
	for _, n := range []int{0, 1, 255, 256, 257, 1000, 4096} {
		b := workBudget(math.MaxInt64)
		if _, err := ParseStylesheet(tokenSheet(n), OriginDocument, b); err != nil {
			t.Fatal(err)
		}
		if got := b.Used(budget.Work); got != int64(n) {
			t.Errorf("%d tokens charged %d Work", n, got)
		}
	}
	// Comments are lexer tokens too.
	b := workBudget(math.MaxInt64)
	if _, err := ParseStylesheet([]byte("a /* c */ b"), OriginDocument, b); err != nil || b.Used(budget.Work) != 5 {
		t.Fatalf("comment: %v, %d", err, b.Used(budget.Work))
	}
}

func TestWorkLimit(t *testing.T) {
	const n = 1000
	for _, tt := range []struct {
		name  string
		limit int64
		ok    bool
	}{{"just under", n + 1, true}, {"at the limit", n, true}, {"over", n - 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(tokenSheet(n), OriginDocument, workBudget(tt.limit))
			var le *budget.LimitError
			if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.Work || ss != nil)) {
				t.Fatalf("err = %v", err)
			}
			_, err = ParseDeclarations([]byte("a:"+strings.Repeat("b ", n/2-1)), workBudget(tt.limit))
			if tt.ok != (err == nil) {
				t.Fatalf("ParseDeclarations err = %v", err)
			}
		})
	}
}

// cancelAfter is a context that reports Canceled from its n-th Err call on.
type cancelAfter struct {
	context.Context
	left  int
	calls int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.left {
		return context.Canceled
	}
	return nil
}

func TestCancellationMidSheet(t *testing.T) {
	src := tokenSheet(512 << 10)
	ctx := &cancelAfter{Context: context.Background(), left: 3}
	b := budget.New(ctx, budget.Defaults(), nil)
	start := time.Now()
	_, err := ParseStylesheet(src, OriginDocument, b)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	// Err is polled once at the start and once per 4096 Work units: the parse
	// must stop right after the third poll, not run to the end.
	if ctx.calls != 4 || b.Used(budget.Work) > 5*4096 {
		t.Fatalf("polls = %d, work = %d", ctx.calls, b.Used(budget.Work))
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("cancellation took %v", d)
	}
	if _, err := ParseDeclarations(src, budget.New(&cancelAfter{Context: context.Background(), left: 2}, budget.Defaults(), nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseDeclarations err = %v", err)
	}
}

func TestKeywordsAreASCIIOnly(t *testing.T) {
	// U+212A KELVIN SIGN lowercases to "k" under Unicode rules.
	got, _ := parseSheet(t, "a{BLACK:1; COLOR:Red; x:1 !IMPORTANT; y:1 !imporKtant; w:5PX; v:5K}")
	// Nothing is folded: the odd names and units stay what they are, and only
	// the real keywords match.
	want := "a{blac\u212a:1;color:Red;x:1!;y:1_!impor\u212atant;w:5u1;v:5\u212a}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := lowerASCII("AbCK"); got != "abcK" {
		t.Fatalf("lowerASCII = %q", got)
	}
	if got := lowerASCII("already"); got != "already" {
		t.Fatalf("lowerASCII changed %q", got)
	}
}
