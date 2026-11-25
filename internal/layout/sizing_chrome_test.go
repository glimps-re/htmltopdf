package layout

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

type sizingCase struct {
	Name string               `json:"name"`
	W    float64              `json:"w"`
	CSS  string               `json:"css"`
	HTML string               `json:"html"`
	M    map[string][]float64 `json:"m"`
}

func loadSizingCases(t *testing.T) []sizingCase {
	t.Helper()
	return loadChromeCases(t, "testdata/chrome_sizing.txt")
}

// loadChromeCases reads the probe cases of a testdata/chrome_*.txt file.
func loadChromeCases(t *testing.T, path string) []sizingCase {
	t.Helper()
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []sizingCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, `{"meta"`) {
			continue
		}
		// Cases whose keys hold lists ("li[]") carry them as arrays of rects.
		var raw struct {
			sizingCase
			M map[string]json.RawMessage `json:"m"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatal(err)
		}
		c := raw.sizingCase
		c.M = map[string][]float64{}
		for k, v := range raw.M {
			var r []float64
			if json.Unmarshal(v, &r) == nil {
				c.M[k] = r
			}
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// sizingOwner names the task that owns a probe case this test does not check,
// "" for a case it does. Skipped cases are reported by name, never silently.
func sizingOwner(name string) string {
	has := func(subs ...string) bool {
		for _, sub := range subs {
			if strings.Contains(name, sub) {
				return true
			}
		}
		return false
	}
	switch {
	case strings.HasPrefix(name, "height"):
		switch {
		case has("on table", "on td", "on tr", "less than content on td"):
			return "M3-07 (table heights)"
		case has("flex"):
			return "M3-06 (flex heights)"
		case has("abs"):
			return "M3-04 (absolute boxes)"
		case has("huge"):
			return "TestHeightHostile"
		case has("cover"):
			return "the 02 conformance test (needs the 02 template CSS)"
		}
		return ""
	case strings.HasPrefix(name, "min-width"):
		switch {
		case has("on table", "on td"):
			return "M3-07 (table widths)"
		case has("abs"):
			return "M3-04 (absolute boxes)"
		case has("flex"):
			return "M3-06 (flex min-width)"
		}
		return ""
	case strings.HasPrefix(name, "vertical-align"):
		if has("%", "text-top", "text-bottom", "sub", "super", "calc") {
			return "unsupported value (invalid-value): not implemented"
		}
		return ""
	}
	return "M3-02 / M3-06 (02 legend and breakdown need ::before and flex min-width)"
}

// sizingKnownDeviation lists what the probe measures but this engine does not
// reproduce, for causes outside M3-03:
//   - inline boxes ("s") have no fragment, and "child" labels an anonymous box;
//   - "n" is set in the default font of the browser, and the rem of the probe page
//     is 13.328 px (its root font size);
//   - the line "l" of a fixed line-height holding a baseline-aligned atom taller
//     than the strut is 0.5 to 0.9 px shorter than Chrome's, with or without
//     vertical-align: <length>, and so is a middle-aligned atom there (M3-12).
func sizingKnownDeviation(name, id string) bool {
	switch {
	case id == "n" && strings.HasPrefix(name, "vertical-align"):
		return true
	case name == "height pt/in/em/rem" && id == "d":
		return true
	case name == "height on inline (ignored)" && id == "s", name == "min-width on inline (ignored)" && id == "a",
		name == "height 0" && id == "child":
		return true
	case strings.Contains(name, " lh="):
		return id == "l" || id == "a" && strings.HasPrefix(name, "vertical-align middle")
	}
	return false
}

// Each probe case of testdata/chrome_sizing.txt in scope: the border box of every
// element with an id within 0.5 px of what Chrome laid out.
func TestSizingMatchesChrome(t *testing.T) {
	n, rects := 0, 0
	for _, c := range loadSizingCases(t) {
		if owner := sizingOwner(c.Name); owner != "" {
			t.Run(c.Name, func(t *testing.T) { t.Skipf("owned by %s", owner) })
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
			html := `<div style="overflow:hidden">` + c.HTML + `</div>`
			pages := newRun(t, html, opts{extra: page, css: c.CSS}).all(t)
			for id, want := range c.M {
				if len(want) != 4 {
					continue
				}
				if sizingKnownDeviation(c.Name, id) {
					continue
				}
				f := findFrag(pages, id)
				if f == nil {
					t.Errorf("#%s: no fragment", id)
					continue
				}
				rects++
				got := [4]float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H}
				for i := range got {
					if math.Abs(got[i]-want[i]) > 0.5 {
						t.Errorf("#%s: [x y w h] = %.2f, Chrome %.2f", id, got, want)
						break
					}
				}
			}
		})
	}
	if n < 70 || rects < 130 {
		t.Fatalf("%d cases and %d rects compared", n, rects)
	}
}

// A block with a height is sliced across pages, the fragments consuming the
// height; the next sibling follows it.
func TestHeightIsSlicedAcrossPages(t *testing.T) {
	r := newRun(t, `<div id=a style="height:1500px;background:#ccc"><p id=p>x</p></div><div id=b>after</div>`,
		opts{extra: "@page { size: 800px 600px; margin: 0 } body { margin: 0 } p { margin: 0 }"})
	pages := r.all(t)
	if len(pages) != 3 {
		t.Fatalf("%d pages", len(pages))
	}
	sum := 0.0
	walkPages(pages, func(f *frag) {
		if f.box.ID == "a" {
			sum += f.rect.H
		}
	})
	if math.Abs(sum-1500) > 0.01 {
		t.Errorf("fragments sum to %v", sum)
	}
	b := fragByID(t, pages, "b")
	if math.Abs(b.rect.Y-300) > 0.01 {
		t.Errorf("next sibling at %v on its page", b.rect.Y)
	}
}

func walkPages(pages []*Page, fn func(*frag)) {
	for _, pg := range pages {
		walk(pg.root, fn)
	}
}

// A huge height costs a page per page area and ends in LimitPages like the
// twin of 500 pages; one of 499 pages renders.
func TestHeightHostile(t *testing.T) {
	start := time.Now()
	page := "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
	run := func(h string) (*run, error) {
		r := newRun(t, `<div style="height:`+h+`">x</div><p>y</p>`, opts{extra: page, limits: func(l *budget.Limits) { l[budget.Work] = 1 << 30 }})
		for {
			if _, err := r.pag.Next(); err != nil {
				if errors.Is(err, io.EOF) {
					return r, nil
				}
				return r, err
			}
		}
	}
	twin, err := run("299000px")
	if err != nil {
		t.Fatalf("499 pages: %v", err)
	}
	_, err = run("1e7px")
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Pages {
		t.Errorf("1e7px: %v", err)
	}
	if w := twin.b.Used(budget.Work); w > 1<<20 {
		t.Errorf("work %d for 499 pages", w)
	}
	if time.Since(start) > time.Minute {
		t.Error("hang")
	}
}

func findFrag(pages []*Page, id string) *frag {
	var found *frag
	walkPages(pages, func(f *frag) {
		if f.box.ID == id && found == nil {
			found = f
		}
	})
	return found
}

// A fixed height smaller than its content: hidden ends the box at the height on
// the page, without extra pages; visible keeps every line and the fragments of
// the box sum to the height.
func TestHeightSmallerThanContent(t *testing.T) {
	lines := strings.Repeat("line<br>", 100)
	css := "@page { size: 800px 600px; margin: 0 } body { margin: 0 } p { margin: 0; line-height: 20px }"
	t.Run("hidden", func(t *testing.T) {
		pages := newRun(t, `<div id=a style="height:50px;overflow:hidden"><p>`+lines+`</p></div><div id=b>after</div>`, opts{extra: css}).all(t)
		if len(pages) != 1 {
			t.Fatalf("%d pages", len(pages))
		}
		if a := fragByID(t, pages, "a"); math.Abs(a.rect.H-50) > 0.01 {
			t.Errorf("box %v tall", a.rect.H)
		}
		if b := fragByID(t, pages, "b"); math.Abs(b.rect.Y-50) > 0.01 {
			t.Errorf("sibling at %v", b.rect.Y)
		}
	})
	t.Run("visible", func(t *testing.T) {
		pages := newRun(t, `<div id=a style="height:50px"><p>`+lines+`</p></div><div id=b>after</div>`, opts{extra: css}).all(t)
		sum, n := 0.0, 0
		walkPages(pages, func(f *frag) {
			if f.box.ID == "a" {
				sum += f.rect.H
			}
		})
		for _, pg := range pages {
			n += len(linesOf(pg))
		}
		if math.Abs(sum-50) > 0.01 || n != 101 {
			t.Errorf("fragments sum to %v, %d lines on %d pages", sum, n, len(pages))
		}
	})
	t.Run("visible within one page", func(t *testing.T) {
		pages := newRun(t, `<div id=a style="height:50px"><p>`+strings.Repeat("line<br>", 10)+`</p></div><div id=b>after</div>`, opts{extra: css}).all(t)
		if b := fragByID(t, pages, "b"); math.Abs(b.rect.Y-50) > 0.01 {
			t.Errorf("sibling at %v", b.rect.Y)
		}
	})
	t.Run("7px hidden with 10000 lines", func(t *testing.T) {
		pages := newRun(t, `<div style="height:7px;overflow:hidden"><p>`+strings.Repeat("line<br>", 10000)+`</p></div>`, opts{extra: css}).all(t)
		if len(pages) != 1 {
			t.Errorf("%d pages", len(pages))
		}
	})
}
