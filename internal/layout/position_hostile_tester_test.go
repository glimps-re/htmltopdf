package layout

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// 10 000 absolute boxes on one page cost no more than twice the static twin
// plus 10 000 Work.
func TestManyAbsoluteBoxesWork(t *testing.T) {
	const n = 10000
	abs := `<div style="position:relative;height:100px">` + strings.Repeat(`<i style="position:absolute;left:5px;top:7px;width:10px;height:10px;background:#c00"></i>`, n) + `</div>`
	static := `<div style="position:relative;height:100px">` + strings.Repeat(`<i style="display:block;width:10px;height:0;background:#c00"></i>`, n) + `</div>`
	ra, pa, err := sizingRun(t, abs)
	if err != nil {
		t.Fatal(err)
	}
	rs, _, err := sizingRun(t, static)
	if err != nil {
		t.Fatal(err)
	}
	wa, ws := ra.b.Used(budget.Work), rs.b.Used(budget.Work)
	t.Logf("work %d, static twin %d, %d pages", wa, ws, len(pa))
	if wa > 2*ws+10000 {
		t.Errorf("work %d against %d for the static twin", wa, ws)
	}
	if len(pa) != 1 {
		t.Errorf("%d pages", len(pa))
	}
}

// Absolute boxes nested 480 deep cost at most 2.2 times 240 deep.
func TestNestedAbsoluteBoxesWork(t *testing.T) {
	nest := func(d int) string {
		return strings.Repeat(`<div style="position:absolute;left:1px;top:1px;width:200px;height:200px">`, d) + "x" + strings.Repeat("</div>", d)
	}
	r240, _, err := sizingRun(t, nest(240))
	if err != nil {
		t.Fatal(err)
	}
	r480, _, err := sizingRun(t, nest(480))
	if err != nil {
		t.Fatal(err)
	}
	w240, w480 := r240.b.Used(budget.Work), r480.b.Used(budget.Work)
	t.Logf("work 240 deep %d, 480 deep %d", w240, w480)
	if float64(w480) > 2.2*float64(w240)+10000 {
		t.Errorf("480 deep %d against %d at 240", w480, w240)
	}
}

// Absolute boxes never create pages and keep finite coordinates whatever their insets, sizes and
// margins: the page count equals the twin without them.
func TestAbsoluteBoxesNeverMakePagesNorNaN(t *testing.T) {
	base := func(extra string) string {
		return `<div style="position:relative;height:300px">flow</div>` + extra
	}
	twin, tpages, err := sizingRun(t, base(""))
	if err != nil {
		t.Fatal(err)
	}
	_ = twin
	for name, style := range map[string]string{
		"left 1e7":                "left:1e7px;top:1e7px",
		"right -1e7":              "right:-1e7px;bottom:-1e7px",
		"left -1e7 top -1e7":      "left:-1e7px;top:-1e7px;width:1e7px;height:1e7px",
		"1e30":                    "left:1e30px;top:1e30px;width:1e30px;height:1e30px",
		"percent 1e9":             "left:1e9%;top:1e9%;width:1e9%;height:1e9%",
		"insets all + 1e9 height": "left:0;right:0;top:0;bottom:0;height:1e9px",
		"margin auto 1e30":        "left:0;right:0;margin:auto 1e30px;width:10px",
		"negative size":           "left:0;top:0;width:-5px;height:-5px",
		"width 0 text":            "left:0;top:0;width:0",
		"min-width 1e9":           "left:0;top:0;min-width:1e9px",
	} {
		t.Run(name, func(t *testing.T) {
			html := base(`<div style="position:absolute;background:#c00;` + style + `">text that wraps</div>`)
			_, pages, err := sizingRun(t, html)
			if err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatal(err)
				}
				return
			}
			allFinite(t, name, pages)
			if len(pages) != len(tpages) {
				t.Errorf("%d pages, %d without the absolute box", len(pages), len(tpages))
			}
		})
	}
	start := time.Now()
	var sb strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&sb, `<div style="position:absolute;left:%dpx;top:%dpx;right:%dpx;bottom:%dpx;z-index:%d"></div>`, i, -i, i*1000, -i*1000, i%7-3)
	}
	_, pages, err := sizingRun(t, base(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	allFinite(t, "flood", pages)
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

// An absolute inline element is out of flow: its paragraph keeps one line.
func TestAbsoluteInlineDoesNotBreakTheLine(t *testing.T) {
	_, pages, err := sizingRun(t, `<p id=p style="position:relative;margin:0">Hello <span style="position:absolute;background:#c00">badge</span> world</p>`)
	if err != nil {
		t.Fatal(err)
	}
	f := findAny(pages, "p")
	if f == nil || len(f.lines) != 1 {
		t.Fatalf("paragraph %+v", f)
	}
}

// A positioned cell in a row cut over three pages paints its absolute layer once, on the page of its
// fragment, whatever the number of fragments; relatives and clips with huge insets stay bounded.
func TestPositionedCellInCutRowPaintsItsLayerOnce(t *testing.T) {
	cell := strings.Repeat("x<br>", 90) // 1800px of lines: three pages of 600px
	_, pages, err := sizingRun(t, `<table style="border-spacing:0"><tr><td style="position:relative;padding:0">`+cell+`<i id=badge style="position:absolute;left:5px;top:5px;width:20px;height:20px;background:#c00"></i></td></tr></table>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	n := 0
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			for _, a := range f.abs {
				if a.f.box.ID == "badge" {
					n++
				}
			}
		})
	}
	if n != 1 {
		t.Errorf("the absolute box is a layer of %d fragments, want 1", n)
	}
}

func TestHostileRelativesAndClips(t *testing.T) {
	base, bp, err := sizingRun(t, `<div style="height:100px">flow</div>`)
	if err != nil {
		t.Fatal(err)
	}
	_ = base
	for name, html := range map[string]string{
		"relative 1e7":         `<div style="position:relative;left:1e7px;top:1e7px;height:100px">flow</div>`,
		"relative -1e30":       `<div style="position:relative;left:-1e30px;right:1e30px;top:-1e30px;bottom:1e30px;height:100px">flow</div>`,
		"relative percent 1e9": `<div style="position:relative;left:1e9%;top:1e9%;height:100px">flow</div>`,
		"nested relatives":     strings.Repeat(`<div style="position:relative;left:3px;top:3px">`, 300) + "flow" + strings.Repeat("</div>", 300),
		"overflow hidden abs":  `<div style="position:relative;overflow:hidden;height:50px;width:50px"><i style="position:absolute;left:1e7px;top:-1e7px;width:1e7px;height:1e7px;background:#c00"></i>flow</div>`,
		"rotate chain":         `<div style="position:relative;overflow:hidden;height:50px"><div style="position:absolute;left:0;top:0;transform:rotate(1e308deg);width:10px;height:10px;background:#c00"></div></div>`,
		"clip + relative abs":  `<div style="overflow:hidden;position:relative;height:30px"><div style="position:relative;left:50px"><i style="position:absolute;left:-1e6px;top:0;width:2e6px;height:10px;background:#c00"></i></div></div>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, pages, err := sizingRun(t, html)
			if err != nil {
				var le *budget.LimitError
				if !errors.As(err, &le) {
					t.Fatal(err)
				}
				return
			}
			allFinite(t, name, pages)
			if len(pages) != len(bp) {
				t.Errorf("%d pages, %d for the plain box", len(pages), len(bp))
			}
		})
	}
}
