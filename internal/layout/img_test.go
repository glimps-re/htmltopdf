package layout

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/raster"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// imgSource returns a validated 100×50 PNG source, the image of the Chrome probe.
func imgSource(tb testing.TB) *raster.Source {
	tb.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, 100, 50))
	for i := range m.Pix {
		m.Pix[i] = 0x80
	}
	m.SetNRGBA(0, 0, color.NRGBA{R: 1, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		tb.Fatal(err)
	}
	src, err := raster.NewSet(testBudget(nil)).Add("data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
	if err != nil {
		tb.Fatal(err)
	}
	return src
}

var imgTag = regexp.MustCompile(`<img([^>]*)>`)

// imgDoc turns every <img> of html into an <svg> with the same attributes: the
// builder makes a replaced box of it with the style of the element, which
// asImages turns into an image box, so the tests do not depend on the builder.
func imgDoc(html string) string {
	return imgTag.ReplaceAllString(html, "<svg$1></svg>")
}

// asImages returns a box edit that makes every svg box an image box of src,
// keeping its width and height attributes.
func asImages(src *raster.Source) func(*box.Box) {
	var conv func(b *box.Box)
	conv = func(b *box.Box) {
		if b.Kind == box.KindSVG {
			b.Kind = box.KindImage
			b.Img = &box.Img{Src: src, Width: b.SVG.Width, Height: b.SVG.Height}
			b.SVG = nil
		}
		for _, c := range b.Children {
			conv(c)
		}
		for _, c := range b.OutOfFlow {
			conv(c)
		}
	}
	return conv
}

type imgCase struct {
	Kind    string               `json:"kind"`
	Name    string               `json:"name"`
	W       float64              `json:"w"`
	CSS     string               `json:"css"`
	HTML    string               `json:"html"`
	M       map[string][]float64 `json:"m"`
	PageCSS string               `json:"page_css"`
	Pages   int                  `json:"pages"`
	PerPage []struct {
		Page   int         `json:"page"`
		Images [][]float64 `json:"images_bbox_px"`
	} `json:"per_page"`
}

func loadImgCases(t *testing.T) []imgCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_img.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []imgCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		var c imgCase
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// imgDeviation is a documented difference from Chrome: the image size we
// produce and why.
type imgDeviation struct {
	w, h float64
	why  string
}

// imgDeviations are the image boxes of the probe whose size differs from
// Chrome, keyed by case number and id. The test pins our size, so a change in
// either direction is seen.
var imgDeviations = map[string]imgDeviation{
	// Chrome 300x150.
	"03/i3": {100, 50, "the percentage height of an atomic inline does not resolve: inline layout has no containing block height"},
	// Chrome 160x80: it keeps the ratio under min-height.
	"07/i": {100, 50, "min-height is not supported by the style package"},
	// Chrome 40x20: it keeps the ratio under max-height.
	"07/i3": {100, 50, "max-height is not supported by the style package"},
	// Chrome 300x20.
	"07/i4": {300, 150, "max-height is not supported by the style package"},
}

// asTwins returns a box edit that makes every svg box an empty box of the
// same display with the content size given by id, for comparing the position
// of an image with that of any atomic box of its size.
func asTwins(size map[string][2]float64) func(*box.Box) {
	var conv func(b *box.Box)
	conv = func(b *box.Box) {
		if b.Kind == box.KindSVG {
			st := *b.Style
			sz := size[b.ID]
			st.Width = style.Dim{Kind: style.DimPx, Value: sz[0]}
			st.Height = style.Dim{Kind: style.DimPx, Value: sz[1]}
			st.BoxSizing = style.ContentBox
			b.Style, b.SVG = &st, nil
			b.Kind = box.KindInlineBlock
			if blockLevel(&box.Box{Kind: box.KindSVG, Style: &st}) {
				b.Kind = box.KindBlock
			}
		}
		for _, c := range b.Children {
			conv(c)
		}
	}
	return conv
}

// The image sizes of the Chrome probe are matched within 0.5 px on the 11
// layout cases, but for the documented deviations, whose size is pinned. A
// position or container size that differs from Chrome must be that of an
// empty atomic box of the same size: the difference is then in the line
// metrics or vertical-align of the engine (the svg and inline-block probes
// list them), not in images.
func TestImgSizesMatchChrome(t *testing.T) {
	src := imgSource(t)
	n := 0
	for _, c := range loadImgCases(t) {
		if c.Kind != "dom" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
			doc := `<div style="overflow:hidden">` + imgDoc(c.HTML) + `</div>`
			pages := newRun(t, doc, opts{extra: page, css: c.CSS, boxes: asImages(src)}).all(t)
			size := map[string][2]float64{}
			walkPages(pages, func(f *frag) {
				if f.box.Kind == box.KindImage {
					cb := contentBox(f)
					size[f.box.ID] = [2]float64{cb.W, cb.H}
				}
			})
			twins := newRun(t, doc, opts{extra: page, css: c.CSS, boxes: asTwins(size)}).all(t)
			for key, want := range c.M {
				f := findFrag(pages, key)
				if f == nil {
					if strings.HasPrefix(key, "i") {
						t.Errorf("%s: no box", key)
					}
					// An inline span has no fragment of its own.
					continue
				}
				got := [4]float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H}
				near := func(i int) bool { return math.Abs(got[i]-want[i]) <= 0.5 }
				if dev, ok := imgDeviations[c.Name[:2]+"/"+key]; ok {
					if math.Abs(got[2]-dev.w) > 0.01 || math.Abs(got[3]-dev.h) > 0.01 {
						t.Errorf("%s: %.2fx%.2f, documented deviation %vx%v (%s), Chrome %.2f", key, got[2], got[3], dev.w, dev.h, dev.why, want)
					}
					continue
				}
				if strings.HasPrefix(key, "i") && (!near(2) || !near(3)) {
					t.Errorf("%s: [x y w h] = %.2f, Chrome %.2f", key, got, want)
					continue
				}
				if near(0) && near(1) && near(2) && near(3) {
					continue
				}
				tw := findFrag(twins, key)
				if tw == nil || tw.rect != f.rect {
					t.Errorf("%s: [x y w h] = %.2f, Chrome %.2f, and an atomic box of the same size differs", key, got, want)
					continue
				}
				t.Logf("engine line metrics %s: [x y w h] = %.2f, Chrome %.2f (same as an atomic box)", key, got, want)
			}
		})
	}
	if n != 11 {
		t.Fatalf("%d layout cases, want 11", n)
	}
}

