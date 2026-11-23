package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

var tdRe = regexp.MustCompile(`(?m)^(-?[0-9.]+) (-?[0-9.]+) Td$`)

// lastTd returns the x and y of the last text-positioning operator of a page.
func lastTd(tb testing.TB, content []byte) (x, y float64) {
	tb.Helper()
	m := tdRe.FindAllSubmatch(content, -1)
	if len(m) == 0 {
		tb.Fatalf("no Td in %q", content)
	}
	l := m[len(m)-1]
	x, _ = strconv.ParseFloat(string(l[1]), 64)
	y, _ = strconv.ParseFloat(string(l[2]), 64)
	return x, y
}

// pageAreaHeight measures, by rendering, the page area height of the options
// in the units of a tall padding-top block.
func pageAreaHeight(t *testing.T, c *Converter) float64 {
	t.Helper()
	pages := func(px int) int {
		res, _ := renderBytes(t, c, fmt.Sprintf(`<div style="padding-top:%dpx"></div>`, px))
		return res.Pages
	}
	a, b := pages(20000), pages(40000)
	if b <= a {
		t.Fatalf("pages(20000)=%d pages(40000)=%d", a, b)
	}
	return 20000 / float64(b-a)
}

// documentOfPages returns a document that paginates to exactly n pages.
func documentOfPages(t *testing.T, c *Converter, n int) string {
	t.Helper()
	h := pageAreaHeight(t, c)
	for _, adj := range []float64{40, 100, 200, 10, 300} {
		html := fmt.Sprintf(`<div style="padding-top:%dpx"></div>`, int(float64(n)*h-adj))
		var buf bytes.Buffer
		res, err := c.Render(context.Background(), strings.NewReader(html), &buf)
		if err == nil && res.Pages == n {
			return html
		}
	}
	t.Fatalf("cannot build a %d-page document", n)
	return ""
}

func TestFooterOnEveryPageAtCounts(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	counts := []int{1, 9, 10, 99, 100, 500}
	if testing.Short() {
		counts = []int{1, 9, 10, 99}
	}
	for _, n := range counts {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			html := documentOfPages(t, c, n)
			var buf bytes.Buffer
			res, err := c.Render(context.Background(), strings.NewReader(html), &buf)
			if err != nil || res.Pages != n {
				t.Fatalf("%d pages, %v", res.Pages, err)
			}
			doc := pdftest.Validate(t, buf.Bytes())
			if len(doc.Pages) != n {
				t.Fatalf("%d pages in the file", len(doc.Pages))
			}
			for i, pg := range doc.Pages {
				text := strings.Join(pg.Text, " ")
				if !strings.Contains(text, "TLP:AMBER") {
					t.Errorf("page %d: no TLP:AMBER", i+1)
				}
				if want := fmt.Sprintf("Page %d of %d", i+1, n); !strings.Contains(text, want) {
					t.Errorf("page %d: %q missing in %q", i+1, want, text)
				}
				if strings.Count(text, "TLP:AMBER") != 1 || strings.Count(text, "Page ") != 1 {
					t.Errorf("page %d: footer drawn more than once: %q", i+1, text)
				}
			}
		})
	}
}

// The right footer is right-aligned: "Page 10 of 10" is wider than "Page 9 of 10",
// so it starts further left, and the width follows the final count.
func TestFooterWidthFollowsTheCount(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	x := func(n, page int) float64 {
		_, data := renderBytes(t, c, documentOfPages(t, c, n))
		doc := pdftest.Validate(t, data)
		x, _ := lastTd(t, doc.Pages[page-1].Content)
		return x
	}
	x9of10, x10of10 := x(10, 9), x(10, 10)
	if !(x10of10 < x9of10) {
		t.Errorf("Page 10 of 10 starts at %v, Page 9 of 10 at %v: the wider text must start further left", x10of10, x9of10)
	}
	x9of9 := x(9, 9)
	if !(x9of10 < x9of9) {
		t.Errorf("Page 9 of 10 starts at %v, Page 9 of 9 at %v: the total must be known when laying out", x9of10, x9of9)
	}
	if d1, d2 := x9of9-x9of10, x9of10-x10of10; math.Abs(d1-d2) > 0.01 {
		t.Errorf("one more digit moves the text by %v then %v", d1, d2)
	}
}

