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

// M3-11: shadows of a box split over pages are not drawn on its cut edges.
// Pages are A4 at Scale 0.9: one CSS px is 0.675 pt.

const (
	cutCSS     = `<style>@page{margin:100px} body{margin:0} .b{line-height:100px;font-size:10px}</style>`
	pxToPt     = 0.675
	m3Guard    = time.Minute
	m3HeapCeil = 256 << 20
)

var clipRectRE = regexp.MustCompile(`(?m)^(-?[0-9.]+) (-?[0-9.]+) (-?[0-9.]+) (-?[0-9.]+) re\nW n$`)

type m3Run struct {
	res  *Result
	data []byte
	doc  *pdftest.Doc
	peak uint64
	took time.Duration
}

func m3Render(t *testing.T, html string) m3Run {
	t.Helper()
	r := h4Render(t, m2Options(t), html, true)
	if r.err != nil {
		t.Fatalf("render: %v", r.err)
	}
	return m3Run{res: r.res, data: r.data, doc: r.doc, peak: r.peak, took: r.took}
}

func (r m3Run) fills(page int) int {
	return bytes.Count(r.doc.Pages[page].Content, []byte("\nf\n"))
}

func (r m3Run) balanced(t *testing.T) {
	t.Helper()
	for i, p := range r.doc.Pages {
		var q, Q int
		for l := range bytes.SplitSeq(p.Content, []byte("\n")) {
			switch string(l) {
			case "q":
				q++
			case "Q":
				Q++
			}
		}
		if q != Q {
			t.Errorf("page %d: %d q for %d Q", i, q, Q)
		}
	}
}

