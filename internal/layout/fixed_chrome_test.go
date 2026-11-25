package layout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

type fixedBox struct {
	Order int        `json:"order"`
	Color string     `json:"color"`
	BBox  [4]float64 `json:"bbox_px"`
}

type fixedPage struct {
	Page  int        `json:"page"`
	Boxes []fixedBox `json:"boxes"`
}

type fixedCase struct {
	PageCSS string      `json:"page_css"`
	Scale   float64     `json:"scale"`
	Case    string      `json:"case"`
	Pages   int         `json:"pages"`
	PerPage []fixedPage `json:"per_page"`
}

func loadFixedCases(t testing.TB) []fixedCase {
	t.Helper()
	var out []fixedCase
	for _, name := range []string{"testdata/chrome_fixed.txt", "testdata/chrome_fixed_late.txt"} {
		out = append(out, loadFixedFile(t, name)...)
	}
	return out
}

func loadFixedFile(t testing.TB, name string) []fixedCase {
	t.Helper()
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []fixedCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), "{") {
			continue
		}
		var c fixedCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if c.Scale == 0 {
			c.Scale = 1
		}
		out = append(out, c)
	}
	return out
}

const fixedFlow = `<div style="height:2700px;background:#e0e0e0"></div>`

func fixedBoxHTML(col, style, inner string) string {
	return fmt.Sprintf(`<div style="position:fixed;background:%s;%s">%s</div>`, col, style, inner)
}

