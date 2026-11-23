package htmltopdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func renderPDF(t *testing.T, html string) (*Result, []byte) {
	t.Helper()
	c := mustNew(t, referenceOptions(t))
	var buf bytes.Buffer
	res, err := c.Render(context.Background(), strings.NewReader(html), &buf)
	if err != nil {
		t.Fatal(err)
	}
	return res, buf.Bytes()
}

func manyParagraphs(n int) string {
	var sb strings.Builder
	sb.WriteString("<html><body>")
	for i := range n {
		fmt.Fprintf(&sb, "<p style=\"padding:150px 0\">paragraph %d</p>", i)
	}
	return sb.String() + "</body></html>"
}

func TestFooterOnEveryPage(t *testing.T) {
	res, pdf := renderPDF(t, manyParagraphs(40))
	if res.Pages < 10 {
		t.Fatalf("only %d pages", res.Pages)
	}
	doc := pdftest.Validate(t, pdf)
	if len(doc.Pages) != res.Pages {
		t.Fatalf("%d pages in the file, %d in the Result", len(doc.Pages), res.Pages)
	}
	for i, pg := range doc.Pages {
		text := strings.Join(strings.Fields(strings.Join(pg.Text, " ")), " ")
		if !strings.Contains(text, "TLP:AMBER") {
			t.Errorf("page %d: no TLP:AMBER in %q", i+1, text)
		}
		if want := fmt.Sprintf("Page %d of %d", i+1, res.Pages); !strings.Contains(text, want) {
			t.Errorf("page %d: %q not in %q", i+1, want, text)
		}
	}
}

func TestDocumentCannotReplaceTheFooter(t *testing.T) {
	html := `<html><head><style>@page { size: A4; margin: 10mm; @bottom-left { content: "TLP:CLEAR" !important } }</style></head><body><p>x</p></body></html>`
	res, pdf := renderPDF(t, html)
	doc := pdftest.Validate(t, pdf)
	text := strings.Join(doc.Pages[0].Text, " ")
	if strings.Contains(text, "TLP:CLEAR") || !strings.Contains(text, "TLP:AMBER") {
		t.Errorf("footer of the page: %q (warnings %v)", text, res.Warnings)
	}
}

func TestPageWithoutMarginBoxesIsNotDeferred(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := c.Render(context.Background(), strings.NewReader("<p>x"), &buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("/Contents [")) {
		t.Error("a page without margin box has two content streams")
	}
}