func clipsOf(t *testing.T, content []byte) [][4]float64 {
	t.Helper()
	var out [][4]float64
	for _, m := range clipRectRE.FindAllSubmatch(content, -1) {
		var v [4]float64
		for i := range v {
			f, err := strconv.ParseFloat(string(m[i+1]), 64)
			if err != nil {
				t.Fatal(err)
			}
			v[i] = f
		}
		out = append(out, v)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.02 }

func lines(n int) string { return strings.Repeat("x<br>", n-1) + "x" }

// A box of 30 lines over 3 pages with a shadow shifted 30px down: the clip of
// each fragment extends by the shadow extent only on the uncut sides, and the
// pages and text are those of the twin without a shadow.
func TestM3ShadowCutEdgesAreClipped(t *testing.T) {
	box := func(style string) string {
		return cutCSS + `<div class=b style="` + style + `">` + lines(30) + `</div>`
	}
	with := m3Render(t, box("box-shadow:0 30px 0 0 rgba(0,0,0,.5)"))
	twin := m3Render(t, box(""))
	if with.res.Pages != 3 || twin.res.Pages != 3 {
		t.Fatalf("%d pages with the shadow, %d without, want 3", with.res.Pages, twin.res.Pages)
	}
	if pagesText(with.doc) != pagesText(twin.doc) {
		t.Error("the shadow changes the text")
	}
	ext := 30 * pxToPt
	for i, p := range with.doc.Pages {
		clips := clipsOf(t, p.Content)
		fills := regexp.MustCompile(`(?m)^(-?[0-9.]+) (-?[0-9.]+) (-?[0-9.]+) (-?[0-9.]+) re\nf$`).FindAllSubmatch(p.Content, -1)
		if len(clips) != 1 || len(fills) != 1 || with.fills(i) != 1 {
			t.Fatalf("page %d: %d clips, %d fills", i, len(clips), len(fills))
		}
		var f [4]float64
		for k := range f {
			f[k], _ = strconv.ParseFloat(string(fills[0][k+1]), 64)
		}
		bottom, top := f[1]+ext, f[1]+f[3]+ext // fragment edges: the shadow is shifted down by ext
		c := clips[0]
		cb, ct := c[1], c[1]+c[3]
		first, last := i == 0, i == len(with.doc.Pages)-1
		// A cut side is exactly the fragment edge; an uncut side reaches at least the shadow extent.
		okBottom := near(cb, bottom)
		if last {
			okBottom = cb <= bottom-ext+0.02
		}
		okTop := near(ct, top)
		if first {
			okTop = ct >= top+ext-0.02
		}
		if !okBottom || !okTop {
			t.Errorf("page %d: clip y %.2f..%.2f, fragment %.2f..%.2f (extent %.2f)", i, cb, ct, bottom, top, ext)
		}
	}
	with.balanced(t)
}

// An unsplit shadowed box is drawn exactly as before: its shadow has no clip.
func TestM3ShadowOfUnsplitBoxHasNoClip(t *testing.T) {
	r := m3Render(t, cutCSS+`<div class=b style="box-shadow:0 3px 5px 2px rgba(0,0,0,.5)">`+lines(3)+`</div>`)
	if r.res.Pages != 1 {
		t.Fatalf("%d pages", r.res.Pages)
	}
	if n := len(clipsOf(t, r.doc.Pages[0].Content)); n != 0 {
		t.Errorf("%d clips on an unsplit box", n)
	}
	if r.fills(0) == 0 {
		t.Error("the shadow is not drawn")
	}
}

// A shadow with nothing visible (transparent, shrunk to nothing) on a split box
// adds neither a clip nor an unbalanced q/Q.
func TestM3InvisibleShadowOnSplitBoxAddsNothing(t *testing.T) {
	for _, shadow := range []string{"0 0 0 0 transparent", "0 0 0 -500px red"} {
		with := m3Render(t, cutCSS+`<div class=b style="box-shadow:`+shadow+`">`+lines(30)+`</div>`)
		without := m3Render(t, cutCSS+`<div class=b>`+lines(30)+`</div>`)
		if !bytes.Equal(pdfContentOf(with.doc), pdfContentOf(without.doc)) {
			t.Errorf("%s: the content stream differs from the twin without a shadow", shadow)
		}
	}
}

func pdfContentOf(d *pdftest.Doc) []byte {
	var b bytes.Buffer
	for _, p := range d.Pages {
		b.Write(p.Content)
	}
	return b.Bytes()
}

// 400 nested boxes, each with 8 blurred shadows, all split by the same page
// cuts: the page bound holds (4096 rectangles), the clip pairs stay balanced
// and the twin without shadows has the same pages and text.
func TestM3NestedSplitShadowedBoxes(t *testing.T) {
	const depth = 400
	shadow := "box-shadow:" + strings.TrimSuffix(strings.Repeat("1px 2px 8px 1px red,", 8), ",")
	doc := func(style string) string {
		return cutCSS + `<style>.n{` + style + `}</style>` + strings.Repeat(`<div class="n">`, depth) + `<div class=b>` + lines(30) + `</div>` + strings.Repeat("</div>", depth)
	}
	with := m3Render(t, doc(shadow))
	twin := m3Render(t, doc(""))
	if with.res.Pages != twin.res.Pages || pagesText(with.doc) != pagesText(twin.doc) {
		t.Errorf("pages %d vs %d, or text differs", with.res.Pages, twin.res.Pages)
	}
	for i := range with.doc.Pages {
		if n := with.fills(i); n > maxShadowItemsPerPage {
			t.Errorf("page %d: %d fills, bound %d", i, n, maxShadowItemsPerPage)
		}
	}
	if with.fills(0) != maxShadowItemsPerPage || !hasClampWarning(with.res) {
		t.Errorf("page 1: %d fills, warned %v; want %d and a clamping warning", with.fills(0), hasClampWarning(with.res), maxShadowItemsPerPage)
	}
	with.balanced(t)
}

// The 4096 bound is exact on a page whose last box is split: 1023 blurred
// boxes (4092 rectangles) then a split box with 8 flat shadows: 4 are drawn on
// page 1, the counter restarts on page 2 and draws all 8.
func TestM3SplitBoxAtTheShadowBound(t *testing.T) {
	blurred := `<div style="padding-top:20px;margin-bottom:-20px;box-shadow:1px 2px 8px 1px red"></div>`
	flat := "box-shadow:" + strings.TrimSuffix(strings.Repeat("1px 2px 0 1px red,", 8), ",")
	html := cutCSS + strings.Repeat(blurred, 1023) + `<div class=b style="` + flat + `">` + lines(30) + `</div>`
	r := m3Render(t, html)
	if r.res.Pages < 3 {
		t.Fatalf("%d pages", r.res.Pages)
	}
	if r.fills(0) != maxShadowItemsPerPage || !hasClampWarning(r.res) {
		t.Errorf("page 1: %d fills, warned %v", r.fills(0), hasClampWarning(r.res))
	}
	if r.fills(1) != 8 {
		t.Errorf("page 2: %d fills, want the 8 shadows of the continued box", r.fills(1))
	}
	r.balanced(t)
}

// 1200 shadowed boxes of 3 lines flow over about 300 pages: every page cut
// splits one box. Pages and text equal the twin, every page respects the bound
// and the q/Q pairs balance.
func TestM3ManyPageCutsThroughShadowedBoxes(t *testing.T) {
	const n = 1200
	box := `<div class=b style="box-shadow:0 5px 9px 3px rgba(0,0,0,.4)">` + lines(3) + `</div>`
	with := m3Render(t, cutCSS+strings.Repeat(box, n))
	twin := m3Render(t, cutCSS+strings.Repeat(strings.Replace(box, "box-shadow:0 5px 9px 3px rgba(0,0,0,.4)", "", 1), n))
	if with.res.Pages != twin.res.Pages {
		t.Fatalf("%d pages with shadows, %d without", with.res.Pages, twin.res.Pages)
	}
	if pagesText(with.doc) != pagesText(twin.doc) {
		t.Error("the shadows change the text")
	}
	clipped := 0
	for i := range with.doc.Pages {
		if with.fills(i) > maxShadowItemsPerPage {
			t.Errorf("page %d: %d fills", i, with.fills(i))
		}
		clipped += len(clipsOf(t, with.doc.Pages[i].Content))
	}
	if clipped < with.res.Pages-1 {
		t.Errorf("%d clips for %d page cuts", clipped, with.res.Pages-1)
	}
	with.balanced(t)
}

// M3-08: translucent gradients cost 2 shadings each (colour + alpha mask) of
// the 4096 allowed: 2048 distinct ones render, the 2049th is painted as its
// mid colour with a WarnClamped; identical ones are shared and never clamp.
func TestM3TranslucentGradientsAtTheShadingLimit(t *testing.T) {
	doc := func(n int, same bool) string {
		var sb strings.Builder
		sb.WriteString(`<style>body{margin:0} div{height:2px;margin:0}</style>`)
		for i := range n {
			a, b := i%256, i/256
			if same {
				a, b = 7, 9
			}
			fmt.Fprintf(&sb, `<div style="background:linear-gradient(90deg,rgba(%d,%d,0,.5),rgba(0,0,255,.8))"></div>`, a, b)
		}
		return sb.String()
	}
	clamped := func(res *Result) bool {
		for _, w := range res.Warnings {
			if w.Code == WarnClamped && strings.HasPrefix(w.Detail, "gradients >") {
				return true
			}
		}
		return false
	}
	at := m3Render(t, doc(2048, false))
	if clamped(at.res) {
		t.Errorf("2048 distinct translucent gradients warn: %+v", at.res.Warnings)
	}
	over := m3Render(t, doc(2049, false))
	if !clamped(over.res) {
		t.Errorf("2049 distinct translucent gradients do not warn")
	}
	same := m3Render(t, doc(5000, true))
	if clamped(same.res) {
		t.Errorf("5000 identical gradients clamp: %+v", same.res.Warnings)
	}
	for name, r := range map[string]m3Run{"2048": at, "2049": over, "5000 same": same} {
		shaded := 0
		for _, p := range r.doc.Pages {
			shaded += bytes.Count(p.Content, []byte(" sh\n"))
		}
		t.Logf("%s: %d pages, %d shadings painted, %v, heap %d MiB", name, r.res.Pages, shaded, r.took, r.peak>>20)
		r.balanced(t)
	}
}

// M3-02: generated content through Render. 2000 paragraphs of 100 KB of
// ::before text stop at the 1 MiB cap with a WarnClamped, the 1 MiB twin does
// not warn, and counters nested 500 deep cost like the flat twin.
func TestM3GeneratedTextCapThroughRender(t *testing.T) {
	const piece = 100 << 10
	long := strings.Repeat("a", piece)
	doc := func(n int) string {
		return `<style>p{margin:0;font-size:1px;line-height:1px} p::before{content:"` + long + `"}</style>` + strings.Repeat("<p></p>", n)
	}
	clamped := func(res *Result) bool {
		for _, w := range res.Warnings {
			if w.Code == WarnClamped && w.Detail == "content" {
				return true
			}
		}
		return false
	}
	over := m3Render(t, doc(2000))
	if !clamped(over.res) {
		t.Errorf("2000 x 100 KB of generated text: no clamping warning (%+v)", over.res.Warnings)
	}
	under := m3Render(t, doc((1<<20)/piece))
	if clamped(under.res) {
		t.Errorf("a 1 MiB total warns: %+v", under.res.Warnings)
	}
	t.Logf("over: %d pages %v heap %d MiB; under: %d pages %v heap %d MiB", over.res.Pages, over.took, over.peak>>20, under.res.Pages, under.took, under.peak>>20)
	over.balanced(t)
}

// m3RenderTwin renders html twice and returns the run with the larger heap peak: a single run varies by
// several MiB under load (the M2 heap-assertion rule).
func m3RenderTwin(t *testing.T, html string) m3Run {
	t.Helper()
	a, b := m3Render(t, html), m3Render(t, html)
	if b.peak > a.peak {
		return b
	}
	return a
}

func TestM3DeepCounterNestingCostsLikeFlat(t *testing.T) {
	const depth = 480
	css := `<style>div{counter-reset:c;counter-increment:c} div::before{content:counter(c)}</style>`
	deep := m3Render(t, css+strings.Repeat("<div>", depth)+strings.Repeat("</div>", depth))
	flat := m3RenderTwin(t, css+strings.Repeat("<div></div>", depth))
	t.Logf("deep heap %d MiB, flat heap %d MiB", deep.peak>>20, flat.peak>>20)
	if deep.peak > flat.peak+32<<20 || deep.peak > 64<<20 {
		t.Errorf("nesting costs %d MiB more than the flat twin (%d MiB deep)", (deep.peak-min(deep.peak, flat.peak))>>20, deep.peak>>20)
	}
	if !strings.Contains(pagesText(deep.doc), "1") {
		t.Error("no counter text")
	}
}

// Counter values and decimal-leading-zero reach the PDF text like Chrome's.
func TestM3CountersInThePDFText(t *testing.T) {
	r := m3Render(t, `<style>li{counter-increment:n} li::before{content:counter(n,decimal-leading-zero) ". "} ol{list-style:none;counter-reset:n 0}</style><ol>`+strings.Repeat("<li>x</li>", 12)+`</ol>`)
	txt := pagesText(r.doc)
	for _, want := range []string{"01", "09", "10", "12"} {
		if !strings.Contains(txt, want+".") && !strings.Contains(txt, want) {
			t.Errorf("%q missing from %q", want, txt)
		}
	}
}

// M3-05b: inline SVG through Render. 10 000 icons render (valid PDF) at a
// time and heap in proportion to the 5 000 twin; hostile viewBoxes and sizes
// never fail the render; currentColor takes the colour of the box.
func TestM3ManyInlineSVGIcons(t *testing.T) {
	doc := func(n int) string {
		return `<style>body{margin:0} svg{width:8px;height:8px}</style>` + strings.Repeat(`<svg viewBox="0 0 24 24" fill="none" stroke="red" stroke-width="2"><path d="M3 12h18M12 3v18"/></svg>`, n)
	}
	half := m3RenderTwin(t, doc(5000))
	full := m3Render(t, doc(10000))
	t.Logf("5000: %d pages heap %d MiB; 10000: %d pages heap %d MiB", half.res.Pages, half.peak>>20, full.res.Pages, full.peak>>20)
	if full.peak > 2*half.peak+32<<20 || full.peak > 128<<20 {
		t.Errorf("heap %d MiB against %d MiB for the twin", full.peak>>20, half.peak>>20)
	}
	full.balanced(t)
}

func TestM3HostileSVGAttributesRender(t *testing.T) {
	for name, attrs := range map[string]string{
		"viewBox 0":        `viewBox="0 0 0 0"`,
		"viewBox negative": `viewBox="0 0 -5 -5"`,
		"viewBox 1e9":      `viewBox="0 0 1e9 1e9"`,
		"viewBox 1e-9":     `viewBox="0 0 1e-9 1e-9"`,
		"viewBox NaN":      `viewBox="NaN NaN NaN NaN"`,
		"viewBox huge min": `viewBox="1e308 -1e308 1e308 1e308"`,
		"viewBox garbage":  `viewBox="a b c d"`,
		"size 1e30":        `viewBox="0 0 24 24" style="width:1e30px;height:1e30px"`,
		"size 0":           `viewBox="0 0 24 24" style="width:0;height:0"`,
		"size negative":    `viewBox="0 0 24 24" style="width:-5px;height:-5px"`,
		"size 1e9 wide":    `viewBox="0 0 24 24" style="width:1e9px;height:10px"`,
		"1e7 icon":         `viewBox="0 0 24 24" style="width:1e7px;height:10px"`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), m3Guard)
			defer cancel()
			var buf bytes.Buffer
			html := `<p>a <svg ` + attrs + ` stroke="blue"><path d="M0 0L24 24M0 24L24 0" stroke-width="2"/><circle cx="12" cy="12" r="1e9"/></svg> b</p>`
			_, err := mustNew(t, m2Options(t)).Render(ctx, strings.NewReader(html), &buf)
			if err != nil {
				var le *LimitError
				if !errors.As(err, &le) {
					t.Fatalf("%v", err)
				}
				return
			}
			pdftest.Validate(t, buf.Bytes())
		})
	}
}