// fixedProbeHTML reproduces the documents of tmp/m4probe/fixed.py (dev-only).
func fixedProbeHTML(name string) (string, bool) {
	const R, G, B, Y = "#ff0000", "#00ff00", "#0000ff", "#ffff00"
	box := fixedBoxHTML
	switch name {
	case "top left px":
		return fixedFlow + box(R, "top:10px;left:20px;width:50px;height:30px", ""), true
	case "bottom right px":
		return fixedFlow + box(R, "bottom:10px;right:20px;width:50px;height:30px", ""), true
	case "top left bottom right (stretch)":
		return fixedFlow + box(R, "top:10px;left:20px;bottom:30px;right:40px", ""), true
	case "percent insets and size":
		return fixedFlow + box(R, "top:10%;left:20%;width:30%;height:5%", ""), true
	case "percent bottom right":
		return fixedFlow + box(R, "bottom:5%;right:10%;width:20%;height:10%", ""), true
	case "auto insets (static position)":
		return `<div style="height:100px">above</div>` + box(R, "width:50px;height:30px", "") + fixedFlow, true
	case "auto insets after flow of 1500px":
		return `<div style="height:1500px"></div>` + box(R, "width:50px;height:30px", "") + fixedFlow, true
	case "only top":
		return fixedFlow + box(R, "top:50px;width:60px;height:20px", ""), true
	case "only left":
		return fixedFlow + box(R, "left:50px;width:60px;height:20px", ""), true
	case "only right":
		return fixedFlow + box(R, "right:50px;width:60px;height:20px", ""), true
	case "only bottom":
		return fixedFlow + box(R, "bottom:50px;width:60px;height:20px", ""), true
	case "top px after flow of 1500px":
		return `<div style="height:1500px;background:#e0e0e0"></div>` + box(R, "top:10px;left:10px;width:80px;height:80px", "") + fixedFlow, true
	case "top inset right after break-before:page":
		return fixedLateFlow(300) + `<div style="break-before:page"></div>` + fixedLate(0, "top:10px;left:20px;width:50px;height:30px", 300), true
	case "top inset inside a break-before:page block":
		return fixedLateFlow(300) + `<div style="break-before:page">` + fixedBoxHTML("#ff0000", "top:10px;left:20px;width:50px;height:30px", "") + `p2</div>` + fixedLateFlow(300), true
	case "auto insets right after break-before:page":
		return fixedLateFlow(300) + `<div style="break-before:page"></div>` + fixedLate(0, "left:20px;width:50px;height:30px", 300), true
	case "auto insets inside a break-before:page block":
		return fixedLateFlow(300) + `<div style="break-before:page">` + fixedBoxHTML("#ff0000", "left:20px;width:50px;height:30px", "") + `p2</div>` + fixedLateFlow(300), true
	case "bottom inset after two forced breaks":
		return fixedLateFlow(100) + `<div style="break-before:page"></div>` + fixedLateFlow(100) + `<div style="break-before:page"></div>` + fixedLate(0, "bottom:10px;left:20px;width:50px;height:30px", 100), true
	case "top inset after 1500px":
		return fixedLate(1500, "top:10px;left:20px;width:50px;height:30px", 1200), true
	case "bottom inset after 1500px":
		return fixedLate(1500, "bottom:10px;left:20px;width:50px;height:30px", 1200), true
	case "top and bottom bars after 1500px":
		return fixedLate(1500, "top:0;left:0;right:0;height:40px", 0) + box(B, "bottom:0;left:0;right:0;height:40px", "") + fixedLateFlow(1200), true
	case "top inset after 1500px, flow ends right after":
		return fixedLate(1500, "top:10px;left:20px;width:50px;height:30px", 0), true
	case "bottom inset after 1500px, flow ends right after":
		return fixedLate(1500, "bottom:10px;left:20px;width:50px;height:30px", 0), true
	case "top inset at the start":
		return fixedLate(0, "top:10px;left:20px;width:50px;height:30px", 2700), true
	case "top inset after 100px":
		return fixedLate(100, "top:10px;left:20px;width:50px;height:30px", 2700), true
	case "top inset at the top of page 2 (1123px)":
		return fixedLate(1123, "top:10px;left:20px;width:50px;height:30px", 1200), true
	case "top inset after 2500px (page 3)":
		return fixedLate(2500, "top:10px;left:20px;width:50px;height:30px", 400), true
	case "auto insets after 1500px with top only width":
		return fixedLate(1500, "left:20px;width:50px;height:30px", 1200), true
	case "negative insets":
		return fixedFlow + box(R, "top:-10px;left:-10px;width:60px;height:60px", ""), true
	case "larger than the page":
		return fixedFlow + box(R, "top:0;left:0;width:900px;height:1300px", ""), true
	case "auto width shrink-to-fit text DejaVu Sans":
		return fixedFlow + box(R, "top:10px;left:10px;font:16px 'DejaVu Sans'", "some text in the box"), true
	case "z-index -1 below flow":
		return strings.Replace(fixedFlow, "#e0e0e0", "#ffffff", 1) + box(R, "top:10px;left:10px;width:80px;height:80px;z-index:-1", ""), true
	case "z-index order -1 auto 1":
		return fixedFlow + box(R, "top:10px;left:10px;width:80px;height:80px;z-index:1", "") + box(G, "top:30px;left:30px;width:80px;height:80px", "") + box(B, "top:50px;left:50px;width:80px;height:80px;z-index:-1", ""), true
	case "z-index order 2 1 auto 0":
		return fixedFlow + box(R, "top:10px;left:10px;width:80px;height:80px;z-index:2", "") + box(G, "top:30px;left:30px;width:80px;height:80px;z-index:1", "") + box(B, "top:50px;left:50px;width:80px;height:80px", "") + box(Y, "top:70px;left:70px;width:80px;height:80px;z-index:0", ""), true
	case "fixed and absolute overlap":
		return fixedFlow + `<div style="position:relative;height:0"><div style="position:absolute;top:20px;left:20px;width:90px;height:90px;background:` + G + `"></div></div>` + box(R, "top:10px;left:10px;width:80px;height:80px", ""), true
	case "inside an overflow:hidden ancestor":
		return fixedFlow + `<div style="position:absolute;top:200px;left:100px;width:100px;height:100px;overflow:hidden;background:` + G + `">` + box(R, "top:10px;left:10px;width:200px;height:200px", "") + `</div>`, true
	case "inside overflow:hidden relative ancestor":
		return fixedFlow + `<div style="position:relative;margin:300px 0 0 100px;width:100px;height:100px;overflow:hidden;background:` + G + `">` + box(R, "top:10px;left:10px;width:200px;height:200px", "") + `</div>`, true
	case "fixed inside fixed":
		return fixedFlow + box(G, "top:100px;left:100px;width:200px;height:200px", box(R, "top:10px;left:10px;width:60px;height:60px", "")), true
	case "fixed inside absolute":
		return fixedFlow + `<div style="position:absolute;top:300px;left:50px;width:200px;height:200px;background:` + G + `">` + box(R, "top:10px;left:10px;width:60px;height:60px", "") + `</div>`, true
	case "two fixed siblings top and bottom bars":
		return fixedFlow + box(R, "top:0;left:0;right:0;height:40px", "") + box(B, "bottom:0;left:0;right:0;height:40px", ""), true
	case "fixed with border and padding":
		return fixedFlow + box(R, "top:10px;left:10px;width:60px;height:30px;padding:5px;border:3px solid #000", ""), true
	case "fixed with display:none sibling flow":
		return `<div style="display:none">x</div>` + fixedFlow + box(R, "top:10px;left:10px;width:80px;height:80px", ""), true
	}
	return "", false
}