func TestDocumentPageRuleCannotChangeFooter(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	docs := []string{
		`<style>@page { @bottom-left { content: "TLP:CLEAR" !important } }</style><p>x</p>`,
		`<style>@page { size: A3; margin: 0; @bottom-left { content: "TLP:CLEAR" !important; display: none; color: red } @bottom-right { content: "Page 1 of 1" } }</style><p>x</p>`,
		`<style>@page { @bottom-left { content: "TLP:CLEAR" } } p { color: red }</style><p>x</p>`,
		`<style>@media print { @page { @bottom-left { content: "TLP:CLEAR" !important } } }</style><p>x</p>`,
	}
	for i, h := range docs {
		res, data := renderBytes(t, c, h)
		doc := pdftest.Validate(t, data)
		text := strings.Join(doc.Pages[0].Text, " ")
		if strings.Contains(text, "TLP:CLEAR") || !strings.Contains(text, "TLP:AMBER") || !strings.Contains(text, "Page 1 of 1") {
			t.Errorf("doc %d: %q", i, text)
		}
		mb := doc.Pages[0].MediaBox
		if mb[2] < 594 || mb[2] > 596 {
			t.Errorf("doc %d: ExtraCSS page size lost: %v", i, mb)
		}
		_ = res
	}
}

// Six 1 KiB margin boxes on 500 pages: static text is shaped once, so the
// glyph budget is not spent per page.
func TestStaticMarginTextIsNotChargedPerPage(t *testing.T) {
	if testing.Short() {
		t.Skip("500 pages")
	}
	box := func(pos string) string {
		return "@" + pos + " { content: \"" + strings.Repeat("abcdefg ", 128) + "\"; font-size: 4px }"
	}
	css := "@page { size: 300px 300px; margin: 60px 10px; " + strings.Join([]string{
		box("top-left"), box("top-center"), box("top-right"), box("bottom-left"), box("bottom-center"), box("bottom-right"),
	}, " ") + " }"
	c := mustNew(t, Options{ExtraCSS: css, Limits: Limits{MaxWork: 1 << 40, Timeout: 2 * time.Minute}})
	html := fmt.Sprintf(`<div style="padding-top:%dpx"></div>`, 500*180-60)
	var buf bytes.Buffer
	res, err := c.Render(context.Background(), strings.NewReader(html), &buf)
	if err != nil {
		t.Fatalf("%v (pages %d)", err, res.Pages)
	}
	if res.Pages < 400 {
		t.Fatalf("only %d pages", res.Pages)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	for _, i := range []int{0, len(doc.Pages) / 2, len(doc.Pages) - 1} {
		if n := strings.Count(strings.Join(doc.Pages[i].Text, " "), "abcdefg"); n < 6*100 {
			t.Errorf("page %d: %d repetitions of the margin text", i+1, n)
		}
	}
	// Counters still count: per page, only the digits are charged.
	c = mustNew(t, Options{ExtraCSS: "@page { size: 300px 300px; margin: 60px 10px; @bottom-right { content: \"Page \" counter(page) \" of \" counter(pages) } }", Limits: Limits{MaxGlyphs: 500*20 + 2000, MaxWork: 1 << 40}})
	if _, err := c.Render(context.Background(), strings.NewReader(html), io.Discard); err != nil {
		t.Errorf("counter footer on 500 pages: %v", err)
	}
}

func TestMarginBoxesAreDeterministic(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	html := manyParagraphs(30)
	_, a := renderBytes(t, c, html)
	_, b := renderBytes(t, mustNew(t, referenceOptions(t)), html)
	if !bytes.Equal(a, b) {
		t.Error("two Converters give different bytes")
	}
	_, d := renderBytes(t, c, html)
	if !bytes.Equal(a, d) {
		t.Error("two renders give different bytes")
	}
	pdftest.Validate(t, a)
}

var hexGlyphs = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)

