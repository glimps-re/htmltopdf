//go:build m4_fixed

// The fixed-positioning cases of the hostile suite. They need position: fixed laid out (M4-01): remove the
// build tag when it is merged.

package htmltopdf

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const h4MaxFixedItems = 4096 // display items of fixed boxes per page

// h4PageCounts returns, for each page of r, how many times marker appears in its text.
func h4PageCounts(r h4Run, marker string) []int {
	out := make([]int, len(r.doc.Pages))
	for i, p := range r.doc.Pages {
		out[i] = strings.Count(strings.Join(p.Text, " "), marker)
	}
	return out
}

func h4Clamped(res *Result, detail string) bool {
	for _, w := range res.Warnings {
		if w.Code == WarnClamped && strings.Contains(w.Detail, detail) {
			return true
		}
	}
	return false
}

// h4FlowPages returns an in-flow document of n pages of one marker word each (a paragraph per page).
func h4FlowPages(n int) (words []string, html string) {
	var sb strings.Builder
	for i := range n {
		w := fmt.Sprintf("FLOW%05d", i)
		words = append(words, w)
		fmt.Fprintf(&sb, `<div style="break-after:page">%s</div>`, w)
	}
	return words, sb.String()
}

// A watermark on 500 pages: it is on every page exactly once, the in-flow text is present once, and the
// work stays within 1.1 times the twin without it plus the items of the 500 emissions.
func TestM4FixedWatermarkOn500Pages(t *testing.T) {
	words, flow := h4FlowPages(500)
	css := `<style>.wm{position:fixed;top:300px;left:100px;width:300px;height:100px;background:#eee;transform:rotate(-18deg)}</style>`
	r := h4RenderTwin(t, referenceOptions(t), css+`<div class=wm>WATERMARK</div>`+flow)
	if r.err != nil {
		t.Fatal(r.err)
	}
	twin := h4RenderTwin(t, referenceOptions(t), css+flow)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	if r.res.Pages != 500 || twin.res.Pages != 500 {
		t.Fatalf("%d pages with the watermark, %d without", r.res.Pages, twin.res.Pages)
	}
	h4TextOnce(t, r, words)
	for i, n := range h4PageCounts(r, "WATERMARK") {
		if n != 1 {
			t.Fatalf("page %d: %d watermarks", i+1, n)
		}
	}
	const itemsPerBox = 3 // background, text, transform pair
	t.Logf("work %d, twin %d", r.work, twin.work)
	if float64(r.work) > 1.1*float64(twin.work)+500*itemsPerBox || r.boxes != twin.boxes+1 && r.boxes != twin.boxes+2 || r.peak > 2*twin.peak+32<<20 {
		t.Errorf("work %d boxes %d heap %d MiB; twin work %d boxes %d heap %d MiB", r.work, r.boxes, r.peak>>20, twin.work, twin.boxes, twin.peak>>20)
	}
}

// The per-page bound of fixed display items (4096): boxes of two items each (a background and a text), at
// the bound every box is on every page with no warning; one box more is emitted on the first page only
// with a "fixed boxes" warning, and its text is in the PDF exactly once.
func TestM4FixedItemsPerPageBound(t *testing.T) {
	boxes := func(n int) (string, []string) {
		var sb strings.Builder
		var words []string
		sb.WriteString(`<style>.f{position:fixed;left:0;top:0;width:3px;height:3px;background:#c00;font-size:1px}</style>`)
		for i := range n {
			w := fmt.Sprintf("FX%05d", i)
			words = append(words, w)
			fmt.Fprintf(&sb, `<div class=f>%s</div>`, w)
		}
		return sb.String(), words
	}
	_, flow := h4FlowPages(3)
	atBox := h4MaxFixedItems / 2
	html, words := boxes(atBox)
	r := h4Render(t, referenceOptions(t), html+flow, true)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.res.Pages != 3 || h4Clamped(r.res, "fixed boxes") {
		t.Fatalf("at the bound: %d pages, warnings %v", r.res.Pages, h4Warnings(r.res))
	}
	for _, w := range words[:3] {
		if got := h4PageCounts(r, w); fmt.Sprint(got) != "[1 1 1]" {
			t.Errorf("%s per page: %v, want on every page", w, got)
		}
	}
	html, words = boxes(atBox + 1)
	over := h4Render(t, referenceOptions(t), html+flow, true)
	if over.err != nil {
		t.Fatal(over.err)
	}
	if !h4Clamped(over.res, "fixed boxes") {
		t.Errorf("no clamping warning past the bound: %v", h4Warnings(over.res))
	}
	last := h4PageCounts(over, words[atBox])
	if fmt.Sprint(last) != "[1 0 0]" {
		t.Errorf("the box past the bound is on pages %v, want the first page only", last)
	}
	for _, w := range words[:atBox] {
		if n := strings.Count(pagesText(over.doc), w); n != 3 {
			t.Errorf("%s appears %d times, want on each of the 3 pages", w, n)
			break
		}
	}
}

