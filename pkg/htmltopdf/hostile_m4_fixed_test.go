package htmltopdf

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Copies of the contracts of internal/layout/fixed.go and the default limits: a change there must fail here.
const (
	h4MaxFixedItems  = 4096 // display items of fixed boxes per page
	h4RepeatShare    = 4    // the repeats of fixed text may take 1/h4RepeatShare of MaxGlyphs
	h4FixedPages     = 500  // MaxPages
	h4PageWorkSlack  = 1024 // work per page beyond its emitted items and re-layouts (measured about 625: page area setup)
	h4HeapSlack      = 64 << 20
	h4FlowWordGlyphs = 12 // glyphs charged for a FLOWnnnnn word of 9 characters (measured 10), with a margin
	fixedClampDetail = "fixed boxes"
)

// h4Size is full, or reduced under -short and the race detector, whose 5 to 10 times slower renders would
// come near the one-minute guard of a render at the full sizes.
func h4Size(full, reduced int) int {
	if testing.Short() || h4Race {
		return reduced
	}
	return full
}

// h4PageCounts returns, for each page of r, how many times marker appears in its text.
func h4PageCounts(r h4Run, marker string) []int {
	out := make([]int, len(r.doc.Pages))
	for i, p := range r.doc.Pages {
		out[i] = strings.Count(strings.Join(p.Text, " "), marker)
	}
	return out
}

// h4FixedBoxes returns n boxes of class f, each holding a distinct marker of the given prefix, and their
// markers; css is the declaration block of the class.
func h4FixedBoxes(css, prefix string, n int) (html string, words []string) {
	var sb strings.Builder
	sb.WriteString(`<style>.f{` + css + `}</style>`)
	for i := range n {
		w := fmt.Sprintf("%s%05d", prefix, i)
		words = append(words, w)
		fmt.Fprintf(&sb, `<div class=f>%s</div>`, w)
	}
	return sb.String(), words
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
	pages := h4Size(h4FixedPages, 150)
	words, flow := h4FlowPages(pages)
	css := `<style>.wm{position:fixed;top:300px;left:100px;width:300px;height:100px;background:#eee;transform:rotate(-18deg)}</style>`
	r := h4RenderTwin(t, referenceOptions(t), css+`<div class=wm>WATERMARK</div>`+flow)
	if r.err != nil {
		t.Fatal(r.err)
	}
	twin := h4RenderTwin(t, referenceOptions(t), css+flow)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	if r.res.Pages != pages || twin.res.Pages != pages {
		t.Fatalf("%d pages with the watermark, %d without", r.res.Pages, twin.res.Pages)
	}
	h4TextOnce(t, r, words)
	for i, n := range h4PageCounts(r, "WATERMARK") {
		if n != 1 {
			t.Fatalf("page %d: %d watermarks", i+1, n)
		}
	}
	const workPerWatermark = 3 // background, text, transform pair per emission
	t.Logf("work %d, twin %d", r.work, twin.work)
	if float64(r.work) > 1.1*float64(twin.work)+float64(pages*workPerWatermark) || (r.boxes != twin.boxes+1 && r.boxes != twin.boxes+2) || r.peak > 2*twin.peak+h4HeapSlack {
		t.Errorf("work %d boxes %d heap %d MiB; twin work %d boxes %d heap %d MiB", r.work, r.boxes, r.peak>>20, twin.work, twin.boxes, twin.peak>>20)
	}
}

