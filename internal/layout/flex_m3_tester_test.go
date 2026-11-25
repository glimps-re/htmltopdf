package layout

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// Random single-line flex rows of fixed-size items with auto and fixed
// margins: with positive free space and an auto margin, the auto margins
// share it equally and the line ends exactly at the container's right edge;
// without free space they are 0 and the items overflow in order.
func TestFlexAutoMarginsDistributeFreeSpace(t *testing.T) {
	rng := uint64(0x9E3779B97F4A7C15)
	next := func(k int) int {
		rng ^= rng << 13
		rng ^= rng >> 7
		rng ^= rng << 17
		return int(rng%1000003) % k
	}
	for c := range 250 {
		const cw = 400.0
		n := 1 + next(6)
		type item struct {
			w        float64
			ml, mr   float64
			mlA, mrA bool
		}
		items := make([]item, n)
		var sb strings.Builder
		fmt.Fprintf(&sb, `<div style="display:flex;width:%gpx">`, cw)
		fixed, autos := 0.0, 0
		for i := range items {
			it := &items[i]
			it.w = float64(10 + next(120))
			style := fmt.Sprintf("width:%gpx;flex:none;", it.w)
			switch next(4) {
			case 0:
				it.mlA = true
				style += "margin-left:auto;"
			case 1:
				it.ml = float64(next(15))
				style += fmt.Sprintf("margin-left:%gpx;", it.ml)
			}
			switch next(4) {
			case 0:
				it.mrA = true
				style += "margin-right:auto;"
			case 1:
				it.mr = float64(next(15))
				style += fmt.Sprintf("margin-right:%gpx;", it.mr)
			}
			if it.mlA {
				autos++
			}
			if it.mrA {
				autos++
			}
			fixed += it.w + it.ml + it.mr
			fmt.Fprintf(&sb, `<div id=i%d style="%s">x</div>`, i, style)
		}
		sb.WriteString("</div>")
		_, pages, err := sizingRun(t, sb.String())
		if err != nil {
			t.Fatal(err)
		}
		free := cw - fixed
		share := 0.0
		if autos > 0 && free > 0 {
			share = free / float64(autos)
		}
		x := 0.0
		for i, it := range items {
			if it.mlA {
				x += share
			} else {
				x += it.ml
			}
			f := findFrag(pages, fmt.Sprintf("i%d", i))
			if f == nil {
				t.Fatalf("case %d: no item %d", c, i)
			}
			if math.Abs(f.rect.X-x) > 0.02 || math.Abs(f.rect.W-it.w) > 0.02 {
				t.Fatalf("case %d item %d: x %.2f w %.2f, want x %.2f w %.2f (free %.1f, %d autos)\n%s", c, i, f.rect.X, f.rect.W, x, it.w, free, autos, sb.String())
			}
			x += it.w
			if it.mrA {
				x += share
			} else {
				x += it.mr
			}
		}
		if autos > 0 && free > 0 && math.Abs(x-cw) > 0.05 {
			t.Fatalf("case %d: line ends at %.2f, container %.0f", c, x, cw)
		}
	}
}

// Hostile flex rows: huge min-width, absurd margins, baseline chains, definite
// heights. Each either renders with finite rects or ends in a *LimitError.
func TestFlexM3Hostile(t *testing.T) {
	nest := func(depth int, open, closeS, leaf string) string {
		return strings.Repeat(open, depth) + leaf + strings.Repeat(closeS, depth)
	}
	for name, html := range map[string]string{
		"min-width 1e9":              `<div style="display:flex;width:300px"><div style="min-width:1e9px;flex:1">a</div><div style="flex:1">b</div></div>`,
		"min-width 1e30 and wrap":    `<div style="display:flex;flex-wrap:wrap;width:300px"><div style="min-width:1e30px">a</div><div>b</div></div>`,
		"min-width 100000%":          `<div style="display:flex;width:300px"><div style="min-width:100000%;flex:1">a</div><div>b</div></div>`,
		"margin auto and 1e30":       `<div style="display:flex;width:300px"><div style="margin:0 1e30px 0 auto">a</div></div>`,
		"margin -1e30 and auto":      `<div style="display:flex;width:300px"><div style="margin-left:-1e30px;margin-right:auto">a</div></div>`,
		"both auto, overflow":        `<div style="display:flex;width:50px"><div style="width:200px;flex:none;margin:auto">a</div></div>`,
		"auto + grow":                `<div style="display:flex;width:300px"><div style="flex:1;margin-left:auto">a</div></div>`,
		"auto + justify + wrap gap":  `<div style="display:flex;flex-wrap:wrap;gap:1e9px;width:300px;justify-content:space-around"><div style="margin-left:auto">a</div><div style="margin-right:auto">b</div></div>`,
		"baseline empty items":       `<div style="display:flex;align-items:baseline"><div></div><div style="height:1e9px"></div><div>x</div></div>`,
		"baseline huge margins":      `<div style="display:flex;align-items:baseline"><div style="margin-top:1e30px">a</div><div style="margin-top:-1e30px">b</div></div>`,
		"baseline in 120 nested":     nest(120, `<div style="display:flex;align-items:baseline"><div>t</div>`, `</div>`, `<div>leaf</div>`),
		"baseline column":            `<div style="display:flex;flex-direction:column;align-items:baseline"><div>a</div><div style="font-size:30px">b</div></div>`,
		"baseline with display:none": `<div style="display:flex;align-items:baseline"><div style="display:none">a</div><div>b</div></div>`,
		"definite height 1e9":        `<div style="display:flex;height:1e9px"><div>x</div><div style="height:50%">y</div></div>`,
		"definite height 0":          `<div style="display:flex;height:0"><div>x</div><div style="height:50%">y</div></div>`,
		"height % of item, nested":   nest(60, `<div style="display:flex;height:900px"><div style="height:90%">`, `</div></div>`, `x`),
		"wrap 3000 auto margins":     `<div style="display:flex;flex-wrap:wrap;width:300px">` + strings.Repeat(`<div style="width:40px;margin-left:auto">a</div>`, 3000) + `</div>`,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, pages, err := sizingRun(t, html)
			if err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatal(err)
				}
			} else {
				allFinite(t, name, pages)
			}
			if time.Since(start) > time.Minute {
				t.Error("hang")
			}
		})
	}
}

// 10 000 flex items with auto margins, baseline alignment and min-width cost
// no more than twice the plain twin and stay under the M2-04 bound.
func TestFlexM3WorkIsLinear(t *testing.T) {
	const n = 10000
	item := func(style string) string { return `<div style="width:30px;flex:none;` + style + `">a</div>` }
	mk := func(cont, style string) string {
		return `<div style="display:flex;flex-wrap:wrap;width:600px;` + cont + `">` + strings.Repeat(item(style), n) + `</div>`
	}
	rw, _, err := sizingRun(t, mk("align-items:baseline", "margin-left:auto;min-width:10px"))
	if err != nil {
		t.Fatal(err)
	}
	rt, _, err := sizingRun(t, mk("", ""))
	if err != nil {
		t.Fatal(err)
	}
	ww, wt := rw.b.Used(budget.Work), rt.b.Used(budget.Work)
	t.Logf("work %d, twin %d", ww, wt)
	if ww > 1_900_000 || ww > 2*wt+10000 {
		t.Errorf("work %d (twin %d; bound 1.9M)", ww, wt)
	}
}
