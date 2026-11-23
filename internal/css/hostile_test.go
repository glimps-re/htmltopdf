package css_test

import (
	"context"
	"errors"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

var (
	styleElem = regexp.MustCompile(`(?s)<style>(.*?)</style>`)
	styleAttr = regexp.MustCompile(`(?s) style="(.*?)"`)
)

// maxHeapDelta bounds the bytes allocated (cumulative, growth garbage included)
// while parsing one hostile document of at most 512 KiB.
const maxHeapDelta = 128 << 20

func warnText(b *budget.Budget) string {
	ws, _ := b.Warnings()
	var s strings.Builder
	for _, w := range ws {
		s.WriteString(w.Code + ":" + w.Detail + "\n")
	}
	return s.String()
}

// TestHostileCorpusCSS feeds the CSS of every hostile corpus document to the
// parser: no panic, bounded allocation, the expected limit or warnings.
func TestHostileCorpusCSS(t *testing.T) {
	limits := budget.Defaults()
	seen := 0
	for _, c := range testcorpus.Hostile(limits) {
		doc := string(c.HTML())
		var sheets, attrs []string
		for _, m := range styleElem.FindAllStringSubmatch(doc, -1) {
			sheets = append(sheets, html.UnescapeString(m[1]))
		}
		for _, m := range styleAttr.FindAllStringSubmatch(doc, -1) {
			attrs = append(attrs, html.UnescapeString(m[1]))
		}
		if len(sheets)+len(attrs) == 0 {
			continue
		}
		seen++
		if raceEnabled && len(doc) > 64<<10 {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			b := budget.New(context.Background(), limits, nil)
			var ms0, ms1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&ms0)
			var limitErr error
			for _, s := range sheets {
				if _, err := css.ParseStylesheet([]byte(s), css.OriginDocument, b); err != nil {
					limitErr = err
				}
			}
			for _, s := range attrs {
				if _, err := css.ParseDeclarations([]byte(s), b); err != nil {
					limitErr = err
				}
			}
			runtime.ReadMemStats(&ms1)
			if d := ms1.TotalAlloc - ms0.TotalAlloc; d > maxHeapDelta {
				t.Errorf("allocated %d MiB", d>>20)
			}
			var le *budget.LimitError
			if limitErr != nil && (!errors.As(limitErr, &le) || le.Kind != budget.CSSBytes) {
				t.Fatalf("unexpected error %v", limitErr)
			}
			w := warnText(b)
			switch {
			case c.Limit == "MaxCSSBytes" && c.Want == testcorpus.Limit:
				if limitErr == nil {
					t.Error("over the CSSBytes limit: no error")
				}
			case c.Limit == "MaxCSSBytes":
				if limitErr != nil || w != "" {
					t.Errorf("at the limit: err=%v warnings=%q", limitErr, w)
				}
			case c.Limit == "css-nesting" && c.Want == testcorpus.Degraded:
				if !strings.Contains(w, "nesting too deep") {
					t.Errorf("over the nesting limit: warnings %q", w)
				}
			case c.Limit == "css-nesting":
				if limitErr != nil || w != "" {
					t.Errorf("at the nesting limit: err=%v warnings=%q", limitErr, w)
				}
			case strings.HasPrefix(c.Name, "css_malformed_"):
				if limitErr != nil {
					t.Errorf("err %v", limitErr)
				}
			default:
				// Selector, var() and value limits belong to later tasks: the
				// syntax parser must accept these sheets silently.
				if limitErr != nil || w != "" {
					t.Errorf("err=%v warnings=%q", limitErr, w)
				}
			}
		})
	}
	if seen < 30 {
		t.Fatalf("only %d corpus documents carry CSS", seen)
	}
}

func TestMalformedCorpusWarns(t *testing.T) {
	for i, src := range []string{
		"p{color:red", "}}}} p{color:red} {{{{", `p{content:"unterminated}`, "@media",
		"@media screen", "@media screen{p{color:red}", "@page{@bottom-left{content:",
		"p{background:url(", "p{color:;;;: ; :red}", "@@@ p{{{ color }}}", "p::before{content:var(--",
	} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := css.ParseStylesheet([]byte(src), css.OriginDocument, b); err != nil {
			t.Fatal(err)
		}
		if i != 1 && i != 9 && warnText(b) == "" {
			t.Errorf("%q: dropped silently", src)
		}
	}
}

