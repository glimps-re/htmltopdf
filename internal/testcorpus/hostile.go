package testcorpus

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// Outcome is what a render of a hostile document must produce.
type Outcome uint8

// Outcomes of a Case.
const (
	// Success: the render succeeds within the time and memory budgets.
	Success Outcome = iota
	// Limit: the render fails with a limit error naming Case.Limit.
	Limit
	// Degraded: the render succeeds and reports at least one warning.
	Degraded
	// LimitOrDegraded: either Limit or Degraded is acceptable (design §11.6).
	LimitOrDegraded
	// Robust: any error or success, but no panic, within 2 s and 256 MiB.
	Robust
)

// Case is a hostile or boundary document. Over cases sit past a limit; their
// Under twin sits below it and must succeed. The margin of each twin and the
// reason for it are given where the generator builds it: it is zero when one
// unit of the document is exactly one unit of the limit.
type Case struct {
	Name string
	// Limit names the exercised limit: a budget.Kind name ("MaxNodes",
	// "HTMLDepth"…) or a short label for the limits that are not budget kinds
	// ("colspan", "length", "page-size"…). Empty for robustness cases.
	Limit string
	// Twin is the name of the Over/Under counterpart, empty if none.
	Twin string
	Want Outcome
	// HTML generates the document. It is deterministic.
	HTML func() []byte
}

func doc(head, body string) []byte {
	return []byte("<html><head>" + head + "</head><body>" + body + "</body></html>")
}

func style(css string) string { return "<style>" + css + "</style>" }

func pair(name, limit string, over Outcome, overDoc, underDoc func() []byte) []Case {
	return []Case{
		{Name: name + "_over", Limit: limit, Twin: name + "_under", Want: over, HTML: overDoc},
		{Name: name + "_under", Limit: limit, Twin: name + "_over", Want: Success, HTML: underDoc},
	}
}

func isqrt(n int64) int64 { return int64(math.Sqrt(float64(n))) }

// repeat is strings.Repeat that yields "" for a negative count, so that
// generators stay total when the limits they derive from are tiny.
func repeat[N ~int | ~int64](s string, n N) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, int(n))
}

// Boundaries fixed by the design, used as n and n+1.
const (
	maxDivDepth  = 510 // 512 open elements of x/net/html minus html and body
	maxCompounds = 32
	maxSimples   = 32
	maxNesting   = 64
	maxColumns   = 1000
	maxLinkBytes = 2048
	maxStops     = 64
)

func padComment(total int) string {
	return "/*" + repeat(" ", total-4) + "*/"
}

// SpikeHostile returns the seven hostile documents of the spike, byte for byte.
// Their outcomes hold for budget.Defaults().
func SpikeHostile() []Case {
	mk := func(name, limit string, want Outcome, gen func() []byte) Case {
		return Case{Name: name, Limit: limit, Want: want, HTML: gen}
	}
	return []Case{
		mk("hostile_deep_nesting", "HTMLDepth", Limit, func() []byte {
			return []byte("<html><body>" + repeat("<div>", 20000) + "deep" +
				repeat("</div>", 20000) + "</body></html>")
		}),
		mk("hostile_long_word", "MaxInputBytes", Limit, func() []byte {
			return []byte("<html><body><table style='width:100%;table-layout:fixed;word-wrap:break-word'><tr><td>" +
				repeat("A", 2<<20) + "</td></tr></table></body></html>")
		}),
		mk("hostile_50k_rows", "MaxInputBytes", Limit, func() []byte {
			var b strings.Builder
			b.WriteString("<html><body><table>")
			for i := range 50000 {
				fmt.Fprintf(&b, "<tr><td>%d</td><td>%s</td></tr>", i, sha(i))
			}
			b.WriteString("</table></body></html>")
			return []byte(b.String())
		}),
		mk("hostile_var_bomb", "MaxVarBytes", Degraded, func() []byte {
			var b strings.Builder
			b.WriteString("<html><head><style>:root{--v0: x;")
			for i := 1; i <= 40; i++ {
				fmt.Fprintf(&b, "--v%d: var(--v%d) var(--v%d);", i, i-1, i-1)
			}
			b.WriteString("} p::before{content: var(--v40)} p{font-family: var(--v40)}</style></head><body><p>var bomb</p></body></html>")
			return []byte(b.String())
		}),
		mk("hostile_colspan", "colspan", Degraded, func() []byte {
			return []byte(`<html><body><table><tr><td colspan="999999999" rowspan="65534">x</td></tr>` +
				`<tr><td colspan="2147483647">y</td></tr></table></body></html>`)
		}),
		mk("hostile_remote", "no-resource-access", Degraded, func() []byte {
			return []byte(`<html><head><link rel="stylesheet" href="http://127.0.0.1:1/x.css"></head><body>` +
				`<img src="http://169.254.169.254/latest/meta-data/"><img src="file:///etc/passwd"><p>ssrf</p></body></html>`)
		}),
		mk("hostile_huge_page", "page-size", Degraded, func() []byte {
			return []byte(`<html><head><style>@page{size: 99999999in 99999999in}` +
				`p{font-size: 9999999px; width: 1e30px; margin: -1e30px}</style></head><body><p>big</p></body></html>`)
		}),
	}
}

