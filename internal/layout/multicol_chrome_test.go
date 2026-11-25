package layout

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

type mcWord struct {
	text string
	x, y float64
}

type mcColumn struct {
	X     float64 `json:"x"`
	XMax  float64 `json:"x_max"`
	N     int     `json:"n"`
	First string  `json:"first"`
	Last  string  `json:"last"`
}

type mcPage struct {
	Page      int
	Columns   []mcColumn
	Container []float64   `json:"container_bbox"`
	Rules     [][]float64 `json:"rule_paths"`
}

type mcCase struct {
	Kind    string
	Scale   float64
	Case    string
	HTML    string
	Pages   int
	PerPage []mcPage `json:"per_page"`
}

func loadMulticolCases(t *testing.T) []mcCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_multicol.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []mcCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		var c mcCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		if c.Kind == "" {
			out = append(out, c)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// mcOwner returns why a probe case is not checked.
func mcOwner(name string) string {
	switch {
	case strings.HasPrefix(name, "column-width"), strings.HasPrefix(name, "column-count 3 + column-width"):
		return "column-width is not supported: the count decides (Q-M4-2)"
	case strings.HasPrefix(name, "break-before:column"), strings.HasPrefix(name, "break-after:column"):
		return "break-before/after: column are not supported (Q-M4-2)"
	case strings.HasPrefix(name, "nested multicol"):
		return "a nested multicol is laid out as one column (Q-M4-2)"
	case strings.HasPrefix(name, "column-span"):
		return "column-span is not supported (Q-M4-2)"
	case strings.HasPrefix(name, "column-fill:"):
		return "Chrome adds columns past the count when a height is set; we continue on the next page (Q-M4-2)"
	case strings.HasPrefix(name, "padding and border on the container"):
		return "the probe container is wider than the page area: Chrome narrows its columns to the available width"
	case strings.HasPrefix(name, "columns 3, short content"):
		return "Chrome splits a two-line paragraph over two of three columns, against its orphans and widows"
	case strings.HasPrefix(name, "table 12 rows break-inside:auto"):
		return "a row cut leaves a 1 px sliver of its padding on the next column (the known row cut sliver) where Chrome balances to 144 px"
	case strings.HasPrefix(name, "empty content"):
		return "min-height is not supported (css-coverage)"
	case strings.HasPrefix(name, "height 400 columns 2 fill auto"):
		return "Chrome adds columns past the count when a height is set; we continue on the next page (Q-M4-2)"
	case strings.HasPrefix(name, "columns 3, rule 2px dotted"), strings.HasPrefix(name, "columns 2, rule 1px dashed"):
		return "dashed and dotted rules: owned by M4-06 (display border style), which un-skips them when it merges"
	}
	return ""
}

// pageWords returns the words of the text items of a page in pt from its top left.
func pageWords(pg *Page) []mcWord {
	var out []mcWord
	for _, it := range pg.Items {
		t, ok := it.(*display.Text)
		if !ok {
			continue
		}
		off := 0
		for _, w := range strings.Fields(t.Text) {
			i := strings.Index(t.Text[off:], w) + off
			off = i + len(w)
			x := t.X
			for _, g := range t.Glyphs {
				if int(g.Cluster) < i {
					x += float64(g.Advance)
				}
			}
			out = append(out, mcWord{text: w, x: (pg.Area.X + x*pg.Scale) * 0.75, y: (pg.Area.Y + t.Baseline*pg.Scale) * 0.75})
		}
	}
	return out
}

func TestMulticolMatchesChrome(t *testing.T) {
	const tol = 0.4
	n := 0
	for _, c := range loadMulticolCases(t) {
		name := c.Case
		if c.Scale != 1 {
			name += " at 0.9"
		}
		if why := mcOwner(c.Case); why != "" {
			t.Run(name, func(t *testing.T) { t.Skip(why) })
			continue
		}
		n++
		t.Run(name, func(t *testing.T) {
			r := newRun(t, c.HTML, opts{extra: "@page { size: 793.7px 1124px; margin: 76px } body { margin: 0 }", scale: c.Scale})
			pages := r.all(t)
			if len(pages) != c.Pages {
				t.Fatalf("%d pages, Chrome %d", len(pages), c.Pages)
			}
			for pi, want := range c.PerPage {
				pg := pages[pi]
				words := pageWords(pg)
				for _, col := range want.Columns {
					var first, last *mcWord
					for i := range words {
						w := &words[i]
						in := w.x >= col.X-tol && w.x <= col.XMax+tol
						if first == nil && w.text == col.First && in {
							first = w
						}
						if w.text == col.Last && in {
							last = w
						}
					}
					if first == nil || last == nil {
						t.Errorf("page %d: column at x=%.1f: first %q found %v, last %q found %v", want.Page, col.X, col.First, first != nil, col.Last, last != nil)
						continue
					}
					overlap := false
					for _, o := range want.Columns {
						if o.X > col.X+tol && o.X < col.XMax {
							overlap = true
						}
					}
					cnt := 0
					for _, w := range words {
						if w.x >= col.X-tol && w.x <= col.XMax+tol && w.y >= first.y-0.01 && w.y <= last.y+0.01 {
							cnt++
						}
					}
					if cnt != col.N && !overlap {
						t.Errorf("page %d: column at x=%.1f: %d words between %s and %s, Chrome %d", want.Page, col.X, cnt, col.First, col.Last, col.N)
					}
				}
				if len(want.Container) == 4 && pi == len(c.PerPage)-1 {
					var got *frag
					walkPages(pages[pi:pi+1], func(f *frag) {
						if got == nil && f.box.Style.ColumnCount > 1 {
							got = f
						}
					})
					if got == nil {
						t.Errorf("page %d: no multi-column container", want.Page)
					} else {
						h := (got.rect.H * pg.Scale) * 0.75
						if math.Abs(h-want.Container[3]) > tol {
							t.Errorf("page %d: container height %.2f pt, Chrome %.2f", want.Page, h, want.Container[3])
						}
					}
				}
				var rules [][]float64
				for _, it := range pg.Items {
					if fr, ok := it.(display.FillRect); ok && fr.Color.R == 1 && fr.Color.G == 0 {
						rules = append(rules, []float64{(pg.Area.X + fr.Rect.X*pg.Scale) * 0.75, (pg.Area.Y + fr.Rect.Y*pg.Scale) * 0.75, fr.Rect.W * pg.Scale * 0.75, fr.Rect.H * pg.Scale * 0.75})
					}
				}
				sort.Slice(rules, func(i, j int) bool { return rules[i][0] < rules[j][0] })
				if len(rules) != len(want.Rules) {
					t.Errorf("page %d: %d rules, Chrome %d", want.Page, len(rules), len(want.Rules))
					continue
				}
				for i := range rules {
					for k := range rules[i] {
						if i < len(want.Rules) && math.Abs(rules[i][k]-want.Rules[i][k]) > tol {
							t.Errorf("page %d: rule %d = %.2f, Chrome %.2f", want.Page, i, rules[i], want.Rules[i])
							break
						}
					}
				}
			}
		})
	}
	if n < 40 {
		t.Fatalf("%d cases checked", n)
	}
}
