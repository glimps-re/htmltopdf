package box

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func buildDoc(tb testing.TB, html string, mod func(*budget.Limits)) (*Box, *budget.Budget, error) {
	tb.Helper()
	b := testBudget(mod)
	d := parseDoc(tb, html, b)
	root, err := Build(d, newStyler(tb, d, b), b)
	return root, b, err
}

func walkBoxes(b *Box, fn func(*Box)) {
	fn(b)
	for _, c := range b.Children {
		walkBoxes(c, fn)
	}
}

func hrefs(b *Box) (n int) {
	walkBoxes(b, func(x *Box) {
		if x.Href != "" {
			n++
		}
	})
	return n
}

// Dangerous link targets end with no Href on any box and one blocked-link
// warning, through the whole pipeline.
func TestDangerousLinksThroughBuild(t *testing.T) {
	for name, href := range map[string]string{
		"mixed case":             "JaVaScRiPt:alert(1)",
		"control prefix":         "\x01javascript:alert(1)",
		"tab inside":             "java\tscript:alert(1)",
		"newline inside":         "java\nscript:alert(1)",
		"leading spaces":         "   javascript:alert(1)",
		"entity colon":           "javascript&colon;alert(1)",
		"data":                   "DATA:text/html;base64,AAAA",
		"vbscript":               "vbscript:msgbox(1)",
		"file":                   "FILE:///etc/passwd",
		"relative":               "//evil.example/x",
		"c1 control":             "\u0085javascript:x",
		"nul":                    "java\x00script:x",
		"unicode whitespace":     " javascript:x",
		"scheme starts with dig": "1http://x",
	} {
		t.Run(name, func(t *testing.T) {
			html := `<p><a href="` + strings.ReplaceAll(href, `"`, "&quot;") + `">go <b>bold</b> <span>s</span></a> after</p>`
			root, b, err := buildDoc(t, html, nil)
			if err != nil {
				t.Fatal(err)
			}
			if n := hrefs(root); n != 0 {
				var buf bytes.Buffer
				_ = Dump(&buf, root)
				t.Fatalf("%d boxes keep an Href:\n%s", n, buf.String())
			}
			found := false
			for _, w := range warnings(b) {
				found = found || strings.HasPrefix(w, string(budget.WarnBlockedLink)+":")
			}
			if !found && name != "unicode whitespace" {
				t.Errorf("no blocked-link warning: %v", warnings(b))
			}
		})
	}
	t.Run("blocked link nested in a good link", func(t *testing.T) {
		root, _, err := buildDoc(t, `<a href="https://ok.example/"><span>x</span><a href="javascript:x">y</a></a>`, nil)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		walkBoxes(root, func(x *Box) {
			if x.Kind == KindText {
				texts = append(texts, x.Text+"|"+x.Href)
			}
		})
		t.Logf("%v", texts)
		for _, s := range texts {
			if strings.Contains(s, "javascript") {
				t.Errorf("a javascript target survives: %q", s)
			}
		}
	})
	t.Run("link around a block", func(t *testing.T) {
		root, _, err := buildDoc(t, `<a href="JAVASCRIPT:1"><div>x<p>y</p></div></a>`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if n := hrefs(root); n != 0 {
			t.Errorf("%d boxes keep an Href", n)
		}
	})
}

// A 2 MiB href is refused with bounded allocation.
func TestHugeHrefIsBounded(t *testing.T) {
	for name, val := range map[string]string{
		"ascii":      "https://e.example/" + strings.Repeat("a", 2<<20),
		"non ascii":  "https://e.example/" + strings.Repeat("é", 1<<20),
		"spaces":     "https://e.example/" + strings.Repeat(" ", 2<<20),
		"just under": "https://e.example/" + strings.Repeat("a", maxHrefBytes-len("https://e.example/")),
		"just over":  "https://e.example/" + strings.Repeat("a", maxHrefBytes-len("https://e.example/")+1),
	} {
		t.Run(name, func(t *testing.T) {
			b := testBudget(nil)
			d := parseDoc(t, `<a href="`+val+`">x</a><a href="`+val+`">y</a>`, b)
			st := newStyler(t, d, b)
			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)
			start := time.Now()
			root, err := Build(d, st, b)
			took := time.Since(start)
			runtime.ReadMemStats(&m1)
			if err != nil {
				t.Fatal(err)
			}
			alloc := m1.TotalAlloc - m0.TotalAlloc
			kept := 0
			walkBoxes(root, func(x *Box) { kept = max(kept, len(x.Href)) })
			t.Logf("%d byte href: allocated %d KiB in %v, kept %d bytes", len(val), alloc>>10, took.Round(time.Microsecond), kept)
			if kept > maxHrefBytes {
				t.Errorf("kept %d bytes", kept)
			}
			if limit := uint64(4*len(val) + 1<<20); alloc > limit {
				t.Errorf("allocated %d KiB for two %d KiB hrefs", alloc>>10, len(val)>>10)
			}
			// Trailing white space is trimmed: the "spaces" href is a short valid one.
			wantKept := name == "just under" || name == "spaces"
			if wantKept != (kept > 0) {
				t.Errorf("kept %d bytes, wantKept %v", kept, wantKept)
			}
		})
	}
}

