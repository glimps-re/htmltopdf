package dom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/iotest"
	"unsafe"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"golang.org/x/net/html"
)

func newBudget(l budget.Limits) *budget.Budget {
	return budget.New(context.Background(), l, nil)
}

func parse(t testing.TB, src string) *Document {
	t.Helper()
	doc, err := Parse(strings.NewReader(src), newBudget(budget.Defaults()))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func elementNamed(t testing.TB, d *Document, name string) *Element {
	t.Helper()
	for i := range d.Elements {
		e := &d.Elements[i]
		if (e.Tag != 0 && e.Tag.String() == name) || e.Name == name {
			return e
		}
	}
	t.Fatalf("no element %q", name)
	return nil
}

func TestParseStructure(t *testing.T) {
	d := parse(t, `<!DOCTYPE html><html><head><title> My   big
 title </title></head><body id="top" class="  a b
	c " style="color:red" data-x="1" ID="dup">
<!-- gone --><p>one</p>text<p class=z>two</p><custom-el></custom-el></body></html>`)

	if d.Title != "My big title" {
		t.Errorf("Title = %q", d.Title)
	}
	root := d.Elements[0]
	if root.Tag.String() != "html" || root.Parent != -1 || root.SiblingCount != 1 || root.SiblingIndex != 0 || root.PrevElement != -1 {
		t.Errorf("root = %+v", root)
	}
	body := elementNamed(t, d, "body")
	if body.ID != "top" || body.Style != "color:red" || strings.Join(body.Classes, ",") != "a,b,c" {
		t.Errorf("body = %+v", body)
	}
	if len(body.Attrs) != 1 || body.Attrs[0] != (Attr{"data-x", "1"}) {
		t.Errorf("body attrs = %+v, want data-x only (a repeated id is dropped)", body.Attrs)
	}
	if v, ok := body.Attr("class"); !ok || v != "a b c" {
		t.Errorf("Attr(class) = %q, %v", v, ok)
	}
	if v, ok := body.Attr("data-x"); !ok || v != "1" {
		t.Errorf("Attr(data-x) = %q, %v", v, ok)
	}
	if _, ok := body.Attr("nope"); ok {
		t.Error("Attr(nope) found")
	}

	var kids []string
	for r := body.FirstChild; r != NoNode; r = d.NextSibling(r) {
		if i, ok := r.Element(); ok {
			e := d.Elements[i]
			name := e.Name
			if e.Tag != 0 {
				name = e.Tag.String()
			}
			kids = append(kids, fmt.Sprintf("%s[%d/%d prev=%d]", name, e.SiblingIndex, e.SiblingCount, e.PrevElement))
		} else if i, ok := r.Text(); ok {
			kids = append(kids, fmt.Sprintf("%q", d.Texts[i].Data))
		}
	}
	want := `"\n" p[0/3 prev=-1] "text" p[1/3 prev=` + fmt.Sprint(int(indexOf(d, "p", 0))) + `] custom-el[2/3 prev=` + fmt.Sprint(int(indexOf(d, "p", 1))) + `]`
	if got := strings.Join(kids, " "); got != want {
		t.Errorf("children:\n got %s\nwant %s", got, want)
	}
}

func indexOf(d *Document, tag string, nth int) int32 {
	for i := range d.Elements {
		if d.Elements[i].Tag.String() == tag {
			if nth == 0 {
				return int32(i)
			}
			nth--
		}
	}
	return -1
}

func TestParseParents(t *testing.T) {
	d := parse(t, `<div><span>a</span>b</div>`)
	div := indexOf(d, "div", 0)
	span := indexOf(d, "span", 0)
	if d.Elements[span].Parent != div {
		t.Errorf("span parent = %d, want %d", d.Elements[span].Parent, div)
	}
	if len(d.Texts) != 2 || d.Texts[0].Parent != span || d.Texts[1].Parent != div {
		t.Errorf("texts = %+v", d.Texts)
	}
	if got := d.NextSibling(NoNode); got != NoNode {
		t.Errorf("NextSibling(NoNode) = %d", got)
	}
}

func TestParseFirstTitleOnly(t *testing.T) {
	d := parse(t, `<svg><title>svg</title></svg><title>first</title><title>second</title>`)
	if d.Title != "first" {
		t.Errorf("Title = %q, want the first HTML title", d.Title)
	}
}

func TestParseSanitizesText(t *testing.T) {
	d := parse(t, "<p>a\u202Eb\u200Bc\x01d\u00ADe</p>")
	if len(d.Texts) != 1 || d.Texts[0].Data != "a\u27E8RLO\u27E9bc\uFFFDde" {
		t.Errorf("texts = %q", d.Texts)
	}
}

func TestParseDropsEmptyText(t *testing.T) {
	d := parse(t, "<p>\u200B\u200B</p>")
	if len(d.Texts) != 0 {
		t.Errorf("texts = %+v, want none", d.Texts)
	}
	if p := elementNamed(t, d, "p"); p.FirstChild != NoNode {
		t.Errorf("p.FirstChild = %d", p.FirstChild)
	}
}

func TestNodeRef(t *testing.T) {
	if i, ok := ElementRef(5).Element(); !ok || i != 5 {
		t.Error("ElementRef round trip")
	}
	if _, ok := ElementRef(5).Text(); ok {
		t.Error("element ref is not a text")
	}
	if i, ok := TextRef(7).Text(); !ok || i != 7 {
		t.Error("TextRef round trip")
	}
	if _, ok := TextRef(0).Element(); ok {
		t.Error("text ref is not an element")
	}
	if _, ok := NoNode.Element(); ok {
		t.Error("NoNode is not an element")
	}
	if _, ok := NoNode.Text(); ok {
		t.Error("NoNode is not a text")
	}
}

func TestParseInputBytesLimit(t *testing.T) {
	l := budget.Defaults()
	l[budget.InputBytes] = 100
	atLimit := "<p>" + strings.Repeat("a", 97)
	if _, err := Parse(strings.NewReader(atLimit), newBudget(l)); err != nil {
		t.Fatalf("100 bytes: %v", err)
	}
	_, err := Parse(strings.NewReader(atLimit+"a"), newBudget(l))
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.InputBytes || le.Max != 100 {
		t.Fatalf("101 bytes: got %v", err)
	}
}

func TestParseReadError(t *testing.T) {
	want := errors.New("boom")
	_, err := Parse(iotest.ErrReader(want), newBudget(budget.Defaults()))
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestParseContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Parse(strings.NewReader("<p>"), budget.New(ctx, budget.Defaults(), nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestParseContextCanceledInPrepass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := budget.New(ctx, budget.Defaults(), func(stage string) {
		if stage == "dom.read" {
			cancel()
		}
	})
	_, err := Parse(strings.NewReader(strings.Repeat("<br>", 5000)), b)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestParseCheckpoints(t *testing.T) {
	var stages []string
	b := budget.New(context.Background(), budget.Defaults(), func(s string) { stages = append(stages, s) })
	if _, err := Parse(strings.NewReader("<p>x"), b); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stages, ","); got != "dom.read,dom.prepass,dom.parse,dom.convert" {
		t.Errorf("stages = %s", got)
	}
}

func TestHTMLDepth(t *testing.T) {
	// html and body take two of the 512 slots of the parser's stack.
	nested := func(n int) string { return strings.Repeat("<div>", n) }
	if _, err := Parse(strings.NewReader(nested(510)), newBudget(budget.Defaults())); err != nil {
		t.Fatalf("510 nested divs: %v", err)
	}
	_, err := Parse(strings.NewReader(nested(511)), newBudget(budget.Defaults()))
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.HTMLDepth || le.Max != MaxHTMLDepth {
		t.Fatalf("511 nested divs: got %v", err)
	}
	_, err = Parse(strings.NewReader(nested(20000)), newBudget(budget.Defaults()))
	if !errors.As(err, &le) || le.Kind != budget.HTMLDepth {
		t.Fatalf("20000 nested divs: got %v", err)
	}
}

func TestConvertChargesExactNodes(t *testing.T) {
	b := newBudget(budget.Defaults())
	if _, err := Parse(strings.NewReader(`<p a=1 b=2>x</p><!--c-->`), b); err != nil {
		t.Fatal(err)
	}
	// html, head, body, p + 2 attributes, text, comment.
	if got := b.Used(budget.Nodes); got != 8 {
		t.Errorf("Used(Nodes) = %d, want 8", got)
	}
}

func TestConvertNodesLimitExact(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<p a=1 b=2>x</p><!--c-->`))
	if err != nil {
		t.Fatal(err)
	}
	l := budget.Defaults()
	l[budget.Nodes] = 8
	if _, err := Convert(root, newBudget(l)); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	l[budget.Nodes] = 7
	_, err = Convert(root, newBudget(l))
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Nodes || le.Max != 7 {
		t.Fatalf("just over: got %v", err)
	}
}

func TestNamespaces(t *testing.T) {
	d := parse(t, `<p><svg xlink:href="u" width="3"><style>x</style><title>t</title><foreignObject><style>y</style></foreignObject></svg><math><mi>x</mi></math></p>`)
	ns := func(tag string, nth int) Namespace {
		t.Helper()
		for i := range d.Elements {
			e := &d.Elements[i]
			if (e.Tag.String() == tag || e.Name == tag) && nth == 0 {
				return e.NS
			}
			if e.Tag.String() == tag || e.Name == tag {
				nth--
			}
		}
		t.Fatalf("no element %s", tag)
		return 0
	}
	if ns("p", 0) != NSHTML || ns("svg", 0) != NSSVG || ns("mi", 0) != NSMathML || ns("math", 0) != NSMathML {
		t.Error("wrong namespaces for p, svg, math, mi")
	}
	if ns("style", 0) != NSSVG {
		t.Error("svg style must be in the SVG namespace")
	}
	if ns("style", 1) != NSHTML {
		t.Error("style under foreignObject must be an HTML element")
	}
	if ns("title", 0) != NSSVG || d.Title != "" {
		t.Errorf("svg title must not become the document title: %q", d.Title)
	}
	svg := elementNamed(t, d, "svg")
	if v, ok := svg.Attr("xlink:href"); !ok || v != "u" {
		t.Errorf("attrs = %+v, want key xlink:href", svg.Attrs)
	}
	if v, ok := svg.Attr("width"); !ok || v != "3" {
		t.Errorf("attrs = %+v", svg.Attrs)
	}
}

func TestElementDoesNotGrow(t *testing.T) {
	// NS lives in the padding after Tag.
	if got := unsafe.Sizeof(Element{}); got != 128 {
		t.Fatalf("sizeof(Element) = %d", got)
	}
}

func TestClassesAreDeduplicated(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a b c", "a b c"},
		{"a a a", "a"},
		{"b a b c a", "b a c"},
		{"  a\tb\na \f b ", "a b"},
		{"", ""},
		{"a b c d e f g h i a b c j", "a b c d e f g h i j"},
	}
	for _, tt := range tests {
		d := parse(t, `<p class="`+tt.in+`">`)
		if got := strings.Join(elementNamed(t, d, "p").Classes, " "); got != tt.want {
			t.Errorf("class %q: got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestClassBombIsDeduplicated(t *testing.T) {
	const input = 2 << 20
	src := `<p class="` + strings.Repeat("a ", (input-30)/2) + `">`
	d := parse(t, src)
	if got := elementNamed(t, d, "p").Classes; len(got) != 1 || got[0] != "a" {
		t.Fatalf("got %d classes, want 1", len(got))
	}
	// Distinct classes stay distinct, and the work stays linear.
	var sb strings.Builder
	sb.WriteString(`<p class="`)
	for i := range 100_000 {
		fmt.Fprintf(&sb, "c%d ", i%1000)
	}
	sb.WriteString(`">`)
	if got := elementNamed(t, parse(t, sb.String()), "p").Classes; len(got) != 1000 {
		t.Fatalf("got %d classes, want 1000", len(got))
	}
}