// Hostile returns the hostile documents of design §6.1, §8.4 and §11.6, sized
// from the limits l (budget.Defaults() for the committed SHA-256 constants).
// It never panics, whatever l: counts are clamped at zero, so tiny limits give
// degenerate documents, and huge ones only build closures (generating a
// document sized from an unlimited limit would exhaust memory). The boundary
// properties hold for limits of at least a few KiB. Documents of the counted
// limits never exceed l[InputBytes] unless they target it. The OutputBytes limit has no generator: tests set a tiny
// Limits.MaxOutputBytes and render a report.
func Hostile(l budget.Limits) []Case {
	var cs []Case
	cs = append(cs, inputCases(l)...)
	cs = append(cs, nodeCases(l)...)
	cs = append(cs, cssCases(l)...)
	cs = append(cs, layoutCases(l)...)
	cs = append(cs, valueCases()...)
	cs = append(cs, robustCases()...)
	return cs
}

func inputCases(l budget.Limits) []Case {
	pad := func(n int64) func() []byte {
		return func() []byte {
			head, tail := "<html><body><p>x</p><!--", "-->"
			return []byte(head + repeat(" ", int(n)-len(head)-len(tail)) + tail)
		}
	}
	// 510 nested divs plus html and body fill the 512 open elements of x/net/html.
	nest := func(n int) func() []byte {
		return func() []byte { return []byte(repeat("<div>", n) + "x") }
	}
	cs := pair("input_bytes", "MaxInputBytes", Limit, pad(l[budget.InputBytes]+1), pad(l[budget.InputBytes]))
	cs = append(cs, pair("html_depth", "HTMLDepth", Limit, nest(maxDivDepth+1), nest(maxDivDepth))...)
	return cs
}

type nodeUnit struct {
	name, unit string
	// cost is the number of nodes the pre-pass charges per repetition.
	cost int64
}

var nodeUnits = []nodeUnit{
	{"x_p", "x<p>", 2},
	{"p", "<p>", 1},
	{"br", "<br>", 1},
	{"p_8_attrs", "<p a b c d e f g h>", 9},
	{"comment", "<!---->", 1},
}

func nodeCases(l budget.Limits) []Case {
	var cs []Case
	rep := func(unit string, n int64) func() []byte {
		return func() []byte { return []byte(repeat(unit, int(n))) }
	}
	maxNodes, maxIn := l[budget.Nodes], l[budget.InputBytes]
	for _, u := range nodeUnits {
		over := maxIn / int64(len(u.unit))
		// The pre-pass also counts the implied html, head and body nodes.
		under := (maxNodes - 16) / u.cost
		cs = append(cs, pair("nodes_"+u.name, "MaxNodes", Limit, rep(u.unit, over), rep(u.unit, under))...)
	}
	// Reconstruction of formatting elements multiplies nodes per token, so the
	// twin stays at a fortieth of the limit instead of hugging it.
	const bomb = "<b><i><u><p>x"
	cs = append(cs, pair("nodes_format_bomb", "MaxNodes", Limit,
		rep(bomb, maxIn/int64(len(bomb))), rep(bomb, maxNodes/40))...)
	return cs
}