// 100 rotated fixed boxes on 500 pages: each of the 100 texts is on every page exactly once, the flow text
// is present once, layout is paid once per box (boxes equal the twin's plus 100) and work grows by the
// emissions only, 500 pages x 100 boxes x a few items.
func TestM4FixedHundredRotatedBoxesOn500Pages(t *testing.T) {
	const nBoxes = 100
	nPages := h4Size(h4FixedPages, 120)
	words, flow := h4FlowPages(nPages)
	var sb strings.Builder
	sb.WriteString(`<style>.r{position:fixed;width:60px;height:20px;background:#fc0;font-size:8px}</style>`)
	for i := range nBoxes {
		fmt.Fprintf(&sb, `<div class=r style="top:%dpx;left:%dpx;transform:rotate(%ddeg)">ROT%03d</div>`, 20+i*9, 10+i*5, i*3%360, i)
	}
	r := h4RenderTwin(t, referenceOptions(t), sb.String()+flow)
	twin := h4RenderTwin(t, referenceOptions(t), flow)
	if r.err != nil || twin.err != nil {
		t.Fatal(r.err, twin.err)
	}
	if r.res.Pages != nPages {
		t.Fatalf("%d pages, want %d", r.res.Pages, nPages)
	}
	h4TextOnce(t, r, words)
	for i := range nBoxes {
		for p, n := range h4PageCounts(r, fmt.Sprintf("ROT%03d", i)) {
			if n != 1 {
				t.Fatalf("box %d on page %d: %d times, want once", i, p+1, n)
			}
		}
	}
	const workPerRotatedBox = 5 // the emission cost of a box plus a margin
	t.Logf("work %d boxes %d heap %d MiB; twin work %d boxes %d", r.work, r.boxes, r.peak>>20, twin.work, twin.boxes)
	if r.boxes > twin.boxes+2*nBoxes || r.work > twin.work+int64(nPages*nBoxes*workPerRotatedBox) || r.peak > 2*twin.peak+h4HeapSlack {
		t.Errorf("work %d boxes %d heap %d MiB; twin work %d boxes %d heap %d MiB", r.work, r.boxes, r.peak>>20, twin.work, twin.boxes, twin.peak>>20)
	}
}

// Empty fixed boxes (no background, no text) over 500 pages count one layer each toward the per-page bound
// and are charged per emission: 100 000 of them exceed MaxNodes; 20 000 render with the "fixed boxes"
// warning, the flow text once, at most 4096 layers on each page after the first, and a Work that
// includes those emissions without growing past them.
func TestM4FixedEmptyFlood(t *testing.T) {
	pages := h4Size(h4FixedPages, 150)
	words, flow := h4FlowPages(pages)
	empties := func(pos string, n int) string {
		return `<style>.e{position:` + pos + `;top:0;left:0}</style>` + strings.Repeat(`<div class=e></div>`, n) + flow
	}
	t.Run("over MaxNodes", func(t *testing.T) {
		r := h4Render(t, referenceOptions(t), empties("fixed", DefaultLimits().MaxNodes+1), false)
		var le *LimitError
		if !errors.As(r.err, &le) || le.Limit != LimitNodes {
			t.Fatalf("%v, want a LimitError on MaxNodes", r.err)
		}
	})
	t.Run("boxes against absolute", func(t *testing.T) {
		n := h4Size(20000, 6000)
		r := h4RenderTwin(t, referenceOptions(t), empties("fixed", n))
		twin := h4Render(t, referenceOptions(t), empties("absolute", n), false)
		if r.err != nil || twin.err != nil {
			t.Fatal(r.err, twin.err)
		}
		h4TextOnce(t, r, words)
		if !m3Clamped(r.res, fixedClampDetail) {
			t.Errorf("no \"fixed boxes\" warning in %v", h4Warnings(r.res))
		}
		emitted := int64((pages - 1) * h4MaxFixedItems)
		if r.work < twin.work+emitted || r.work > twin.work+8*emitted || r.boxes != twin.boxes {
			t.Errorf("work %d boxes %d; absolute twin work %d boxes %d, emitted layers %d", r.work, r.boxes, twin.work, twin.boxes, emitted)
		}
	})
}

// A fixed box inside a fixed box, the inner one without a vertical inset: its text is on every page when
// the document has several, and in the PDF once when it has one.
func TestM4FixedNestedWithoutVerticalInset(t *testing.T) {
	nested := `<div style="position:fixed;top:0;left:0">OUTERMARK<div style="position:fixed;left:200px">INNERMARK</div></div>`
	for _, tc := range []struct {
		name  string
		lines int
		pages bool
	}{{"several pages", 150, true}, {"one page", 3, false}} {
		t.Run(tc.name, func(t *testing.T) {
			words, flow := h4Markers("NL", tc.lines)
			r := h4Render(t, referenceOptions(t), nested+flow, true)
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4TextOnce(t, r, words)
			if multi := r.res.Pages > 1; multi != tc.pages {
				t.Fatalf("%d pages", r.res.Pages)
			}
			for _, w := range []string{"OUTERMARK", "INNERMARK"} {
				for i, n := range h4PageCounts(r, w) {
					if n != 1 {
						t.Errorf("%s on page %d: %d times, want once", w, i+1, n)
					}
				}
			}
		})
	}
}