// imgItems returns the image items of each page.
func imgItems(pages []*Page) [][]display.Image {
	out := make([][]display.Image, len(pages))
	for i, pg := range pages {
		for _, it := range pg.Items {
			if v, ok := it.(display.Image); ok {
				out[i] = append(out[i], v)
			}
		}
	}
	return out
}

// Pagination: an image that does not fit moves whole to the next page, like an
// svg. Chrome slices an image taller than a page (12c); here it moves whole and
// the page clips it.
func TestImgPaginationMatchesChrome(t *testing.T) {
	src := imgSource(t)
	n := 0
	for _, c := range loadImgCases(t) {
		if c.Kind != "pdf" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			pages := newRun(t, imgDoc(c.HTML), opts{extra: c.PageCSS, boxes: asImages(src)}).all(t)
			got := imgItems(pages)
			if strings.HasPrefix(c.Name, "12c") {
				if len(pages) != 2 || len(got[0]) != 0 || len(got[1]) != 1 {
					t.Fatalf("%d pages, images per page %v; want 2 pages, the image on page 2", len(pages), got)
				}
				r := got[1][0].Rect
				if r.Y != 0 || math.Abs(r.H-700) > 0.5 || math.Abs(r.W-100) > 0.5 {
					t.Errorf("image %+v, want at the top of page 2, 100x700", r)
				}
				return
			}
			if len(pages) != c.Pages {
				t.Fatalf("%d pages, Chrome %d", len(pages), c.Pages)
			}
			for i, pp := range c.PerPage {
				if len(got[i]) != len(pp.Images) {
					t.Fatalf("page %d: %d images, Chrome %d", pp.Page, len(got[i]), len(pp.Images))
				}
				for j, want := range pp.Images {
					r := got[i][j].Rect
					// The PDF probe reads drawn rectangles with ≈ 0.1 px of rounding.
					for k, v := range []float64{r.X, r.Y, r.W, r.H} {
						if math.Abs(v-want[k]) > 0.5 {
							t.Errorf("page %d image %d: %+v, Chrome %v", pp.Page, j, r, want)
							break
						}
					}
				}
			}
		})
	}
	if n != 4 {
		t.Fatalf("%d pagination cases, want 4", n)
	}
}

