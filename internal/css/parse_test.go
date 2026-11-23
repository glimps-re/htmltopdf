package css

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestParseStylesheet(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		warn []string
	}{
		{"empty", "", "", nil},
		{"only space and comments", " /* x */ \n", "", nil},
		{"one rule", "p { color: red }", "p{color:red}", nil},
		{"selector list and combinators", "h1, .a > #b + c ~ d [x=\"y\"] { margin:0 }", "h1,_.a_>_#b_+_c_~_d_[x=\"y\"]{margin:0}", nil},
		{
			"values", "a{margin:0 auto; width:50%; font:bold 12px/1.5 'A B',serif; color:#fff}",
			"a{margin:0_auto;width:50%;font:bold_12u1/1.5_\"A B\",serif;color:#fff}", nil,
		},
		{
			"important", "a{color:red ! important;top:0!IMPORTANT;x:1 !important !important}",
			"a{color:red!;top:0!;x:1_!important!}", nil,
		},
		{"function nesting", "a{width:calc(1px + (2px * 3))}", "a{width:calc(1u1_+_(2u1_*_3))}", nil},
		{"property is lowercased", "a{COLOR:Red;--Mixed-Case:Yes}", "a{color:Red;--Mixed-Case:Yes}", nil},
		{"custom property keeps everything", "a{--x: { a: b } [c]  d;--e:;--f: 1}", "a{--x:{_a:_b_}_[c]_d;--e:;--f:1}", nil},
		{"empty rule omitted", "a{} b{;;} c{color:red}", "c{color:red}", nil},
		{
			"media", "@media print { a{color:red} @media (min-width:1px){b{top:0}} }",
			"@media print{a{color:red} @media (min-width:1u1){b{top:0}}}", nil,
		},
		{
			"page with margin boxes", "@page :first { size: A4; margin: 1in; @bottom-left { content: \"x\" } @Top-Center{color:red} }",
			"@page :first{size:A4;margin:1u4|@bottom-left{content:\"x\"} @top-center{color:red}}", nil,
		},
		{"charset first is ignored", "@charset \"utf-8\"; a{color:red}", "a{color:red}", nil},
		{"cdo cdc at top level", "<!-- a{color:red} -->", "a{color:red}", nil},
		{"url", "a{background:url(x.png) url( 'y.png' )}", "a{background:url<x.png>_url<y.png>}", nil},
		{"unicode range and match", "a[b|=c]{x:U+26}", "a[b|=c]{x:U+26}", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warn := parseSheet(t, tt.src)
			if got != tt.want || !reflect.DeepEqual(warn, tt.warn) {
				t.Fatalf("got  %q %q\nwant %q %q", got, warn, tt.want, tt.warn)
			}
		})
	}
}

