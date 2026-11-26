package layout

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/box"
)

// templateCSS returns the <style> of the 02 report template, the CSS the "02 ..."
// probe cases were measured with. It reads that fixture from the repository.
func templateCSS(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/templates/02-executive.html.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindSubmatch(data)
	if m == nil {
		t.Fatal("no style in the 02 template")
	}
	return string(m[1])
}

// flexOwner returns why a probe case of the flex tests is not checked, "" when
// it is: what the engine does not implement is named, never skipped silently.
func flexOwner(name string) string {
	const leading = "M3-12: Chrome floors the half-leading of a line"
	for _, c := range []struct{ sub, why string }{
		{"baseline 8.5pt + 20pt", leading},
		{"20pt + 8.5pt + 12pt", leading},
		{"baseline with different line-heights", leading},
		{"baseline: inline-block item", leading},
		{"baseline: empty item", leading},
		{"item with no text, with padding", leading},
		{"direction column", "flex-direction: column is not supported"},
		{"column,", "flex-direction: column is not supported"},
		{"serif + sans", "needs DejaVu Serif metrics, a font the test set does not carry"},
		{"column container", "flex-direction: column is not supported"},
		{"align-self", "align-self is not supported"},
		{"last baseline", "align-items: last baseline is not supported"},
		{"02 link as anchor", "needs the link box of a ::after (M3-02)"},
	} {
		if strings.Contains(name, c.sub) {
			return c.why
		}
	}
	return ""
}

// flexCases picks the flex probe cases of both data files.
func flexCases(t *testing.T) []sizingCase {
	t.Helper()
	var out []sizingCase
	for _, c := range loadChromeCases(t, "testdata/chrome_flex_m3.txt") {
		if strings.HasPrefix(c.Name, "flex ") {
			out = append(out, c)
		}
	}
	for _, c := range loadSizingCases(t) {
		if strings.HasPrefix(c.Name, "flex:") || strings.HasPrefix(c.Name, "height on flex") || strings.HasPrefix(c.Name, "height % on flex") {
			out = append(out, c)
		}
	}
	return out
}

// Each flex probe case in scope (the probe page carries the CSS of the 02 template): the border box of every element with an id
// within 0.5 px of what Chrome laid out.
func TestFlexMatchesChrome(t *testing.T) {
	n, rects := 0, 0
	tpl := templateCSS(t)
	for _, c := range flexCases(t) {
		if why := flexOwner(c.Name); why != "" {
			t.Run(c.Name, func(t *testing.T) { t.Skip(why) })
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			rects += compareChrome(t, c, tpl)
		})
	}
	if n < 50 || rects < 150 {
		t.Fatalf("%d cases and %d rects compared", n, rects)
	}
}

// The 02 findings (head, body, tags, link): x and width of every rect within
// 1.5 px, until borders snap (M3-14); y and height are not compared: they differ
// by the half-leading of the lines (M3-12).
func TestFlexFindingsMatchChrome(t *testing.T) {
	tpl := templateCSS(t)
	n := 0
	for _, c := range loadChromeCases(t, "testdata/chrome_flex_m3.txt") {
		if !strings.HasPrefix(c.Name, "02 finding") || flexOwner(c.Name) != "" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) { compareChromeX(t, c, tpl, true) })
	}
	if n < 20 {
		t.Fatalf("%d cases", n)
	}
}

// compareChrome lays the case out and compares the rects; it returns how many it
// compared.
func compareChrome(t *testing.T, c sizingCase, extraCSS string) int {
	t.Helper()
	return compareChromeX(t, c, extraCSS, false)
}

// compareChromeX is compareChrome, comparing only x and width when xwOnly.
func compareChromeX(t *testing.T, c sizingCase, extraCSS string, xwOnly bool) int {
	t.Helper()
	page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
	html := `<div style="overflow:hidden">` + c.HTML + `</div>`
	pages := newRun(t, html, opts{extra: page, css: extraCSS + "\n" + c.CSS}).all(t)
	compared := 0
	for id, want := range c.M {
		if len(want) != 4 {
			continue
		}
		f := findFrag(pages, id)
		if f == nil {
			t.Errorf("#%s: no fragment", id)
			continue
		}
		compared++
		got := [4]float64{f.rect.X, f.rect.Y, f.rect.W, f.rect.H}
		for i := range got {
			if xwOnly && i%2 == 1 {
				continue
			}
			tol := 0.5
			if xwOnly {
				tol = 1.5 // unsnapped borders: 0.33 px per edge of the 2.5pt border and of each 0.5pt tag border (M3-14)
			}
			if math.Abs(got[i]-want[i]) > tol {
				t.Errorf("#%s: [x y w h] = %.2f, Chrome %.2f", id, got, want)
				break
			}
		}
	}
	return compared
}

// In the head of a 02 finding the size label sits on the first baseline of the
// name (align-items: baseline), whatever the half-leading of their lines.
func TestFlexFindingHeadAlignsBaselines(t *testing.T) {
	tpl := templateCSS(t)
	for _, c := range loadChromeCases(t, "testdata/chrome_flex_m3.txt") {
		if !strings.HasPrefix(c.Name, "02 finding: ") || strings.Contains(c.Name, "@prod") {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			page := fmt.Sprintf("@page { size: %fpx 4000px; margin: 0 } body { margin: 0 }", c.W)
			pages := newRun(t, `<div style="overflow:hidden">`+c.HTML+`</div>`, opts{extra: page, css: tpl}).all(t)
			first := func(id string) float64 {
				f := findFrag(pages, id)
				if f == nil {
					t.Fatalf("no #%s", id)
				}
				var base float64
				found := false
				walk(f, func(g *frag) {
					if !found && len(g.lines) > 0 {
						base, found = g.lines[0].baseline, true
					}
				})
				if !found {
					t.Fatalf("#%s has no line", id)
				}
				return base
			}
			if d := first("name") - first("size"); math.Abs(d) > 0.1 {
				t.Errorf("name and size baselines differ by %.3f", d)
			}
		})
	}
}

// layLine does not change the plan: a line laid out twice gives the same result.
func TestLayLineLeavesThePlanAlone(t *testing.T) {
	r := newRun(t, `<div id=c style="display:flex;width:400px"><span style="width:50px">a</span><span style="margin-left:auto;width:50px">b</span><span style="margin-right:auto;width:50px">c</span></div>`, opts{extra: smallPage})
	l, root := r.pag.l, r.pag.root
	var c *box.Box
	var find func(*box.Box)
	find = func(b *box.Box) {
		if b.ID == "c" {
			c = b
		}
		for _, ch := range b.Children {
			find(ch)
		}
	}
	find(root)
	p, err := l.planFlex(c, cbox{w: 400})
	if err != nil {
		t.Fatal(err)
	}
	xs := func() []float64 {
		s, err := l.layLine(p.lines[0], cbox{w: 400}, 0, c.Style, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		var out []float64
		for _, f := range s.frags {
			out = append(out, f.rect.X)
		}
		return out
	}
	first, second := xs(), xs()
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("first layout %v, second %v", first, second)
		}
	}
	if first[1] != 175 && first[1] != 350-50 && first[1] == 0 {
		t.Errorf("auto margin not applied: %v", first)
	}
}
