package style

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

type borderCase struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	HTML string `json:"html"`
	CSS  string `json:"css"`
	CS   map[string]map[string]string
}

// Every probe case of chrome_borders.txt that styles a single box "a": the computed border widths
// (and no border for none/hidden styles) equal Chrome's snapped used values.
func TestBorderWidthsMatchChromeComputedValues(t *testing.T) {
	f, err := os.Open("../layout/testdata/chrome_borders.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	n := 0
	for sc.Scan() {
		l := sc.Text()
		if !strings.HasPrefix(l, `{"kind":"dom"`) && !strings.HasPrefix(l, `{"kind": "dom"`) {
			continue
		}
		var c borderCase
		var raw struct {
			borderCase
			CS map[string]map[string]string `json:"cs"`
		}
		if err := json.Unmarshal([]byte(l), &raw); err != nil {
			t.Fatal(err)
		}
		c = raw.borderCase
		a := raw.CS["a"]
		if a == nil || !strings.Contains(c.HTML, `<div id=a style=`) || strings.Contains(c.HTML, "zoom") || strings.Contains(c.Name, "css zoom") {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			x := resolve(t, c.HTML, c.CSS, "")
			s := x.by(t, "#a")
			if unsupported(c.Name) {
				// Known deviation: calc() and the border styles other than solid, none and hidden are not
				// supported (the declaration is dropped, no border); the test fails once one is.
				for i := range s.Border {
					if s.Border[i].Width != 0 {
						t.Errorf("border %d width %v: now supported, remove it from unsupported()", i, s.Border[i].Width)
					}
				}
				return
			}
			for i, side := range []string{"top", "right", "bottom", "left"} {
				want, err := strconv.ParseFloat(strings.TrimSuffix(a["border-"+side+"-width"], "px"), 64)
				if err != nil {
					t.Fatal(err)
				}
				if got := s.Border[i].Width; got != want {
					t.Errorf("border-%s-width = %v, Chrome %v (html %s)", side, got, want, c.HTML)
				}
			}
		})
	}
	if n < 50 {
		t.Fatalf("%d cases checked", n)
	}
}

func unsupported(name string) bool {
	if strings.Contains(name, "calc(") {
		return true
	}
	for _, st := range []string{"double", "groove", "ridge", "inset", "outset"} {
		if strings.HasPrefix(name, "style "+st+" ") {
			return true
		}
	}
	return false
}

// Hostile border widths: finite, snapped, never above the length bound, negative or garbage values
// leave no border.
func TestHostileBorderWidths(t *testing.T) {
	for _, w := range []string{"1e30px", "1e308px", "1e999px", "-1e30px", "0.0000001px", "1e-300px", "99999999999em", "1e30pt", "1e30mm", "NaN", "inf", "1.5", "calc(1px+2px)", "1px 2px 3px 4px 5px", "thick thin", "0.9999999px", "1.0000001px", "2.9999999px"} {
		c, _ := computedOf(t, "border:"+w+" solid red")
		for i := range c.Border {
			v := c.Border[i].Width
			if v != v || v < 0 || v > 1e7 || v != float64(int64(v)) {
				t.Errorf("border:%s side %d: width %v", w, i, v)
			}
		}
	}
	for w, want := range map[string]float64{"0.0000001px": 1, "1e-300px": 0, "1e-40px": 1, "1e-20px": 1, "0.9999999px": 1, "1.0000001px": 1, "2.9999999px": 3, "1.99999px": 1, "2.99px": 2} {
		c, _ := computedOf(t, "border:"+w+" solid red")
		if c.Border[0].Width != want {
			t.Errorf("border:%s: %v, want %v", w, c.Border[0].Width, want)
		}
	}
}