// Strict callers (ExtraCSS, UA sheet) check that no warning was recorded: every
// kind of dropped item must leave one.
func TestStrictDroppedItems(t *testing.T) {
	deep := "a{x:" + strings.Repeat("(", css.MaxNesting+1) + strings.Repeat(")", css.MaxNesting+1) + "}"
	deepMedia := strings.Repeat("@media a{", css.MaxNesting+1) + "b{c:d}" + strings.Repeat("}", css.MaxNesting+1)
	drops := []string{
		"a{color red}", "a{color:}", "a{12:3}", "a{@x;}", "a{b{c:d}}", "a{color:red", "a{x:calc(1px",
		"a{x:1)}", "a{x:{0}}", "a{x:\"a\n}", "a{x:url(a b)}", "@import 'x';", "@foo{}", "@foo;", "@foo",
		"@media print;", "@page;", "@media print{a{color red}}", "@page{@middle{x:y}}",
		"@page{@top-left{@top-right{x:y}}}", "a{} b", "a{b:c} @charset 'x';",
		"@media print{@font-face{}}", "@media print{b{x:y", deep, deepMedia,
	}
	for _, src := range drops {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := css.ParseStylesheet([]byte(src), css.OriginExtra, b); err != nil {
			t.Fatal(err)
		}
		if warnText(b) == "" {
			t.Errorf("%.80q: dropped without a warning", src)
		}
	}
	for _, src := range []string{"a{x:1;;y}", "a{color:red;color red}", "a{x:1;y:}"} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := css.ParseDeclarations([]byte(src), b); err != nil {
			t.Fatal(err)
		}
	}
	for _, src := range []string{"color red", "color:", "12:3", "x:{0}", "x:1)", "@x;y:1", "x:url(a b)"} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		if _, err := css.ParseDeclarations([]byte(src), b); err != nil {
			t.Fatal(err)
		}
		if warnText(b) == "" {
			t.Errorf("declarations %q: dropped without a warning", src)
		}
	}
	clean := "@charset 'x'; a{color:red} @media print{b{top:0}} @page{margin:0;@top-left{content:'x'}}"
	b := budget.New(context.Background(), budget.Defaults(), nil)
	if _, err := css.ParseStylesheet([]byte(clean), css.OriginUA, b); err != nil {
		t.Fatal(err)
	}
	if w := warnText(b); w != "" {
		t.Errorf("valid sheet warned: %q", w)
	}
}

// Past the warning cap every drop is still counted.
func TestStrictManyDrops(t *testing.T) {
	const n = 1000
	var src strings.Builder
	for i := 0; i < n; i++ {
		src.WriteString("a{x" + strings.Repeat("y", i%7) + " z" + string(rune('a'+i%26)) + "}")
		src.WriteString("@u" + strings.Repeat("v", i%251) + ";")
	}
	b := budget.New(context.Background(), budget.Defaults(), nil)
	if _, err := css.ParseStylesheet([]byte(src.String()), css.OriginExtra, b); err != nil {
		t.Fatal(err)
	}
	ws, dropped := b.Warnings()
	if len(ws) == 0 || len(ws) > 256 || len(ws)+dropped < 2 {
		t.Fatalf("%d warnings, %d dropped", len(ws), dropped)
	}
}

// Additional CSS Syntax §5 recovery cases.
func TestErrorRecoveryMore(t *testing.T) {
	tests := []struct {
		name, src string
		rules     int
		wantWarn  bool
	}{
		{"semicolon inside a block value", "a{x:{;};y:1}", 1, true},
		{"unmatched close bracket then rule", "a{x:]} b{y:1}", 1, true},
		{"declaration eaten by an unclosed function", "a{x:f(1;y:2} b{z:3}", 0, true},
		{"at-rule prelude with a nested block then rule", "@foo [{;}] {x:y} a{y:1}", 1, true},
		{"cdo and cdc inside a declaration block", "a{<!-- x:1 -->}", 0, true},
		{"escaped brace in a prelude does not open a block", "a\\{b{x:1}", 1, false},
		{"escaped newline in a string", "a{x:\"a\\\nb\"}", 1, false},
		{"unterminated url at EOF", "a{x:url(abc", 1, true},
		{"nul and replacement characters", "a{x:\x00;y:1}", 1, false},
		{"bad string then valid declaration", "a{x:'a\nb;y:1}", 1, true},
		{"only closers", "}}}}))))]]]]", 0, true},
		{"important in the middle", "a{x:1 !important 2;y:1}", 1, false},
		{"!important without value", "a{x:!important;y:1}", 1, true},
		{"declaration name is a number", "a{1px:2;y:1}", 1, true},
		{"colon at the start", "a{:x;y:1}", 1, true},
		{"empty prelude", "{x:1} a{y:1}", 2, false},
		{"unicode escape past the range", "a{x:\"\\110000 \\0 \\d800\";y:1}", 1, false},
		{"huge number", "a{x:1e999999;y:-1e999999 .5e-999999;z:" + strings.Repeat("9", 400) + "}", 1, false},
		{"nested media with garbage", "@media print{@media {{{ a{x:1}", 1, true},
		{"page margin box without a block", "@page{@top-left; margin:0}", 1, true},
		{"at-rule then EOF inside a margin box", "@page{@top-left{content:", 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := budget.New(context.Background(), budget.Defaults(), nil)
			ss, err := css.ParseStylesheet([]byte(tt.src), css.OriginDocument, b)
			if err != nil {
				t.Fatal(err)
			}
			if len(ss.Rules) != tt.rules {
				t.Errorf("%d rules, want %d", len(ss.Rules), tt.rules)
			}
			if got := warnText(b) != ""; got != tt.wantWarn {
				t.Errorf("warned=%v want %v: %q", got, tt.wantWarn, warnText(b))
			}
		})
	}
}

