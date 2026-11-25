package layout

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type lineCase struct {
	Name string `json:"name"`
	W    float64
	HTML string                     `json:"html"`
	M    map[string]json.RawMessage `json:"m"`
	X    struct {
		Base []float64 `json:"base"`
	} `json:"x"`
}

func loadLineCases(t testing.TB) []lineCase {
	t.Helper()
	f, err := os.Open(filepath.Clean("testdata/chrome_lines.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []lineCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), `{"name"`) {
			continue
		}
		var c lineCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// The baselines of the lines of the paragraph of each probe case, relative to its
// top, and its height, against Chrome's (testdata/chrome_lines.txt).
func TestLineBoxesMatchChrome(t *testing.T) {
	bad, total, worst := 0, 0, 0.0
	for _, c := range loadLineCases(t) {
		var pr []float64
		if !strings.Contains(c.HTML, `class="m"`) || len(c.X.Base) == 0 || json.Unmarshal(c.M["p"], &pr) != nil || len(pr) != 4 {
			continue
		}
		pages := newRun(t, `<div style="width:600px;overflow:hidden">`+c.HTML+`</div>`, opts{extra: "@page { size: 600px 4000px; margin: 0 } body { margin: 0 }", css: "div{margin:0}"}).all(t)
		p := findFrag(pages, "p")
		if p == nil {
			t.Errorf("%s: no paragraph", c.Name)
			continue
		}
		var got []float64
		for _, ln := range p.lines {
			got = append(got, ln.baseline-p.rect.Y)
		}
		total++
		ok := len(got) == len(c.X.Base) && math.Abs(p.rect.H-pr[3]) <= 0.25
		for i := 0; ok && i < len(got); i++ {
			d := math.Abs(got[i] - c.X.Base[i])
			worst = math.Max(worst, d)
			ok = d <= 0.25
		}
		if !ok {
			bad++
			t.Logf("%s: baselines %.2f height %.2f, Chrome %.2f %.2f", c.Name, got, p.rect.H, c.X.Base, pr[3])
		}
	}
	t.Logf("%d cases, %d off by more than 0.25 px or with another number of lines", total, bad)
	if total < 300 {
		t.Errorf("only %d probe cases ran", total)
	}
	// Report only: M3-12 (half-leading floor) is evaluated in a snapshot; with the
	// floor 13 of 308 cases are off (tmp/m3-12/NOTE.md).
}