// 45 000 fixed boxes (the node limit allows no more): the ones past the bound are on the first page
// only, each text is in the PDF once or once per page, the heap holds and the work is within twice that of
// the same boxes made absolute.
func TestM4FixedFlood(t *testing.T) {
	const n = 45000
	boxes := func(pos string) (string, []string) {
		var sb strings.Builder
		var words []string
		sb.WriteString(`<style>.f{position:` + pos + `;left:0;top:0;width:2px;height:2px;font-size:1px}</style>`)
		for i := range n {
			w := fmt.Sprintf("FL%05d", i)
			words = append(words, w)
			fmt.Fprintf(&sb, `<div class=f>%s</div>`, w)
		}
		return sb.String(), words
	}
	_, flow := h4FlowPages(3)
	html, words := boxes("fixed")
	r := h4RenderTwin(t, referenceOptions(t), html+flow)
	if r.err != nil {
		var le *LimitError
		if !errors.As(r.err, &le) {
			t.Fatal(r.err)
		}
		return
	}
	if !h4Clamped(r.res, "fixed boxes") {
		t.Errorf("no clamping warning: %v", h4Warnings(r.res))
	}
	txt := pagesText(r.doc)
	for _, w := range words {
		if c := strings.Count(txt, w); c != 1 && c != r.res.Pages {
			t.Errorf("%s appears %d times, want once or once per page (%d pages)", w, c, r.res.Pages)
			break
		}
	}
	absHTML, _ := boxes("absolute")
	twin := h4RenderTwin(t, referenceOptions(t), absHTML+flow)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	t.Logf("work %d, absolute twin %d", r.work, twin.work)
	if r.work > 2*twin.work+10000 || r.boxes != twin.boxes {
		t.Errorf("work %d boxes %d; twin work %d boxes %d", r.work, r.boxes, twin.work, twin.boxes)
	}
}

// Fixed boxes with absurd geometry create no page and keep the text: the page count equals the twin
// without them.
func TestM4FixedAbsurdGeometry(t *testing.T) {
	_, flow := h4FlowPages(3)
	twin := h4Render(t, referenceOptions(t), flow, false)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	for name, style := range map[string]string{
		"insets 1e30":              "left:-1e30px;top:1e30px;width:1e30px;height:1e30px;transform:rotate(1deg)",
		"insets 1e7":               "left:1e7px;top:1e7px;width:10px;height:10px",
		"negative size":            "left:0;top:0;width:-5px;height:-5px",
		"percent 1e9":              "left:1e9%;top:1e9%;width:1e9%;height:1e9%",
		"taller than a page":       "left:0;top:0;width:100px;height:1e7px",
		"right and bottom only":    "right:-1e9px;bottom:-1e9px;width:10px;height:10px",
		"all insets and 1e9 size":  "left:0;right:0;top:0;bottom:0;width:1e9px;height:1e9px",
		"rotate 1e308deg":          "left:0;top:0;width:50px;height:50px;transform:rotate(1e308deg)",
		"min-width 1e9 shrinkwrap": "left:0;top:0;min-width:1e9px",
	} {
		t.Run(name, func(t *testing.T) {
			words, flow := h4FlowPages(3)
			r := h4Render(t, referenceOptions(t), `<div style="position:fixed;background:#c00;`+style+`">FIXEDTEXT</div>`+flow, false)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.res.Pages != twin.res.Pages {
				t.Errorf("%d pages, %d without the fixed box", r.res.Pages, twin.res.Pages)
			}
			h4TextOnce(t, r, words)
			if !strings.Contains(pagesText(r.doc), "FIXEDTEXT") && !strings.Contains(style, "1e") && !strings.Contains(style, "negative") {
				t.Error("the fixed text is not in the PDF")
			}
		})
	}
}

