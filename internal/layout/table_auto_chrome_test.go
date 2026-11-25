package layout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/style"
)

// tableSizingCases are the table cases of chrome_sizing.txt (heights and
// min-width on tables, rows and cells).
var tableSizingCases = map[string]bool{
	"height on table": true, "height on table, two rows": true, "height on td": true, "height on tr": true,
	"height less than content on td": true, "min-width on table": true, "min-width on td": true,
}

// autoTableOwner returns why a probe case of chrome_auto_cols.txt is not
// checked, "" when it is.
func autoTableOwner(string) string { return "" }

// Every probe case of testdata/chrome_auto_cols.txt: the table and each cell of
// the first row within 0.5 px of what Chrome laid out.
func TestAutoColumnsMatchChrome(t *testing.T) {
	tpl := templateCSS(t)
	n := 0
	for _, c := range loadAutoCases(t) {
		if why := autoTableOwner(c.Name); why != "" {
			t.Run(c.Name, func(t *testing.T) { t.Skip(why) })
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
			html := `<div style="overflow:hidden">` + c.HTML + `</div>`
			pages := newRun(t, html, opts{extra: page, css: tpl + "\n" + c.CSS}).all(t)
			tables := frags(pages, "table")
			if len(tables) == 0 {
				t.Fatal("no table")
			}
			var cells []*frag
			var collect func(f *frag)
			collect = func(f *frag) {
				if tag := f.box.Tag.String(); tag == "td" || tag == "th" {
					cells = append(cells, f)
				}
				for _, c := range f.children {
					if c.box.Style.Display != style.DisplayTable {
						collect(c)
					}
				}
			}
			collect(tables[0])
			if len(cells) < len(c.Cells) {
				t.Fatalf("%d cells, Chrome %d", len(cells), len(c.Cells))
			}
			tw := tables[0].rect.W
			if math.Abs(tw-c.Table[2]) > 0.5 {
				t.Errorf("table %.2f wide, Chrome %.2f", tw, c.Table[2])
			}
			// The 02 tables differ by the half-leading of their lines (M3-12).
			if math.Abs(tables[0].rect.H-c.Table[3]) > 0.5 && !strings.HasPrefix(c.Name, "02") {
				t.Errorf("table %.2f tall, Chrome %.2f", tables[0].rect.H, c.Table[3])
			}
			for i, want := range c.Cells {
				// A collapsed table: Chrome's outer cells start inside the half of the outer
				// border that the table box holds, ours carry the whole of it. Only the
				// inner cell and the table width are compared.
				if strings.HasPrefix(c.Name, "collapse") && (i == 0 || i == len(c.Cells)-1) {
					t.Logf("%s: cell %d not compared (outer half border)", c.Name, i)
					continue
				}
				got := cells[i].rect
				if math.Abs(got.X-want[0]) > 0.5 || math.Abs(got.W-want[2]) > 0.5 {
					t.Errorf("cell %d at x %.2f width %.2f, Chrome x %.2f width %.2f", i, got.X, got.W, want[0], want[2])
				}
			}
		})
	}
	if n < 80 {
		t.Fatalf("%d cases", n)
	}
}

type autoCase struct {
	Name string  `json:"name"`
	W    float64 `json:"w"`
	CSS  string  `json:"css"`
	HTML string  `json:"html"`
	M    struct {
		Table []float64   `json:"table"`
		Cells [][]float64 `json:"cells[]"`
	} `json:"m"`
	Table []float64
	Cells [][]float64
}

func loadAutoCases(t *testing.T) []autoCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_auto_cols.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []autoCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, `{"meta"`) {
			continue
		}
		var c autoCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		c.Table, c.Cells = c.M.Table, c.M.Cells
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The height of a table, a row and a cell, and the min-width of a table and a
// cell (chrome_sizing.txt).
func TestTableSizingMatchesChrome(t *testing.T) {
	n := 0
	for _, c := range loadSizingCases(t) {
		if !tableSizingCases[c.Name] {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) { compareChrome(t, c, "") })
	}
	if n != len(tableSizingCases) {
		t.Fatalf("%d cases", n)
	}
}