func TestM3SVGCurrentColorReachesThePDF(t *testing.T) {
	r := m3Render(t, `<div style="color:#ff0000"><svg viewBox="0 0 10 10" style="width:20px;height:20px" fill="none" stroke="currentColor"><path d="M0 0L10 10"/></svg></div>`+
		`<div style="color:#0000ff"><svg viewBox="0 0 10 10" style="width:20px;height:20px" fill="currentColor"><rect width="5" height="5"/></svg></div>`)
	c := string(pdfContentOf(r.doc))
	if !strings.Contains(c, "1 0 0 RG") || !strings.Contains(c, "0 0 1 rg") {
		t.Errorf("currentColor not resolved per box:\n%s", c)
	}
}

// M3-04: painting order of overlapping absolute boxes (CSS 2.1 Appendix E): negative z-index, then the
// in-flow content, then z auto and 0 in tree order, then positive z-indices ascending.
func TestM3AbsolutePaintingOrder(t *testing.T) {
	box := func(color string, z string) string {
		return `<div style="position:absolute;left:5px;top:5px;width:60px;height:60px;background:` + color + `;` + z + `"></div>`
	}
	html := `<div style="position:relative;height:100px;background:#0000ff">` +
		box("#ff0000", "z-index:2") + box("#00ff00", "") + box("#ffff00", "z-index:-1") + box("#ff00ff", "z-index:1") + box("#00ffff", "z-index:0") + `</div>`
	r := m3Render(t, html)
	c := string(pdfContentOf(r.doc))
	order := []string{"1 1 0 rg", "0 0 1 rg", "0 1 0 rg", "0 1 1 rg", "1 0 1 rg", "1 0 0 rg"}
	last := -1
	for _, col := range order {
		i := strings.Index(c, col)
		if i < 0 {
			t.Fatalf("%q not painted:\n%s", col, c)
		}
		if i < last {
			t.Errorf("%q painted before the previous colour of the expected order %v", col, order)
		}
		last = i
	}
}