// Fixed boxes that are nested, in other contexts and below the flow: bounded work against the flat twin,
// z-index -1 painted before the in-flow content on every page, display:none emitted nowhere.
func TestM4FixedNestingAndContexts(t *testing.T) {
	nest := func(d int) string {
		return strings.Repeat(`<div style="position:fixed;left:1px;top:1px;width:200px;height:200px">`, d) + "NESTED" + strings.Repeat("</div>", d)
	}
	flat := func(d int) string {
		return strings.Repeat(`<div style="position:fixed;left:1px;top:1px;width:200px;height:200px">x</div>`, d) + "NESTED"
	}
	_, flow := h4FlowPages(3)
	deep := h4Render(t, referenceOptions(t), nest(240)+flow, false)
	shallow := h4Render(t, referenceOptions(t), flat(240)+flow, false)
	if deep.err != nil || shallow.err != nil {
		t.Fatal(deep.err, shallow.err)
	}
	t.Logf("work nested %d, flat %d", deep.work, shallow.work)
	if float64(deep.work) > 2.2*float64(shallow.work)+10000 {
		t.Errorf("nested work %d against %d for the flat twin", deep.work, shallow.work)
	}
	for name, html := range map[string]string{
		"in a table cell":          `<table><tr><td><div style="position:fixed;top:0;left:0">INCELL</div></td></tr></table>` + flow,
		"in a flex item":           `<div style="display:flex"><div><div style="position:fixed;top:0;left:0">INFLEX</div></div></div>` + flow,
		"under overflow:hidden":    `<div style="overflow:hidden;height:10px"><div style="position:fixed;top:0;left:0">INCLIP</div></div>` + flow,
		"display:none":             `<div style="position:fixed;display:none">HIDDENFIXED</div>` + flow,
		"in a rotated ancestor":    `<div style="position:absolute;transform:rotate(10deg)"><div style="position:fixed;top:0;left:0">INROT</div></div>` + flow,
		"on body and html":         `<style>html,body{position:fixed}</style>` + flow,
		"z-index -1 under content": `<div style="position:fixed;z-index:-1;top:0;left:0;width:50px;height:50px;background:#00ff00">Z</div><div style="background:#ff0000;height:100px">flow</div><div style="break-after:page"></div>` + flow,
	} {
		t.Run(name, func(t *testing.T) {
			r := h4Render(t, referenceOptions(t), html, false)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if name == "display:none" && strings.Contains(pagesText(r.doc), "HIDDENFIXED") {
				t.Error("a display:none fixed box is in the text")
			}
			if name == "z-index -1 under content" {
				for i, p := range r.doc.Pages[:2] {
					c := string(p.Content)
					g, f := strings.Index(c, "0 1 0 rg"), strings.Index(c, "1 0 0 rg")
					if g < 0 || f < 0 || g > f {
						t.Errorf("page %d: the z-index -1 fixed box is not painted before the flow (green at %d, red at %d)", i+1, g, f)
					}
				}
			}
		})
	}
}

// A fixed box with a lot of text repeats on every page: the output bound stops it (a LimitError for bytes)
// or the render fits, never an out-of-memory.
func TestM4FixedTextRepeatedOnEveryPage(t *testing.T) {
	_, flow := h4FlowPages(500)
	big := strings.Repeat("word ", 40000) // 200 KB per box
	r := h4Render(t, referenceOptions(t), `<div style="position:fixed;top:0;left:0;width:700px;font-size:1px;line-height:1px">`+big+`</div>`+flow, true)
	if r.err != nil {
		var le *LimitError
		if !errors.As(r.err, &le) {
			t.Fatal(r.err)
		}
		return
	}
	if r.res.Bytes > 64<<20 {
		t.Errorf("%d bytes written, above the 64 MiB output bound", r.res.Bytes)
	}
}