func fixedLateFlow(h int) string {
	if h == 0 {
		return ""
	}
	return fmt.Sprintf(`<div style="height:%dpx;background:#e0e0e0"></div>`, h)
}

// fixedLate reproduces the documents of tmp/m4probe/fixed_late.py: a fixed box
// after before px of flow and followed by after px.
func fixedLate(before int, style string, after int) string {
	return fixedLateFlow(before) + fixedBoxHTML("#ff0000", style, "") + fixedLateFlow(after)
}

func fixedColor(c string) (r, g, b float64) {
	var ri, gi, bi int
	if _, err := fmt.Sscanf(c, "#%02x%02x%02x", &ri, &gi, &bi); err != nil {
		return -1, 0, 0
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255
}

// fixedRects returns the filled rects of the page with the color of the probe boxes,
// transformed by the translations in effect, in drawing order.
func fixedRects(items display.List, c string) (rects []display.Rect) {
	r, g, b := fixedColor(c)
	for _, it := range items {
		if f, ok := it.(display.FillRect); ok && math.Abs(f.Color.R-r) < 1e-6 && math.Abs(f.Color.G-g) < 1e-6 && math.Abs(f.Color.B-b) < 1e-6 && f.Color.A > 0.99 {
			rects = append(rects, f.Rect)
		}
	}
	return rects
}

// The probe documents of testdata/chrome_fixed.txt: a fixed box is on every page
// (from its static position's page when it has no vertical inset), at the same
// place of the page area, within 0.5 px of Chrome's, under the in-flow content
// for z-index -1.
func TestFixedMatchesChrome(t *testing.T) {
	cases, compared := loadFixedCases(t), 0
	if len(cases) < 100 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		html, ok := fixedProbeHTML(c.Case)
		if !ok {
			if !fixedUnmapped[c.Case] {
				t.Errorf("probe %q has no document", c.Case)
			}
			continue
		}
		t.Run(fmt.Sprintf("%s/%g/%s", c.PageCSS, c.Scale, c.Case), func(t *testing.T) {
			// The paper of the probe (the page counts and the offsets of its flow give
			// 793.9 x 1122.85 px; the fixed boxes see its whole px).
			margin := "0"
			if c.PageCSS == "margin20mm" {
				margin = "20mm"
			}
			page := "@page{size:A4;margin:" + margin + "} body{margin:0}"
			pages := newRun(t, html, opts{extra: page, scale: c.Scale}).all(t)
			for _, pp := range c.PerPage {
				if pp.Page <= len(pages) {
					checkFixedOrder(t, c.Case, pp, pages[pp.Page-1].Items)
				}
				if pp.Page > len(pages) {
					t.Errorf("page %d: only %d pages", pp.Page, len(pages))
					continue
				}
				for _, want := range pp.Boxes {
					got := fixedRects(pages[pp.Page-1].Items, want.Color)
					pg := pages[pp.Page-1]
					for i, r := range got {
						got[i] = display.Rect{X: pg.Area.X + pg.Scale*r.X, Y: pg.Area.Y + pg.Scale*r.Y, W: pg.Scale * r.W, H: pg.Scale * r.H}
					}
					tol := fixedTolerance(c.PageCSS, fixedFollowsFlow(html, want.Color), pp.Page)
					found := false
					for _, r := range got {
						found = found || math.Abs(r.X-want.BBox[0]) <= tol[0] && math.Abs(r.Y-want.BBox[1]) <= tol[1] &&
							(math.Abs(r.W-want.BBox[2]) <= tol[2] || fixedWidthDiff(c.Case)) && math.Abs(r.H-want.BBox[3]) <= tol[3]
					}
					compared++
					if !found && want.Color != "#ffffff" && !fixedKnownDiff(c.Case, want) {
						t.Errorf("page %d: no %s rect at %v; rects %v", pp.Page, want.Color, want.BBox, got)
					}
				}
			}
		})
	}
	if compared < 509 {
		t.Errorf("%d boxes compared", compared)
	}
}

