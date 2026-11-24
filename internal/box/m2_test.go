package box

import (
	"errors"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

const (
	ib    = `style="display:inline-block"`
	flex  = `style="display:flex"`
	grid  = `style="display:grid"`
	upper = `style="text-transform:uppercase"`
)

func TestInlineBlockStructure(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"in a paragraph", `<p>a <span ` + ib + `>b</span> c</p>`, lines(
			"block p",
			"  text \"a \"",
			"  inline-block span",
			"    text \"b\"",
			"  text \" c\"")},
		{"between two texts without space", `<p>a<span ` + ib + `>b</span>c</p>`, lines(
			"block p",
			"  text \"a\"",
			"  inline-block span",
			"    text \"b\"",
			"  text \"c\"")},
		{"space before a trailing inline-block is kept", `<p>a <span ` + ib + `>b</span></p>`, lines(
			"block p",
			"  text \"a \"",
			"  inline-block span",
			"    text \"b\"")},
		{"space after it is kept, then collapsed", `<p><span ` + ib + `>b</span>  <i> c</i></p>`, lines(
			"block p",
			"  inline-block span",
			"    text \"b\"",
			"  text \" \"",
			"  inline i",
			"    text \"c\"")},
		{"in a link keeps the href", `<p><a href="https://a.test/x"><span ` + ib + ` id="k">b</span></a></p>`, lines(
			"block p",
			"  inline a href=\"https://a.test/x\"",
			"    inline-block span #k href=\"https://a.test/x\"",
			"      text href=\"https://a.test/x\" \"b\"")},
		{"containing blocks", `<div><span ` + ib + `><div>a</div>t<p>b</p></span></div>`, lines(
			"block div",
			"  inline-block span",
			"    block div",
			"      text \"a\"",
			"    anon-block",
			"      text \"t\"",
			"    block p",
			"      text \"b\"")},
		{"empty one is kept", `<p>a<span ` + ib + `></span>b</p>`, lines(
			"block p",
			"  text \"a\"",
			"  inline-block span",
			"  text \"b\"")},
		{"inside an inline box", `<p><b>x<span ` + ib + `>y</span>z</b></p>`, lines(
			"block p",
			"  inline b",
			"    text \"x\"",
			"    inline-block span",
			"      text \"y\"",
			"    text \"z\"")},
		{"does not split the inline box around it", `<div><b>x<span ` + ib + `><div>y</div></span>z</b></div>`, lines(
			"block div",
			"  inline b",
			"    text \"x\"",
			"    inline-block span",
			"      block div",
			"        text \"y\"",
			"    text \"z\"")},
		{"alone in a div", `<div><span ` + ib + `>x</span></div>`, lines(
			"block div",
			"  inline-block span",
			"    text \"x\"")},
		{"nested", `<p><span ` + ib + `>a<b ` + ib + `>c</b></span></p>`, lines(
			"block p",
			"  inline-block span",
			"    text \"a\"",
			"    inline-block b",
			"      text \"c\"")},
		{"br inside", `<p><span ` + ib + `>a<br>b</span></p>`, lines(
			"block p",
			"  inline-block span",
			"    text \"a\"",
			"    br",
			"    text \"b\"")},
		{"inline-block next to a block", `<div><p>a</p><span ` + ib + `>x</span></div>`, lines(
			"block div",
			"  block p",
			"    text \"a\"",
			"  anon-block",
			"    inline-block span",
			"      text \"x\"")},
		{"blockified in a flex container", `<div ` + flex + `><span ` + ib + `>x</span></div>`, lines(
			"block div",
			"  block span",
			"    text \"x\"")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, b := buildHTML(t, tt.html)
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if w := warnings(b); len(w) != 0 {
				t.Errorf("warnings %v", w)
			}
		})
	}
}

