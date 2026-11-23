package css

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// checkTokens verifies the structural invariants of a token slice.
func checkTokens(tb testing.TB, toks []Token, what string) {
	tb.Helper()
	if len(toks) > 0 && (toks[0].Type == TWhitespace || toks[len(toks)-1].Type == TWhitespace) {
		tb.Fatalf("%s: untrimmed whitespace", what)
	}
	if cap(toks) != len(toks) {
		tb.Fatalf("%s: slice can grow into its neighbours", what)
	}
	var stack []int
	for i, t := range toks {
		if t.Type < TIdent || t.Type > TRBrace {
			tb.Fatalf("%s: token %d has type %d", what, i, t.Type)
		}
		if !utf8.ValidString(t.Text) || needsClean(t.Text) {
			tb.Fatalf("%s: token %d has unsafe text %q", what, i, t.Text)
		}
		if t.Num != t.Num || math.IsInf(float64(t.Num), 0) {
			tb.Fatalf("%s: token %d has number %v", what, i, t.Num)
		}
		if !isOpener(t.Type) {
			if t.End != 0 {
				tb.Fatalf("%s: non-opening token %d has End %d", what, i, t.End)
			}
		} else {
			end := i + int(t.End)
			closed := end <= len(toks) && toks[end-1].Type == closerOf(t.Type)
			if t.End < 1 || end > len(toks) || (!closed && end != len(toks)) {
				tb.Fatalf("%s: opening token %d has bad End %d (len %d)", what, i, t.End, len(toks))
			}
			stack = append(stack, i)
			if len(stack) > MaxNesting {
				tb.Fatalf("%s: nesting %d exceeds the limit", what, len(stack))
			}
		}
		if isCloser(t.Type) && len(stack) > 0 && closerOf(toks[stack[len(stack)-1]].Type) == t.Type {
			stack = stack[:len(stack)-1]
		}
	}
}

func checkRules(tb testing.TB, rules []Rule, mediaDepth int) {
	tb.Helper()
	for _, r := range rules {
		if r.Kind < RuleStyle || r.Kind > RuleMarginBox {
			tb.Fatalf("rule kind %d", r.Kind)
		}
		checkTokens(tb, r.Prelude, "prelude")
		if r.Kind == RuleMedia {
			if mediaDepth+1 > MaxNesting {
				tb.Fatalf("@media nesting exceeds the limit")
			}
			checkRules(tb, r.Rules, mediaDepth+1)
		} else {
			checkRules(tb, r.Rules, mediaDepth)
		}
		checkDecls(tb, r.Decls)
		if r.Kind == RuleStyle && len(r.Decls) == 0 {
			tb.Fatalf("empty style rule kept")
		}
	}
}

func checkDecls(tb testing.TB, ds []Declaration) {
	tb.Helper()
	for _, d := range ds {
		custom := strings.HasPrefix(d.Name, "--")
		if d.Name == "" || (!custom && d.Name != lowerASCII(d.Name)) {
			tb.Fatalf("bad declaration name %q", d.Name)
		}
		if needsClean(d.Name) {
			tb.Fatalf("unsafe declaration name %q", d.Name)
		}
		checkTokens(tb, d.Value, "value of "+d.Name)
		if len(d.Value) == 0 && !custom {
			tb.Fatalf("%s: empty value kept", d.Name)
		}
		for _, t := range d.Value {
			if t.Type == TBadString || t.Type == TBadURL || (!custom && t.Type == TLBrace) {
				tb.Fatalf("%s: invalid token kept", d.Name)
			}
		}
	}
}

var fuzzSeeds = []string{
	"", "a{color:red}", "a{color:red;;margin:0 auto!important}", "@media print{a{b:c}}", "@page :first{margin:1in;@bottom-left{content:\"x\"}}",
	"@import url(x);@font-face{src:url(a)}a{b:c}", "a{--x:{y}[z];w:calc(1px + (2*3))}", "a{content:\"\\202E\\0 \\110000\"}",
	"/* c */<!-- a{b:c} -->", "a{b:\"unterminated", "a{b:url(", "a{b:c", "}}}{{{", "a{b:1e999 -1e-999 .5.5}",
	"@charset \"x\";a{b:c}", "a,b>c+d~e[f=\"g\"]:h::i{j:k}", "\xff\xfe\x00{b:c}", strings.Repeat("(", 70) + strings.Repeat(")", 70),
	"a{b:" + strings.Repeat("f(", 70) + "}", strings.Repeat("@media a{", 70), "a{b:c;d}e{f:g}",
}

func FuzzParseStylesheet(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		b := newBudget(1 << 30)
		ss, err := ParseStylesheet([]byte(src), OriginDocument, b)
		if err != nil {
			t.Fatal(err)
		}
		checkRules(t, ss.Rules, 0)
		ds, err := ParseDeclarations([]byte(src), b)
		if err != nil {
			t.Fatal(err)
		}
		checkDecls(t, ds)
		if ws, dropped := b.Warnings(); len(ws) > 256 || dropped < 0 {
			t.Fatalf("%d warnings", len(ws))
		}
	})
}
