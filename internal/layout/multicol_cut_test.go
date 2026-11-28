package layout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

func brLines(prefix string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return strings.Join(lines, "<br>")
}

func tableRows(n, lines int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "<tr><td>%s</td></tr>", brLines(fmt.Sprintf("r%dl", i), lines))
	}
	return sb.String()
}

// textAboveColumns returns the first text of a multi-column container drawn
// with its glyph tops above top on its page, "" when there is none.
func textAboveColumns(pages []*Page, top float64) string {
	var walk func(f *frag, in bool) string
	walk = func(f *frag, in bool) string {
		in = in || f.box != nil && f.box.Style != nil && f.box.Style.ColumnCount > 1
		for _, ln := range f.lines {
			for _, it := range ln.items {
				if in && it.text != nil && ln.baseline-it.ascent < top-cutEps {
					return fmt.Sprintf("%q at %v", it.text.Text, ln.baseline-it.ascent)
				}
			}
		}
		for _, c := range f.children {
			if s := walk(c, in); s != "" {
				return s
			}
		}
		return ""
	}
	for _, pg := range pages {
		if pg.root != nil {
			if s := walk(pg.root, false); s != "" {
				return fmt.Sprintf("page %d: %s", pg.Number, s)
			}
		}
	}
	return ""
}

// columnsUsed is the number of distinct x positions of the lines of the page.
func columnsUsed(pg *Page) int {
	xs := map[float64]bool{}
	var walk func(f *frag)
	walk = func(f *frag) {
		for _, ln := range f.lines {
			if len(ln.items) > 0 {
				xs[ln.rect.X] = true
			}
		}
		for _, c := range f.children {
			walk(c)
		}
	}
	if pg.root != nil {
		walk(pg.root)
	}
	return len(xs)
}

// A balanced column is never shorter than a line of a table row or a flex or
// grid band that it slices: the line is drawn whole in its column, not above
// the column top.
func TestBalancedColumnsDoNotCutALine(t *testing.T) {
	const page = "@page { size: 300px 200px; margin: 10px }"
	const intro = "<div style='height:60px'>intro</div>"
	for _, tc := range []struct {
		name string
		html string
		// top is the top of the columns: the glyph tops in them start below it
		// (68 after the 8 px body margin and the 60 px intro).
		top   float64
		pages int
		// cols is the number of distinct x positions of the lines on the last
		// page, the intro included.
		cols int
	}{
		{"one-line table rows, last row alone", "<div style='columns:3'><table>" + tableRows(22, 1) + "</table></div>", 0, 2, 1},
		{"one-line table rows over three pages", "<div style='columns:3'><table>" + tableRows(46, 1) + "</table></div>", 0, 3, 1},
		{"row resumed with one line left, thead", "<div style='columns:3;column-gap:4px;column-rule:1px solid'><table><thead><tr><th>h</th></tr></thead>" + tableRows(11, 2) + "</table></div>", 0, 2, 1},
		{"flex item resumed with one line left", "<div style='columns:3'><div style='display:flex'><div>" + brLines("w", 28) + "</div></div></div>", 0, 2, 1},
		{"grid item resumed with one line left", "<div style='columns:3'><div style='display:grid'><div>" + brLines("w", 28) + "</div></div></div>", 0, 2, 1},
		{"one-row table mid-page", intro + "<div style='columns:3'><table>" + tableRows(1, 1) + "</table></div>", 68, 1, 2},
		{"two-line flex item mid-page", intro + "<div style='columns:3'><div style='display:flex'><div>" + brLines("w", 2) + "</div></div></div>", 68, 1, 2},
		{"flex item still balanced", "<div style='columns:3'><div style='display:flex'><div>" + brLines("w", 12) + "</div></div></div>", 0, 1, 3},
		{"table rows still balanced", "<div style='columns:3'><table>" + tableRows(12, 1) + "</table></div>", 0, 1, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRun(t, tc.html, opts{extra: page})
			pages := r.all(t)
			if len(pages) == 0 {
				t.Fatal("no page")
			}
			if lost := TextLost(r.root, pages); lost != "" {
				t.Errorf("text lost: %s", lost)
			}
			if s := textAboveColumns(pages, tc.top); s != "" {
				t.Errorf("text above the column top: %s", s)
			}
			if len(pages) != tc.pages {
				t.Errorf("%d pages, want %d", len(pages), tc.pages)
			}
			if cols := columnsUsed(pages[len(pages)-1]); cols != tc.cols {
				t.Errorf("%d columns used on the last page, want %d", cols, tc.cols)
			}
		})
	}
}

// A text taller than the room of the columns is cut whatever their height:
// balancing does not reject every height for it, so the page count and the
// passes stay those of a plain balance.
func TestBalancedColumnsUnavoidableCut(t *testing.T) {
	html := strings.Repeat("<div style='columns:3'><table><tr><td>a<span style='font-size:300px'>X</span>b</td></tr><tr><td>y</td></tr></table></div>", 3)
	r := newRun(t, html, opts{extra: "@page { size: 300px 200px; margin: 10px }"})
	passes := 0
	r.pag.l.onColumns = func() { passes++ }
	pages := r.all(t)
	if len(pages) != 1 {
		t.Errorf("%d pages, want 1", len(pages))
	}
	if passes > 49 {
		t.Errorf("%d balancing passes, want at most 49", passes)
	}
}

// cutStart marks the layout sliced when a text cut by the previous end stays
// cut and a column of the container's room would hold it; a text it lifts is
// whole, and one taller than the room, or outside columns, is not marked.
func TestCutStartMarksSliced(t *testing.T) {
	item := lineItem{text: &display.Text{Baseline: 304, Size: 16}, ascent: 12}
	ln := &lineFrag{rect: display.Rect{Y: 290, H: 18}, baseline: 304, items: []lineItem{item}}
	for _, tc := range []struct {
		name    string
		room    float64
		colRoom float64
		sliced  bool
	}{
		{"lifted whole", 100, 300, false},
		{"deeper than half the room", 10, 300, true},
		{"taller than the column room", 10, 11, false},
		{"outside columns", 10, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &layouter{b: testBudget(nil), bottom: tc.room, pageH: 300, colRoom: tc.colRoom}
			lift, err := l.cutStart([]*frag{{lines: []*lineFrag{ln}}}, 300, 0)
			if err != nil {
				t.Fatal(err)
			}
			if l.sliced != tc.sliced {
				t.Errorf("sliced %v (lift %v), want %v", l.sliced, lift, tc.sliced)
			}
		})
	}
}