func cssCases(l budget.Limits) []Case {
	var cs []Case
	maxCSS := int(l[budget.CSSBytes])
	cs = append(cs, pair("css_style_element", "MaxCSSBytes", Limit,
		func() []byte { return doc(style(padComment(maxCSS+1)), "<p>x</p>") },
		func() []byte { return doc(style(padComment(maxCSS)), "<p>x</p>") })...)
	cs = append(cs, pair("css_style_attribute", "MaxCSSBytes", Limit,
		func() []byte { return doc("", `<p style="`+padComment(maxCSS+1)+`">x</p>`) },
		func() []byte { return doc("", `<p style="`+padComment(maxCSS)+`">x</p>`) })...)

	list := func(n int64) func() []byte {
		return func() []byte { return doc(style(repeat("a,", int(n)-1)+"a{color:red}"), "<a>x</a>") }
	}
	maxSel := l[budget.Selectors]
	cs = append(cs, pair("selectors_list", "MaxSelectors", Limit, list(maxSel+1), list(maxSel))...)
	cs = append(cs, Case{
		Name: "selectors_list_max_css", Limit: "MaxSelectors", Want: Limit,
		HTML: list((int64(maxCSS) - 12) / 2),
	})

	compounds := func(n int) func() []byte {
		return func() []byte {
			return doc(style(repeat("a ", n-1)+"a{color:red}"), "<a>x</a>")
		}
	}
	cs = append(cs, pair("selector_compounds", "compounds", Degraded, compounds(maxCompounds+1), compounds(maxCompounds))...)
	cs = append(cs, Case{Name: "selector_compounds_10000", Limit: "compounds", Want: Degraded, HTML: compounds(10000)})

	simples := func(n int) func() []byte {
		return func() []byte {
			var sel strings.Builder
			sel.WriteString("a")
			for i := 1; i < n; i++ {
				fmt.Fprintf(&sel, ".c%d", i)
			}
			return doc(style(sel.String()+"{color:red}"), `<a class="c1">x</a>`)
		}
	}
	cs = append(cs, pair("selector_simples", "simple-selectors", Degraded, simples(maxSimples+1), simples(maxSimples))...)

	bombLevels := func(k int) func() []byte {
		return func() []byte {
			var b strings.Builder
			b.WriteString(":root{--v0: x;")
			for i := 1; i <= k; i++ {
				fmt.Fprintf(&b, "--v%d: var(--v%d) var(--v%d);", i, i-1, i-1)
			}
			fmt.Fprintf(&b, "} p{font-family: var(--v%d)}", k)
			return doc(style(b.String()), "<p>var</p>")
		}
	}
	// Each level doubles the value: len(v_k) = 2^(k+1)-1 bytes.
	// The bound of 61 keeps the shift in range for an "unlimited" VarBytes.
	over := 0
	for over < 61 && int64(1)<<(over+1)-1 <= l[budget.VarBytes] {
		over++
	}
	cs = append(cs, pair("var_doubling", "MaxVarBytes", Degraded, bombLevels(over), bombLevels(max(over-1, 0)))...)

	nested := func(n int) func() []byte {
		return func() []byte {
			return doc(style(repeat("@media screen{", n)+"p{color:red}"+repeat("}", n)), "<p>x</p>")
		}
	}
	cs = append(cs, pair("css_nesting", "css-nesting", Degraded, nested(maxNesting+1), nested(maxNesting))...)
	cs = append(cs, Case{Name: "css_nesting_1000", Limit: "css-nesting", Want: Degraded, HTML: nested(1000)})

	for i, css := range malformedCSS {
		cs = append(cs, Case{
			Name: fmt.Sprintf("css_malformed_%d", i), Want: Robust,
			HTML: func() []byte { return doc(style(css), "<p>x</p>") },
		})
	}
	return cs
}

var malformedCSS = []string{
	"p{color:red",
	"}}}} p{color:red} {{{{",
	`p{content:"unterminated}`,
	"@media",
	"@media screen",
	"@media screen{p{color:red}",
	"@page{@bottom-left{content:",
	"p{background:url(",
	"p{color:red} /* never closed",
	"p{color:;;;: ; :red}",
	"@@@ p{{{ color }}}",
	"p::before{content:var(--",
	"a[href=](x){color:red}",
	"\\",
	"p{width:calc(1px +)}",
}

