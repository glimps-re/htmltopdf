package layout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

type colCase struct {
	Name     string   `json:"name"`
	Cols     []string `json:"cols"`
	Cells    [][]any  `json:"cells"`
	Collapse bool     `json:"collapse"`
	Spacing  float64  `json:"spacing"`
	TW       string   `json:"tw"`
}

type colWant struct {
	Case  colCase      `json:"case"`
	Cells [][2]float64 `json:"cells"`
	Table float64      `json:"table"`
}

// html builds the table of the probe that measured the case in Chrome
// (tmp/colprobe/probe.html, dev-only, never committed): a wrapper of 800px, a
// fixed table, one row of cells without padding.
func (c colCase) html() string {
	var sb strings.Builder
	collapse := "separate"
	if c.Collapse {
		collapse = "collapse"
	}
	fmt.Fprintf(&sb, `<div style="width:800px"><table id=t style="table-layout:fixed;width:%s;border-collapse:%s;border-spacing:%gpx">`, c.TW, collapse, c.Spacing)
	if c.Cols != nil {
		sb.WriteString("<colgroup>")
		for _, w := range c.Cols {
			if w == "" {
				sb.WriteString("<col>")
			} else {
				fmt.Fprintf(&sb, `<col style="width:%s">`, w)
			}
		}
		sb.WriteString("</colgroup>")
	}
	sb.WriteString("<tr>")
	if c.Cells != nil {
		for _, cell := range c.Cells {
			w, _ := cell[0].(string)
			span, _ := cell[1].(float64)
			style := "padding:0"
			if w != "" {
				style += ";width:" + w
			}
			fmt.Fprintf(&sb, `<td style="%s" colspan=%d>x</td>`, style, int(span))
		}
	} else {
		for range c.Cols {
			sb.WriteString(`<td style="padding:0">x</td>`)
		}
	}
	sb.WriteString("</tr></table></div>")
	return sb.String()
}

func loadColCases(t *testing.T) []colWant {
	t.Helper()
	f, err := os.Open("testdata/chrome_cols.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []colWant
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "#") {
			continue
		}
		var w colWant
		if err := json.Unmarshal(sc.Bytes(), &w); err != nil {
			t.Fatal(err)
		}
		out = append(out, w)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Every probe case: the x and width of each cell of the first row, relative to
// the table, within 0.1px of what Chrome laid out (testdata/chrome_cols.txt).
func TestColumnsMatchChrome(t *testing.T) {
	cases := loadColCases(t)
	if len(cases) < 40 {
		t.Fatalf("%d probe cases", len(cases))
	}
	for _, w := range cases {
		t.Run(w.Case.Name, func(t *testing.T) {
			if w.Case.Name == "px 1e7 x3" {
				t.Skip("3e7px in Chrome, bounded to 1e7px here: see TestHostileColumnWidths")
			}
			pages := newRun(t, w.Case.html(), opts{extra: smallPage, css: flat + " td { padding: 0 }"}).all(t)
			table := fragByID(t, pages, "t")
			rows := frags(pages, "tr")
			if len(rows) == 0 {
				t.Fatal("no row")
			}
			if math.Abs(rows[0].rect.W-w.Table) > 0.1 || math.Abs(table.rect.W-w.Table) > 0.1 {
				t.Errorf("row %.2f and table %.2f wide, Chrome table %.2f", rows[0].rect.W, table.rect.W, w.Table)
			}
			var cells []*frag
			walk(table, func(f *frag) {
				if f.box.Tag.String() == "td" && len(cells) < len(w.Cells) {
					cells = append(cells, f)
				}
			})
			if len(cells) != len(w.Cells) {
				t.Fatalf("%d cells, want %d", len(cells), len(w.Cells))
			}
			for i, c := range cells {
				if x, wd := c.rect.X-table.rect.X, c.rect.W; math.Abs(x-w.Cells[i][0]) > 0.1 || math.Abs(wd-w.Cells[i][1]) > 0.1 {
					t.Errorf("cell %d at x %.2f width %.2f, Chrome x %.2f width %.2f", i, x, wd, w.Cells[i][0], w.Cells[i][1])
				}
			}
		})
	}
}

func wideTable(n int, width string) string {
	var cols, cells strings.Builder
	for range n {
		fmt.Fprintf(&cols, `<col style="width:%s">`, width)
		cells.WriteString("<td>x</td>")
	}
	return `<table id=t style="table-layout:fixed;width:100%;border-spacing:0"><colgroup>` + cols.String() + `</colgroup><tr>` + cells.String() + `</tr></table>`
}

// Hostile widths are bounded: 1000 columns of 1e7px make a table of 1e7px, with a
// warning, and cost what 1000 columns of 10px cost; 1000 columns of 1e6 % share
// the table width equally.
func TestHostileColumnWidths(t *testing.T) {
	start := time.Now()
	run := func(html string) (*run, []*Page) {
		r := newRun(t, html, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Work] = 1 << 30; l[budget.Pages] = 5000 }})
		return r, r.all(t)
	}
	twin, _ := run(wideTable(1000, "10px"))
	r, pages := run(wideTable(1000, "1e7px"))
	rows := frags(pages, "tr")
	if len(rows) == 0 {
		t.Fatal("no row")
	}
	row := rows[0]
	if row.rect.W > 1e7+1 || row.rect.W < 1e7-1 {
		t.Errorf("table row %v wide, want the 1e7 bound", row.rect.W)
	}
	if w, _ := r.b.Warnings(); !hasWarning(w, "table width") {
		t.Errorf("no clamp warning: %+v", w)
	}
	if float64(r.b.Used(budget.Work)) > 1.1*float64(twin.b.Used(budget.Work)) {
		t.Errorf("work %d against %d for the twin", r.b.Used(budget.Work), twin.b.Used(budget.Work))
	}
	rp, pp := run(wideTable(1000, "1000000%"))
	tds := frags(pp, "td")
	if len(tds) != 1000 {
		t.Fatalf("%d cells", len(tds))
	}
	prows := frags(pp, "tr")
	if len(prows) == 0 {
		t.Fatal("no row")
	}
	prow := prows[0]
	sum := 0.0
	for _, td := range tds {
		sum += td.rect.W
		if math.Abs(td.rect.W-tds[0].rect.W) > 0.1 {
			t.Fatalf("cells %v and %v wide: not equal shares", tds[0].rect.W, td.rect.W)
		}
	}
	if math.Abs(sum-prow.rect.W) > 0.1 {
		t.Errorf("cells sum to %v in a row %v wide: %v", sum, prow.rect.W, tds[0].rect)
	}
	if w, _ := rp.b.Warnings(); hasWarning(w, "table width") {
		t.Errorf("clamp warning for percentages: %+v", w)
	}
	if float64(rp.b.Used(budget.Work)) > 1.1*float64(twin.b.Used(budget.Work)) {
		t.Errorf("1e6%%: work %d against %d", rp.b.Used(budget.Work), twin.b.Used(budget.Work))
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

func hasWarning(ws []budget.Warning, detail string) bool {
	for _, w := range ws {
		if w.Detail == detail {
			return true
		}
	}
	return false
}
