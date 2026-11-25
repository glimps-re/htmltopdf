package htmltopdf

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// h4MaxDashes returns the largest number of dashes of a stroked line of content: the length of its m/l path
// divided by the period (on + off) of the dash pattern in force. A solid fill or a stroke without pattern
// counts for none.
func h4MaxDashes(content string) float64 {
	var period, length, px, py float64
	most := 0.0
	for _, l := range strings.Split(content, "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) >= 4 && f[len(f)-1] == "d":
			period = 0
			for _, v := range f[:len(f)-2] {
				if n, err := strconv.ParseFloat(strings.Trim(v, "[]"), 64); err == nil {
					period += n
				}
			}
		case len(f) == 3 && (f[2] == "m" || f[2] == "l"):
			x, errX := strconv.ParseFloat(f[0], 64)
			y, errY := strconv.ParseFloat(f[1], 64)
			if errX == nil && errY == nil {
				if f[2] == "l" {
					length += math.Hypot(x-px, y-py)
				}
				px, py = x, y
			}
		case len(f) == 1 && (f[0] == "S" || f[0] == "s"):
			if period > 0 {
				most = max(most, length/period)
			}
			length = 0
		case len(f) == 1 && (f[0] == "f" || f[0] == "n" || f[0] == "f*"):
			length = 0
		}
	}
	return most
}

// 10 000 absolute boxes (26 classes) with a dashed or dotted border whose sides run from 1e5 to 1e30 px: the dash
// strokes are clipped to the page, so the bytes and the work stay within a small factor of the same boxes
// with a solid border, the heap holds, the text is in the PDF once and the PDF is valid.
func TestM4DashedBordersOfHugeSides(t *testing.T) {
	const n = 10000
	build := func(style string) (string, []string) {
		var sb strings.Builder
		sb.WriteString("<style>")
		for e := 5; e <= 30; e++ {
			fmt.Fprintf(&sb, ".s%d{position:absolute;left:0;top:0;width:1e%dpx;height:1e%dpx;border:%s red}", e, e, e, style)
		}
		sb.WriteString("</style>")
		for i := range n {
			fmt.Fprintf(&sb, `<div class=s%d></div>`, 5+i%26)
		}
		return sb.String() + "<p>DASHWORD</p>", []string{"DASHWORD"}
	}
	for _, kind := range []string{"2px dashed", "3px dotted"} {
		t.Run(kind, func(t *testing.T) {
			html, words := build(kind)
			solid, _ := build(strings.Fields(kind)[0] + " solid")
			r := h4RenderTwin(t, referenceOptions(t), html)
			twin := h4Render(t, referenceOptions(t), solid, false)
			if r.err != nil || twin.err != nil {
				t.Fatal(r.err, twin.err)
			}
			h4TextOnce(t, r, words)
			if d := h4MaxDashes(string(r.doc.Pages[0].Content)); d > 4096 {
				t.Errorf("%.0f dashes on a line", d)
			}
			t.Logf("%s: work %d bytes %d boxes %d glyphs %d; solid twin work %d bytes %d boxes %d", kind, r.work, r.res.Bytes, r.boxes, r.glyphs, twin.work, twin.res.Bytes, twin.boxes)
			if r.boxes != twin.boxes || r.glyphs != twin.glyphs || r.work > twin.work+twin.work/8 {
				t.Errorf("work %d boxes %d glyphs %d; twin work %d boxes %d glyphs %d", r.work, r.boxes, r.glyphs, twin.work, twin.boxes, twin.glyphs)
			}
			if r.res.Bytes > 4*twin.res.Bytes+1<<20 {
				t.Errorf("%d bytes against %d for the solid twin", r.res.Bytes, twin.res.Bytes)
			}
		})
	}
}

// One box that lies on the page with a side of 1e7 px, with a dashed or dotted border, alone and under a
// rotation: only the visible dashes are written, so the PDF stays within 64 KiB of the solid twin's.
func TestM4DashedHugeSideOnPage(t *testing.T) {
	for _, tc := range []struct {
		name, style string
		wantPattern bool // the unrotated line keeps its dash pattern; a rotated one may fall back to solid
	}{
		{"dashed", "border:2px dashed red", true},
		{"dotted", "border:3px dotted red", true},
		{"dashed rotated", "border:2px dashed red;transform:rotate(7deg)", false},
		{"dotted rotated", "border:3px dotted red;transform:rotate(-30deg)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := func(style string) string {
				return `<div style="position:absolute;left:10px;top:10px;width:1e7px;height:120px;` + style + `"></div><p>DASHWORD</p>`
			}
			r := h4Render(t, referenceOptions(t), doc(tc.style), true)
			twin := h4Render(t, referenceOptions(t), doc(strings.Replace(strings.Replace(tc.style, "dashed", "solid", 1), "dotted", "solid", 1)), false)
			if r.err != nil || twin.err != nil {
				t.Fatal(r.err, twin.err)
			}
			h4TextOnce(t, r, []string{"DASHWORD"})
			content := string(r.doc.Pages[0].Content)
			if tc.wantPattern && !strings.Contains(content, "] 0 d") {
				t.Error("no dash pattern in the page content")
			}
			if d := h4MaxDashes(content); d > 4096 {
				t.Errorf("%.0f dashes on a line", d)
			}
			if r.res.Bytes > twin.res.Bytes+64<<10 {
				t.Errorf("%d bytes against %d for the solid twin", r.res.Bytes, twin.res.Bytes)
			}
			if r.work > twin.work+twin.work/8 {
				t.Errorf("work %d against %d", r.work, twin.work)
			}
		})
	}
}

// A rotated box with a side of 1e7 px and a dashed top border, alone and a thousand times on the page: no
// stroked line carries more than 4096 dashes (a longer one falls back to solid), and the bytes and the
// work stay within a small factor of the same boxes with a solid border.
func TestM4DashedRotatedHugeSides(t *testing.T) {
	box := func(border, transform string) string {
		return `<div style="position:absolute;left:10px;top:10px;width:1e7px;height:50px;border-top:` + border + `;transform:` + transform + `"></div>`
	}
	for _, transform := range []string{"rotate(10deg)", "rotate(-35deg)", "rotate(89deg)"} {
		for _, n := range []int{1, 1000} {
			t.Run(fmt.Sprintf("%s x%d", transform, n), func(t *testing.T) {
				doc := func(border string) string {
					return strings.Repeat(box(border, transform), n) + "<p>DASHWORD</p>"
				}
				r := h4Render(t, referenceOptions(t), doc("2px dashed red"), true)
				twin := h4Render(t, referenceOptions(t), doc("2px solid red"), false)
				if r.err != nil || twin.err != nil {
					t.Fatal(r.err, twin.err)
				}
				h4TextOnce(t, r, []string{"DASHWORD"})
				if d := h4MaxDashes(string(r.doc.Pages[0].Content)); d > 4096 {
					t.Errorf("%.0f dashes on a line", d)
				}
				if r.res.Bytes > 2*twin.res.Bytes+64<<10 {
					t.Errorf("%d bytes against %d for the solid twin", r.res.Bytes, twin.res.Bytes)
				}
				if r.work > twin.work+twin.work/8 {
					t.Errorf("work %d against %d for the solid twin", r.work, twin.work)
				}
			})
		}
	}
}