// Absolute floods through Render: 10 000 absolute boxes, insets of ±1e7 px and 1e30, never create a
// page nor fail the render.
func TestM3AbsoluteFloodsThroughRender(t *testing.T) {
	twin := m3Render(t, `<div style="height:300px;position:relative">flow</div>`)
	flood := `<div style="height:300px;position:relative">flow` + strings.Repeat(`<i style="position:absolute;left:3px;top:4px;width:5px;height:5px;background:#c00"></i>`, 10000) + `</div>`
	r := m3Render(t, flood)
	if r.res.Pages != twin.res.Pages {
		t.Errorf("%d pages with 10000 absolute boxes, %d without", r.res.Pages, twin.res.Pages)
	}
	r.balanced(t)
	for _, style := range []string{"left:1e7px;top:1e7px", "right:-1e7px;bottom:-1e7px;width:1e7px;height:1e7px", "left:1e30px;top:-1e30px;width:1e30px", "left:0;right:0;top:0;bottom:0;height:1e9px"} {
		r := m3Render(t, `<div style="height:300px;position:relative">flow<div style="position:absolute;background:#c00;`+style+`">x</div></div>`)
		if r.res.Pages != twin.res.Pages {
			t.Errorf("%q: %d pages, want %d", style, r.res.Pages, twin.res.Pages)
		}
	}
}

