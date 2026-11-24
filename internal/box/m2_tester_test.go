package box

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// xorshift is a fixed, dependency-free generator: the documents are the same on
// every run.
type xorshift struct{ s uint64 }

func (x *xorshift) Intn(n int) int {
	x.s ^= x.s << 13
	x.s ^= x.s >> 7
	x.s ^= x.s << 17
	return int(x.s>>33&0x7fffffff) % n
}

func boxText(root *Box) string {
	var sb strings.Builder
	walkBoxes(root, func(b *Box) {
		if b.Kind == KindText {
			sb.WriteString(b.Text)
		}
	})
	return sb.String()
}

func letters(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

// Flex and grid containers drop the runs of white space only and keep every
// other run, including a br and preserved text.
func TestFlexGridBlankRuns(t *testing.T) {
	for _, tc := range []struct{ name, html, want string }{
		{"br is content", `<div ` + flex + `> <br> </div>`, lines("block div", "  anon-block", "    br")},
		{"pre newline only", "<div style=\"display:grid;white-space:pre\">\n\n<p>x</p>\n</div>", lines("block div", "  block p", "    text \"x\"")},
		{"nbsp is not blank", "<div " + flex + ">&nbsp;</div>", lines("block div", "  anon-block", `    text "\u00a0"`)},
		{"text then block", `<div ` + grid + `>a<p>b</p></div>`, lines("block div", "  anon-block", "    text \"a\"", "  block p", "    text \"b\"")},
		{"nested flex in grid", `<div ` + grid + `> <div ` + flex + `> x </div> </div>`, lines("block div", "  block div", "    anon-block", "      text \"x\"")},
		{"empty container", `<div ` + flex + `></div>`, "block div"},
		{"display none child", `<div ` + flex + `><b style="display:none">h</b> </div>`, "block div"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := buildHTML(t, tc.html)
			if got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// Nesting just under the HTML depth limit (512) gives a valid tree that keeps
// its text; the parser refuses deeper documents before the box builder runs.
func TestM2DeepNesting(t *testing.T) {
	const n = 500
	for _, tc := range []struct{ name, open, close string }{
		{"inline-block", `<span ` + ib + `>`, `</span>`},
		{"flex", `<div ` + flex + `>`, `</div>`},
		{"grid", `<div ` + grid + `>`, `</div>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := strings.Repeat(tc.open, n) + "deep" + strings.Repeat(tc.close, n)
			root, _, err := buildDoc(t, html, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkTree(t, root)
			if got := boxText(root); got != "deep" {
				t.Errorf("text %q, want deep", got)
			}
		})
	}
}

// text-transform never panics, keeps the text well formed and bounded.
func TestTextTransformHostile(t *testing.T) {
	grow := strings.Repeat("ɐ", 4000) // 2 bytes, upper case is 3
	for _, tc := range []struct{ name, text, tr string }{
		{"growing upper", grow, "uppercase"},
		{"shrinking lower", strings.Repeat("Ⱥ", 4000), "lowercase"},
		{"invalid utf-8", "a\xffb\xc0", "uppercase"},
		{"nul", "a\x00b", "uppercase"},
		{"combining", "éßİ", "lowercase"},
		{"astral", "\U00010428\U00010400", "uppercase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, err := buildDoc(t, `<p style="text-transform:`+tc.tr+`">`+tc.text+`</p>`, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkTree(t, root)
			got := boxText(root)
			if !utf8.ValidString(got) {
				t.Errorf("invalid UTF-8 after the transform: %q", got)
			}
			if len(got) > 2*len(tc.text)+8 {
				t.Errorf("text grew from %d to %d bytes", len(tc.text), len(got))
			}
		})
	}
	if got, _ := buildHTML(t, `<p style="text-transform:uppercase">straße</p>`); got != lines("block p", "  text \"STRAßE\"") {
		t.Errorf("ß: %s", got)
	}
}

// No text is lost or invented by flex, grid and inline-block boxes: the text
// of the boxes is the text of the source, case and white space aside, whatever
// the nesting of display values.
func TestM2NoTextLoss(t *testing.T) {
	displays := []string{"block", "inline", "inline-block", "flex", "grid", "none-not", "table"}
	rng := &xorshift{7}
	for i := 0; i < 300; i++ {
		var sb strings.Builder
		var want strings.Builder
		n := 0
		var gen func(depth int)
		gen = func(depth int) {
			for k := rng.Intn(4); k >= 0; k-- {
				switch rng.Intn(4) {
				case 0, 1:
					w := fmt.Sprintf("w%dx", n)
					n++
					if rng.Intn(3) == 0 {
						w = "É" + w
					}
					sb.WriteString(" " + w + " ")
					want.WriteString(w)
				case 2:
					if depth > 0 {
						d := displays[rng.Intn(len(displays))]
						tag := "div"
						if d == "inline" || d == "inline-block" {
							tag = "span"
						}
						if d == "table" {
							sb.WriteString("<table><tr><td>")
							gen(depth - 1)
							sb.WriteString("</td></tr></table>")
							continue
						}
						if d == "none-not" {
							d = "contents-ignored"
						}
						fmt.Fprintf(&sb, `<%s style="display:%s">`, tag, d)
						gen(depth - 1)
						fmt.Fprintf(&sb, `</%s>`, tag)
					}
				case 3:
					sb.WriteString("<br>")
				}
			}
		}
		gen(4)
		html := sb.String()
		root, _, err := buildDoc(t, html, nil)
		if err != nil {
			t.Fatalf("%s: %v", html, err)
		}
		checkTree(t, root)
		if got, w := letters(boxText(root)), letters(want.String()); got != w {
			t.Fatalf("text changed\nhtml: %s\ngot:  %q\nwant: %q", html, got, w)
		}
	}
}