// Past the per-page bound the repeats are dropped, and their glyphs are not charged: 20 000 fixed
// paragraphs over a 3 000 line flow render, with the "fixed boxes" warning and the flow text once.
func TestM4FixedCapWithRepeats(t *testing.T) {
	boxes := h4Size(20000, 8000)
	var sb strings.Builder
	for range boxes {
		sb.WriteString(`<p style="position:fixed;left:0">ABCDEFGHIJ</p>`)
	}
	words, flow := h4Markers("CL", h4Size(3000, 1000))
	r := h4Render(t, referenceOptions(t), sb.String()+flow, true)
	if r.err != nil {
		t.Fatal(r.err)
	}
	h4TextOnce(t, r, words)
	if !m3Clamped(r.res, fixedClampDetail) {
		t.Errorf("no \"fixed boxes\" warning in %v", h4Warnings(r.res))
	}
	const boxGlyphs, margin = len("ABCDEFGHIJ"), 1 << 16
	// A fixed box's text is charged twice on its first page (layout and emission), once per repeat.
	allowed := int64(DefaultLimits().MaxGlyphs/h4RepeatShare + 2*boxes*boxGlyphs + len(words)*h4FlowWordGlyphs + margin)
	if r.glyphs > allowed {
		t.Errorf("%d glyphs charged, above the repeat quarter, the first copy of the boxes and the flow (%d)", r.glyphs, allowed)
	}
}

// The repeats of fixed text may take a quarter of MaxGlyphs over the whole document: a box whose 500
// repetitions fit is on every page with no warning; a bigger one is on the first K pages only, with the
// "fixed boxes" warning, K about a quarter of MaxGlyphs divided by its glyphs; the flow text is present
// once whatever the box, and the glyphs charged stay within the quarter plus the flow.
func TestM4FixedTextRepeatCapAtTheBound(t *testing.T) {
	if h4Size(0, 1) == 1 {
		t.Skip("needs the 500 pages whose repeats reach the glyph quarter")
	}
	words, flow := h4FlowPages(h4FixedPages)
	quarter := int64(DefaultLimits().MaxGlyphs / h4RepeatShare)
	for _, tc := range []struct {
		name         string
		fillers      int
		minK, maxK   int
		wantClamping bool
	}{
		{"just under the quarter", 300, 500, 500, false},
		{"just over the quarter", 340, 470, 495, true},
		{"twice the quarter", 700, 220, 250, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := "FIXMARK " + strings.Repeat("ab ", tc.fillers)
			r := h4Render(t, referenceOptions(t), `<div style="position:fixed;top:0;left:0;width:700px;font-size:1px;line-height:1px">`+text+`</div>`+flow, true)
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4TextOnce(t, r, words)
			counts := h4PageCounts(r, "FIXMARK")
			k := 0
			for k < len(counts) && counts[k] == 1 {
				k++
			}
			for i := k; i < len(counts); i++ {
				if counts[i] != 0 {
					t.Fatalf("page %d has the box %d times after it stopped at page %d", i+1, counts[i], k)
				}
			}
			if k < tc.minK || k > tc.maxK {
				t.Errorf("the box is on %d pages, want %d to %d", k, tc.minK, tc.maxK)
			}
			if got := m3Clamped(r.res, fixedClampDetail); got != tc.wantClamping {
				t.Errorf("fixed boxes warning %v, want %v (%v)", got, tc.wantClamping, h4Warnings(r.res))
			}
			if allowed := quarter + int64(len(words)*h4FlowWordGlyphs+len(text)); r.glyphs > allowed {
				t.Errorf("%d glyphs charged, above the quarter %d plus the flow and the first copy (%d)", r.glyphs, quarter, allowed)
			}
		})
	}
}

// Late fixed boxes on a page 100 px high: a fixed box A after a 300 px block that cannot break, then a
// fixed box B in a div of its own; both texts are in the PDF at least once, and the flow text once, also
// when 4100 pinned empty boxes before them exceed the per-page bound.
func TestM4FixedLateBoxesOnShortPages(t *testing.T) {
	const doc = `<style>@page{margin:0}</style><div style="height:300px;break-inside:avoid">FLOWBLOCK</div>` +
		`<div style="position:fixed;left:0">TEXTA</div>` +
		`<div><div style="position:fixed;left:0">TEXTB</div></div><div style="height:50px">TAIL</div>`
	for name, pre := range map[string]string{
		"plain":                       "",
		"pinned empties over the cap": strings.Repeat(`<div style="position:fixed;top:0;left:0"></div>`, h4MaxFixedItems+4),
	} {
		t.Run(name, func(t *testing.T) {
			r := h4Render(t, h4PageSizeOpts(t, PageSize{Width: 400, Height: 100}, ""), pre+doc, true)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.res.Pages < 3 {
				t.Fatalf("%d pages, want the 300 px block over at least 3", r.res.Pages)
			}
			h4TextOnce(t, r, []string{"FLOWBLOCK", "TAIL"})
			for _, w := range []string{"TEXTA", "TEXTB"} {
				total := 0
				for _, n := range h4PageCounts(r, w) {
					total += n
				}
				if total == 0 {
					t.Errorf("%s is not in the PDF (%d pages)", w, r.res.Pages)
				}
			}
		})
	}
}