func layoutCases(l budget.Limits) []Case {
	var cs []Case
	word := func(n int64) func() []byte {
		return func() []byte { return doc("", "<p>"+repeat("A", int(n))+"</p>") }
	}
	maxGlyphs := l[budget.Glyphs]
	cs = append(cs, pair("glyphs_word", "MaxGlyphs", Limit, word(maxGlyphs+1), word(maxGlyphs))...)

	uniqueStyles := func(n int64) func() []byte {
		return func() []byte {
			var b strings.Builder
			for i := range n {
				fmt.Fprintf(&b, `<i style="width:%dpx"></i>`, i)
			}
			return doc("", b.String())
		}
	}
	// html, head and body are styled too: a 10-style allowance for them.
	maxStyles := l[budget.Styles]
	cs = append(cs, pair("styles_unique", "MaxStyles", Limit, uniqueStyles(maxStyles+1), uniqueStyles(maxStyles-10))...)

	// Generated content triples the boxes of each element; without it the
	// document only degrades with warnings. The twin stays at a sixth of the
	// limit because the boxes per element (3 to 5) depend on the box tree.
	pseudo := func(n int64) func() []byte {
		return func() []byte {
			return doc(style(`i::before,i::after{content:"x";display:block}`), repeat("<i></i>", int(n)))
		}
	}
	cs = append(cs, pair("boxes_pseudo", "MaxBoxes", LimitOrDegraded,
		pseudo(l[budget.Nodes]*9/10), pseudo(l[budget.Boxes]/6))...)

	// A blank block taller than the pages: A4 with 1 in margins holds 930 px,
	// so the twin fills Pages-1 pages and the over case far more than Pages.
	tall := func(px int64) func() []byte {
		return func() []byte {
			return doc(style("@page{size:A4;margin:1in}"), fmt.Sprintf(`<div style="height:%dpx"></div>`, px))
		}
	}
	maxPages := l[budget.Pages]
	cs = append(cs, pair("pages_tall_block", "MaxPages", Limit,
		tall((maxPages+10)*1123), tall((maxPages-1)*930))...)

	rows := func(n int64) func() []byte {
		return func() []byte {
			var b strings.Builder
			b.WriteString("<table>")
			for range n {
				b.WriteString("<tr><td>1</td></tr>")
			}
			b.WriteString("</table>")
			return doc("", b.String())
		}
	}
	// Row height depends on the UA stylesheet (18 to 30 px): a third of the
	// rows stays under MaxPages whatever it is.
	overRows := l[budget.Nodes] * 9 / 10 / 3
	cs = append(cs, pair("pages_table_rows", "MaxPages", Limit, rows(overRows), rows(overRows/3))...)

	// The rightmost compound selects every <i>; "~" then scans the previous
	// siblings once per selector: about n²/2 units per selector.
	siblings := func(n, sel int64) func() []byte {
		return func() []byte {
			var css strings.Builder
			for i := range sel {
				if i > 0 {
					css.WriteByte(',')
				}
				fmt.Fprintf(&css, "b%d~i", i)
			}
			css.WriteString("{color:red}")
			return doc(style(css.String()), repeat("<i></i>", int(n)))
		}
	}
	// The twin spends about a quarter of MaxWork by the §6.2 accounting, which
	// leaves room for the ancestor steps and candidate charges it adds.
	workUnder := int64(100)
	underN := isqrt(2 * (l[budget.Work] / 4) / workUnder)
	cs = append(cs, pair("work_sibling_scan", "MaxWork", Limit,
		siblings(l[budget.Nodes]*9/10, l[budget.Selectors]), siblings(underN, workUnder))...)

	cells := func(n int) string { return repeat("<td>x</td>", n) }
	table := func(cols int, spanCols int) func() []byte {
		return func() []byte {
			return doc("", fmt.Sprintf(`<table><tr>%s</tr><tr><td colspan="%d">y</td></tr></table>`, cells(cols), spanCols))
		}
	}
	cs = append(cs, pair("colspan", "colspan", Degraded, table(maxColumns, maxColumns+1), table(maxColumns, maxColumns))...)
	cs = append(cs, pair("table_columns", "table-columns", LimitOrDegraded, table(maxColumns+1, 1), table(maxColumns, 1))...)
	return cs
}