// Nested inline elements around blocks: every block re-opens the open inlines,
// so the box count grows with depth x blocks. LimitBoxes ends it; the twin at
// the exact count succeeds.
func TestSpanNestingAroundBlocksIsBounded(t *testing.T) {
	doc := func(depth, blocks int) string {
		return strings.Repeat("<span>", depth) + strings.Repeat("<div>x</div>", blocks) + strings.Repeat("</span>", depth)
	}
	for _, tc := range []struct{ depth, blocks int }{{5, 5}, {50, 20}, {400, 10}} {
		html := doc(tc.depth, tc.blocks)
		_, b0, err := buildDoc(t, html, nil)
		if err != nil {
			t.Fatal(err)
		}
		n := b0.Used(budget.Boxes)
		for _, c := range []struct {
			name  string
			limit int64
			ok    bool
		}{{"over by one", n - 1, false}, {"at the limit", n, true}, {"just under", n + 1, true}} {
			_, _, err := buildDoc(t, html, func(l *budget.Limits) { l[budget.Boxes] = c.limit })
			var le *budget.LimitError
			if c.ok != (err == nil) || (!c.ok && (!errors.As(err, &le) || le.Kind != budget.Boxes)) {
				t.Errorf("depth %d x %d blocks, %s (%d boxes): %v", tc.depth, tc.blocks, c.name, n, err)
			}
		}
	}
	// 400 spans around 100 000 blocks: 40 million boxes without the limit.
	html := doc(400, 100_000)
	b := testBudget(nil)
	d := parseDoc(t, html, b)
	st := newStyler(t, d, b)
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	start := time.Now()
	root, err := Build(d, st, b)
	took := time.Since(start)
	runtime.ReadMemStats(&m1)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Boxes || root != nil {
		t.Fatalf("err = %v", err)
	}
	t.Logf("400 spans x 100 000 blocks: LimitBoxes after %v, %d MiB allocated", took.Round(time.Millisecond), (m1.TotalAlloc-m0.TotalAlloc)>>20)
	if got := b.Used(budget.Boxes); got > budget.Defaults()[budget.Boxes]+1 {
		t.Errorf("%d boxes charged", got)
	}
}

func textBytes(b *Box) (n int) {
	walkBoxes(b, func(x *Box) { n += len(x.Text) })
	return n
}