// One pinned box holding 20 000 empty fixed boxes over 500 pages whose page areas all differ (a named
// page with its own top margin each): the render carries the "fixed boxes" warning and its Work lies
// between the emissions of the 4096 layers of each page and twice that.
func TestM4FixedNestedEmptyBoxesOnDistinctPageAreas(t *testing.T) {
	pages := h4Size(h4FixedPages, 150)
	var css, flow strings.Builder
	words := make([]string, 0, pages)
	for i := range pages {
		fmt.Fprintf(&css, "@page p%d{margin:%gpx 0 0 0}", i, float64(i)/2)
		w := fmt.Sprintf("NP%05d", i)
		words = append(words, w)
		fmt.Fprintf(&flow, `<div style="page:p%d;break-after:page">%s</div>`, i, w)
	}
	build := func(inner string) string {
		return "<style>" + css.String() + `</style><div style="position:fixed;top:0;left:0">` + inner + "</div>" + flow.String()
	}
	r := h4Render(t, h4PageSizeOpts(t, PageSize{Width: 600, Height: 400}, ""), build(strings.Repeat(`<div style="position:fixed;left:1px;top:1px"></div>`, h4Size(20000, 6000))), true)
	twin := h4Render(t, h4PageSizeOpts(t, PageSize{Width: 600, Height: 400}, ""), build(""), false)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	h4TextOnce(t, r, words)
	if !m3Clamped(r.res, fixedClampDetail) {
		t.Errorf("no \"fixed boxes\" warning in %v", h4Warnings(r.res))
	}
	// Each page charges its 4096 emitted items, and the kept layers are laid out again for every distinct
	// page area (measured: 2.4 M with one area, 4.2 M with 500), plus a little.
	floor, bound := twin.work+int64(pages*h4MaxFixedItems), twin.work+int64(pages*(2*h4MaxFixedItems+h4PageWorkSlack))
	if r.work < floor || r.work > bound {
		t.Errorf("work %d, want %d to %d (twin %d)", r.work, floor, bound, twin.work)
	}
}

// The per-page bound of fixed display items (4096): boxes of two items each (a background and a text), at
// the bound every box is on every page with no warning; one box more is emitted on the first page only
// with a "fixed boxes" warning, and its text is in the PDF exactly once.
func TestM4FixedItemsPerPageBound(t *testing.T) {
	boxes := func(n int) (string, []string) {
		return h4FixedBoxes("position:fixed;left:0;top:0;width:3px;height:3px;background:#c00;font-size:1px", "FX", n)
	}
	flowWords, flow := h4FlowPages(3)
	atBox := h4MaxFixedItems / 2
	html, words := boxes(atBox)
	r := h4Render(t, referenceOptions(t), html+flow, true)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.res.Pages != 3 || m3Clamped(r.res, fixedClampDetail) {
		t.Fatalf("at the bound: %d pages, warnings %v", r.res.Pages, h4Warnings(r.res))
	}
	h4TextOnce(t, r, flowWords)
	tokens := h4Tokens(r)
	for _, w := range words {
		if tokens[w] != r.res.Pages {
			t.Fatalf("%s appears %d times, want on each of the %d pages", w, tokens[w], r.res.Pages)
		}
	}
	html, words = boxes(atBox + 1)
	over := h4Render(t, referenceOptions(t), html+flow, true)
	if over.err != nil {
		t.Fatal(over.err)
	}
	if !m3Clamped(over.res, fixedClampDetail) {
		t.Errorf("no clamping warning past the bound: %v", h4Warnings(over.res))
	}
	last := h4PageCounts(over, words[atBox])
	if !slices.Equal(last, []int{1, 0, 0}) {
		t.Errorf("the box past the bound is on pages %v, want the first page only", last)
	}
	h4TextOnce(t, over, flowWords)
	tokens = h4Tokens(over)
	for _, w := range words[:atBox] {
		if tokens[w] != over.res.Pages {
			t.Fatalf("%s appears %d times, want on each of the %d pages", w, tokens[w], over.res.Pages)
		}
	}
}