func valueCases() []Case {
	var cs []Case
	margin := func(px string) func() []byte {
		return func() []byte { return doc("", `<p style="margin-left:`+px+`">x</p>`) }
	}
	cs = append(cs, pair("length", "length", Degraded, margin("10000001px"), margin("10000000px"))...)
	cs = append(cs, Case{
		Name: "font_size_1e30", Limit: "length", Want: Degraded,
		HTML: func() []byte { return doc(style("p{font-size:1e30px}"), "<p>x</p>") },
	})

	page := func(css string) func() []byte {
		return func() []byte { return doc(style("@page{"+css+"}"), "<p>x</p>") }
	}
	cs = append(cs, pair("page_size_max", "page-size", Degraded, page("size:201in 201in"), page("size:200in 200in"))...)
	cs = append(cs, pair("page_size_min", "page-size", Degraded, page("size:0.99in 0.99in"), page("size:1in 1in"))...)
	cs = append(cs, Case{Name: "page_size_zero", Limit: "page-size", Want: Degraded, HTML: page("size:0")})
	// 3.6 in margins leave 1.07 in of A4's width; 50 in leave nothing.
	cs = append(cs, pair("page_margins", "page-margins", Degraded, page("size:A4;margin:50in"), page("size:A4;margin:3.6in"))...)

	gradient := func(stops int) func() []byte {
		return func() []byte {
			var b strings.Builder
			b.WriteString("p{background:linear-gradient(")
			for i := range stops {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "rgb(%d,0,0)", i)
			}
			b.WriteString(")}")
			return doc(style(b.String()), "<p>x</p>")
		}
	}
	cs = append(cs, pair("gradient_stops", "gradient-stops", Degraded, gradient(maxStops+1), gradient(maxStops))...)

	link := func(n int) func() []byte {
		return func() []byte { return doc("", `<a href="`+longLink(n)+`">x</a>`) }
	}
	cs = append(cs, linkCase())
	cs = append(cs, pair("link_target", "link-length", Degraded, link(maxLinkBytes+1), link(maxLinkBytes))...)

	for _, tc := range TextCases() {
		cs = append(cs, Case{Name: "text_" + tc.Name, Want: Success, HTML: func() []byte { return TextDocument(tc.Raw) }})
	}
	return cs
}

func longLink(n int) string {
	const prefix = "http://a/"
	return prefix + repeat("a", n-len(prefix))
}

// LinkCase is an href and whether design §8.2 lets it through.
type LinkCase struct {
	Href    string
	Allowed bool
}

