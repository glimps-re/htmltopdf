package box

import (
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/svg"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func dumpWith(t testing.TB, css, body string) (string, *budget.Budget) {
	t.Helper()
	return buildHTML(t, "<style>"+css+"</style>"+body)
}

func TestPseudoBoxes(t *testing.T) {
	tests := []struct {
		name, css, body, want string
	}{
		{"inline before and after", `b::before{content:"<"} b::after{content:">"}`, `<p><b>x</b></p>`, lines(
			"block p", "  inline b", `    inline`, `      text "<"`, `    text "x"`, "    inline", `      text ">"`)},
		{"inline-block", `p::before{content:"n";display:inline-block}`, `<p>t</p>`, lines(
			"block p", "  inline-block", `    text "n"`, `  text "t"`)},
		{"block pseudo wraps the inline content", `p::before{content:"n";display:block}`, `<p>t</p>`, lines(
			"block p", "  block", `    text "n"`, "  anon-block", `    text "t"`)},
		{"empty content generates a box", `p::after{content:"";display:block}`, `<p>t</p>`, lines(
			"block p", "  anon-block", `    text "t"`, "  block")},
		{"content none generates nothing", `p::before{content:none}`, `<p>t</p>`, lines("block p", `  text "t"`)},
		{"no content generates nothing", `p::before{color:red}`, `<p>t</p>`, lines("block p", `  text "t"`)},
		{"pseudo display none generates nothing", `p::before{content:"x";display:none}`, `<p>t</p>`, lines("block p", `  text "t"`)},
		{"element display none generates nothing", `p::before{content:"x"} p{display:none}`, `<p>t</p>`, ""},
		{"after the last child", `div::after{content:"end"}`, `<div><p>a</p></div>`, lines(
			"block div", "  block p", `    text "a"`, "  anon-block", `    inline`, `      text "end"`)},
		{"pseudo in a link", `a::before{content:"→"}`, `<p><a href="https://x.test/">l</a></p>`, lines(
			"block p", `  inline a href="https://x.test/"`, `    inline href="https://x.test/"`, `      text href="https://x.test/" "→"`, `    text href="https://x.test/" "l"`)},
		{"generated text is white-space processed", `p::before{content:"a    b  "}`, `<p>c</p>`, lines(
			"block p", `  inline`, `    text "a b "`, `  text "c"`)},
		{"text-transform applies to generated text", `p::before{content:"ab";text-transform:uppercase}`, `<p>c</p>`, lines(
			"block p", `  inline`, `    text "AB"`, `  text "c"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := dumpWith(t, tt.css, tt.body)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// texts returns the text of every KindText box of the tree, in order.
func texts(root *Box) []string {
	var out []string
	walkBoxes(root, func(b *Box) {
		if b.Kind == KindText {
			out = append(out, b.Text)
		}
	})
	return out
}

func buildTexts(t testing.TB, css, body string) []string {
	t.Helper()
	root, _, err := buildDoc(t, "<style>"+css+"</style>"+body, nil)
	if err != nil {
		t.Fatal(err)
	}
	return texts(root)
}

func TestCounters(t *testing.T) {
	const show = `i::before{content:counter(c)} `
	tests := []struct {
		name, css, body string
		want            []string
	}{
		{"increment without reset creates the counter", show + `i{counter-increment:c}`, `<i></i><i></i><i></i>`, []string{"1", "2", "3"}},
		{"reset then increment", show + `ul{counter-reset:c 10} i{counter-increment:c 5}`, `<ul><i></i><i></i></ul>`, []string{"15", "20"}},
		{"reset on the element itself comes before its increment", show + `i{counter-reset:c 7;counter-increment:c}`, `<i></i>`, []string{"8"}},
		{"the element's own counters apply to its before box", show + `i{counter-increment:c 3}`, `<i></i>`, []string{"3"}},
		{"reading a missing counter gives 0 and creates it", `i::before{content:counter(c)} b{counter-increment:c}`, `<i></i><b></b><i></i>`, []string{"0", "1"}},
		{
			"nested reset restarts, the outer counter comes back", show + `ol{counter-reset:c} li{counter-increment:c}`,
			`<ol><li><i></i><ol><li><i></i></li><li><i></i></li></ol></li><li><i></i></li></ol>`,
			[]string{"1", "1", "2", "2"},
		},
		{
			"a sibling reset replaces the counter instead of nesting", show + `p{counter-reset:c 4} u{counter-increment:c}`,
			`<div><p></p><p></p><u></u><i></i></div><i></i>`,
			[]string{"5", "0"},
		},
		{"the counters of a subtree end with its parent", show + `div{counter-reset:c 9}`, `<section><div></div></section><i></i>`, []string{"0"}},
		{"a counter of a sibling is visible to the following ones", show + `p{counter-reset:c 6}`, `<p></p><section><i></i></section>`, []string{"6"}},
		{"two counters", `i::before{content:counter(a) "-" counter(b)} i{counter-increment:a b 2}`, `<i></i><i></i>`, []string{"1-2", "2-4"}},
		{"negative increment", show + `i{counter-reset:c 3;counter-increment:c -5}`, `<i></i>`, []string{"-2"}},
		{"display none has no effect", show + `u{display:none;counter-increment:c} i{counter-increment:c}`, `<u></u><i></i>`, []string{"1"}},
		{
			"leading zero pads 0 to 9", `i::before{content:counter(c,decimal-leading-zero)} i{counter-increment:c} p{counter-reset:c -1}`, `<p></p>` + strings.Repeat(`<i></i>`, 11),
			[]string{"00", "01", "02", "03", "04", "05", "06", "07", "08", "09", "10"},
		},
		{"leading zero keeps the sign of a negative number", `i::before{content:counter(c,decimal-leading-zero)} i{counter-reset:c -5}`, `<i></i>`, []string{"-5"}},
		{"a name listed twice sums", show + `i{counter-increment:c 2 c 3}`, `<i></i>`, []string{"5"}},
		{"pseudo-elements' own counters", `i::before{counter-increment:c;content:counter(c)} i::after{counter-increment:c;content:counter(c)}`, `<i></i><i></i>`, []string{"1", "2", "1", "2"}},
		{"the counter of an inline element is visible to its following siblings", show + `u{counter-reset:c 5} u i{counter-increment:c}`, `<u><i></i></u><i></i>`, []string{"6", "6"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTexts(t, tt.css, tt.body)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCounterSaturates(t *testing.T) {
	got := buildTexts(t, `i::before{content:counter(c)} i{counter-reset:c 2147483647;counter-increment:c 5} p{counter-reset:d -2147483647;counter-increment:d -9} p::before{content:counter(d)}`, `<i></i><p></p>`)
	if len(got) != 2 || got[0] != "2147483647" || got[1] != "-2147483648" {
		t.Errorf("got %q", got)
	}
}

func TestFormatCounter(t *testing.T) {
	for _, tt := range []struct {
		v    int32
		f    style.CounterFormat
		want string
	}{
		{0, style.CounterDecimal, "0"},
		{7, style.CounterDecimal, "7"},
		{-5, style.CounterDecimal, "-5"},
		{0, style.CounterDecimalLeadingZero, "00"},
		{7, style.CounterDecimalLeadingZero, "07"},
		{9, style.CounterDecimalLeadingZero, "09"},
		{10, style.CounterDecimalLeadingZero, "10"},
		{-5, style.CounterDecimalLeadingZero, "-5"},
		{math.MinInt32, style.CounterDecimalLeadingZero, "-2147483648"},
	} {
		if got := formatCounter(tt.v, tt.f); got != tt.want {
			t.Errorf("formatCounter(%d, %d) = %q, want %q", tt.v, tt.f, got, tt.want)
		}
	}
}

func TestSVGBoxes(t *testing.T) {
	tests := []struct {
		name, css, body, want string
	}{
		{"inline in a paragraph", ``, `<p>a<svg viewBox="0 0 24 24"><path d="M0 0 L1 1" stroke="red"/><rect width="2" height="2"/></svg>b</p>`, lines(
			"block p", `  text "a"`, "  svg svg shapes=2", `  text "b"`)},
		{"alone: inline in the body", ``, `<svg><path d="M0 0 L1 1" stroke="red"/></svg>`, "svg svg shapes=1"},
		{"display block: a block-level child", `svg{display:block}`, `<p>a</p><svg><path d="M0 0 L1 1" stroke="red"/></svg><p>b</p>`, lines(
			"block p", `  text "a"`, "svg svg shapes=1", "block p", `  text "b"`)},
		{"flex item", `div{display:flex}`, `<div><svg></svg><p>x</p></div>`, lines("block div", "  svg svg shapes=0", "  block p", `    text "x"`)},
		{"in a link", ``, `<a href="https://l.test/"><svg></svg></a>`, lines(`inline a href="https://l.test/"`, `  svg svg shapes=0 href="https://l.test/"`)},
		{"display none", `svg{display:none}`, `<p>a</p><svg><path d="M0 0 L1 1"/></svg>`, lines("block p", `  text "a"`)},
		{"with an id", ``, `<p><svg id="s"></svg></p>`, lines("block p", "  svg svg #s shapes=0")},
		{"the children are not boxes", ``, `<p><svg><g><circle r="1"/></g><text>t</text></svg></p>`, lines("block p", "  svg svg shapes=1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := dumpWith(t, tt.css, tt.body)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestMathStaysSkipped(t *testing.T) {
	got, b := buildHTML(t, `<p>a<math><mi>x</mi></math>b</p>`)
	if got != lines("block p", `  text "a"`, `  text "b"`) {
		t.Errorf("got:\n%s", got)
	}
	if ws := strings.Join(warnings(b), "|"); !strings.Contains(ws, "unsupported-element:math") {
		t.Errorf("warnings %q", ws)
	}
}

func TestInternalLinks(t *testing.T) {
	tests := []struct {
		name, body string
		want       string
	}{
		{"target exists", `<a href="#t">l</a><p id=t>x</p>`, lines("anon-block", `  inline a href="#t"`, `    text href="#t" "l"`, "block p #t anchor", `  text "x"`)},
		{"target before the link", `<p id=t>x</p><a href="#t">l</a>`, lines("block p #t anchor", `  text "x"`, "anon-block", `  inline a href="#t"`, `    text href="#t" "l"`)},
		{"no target: dropped", `<a href="#nope">l</a><p id=t>x</p>`, lines("anon-block", "  inline a", `    text "l"`, "block p #t", `  text "x"`)},
		{"empty fragment: dropped", `<a href="#">l</a>`, lines("inline a", `  text "l"`)},
		{"first box with the id", `<a href="#t">l</a><p id=t>1</p><p id=t>2</p>`, lines("anon-block", `  inline a href="#t"`, `    text href="#t" "l"`, "block p #t anchor", `  text "1"`, "block p #t", `  text "2"`)},
		{"target display none: dropped", `<a href="#t">l</a><p id=t style="display:none">x</p>`, lines("inline a", `  text "l"`)},
		{"percent-encoded id", `<a href="#caf%C3%A9">l</a><p id="café">x</p>`, lines("anon-block", `  inline a href="#café"`, `    text href="#café" "l"`, "block p #café anchor", `  text "x"`)},
		{"id of an inline element", `<a href="#t">l</a><p><b id=t>x</b></p>`, lines("anon-block", `  inline a href="#t"`, `    text href="#t" "l"`, "block p", "  inline b #t anchor", `    text "x"`)},
		{"a block link keeps the target on every box", `<a href="#t"><p>in</p></a><div id=t></div>`, lines(`block p href="#t"`, `  text href="#t" "in"`, "block div #t anchor")},
		{"control character in the id: dropped", "<a href=\"#a\x01b\">l</a><p id=\"a\x01b\">x</p>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := buildHTML(t, tt.body)
			if tt.want == "" {
				if strings.Contains(got, "href=") || strings.Contains(got, "anchor") {
					t.Errorf("got:\n%s", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestInternalLinkOverMaxHrefIsDropped(t *testing.T) {
	long := strings.Repeat("a", maxHrefBytes)
	for name, tc := range map[string]struct {
		id   string
		keep bool
	}{"at the limit": {long[:maxHrefBytes-1], true}, "over the limit": {long, false}} {
		got, _ := buildHTML(t, `<a href="#`+tc.id+`">l</a><p id="`+tc.id+`">x</p>`)
		if kept := strings.Contains(got, "anchor"); kept != tc.keep {
			t.Errorf("%s: anchor=%v", name, kept)
		}
	}
}

func TestAbsoluteBoxStaysInPlaceBlockified(t *testing.T) {
	root, _, err := buildDoc(t, `<style>.a{position:absolute} .c{position:relative}</style><div class=c><p>x</p><span class=a>abs</span><p>y</p></div>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var abs []*Box
	walkBoxes(root, func(b *Box) {
		if b.Style != nil && b.Style.Position == style.PositionAbsolute && b.Kind != KindText {
			abs = append(abs, b)
		}
	})
	if len(abs) != 1 || abs[0].Kind != KindBlock || abs[0].Style.Display != style.DisplayBlock {
		t.Fatalf("absolute boxes: %+v", abs)
	}
}

func buildWork(t testing.TB, html string, mod func(*budget.Limits)) (work int64, err error) {
	t.Helper()
	root, b, err := buildDoc(t, html, mod)
	if root != nil {
		runtime.KeepAlive(root)
	}
	return b.Used(budget.Work), err
}

func TestPseudoBoxesHitTheBoxLimit(t *testing.T) {
	const css = `<style>p::before{content:""} p::after{content:""}</style>`
	build := func(n int) (boxes int64, heap uint64, err error) {
		var m0, m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m0)
		deadline := time.AfterFunc(time.Minute, func() { panic("1-minute guard") })
		defer deadline.Stop()
		root, b, err := buildDoc(t, css+strings.Repeat("<p></p>", n), nil)
		runtime.ReadMemStats(&m1)
		runtime.KeepAlive(root)
		return b.Used(budget.Boxes), m1.HeapAlloc, err
	}
	boxes, heap, err := build(66_000)
	if err != nil {
		t.Fatalf("66 000 elements with both pseudo boxes: %v", err)
	}
	if boxes < 3*66_000 || boxes > budget.Defaults()[budget.Boxes] || heap > 256<<20 {
		t.Errorf("%d boxes, heap %d MiB", boxes, heap>>20)
	}
	_, _, err = build(100_000)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Boxes {
		t.Fatalf("100 000 elements with both pseudo boxes: %v", err)
	}
}

func TestDeepCounterNestingCostsAsMuchAsFlat(t *testing.T) {
	const depth = 480
	nested := strings.Repeat(`<div style="counter-reset:c;counter-increment:c">`, depth) + "<i></i>" + strings.Repeat("</div>", depth)
	flat := strings.Repeat(`<div style="counter-reset:c;counter-increment:c"></div>`, depth) + "<i></i>"
	const css = `<style>i::before{content:counter(c)}</style>`
	nestedWork, err := buildWork(t, css+nested, nil)
	if err != nil {
		t.Fatal(err)
	}
	flatWork, err := buildWork(t, css+flat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if nestedWork > 2*flatWork {
		t.Errorf("nested Work %d, flat twin %d", nestedWork, flatWork)
	}
	root, _, err := buildDoc(t, css+nested, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(root); len(got) != 1 || got[0] != "1" {
		t.Errorf("innermost counter %q, want its own 1", got)
	}
}

func TestManyCounterNamesAreReleased(t *testing.T) {
	var sb strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&sb, `<p style="counter-reset:n%d"></p>`, i)
	}
	b := testBudget(nil)
	d := parseDoc(t, "<div>"+sb.String()+"</div>", b)
	st := newStyler(t, d, b)
	bd := &builder{d: d, st: st, b: b}
	cs, err := st.Element(d, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	bd.enterScope()
	if _, err := bd.block(0, KindBlock, cs, ""); err != nil {
		t.Fatal(err)
	}
	if len(bd.counters.byName) != 0 || len(bd.counters.scopes) != 1 {
		t.Errorf("%d counters and %d scopes left", len(bd.counters.byName), len(bd.counters.scopes))
	}
}

// runs splits a sequence of counter texts into the runs that count 1, 2, 3...
func runs(t testing.TB, seq []string, format func(int) string) (n []int) {
	t.Helper()
	next := 1
	count := 0
	for _, s := range seq {
		if s == format(1) && count > 0 {
			n = append(n, count)
			next, count = 1, 0
		}
		if s != format(next) {
			t.Fatalf("counter text %q, want %q (run so far %d)", s, format(next), count)
		}
		next++
		count++
	}
	return append(n, count)
}

// buildReport builds a template.
func buildReport(t testing.TB, name, size string) *Box {
	t.Helper()
	b := testBudget(nil)
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, testcorpus.Size(size))
	if err != nil {
		t.Fatal(err)
	}
	d := parseDoc(t, string(html), b)
	root, err := Build(d, newStyler(t, d, b), b)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestTemplate02Counters(t *testing.T) {
	root := buildReport(t, "02-executive", "small")
	var h2 []string
	var findings []string
	var legend, lists int
	walkBoxes(root, func(x *Box) {
		switch {
		case x.Tag == atom.H2 && len(x.Children) > 0:
			if c := x.Children[0]; c.Kind == KindInline && len(c.Children) == 1 {
				h2 = append(h2, c.Children[0].Text+x.Children[1].Text)
			} else {
				h2 = append(h2, "-"+c.Text)
			}
		case x.Tag == 0 && (x.Kind == KindInline || x.Kind == KindBlock) && len(x.Children) == 1 && x.Children[0].Kind == KindText && isDigits(x.Children[0].Text):
			findings = append(findings, x.Children[0].Text)
		case x.Tag == atom.Li && len(x.Children) > 1 && x.Children[0].Kind == KindInlineBlock && x.Children[0].Tag == 0:
			legend++
		case x.Tag == atom.Ol && len(x.Style.CounterReset) > 0:
			lists++
		}
	})
	want := []string{"-Sommaire", "01 Résumé", "02 Fichiers bloqués", "03 Archives bloquées", "04 Fichiers non analysés", "05 Fichiers en erreur"}
	if fmt.Sprint(h2) != fmt.Sprint(want) {
		t.Errorf("h2: %q\nwant %q", h2, want)
	}
	if legend != 4 {
		t.Errorf("%d legend items with a ::before inline-block, want 4", legend)
	}
	counts := runs(t, findings, strconv.Itoa)
	if len(counts) != lists-1 && len(counts) != lists {
		t.Logf("%d runs of finding numbers %v for %d lists", len(counts), counts, lists)
	}
	for _, n := range counts {
		if n == 0 {
			t.Error("an empty run")
		}
	}
}

func TestTemplate02LargeNestedFindingsRestart(t *testing.T) {
	root := buildReport(t, "02-executive", "large")
	var seq []string
	walkBoxes(root, func(x *Box) {
		if x.Tag == 0 && (x.Kind == KindInline || x.Kind == KindBlock) && len(x.Children) == 1 && x.Children[0].Kind == KindText && isDigits(x.Children[0].Text) {
			seq = append(seq, x.Children[0].Text)
		}
	})
	counts := runs(t, seq, strconv.Itoa)
	sevens := 0
	for _, n := range counts {
		if n == 7 {
			sevens++
		}
	}
	if sevens < 2 {
		t.Errorf("runs %v: the nested findings of chrome09 02L p28 are numbered 1 to 7", counts)
	}
}

func TestTemplate03RowNumbers(t *testing.T) {
	root := buildReport(t, "03-analyst", "small")
	var idx []string
	walkBoxes(root, func(x *Box) {
		if x.Tag == atom.Td && len(x.Children) > 0 && x.Children[0].Kind == KindInline && len(x.Children[0].Children) == 1 {
			if s := x.Children[0].Children[0].Text; len(s) == 2 && s[0] >= '0' && s[0] <= '9' {
				idx = append(idx, s)
			}
		}
	})
	if len(idx) < 3 {
		t.Fatalf("%d row numbers", len(idx))
	}
	runs(t, idx, func(i int) string { return fmt.Sprintf("%02d", i) })
}

func TestHostileSVGsAreBounded(t *testing.T) {
	for name, tc := range map[string]struct {
		html string
		mod  func(*budget.Limits)
	}{
		"many svgs":        {strings.Repeat("<svg></svg>", 3000), nil},
		"svg in svg":       {strings.Repeat("<svg>", 200) + strings.Repeat("</svg>", 200), nil},
		"deep groups":      {"<svg>" + strings.Repeat("<g>", 400) + `<path d="M0 0 L1 1"/>` + strings.Repeat("</g>", 400) + "</svg>", nil},
		"many shapes":      {"<svg>" + strings.Repeat(`<path d="M0 0 L1 1"/>`, 12000) + "</svg>", nil},
		"huge path":        {`<svg><path d="M0 0` + strings.Repeat(" 1 1", 40000) + `"/></svg>`, nil},
		"svg in a link":    {`<a href="https://e.example/"><svg><text>t</text></svg></a>`, nil},
		"svg inside table": {`<table><tr><td><svg><circle r="3"/></svg></td></tr></table>`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			root, b, err := buildDoc(t, tc.html, tc.mod)
			if err != nil {
				t.Fatal(err)
			}
			checkTree(t, root)
			svgs := 0
			walkBoxes(root, func(x *Box) {
				if x.Kind == KindSVG {
					svgs++
				}
			})
			if svgs == 0 {
				t.Error("no svg box")
			}
			if ws, _ := b.Warnings(); len(ws) > budget.MaxWarnings {
				t.Errorf("%d warnings", len(ws))
			}
		})
	}
}

func TestSVGWorkLimitStopsTheBuild(t *testing.T) {
	html := "<svg>" + strings.Repeat(`<path d="M0 0 L1 1 L2 2"/>`, 100) + "</svg>"
	work, err := buildWork(t, html, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildWork(t, html, func(l *budget.Limits) { l[budget.Work] = work - 1 })
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("err = %v", err)
	}
	if _, err = buildWork(t, html, func(l *budget.Limits) { l[budget.Work] = work }); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
}

func TestAbsolutePseudoElementsAreGenerated(t *testing.T) {
	got, _ := dumpWith(t, `p::before{content:"x";position:absolute} p{position:relative}`, `<p>t</p>`)
	if got != lines("block p", "  block", `    text "x"`, "  anon-block", `    text "t"`) {
		t.Errorf("got:\n%s", got)
	}
}

func TestGeneratedTextIsCapped(t *testing.T) {
	const piece = 100 << 10
	gen := func(css string, n int) (generated int, warned bool, heap uint64) {
		var m0, m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m0)
		root, b, err := buildDoc(t, "<style>"+css+"</style>"+strings.Repeat("<p></p>", n), nil)
		if err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		runtime.ReadMemStats(&m1)
		runtime.KeepAlive(root)
		walkBoxes(root, func(x *Box) {
			if x.Kind == KindText {
				generated += len(x.Text)
			}
		})
		for _, w := range warnings(b) {
			warned = warned || w == "clamped:content"
		}
		return generated, warned, m1.HeapAlloc
	}
	long := strings.Repeat("a", piece)
	g, warned, heap := gen(`p::before{content:"`+long+`"}`, 2000)
	if g != MaxGeneratedBytes || !warned || heap > 64<<20 {
		t.Errorf("2000 x 100 KB: %d bytes (want %d), warned=%v, heap %d MiB", g, MaxGeneratedBytes, warned, heap>>20)
	}
	n := MaxGeneratedBytes / piece
	if g, warned, _ = gen(`p::before{content:"`+long+`"}`, n); g != n*piece || warned {
		t.Errorf("under the cap: %d bytes, warned=%v", g, warned)
	}
	spaces := strings.Repeat("a       ", piece/8)
	g, warned, heap = gen(`p::before{content:"`+spaces+`";white-space:pre}`, 2000)
	if g > MaxGeneratedBytes || !warned || heap > 64<<20 {
		t.Errorf("preserved spaces: %d bytes, warned=%v, heap %d MiB", g, warned, heap>>20)
	}
	g, _, heap = gen(`p::before{content:"`+spaces+`"}`, 2000)
	if g > MaxGeneratedBytes || heap > 64<<20 {
		t.Errorf("collapsed spaces: %d bytes, heap %d MiB", g, heap>>20)
	}
}

func TestGeneratedTextIsCutOnARuneBoundary(t *testing.T) {
	s := strings.Repeat("é", 10)
	for n, want := range map[int]string{0: "", 1: "", 2: "é", 3: "é", 19: strings.Repeat("é", 9), 20: s, 25: s} {
		if got := cutRunes(s, n); got != want {
			t.Errorf("cutRunes(%d) = %q, want %q", n, got, want)
		}
	}
	bd := &builder{b: testBudget(nil), generated: MaxGeneratedBytes - 3}
	if got := bd.takeGenerated("éé"); got != "é" || bd.generated != MaxGeneratedBytes-1 {
		t.Errorf("took %q, generated %d", got, bd.generated)
	}
}

func TestSVGSegmentCapIsSharedByTheBuild(t *testing.T) {
	const per = svg.MaxPathBytes / 4
	one := "<svg>" + strings.Repeat(`<polyline points="`+strings.Repeat("0,0 ", per)+`"/>`, 4) + "</svg>"
	html := strings.Repeat(one, svg.MaxSegments/(4*per)+1)
	root, b, err := buildDoc(t, html, func(l *budget.Limits) { l[budget.InputBytes], l[budget.Nodes] = 64<<20, 1<<20 })
	if err != nil {
		t.Fatal(err)
	}
	segs := 0
	walkBoxes(root, func(x *Box) {
		if x.SVG != nil {
			for _, sh := range x.SVG.Shapes {
				segs += len(sh.Segs)
			}
		}
	})
	if segs != svg.MaxSegments || !slices.Contains(warnings(b), "clamped:svg segments") {
		t.Errorf("%d segments, warnings %v", segs, warnings(b))
	}
}

func isDigits(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil && !strings.HasPrefix(s, "+") && !strings.HasPrefix(s, "-")
}

func TestAbsoluteBoxesInInlineContentAreOutOfFlow(t *testing.T) {
	got, _ := dumpWith(t, `.a{position:absolute}`, `<p>Hello <span class=a>badge</span> world</p><div style="display:flex"><i class=a>f</i><b>x</b></div>`)
	want := lines(
		"block p", `  text "Hello "`, `  text "world"`, "  out-of-flow", "    block span", `      text "badge"`,
		"block div", "  block b", `    text "x"`, "  out-of-flow", "    block i", `      text "f"`)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// A block-level element stays a block child: it starts a new line, as in Chrome.
	got, _ = dumpWith(t, `.a{position:absolute}`, `<div>Hello <div class=a>block</div> world</div>`)
	if strings.Contains(got, "out-of-flow") {
		t.Errorf("a div is not out of flow in the line:\n%s", got)
	}
}