func TestFlexGridChildren(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"text, span and div", `<div ` + flex + `>text <span>s</span><div>d</div></div>`, lines(
			"block div",
			"  anon-block",
			"    text \"text\"",
			"  block span",
			"    text \"s\"",
			"  block div",
			"    text \"d\"")},
		{"text only gets an anonymous block", `<div ` + flex + `>a b</div>`, lines(
			"block div",
			"  anon-block",
			"    text \"a b\"")},
		{"white space runs are dropped", `<div ` + grid + `> <p>a</p>
		<p>b</p> </div>`, lines(
			"block div",
			"  block p",
			"    text \"a\"",
			"  block p",
			"    text \"b\"")},
		{"only white space", `<div ` + grid + `>   ` + "\n\t" + `</div>`, "block div"},
		{"text around an item", `<div ` + flex + `>x<p>a</p>y</div>`, lines(
			"block div",
			"  anon-block",
			"    text \"x\"",
			"  block p",
			"    text \"a\"",
			"  anon-block",
			"    text \"y\"")},
		{"br is not white space", `<div ` + flex + `><br></div>`, lines(
			"block div",
			"  anon-block",
			"    br")},
		{"preserved white space only", `<div style="display:flex;white-space:pre"> ` + "\n\n" + `</div>`, "block div"},
		{"inline-block inside a nested paragraph is not blockified", `<div ` + flex + `><p>a <span ` + ib + `>b</span></p></div>`, lines(
			"block div",
			"  block p",
			"    text \"a \"",
			"    inline-block span",
			"      text \"b\"")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, b := buildHTML(t, tt.html)
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if w := warnings(b); len(w) != 0 {
				t.Errorf("warnings %v", w)
			}
		})
	}
}

func TestTextTransform(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"uppercase", `<p ` + upper + `>é ß ǆ</p>`, lines("block p", "  text \"É ß Ǆ\"")},
		{"lowercase", `<p style="text-transform:lowercase">ÉA İ</p>`, lines("block p", "  text \"éa i\"")},
		{"marker unchanged", "<p " + upper + ">a\u202eb \u27e8RLO\u27e9</p>", lines("block p", "  text \"A\u27e8RLO\u27e9B \u27e8RLO\u27e9\"")},
		{"inherited, Href and ID unchanged", `<p ` + upper + `><a id="i" href="https://a.test/Path">go <b>x</b></a></p>`, lines(
			"block p",
			"  inline a #i href=\"https://a.test/Path\"",
			"    text href=\"https://a.test/Path\" \"GO \"",
			"    inline b href=\"https://a.test/Path\"",
			"      text href=\"https://a.test/Path\" \"X\"")},
		{"reset in a child", `<p ` + upper + `>a<span style="text-transform:none">b</span></p>`, lines(
			"block p",
			"  text \"A\"",
			"  inline span",
			"    text \"b\"")},
		{"after white space processing", `<p ` + upper + `>a   b
c</p>`, lines("block p", "  text \"A B C\"")},
		{"pre", `<pre ` + upper + `>a  b
c</pre>`, lines("block pre", "  text \"A  B\"", "  br", "  text \"C\"")},
		{"in an inline-block", `<p ` + upper + `><span ` + ib + `>ab</span></p>`, lines(
			"block p",
			"  inline-block span",
			"    text \"AB\"")},
		{"trailing space still trimmed", `<p ` + upper + `>ab <br>c</p>`, lines(
			"block p",
			"  text \"AB\"",
			"  br",
			"  text \"C\"")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := buildHTML(t, tt.html)
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestNowrapCollapsesLikeNormal(t *testing.T) {
	got, _ := buildHTML(t, `<p style="white-space:nowrap">a   b
c </p>`)
	if want := lines("block p", "  text \"a b c\""); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestInlineBlockLimits(t *testing.T) {
	const n = 100000
	html := "<p>" + strings.Repeat("<i "+ib+">x</i>", n) + "</p>"
	// html, body, p, then n inline-blocks of one text box each.
	boxes := int64(3 + 2*n)
	for _, tt := range []struct {
		name  string
		limit int64
		ok    bool
	}{{"just under", boxes + 1, true}, {"at the limit", boxes, true}, {"over", boxes - 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			root, b, err := buildDoc(t, html, func(l *budget.Limits) { l[budget.Boxes] = tt.limit })
			var le *budget.LimitError
			if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.Boxes)) {
				t.Fatalf("err = %v", err)
			}
			if tt.ok {
				if got := countBoxes(root); int64(got) != boxes || b.Used(budget.Boxes) != boxes {
					t.Fatalf("%d boxes, %d charged, want %d", got, b.Used(budget.Boxes), boxes)
				}
			}
		})
	}
}

func TestFlexWrapsAreBounded(t *testing.T) {
	html := "<div " + flex + ">" + strings.Repeat("a<b>x</b>", 5000) + "</div>"
	_, _, err := buildDoc(t, html, func(l *budget.Limits) { l[budget.Boxes] = 1000 })
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Boxes {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckTreeInlineBlock(t *testing.T) {
	root, _, err := buildDoc(t, `<div `+flex+`>a <b>b</b><p>c <span `+ib+`>d<div>e</div></span></p>f</div>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkTree(t, root)
}