// One image box draws one display.Image on its content box; a border radius
// clips it to the inner radius; opacity is carried; opacity 0 draws nothing.
func TestImgEmit(t *testing.T) {
	src := imgSource(t)
	const page = "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
	tests := []struct {
		name, html string
		items      []string
		rect       display.Rect
		opacity    float64
	}{
		{"plain", `<img>`, []string{"img"}, display.Rect{W: 100, H: 50}, 1},
		{"padding and border", `<img style="display:block;padding:5px;border:3px solid red">`, []string{"img"}, display.Rect{X: 8, Y: 8, W: 100, H: 50}, 1},
		{"radius", `<img style="display:block;border-radius:10px;border:2px solid red">`, []string{"clip", "img", "pop"}, display.Rect{X: 2, Y: 2, W: 100, H: 50}, 1},
		{"opacity", `<img style="display:block;opacity:0.5">`, []string{"img"}, display.Rect{W: 100, H: 50}, 0.5},
		{"opacity 0", `<img style="display:block;opacity:0">`, nil, display.Rect{}, 0},
		{"zero width", `<img style="display:block;width:0">`, nil, display.Rect{}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pages := newRun(t, imgDoc(tc.html), opts{extra: page, boxes: asImages(src)}).all(t)
			var kinds []string
			var img display.Image
			for _, it := range pages[0].Items {
				switch v := it.(type) {
				case display.Image:
					kinds, img = append(kinds, "img"), v
				case display.PushClip:
					kinds = append(kinds, "clip")
					if !v.Radii.IsZero() && (v.Radii[0].H != 8 || v.Rect.W != 100) {
						t.Errorf("clip %+v, want the padding box with inner radius 8", v)
					}
				case display.PopClip:
					kinds = append(kinds, "pop")
				}
			}
			if strings.Join(kinds, ",") != strings.Join(tc.items, ",") {
				t.Fatalf("items %v, want %v", kinds, tc.items)
			}
			if tc.items == nil {
				return
			}
			if img.Src != src || img.Rect != tc.rect || img.Opacity != tc.opacity {
				t.Errorf("image %+v, want rect %+v opacity %v", img, tc.rect, tc.opacity)
			}
		})
	}
}

