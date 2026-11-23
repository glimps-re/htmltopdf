package htmltopdf

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const reportHTML = `<!DOCTYPE html><html><head><title>Rapport &amp; test</title><style>
body { font-family: sans-serif; margin: 0 }
h1 { color: #ea5b0c; border-bottom: 2px solid #ea5b0c }
.card { background: #eef; border: 3px solid #123456; padding: 8px; margin: 12px 0; border-radius: 6px }
.warn { background: #fff4ec; color: #a36f00 }
a { color: #0969da }
</style></head><body>
<h1>Analysis report</h1>
<p>First paragraph with <b>bold</b>, <i>italic</i> and a <a href="https://example.com/x">link</a>.</p>
<div class="card"><h2>Colored block</h2><p>Inside the card.</p></div>
<div class="card warn"><p>Another block, <code>monospace</code> text.</p></div>
<ul><li>one</li><li>two</li></ul>
</body></html>`

func mustNew(tb testing.TB, opts Options) *Converter {
	tb.Helper()
	c, err := New(opts)
	if err != nil {
		tb.Fatal(err)
	}
	return c
}

func renderBytes(tb testing.TB, c *Converter, html string) (*Result, []byte) {
	tb.Helper()
	var buf bytes.Buffer
	res, err := c.Render(context.Background(), strings.NewReader(html), &buf)
	if err != nil {
		tb.Fatalf("Render: %v (warnings %+v)", err, res.Warnings)
	}
	return res, buf.Bytes()
}

// referenceOptions is the host-connector configuration of design §4.1.
func referenceOptions(tb testing.TB) Options {
	tb.Helper()
	files, err := testcorpus.ReadHostConnectorFonts(os.DirFS("../../testdata/fonts"))
	if err != nil {
		tb.Fatal(err)
	}
	fonts := make([]Font, len(files))
	for i, f := range files {
		fonts[i] = Font{Data: f.Data, Family: f.Family}
	}
	return Options{
		Fonts:    fonts,
		Generic:  GenericFamilies{SansSerif: "Liberation Sans", Monospace: "DejaVu Sans Mono"},
		Fallback: []string{"DejaVu Sans"},
		Scale:    0.9,
		ExtraCSS: testcorpus.HostConnectorPageCSS,
	}
}

func pagesText(doc *pdftest.Doc) string {
	var sb strings.Builder
	for _, p := range doc.Pages {
		sb.WriteString(strings.Join(p.Text, " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func pagesContent(tb testing.TB, data []byte) string {
	tb.Helper()
	doc := pdftest.Validate(tb, data)
	var sb strings.Builder
	for _, p := range doc.Pages {
		sb.Write(p.Content)
	}
	return sb.String()
}