// 33 000 fixed boxes (6 000 when reduced; 33 000 is near what the node limit allows): the ones past the per-page bound are on the first
// page only, the others on every page, every text is in the PDF once or once per page, the work is within
// twice that of the same boxes made absolute and the heap holds.
func TestM4FixedFlood(t *testing.T) {
	n := h4Size(33000, 6000)
	boxes := func(pos string) (string, []string) {
		return h4FixedBoxes("position:"+pos+";left:0;top:0;width:2px;height:2px;font-size:1px", "FL", n)
	}
	flowWords, flow := h4FlowPages(3)
	html, words := boxes("fixed")
	r := h4RenderTwin(t, referenceOptions(t), html+flow)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !m3Clamped(r.res, fixedClampDetail) {
		t.Errorf("no clamping warning: %v", h4Warnings(r.res))
	}
	h4TextOnce(t, r, flowWords)
	tokens := h4Tokens(r)
	once, everyPage := 0, 0
	for _, w := range words {
		switch tokens[w] {
		case 1:
			once++
		case r.res.Pages:
			everyPage++
		default:
			t.Fatalf("%s appears %d times, want once or once per page (%d pages)", w, tokens[w], r.res.Pages)
		}
	}
	if everyPage == 0 || everyPage > h4MaxFixedItems || once+everyPage != n {
		t.Errorf("%d boxes on every page, %d on the first only, of %d", everyPage, once, n)
	}
	absHTML, _ := boxes("absolute")
	twin := h4RenderTwin(t, referenceOptions(t), absHTML+flow)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	if r.work > 2*twin.work+twin.work/8 || r.boxes != twin.boxes {
		t.Errorf("work %d boxes %d; twin work %d boxes %d", r.work, r.boxes, twin.work, twin.boxes)
	}
}

// Fixed boxes with absurd geometry create no page and keep the flow text; a box that lies on the page has
// its own text in the PDF too.
func TestM4FixedAbsurdGeometry(t *testing.T) {
	words, flow := h4FlowPages(3)
	twin := h4Render(t, referenceOptions(t), flow, false)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	for _, tc := range []struct {
		name, style string
		wantText    bool
	}{
		{"insets 1e30", "left:-1e30px;top:1e30px;width:1e30px;height:1e30px;transform:rotate(1deg)", false},
		{"insets 1e7", "left:1e7px;top:1e7px;width:10px;height:10px", false},
		{"negative size", "left:0;top:0;width:-5px;height:-5px", true},
		{"percent 1e9", "left:1e9%;top:1e9%;width:1e9%;height:1e9%", false},
		{"taller than a page", "left:0;top:0;width:100px;height:1e7px", true},
		{"right and bottom only", "right:-1e9px;bottom:-1e9px;width:10px;height:10px", false},
		{"all insets and 1e9 size", "left:0;right:0;top:0;bottom:0;width:1e9px;height:1e9px", true},
		{"rotate 1e308deg", "left:0;top:0;width:50px;height:50px;transform:rotate(1e308deg)", true},
		{"min-width 1e9 shrinkwrap", "left:0;top:0;min-width:1e9px", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := h4Render(t, referenceOptions(t), `<div style="position:fixed;background:#c00;`+tc.style+`">FIXEDTEXT</div>`+flow, true)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.res.Pages != twin.res.Pages {
				t.Errorf("%d pages, %d without the fixed box", r.res.Pages, twin.res.Pages)
			}
			h4TextOnce(t, r, words)
			if !strings.Contains(pagesText(r.doc), "FIXEDTEXT") && tc.wantText {
				t.Error("the fixed text is not in the PDF")
			}
		})
	}
}