func TestNestingBoundaryRules(t *testing.T) {
	media := func(n int) string {
		return strings.Repeat("@media print{", n) + "a{top:0}" + strings.Repeat("}", n)
	}
	for _, tt := range []struct {
		n    int
		kept bool
	}{{css.MaxNesting - 1, true}, {css.MaxNesting, true}, {css.MaxNesting + 1, false}} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ss, err := css.ParseStylesheet([]byte(media(tt.n)), css.OriginDocument, b)
		if err != nil {
			t.Fatal(err)
		}
		depth, r := 0, ss.Rules
		for len(r) > 0 && r[0].Kind == css.RuleMedia {
			depth++
			r = r[0].Rules
		}
		if tt.kept != (depth == tt.n && len(r) == 1) {
			t.Errorf("depth %d: reached %d", tt.n, depth)
		}
		if tt.kept != (warnText(b) == "") {
			t.Errorf("depth %d: warnings %q", tt.n, warnText(b))
		}
	}
}

func TestHostileSources(t *testing.T) {
	big := 1 << 20
	if raceEnabled {
		big = 1 << 17
	}
	for name, src := range map[string]string{
		"deep brackets":   "a{x:" + strings.Repeat("[", big/2) + "}",
		"many rules":      strings.Repeat("a{x:1}", big/6),
		"many selectors":  strings.Repeat("a,", big/2) + "a{x:1}",
		"many decls":      "a{" + strings.Repeat("x:1;", big/4) + "}",
		"long ident":      "a{x:" + strings.Repeat("b", big) + "}",
		"many at-rules":   strings.Repeat("@x;", big/3),
		"many open media": strings.Repeat("@media a{", big/9),
		"cdo flood":       strings.Repeat("<!--", big/4),
		"bad utf8":        strings.Repeat("\xff\xfe", big/2),
		"comment flood":   strings.Repeat("/**/", big/4),
	} {
		t.Run(name, func(t *testing.T) {
			l := budget.Defaults()
			l[budget.CSSBytes] = int64(len(src))
			b := budget.New(context.Background(), l, nil)
			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)
			if _, err := css.ParseStylesheet([]byte(src), css.OriginDocument, b); err != nil {
				t.Fatal(err)
			}
			runtime.ReadMemStats(&m1)
			if d := m1.TotalAlloc - m0.TotalAlloc; d > 200<<20 {
				t.Errorf("allocated %d MiB for %d bytes", d>>20, len(src))
			}
		})
	}
}

func TestLexerNeverWritesCallerBuffer(t *testing.T) {
	src := []byte("a{color:red} b{x:\"y")
	cp := append([]byte(nil), src...)
	b := budget.New(context.Background(), budget.Defaults(), nil)
	if _, err := css.ParseStylesheet(src[:len(src):len(src)], css.OriginDocument, b); err != nil {
		t.Fatal(err)
	}
	if string(src) != string(cp) {
		t.Fatal("caller buffer modified")
	}
}

func TestOnlyNewInputBytes(t *testing.T) {
	for _, f := range []string{"parse.go", "token.go"} {
		data, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		for _, bad := range []string{"parse.NewInput(", "parse.NewInputString", "NewInputReader", "io.ReadAll", "os/exec"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s uses %s", f, bad)
			}
		}
	}
}

// A stylesheet at the CSSBytes limit made only of tokens (the worst case for
// the token arena) must stay below 64 MiB once parsed.
func TestRetainedHeapAtCSSLimit(t *testing.T) {
	l := budget.Defaults()
	src := []byte(strings.Repeat("a,", int(l[budget.CSSBytes]-12)/2) + "a{color:red}")
	b := budget.New(context.Background(), l, nil)
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	ss, err := css.ParseStylesheet(src, css.OriginDocument, b)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	runtime.KeepAlive(ss)
	t.Logf("%d source bytes retain %d KiB", len(src), (m1.HeapAlloc-m0.HeapAlloc)>>10)
	if d := m1.HeapAlloc - m0.HeapAlloc; d > 64<<20 {
		t.Errorf("retained %d MiB", d>>20)
	}
}
