package htmltopdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"strings"

	"github.com/glimps-re/htmltopdf/pkg/htmltopdf"
)

func ExampleConverter_Render() {
	conv, err := htmltopdf.New(htmltopdf.Options{
		ExtraCSS: "@page { size: A4; margin: 20mm }",
	})
	if err != nil {
		log.Fatal(err)
	}
	var pdf bytes.Buffer
	res, err := conv.Render(context.Background(),
		strings.NewReader("<h1>Report</h1><p>Hello, <b>world</b>.</p>"), &pdf)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Pages, "page,", bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")))
	// Output: 1 page, true
}

func ExampleConverter_RenderTemplate() {
	conv, err := htmltopdf.New(htmltopdf.Options{})
	if err != nil {
		log.Fatal(err)
	}
	t := template.Must(template.New("report").Parse(
		`<h1>{{.Title}}</h1><table>{{range .Files}}<tr><td>{{.}}</td></tr>{{end}}</table>`))
	data := map[string]any{
		"Title": "Scan report",
		// Hostile file names are escaped by html/template and printed as text.
		"Files": []string{"invoice.pdf.exe", `</td><script>alert(1)</script>`},
	}
	var pdf bytes.Buffer
	res, err := conv.RenderTemplate(context.Background(), t, data, &pdf)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Pages, "page")
	// Output: 1 page
}

// logoPNG is a 2×2 gray PNG.
const logoPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAAAAABX3VL4AAAAE0lEQVR4nAAGAPn/Av8AAAAAAwAFDQECdkt/VgAAAABJRU5ErkJggg=="

func ExampleConverter_RenderTemplate_image() {
	conv, err := htmltopdf.New(htmltopdf.Options{})
	if err != nil {
		log.Fatal(err)
	}
	t := template.Must(template.New("report").Parse(`<img src="{{.Logo}}" alt="logo"><p>Report</p>`))
	var trusted template.URL = logoPNG
	for _, data := range []map[string]any{
		// html/template rewrites a data: URL given as a string to #ZgotmplZ:
		// the image is rejected and its alt text is drawn instead.
		{"Logo": logoPNG},
		// A trusted URL typed as template.URL is kept: the image is drawn.
		{"Logo": trusted},
	} {
		res, err := conv.RenderTemplate(context.Background(), t, data, &bytes.Buffer{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(len(res.Warnings), "warnings", res.Warnings)
	}
	// Output:
	// 1 warnings [{image-rejected scheme 1}]
	// 0 warnings []
}

func ExampleConverter_Render_warnings() {
	conv, err := htmltopdf.New(htmltopdf.Options{})
	if err != nil {
		log.Fatal(err)
	}
	html := `<style>p { float: left }</style><img src="logo.png"><p>text</p>`
	res, err := conv.Render(context.Background(), strings.NewReader(html), &bytes.Buffer{})
	if err != nil {
		log.Fatal(err)
	}
	for _, w := range res.Warnings {
		fmt.Println(w.Code, w.Detail, w.Count)
	}
	// Output:
	// image-rejected scheme 1
	// unsupported-property float 1
}

func ExampleConverter_Render_limits() {
	conv, err := htmltopdf.New(htmltopdf.Options{Limits: htmltopdf.Limits{MaxNodes: 10}})
	if err != nil {
		log.Fatal(err)
	}
	_, err = conv.Render(context.Background(), strings.NewReader(strings.Repeat("<p>x</p>", 20)), &bytes.Buffer{})
	var le *htmltopdf.LimitError
	if errors.As(err, &le) {
		fmt.Println(le.Limit, errors.Is(err, htmltopdf.ErrLimit))
	}
	// Output: MaxNodes true
}

func ExampleNew_invalidExtraCSS() {
	// ExtraCSS is strict: a typo cannot silently drop a footer.
	_, err := htmltopdf.New(htmltopdf.Options{ExtraCSS: "@page { margin: 1in; @bottom-left { content: attr(title) } }"})
	fmt.Println(errors.Is(err, htmltopdf.ErrInvalidOptions))
	// Output: true
}

func ExampleDefaultLimits() {
	l := htmltopdf.DefaultLimits()
	fmt.Println(l.MaxInputBytes, l.MaxPages, l.Timeout)
	// Output: 2097152 500 1m0s
}

func ExamplePageSize() {
	// Letter paper, whatever the document asks: only the orientation of its @page
	// rules is kept, here landscape.
	conv, err := htmltopdf.New(htmltopdf.Options{PageSize: htmltopdf.PageSize{Width: 816, Height: 1056}})
	if err != nil {
		log.Fatal(err)
	}
	html := `<style>@page { size: A4 landscape }</style><p>wide</p>`
	var pdf bytes.Buffer
	if _, err := conv.Render(context.Background(), strings.NewReader(html), &pdf); err != nil {
		log.Fatal(err)
	}
	// MediaBox in points: 1056 x 816 px.
	fmt.Println(strings.Contains(pdf.String(), "/MediaBox [0 0 792 612]"))
	// Output: true
}

func ExampleOptions_referenceConfiguration() {
	conv, err := htmltopdf.New(htmltopdf.Options{
		PageSize: htmltopdf.PageSize{Width: 794.56, Height: 1122.56},
		Scale:    0.9,
		ExtraCSS: `@page { margin: 96px 28px 124px 28px;
			@bottom-right { content: "Page " counter(page) " of " counter(pages) } }`,
	})
	if err != nil {
		log.Fatal(err)
	}
	var pdf bytes.Buffer
	res, err := conv.Render(context.Background(), strings.NewReader("<h1>Report</h1>"), &pdf)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Pages, strings.Contains(pdf.String(), "/MediaBox [0 0 595.92 841.92]"))
	// Output: 1 true
}

func ExampleConverter_Render_textOffPage() {
	c, err := htmltopdf.New(htmltopdf.Options{})
	if err != nil {
		log.Fatal(err)
	}
	res, err := c.Render(context.Background(), strings.NewReader(`<p style="margin-left:5000px">far away</p>`), io.Discard)
	if err != nil {
		log.Fatal(err)
	}
	for _, w := range res.Warnings {
		if w.Code == htmltopdf.WarnTextOffPage {
			fmt.Println(w.Detail)
		}
	}
	// Output: right
}