// footerLines returns, for each text block drawn in the bottom 150 pt of a
// page, its x and its glyph count.
func footerLines(content []byte) (xs []float64, glyphs []int) {
	for _, blk := range bytes.Split(content, []byte("BT"))[1:] {
		m := tdRe.FindSubmatch(blk)
		if m == nil {
			continue
		}
		y, _ := strconv.ParseFloat(string(m[2]), 64)
		if y >= 150 {
			continue
		}
		x, _ := strconv.ParseFloat(string(m[1]), 64)
		n := 0
		for _, g := range hexGlyphs.FindAllSubmatch(blk, -1) {
			n += len(g[1]) / 4
		}
		xs, glyphs = append(xs, x), append(glyphs, n)
	}
	return xs, glyphs
}

// Static margin text wraps and each line follows text-align; text with a
// counter stays on one line, clipped to its box.
func TestMarginTextWrappingAndAlignment(t *testing.T) {
	render := func(align, contentValue string) []byte {
		css := fmt.Sprintf(`@page { size: 300px 300px; margin: 0 80px 150px; @bottom-center { content: %s; text-align: %s; font-family: monospace; font-size: 10px; vertical-align: top } }`, contentValue, align)
		_, data := renderBytes(t, mustNew(t, Options{ExtraCSS: css}), "<p>x</p>")
		return pdftest.Validate(t, data).Pages[0].Content
	}
	advance := func() float64 {
		x1, _ := lastTd(t, render("right", `"aaaa"`))
		x2, _ := lastTd(t, render("right", `"aaaaaaaa"`))
		adv := (x1 - x2) / 4
		if adv <= 0 {
			t.Fatalf("advance %v", adv)
		}
		return adv
	}()
	t.Run("static text wraps, lines follow text-align", func(t *testing.T) {
		const words = `"aaa bb cccccc d eeeee ff gggggggg hh iiii j kkkkkkk ll mmm nnnnn o pppppp qq rrr"`
		for _, align := range []string{"left", "center", "right"} {
			xs, ns := footerLines(render(align, words))
			if len(xs) < 3 {
				t.Fatalf("%s: %d lines, want the text wrapped on at least 3", align, len(xs))
			}
			anchor := func(i int) float64 {
				switch align {
				case "center":
					return xs[i] + float64(ns[i])*advance/2
				case "right":
					return xs[i] + float64(ns[i])*advance
				}
				return xs[i]
			}
			for i := 1; i < len(xs); i++ {
				if math.Abs(anchor(i)-anchor(0)) > 0.6 {
					t.Errorf("%s: line %d (%d glyphs) anchors at %.2f, line 0 (%d glyphs) at %.2f", align, i, ns[i], anchor(i), ns[0], anchor(0))
				}
			}
		}
	})
	t.Run("text with a counter stays on one clipped line", func(t *testing.T) {
		long := fmt.Sprintf(`"%s" counter(page)`, strings.Repeat("abcdef ", 40))
		for _, align := range []string{"left", "center", "right"} {
			content := render(align, long)
			xs, _ := footerLines(content)
			if len(xs) != 1 {
				t.Fatalf("%s: %d lines, want 1", align, len(xs))
			}
			if !bytes.Contains(content, []byte("W n")) {
				t.Errorf("%s: no clip around the margin box", align)
			}
			if xs[0] < 59.9 || xs[0] > 60.1 {
				t.Errorf("%s: overflowing text starts at x=%.2f, want the box start 60", align, xs[0])
			}
		}
	})
	t.Run("short text with a counter follows text-align", func(t *testing.T) {
		for _, align := range []string{"left", "center", "right"} {
			var anchors []float64
			for _, c := range []string{`"a" counter(page)`, `"abcdefgh" counter(page)`, `"abcdefghijkl" counter(page)`} {
				xs, ns := footerLines(render(align, c))
				if len(xs) != 1 {
					t.Fatalf("%s %s: %d lines", align, c, len(xs))
				}
				w := float64(ns[0]) * advance
				switch align {
				case "center":
					anchors = append(anchors, xs[0]+w/2)
				case "right":
					anchors = append(anchors, xs[0]+w)
				default:
					anchors = append(anchors, xs[0])
				}
			}
			for i := 1; i < len(anchors); i++ {
				if math.Abs(anchors[i]-anchors[0]) > 0.6 {
					t.Errorf("%s: anchors %v differ", align, anchors)
				}
			}
		}
	})
}

