package box

import (
	"reflect"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/style"
)

func lines(ls ...string) string { return strings.Join(ls, "\n") }

func TestBuildStructure(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"empty body", "", ""},
		{"inline in block", "<p>Hello <b>big</b> world</p>", lines(
			"block p",
			"  text \"Hello \"",
			"  inline b",
			"    text \"big\"",
			"  text \" world\"")},
		{"text directly in body", "just text", `text "just text"`},
		{"mixed children get anonymous blocks", "<div>a<p>b</p>c</div>", lines(
			"block div",
			"  anon-block",
			"    text \"a\"",
			"  block p",
			"    text \"b\"",
			"  anon-block",
			"    text \"c\"")},
		{"white space between blocks is dropped", "<div>\n <p>a</p>\n <p>b</p>\n</div>", lines(
			"block div",
			"  block p",
			"    text \"a\"",
			"  block p",
			"    text \"b\"")},
		{"inline between blocks", "<div><p>a</p><b>x</b><p>b</p></div>", lines(
			"block div",
			"  block p",
			"    text \"a\"",
			"  anon-block",
			"    inline b",
			"      text \"x\"",
			"  block p",
			"    text \"b\"")},
		{"nested blocks", "<div><div><p>x</p></div></div>", lines(
			"block div",
			"  block div",
			"    block p",
			"      text \"x\"")},
		{"list items are blocks", "<ul><li>a</li><li>b</li></ul>", lines(
			"block ul",
			"  block li",
			"    text \"a\"",
			"  block li",
			"    text \"b\"")},
		{"table parts are blocks for now", "<table><tbody><tr><td>x</td></tr></tbody></table>", lines(
			"block table",
			"  block tbody",
			"    block tr",
			"      block td",
			"        text \"x\"")},
		{"display none", `<p>a<span style="display:none">x<b>y</b></span>b</p>`, lines(
			"block p",
			"  text \"a\"",
			"  text \"b\"")},
		{"display none keeps nothing", `<div style="display:none"><p>x</p></div><p>y</p>`, lines(
			"block p",
			"  text \"y\"")},
		{"head, script and style are not boxes", "<script>x</script><style>p{}</style><p>t</p>", lines(
			"block p",
			"  text \"t\"")},
		{"empty inline kept", "<p>a<span></span>b</p>", lines(
			"block p",
			"  text \"a\"",
			"  inline span",
			"  text \"b\"")},
		{"custom elements are inline", "<p><my-tag>x</my-tag></p>", lines(
			"block p",
			"  inline",
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

func TestBlockInInlineSplitsTheInlineBox(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"span around a div", "<span>a<div>b</div>c</span>", lines(
			"anon-block",
			"  inline span",
			"    text \"a\"",
			"block div",
			"  text \"b\"",
			"anon-block",
			"  inline span (cont)",
			"    text \"c\"")},
		{"nothing before", "<span><div>b</div>c</span>", lines(
			"block div",
			"  text \"b\"",
			"anon-block",
			"  inline span (cont)",
			"    text \"c\"")},
		{"nothing after", "<span>a<div>b</div></span>", lines(
			"anon-block",
			"  inline span",
			"    text \"a\"",
			"block div",
			"  text \"b\"")},
		{"only the block", "<span><div>b</div></span>", lines(
			"block div",
			"  text \"b\"")},
		{"nested inlines and a link", `<a href="https://x.test/"><b>1<div>2</div>3</b></a>`, lines(
			"anon-block",
			"  inline a href=\"https://x.test/\"",
			"    inline b href=\"https://x.test/\"",
			"      text href=\"https://x.test/\" \"1\"",
			"block div href=\"https://x.test/\"",
			"  text href=\"https://x.test/\" \"2\"",
			"anon-block",
			"  inline a href=\"https://x.test/\" (cont)",
			"    inline b href=\"https://x.test/\" (cont)",
			"      text href=\"https://x.test/\" \"3\"")},
		{"two blocks in one inline", "<i>a<p>b</p><p>c</p>d</i>", lines(
			"anon-block",
			"  inline i",
			"    text \"a\"",
			"block p",
			"  text \"b\"",
			"block p",
			"  text \"c\"",
			"anon-block",
			"  inline i (cont)",
			"    text \"d\"")},
		{"the id stays on the first fragment", "<span id=s>a<div>b</div>c</span>", lines(
			"anon-block",
			"  inline span #s",
			"    text \"a\"",
			"block div",
			"  text \"b\"",
			"anon-block",
			"  inline span (cont)",
			"    text \"c\"")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := buildHTML(t, tt.html); got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestWhiteSpace(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"collapse", "<p>a   b\n\tc</p>", lines("block p", `  text "a b c"`)},
		{"leading and trailing", "<p>  a  </p>", lines("block p", `  text "a"`)},
		{"only white space", "<p>   \n  </p>", `block p`},
		{"across inline boxes", "<p>a <b> b</b> <i> c </i></p>", lines(
			"block p",
			`  text "a "`,
			"  inline b",
			`    text "b"`,
			`  text " "`,
			"  inline i",
			`    text "c"`)},
		{"after a line break", "<p>a <br> b</p>", lines("block p", `  text "a"`, "  br", `  text "b"`)},
		{"two line breaks", "<p>a<br><br>b</p>", lines("block p", `  text "a"`, "  br", "  br", `  text "b"`)},
		{"line break at the end", "<p>a<br></p>", lines("block p", `  text "a"`, "  br")},
		{"nbsp is not collapsible", "<p>a&nbsp;&nbsp;b</p>", lines("block p", `  text "a\u00a0\u00a0b"`)},
		{"pre keeps spaces and breaks lines", "<pre>  a\n b\n</pre>", lines(
			"block pre", `  text "  a"`, "  br", `  text " b"`, "  br")},
		{"pre tab stops", "<pre>\tx\nab\tc</pre>", lines(
			"block pre", `  text "        x"`, "  br", `  text "ab      c"`)},
		{"pre CR LF is one break", "<pre>a\r\nb</pre>", lines("block pre", `  text "a"`, "  br", `  text "b"`)},
		{"white-space is inherited by inline boxes", "<pre>a <b> b</b></pre>", lines(
			"block pre", `  text "a "`, "  inline b", `    text " b"`)},
		{"after a block", "<p>a</p> <b> x</b>", lines("block p", `  text "a"`, "anon-block", "  inline b", `    text "x"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := buildHTML(t, tt.html); got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestIDsAreCopied(t *testing.T) {
	got, _ := buildHTML(t, `<div id=a><p id=b>x<span id=c>y</span><br id=d></p></div>`)
	want := lines(
		"block div #a",
		"  block p #b",
		`    text "x"`,
		"    inline span #c",
		`      text "y"`,
		"    br #d")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestForeignSubtreesAreSkipped(t *testing.T) {
	got, b := buildHTML(t, `<p>a<svg width=10><text>in svg</text></svg>b<math><mi>x</mi></math>c</p>`)
	want := lines("block p", `  text "a"`, "  svg svg shapes=0", `  text "b"`, `  text "c"`)
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	w := strings.Join(warnings(b), "|")
	if !strings.Contains(w, "unsupported-element:svg text") || !strings.Contains(w, "unsupported-element:math") {
		t.Fatalf("warnings %q", w)
	}
}

func TestForeignObjectContentIsSkippedToo(t *testing.T) {
	got, _ := buildHTML(t, `<svg><foreignObject><p>html in svg</p></foreignObject></svg><p>after</p>`)
	if got != lines("anon-block", "  svg svg shapes=0", "block p", `  text "after"`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestLinksInTheTree(t *testing.T) {
	tests := []struct {
		name string
		href string
		want string // the Href of the text box, "" when none
		warn string
	}{
		{"https", "https://example.com/a", "https://example.com/a", ""},
		{"mixed case scheme", "HTTPS://Example.com/A", "https://Example.com/A", ""},
		{"mailto", "mailto:a@b.c", "mailto:a@b.c", ""},
		{"javascript", "javascript:alert(1)", "", "blocked-link:scheme javascript"},
		{"mixed case javascript", "JaVaScRiPt:alert(1)", "", "blocked-link:scheme javascript"},
		{"data", "data:text/html,x", "", "blocked-link:scheme data"},
		{"file", "file:///etc/passwd", "", "blocked-link:scheme file"},
		{"relative", "/path/page.html", "", "blocked-link:no scheme"},
		{"fragment without a target", "#top", "", "blocked-link:internal link without a target"},
		{"empty", "", "", ""},
		{"spaces", "https://example.com/a b", "https://example.com/a%20b", ""},
		{"non ASCII", "https://example.com/é", "https://example.com/%C3%A9", ""},
		{"leading whitespace", "  https://example.com/  ", "https://example.com/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, b := buildHTML(t, `<a href="`+tt.href+`">go</a>`)
			var want string
			if tt.want != "" {
				want = lines(`inline a href="`+tt.want+`"`, `  text href="`+tt.want+`" "go"`)
			} else {
				want = lines("inline a", `  text "go"`)
			}
			if got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			ws := warnings(b)
			if (tt.warn == "") != (len(ws) == 0) || (tt.warn != "" && ws[0] != tt.warn) {
				t.Errorf("warnings %v, want %q", ws, tt.warn)
			}
		})
	}
}

func TestAnchorWithoutHrefIsNotALink(t *testing.T) {
	got, b := buildHTML(t, `<a name=x>t</a>`)
	if got != lines("inline a", `  text "t"`) || len(warnings(b)) != 0 {
		t.Fatalf("got:\n%s\nwarnings %v", got, warnings(b))
	}
}

func TestBoxHoldsNoDOMPointer(t *testing.T) {
	typ := reflect.TypeOf(Box{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if strings.Contains(f.Type.String(), "dom.") {
			t.Errorf("field %s has type %s", f.Name, f.Type)
		}
	}
}

func TestAnonymousStyleIsRequestedOnlyWhenNeeded(t *testing.T) {
	b := testBudget(nil)
	for html, want := range map[string]int{
		"<p>a</p><p>b</p>": 0, "<div>a<p>b</p></div>": 1, "<p>x <b>y</b></p>": 0, "<div>a<p>b</p>c<p>d</p>e</div>": 1,
	} {
		d := parseDoc(t, html, b)
		st := newStyler(t, d, b)
		if _, err := Build(d, st, b); err != nil {
			t.Fatal(err)
		}
		// One request per container with block children and loose runs.
		if st.anonymous != want {
			t.Errorf("%q: %d requests, want %d", html, st.anonymous, want)
		}
	}
}

// The resolver itself is a Styler.
var _ Styler = (*style.Resolver)(nil)

func TestAnonymousBlocksDoNotInheritBoxProperties(t *testing.T) {
	b := testBudget(nil)
	d := parseDoc(t, `<div style="margin:30px;padding:7px;border:5px solid red;background:#123456;color:#00ff00;font-size:20px">loose<p>x</p></div>`, b)
	root, err := Build(d, newStyler(t, d, b), b)
	if err != nil {
		t.Fatal(err)
	}
	div := root.Children[0].Children[0]
	anon := div.Children[0]
	if !anon.Anonymous || anon.Kind != KindBlock || anon.Style.Display != style.DisplayBlock {
		t.Fatalf("anonymous block = %+v", anon)
	}
	st := anon.Style
	if st.Margin != [4]style.Dim{} || st.Padding != [4]style.Dim{} || st.Border[style.Top].Width != 0 ||
		st.BackgroundColor.A != 0 || st.FontSize != 20 || st.Color.G != 1 {
		t.Fatalf("anonymous style: margin %v padding %v border %v background %v font %v color %v",
			st.Margin, st.Padding, st.Border[0], st.BackgroundColor, st.FontSize, st.Color)
	}
	if div.Style.Margin[style.Top].Value != 30 {
		t.Fatal("the element lost its own margin")
	}
}
