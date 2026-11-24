package box

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"weak"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// checkTree verifies the invariants of a built tree.
func checkTree(tb testing.TB, b *Box) {
	tb.Helper()
	if b.Style == nil {
		tb.Fatalf("%s box without a style", b.Kind)
	}
	switch b.Kind {
	case KindText:
		if b.Text == "" || strings.ContainsAny(b.Text, "\n\r") {
			tb.Fatalf("text box with text %q", b.Text)
		}
		if len(b.Children) != 0 {
			tb.Fatal("text box with children")
		}
	case KindLineBreak:
		if len(b.Children) != 0 {
			tb.Fatal("line break with children")
		}
	case KindBlock, KindInlineBlock:
		blocks, inlines := 0, 0
		for _, c := range b.Children {
			if c.Kind == KindBlock {
				blocks++
			} else {
				inlines++
			}
		}
		if blocks > 0 && inlines > 0 {
			tb.Fatalf("block with %d block and %d inline-level children", blocks, inlines)
		}
		if b.Anonymous && blocks > 0 {
			tb.Fatal("anonymous block with block children")
		}
	case KindInline:
		for _, c := range b.Children {
			if c.Kind == KindBlock {
				tb.Fatal("inline box with a block child")
			}
		}
		if b.fragment && len(b.Children) == 0 {
			tb.Fatal("empty fragment kept")
		}
	default:
		tb.Fatalf("kind %d", b.Kind)
	}
	if b.Anonymous && b.Kind != KindBlock {
		tb.Fatal("anonymous non-block")
	}
	if b.Href != "" && !strings.HasPrefix(b.Href, "http") && !strings.HasPrefix(b.Href, "mailto:") {
		tb.Fatalf("href %q", b.Href)
	}
	for _, c := range b.Children {
		checkTree(tb, c)
	}
}

func TestCorpusTrees(t *testing.T) {
	for _, name := range corpusNames(t) {
		for _, size := range corpusSizes {
			t.Run(fmt.Sprintf("%s/%s", name, size), func(t *testing.T) {
				b := testBudget(nil)
				d := parseReport(t, name, size, b)
				st := newStyler(t, d, b)
				root, err := Build(d, st, b)
				if err != nil {
					t.Fatal(err)
				}
				checkTree(t, root)
				boxes := countBoxes(root)
				// Boxes dropped after being charged (text trimmed to nothing, empty
				// fragments) still count: the charge is an upper bound.
				if int64(boxes) > b.Used(budget.Boxes) {
					t.Fatalf("%d boxes, %d charged", boxes, b.Used(budget.Boxes))
				}
				t.Logf("%d elements, %d texts -> %d boxes", len(d.Elements), len(d.Texts), boxes)
			})
		}
	}
}

// Build keeps nothing of the document: it is garbage collected while the tree
// is still reachable.
func TestDocumentIsCollectedAfterBuild(t *testing.T) {
	var doc weak.Pointer[dom.Document]
	var root *Box
	func() {
		b := testBudget(nil)
		d := parseReport(t, "00-host-connector", "small", b)
		doc = weak.Make(d)
		var err error
		if root, err = Build(d, newStyler(t, d, b), b); err != nil {
			t.Fatal(err)
		}
	}()
	for range 5 {
		testGC()
	}
	if doc.Value() != nil {
		t.Fatal("the document is still reachable from the tree")
	}
	checkTree(t, root)
}

func FuzzBuild(f *testing.F) {
	for _, s := range []string{
		"<p>a <b>b</b></p>", "<span>a<div>b</div>c</span>", "<pre>a\n\tb</pre>", "<a href='javascript:x'>l</a>", "<svg><text>t</text></svg>",
		"<div>a<p>b</p>c</div>", "<ul><li>a<li>b</ul>", "<table><tr><td>x</table>", "<p style='white-space:pre-line'>a  \n b</p>",
		"<i><b><u>x<div>y</div>z</u></b></i>", "<p>a <span style='display:inline-block'>b<div>c</div></span> d</p>",
		"<div style='display:flex'> a <b>b</b><i> </i><div>c</div>d</div>", "<div style='display:grid'> <p>x</p> \n </div>",
		"<p style='text-transform:uppercase'>é ß <a href='https://a.test' id=i>x</a></p>", "text <br> more<br><br>", "<div style='display:none'>x</div>y",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, html string) {
		b := testBudget(func(l *budget.Limits) { l[budget.Boxes] = 5000 })
		d := parseDoc(t, html, b)
		ua, err := style.CompileUA(budget.New(context.Background(), budget.Defaults(), nil))
		if err != nil {
			t.Fatal(err)
		}
		st := &testStyler{Resolver: newResolver(t, d, ua, b)}
		root, err := Build(d, st, b)
		if err != nil {
			return
		}
		checkTree(t, root)
	})
}
