package svg

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

type chromeCase struct {
	Name   string     `json:"name"`
	Markup string     `json:"markup"`
	BBox   [4]float64 `json:"bbox"`
	Len    float64    `json:"len"`
	Err    string     `json:"err"`
}

func loadChromeSVG(t *testing.T) []chromeCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_svg.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []chromeCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		var c chromeCase
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

// geom returns the tight bounding box and the length of segs (curves sampled).
// A moveto only counts once something is drawn from it, as in Chrome.
func geom(segs []Seg) (bbox [4]float64, length float64, curves, any bool) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	add := func(p Point) {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		any = true
	}
	var cur, start Point
	moved := false
	draw := func() {
		if moved {
			add(cur)
			moved = false
		}
	}
	for _, s := range segs {
		switch s.Op {
		case MoveTo:
			cur, start = s.P[0], s.P[0]
			moved = true
		case LineTo:
			draw()
			length += math.Hypot(s.P[0].X-cur.X, s.P[0].Y-cur.Y)
			cur = s.P[0]
			add(cur)
		case CubicTo:
			draw()
			curves = true
			prev := cur
			const n = 4000
			for i := 1; i <= n; i++ {
				u := float64(i) / n
				v := 1 - u
				p := Point{
					v*v*v*cur.X + 3*v*v*u*s.P[0].X + 3*v*u*u*s.P[1].X + u*u*u*s.P[2].X,
					v*v*v*cur.Y + 3*v*v*u*s.P[0].Y + 3*v*u*u*s.P[1].Y + u*u*u*s.P[2].Y,
				}
				length += math.Hypot(p.X-prev.X, p.Y-prev.Y)
				prev = p
				add(p)
			}
			cur = s.P[2]
		case Close:
			draw()
			length += math.Hypot(start.X-cur.X, start.Y-cur.Y)
			cur = start
		}
	}
	if any {
		bbox = [4]float64{minX, minY, maxX - minX, maxY - minY}
	}
	return
}

// Cases where ours differs from Chrome, with the reason; the test fails when
// one stops differing so that the list stays true.
var chromeKnownDiff = map[string]string{
	"rect zero width":      "getBBox of a rect that is not rendered (width 0): not drawn on either side",
	"rect negative height": "getBBox of a rect that is not rendered (negative height): not drawn on either side",
	"ellipse rx 0":         "getBBox of an ellipse that is not rendered (rx 0): not drawn on either side",
}

// The geometry of every shape of the probe matches Chrome's getBBox (arcs are
// cubics: a small tolerance) and, for shapes without curves, getTotalLength
// (Chrome's length of a curve is a coarse estimate: a circle of radius 20 is
// 124.85 instead of 125.66).
func TestGeometryMatchesChrome(t *testing.T) {
	cases := loadChromeSVG(t)
	if len(cases) < 90 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Err != "" {
				t.Skip(c.Err)
			}
			img := mustSVG(t, c.Markup)
			var all []Seg
			for _, s := range img.Shapes {
				all = append(all, s.Segs...)
			}
			got, length, curves, any := geom(all)
			var diffs []string
			if !any {
				if c.BBox[2] != 0 || c.BBox[3] != 0 || c.Len != 0 {
					diffs = append(diffs, "no shape, Chrome bbox "+fmtBox(c.BBox))
				}
			} else {
				tol := 2e-3 * (1 + math.Max(c.BBox[2], c.BBox[3]))
				for i := range got {
					if i < 2 && c.BBox[2] == 0 && c.BBox[3] == 0 {
						continue
					}
					if math.Abs(got[i]-c.BBox[i]) > tol {
						diffs = append(diffs, "bbox "+fmtBox(got)+", Chrome "+fmtBox(c.BBox))
						break
					}
				}
				if !curves && math.Abs(length-c.Len) > 2e-3*(1+c.Len) {
					diffs = append(diffs, "length "+strconv.FormatFloat(length, 'f', 4, 64)+", Chrome "+strconv.FormatFloat(c.Len, 'f', 4, 64))
				}
			}
			why, known := chromeKnownDiff[c.Name]
			switch {
			case len(diffs) > 0 && known:
				t.Logf("known deviation (%s): %v", why, diffs)
			case len(diffs) > 0:
				t.Errorf("%v", diffs)
			case known:
				t.Errorf("no longer differs from Chrome (%s): remove it from chromeKnownDiff", why)
			}
		})
	}
}

func fmtBox(b [4]float64) string {
	return strconv.FormatFloat(b[0], 'f', 3, 64) + "," + strconv.FormatFloat(b[1], 'f', 3, 64) + "," + strconv.FormatFloat(b[2], 'f', 3, 64) + "," + strconv.FormatFloat(b[3], 'f', 3, 64)
}
