package htmltopdf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func renderM2HTML(tb testing.TB, html string) (*Result, *pdftest.Doc) {
	tb.Helper()
	res, data := renderBytes(tb, mustNew(tb, m2Options(tb)), html)
	return res, pdftest.Validate(tb, data)
}

// Whatever the layout of these displays, no text is dropped: the words of the
// source come out in the PDF.
func TestM2RenderKeepsText(t *testing.T) {
	long := strings.Repeat("longword ", 400)
	for _, tc := range []struct {
		name string
		html string
		want []string
	}{
		{"inline-block pills", `<p>a <span style="display:inline-block;padding:2px;border-radius:9px">pillone</span> <span style="display:inline-block">pilltwo</span> end</p>`, []string{"pillone", "pilltwo", "end"}},
		{"inline-block with blocks", `<p>before<span style="display:inline-block"><div>blockone</div><p>blocktwo</p>tail</span>after</p>`, []string{"before", "blockone", "blocktwo", "tail", "after"}},
		{"empty inline-block", `<p>x<span style="display:inline-block"></span>y</p>`, []string{"x", "y"}},
		{"inline-block in a link", `<p><a href="https://a.test/"><span style="display:inline-block">linked</span></a></p>`, []string{"linked"}},
		{"inline-block with a table", `<p><span style="display:inline-block"><table><tr><td>cellone</td><td>celltwo</td></tr></table></span>z</p>`, []string{"cellone", "celltwo"}},
		{"nested inline-blocks", `<p><span style="display:inline-block">a1<span style="display:inline-block">b2<span style="display:inline-block">c3</span></span></span></p>`, []string{"a1b2c3"}},
		{"inline-block with br", `<p><span style="display:inline-block">one<br>two</span></p>`, []string{"one", "two"}},
		{"very long inline-block", `<p><span style="display:inline-block">` + long + `</span>last</p>`, []string{"last"}},
		{"many inline-blocks", "<p>" + strings.Repeat(`<span style="display:inline-block">k</span> `, 2000) + "tailword</p>", []string{"tailword"}},
		{"flex mix", `<div style="display:flex"> text <span>spanned</span><div>blocked</div> more </div>`, []string{"text", "spanned", "blocked", "more"}},
		{"grid mix", `<div style="display:grid;grid-template-columns:1fr 1fr"> <p>gone</p>  <span>gtwo</span> loose </div>`, []string{"gone", "gtwo", "loose"}},
		{"transform", `<p style="text-transform:uppercase">straße édé</p>`, []string{"STRAßE", "ÉDÉ"}},
		{"transform marker", "<p style=\"text-transform:uppercase\">a\u202eb \u27e8RLO\u27e9</p>", []string{"A", "B"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, doc := renderM2HTML(t, tc.html)
			text := squash(pagesText(doc))
			for _, w := range tc.want {
				if !strings.Contains(text, squash(w)) {
					t.Errorf("%q is missing from the PDF text %.200q", w, text)
				}
			}
			if res.Pages < 1 {
				t.Errorf("%d pages", res.Pages)
			}
			for _, w := range res.Warnings {
				if w.Code == "" {
					t.Errorf("warning without a code: %+v", w)
				}
			}
		})
	}
}

// Every word of a document that mixes the display values survives, in order.
func TestM2RenderGeneratedWords(t *testing.T) {
	displays := []string{"block", "inline", "inline-block", "flex", "grid"}
	var sb strings.Builder
	var want []string
	for i := 0; i < 120; i++ {
		d := displays[i%len(displays)]
		tag := "div"
		if d == "inline" || d == "inline-block" {
			tag = "span"
		}
		w := fmt.Sprintf("word%03dq", i)
		fmt.Fprintf(&sb, `<%s style="display:%s">%s <b>%s</b></%s> `, tag, d, w, "in"+w, tag)
		want = append(want, w, "in"+w)
	}
	_, doc := renderM2HTML(t, "<body>"+sb.String())
	absent, mis := matchInOrder(want, pageStream(m2Pages(doc)))
	if len(absent)+len(mis) > 0 {
		t.Errorf("%d absent %v, %d out of order %v", len(absent), absent[:min(3, len(absent))], len(mis), mis[:min(3, len(mis))])
	}
}