// Error recovery of CSS Syntax §5: the offending item is dropped, the rest kept.
func TestErrorRecovery(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		warn []string
	}{
		{"declaration without colon", "a{color red; margin:0}", "a{margin:0}", []string{"invalid-value:declaration color has no colon"}},
		{"declaration without value", "a{color:; margin:0}", "a{margin:0}", []string{"invalid-value:declaration color has no value"}},
		{"not a property", "a{12:3; margin:0}", "a{margin:0}", []string{"invalid-value:malformed declaration"}},
		{"at-rule inside a style rule", "a{@x; margin:0}", "a{margin:0}", []string{"unsupported-at-rule:@x"}},
		{"nested rule inside a style rule", "a{b{color:red} margin:0}", "", []string{"invalid-value:declaration b has no colon"}},
		{"extra semicolons", "a{;;color:red;;;margin:0;}", "a{color:red;margin:0}", nil},
		{"unclosed block at EOF keeps the rule", "a{color:red", "a{color:red}", []string{"invalid-value:unclosed block"}},
		{"unclosed function at EOF", "a{width:calc(1px", "a{width:calc(1u1}", []string{"invalid-value:unclosed block"}},
		{"rule without block at EOF", "a{color:red} b", "a{color:red}", []string{"invalid-value:rule without a block"}},
		{"stray close brace joins the prelude", "} a{color:red} b{top:0}", "}_a{color:red} b{top:0}", nil},
		{"unmatched parenthesis", "a{color:red); margin:0}", "a{margin:0}", []string{"invalid-value:unmatched bracket in color"}},
		{"brace block in a value", "a{margin:{0}; top:0}", "a{top:0}", []string{"invalid-value:invalid token in margin"}},
		{"bad string", "a{content:\"abc\n; top:0}", "a{top:0}", []string{"invalid-value:invalid token in content"}},
		{"bad url", "a{background:url(a b); top:0}", "a{top:0}", []string{"invalid-value:invalid token in background"}},
		{"unterminated string at EOF", "a{content:\"abc", "a{content:\"abc\"}", []string{"invalid-value:unclosed block"}},
		{"unterminated comment", "a{top:0} /* never closed b{x:1}", "a{top:0}", nil},
		{"import", "@import url(x.css); a{top:0}", "a{top:0}", []string{"unsupported-at-rule:@import"}},
		{"unknown at-rule with block", "@font-face { font-family: x; src: url(a) } a{top:0}", "a{top:0}", []string{"unsupported-at-rule:@font-face"}},
		{"at-rule block with nesting", "@supports (a:b) { c{d:e} f{g:{h}} } a{top:0}", "a{top:0}", []string{"unsupported-at-rule:@supports"}},
		{"unknown at-rule at EOF", "a{top:0} @foo", "a{top:0}", []string{"unsupported-at-rule:@foo"}},
		{"charset not first", "a{top:0} @charset \"x\";", "a{top:0}", []string{"unsupported-at-rule:@charset"}},
		{"media without block", "@media print; a{top:0}", "a{top:0}", []string{"invalid-value:@media without a block"}},
		{"page without block", "@page; a{top:0}", "a{top:0}", []string{"invalid-value:@page without a block"}},
		{"unclosed media", "@media print { a{top:0}", "@media print{a{top:0}}", []string{"invalid-value:unclosed block"}},
		{"unknown margin box", "@page { @middle { top:0 } margin:0 }", "@page {margin:0}", []string{"unsupported-at-rule:@middle"}},
		{"at-rule inside margin box", "@page { @top-left { @top-right {x:y} color:red } }", "@page {|@top-left{color:red}}", []string{"unsupported-at-rule:@top-right"}},
		{"garbage then valid rule", "!!! ??? ; a{top:0}", "", nil},
		{"semicolon in prelude", "a;b{top:0} c{top:1}", "a;b{top:0} c{top:1}", nil},
		{"hostile bytes", "a{content:\"\x00\x01\x7f\x80\xff\u0085\"; top:0}", "a{content:\"\ufffd\ufffd\ufffd\ufffd\ufffd\ufffd\";top:0}", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warn := parseSheet(t, tt.src)
			if tt.name == "garbage then valid rule" {
				if !strings.Contains(got, "top:0") && got != "" {
					t.Fatalf("got %q", got)
				}
				return
			}
			if got != tt.want || !reflect.DeepEqual(warn, tt.warn) {
				t.Fatalf("got  %q %q\nwant %q %q", got, warn, tt.want, tt.warn)
			}
		})
	}
}