// Hostile margin content never panics, never breaks the file, never injects.
func TestHostileMarginContent(t *testing.T) {
	long := strings.Repeat("x", 1<<20)
	contents := map[string]string{
		"1 MiB string":         `"` + long + `"`,
		"nul":                  `"a\0 b"`,
		"control":              `"\1\2\1b[31m\7"`,
		"newline escape":       `"line\a two"`,
		"lone surrogate":       `"\d800"`,
		"beyond unicode":       `"\110000 \10ffff"`,
		"bidi":                 `"\202e abc \2066 \2069"`,
		"pdf injection":        `") >> /AA << /S /JavaScript /JS (app.alert(1)) >> ("`,
		"parentheses":          `"(((( )))) \\ \( \)"`,
		"counters repeated":    strings.Repeat(`counter(page) `, 5000),
		"counter then pages":   `counter(page) "/" counter(pages) counter(pages)`,
		"counter style":        `counter(page, upper-roman)`,
		"counter unknown":      `counter(foo)`,
		"counter empty":        `counter()`,
		"counter extra":        `counter(page 1)`,
		"counter unterminated": `counter(page`,
		"attr":                 `attr(title)`,
		"url":                  `url(http://x/y.png)`,
		"quotes":               `open-quote "x" close-quote`,
		"string unterminated":  `"abc`,
		"none":                 `none`,
		"empty":                `""`,
		"numbers":              `1e999 -0 NaN`,
		"nested function":      `counter(counter(page))`,
		"huge escape run":      `"` + strings.Repeat(`\1f600`, 100000) + `"`,
		"combining marks":      `"a` + strings.Repeat("́", 5000) + `"`,
	}
	boxes := []string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}
	c := mustNew(t, Options{})
	for name, content := range contents {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString("<style>@page { size: A4; margin: 1in;")
			for _, b := range boxes {
				fmt.Fprintf(&sb, " @%s { content: %s }", b, content)
			}
			sb.WriteString(" }</style><p>body</p><div style='padding-top:2000px'></div>")
			var buf bytes.Buffer
			res, err := c.Render(context.Background(), strings.NewReader(sb.String()), &buf)
			if res == nil {
				t.Fatal("nil Result")
			}
			if errors.Is(err, ErrInternal) {
				t.Fatalf("internal error: %v", err)
			}
			if err != nil {
				if !errors.Is(err, ErrLimit) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			doc := pdftest.Validate(t, buf.Bytes())
			if len(doc.Pages) != res.Pages {
				t.Errorf("%d pages in the file, %d in the Result", len(doc.Pages), res.Pages)
			}
			if bytes.Contains(buf.Bytes(), []byte("/JavaScript")) || bytes.Contains(buf.Bytes(), []byte("/AA ")) {
				t.Error("PDF injection through margin content")
			}
		})
	}
}