func TestPreWithTabs(t *testing.T) {
	for name, html := range map[string]string{
		"2 MiB of tabs":            "<pre>" + strings.Repeat("\t", 2<<20) + "</pre>",
		"tabs and newlines":        "<pre>" + strings.Repeat("\t\n", 1<<20) + "</pre>",
		"tabs after text":          "<pre>" + strings.Repeat("a\t", 1<<20) + "</pre>",
		"pre-wrap tabs":            `<div style="white-space:pre-wrap">` + strings.Repeat("\t", 2<<20) + "</div>",
		"CRLF":                     "<pre>" + strings.Repeat("a\r\n", 700_000) + "</pre>",
		"tabs in inline":           "<pre>" + strings.Repeat("<b>\t</b>", 200_000) + "</pre>",
		"2 MiB of spaces in pre":   "<pre>" + strings.Repeat(" ", 2<<20) + "</pre>",
		"2 MiB of newlines in pre": "<pre>" + strings.Repeat("\n", 2<<20) + "</pre>",
	} {
		t.Run(name, func(t *testing.T) {
			b := testBudget(nil)
			d := parseDoc(t, html, b)
			st := newStyler(t, d, b)
			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)
			start := time.Now()
			root, err := Build(d, st, b)
			took := time.Since(start)
			runtime.ReadMemStats(&m1)
			var le *budget.LimitError
			if err != nil && (!errors.As(err, &le)) {
				t.Fatal(err)
			}
			alloc := (m1.TotalAlloc - m0.TotalAlloc) >> 20
			size := 0
			if root != nil {
				size = textBytes(root)
				checkTree(t, root)
			}
			t.Logf("err %v, %d boxes budget, text %d KiB, allocated %d MiB, %v", err != nil, b.Used(budget.Boxes), size>>10, alloc, took.Round(time.Millisecond))
			if size > 8*len(html) {
				t.Errorf("%d bytes of text out of %d bytes of input", size, len(html))
			}
			if alloc > 128 {
				t.Errorf("allocated %d MiB", alloc)
			}
		})
	}
}

// The tree holds nothing of the DOM: the element and text arrays are
// collectable once Build has returned.
func TestDOMIsReleasedAfterBuild(t *testing.T) {
	var wEl weak.Pointer[dom.Element]
	var wTx weak.Pointer[dom.Text]
	build := func() *Box {
		b := testBudget(nil)
		d := parseReport(t, "00-host-connector", "small", b)
		wEl = weak.Make(&d.Elements[0])
		wTx = weak.Make(&d.Texts[0])
		root, err := Build(d, newStyler(t, d, b), b)
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	root := build()
	for range 4 {
		runtime.GC()
	}
	if wEl.Value() != nil {
		t.Error("the DOM elements are still reachable after Build")
	}
	if wTx.Value() != nil {
		t.Error("the DOM texts are still reachable after Build")
	}
	if countBoxes(root) < 100 {
		t.Fatal("tree too small")
	}
	runtime.KeepAlive(root)
}

func TestForeignContentIsSkippedWithAWarning(t *testing.T) {
	for name, html := range map[string]string{
		"svg":                  `<p>a<svg width=10><circle r=1></circle><text>t</text></svg>b</p>`,
		"math":                 `<p>a<math><mi>x</mi><mo>+</mo></math>b</p>`,
		"svg in a link":        `<a href="https://e.example/">a<svg><text>t</text></svg>b</a>`,
		"nested foreignObject": `<svg><foreignObject><div>deep<p>x</p></div></foreignObject></svg>`,
		"svg between blocks":   `<div>a</div><svg><g><g><text>t</text></g></g></svg><div>b</div>`,
		"many svgs":            strings.Repeat("<svg></svg>", 2000),
	} {
		t.Run(name, func(t *testing.T) {
			root, b, err := buildDoc(t, html, nil)
			if err != nil {
				t.Fatal(err)
			}
			walkBoxes(root, func(x *Box) {
				switch x.Tag.String() {
				case "svg", "math", "circle", "mi", "mo", "g", "foreignobject":
					t.Errorf("box for foreign element %s", x.Tag)
				}
				if strings.Contains(x.Text, "t") && x.Kind == KindText && strings.TrimSpace(x.Text) == "t" {
					t.Errorf("text of a foreign element kept: %q", x.Text)
				}
			})
			ws := warnings(b)
			found := false
			for _, w := range ws {
				found = found || strings.HasPrefix(w, string(budget.WarnUnsupportedElement)+":")
			}
			if !found {
				t.Errorf("no unsupported-element warning: %v", ws)
			}
			if len(ws) > 256 {
				t.Errorf("%d warnings", len(ws))
			}
		})
	}
}

// collapse is the oracle of white-space: normal on inline content: text and
// <br> (\x00) only.
func collapse(s string) string {
	var sb strings.Builder
	space := true
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f':
			if !space {
				sb.WriteByte(' ')
				space = true
			}
		case 0:
			str := sb.String()
			sb.Reset()
			sb.WriteString(strings.TrimSuffix(str, " "))
			sb.WriteByte(0)
			space = true
		default:
			sb.WriteRune(r)
			space = false
		}
	}
	return strings.TrimSuffix(sb.String(), " ")
}