// M3-09: named pages. 499 and 500 siblings alternating two page names make as many pages and render;
// 501 and more end in a pages *LimitError (MaxPages is 500). Unknown names share the default page, huge page sizes and
// margins are clamped, and thousands of @page rules cost nothing per unused name.
func TestM3NamedPageAlternationAtMaxPages(t *testing.T) {
	doc := func(n int) string {
		var sb strings.Builder
		sb.WriteString(`<style>@page a{margin:10px} @page b{margin:20px} .a{page:a} .b{page:b} div{height:5px}</style>`)
		for i := range n {
			if i%2 == 0 {
				sb.WriteString(`<div class=a>x</div>`)
			} else {
				sb.WriteString(`<div class=b>x</div>`)
			}
		}
		return sb.String()
	}
	for _, n := range []int{499, 500} {
		under := m3Render(t, doc(n))
		if under.res.Pages != n {
			t.Errorf("%d alternating names: %d pages", n, under.res.Pages)
		}
		under.balanced(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), m3Guard)
	defer cancel()
	for _, n := range []int{501, 502, 900} {
		_, err := mustNew(t, m2Options(t)).Render(ctx, strings.NewReader(doc(n)), io.Discard)
		var le *LimitError
		if !errors.As(err, &le) || le.Limit != LimitPages {
			t.Errorf("%d alternating names: %v, want a pages LimitError", n, err)
		}
	}
}