// Links returns the href values of design §8.2 with their expected verdict.
// Lengths are those after percent-encoding; the 2 KiB boundary is covered by
// the link_target cases.
func Links() []LinkCase {
	return []LinkCase{
		{"http://example.com/a", true},
		{"https://example.com/a b", true},
		{"HTTPS://example.com/", true},
		{"mailto:a@example.com", true},
		{"  https://example.com/  ", true},
		{"\x01https://example.com/", true},
		{"https://example.com/é", true},
		{"javascript:alert(1)", false},
		{"JaVaScRiPt:alert(1)", false},
		{"\x01javascript:alert(1)", false},
		{" \tjavascript:alert(1)", false},
		{"java\tscript:alert(1)", false},
		{"java\x00script:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"vbscript:msgbox(1)", false},
		{"file:///etc/passwd", false},
		{"ftp://example.com/", false},
		{"//example.com/", false},
		{"https://exa\x07mple.com/", false},
	}
}

func linkCase() Case {
	return Case{Name: "links_schemes", Limit: "blocked-link", Want: Degraded, HTML: func() []byte {
		var b strings.Builder
		for i, l := range Links() {
			fmt.Fprintf(&b, "<p><a href=\"%s\">link %d</a></p>", strings.ReplaceAll(l.Href, `"`, "&quot;"), i)
		}
		return doc("", b.String())
	}}
}

// TextCase is a raw text and the text design §8.4 expects after sanitation.
// NUL is absent: the HTML parser drops it before sanitation. Every other Cc
// rune except tab, LF and CR is listed, DEL included.
type TextCase struct {
	Name, Raw, Want string
}

// TextCases covers each family of design §8.4: the nine bidi controls drawn as
// markers, dropped zero-width and format characters (ZWNJ and ZWJ kept), C0 and C1 controls and
// invalid UTF-8 replaced by U+FFFD, tab, LF and CR kept. Only single invalid
// bytes are listed: the number of U+FFFD for a malformed multi-byte sequence
// is not fixed by the design (the invalid_utf8_document case covers those).
func TextCases() []TextCase {
	var cs []TextCase
	bidi := []struct {
		r    rune
		name string
	}{
		{0x202a, "LRE"},
		{0x202b, "RLE"},
		{0x202c, "PDF"},
		{0x202d, "LRO"},
		{0x202e, "RLO"},
		{0x2066, "LRI"},
		{0x2067, "RLI"},
		{0x2068, "FSI"},
		{0x2069, "PDI"},
	}
	for _, b := range bidi {
		cs = append(cs, TextCase{"bidi_" + b.name, "a" + string(b.r) + "b", "a⟨" + b.name + "⟩b"})
	}
	cs = append(cs, TextCase{"rlo_filename", "invoice" + string(rune(0x202e)) + "txt.exe", "invoice\u27e8RLO\u27e9txt.exe"})

	for _, r := range runeRange([2]rune{0x200b, 0x200f}, [2]rune{0x2060, 0x2064}, [2]rune{0xfeff, 0xfeff}, [2]rune{0xad, 0xad}) {
		if r == 0x200c || r == 0x200d {
			// ZWNJ and ZWJ shape Persian, Indic and emoji sequences: kept.
			cs = append(cs, TextCase{fmt.Sprintf("kept_U+%04X", r), "a" + string(r) + "b", "a" + string(r) + "b"})
			continue
		}
		cs = append(cs, TextCase{fmt.Sprintf("dropped_U+%04X", r), "a" + string(r) + "b", "ab"})
	}
	for _, r := range runeRange([2]rune{0x01, 0x08}, [2]rune{0x0b, 0x0c}, [2]rune{0x0e, 0x1f}, [2]rune{0x7f, 0x9f}) {
		cs = append(cs, TextCase{fmt.Sprintf("control_U+%04X", r), "a" + string(r) + "b", "a�b"})
	}
	cs = append(cs,
		TextCase{"whitespace_controls_kept", "a\tb\nc", "a\tb\nc"},
		TextCase{"invalid_lone_continuation", "a\x80b", "a�b"},
		TextCase{"invalid_lone_byte_ff", "a\xffb", "a\ufffdb"},
		TextCase{"invalid_lone_byte_fe", "a\xfeb", "a\ufffdb"},
		TextCase{"valid_untouched", "é世\U0001F600", "é世\U0001F600"},
	)
	return cs
}

// runeRange expands inclusive [lo, hi] pairs.
func runeRange(pairs ...[2]rune) []rune {
	var out []rune
	for _, p := range pairs {
		for r := p[0]; r <= p[1]; r++ {
			out = append(out, r)
		}
	}
	return out
}

// TextDocument wraps raw text in a paragraph; the bytes of raw are kept as is.
func TextDocument(raw string) []byte {
	return []byte("<html><body><p>" + raw + "</p></body></html>")
}

func robustCases() []Case {
	garbage := func(n int) func() []byte {
		return func() []byte {
			out := make([]byte, 0, n+sha256.Size)
			for i := 0; len(out) < n; i++ {
				sum := sha256.Sum256(fmt.Appendf(nil, "garbage-%d", i))
				out = append(out, sum[:]...)
			}
			return out[:n]
		}
	}
	return []Case{
		{Name: "empty_document", Want: Robust, HTML: func() []byte { return nil }},
		{Name: "whitespace_document", Want: Robust, HTML: func() []byte { return []byte(" \n\t ") }},
		{Name: "binary_garbage", Want: Robust, HTML: garbage(64 << 10)},
		{Name: "pdf_header_garbage", Want: Robust, HTML: func() []byte { return append([]byte("%PDF-1.7\n"), garbage(4096)()...) }},
		{Name: "invalid_utf8_document", Want: Robust, HTML: func() []byte {
			return []byte("<html><body><p>\xff\xfe\xc3\x28 \xf0\x9f\x98 \xed\xa0\x80</p></body></html>")
		}},
		{Name: "unclosed_tags", Want: Robust, HTML: func() []byte { return []byte("<html><body><table><tr><td><b><i><p") }},
	}
}