// Random inline content: the text of the box tree equals the CSS collapsing of
// the source across inline boundaries, <br> and CR LF.
func TestWhiteSpaceDifferential(t *testing.T) {
	pieces := []string{"a", "b", " ", "  ", "\n", "\t", "\r\n", "\r", " x ", "yz", "&nbsp;", " "}
	tags := []string{"b", "i", "span", "a href=\"https://e.example/\"", "em"}
	rng := newRNG(5)
	for round := range 600 {
		var src, want strings.Builder
		var gen func(depth int)
		gen = func(depth int) {
			for range 1 + rng.n(5) {
				switch k := rng.n(8); {
				case k < 4:
					p := pieces[rng.n(len(pieces))]
					src.WriteString(p)
					want.WriteString(strings.ReplaceAll(p, "&nbsp;", " "))
				case k == 4:
					src.WriteString("<br>")
					want.WriteString("\x00")
				case depth < 4:
					tag := tags[rng.n(len(tags))]
					src.WriteString("<" + tag + ">")
					gen(depth + 1)
					src.WriteString("</" + strings.Fields(tag)[0] + ">")
				}
			}
		}
		gen(0)
		root, _, err := buildDoc(t, "<p>"+src.String()+"</p>", nil)
		if err != nil {
			t.Fatal(err)
		}
		var got strings.Builder
		walkBoxes(root, func(x *Box) {
			switch x.Kind {
			case KindText:
				got.WriteString(x.Text)
			case KindLineBreak:
				got.WriteByte(0)
			}
		})
		expected := strings.ReplaceAll(collapse(strings.ReplaceAll(want.String(), " ", "\x01")), "\x01", " ")
		if got.String() != expected {
			t.Fatalf("round %d: <p>%s</p>\n got  %q\n want %q", round, src.String(), got.String(), expected)
		}
	}
}

type rng uint64

func newRNG(seed uint64) *rng { r := rng(seed*0x9E3779B97F4A7C15 | 1); return &r }

func (r *rng) n(m int) int {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return int(min((uint64(*r)>>11)%uint64(max(m, 1)), 1<<31))
}

func TestWhiteSpaceDumps(t *testing.T) {
	for _, tt := range []struct{ name, html, want string }{
		{"CRLF inside a paragraph", "<p>a\r\nb</p>", lines("block p", `  text "a b"`)},
		{"lone CR", "<p>a\rb</p>", lines("block p", `  text "a b"`)},
		{"spaces around br in inlines", "<p><b>a </b><br> <i> b</i></p>", lines("block p", "  inline b", `    text "a"`, "  br", "  inline i", `    text "b"`)},
		{"collapse over three inlines", "<p>a <b> <i> <u> b</u></i></b></p>", lines("block p", `  text "a "`, "  inline b", "    inline i", "      inline u", `        text "b"`)},
		{"pre-line", `<p style="white-space:pre-line">a  b` + "\n" + ` c</p>`, ""},
		{"nowrap collapses", `<p style="white-space:nowrap">a   b</p>`, lines("block p", `  text "a b"`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := buildHTML(t, tt.html)
			if tt.want == "" {
				t.Logf("dump:\n%s", got)
				return
			}
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