func TestM3NamedPagesUnknownAndHuge(t *testing.T) {
	var rules strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&rules, "@page n%d{margin:%dpx}", i, i%40)
	}
	long := strings.Repeat("z", 300)
	for name, html := range map[string]string{
		"unknown names":       `<style>div{height:5px;break-after:page}</style>` + strings.Repeat(`<div style="page:nope">x</div>`, 20),
		"name over 256 bytes": `<style>div{height:5px;break-after:page}</style><div style="page:` + long + `">x</div><div>y</div>`,
		"huge size":           `<style>@page big{size:1e9px 1e9px;margin:1e9px}</style><div style="page:big">x</div><div>y</div>`,
		"size 0":              `<style>@page z{size:0 0}</style><div style="page:z">x</div>`,
		"negative margins":    `<style>@page m{margin:-1e9px}</style><div style="page:m">x</div>`,
		"5000 rules, 400 used": `<style>` + rules.String() + `div{height:5px}</style>` + func() string {
			var sb strings.Builder
			for i := range 400 {
				fmt.Fprintf(&sb, `<div style="page:n%d">x</div>`, i*12)
			}
			return sb.String()
		}(),
		"named page with boxes":  `<style>@page c{margin:0;@top-center{content:"x";border-bottom:1pt solid red}@bottom-left{content:counter(page)}}</style><div style="page:c">x</div><div>y</div>`,
		"@page selector garbage": `<style>@page :first :left foo bar{margin:1px} @page a b{margin:2px}</style><div>x</div>`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), m3Guard)
			defer cancel()
			var buf bytes.Buffer
			_, err := mustNew(t, m2Options(t)).Render(ctx, strings.NewReader(html), &buf)
			if err != nil {
				var le *LimitError
				if !errors.As(err, &le) {
					t.Fatalf("%v", err)
				}
				return
			}
			pdftest.Validate(t, buf.Bytes())
		})
	}
}

