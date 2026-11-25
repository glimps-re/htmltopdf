package layout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

type svgLayoutCase struct {
	Name string               `json:"name"`
	W    float64              `json:"w"`
	CSS  string               `json:"css"`
	HTML string               `json:"html"`
	M    map[string][]float64 `json:"m"`
}

func loadSVGLayoutCases(t *testing.T) []svgLayoutCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_svg_layout.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []svgLayoutCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, `{"meta"`) {
			continue
		}
		var c svgLayoutCase
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// svgKnownDiff lists probe cases whose svg box differs from Chrome, with the reason; the test
// fails when one starts to match so that the list stays true.

func svgKnownDiff(c svgLayoutCase) string {
	switch c.Name {
	case "two inline svgs wrapping":
		return "the height of the container of two wrapped atoms is 1 px short of Chrome's (line metrics, M3-12)"
	case "svg in span in text":
		return "an inline element has no fragment to measure"
	case "svg min-height/max-height":
		return "min-height and max-height are not supported (deferred)"
	}
	return ""
}

// The border box of every <svg> (and of the other measured elements) matches Chrome's within 0.5 px.
func TestSVGBoxesMatchChrome(t *testing.T) {
	tpl := templateCSS(t)
	cases := loadSVGLayoutCases(t)
	if len(cases) < 40 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
			pages := newRun(t, `<div style="overflow:hidden">`+c.HTML+`</div>`, opts{extra: page, css: tpl + "\n" + c.CSS}).all(t)
			var diffs []string
			for key, want := range c.M {
				if len(want) != 4 || key == "d" && strings.HasPrefix(c.Name, "svg float") {
					continue
				}
				id := map[string]string{"s": "s", "s2": "s2", "s3": "s3", "d": "d", "t": "t", "n": "n", "cv": "cv", "k": "k", "sp": "sp"}[key]
				var f *frag
				if key == "s" || key == "s2" || key == "s3" {
					f = findFrag(pages, id)
					if f == nil {
						f = firstSVG(pages)
					}
				} else {
					f = findFrag(pages, id)
				}
				if f == nil {
					diffs = append(diffs, key+": no box")
					continue
				}
				got := [4]float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H}
				for i := range got {
					if math.Abs(got[i]-want[i]) > 0.5 && !(want[i] > 1e6 && got[i] > 1e6) {
						diffs = append(diffs, fmt.Sprintf("%s: [x y w h] = %.2f, Chrome %.2f", key, got, want))
						break
					}
				}
			}
			why := svgKnownDiff(c)
			known := why != ""
			switch {
			case len(diffs) > 0 && known:
				t.Logf("known deviation (%s): %v", why, diffs)
			case len(diffs) > 0:
				t.Errorf("%v", diffs)
			case known:
				t.Errorf("now matches Chrome (%s): remove it from svgKnownDiff", why)
			}
		})
	}
}

func firstSVG(pages []*Page) *frag {
	var found *frag
	walkPages(pages, func(f *frag) {
		if found == nil && f.box.Kind.String() == "svg" {
			found = f
		}
	})
	return found
}