// Extreme margin-box styles.
func TestHostileMarginBoxStyles(t *testing.T) {
	styles := []string{
		"font-size: 1e9px", "font-size: 0", "padding: 1e9px", "padding: -5px", "border: 1e9px solid red",
		"margin: 1e9px", "margin: -1e9px", "width: 1e9px", "line-height: 1e9px", "letter-spacing: 1e9px",
		"background: linear-gradient(red, blue)", "vertical-align: bottom; padding: 1000px",
		"font-family: 'Nonexistent', serif; font-weight: 1000", "text-align: justify",
	}
	for _, st := range styles {
		t.Run(st, func(t *testing.T) {
			html := fmt.Sprintf(`<style>@page { size: A4; margin: 1in; @bottom-left { content: "TLP:AMBER"; %s } @bottom-right { content: "Page " counter(page) " of " counter(pages); %s } }</style><p>x</p>`, st, st)
			var buf bytes.Buffer
			res, err := mustNew(t, Options{}).Render(context.Background(), strings.NewReader(html), &buf)
			if res == nil || errors.Is(err, ErrInternal) {
				t.Fatalf("got %v", err)
			}
			if err == nil {
				pdftest.Validate(t, buf.Bytes())
			} else if !errors.Is(err, ErrLimit) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("margins leaving no room", func(t *testing.T) {
		for _, m := range []string{"0", "148mm 105mm", "1000in", "-5in"} {
			html := fmt.Sprintf(`<style>@page { size: A4; margin: %s; @bottom-right { content: counter(page) } @top-left { content: "x" } }</style><p>x</p>`, m)
			res, err := mustNew(t, Options{}).Render(context.Background(), strings.NewReader(html), io.Discard)
			if res == nil || errors.Is(err, ErrInternal) {
				t.Errorf("margin %s: %v", m, err)
			}
		}
	})
}

// A cancellation between the last page and the end of the render, which is the margin pass, is seen.
func TestCancelDuringMarginPass(t *testing.T) {
	record := func() []string {
		c := mustNew(t, referenceOptions(t))
		var stages []string
		c.hook = func(s string) { stages = append(stages, s) }
		renderBytes(t, c, manyParagraphs(20))
		return stages
	}
	stages := record()
	last, margin := -1, -1
	for i, s := range stages {
		switch s {
		case "page":
			last = i
		case "margin":
			margin = i
		}
	}
	if last < 0 || margin < 0 {
		t.Fatalf("no page or margin stage in %v", stages)
	}
	if margin == last+1 {
		t.Logf("no checkpoint inside the margin pass: %v", stages[last:])
	}
	for at := last + 1; at <= margin; at++ {
		t.Run(fmt.Sprintf("event %d %s", at, stages[at]), func(t *testing.T) {
			c := mustNew(t, referenceOptions(t))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n := 0
			c.hook = func(string) {
				if n == at {
					cancel()
				}
				n++
			}
			res, err := c.Render(ctx, strings.NewReader(manyParagraphs(20)), io.Discard)
			if res == nil {
				t.Fatal("nil Result")
			}
			if stages[at] != "margin" && !errors.Is(err, context.Canceled) {
				t.Errorf("cancel at %q: got %v", stages[at], err)
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("got %v", err)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		c := mustNew(t, referenceOptions(t))
		c.limits = mustNew(t, Options{Limits: Limits{Timeout: 40 * time.Millisecond}}).limits
		c.timeout = 40 * time.Millisecond
		c.hook = func(s string) {
			if s == "page" {
				time.Sleep(10 * time.Millisecond)
			}
		}
		_, err := c.Render(context.Background(), strings.NewReader(manyParagraphs(20)), io.Discard)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("got %v", err)
		}
	})
}

// A writer that fails during the deferred streams.
func TestWriterFailureDuringMarginPass(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	var full bytes.Buffer
	if _, err := c.Render(context.Background(), strings.NewReader(manyParagraphs(20)), &full); err != nil {
		t.Fatal(err)
	}
	want := errors.New("disk full")
	for _, after := range []int{full.Len() / 2, full.Len() * 3 / 4, full.Len() - 300, full.Len() - 1} {
		if _, err := c.Render(context.Background(), strings.NewReader(manyParagraphs(20)), &failingWriter{after: after, err: want}); !errors.Is(err, want) {
			t.Errorf("after %d bytes: got %v", after, err)
		}
	}
}

// Without margin boxes, no content is deferred and the output has no second stream.
func TestPageCountDoesNotAppearWithoutCounters(t *testing.T) {
	_, data := renderBytes(t, mustNew(t, Options{ExtraCSS: "@page { @bottom-left { content: \"static\" } }"}), manyParagraphs(10))
	doc := pdftest.Validate(t, data)
	for i, pg := range doc.Pages {
		if !strings.Contains(strings.Join(pg.Text, " "), "static") {
			t.Errorf("page %d lacks the static box", i+1)
		}
	}
}
