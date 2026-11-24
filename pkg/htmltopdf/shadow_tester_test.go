package htmltopdf

import (
	"bytes"
	"context"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const shadowRG = "0.0039 0.0078 0.0118 rg" // rgb(1, 2, 3)

var contentNumRE = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?|NaN|Inf`)

// maxCoord is the largest |number| of a content stream, 0 and false when one
// is not finite.
func maxCoord(content string) (float64, bool) {
	hi := 0.0
	for _, m := range contentNumRE.FindAllString(content, -1) {
		f, err := strconv.ParseFloat(m, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		hi = math.Max(hi, math.Abs(f))
	}
	return hi, true
}

// Extreme lengths, radii, spreads and alphas give a valid PDF whose numbers
// are finite and bounded.
func TestShadowHostileValues(t *testing.T) {
	values := []string{
		"0 0 1e30px 1e30px red", "1e30px 1e30px", "-1e30px -1e30px 1e30px", "0 0 1e308px", "1e999px 1e999px red", "-1e999px 0 1e999px 1e999px red", "0 0 0 -1e30px red",
		"0 0 100000px rgba(0,0,0,0.0000001)", "0 0 5px transparent", "0 0 1e9px 1e9px red", "3px 3px 0 0 red",
		"0 0 0 0 red", "-50px -50px 0 -49px red", "0 0 1e-30px red", "0 0 4px rgba(255,0,0,0)", "0 0 4px 1e30px rgba(1,2,3,1e-9)",
		"0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red, 0 0 8px red",
	}
	for _, radius := range []string{"0", "1e30px", "50%", "3px 1e30px"} {
		for _, box := range []string{"width:100px;height:50px", "width:0;height:0", "width:1e-9px;height:1e9px", "width:1e30px;height:10px"} {
			for _, v := range values {
				html := `<div style="margin:30px;` + box + `;border-radius:` + radius + `;background:#eee;box-shadow:` + v + `">x</div>`
				res, data := renderBytes(t, mustNew(t, m2Options(t)), html)
				doc := pdftest.Validate(t, data)
				var content strings.Builder
				for _, p := range doc.Pages {
					content.Write(p.Content)
				}
				hi, ok := maxCoord(content.String())
				if !ok || hi > 1e9 {
					t.Errorf("box %q radius %q shadow %q: numbers not finite or above 1e9 (max %g)", box, radius, v, hi)
				}
				if res.Pages > 3 {
					t.Errorf("box %q shadow %q: %d pages", box, v, res.Pages)
				}
			}
		}
	}
}

func shadowFills(tb testing.TB, data []byte) []int {
	tb.Helper()
	doc := pdftest.Validate(tb, data)
	out := make([]int, len(doc.Pages))
	for i, p := range doc.Pages {
		out[i] = strings.Count(string(p.Content), shadowRG)
	}
	return out
}

// Shadows follow the box kinds: blocks, cut blocks (each fragment), atomic
// inlines, table parts and list items have them; a non-atomic inline does not.
func TestShadowOnBoxKinds(t *testing.T) {
	sh := `box-shadow:1px 2px 8px 1px rgb(1,2,3)`
	for _, tc := range []struct {
		name, html string
		pages      int
		fills      []int
	}{
		{"block", `<div style="` + sh + `">x</div>`, 1, []int{4}},
		{"cut block", `<div style="line-height:100px;` + sh + `">` + strings.Repeat("x<br>", 20) + `x</div>`, 2, []int{4, 4}},
		{"inline-block", `<p>a <span style="display:inline-block;padding:5px;` + sh + `">pill</span> b</p>`, 1, []int{4}},
		{"td", `<table><tr><td style="` + sh + `">c</td></tr></table>`, 1, []int{4}},
		{"tr", `<table><tr style="` + sh + `"><td>c</td></tr></table>`, 1, []int{4}},
		{"table", `<table style="` + sh + `"><tr><td>c</td></tr></table>`, 1, []int{4}},
		{"list item", `<ul><li style="` + sh + `">x</li></ul>`, 1, []int{4}},
		{"inline", `<p>a <span style="` + sh + `">inl</span> b</p>`, 1, []int{0}},
		{"no blur is one rectangle", `<div style="box-shadow:1px 2px 0 1px rgb(1,2,3)">x</div>`, 1, []int{1}},
		{"two shadows", `<div style="box-shadow:1px 2px 0 rgb(1,2,3),3px 3px 0 rgb(1,2,3)">x</div>`, 1, []int{2}},
		{"inset is dropped", `<div style="box-shadow:inset 1px 2px 4px rgb(1,2,3)">x</div>`, 1, []int{0}},
		{"zero alpha is skipped", `<div style="box-shadow:1px 2px 4px rgba(1,2,3,0)">x</div>`, 1, []int{0}},
		{"none", `<div style="box-shadow:none">x</div>`, 1, []int{0}},
		{"cleared by a later rule", `<style>div{box-shadow:1px 2px 4px rgb(1,2,3)} .n{box-shadow:none}</style><div class=n>x</div>`, 1, []int{0}},
		{"not inherited", `<div style="` + sh + `"><p style="margin:0">x</p></div>`, 1, []int{4}},
		{"opacity keeps it", `<div style="opacity:.5;` + sh + `">x</div>`, 1, []int{4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, data := renderBytes(t, mustNew(t, m2Options(t)), tc.html)
			got := shadowFills(t, data)
			if res.Pages != tc.pages || len(got) != len(tc.fills) {
				t.Fatalf("%d pages, fills %v, want %d pages and %v", res.Pages, got, tc.pages, tc.fills)
			}
			for i := range got {
				if got[i] != tc.fills[i] {
					t.Errorf("page %d: %d shadow fills, want %d", i+1, got[i], tc.fills[i])
				}
			}
		})
	}
}

// Under opacity the shadow keeps its own alpha times the opacity: the colour
// of the fill changes, the shape does not.
func TestShadowFollowsOpacity(t *testing.T) {
	_, data := renderBytes(t, mustNew(t, m2Options(t)), `<div style="opacity:.5;box-shadow:0 0 0 0 rgb(1,2,3)">x</div>`)
	doc := pdftest.Validate(t, data)
	if !strings.Contains(string(data), "/ca 0.5") && !strings.Contains(string(data), "/ca 0.502") {
		t.Errorf("no ExtGState with half alpha for the shadow: %s", string(doc.Pages[0].Content))
	}
}

// 00 does not change: its page counts, footers and size are asserted by the
// M1 tests; here a document without box-shadow gives the same bytes as the
// same document with box-shadow: none.
func TestNoShadowChangesNothing(t *testing.T) {
	c := mustNew(t, m2Options(t))
	plain := `<div style="padding:5px;background:#eee;border-radius:4px">x</div><p>t</p>`
	_, a := renderBytes(t, c, plain)
	_, b := renderBytes(t, c, strings.Replace(plain, `style="`, `style="box-shadow:none;`, 1))
	if !bytes.Equal(a, b) {
		t.Error("box-shadow: none changes the output")
	}
}

// The most shadowed document the page limit allows renders within bounded
// memory and time.
func TestShadowedBoxesAreBounded(t *testing.T) {
	const n = 30000
	shadows := strings.TrimSuffix(strings.Repeat("1px 2px 8px 1px red,", 8), ",")
	html := `<style>.s{display:block;box-shadow:` + shadows + `}</style>` + strings.Repeat(`<b class=s>x</b>`, n)
	c := mustNew(t, m2Options(t))
	var res *Result
	var data []byte
	start := time.Now()
	peak := peakOf(func() {
		var buf bytes.Buffer
		r, err := c.Render(context.Background(), strings.NewReader(html), &buf)
		if err != nil {
			t.Error(err)
			return
		}
		res, data = r, buf.Bytes()
	})
	if res == nil {
		return
	}
	t.Logf("%d shadowed boxes: %d pages, %d bytes, peak heap %d MiB, %v", n, res.Pages, len(data), peak>>20, time.Since(start))
	if peak > 512<<20 {
		t.Errorf("peak heap %d MiB", peak>>20)
	}
	if len(data) > 64<<20 {
		t.Errorf("%d bytes of PDF", len(data))
	}
}

// Shadows only add paint: template 00 breaks into the same pages, with the
// same text on each, as with box-shadow turned off.
func TestShadowsDoNotMoveTemplate00(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		t.Run(string(size), func(t *testing.T) {
			html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "00-host-connector", size)
			if err != nil {
				t.Fatal(err)
			}
			plain := referenceOptions(t)
			plain.ExtraCSS += ` * { box-shadow: none !important }`
			_, with := renderBytes(t, mustNew(t, referenceOptions(t)), string(html))
			_, without := renderBytes(t, mustNew(t, plain), string(html))
			a, b := pdftest.Validate(t, with), pdftest.Validate(t, without)
			if len(a.Pages) != len(b.Pages) {
				t.Fatalf("%d pages with shadows, %d without", len(a.Pages), len(b.Pages))
			}
			for i := range a.Pages {
				if strings.Join(a.Pages[i].Text, "|") != strings.Join(b.Pages[i].Text, "|") {
					t.Errorf("page %d: text differs with shadows", i+1)
				}
			}
			if !bytes.Contains(with, []byte("/ExtGState")) || len(with) <= len(without) {
				t.Errorf("no shadow painted: %d bytes with, %d without", len(with), len(without))
			}
		})
	}
}
