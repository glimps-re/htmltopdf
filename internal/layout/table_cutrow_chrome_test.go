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

type cutCase struct {
	Name  string `json:"name"`
	HTML  string `json:"html"`
	CSS   string `json:"css"`
	Pages int    `json:"pages"`
	Frags []struct {
		Page int       `json:"page"`
		H    []float64 `json:"td_visible_h"`
	} `json:"frags"`
}

func loadCutCases(t *testing.T) []cutCase {
	t.Helper()
	f, err := os.Open("testdata/chrome_cutrow.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []cutCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		var c cutCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Every row cut probe of testdata/chrome_cutrow.txt: the same number of pages and
// the height of the cell on each page within 0.5 px (the 1 px sliver that a
// padding leaves on a last page is Chrome's too).
func TestRowCutMatchesChrome(t *testing.T) {
	cases := loadCutCases(t)
	if len(cases) < 20 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			pages := newRun(t, c.HTML, opts{css: c.CSS}).all(t)
			if len(pages) != c.Pages {
				t.Errorf("%d pages, Chrome %d", len(pages), c.Pages)
			}
			for _, want := range c.Frags {
				if want.Page < 1 || want.Page > len(pages) {
					continue
				}
				var got []float64
				walk(pages[want.Page-1].root, func(f *frag) {
					if f.box.Tag.String() == "td" {
						got = append(got, f.rect.H)
					}
				})
				if fmt.Sprint(len(got)) != fmt.Sprint(len(want.H)) {
					t.Errorf("page %d: %d cell fragments %v, Chrome %v", want.Page, len(got), got, want.H)
					continue
				}
				for i := range got {
					if math.Abs(got[i]-want.H[i]) > 0.5 {
						t.Errorf("page %d: cell fragment %.2f tall, Chrome %.2f", want.Page, got[i], want.H[i])
					}
				}
			}
		})
	}
}

// A break-inside: avoid row taller than a page that does not start one moves to
// the next page first, then is cut there: page 1 holds no fragment of it, the
// next pages 600 and 401 px (Chrome, "tr break-inside:avoid" of
// chrome_cutrow.txt, here also checked on the whole text and the cut rows).
func TestTallAvoidRowMovesThenCuts(t *testing.T) {
	cell := strings.Repeat("x<br>", 8) + "x"
	html := `<div class=b>a</div><div class=b>b</div><table style="border-spacing:0;border-collapse:separate"><tr style="break-inside:avoid"><td class=b id=tall>` + cell + `</td></tr></table>`
	pages := newRun(t, html, opts{css: "@page{size:800px 600px;margin:0} body,p{margin:0} .b{line-height:100px;font-size:10px}"}).all(t)
	if len(pages) != 3 {
		t.Fatalf("%d pages", len(pages))
	}
	if n := len(frags(pages[:1], "tr")); n != 0 {
		t.Errorf("%d row fragments on page 1", n)
	}
	lines := 0
	for _, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Tag.String() == "td" {
				lines += len(f.lines)
			}
		})
	}
	if lines != 9 {
		t.Errorf("%d cell lines, want 9", lines)
	}
}
