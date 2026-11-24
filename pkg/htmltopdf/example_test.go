package htmltopdf_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func ExampleConverter_Render_warnings() {
	conv, err := htmltopdf.New(htmltopdf.Options{})
	if err != nil {
		log.Fatal(err)
	}
	html := `<style>p { z-index: 3 }</style><img src="logo.png"><p>text</p>`
	res, err := conv.Render(context.Background(), strings.NewReader(html), &bytes.Buffer{})
	if err != nil {
		log.Fatal(err)
	}
	for _, w := range res.Warnings {
		fmt.Println(w.Code, w.Detail, w.Count)
	}
	// Output:
	// unsupported-element img 1
	// unsupported-property z-index 1
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
