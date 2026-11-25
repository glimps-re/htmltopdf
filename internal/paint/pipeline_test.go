package paint

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// render runs the whole pipeline of M1 on html, up to a PDF.
func render(tb testing.TB, html string) (*pdftest.Doc, []byte) {
	tb.Helper()
	l := budget.Defaults()
	l[budget.Nodes] = 1 << 22
	l[budget.InputBytes] = 1 << 26
	l[budget.CSSBytes] = 1 << 24
	b := budget.New(context.Background(), l, nil)
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		tb.Fatal(err)
	}
	ua, err := style.CompileUA(budget.New(context.Background(), budget.Defaults(), nil))
	if err != nil {
		tb.Fatal(err)
	}
	var sheets []*style.Sheet
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Tag.String() != "style" || e.NS != dom.NSHTML {
			continue
		}
		var sb strings.Builder
		for r := e.FirstChild; r != dom.NoNode; r = d.NextSibling(r) {
			if ti, ok := r.Text(); ok {
				sb.WriteString(d.Texts[ti].Data)
			}
		}
		ss, err := css.ParseStylesheet([]byte(sb.String()), css.OriginDocument, b)
		if err != nil {
			tb.Fatal(err)
		}
		sh, err := style.Compile(ss, b)
		if err != nil {
			tb.Fatal(err)
		}
		sheets = append(sheets, sh)
	}
	extraSS, err := css.ParseStylesheet([]byte(testcorpus.HostConnectorPageCSS+testcorpus.PaperCSS), css.OriginExtra, b)
	if err != nil {
		tb.Fatal(err)
	}
	extra, err := style.Compile(extraSS, b)
	if err != nil {
		tb.Fatal(err)
	}
	r := style.NewResolver(ua, extra, sheets, css.MediaEnv{Print: true, Width: 794, Height: 1123}, b)
	root, err := box.Build(d, r, b)
	if err != nil {
		tb.Fatal(err)
	}
	fs, err := text.NewFontSet([]text.Source{
		{Data: readFont(tb, "LiberationSans-Regular.ttf")},
		{Data: readFont(tb, "LiberationSans-Bold.ttf")},
		{Data: readFont(tb, "LiberationSans-Italic.ttf")},
		{Data: readFont(tb, "LiberationSans-BoldItalic.ttf")},
		{Data: readFont(tb, "DejaVuSans.ttf")},
		{Data: readFont(tb, "DejaVuSans-Bold.ttf")},
		{Data: readFont(tb, "DejaVuSansMono.ttf")},
	}, text.Generic{SansSerif: "Liberation Sans", Monospace: "DejaVu Sans Mono"}, []string{"DejaVu Sans"})
	if err != nil {
		tb.Fatal(err)
	}
	shaper := text.NewShaper(fs, b)
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, b)
	painter := NewPainter(w, shaper, b)
	pg := layout.NewPaginator(root, layout.Config{Page: r.Page(style.PageContext{}), Shaper: shaper, Budget: b})
	for {
		page, err := pg.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			tb.Fatal(err)
		}
		c := &pdf.Content{}
		g := Geometry{Width: page.Width, Height: page.Height, Area: page.Area, Scale: page.Scale}
		links, err := painter.Page(c, g, page.Items)
		if err != nil {
			tb.Fatal(err)
		}
		balanced(tb, string(c.Bytes()))
		if _, err := w.AddPage(page.Width*PxToPt, page.Height*PxToPt, c, false, links); err != nil {
			tb.Fatal(err)
		}
	}
	if err := w.Close(pdf.Info{Title: d.Title}); err != nil {
		tb.Fatal(err)
	}
	return pdftest.Validate(tb, buf.Bytes()), buf.Bytes()
}

func TestPipelineSmallDocument(t *testing.T) {
	doc, _ := render(t, `<!doctype html><title>T</title><style>
	.box { border: 2px solid #c00; background: #eef; padding: 8px; margin: 10px 0 }
	a { color: #06c }
	</style><h1>Rapport</h1><div class=box><p>Détecté : <b>Trojan</b> <a href="https://example.test/x">lien</a></p></div>`)
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages", len(doc.Pages))
	}
	all := strings.Join(doc.Pages[0].Text, "|")
	for _, want := range []string{"Rapport", "Détecté", "Trojan", "lien"} {
		if !strings.Contains(all, want) {
			t.Errorf("text %q lacks %q", all, want)
		}
	}
	if len(doc.Pages[0].Links) != 1 || doc.Pages[0].Links[0].URI != "https://example.test/x" {
		t.Fatalf("links %+v", doc.Pages[0].Links)
	}
}

func TestPipelineCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus")
	}
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, testcorpus.Small)
			if err != nil {
				t.Fatal(err)
			}
			doc, data := render(t, string(html))
			if len(doc.Pages) == 0 {
				t.Fatal("no page")
			}
			t.Logf("%s: %d pages, %d fonts, %d KiB", name, len(doc.Pages), len(doc.Fonts), len(data)>>10)
		})
	}
}
