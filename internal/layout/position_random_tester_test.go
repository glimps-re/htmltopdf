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

type posRandomCase struct {
	Name string               `json:"name"`
	W    float64              `json:"w"`
	CSS  string               `json:"css"`
	HTML string               `json:"html"`
	M    map[string][]float64 `json:"m"`
}

func loadPosRandom(t *testing.T) []posRandomCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_positioned_random.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []posRandomCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, `{"meta"`) {
			continue
		}
		var c posRandomCase
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

// posRandomKnownDiff lists the random cases whose boxes still differ from Chrome; the test fails when
// one starts matching, so the list only shrinks.
var posRandomKnownDiff = map[string]string{
	"pos random 93": "static parent, right: -8px: the box overflows the viewport horizontally and Chrome takes a 15 px scrollbar off the height of the initial containing block (1108 instead of 1123); width/height 30% follow it. Not reproduced (diagnosed)",
}

// Differential test against Chrome: 200 seeded random absolute boxes (insets in px, % and mm, sizes,
// margins incl. auto, padding, border, min-width, in relative, static and absolute parents): the
// parent, the absolute box and the next sibling within 0.5 px of Chrome (the line metrics of the
// text are the same, the fonts being the same).
func TestRandomAbsoluteBoxesMatchChrome(t *testing.T) {
	cases := loadPosRandom(t)
	if len(cases) < 200 {
		t.Fatalf("%d cases", len(cases))
	}
	bad := 0
	for _, c := range cases {
		page := "@page { size: 794px 1123px; margin: 0 } body { margin: 0 }"
		pages := newRun(t, fmt.Sprintf(`<div style="display:flow-root;width:%gpx">`, c.W)+c.HTML+`</div>`, opts{extra: page, css: c.CSS}).all(t)
		var msgs []string
		for _, key := range []string{"p", "c", "n"} {
			want := c.M[key]
			f := findAny(pages, key)
			if len(want) != 4 {
				continue
			}
			if f == nil {
				msgs = append(msgs, key+": no box")
				continue
			}
			got := [4]float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H}
			for i := range got {
				if math.Abs(got[i]-want[i]) > 0.5 {
					msgs = append(msgs, fmt.Sprintf("%s [x y w h] = %.2f, Chrome %.2f", key, got, want))
					break
				}
			}
		}
		_, known := posRandomKnownDiff[c.Name]
		switch {
		case len(msgs) > 0 && known:
			bad++
		case len(msgs) > 0:
			bad++
			t.Errorf("%s (w %g): %s\n%s | %s", c.Name, c.W, strings.Join(msgs, "; "), c.CSS, c.HTML)
		case known:
			t.Errorf("%s now matches Chrome: remove it from posRandomKnownDiff", c.Name)
		}
	}
	t.Logf("%d of %d random cases differ from Chrome", bad, len(cases))
}