// Intrinsic contributions: an inline-block shrinks to the image; a percentage
// width attribute contributes {0, ∞} and resolves against the container; a box
// without a valid image has no size and draws nothing.
func TestImgIntrinsic(t *testing.T) {
	src := imgSource(t)
	const page = "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
	tests := []struct {
		name, html string
		src        *raster.Source
		w, h       float64
		drawn      bool
	}{
		{"inline-block shrinks to the image", `<div id=c style="display:inline-block;border:1px solid"><img id=i></div>`, src, 100, 50, true},
		{"float shrinks to the CSS height through the ratio", `<div id=c style="float:left"><img id=i style="display:block;height:20px"></div>`, src, 40, 20, true},
		{"percent attribute in a 300px block", `<div id=c style="width:300px"><img id=i width="50%"></div>`, src, 150, 75, true},
		{"border-box CSS height", `<div id=c style="float:left"><img id=i style="display:block;box-sizing:border-box;height:30px;padding:5px"></div>`, src, 40, 30, true},
		{"CSS percentage of an auto height overrides the attribute", `<div id=c style="float:left"><img id=i height="10" style="display:block;height:50%"></div>`, src, 100, 50, true},
		{"percent attribute contributes {0, ∞}: a float fills the page", `<div id=c style="float:left"><img id=i width="50%" style="display:block"></div>`, src, 400, 200, true},
		{"no source", `<div id=c style="float:left"><img id=i style="display:block"></div>`, nil, 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pages := newRun(t, imgDoc(tc.html), opts{extra: page, boxes: asImages(tc.src)}).all(t)
			f := findFrag(pages, "i")
			if f == nil {
				t.Fatal("no image box")
			}
			if math.Abs(f.rect.W-tc.w-f.padding[1]-f.padding[3]) > 0.01 || math.Abs(f.rect.H-tc.h) > 0.01 {
				t.Errorf("image %.2fx%.2f, want content %vx%v", f.rect.W, f.rect.H, tc.w, tc.h)
			}
			if got := len(imgItems(pages)[0]) == 1; got != tc.drawn {
				t.Errorf("drawn %v, want %v", got, tc.drawn)
			}
		})
	}
}

// An image is never split by a page break, in flow or in an inline line, and
// counts as a text-free atomic box for the kept-text invariant: a long
// document of images and text paginates without losing text.
func TestImgPagesKeepText(t *testing.T) {
	src := imgSource(t)
	var sb strings.Builder
	for i := range 60 {
		fmt.Fprintf(&sb, "<p>line %d <img> after</p><img style=\"display:block;height:%dpx\">", i, 40+i%7*10)
	}
	pages := newRun(t, imgDoc(sb.String()), opts{extra: "@page { size: 400px 300px; margin: 0 }", boxes: asImages(src)}).all(t)
	n := 0
	for i, items := range imgItems(pages) {
		for _, im := range items {
			n++
			if im.Rect.Y < -0.01 || im.Rect.Bottom() > 300.01 {
				t.Errorf("page %d: image %+v crosses the page", i+1, im.Rect)
			}
		}
	}
	if n != 120 {
		t.Errorf("%d images drawn, want 120", n)
	}
}

// A flex item stretched to the definite height of a single-line container
// takes its width from that height through the ratio, then min-width and
// max-width clamp it; an item that does not stretch keeps its own size.
func TestImgFlexStretch(t *testing.T) {
	src := imgSource(t)
	const page = "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
	tests := []struct {
		name, container, img string
		w, h                 float64
	}{
		{"stretched", "height:100px", `<img id=i>`, 200, 100},
		{"max-width binds", "height:100px", `<img id=i style="max-width:150px">`, 150, 100},
		{"min-width binds", "height:100px", `<img id=i style="min-width:250px">`, 250, 100},
		{"vertical margin, padding and border", "height:100px", `<img id=i style="margin:5px 0;padding:5px 0;border:5px solid">`, 150, 90},
		{"height attribute", "height:100px", `<img id=i height="20">`, 40, 20},
		{"width attribute", "height:100px", `<img id=i width="60">`, 60, 100},
		{"auto margin", "height:100px", `<img id=i style="margin-top:auto">`, 100, 50},
		{"align-items center", "height:100px;align-items:center", `<img id=i>`, 100, 50},
		{"wrap", "height:100px;flex-wrap:wrap", `<img id=i>`, 100, 100},
		{"indefinite height", "", `<img id=i><div style="height:80px;width:10px"></div>`, 100, 80},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pages := newRun(t, imgDoc(`<div style="display:flex;`+tc.container+`">`+tc.img+`</div>`), opts{extra: page, boxes: asImages(src)}).all(t)
			f := findFrag(pages, "i")
			if f == nil {
				t.Fatal("no image box")
			}
			if math.Abs(f.rect.W-tc.w) > 0.01 || math.Abs(f.rect.H-tc.h) > 0.01 {
				t.Errorf("image %.2fx%.2f, want %vx%v", f.rect.W, f.rect.H, tc.w, tc.h)
			}
		})
	}
}
