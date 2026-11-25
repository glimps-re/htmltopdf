package layout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
)

type borderCase struct {
	Kind string                       `json:"kind"`
	Name string                       `json:"name"`
	W    float64                      `json:"w"`
	CSS  string                       `json:"css"`
	HTML string                       `json:"html"`
	M    map[string]json.RawMessage   `json:"m"`
	CS   map[string]map[string]string `json:"cs"`

	// The pdf kinds: the width given, the scale, and the drawn widths in px.
	Scale  float64 `json:"scale"`
	Width  string  `json:"width"`
	Drawn  float64 `json:"drawn_css_px"`
	Top    float64 `json:"top_center_border_bottom_css_px"`
	Bottom float64 `json:"bottom_left_border_css_px"`
}

func loadBorderCases(t *testing.T) []borderCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_borders.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []borderCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		if line := sc.Text(); line != "" && line[0] != '#' {
			var c borderCase
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// borderOwner returns why a probe case is not checked: styles other than solid,
// calc() and zoom are not supported.
func borderOwner(name string) string {
	for _, sub := range []string{"style dashed", "style dotted", "style double", "style groove", "style ridge", "style inset", "style outset"} {
		if strings.HasPrefix(name, sub) {
			return "border-style " + strings.Fields(name)[1] + " is not supported (invalid-value)"
		}
	}
	switch {
	case strings.Contains(name, "dashed") || strings.Contains(name, "double"):
		return "border-style dashed and double are not supported (invalid-value)"
	case strings.Contains(name, "calc("):
		return "calc() is not supported"
	case strings.HasPrefix(name, "css zoom"):
		return "zoom is not supported"
	case strings.HasPrefix(name, "02 .stamp"):
		return "needs the absolute box layout (M3-04)"
	case strings.HasPrefix(name, "02 .tag"):
		return "the probe sets the tag text in a font whose width differs by 3 px, and its line by the half-leading (M3-12)"
	}
	return ""
}

// Every "dom" case of testdata/chrome_borders.txt: the computed border widths of
// the elements with an id and their border boxes within 0.5 px of Chrome.
func TestBordersMatchChrome(t *testing.T) {
	n, rects, widths := 0, 0, 0
	tpl := templateCSS(t)
	for _, c := range loadBorderCases(t) {
		if c.Kind != "dom" {
			continue
		}
		if why := borderOwner(c.Name); why != "" {
			t.Run(c.Name, func(t *testing.T) { t.Skip(why) })
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			css := c.CSS
			if strings.HasPrefix(c.Name, "02 ") {
				css = tpl + "\n" + css
			}
			page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
			pages := newRun(t, `<div style="overflow:hidden">`+c.HTML+`</div>`, opts{extra: page, css: css}).all(t)
			for id, raw := range c.M {
				var want []float64
				if json.Unmarshal(raw, &want) != nil || len(want) != 4 {
					continue
				}
				f := findFrag(pages, id)
				if f == nil {
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
			for id, cs := range c.CS {
				f := findFrag(pages, id)
				if f == nil {
					continue
				}
				for i, side := range []string{"top", "right", "bottom", "left"} {
					want := cs["border-"+side+"-width"]
					widths++
					if got := fmt.Sprintf("%gpx", f.border[i]); want != "" && got != want {
						t.Errorf("#%s border-%s-width %s, Chrome %s", id, side, got, want)
					}
				}
			}
		})
	}
	if n < 60 || rects < 90 || widths < 200 {
		t.Fatalf("%d cases, %d rects and %d widths compared", n, rects, widths)
	}
}

// borderWidths returns the widths of the borders drawn in a list of items.
func borderWidths(pages []*Page) []float64 {
	var out []float64
	for _, pg := range pages {
		for _, it := range pg.Items {
			if b, ok := it.(display.Border); ok {
				for _, side := range b.Sides {
					if side.Width > 0 {
						out = append(out, side.Width)
					}
				}
			}
		}
	}
	return out
}

// The drawn width of a box border and of a margin box border is the snapped
// computed width, the same at Scale 1 and 0.9 ("pdf-box" and "pdf-margin-box" of
// chrome_borders.txt, in layout px: paint applies the scale to everything).
func TestDrawnBorderWidthsMatchChrome(t *testing.T) {
	n, margins := 0, 0
	for _, c := range loadBorderCases(t) {
		switch c.Kind {
		case "pdf-box":
			n++
			t.Run(fmt.Sprintf("box %s at %v", c.Width, c.Scale), func(t *testing.T) {
				page := "@page { size: 800px 600px; margin: 0 } body { margin: 0 }"
				html := `<div style="margin:10px;border:` + c.Width + ` solid #000">x</div>`
				pages := newRun(t, html, opts{extra: page, scale: c.Scale}).all(t)
				got := borderWidths(pages)
				if len(got) != 4 {
					t.Fatalf("%d drawn sides: %v", len(got), got)
				}
				for _, w := range got {
					if w != c.Drawn {
						t.Errorf("drawn %v px, Chrome %v", w, c.Drawn)
					}
				}
			})
		case "pdf-margin-box":
			margins++
			t.Run(fmt.Sprintf("margin box %s at %v", c.Width, c.Scale), func(t *testing.T) {
				css := "@page { size: 800px 600px; margin: 60px; @top-center { content: \"t\"; border-bottom: " + c.Width +
					" solid #000 } @bottom-left { content: \"b\"; border: " + c.Width + " solid #000 } } body { margin: 0 }"
				r := newRun(t, "<p>x</p>", opts{css: css, scale: c.Scale})
				pages := r.all(t)
				items, err := r.pag.MarginBoxes(pages[0], len(pages))
				if err != nil {
					t.Fatal(err)
				}
				var top, bottom []float64
				for _, it := range items {
					if b, ok := it.(display.Border); ok {
						if b.Rect.Y < 100 && b.Sides[2].Width > 0 {
							top = append(top, b.Sides[2].Width)
						} else if b.Rect.Y > 400 {
							bottom = append(bottom, b.Sides[0].Width)
						}
					}
				}
				if len(top) != 1 || top[0] != c.Top || len(bottom) != 1 || bottom[0] != c.Bottom {
					t.Errorf("top %v bottom %v, Chrome %v and %v", top, bottom, c.Top, c.Bottom)
				}
			})
		}
	}
	if n < 50 || margins < 12 {
		t.Fatalf("%d box cases, %d margin box cases", n, margins)
	}
}