// fixedUnmapped are the probes that compare text, rotations, opacity and
// transformed ancestors, which the position of a rectangle does not describe.
var fixedUnmapped = map[string]bool{
	"fixed text overflows page bottom":                             true,
	"inside a transformed ancestor (becomes its containing block)": true,
	"opacity .5 fixed":              true,
	"rotated 45deg":                 true,
	"rotated -4deg":                 true,
	"auto width shrink-to-fit text": true, // the default font of the probe is not ours: the DejaVu Sans row covers it
}

// fixedFollowsFlow reports whether the box of color in the probe document html is
// placed by the flow and not by insets: a fixed box without top and bottom, or a
// box that is not fixed.
func fixedFollowsFlow(html, color string) bool {
	i := strings.Index(html, "background:"+color)
	if i < 0 {
		return false
	}
	start := strings.LastIndex(html[:i], `style="`)
	end := i + strings.Index(html[i:], `"`)
	attr := html[start:end]
	if !strings.Contains(attr, "position:fixed") {
		return true
	}
	return !strings.Contains(attr, "top:") && !strings.Contains(attr, "bottom:")
}

// fixedTolerance is the distance in px to Chrome's box on x, y, width and height:
// 0.5 on each. On y a box that follows the flow gets 0.5 more per page after the
// first, up to 1 more, because Chrome snaps the flow height of a page to whole
// px; a 20 mm margin (75.59 px) is snapped as well: 0.6 more on x and y.
func fixedTolerance(pageCSS string, follows bool, page int) [4]float64 {
	tol := [4]float64{0.5, 0.5, 0.5, 0.5}
	if follows {
		tol[1] += math.Min(0.5*float64(page-1), 1)
	}
	if pageCSS == "margin20mm" {
		tol[0] += 0.6
		tol[1] += 0.6
	}
	return tol
}

// fixedWidthDiff: Chrome's 30% width is 0.9 px short of 30% of its viewport (the
// print path measures it differently); its position and height match.
func fixedWidthDiff(name string) bool { return strings.HasPrefix(name, "percent ") }

// fixedKnownDiff lists the probe boxes this engine does not reproduce, with the
// reason (docs/css-support.md): Chrome fragments a fixed box that overflows the
// page, so it shows the overflowing part again on the next page, and the absolute
// ancestor of a fixed box is on the page of its static position here (M3-04),
// on the first page in Chrome.
func fixedKnownDiff(name string, b fixedBox) bool {
	switch name {
	case "negative insets", "larger than the page":
		return b.BBox[1] > 1000 || b.BBox[1] < -500
	case "inside an overflow:hidden ancestor", "fixed inside absolute":
		return b.Color == "#00ff00"
	}
	return false
}

// checkFixedOrder compares the painting order of the colors of a page with
// Chrome's: the boxes of the probe have different colors, listed by the order of
// their paths.
func checkFixedOrder(t *testing.T, name string, pp fixedPage, items display.List) {
	t.Helper()
	var want []string
	for _, b := range pp.Boxes {
		if b.Color != "#ffffff" && b.Color != "#e0e0e0" {
			want = append(want, b.Color)
		}
	}
	if len(want) < 2 {
		return
	}
	idx := func(color string) int {
		r, g, b := fixedColor(color)
		for i, it := range items {
			if f, ok := it.(display.FillRect); ok && math.Abs(f.Color.R-r) < 1e-6 && math.Abs(f.Color.G-g) < 1e-6 && math.Abs(f.Color.B-b) < 1e-6 {
				return i
			}
		}
		return -1
	}
	prev := -1
	for _, c := range want {
		i := idx(c)
		if i < 0 {
			return // the rect is checked by its position
		}
		if i < prev {
			t.Errorf("%s page %d: %s is painted before the box that Chrome paints first", name, pp.Page, c)
		}
		prev = i
	}
}
