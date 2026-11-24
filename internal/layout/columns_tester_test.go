package layout

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func tableOf(style, cols, cells string) string {
	return `<table id=t style="table-layout:fixed;` + style + `"><colgroup>` + cols + `</colgroup><tr>` + cells + `</tr></table>`
}

func repeat(s string, n int) string { return strings.Repeat(s, n) }

// Hostile column declarations: every cell stays finite, inside the pdf writer's
// coordinate bound, in document order, and the work stays bounded.
func TestColumnsHostileStayBounded(t *testing.T) {
	cells := func(n int) string { return repeat("<td>x</td>", n) }
	cases := []struct{ name, html string }{
		{"spacing 1e9", tableOf("width:100%;border-spacing:1e9px", repeat(`<col style="width:50px">`, 3), cells(3))},
		{"spacing 1e9 pct", tableOf("width:100%;border-spacing:1e9px", repeat(`<col style="width:30%">`, 3), cells(3))},
		{"pct sum 1e6", tableOf("width:400px", repeat(`<col style="width:1000000%">`, 3), cells(3))},
		{"pct mix px 1e7", tableOf("width:400px", `<col style="width:60%"><col style="width:90%"><col style="width:1e7px">`, cells(3))},
		{"huge px three", tableOf("width:400px", repeat(`<col style="width:1e7px">`, 3), cells(3))},
		{"huge px 1e30", tableOf("width:400px", repeat(`<col style="width:1e30px">`, 3), cells(3))},
		{"negative and zero", tableOf("width:400px", `<col style="width:-50px"><col style="width:0px"><col style="width:0%">`, cells(3))},
		{"1000 cols pct", tableOf("width:100%", repeat(`<col style="width:0.1%">`, 1000), cells(1000))},
		{"1000 cols auto", tableOf("width:100%", "", cells(1000))},
		{"1000 cols zero", tableOf("width:100%", repeat(`<col style="width:0px">`, 1000), cells(1000))},
		{"colspan huge", tableOf("width:400px", "", `<td colspan=1000 style="width:1e7px">x</td><td colspan=65535>y</td>`)},
		{"colgroup span", `<table id=t style="table-layout:fixed;width:400px"><colgroup><col span=1000 style="width:1e7px"></colgroup><tr><td>x</table>`},
		{"calc", tableOf("width:400px", `<col style="width:calc(100% + 1e7px)"><col style="width:calc(50% - 1e9px)">`, cells(2))},
		{"table width 1e9", tableOf("width:1e9px", repeat(`<col style="width:50%">`, 2), cells(2))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRun(t, c.html, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Work] = 1 << 30; l[budget.Pages] = 5000 }})
			pages := r.all(t)
			tds := fragsOf(pages, "td")
			if len(tds) == 0 {
				t.Fatal("no cell laid out")
			}
			for i, f := range tds {
				for _, v := range []float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H} {
					if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
						t.Fatalf("cell %d out of bounds: %s", i, rects(tds[i:i+1]))
					}
				}
				if f.rect.W < 0 {
					t.Fatalf("cell %d negative width %v", i, f.rect.W)
				}
				if i > 0 && f.rect.Y == tds[i-1].rect.Y && f.rect.X < tds[i-1].rect.X {
					t.Fatalf("cells out of order: %s", rects(tds[i-1:i+1]))
				}
			}
			if u := r.b.Used(budget.Work); u > 1<<26 {
				t.Errorf("work %d", u)
			}
		})
	}
}

// Overflowing columns keep every cell: nothing is dropped and the cells tile the
// row from the table's left edge.
func TestColumnsOverflowKeepsEveryCell(t *testing.T) {
	for _, n := range []int{2, 5, 40} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			html := tableOf("width:300px;border-spacing:0", repeat(`<col style="width:200px">`, n), repeat("<td>cell</td>", n))
			pages := newRun(t, html, opts{extra: smallPage, css: flat}).all(t)
			tds := fragsOf(pages, "td")
			if len(tds) != n {
				t.Fatalf("%d cells, want %d", len(tds), n)
			}
			for i, f := range tds {
				if math.Abs(f.rect.W-200) > 0.1 || math.Abs(f.rect.X-tds[0].rect.X-200*float64(i)) > 0.1 {
					t.Errorf("cell %d: %s", i, rects(tds[i:i+1]))
				}
			}
		})
	}
}

// Same input, same columns: the rule is a pure function of the declarations.
func TestColumnsDeterministic(t *testing.T) {
	html := tableOf("width:500px;border-spacing:7px", `<col style="width:30%"><col><col style="width:120px"><col style="width:10%">`, repeat("<td>x</td>", 4))
	var first string
	for i := range 3 {
		got := rects(fragsOf(newRun(t, html, opts{extra: smallPage, css: flat}).all(t), "td"))
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("run %d: %s, first %s", i, got, first)
		}
	}
}
