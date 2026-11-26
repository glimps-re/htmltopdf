package layout

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"github.com/glimps-re/htmltopdf/internal/text"
)

var (
	fontsOnce sync.Once
	fontSet   *text.FontSet
	fontErr   error
)

func testFonts(tb testing.TB) *text.FontSet {
	tb.Helper()
	fontsOnce.Do(func() {
		loaded, err := testcorpus.ReadHostConnectorFonts(os.DirFS("../../testdata/fonts"))
		if err != nil {
			fontErr = err
			return
		}
		src := make([]text.Source, len(loaded))
		for i, f := range loaded {
			src[i] = text.Source{Data: f.Data, Family: f.Family}
		}
		fontSet, fontErr = text.NewFontSet(src, text.Generic{SansSerif: "Liberation Sans", Serif: "Liberation Sans", Monospace: "DejaVu Sans Mono"}, []string{"DejaVu Sans"})
	})
	if fontErr != nil {
		tb.Fatal(fontErr)
	}
	return fontSet
}

var (
	uaOnce  sync.Once
	uaSheet *style.Sheet
)

func uaCompiled(tb testing.TB) *style.Sheet {
	tb.Helper()
	uaOnce.Do(func() {
		s, err := style.CompileUA(testBudget(nil))
		if err != nil {
			panic(err)
		}
		uaSheet = s
	})
	return uaSheet
}

func testBudget(mod func(*budget.Limits)) *budget.Budget {
	l := budget.Defaults()
	l[budget.Nodes] = 1 << 22
	l[budget.InputBytes] = 1 << 26
	l[budget.CSSBytes] = 1 << 24
	l[budget.Boxes] = 1 << 22
	if mod != nil {
		mod(&l)
	}
	return budget.New(context.Background(), l, nil)
}

// opts configure a test pipeline.
type opts struct {
	css      string // document CSS
	extra    string // ExtraCSS
	scale    float64
	limits   func(*budget.Limits)
	envWidth float64
	// boxes, when set, edits the box tree before layout.
	boxes func(*box.Box)
}

type run struct {
	b     *budget.Budget
	root  *box.Box
	pag   *Paginator
	style *style.Resolver
}

func newRun(tb testing.TB, html string, o opts) *run {
	tb.Helper()
	b := testBudget(o.limits)
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		tb.Fatal(err)
	}
	var docSheets []*style.Sheet
	cssText := o.css
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Tag.String() == "style" && e.NS == dom.NSHTML {
			for r := e.FirstChild; r != dom.NoNode; r = d.NextSibling(r) {
				if ti, ok := r.Text(); ok {
					cssText += "\n" + d.Texts[ti].Data
				}
			}
		}
	}
	if cssText != "" {
		ss, err := css.ParseStylesheet([]byte(cssText), css.OriginDocument, b)
		if err != nil {
			tb.Fatal(err)
		}
		sh, err := style.Compile(ss, b)
		if err != nil {
			tb.Fatal(err)
		}
		docSheets = append(docSheets, sh)
	}
	var extra *style.Sheet
	if o.extra != "" {
		ss, err := css.ParseStylesheet([]byte(o.extra), css.OriginExtra, b)
		if err != nil {
			tb.Fatal(err)
		}
		if extra, err = style.Compile(ss, b); err != nil {
			tb.Fatal(err)
		}
	}
	w := o.envWidth
	if w == 0 {
		w = 794
	}
	r := style.NewResolver(uaCompiled(tb), extra, docSheets, css.MediaEnv{Print: true, Width: w, Height: 1123}, b)
	root, err := box.Build(d, r, b)
	if err != nil {
		tb.Fatal(err)
	}
	if o.boxes != nil {
		o.boxes(root)
	}
	sh := text.NewShaper(testFonts(tb), b)
	p := NewPaginator(root, Config{Page: r.Page(style.PageContext{Number: 1}), Pages: func(name string) *style.PageStyle { return r.Page(style.PageContext{Name: name}) }, Scale: o.scale, Fonts: testFonts(tb), Shaper: sh, Budget: b, KeepTree: true})
	return &run{b: b, root: root, pag: p, style: r}
}

// all returns every page.
func (r *run) all(tb testing.TB) []*Page {
	tb.Helper()
	var pages []*Page
	for {
		pg, err := r.pag.Next()
		if errors.Is(err, io.EOF) {
			return pages
		}
		if err != nil {
			tb.Fatal(err)
		}
		pages = append(pages, pg)
		if len(pages) > 2000 {
			tb.Fatal("runaway pagination")
		}
	}
}

func dumpPages(tb testing.TB, pages []*Page) string {
	tb.Helper()
	var buf bytes.Buffer
	for _, pg := range pages {
		if err := Dump(&buf, pg); err != nil {
			tb.Fatal(err)
		}
	}
	return buf.String()
}

// a pageCSS for round numbers: 800x600 page, no margin (px).
const smallPage = "@page { size: 800px 600px; margin: 0 }"