// M3-14: hostile border widths through Render never fail it other than by a *LimitError (a border
// of 5000px or more makes a page-filling box) and never panic or leave the PDF invalid.
func TestM3HostileBorderWidthsRender(t *testing.T) {
	for _, w := range []string{"1e30px", "1e999px", "-5px", "0.0000001px", "calc(1px + 1px)", "NaN", "99999999999em", "5000px"} {
		for _, extra := range []string{"", ";border-collapse:collapse", ";border-radius:50%"} {
			ctx, cancel := context.WithTimeout(context.Background(), m3Guard)
			var buf bytes.Buffer
			_, err := mustNew(t, m2Options(t)).Render(ctx, strings.NewReader(`<div style="height:50px;border:`+w+` solid red`+extra+`">x</div><table style="border:`+w+` solid blue`+extra+`"><tr><td style="border:`+w+` solid green">a</td></tr></table>`), &buf)
			cancel()
			if err != nil {
				var le *LimitError
				if !errors.As(err, &le) {
					t.Errorf("border %s%s: %v", w, extra, err)
				}
				continue
			}
			pdftest.Validate(t, buf.Bytes())
		}
	}
}

// M3-10: internal links and named destinations through Render.
func m3Links(r m3Run) (internal int) {
	for _, p := range r.doc.Pages {
		for _, l := range p.Links {
			if l.URI == "" {
				internal++
			}
		}
	}
	return
}

func m3Clamped(res *Result, detail string) bool {
	for _, w := range res.Warnings {
		if w.Code == WarnClamped && strings.Contains(w.Detail, detail) {
			return true
		}
	}
	return false
}

// Ids made of PDF delimiters, parentheses, unicode and NUL never reach the PDF as names: destinations are
// generated /D<n>, every link finds its target, and the document stays valid.
func TestM3HostileIDsInDestinations(t *testing.T) {
	ids := []string{"a(b)c", "x)y(z", `a\\b`, "/Name", "#hash", "a b", "é日本語", "a\x00b", "%PDF", "<<>>", "[x]", "{x}", "a%20b", strings.Repeat("i", 5000), "\u202eevil"}
	var sb strings.Builder
	for i, id := range ids {
		fmt.Fprintf(&sb, `<h2 id="%s">target %d</h2>`, strings.ReplaceAll(id, `"`, "&quot;"), i)
		fmt.Fprintf(&sb, `<a href="#%s">link %d</a> `, strings.ReplaceAll(id, `"`, "&quot;"), i)
	}
	r := m3Render(t, sb.String())
	internal := m3Links(r)
	if internal == 0 {
		t.Error("no internal link made")
	}
	for name := range r.doc.Dests {
		if !regexp.MustCompile(`^D[0-9]+$`).MatchString(name) {
			t.Errorf("destination named %q", name)
		}
	}
	for i, p := range r.doc.Pages {
		for _, l := range p.Links {
			if l.URI == "" {
				if _, ok := r.doc.Dests[l.Dest]; !ok {
					t.Errorf("page %d: link to the missing destination %q", i+1, l.Dest)
				}
			}
		}
	}
	if bytes.Contains(r.data, []byte("evil")) || bytes.Contains(r.data, []byte("日本語")) {
		t.Error("a hostile id text reached the PDF")
	}
}

// 10 000 targets are all linked, the 10 001st link is dropped with a WarnClamped; duplicates and links to
// display:none or missing targets make no annotation.
func TestM3DestinationLimit(t *testing.T) {
	doc := func(n int) string {
		var sb strings.Builder
		sb.WriteString(`<style>body{margin:0}p{margin:0;font-size:1px;line-height:1px}</style>`)
		for i := range n {
			fmt.Fprintf(&sb, `<p id="t%d"><a href="#t%d">.</a></p>`, i, i)
		}
		return sb.String()
	}
	at := m3Render(t, doc(10000))
	if internal := m3Links(at); internal != 10000 || len(at.doc.Dests) != 10000 || m3Clamped(at.res, "destination") {
		t.Errorf("10000 targets: %d internal links, %d destinations, warnings %+v", m3Links(at), len(at.doc.Dests), at.res.Warnings)
	}
	over := m3Render(t, doc(10001))
	internal := m3Links(over)
	if len(over.doc.Dests) != 10000 || internal != 10000 || !m3Clamped(over.res, "dest") {
		t.Errorf("10001 targets: %d internal links, %d destinations, warnings %+v", internal, len(over.doc.Dests), over.res.Warnings)
	}
	dup := m3Render(t, `<h2 id=x>one</h2><h2 id=x>two</h2><p><a href="#x">a</a> <a href="#x">b</a> <a href="#missing">c</a> <a href="#hidden">d</a></p><div style="display:none"><h2 id=hidden>h</h2></div>`)
	if internal := m3Links(dup); internal != 2 || len(dup.doc.Dests) != 1 {
		t.Errorf("duplicates/hidden/missing: %d internal links, %d destinations, want 2 and 1", internal, len(dup.doc.Dests))
	}
}