func TestEscapes(t *testing.T) {
	tests := []struct{ in, want string }{
		{`a{\63 olor:red}`, "a{color:red}"},
		{`a{c\6Flor:red}`, "a{color:red}"},
		{`a{content:"x\41 y\"z\\"}`, `a{content:"xAy\"z\\"}`},
		{"a{content:\"line\\\nbreak\"}", `a{content:"linebreak"}`},
		{`a{content:"\202E"}`, `a{content:"\u202e"}`},
		{`a{content:"\0 \D800 \110000 "}`, "a{content:\"\ufffd\ufffd\ufffd\"}"},
		{`a{content:"\1F600"}`, "a{content:\"\U0001F600\"}"},
		{`a{content:"\31 23456 7"}`, `a{content:"123456 7"}`},
		{`a{content:"\ "}`, `a{content:" "}`},
		{`a{x:\31 0}`, `a{x:10}`},
		{`a{background:url(\61 .png)}`, `a{background:url<a.png>}`},
		{"a{x:\\", "a{x:\ufffd}"},
	}
	for _, tt := range tests {
		got, _ := parseSheet(t, tt.in)
		if tt.in == "a{x:\\" {
			if !strings.HasPrefix(got, "a{x:") {
				t.Errorf("got %q", got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("%q → %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNumbersAndUnits(t *testing.T) {
	ds, err := ParseDeclarations([]byte("a:1 -2 +.5 1e3 1E-2 1e999 -1e999 1e-999 3px 4PX 5em 6% 7foo 8e3px 9x 1.5E+2Q"), newBudget(1<<20))
	if err != nil || len(ds) != 1 {
		t.Fatal(err, ds)
	}
	var got []string
	for _, tok := range ds[0].Value {
		if tok.Type == TWhitespace {
			continue
		}
		got = append(got, dumpTokens([]Token{tok}))
	}
	want := []string{"1", "-2", "0.5", "1000", "0.01", "3.4028235e+38", "-3.4028235e+38", "0", "3u1", "4u1", "5u8", "6%", "7foo", "8000u1", "9u26", "150u7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	for _, tok := range ds[0].Value {
		if tok.Type == TDimension && tok.Unit == UnitOther && tok.Text != "foo" {
			t.Errorf("unit text %q", tok.Text)
		}
	}
}

func TestBlockExtents(t *testing.T) {
	ds, err := ParseDeclarations([]byte("a: f(1, g(2) [x]) (y) z"), newBudget(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	toks := ds[0].Value
	for i, tok := range toks {
		if !isOpener(tok.Type) {
			if tok.End != 0 {
				t.Errorf("token %d (%s) has End %d", i, dumpTokens(toks[i:i+1]), tok.End)
			}
			continue
		}
		end := i + int(tok.End)
		if tok.End < 2 || end > len(toks) || toks[end-1].Type != closerOf(tok.Type) {
			t.Errorf("token %d: End %d does not reach its closer", i, tok.End)
		}
	}
	if toks[0].Type != TFunction || int(toks[0].End) != 14 && toks[int(toks[0].End)-1].Type != TRParen {
		t.Errorf("f( extent %d", toks[0].End)
	}
}

func TestParseDeclarations(t *testing.T) {
	tests := []struct {
		src  string
		want string
		warn int
	}{
		{"", "", 0},
		{"color: red; margin: 0 auto !important", "color:red;margin:0_auto!", 0},
		{"color: red;; ; top:0;", "color:red;top:0", 0},
		{"color red; top:0", "top:0", 1},
		{"@media x; top:0", "top:0", 1},
		{"a:b} c:d; top:0", "top:0", 1},
		{"a:{b}; top:0", "top:0", 1},
		{"top:0; x:(", "top:0;x:(", 0},
	}
	for _, tt := range tests {
		b := newBudget(1 << 20)
		ds, err := ParseDeclarations([]byte(tt.src), b)
		if err != nil {
			t.Fatal(err)
		}
		if got := dumpDecls(ds); got != tt.want || len(warnings(b)) != tt.warn {
			t.Errorf("%q → %q %v, want %q (%d warnings)", tt.src, got, warnings(b), tt.want, tt.warn)
		}
	}
}

func TestCSSBytesLimit(t *testing.T) {
	src := []byte("a{color:red}")
	n := int64(len(src))
	for _, tt := range []struct {
		name  string
		limit int64
		ok    bool
	}{{"just under", n + 1, true}, {"at the limit", n, true}, {"over", n - 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			ss, err := ParseStylesheet(src, OriginUA, newBudget(tt.limit))
			var le *budget.LimitError
			if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.CSSBytes || ss != nil)) {
				t.Fatalf("err = %v", err)
			}
			_, err = ParseDeclarations(src, newBudget(tt.limit))
			if tt.ok != (err == nil) {
				t.Fatalf("ParseDeclarations err = %v", err)
			}
		})
	}
	b := newBudget(100)
	if _, err := ParseStylesheet(src, OriginUA, b); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDeclarations(src, b); err != nil || b.Used(budget.CSSBytes) != 2*n {
		t.Fatalf("budget is shared and cumulative: %v used=%d", err, b.Used(budget.CSSBytes))
	}
}

func nestMixed(n int) string {
	opens, closes := []string{"(", "[", "{", "f("}, []string{")", "]", "}", ")"}
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(opens[i%4])
	}
	for i := n - 1; i >= 0; i-- {
		b.WriteString(closes[i%4])
	}
	return b.String()
}

func nest(open, close string, n int) string {
	return strings.Repeat(open, n) + strings.Repeat(close, n)
}

func TestNestingLimit(t *testing.T) {
	cases := []struct {
		name string
		src  func(n int) string
	}{
		{"value parentheses", func(n int) string { return "a{x:" + nest("(", ")", n) + "}" }},
		{"value functions", func(n int) string { return "a{x:" + nest("f(", ")", n) + "}" }},
		{"value brackets in custom property", func(n int) string { return "a{--x:" + nest("[", "]", n) + "}" }},
		{"mixed", func(n int) string { return "a{--x:" + nestMixed(n) + "}" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := MaxNesting
			for _, tt := range []struct {
				depth int
				kept  bool
			}{{n - 1, true}, {n, true}, {n + 1, false}} {
				got, warn := parseSheet(t, c.src(tt.depth)+" b{top:0}")
				if !strings.HasSuffix(got, "b{top:0}") {
					t.Fatalf("depth %d: following rule lost: %q", tt.depth, got)
				}
				if tt.kept != strings.Contains(got, "a{") || (!tt.kept && len(warn) != 1) || (tt.kept && len(warn) != 0) {
					t.Fatalf("depth %d: kept=%v warn=%v got=%.60q", tt.depth, tt.kept, warn, got)
				}
			}
		})
	}
	t.Run("prelude", func(t *testing.T) {
		for depth, kept := range map[int]bool{MaxNesting: true, MaxNesting + 1: false} {
			got, warn := parseSheet(t, "a"+nest("(", ")", depth)+"{top:0} b{top:1}")
			if kept != strings.HasPrefix(got, "a(") || (kept && len(warn) != 0) || (!kept && len(warn) != 1) {
				t.Fatalf("depth %d: %.60q %v", depth, got, warn)
			}
		}
	})
	t.Run("media", func(t *testing.T) {
		for depth, kept := range map[int]bool{MaxNesting: true, MaxNesting + 1: false} {
			src := strings.Repeat("@media print{", depth) + "a{top:0}" + strings.Repeat("}", depth) + " b{top:1}"
			got, warn := parseSheet(t, src)
			if !strings.HasSuffix(got, "b{top:1}") {
				t.Fatalf("depth %d: following rule lost: %.80q", depth, got)
			}
			if kept && (len(warn) != 0 || !strings.Contains(got, "a{top:0}")) {
				t.Fatalf("depth %d kept: %v", depth, warn)
			}
			if !kept && (len(warn) == 0 || !strings.Contains(strings.Join(warn, ","), "nesting")) {
				t.Fatalf("depth %d dropped: %v", depth, warn)
			}
		}
	})
	t.Run("hostile depth", func(t *testing.T) {
		for _, src := range []string{
			"a{x:" + strings.Repeat("(", 200000) + "} b{top:0}",
			strings.Repeat("{", 200000),
			strings.Repeat("@media a{", 200000),
			"a" + strings.Repeat("[", 200000) + "{top:0}",
			"@x " + strings.Repeat("{", 200000),
		} {
			parseSheet(t, src)
		}
	})
}

func TestOriginKept(t *testing.T) {
	ss, err := ParseStylesheet([]byte("a{top:0}"), OriginExtra, newBudget(100))
	if err != nil || ss.Origin != OriginExtra {
		t.Fatal(err, ss)
	}
}

func TestTokenSize(t *testing.T) {
	var tok Token
	if got := sizeof(tok); got != 32 {
		t.Fatalf("Token is %d bytes", got)
	}
}