// Fixed boxes that are nested, in other contexts and below the flow: bounded work against the flat twin,
// z-index -1 painted on every page and before the in-flow content on the first, display:none emitted nowhere.
func TestM4FixedNestingAndContexts(t *testing.T) {
	nest := func(d int) string {
		return strings.Repeat(`<div style="position:fixed;left:1px;top:1px;width:200px;height:200px">`, d) + "NESTED" + strings.Repeat("</div>", d)
	}
	flat := func(d int) string {
		return strings.Repeat(`<div style="position:fixed;left:1px;top:1px;width:200px;height:200px">x</div>`, d) + "NESTED"
	}
	flowWords, flow := h4FlowPages(3)
	deep := h4Render(t, referenceOptions(t), nest(240)+flow, true)
	shallow := h4Render(t, referenceOptions(t), flat(240)+flow, true)
	if deep.err != nil || shallow.err != nil {
		t.Fatal(deep.err, shallow.err)
	}
	h4TextOnce(t, deep, flowWords)
	h4TextOnce(t, shallow, flowWords)
	if got := h4PageCounts(deep, "NESTED"); !slices.Equal(got, []int{1, 1, 1}) {
		t.Errorf("NESTED in the innermost fixed box is on pages %v, want every page", got)
	}
	if got := h4PageCounts(shallow, "NESTED"); !slices.Equal(got, []int{1, 0, 0}) {
		t.Errorf("NESTED in the flow after the flat boxes is on pages %v, want the first only", got)
	}
	t.Logf("work nested %d, flat %d", deep.work, shallow.work)
	if float64(deep.work) > 2.2*float64(shallow.work)+10000 {
		t.Errorf("nested work %d against %d for the flat twin", deep.work, shallow.work)
	}
	zPainted := func(t *testing.T, r h4Run) {
		for i, p := range r.doc.Pages[:2] {
			c := string(p.Content)
			g, f := strings.Index(c, "0 1 0 rg"), strings.Index(c, "1 0 0 rg")
			if g < 0 {
				t.Errorf("page %d: the z-index -1 fixed box is not painted", i+1)
			}
			if i == 0 && (f < 0 || g > f) {
				t.Errorf("page 1: the z-index -1 fixed box is not painted before the flow (green at %d, red at %d)", g, f)
			}
		}
	}
	notInText := func(word string) func(*testing.T, h4Run) {
		return func(t *testing.T, r h4Run) {
			if strings.Contains(pagesText(r.doc), word) {
				t.Errorf("%s is in the text", word)
			}
		}
	}
	for _, tc := range []struct {
		name, html string
		want       string
		check      func(*testing.T, h4Run)
	}{
		{"in a table cell", `<table><tr><td><div style="position:fixed;top:0;left:0">INCELL</div></td></tr></table>` + flow, "INCELL", nil},
		{"in a flex item", `<div style="display:flex"><div><div style="position:fixed;top:0;left:0">INFLEX</div></div></div>` + flow, "INFLEX", nil},
		{"under overflow:hidden", `<div style="overflow:hidden;height:10px"><div style="position:fixed;top:0;left:0">INCLIP</div></div>` + flow, "INCLIP", nil},
		{"display:none", `<div style="position:fixed;display:none">HIDDENFIXED</div>` + flow, "", notInText("HIDDENFIXED")},
		{"in a rotated ancestor", `<div style="position:absolute;transform:rotate(10deg)"><div style="position:fixed;top:0;left:0">INROT</div></div>` + flow, "INROT", nil},
		{"on body and html", `<style>html,body{position:fixed}</style>` + flow, "", nil},
		{"z-index -1 under content", `<div style="position:fixed;z-index:-1;top:0;left:0;width:50px;height:50px;background:#00ff00">Z</div><div style="background:#ff0000;height:100px">flow</div><div style="break-after:page"></div>` + flow, "", zPainted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := h4Render(t, referenceOptions(t), tc.html, true)
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4TextOnce(t, r, flowWords)
			if tc.want != "" && !strings.Contains(pagesText(r.doc), tc.want) {
				t.Errorf("%s is not in the PDF", tc.want)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
}

// A fixed box with 200 KB of text: its repeats stop at the glyph cap (the box is on the first pages only,
// with the "fixed boxes" warning), the flow text is present once on all 500 pages and the output stays small.
func TestM4FixedTextRepeatedOnEveryPage(t *testing.T) {
	words, flow := h4FlowPages(h4Size(h4FixedPages, 150))
	big := strings.Repeat("word ", 40000)
	r := h4Render(t, referenceOptions(t), `<div style="position:fixed;top:0;left:0;width:700px;font-size:1px;line-height:1px">`+big+`</div>`+flow, true)
	if r.err != nil {
		t.Fatal(r.err)
	}
	h4TextOnce(t, r, words)
	if !m3Clamped(r.res, fixedClampDetail) {
		t.Errorf("no clamping warning: %v", h4Warnings(r.res))
	}
	with := 0
	for _, n := range h4PageCounts(r, "word") {
		if n > 0 {
			with++
		}
	}
	if with == 0 || with > 5 {
		t.Errorf("the box is on %d of %d pages, want the first few", with, r.res.Pages)
	}
	if r.res.Bytes > 8<<20 {
		t.Errorf("%d bytes written", r.res.Bytes)
	}
}