// 30 000 links to one id (the node limit allows no more) keep one destination and bounded time and heap.
func TestM3ManyLinksToOneID(t *testing.T) {
	r := m3Render(t, `<style>body{margin:0}a{display:block;height:1px}</style><h2 id=x>t</h2>`+strings.Repeat(`<a href="#x">.</a>`, 30000))
	internal := m3Links(r)
	t.Logf("%d internal links, %d pages, %v, heap %d MiB", internal, r.res.Pages, r.took, r.peak>>20)
	if len(r.doc.Dests) != 1 {
		t.Errorf("%d destinations", len(r.doc.Dests))
	}
}

// Clamps drop effects, never text: each probe nests or inflates something past its bound with MARKERTEXT
// inside, and the text must still be in the PDF, with the expected warning.
func TestClampsNeverHideText(t *testing.T) {
	const marker = "MARKERTEXT"
	nest := func(n int, open, closing, inner string) string {
		return strings.Repeat(open, n) + inner + strings.Repeat(closing, n)
	}
	big := strings.Repeat("a", 100<<10)
	tests := []struct {
		name, html, warn string
	}{
		{"300 nested overflow:hidden", nest(300, `<div style="overflow:hidden">`, "</div>", marker), "clip nesting"},
		{"300 nested rotate", nest(300, `<div style="position:relative;transform:rotate(1deg)">`, "</div>", marker), "transform nesting"},
		{"300 nested relative left/top", nest(300, `<div style="position:relative;left:1px;top:1px">`, "</div>", marker), ""},
		{"2 MiB generated content", `<style>p::before{content:"` + big + `"}</style>` + strings.Repeat("<p></p>", 21) + "<p>" + marker + "</p>", "content"},
		{"20 nested opacity .5", nest(20, `<div style="opacity:.5">`, "</div>", marker), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := m3Render(t, tc.html)
			if !strings.Contains(pagesText(r.doc), marker) {
				t.Errorf("%s: the text is not in the PDF", marker)
			}
			if tc.warn != "" && !m3Clamped(r.res, tc.warn) {
				t.Errorf("no %q warning: %+v", tc.warn, r.res.Warnings)
			}
			if tc.warn == "" {
				for _, w := range r.res.Warnings {
					if w.Code == WarnClamped {
						t.Errorf("unexpected clamping warning %+v", w)
					}
				}
			}
			r.balanced(t)
		})
	}
	t.Run("unusable matrix", func(t *testing.T) {
		t.Skip("not reachable from the public input: transform accepts only rotate(), whose angle is normalised to [0, 2π), so a matrix is always finite and bounded")
	})
}

// An absolute box inside a rotated absolute box is painted inside its parent's rotation, as in Chrome.
func TestAbsoluteChildFollowsItsRotatedParent(t *testing.T) {
	r := m3Render(t, `<div style="position:absolute;left:100px;top:100px;width:100px;height:50px;background:#00f;transform:rotate(90deg)"><div style="position:absolute;left:0;top:0;width:20px;height:20px;background:#f00"></div></div>`)
	c := string(pdfContentOf(r.doc))
	i, j := strings.Index(c, "1 0 0 rg"), strings.Index(c, " cm\n")
	if i < 0 || j < 0 || strings.Count(c[:i], "\nq\n")+strings.Count(c[:i], "q\n0") == 0 || strings.Count(c[:i], "\nQ\n") >= strings.Count(c[:i], "q\n") {
		t.Errorf("the child is not painted inside its parent's rotation:\n%s", c)
	}
}
