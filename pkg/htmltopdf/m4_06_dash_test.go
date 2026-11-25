package htmltopdf

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

type dashCase struct{ name, html string }

// dashCases reproduces the documents of the M4 dash probe (dev-only, never Go code),
// with Liberation Sans for the Arial of the flex rows (metrics are the same): one
// box per case in a row of 90 px, with the case's color.
func dashCases() []dashCase {
	var cs []dashCase
	add := func(name, tpl string) { cs = append(cs, dashCase{name, tpl}) }
	for _, st := range []string{"dashed", "dotted"} {
		for _, w := range []string{"0.5pt", "1px", "1.5px", "2px", "3px", "4px", "6px"} {
			add(fmt.Sprintf("%s %s all sides", st, w), fmt.Sprintf(`<div style="margin:6px 20px;width:120px;height:40px;border:%s %s {c}"></div>`, w, st))
		}
		for _, side := range []string{"top", "right", "bottom", "left"} {
			add(fmt.Sprintf("%s 2px %s only", st, side), fmt.Sprintf(`<div style="margin:6px 20px;width:120px;height:40px;border-%s:2px %s {c}"></div>`, side, st))
		}
		add(st+" 2px two sides top+left", fmt.Sprintf(`<div style="margin:6px 20px;width:120px;height:40px;border-top:2px %s {c};border-left:2px %s {c}"></div>`, st, st))
		add(st+" 2px radius 8px", fmt.Sprintf(`<div style="margin:6px 20px;width:120px;height:40px;border:2px %s {c};border-radius:8px"></div>`, st))
		add(st+" 1px radius 50% (circle)", fmt.Sprintf(`<div style="margin:6px 20px;width:60px;height:60px;border:1px %s {c};border-radius:50%%"></div>`, st))
		add(st+" 3px radius 50% (circle)", fmt.Sprintf(`<div style="margin:6px 20px;width:60px;height:60px;border:3px %s {c};border-radius:50%%"></div>`, st))
		add(st+" 2px width 7px (short side)", fmt.Sprintf(`<div style="margin:6px 20px;width:7px;height:40px;border:2px %s {c}"></div>`, st))
		add(st+" 2px collapsed table cell", fmt.Sprintf(`<table style="margin:6px 20px;border-collapse:collapse"><tr><td style="width:60px;height:30px;border:2px %s {c}"></td><td style="width:60px;border:2px %s {c}"></td></tr></table>`, st, st))
		add(st+" 1px collapsed table, one row", fmt.Sprintf(`<table style="margin:6px 20px;border-collapse:collapse"><tr><td style="width:60px;height:30px;border:1px %s {c}"></td></tr><tr><td style="height:30px;border:1px %s {c}"></td></tr></table>`, st, st))
	}
	add("dashed 0.5pt flex item (the 03 h2::after)", `<div style="margin:6px 20px;width:400px;display:flex;align-items:center;gap:3mm;font:700 10pt Liberation Sans"><span>TITRE</span><span style="flex:1;border-bottom:0.5pt dashed {c}"></span></div>`)
	add("dashed 0.5pt flex item narrow", `<div style="margin:6px 20px;width:120px;display:flex;align-items:center;gap:3mm;font:700 10pt Liberation Sans"><span>TITRE</span><span style="flex:1;border-bottom:0.5pt dashed {c}"></span></div>`)
	add("dotted 0.5pt flex item", `<div style="margin:6px 20px;width:400px;display:flex;align-items:center;gap:3mm;font:700 10pt Liberation Sans"><span>TITRE</span><span style="flex:1;border-bottom:0.5pt dotted {c}"></span></div>`)
	return cs
}

func dashColor(i int) string {
	return fmt.Sprintf("#%02x%02x%02x", 11+i*3%220, 17+i*5%220, 29+i*11%220)
}

// Tolerances of the comparison with Chrome: 0.1 px (dash and period) and 0.5 px
// (first dash), in pt at scale 1; the thickness of a decoration within 0.05 px.
const (
	dashTolPt  = 0.1 * 0.75
	startTolPt = 0.5 * 0.75
	decorTolPx = 0.05
	driftMaxPt = 1 * 0.75
)

type chromeDash struct {
	Scale    float64      `json:"scale"`
	Case     string       `json:"case"`
	Page     int          `json:"page"`
	Fill     bool         `json:"fill"`
	BBox     [4]float64   `json:"bbox"`
	Subpaths int          `json:"subpaths"`
	Subs     [][4]float64 `json:"subs"`
}

func loadChromeDash(t testing.TB) []chromeDash {
	t.Helper()
	f, err := os.Open("../../internal/layout/testdata/chrome_dash.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []chromeDash
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), "{") {
			continue
		}
		var r chromeDash
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// stroke is a stroked line of a content stream, in points, y up.
type stroke struct {
	page           int
	color          string
	width, on, off float64
	round          bool
	x0, y0, x1, y1 float64
	height         float64
	closed         bool
	curves         int
}

func parseNum(t testing.TB, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("number %q in the content: %v", s, err)
	}
	return v
}

func hexOf(t testing.TB, r, g, b string) string {
	t.Helper()
	c := func(s string) int { return int(math.Round(parseNum(t, s) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", c(r), c(g), c(b))
}

func parseStrokes(t testing.TB, page int, content []byte, height float64) []stroke {
	t.Helper()
	var out []stroke
	var cur stroke
	var m [2]float64
	var l [2]float64
	closed, curves := false, 0
	cur.page, cur.height = page, height
	for _, ln := range strings.Split(string(content), "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		num := func(i int) float64 { return parseNum(t, strings.Trim(f[i], "[]")) }
		switch f[len(f)-1] {
		case "RG":
			cur.color = hexOf(t, f[0], f[1], f[2])
		case "w":
			cur.width = num(0)
		case "J":
			cur.round = f[0] == "1"
		case "d":
			cur.on, cur.off = 0, 0
			if len(f) >= 4 {
				cur.on, cur.off = num(0), num(1)
			}
		case "m":
			m = [2]float64{num(0), num(1)}
			closed, curves = false, 0
		case "l":
			l = [2]float64{num(0), num(1)}
		case "c":
			curves++
		case "h":
			closed = true
		case "S":
			s := cur
			s.x0, s.y0, s.x1, s.y1 = m[0], m[1], l[0], l[1]
			s.closed, s.curves = closed, curves
			out = append(out, s)
		}
	}
	return out
}

// The dashed and dotted borders of chrome_dash.txt: the dash and the period within
// 0.1 px of Chrome's, the first dash within 0.5 px of the border.
func TestM4DashBordersMatchChrome(t *testing.T) {
	cases := dashCases()
	var html strings.Builder
	cols := map[string]string{}
	for i, c := range cases {
		col := dashColor(i)
		cols[col] = c.name
		fmt.Fprintf(&html, `<div style="height:90px;overflow:visible">%s</div>`, strings.ReplaceAll(c.html, "{c}", col))
	}
	o := referenceOptions(t)
	o.Scale = 1
	o.ExtraCSS = "@page{size:A4;margin:0} body{margin:0}"
	_, data := renderBytes(t, mustNew(t, o), html.String())
	doc := pdftest.Validate(t, data)
	var ours []stroke
	for i, p := range doc.Pages {
		ours = append(ours, parseStrokes(t, i+1, p.Content, p.MediaBox[3])...)
	}
	caseIndex := map[string]int{}
	for i, c := range cases {
		caseIndex[c.name] = i
	}
	drawn := map[int]map[string]bool{}
	for i, p := range doc.Pages {
		drawn[i+1] = map[string]bool{}
		for c := range fillRects(t, p.Content) {
			drawn[i+1][c] = true
		}
		for _, st := range ours {
			if st.page == i+1 {
				drawn[i+1][st.color] = true
			}
		}
	}
	compared := 0
	skipped := map[string]int{}
	waived := map[string]int{}
	drift := map[string]float64{}
	for _, r := range loadChromeDash(t) {
		if r.Scale != 1 || !r.Fill || r.Subpaths < 2 || len(r.Subs) < 2 {
			continue
		}
		if strings.Contains(r.Case, "radius") {
			if checkRoundedDash(t, ours, cols, r) {
				compared++
			}
			continue
		}
		bb := r.BBox
		horizontal := bb[2] > bb[3]
		axis, cross := 0, 1
		if !horizontal {
			axis, cross = 1, 0
		}
		on, period, start := r.Subs[0][2+axis], r.Subs[1][axis]-r.Subs[0][axis], bb[axis]
		round := strings.HasPrefix(r.Case, "dotted") && (strings.Contains(r.Case, " 4px ") || strings.Contains(r.Case, " 6px "))
		flex, short := strings.Contains(r.Case, "flex"), strings.Contains(r.Case, "short side")
		cut := bb[axis] < 0.1 || bb[axis]+bb[2+axis] > 841.8 || bb[cross] < 0.1 || bb[cross]+bb[cross+2] > 841.8
		switch {
		case short:
			// Chrome draws the dots of a 7 px side as corner pieces and a shifted run
			// (docs/css-support.md): not reproduced, but the side is drawn.
			skipped["short side: Chrome's corner pieces"]++
			if !drawn[r.Page][dashColor(caseIndex[r.Case])] {
				t.Errorf("%s page %d: nothing is drawn in its color", r.Case, r.Page)
			}
			continue
		case cut:
			skipped["side cut by the page edge: Chrome records the visible part"]++
			continue
		}
		if round {
			on = 0
		}
		var best *stroke
		bestD := math.Inf(1)
		for i := range ours {
			s := &ours[i]
			if s.color != "" && cols[s.color] != r.Case || s.page != r.Page {
				continue
			}
			sh := (s.y0 == s.y1) == horizontal
			c := s.y0
			if !horizontal {
				c = s.x0
			}
			if horizontal {
				c = s.height - s.y0
			}
			d := math.Abs(c - (bb[cross] + bb[cross+2]/2))
			if d > 1 {
				continue
			}
			if !flex {
				from := math.Min(s.x0, s.x1)
				if !horizontal {
					from = s.height - math.Max(s.y0, s.y1)
				}
				d += math.Abs(from-bb[axis]) / 100
			}
			if sh && d < bestD {
				best, bestD = s, d
			}
		}
		if best == nil || bestD > 1 {
			t.Errorf("%s page %d %v: no stroke (%v)", r.Case, r.Page, bb, bestD)
			continue
		}
		compared++
		if math.Abs(best.on-on) > dashTolPt || math.Abs(best.off+best.on-period) > dashTolPt {
			t.Errorf("%s page %d %v: dash %.3f+%.3f, Chrome %.3f period %.3f", r.Case, r.Page, bb, best.on, best.off, on, period)
		}
		ours0 := math.Min(best.x0, best.x1)
		if !horizontal {
			ours0 = best.height - math.Max(best.y0, best.y1)
		}
		if round {
			ours0 -= best.width / 2
		}
		// The flow height is snapped to whole px by Chrome, which shifts the boxes of
		// the later pages by under 1 px along y: a vertical side is compared with
		// the top of its own box, after the drift measured on its horizontal side
		// (asserted to stay within 1 px).
		key := fmt.Sprintf("%s/%d", r.Case, r.Page)
		if horizontal {
			if _, seen := drift[key]; !seen {
				drift[key] = best.height - best.y0 - (bb[1] + bb[3]/2)
				if math.Abs(drift[key]) > driftMaxPt {
					t.Errorf("%s page %d: the box is %.3f pt from Chrome's along y", r.Case, r.Page, drift[key])
				}
			}
		} else {
			ours0 -= drift[key]
		}
		tol := startTolPt
		if flex {
			waived["flex row: its start depends on the font"]++
		}
		if bb[axis] > 0.1 && !flex && math.Abs(ours0-start) > tol {
			t.Errorf("%s page %d %v: starts at %.3f, Chrome %.3f", r.Case, r.Page, bb, ours0, start)
		}
	}
	// Chrome strokes the sides of a dashed box that is too short for two dashes
	// solid (records without a dash): so does this engine.
	for i, c := range cases {
		if !strings.HasPrefix(c.name, "dashed") || !strings.Contains(c.name, "short side") {
			continue
		}
		for j := range ours {
			if ours[j].color == dashColor(i) && ours[j].on > 0 && ours[j].y0 == ours[j].y1 {
				t.Errorf("%s: a dashed stroke on a side that is too short", c.name)
			}
		}
	}
	if compared < 40 {
		t.Errorf("%d sides compared", compared)
	}
	t.Logf("%d sides compared, skipped %v, period and start waived %v", compared, skipped, waived)
}

type chromeDecor struct {
	Scale     float64 `json:"scale"`
	Font      string  `json:"font"`
	Size      string  `json:"size"`
	Thickness string  `json:"thickness"`
	Underline *struct {
		H float64 `json:"h"`
	} `json:"underline"`
}

func loadChromeDecor(t testing.TB) []chromeDecor {
	t.Helper()
	f, err := os.Open("../../internal/layout/testdata/chrome_decor.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []chromeDecor
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), "{") {
			continue
		}
		var r chromeDecor
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// fillRects returns the height of every rectangle the content fills, by fill color.
func fillRects(t testing.TB, content []byte) map[string][]float64 {
	t.Helper()
	out := map[string][]float64{}
	color, h := "", 0.0
	for _, ln := range strings.Split(string(content), "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		switch f[len(f)-1] {
		case "rg":
			color = hexOf(t, f[0], f[1], f[2])
		case "re":
			h = parseNum(t, f[3])
		case "f":
			out[color] = append(out[color], h)
		}
	}
	return out
}

// The thickness of an underline with an explicit text-decoration-thickness is
// Chrome's, whole px and at least 1, within 0.05 px (chrome_decor.txt).
func TestM4DecorationThicknessMatchesChrome(t *testing.T) {
	fonts := []string{"DejaVu Sans", "DejaVu Sans Mono", "Liberation Sans", "DejaVu Serif"}
	sizes := []string{"8pt", "10pt", "14pt", "30pt"}
	thick := []string{"auto", "0.5pt", "1px", "3px", "from-font", "10%"}
	type row struct{ font, size, th string }
	var rows []row
	for _, f := range fonts {
		for _, s := range sizes {
			for _, th := range thick {
				rows = append(rows, row{f, s, th})
			}
		}
	}
	want := map[string]float64{}
	for _, r := range loadChromeDecor(t) {
		if r.Underline != nil {
			want[fmt.Sprintf("%g/%s/%s/%s", r.Scale, r.Font, r.Size, r.Thickness)] = r.Underline.H
		}
	}
	compared := 0
	for _, scale := range []float64{1, 0.9} {
		var html strings.Builder
		cols := make([]string, len(rows))
		for i, r := range rows {
			cols[i] = fmt.Sprintf("#%02x%02x%02x", 10+i%200, 20+(i*7)%200, 30+(i*13)%200)
			fmt.Fprintf(&html, `<div style="height:60px;margin:0 0 0 20px;font:%s '%s';line-height:60px"><span style="color:%s;text-decoration:underline;text-decoration-thickness:%s;text-decoration-color:%s">Hxg%d</span></div>`, r.size, r.font, cols[i], r.th, cols[i], i)
		}
		o := referenceOptions(t)
		o.Scale = scale
		o.ExtraCSS = "@page{size:A4;margin:0} body{margin:0}"
		_, data := renderBytes(t, mustNew(t, o), html.String())
		doc := pdftest.Validate(t, data)
		got := map[string][]float64{}
		for _, p := range doc.Pages {
			for c, hs := range fillRects(t, p.Content) {
				got[c] = append(got[c], hs...)
			}
		}
		for i, r := range rows {
			if r.th == "auto" || r.th == "from-font" {
				continue
			}
			w, ok := want[fmt.Sprintf("%g/%s/%s/%s", scale, r.font, r.size, r.th)]
			hs := got[cols[i]]
			if !ok || len(hs) != 1 {
				t.Errorf("scale %g %s %s %s: %d rects, Chrome %v", scale, r.font, r.size, r.th, len(hs), ok)
				continue
			}
			compared++
			if math.Abs(hs[0]-w) > decorTolPx*0.75*scale+1e-9 {
				t.Errorf("scale %g %s %s %s: thickness %.3f pt, Chrome %.3f", scale, r.font, r.size, r.th, hs[0], w)
			}
		}
	}
	t.Logf("%d rows compared", compared)
}

// checkRoundedDash compares a dashed or dotted rounded border with Chrome's: the
// period (and the dash on the straight top edge of the rounded box), and a
// number of dashes of the closed path within one of Chrome's.
func checkRoundedDash(t *testing.T, ours []stroke, cols map[string]string, r chromeDash) bool {
	var best *stroke
	for i := range ours {
		s := &ours[i]
		if s.closed && s.page == r.Page && cols[s.color] == r.Case {
			best = s
		}
	}
	if best == nil {
		t.Errorf("%s page %d: no closed stroke", r.Case, r.Page)
		return false
	}
	period := (best.on + best.off) / 0.75
	var perimeter float64
	switch {
	case strings.Contains(r.Case, "1px radius 50%"):
		perimeter = math.Pi * 61
	case strings.Contains(r.Case, "3px radius 50%"):
		perimeter = math.Pi * 63
	default:
		perimeter = 315.98
		on, chromePeriod := r.Subs[0][2], r.Subs[1][0]-r.Subs[0][0]
		if math.Abs(best.on-on) > dashTolPt || math.Abs(best.on+best.off-chromePeriod) > dashTolPt {
			t.Errorf("%s: dash %.3f+%.3f, Chrome %.3f period %.3f", r.Case, best.on, best.off, on, chromePeriod)
		}
		if first := best.x0 + best.on + best.off; math.Abs(first-r.Subs[0][0]) > startTolPt {
			t.Errorf("%s: the second dash starts at %.3f, Chrome's first listed at %.3f", r.Case, first, r.Subs[0][0])
		}
	}
	if n := perimeter / period; math.Abs(n-float64(r.Subpaths)) > 1.1 {
		t.Errorf("%s: %.1f dashes, Chrome %d", r.Case, n, r.Subpaths)
	}
	return true
}

func dashedBoxes(n int, style string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<style>b{position:absolute;display:block;width:40px;height:12px;border:1px %s #123456}</style>`, style)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, `<b style="left:%dpx;top:%dpx"></b>`, i%700, i/700*8)
	}
	return sb.String()
}

// Ten thousand dashed or dotted boxes on a page stay under the content cap and
// grow linearly: the PDF of twice the boxes is about twice as large.
func TestM4ManyDashedBoxesAreLinear(t *testing.T) {
	for _, style := range []string{"dashed", "dotted"} {
		size := func(n int) int64 {
			res, _ := renderBytes(t, mustNew(t, referenceOptions(t)), dashedBoxes(n, style))
			return res.Bytes
		}
		a, b := size(5000), size(10000)
		if b > 16<<20 {
			t.Errorf("%s: %d bytes for 10000 boxes", style, b)
		}
		if ratio := float64(b) / float64(a); ratio < 1.5 || ratio > 2.5 {
			t.Errorf("%s: %d bytes for 5000 boxes, %d for 10000", style, a, b)
		}
	}
}

// A dashed side that is 1e7 px long (or more) is one operator, not millions of
// dashes: the page stays small and valid.
func TestM4DashedHugeSides(t *testing.T) {
	for _, w := range []string{"1e5", "1e7", "1e9", "1e30"} {
		html := `<div style="width:` + w + `px;height:20px;border:2px dashed #123456;border-radius:0"></div><div style="width:` + w + `px;height:20px;border:3px dotted #123456"></div>`
		res, data := renderBytes(t, mustNew(t, referenceOptions(t)), html)
		if res.Bytes > 1<<20 {
			t.Errorf("width %s: %d bytes", w, res.Bytes)
		}
		pdftest.Validate(t, data)
	}
}

// The dashed rule after each h2 of template 03 is stroked with a dash pattern
// on every page that has a heading, in the dash and period Chrome uses for a 1 px
// border (3 px and 5 px at the scale of the reference).
func TestM4Template03DashedRules(t *testing.T) {
	_, data := renderBytes(t, mustNew(t, referenceOptions(t)), string(m4HTML(t, testcorpus.Small)))
	doc := pdftest.Validate(t, data)
	found := 0
	for _, p := range doc.Pages {
		for _, s := range parseStrokes(t, 1, p.Content, p.MediaBox[3]) {
			// 1 px at 0.9: 3 px dashes are 2.025 pt.
			if s.on > 0 && math.Abs(s.on-3*0.675) < 1e-3 && math.Abs(s.width-0.675) < 1e-3 {
				found++
			}
		}
	}
	if found < 3 {
		t.Errorf("%d dashed rules", found)
	}
}

type chromeThick struct {
	Scale     float64 `json:"scale"`
	Kind      string  `json:"kind"`
	Size      string  `json:"size"`
	Thickness string  `json:"thickness"`
	Line      *struct {
		H float64 `json:"h"`
	} `json:"line"`
}

func loadChromeThick(t testing.TB) []chromeThick {
	t.Helper()
	f, err := os.Open("../../internal/layout/testdata/chrome_decor_thick.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []chromeThick
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), "{") {
			continue
		}
		var r chromeThick
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// The thickness of underlines and line-throughs for 0, fractions of a px, em and
// percentages is Chrome's: at least 1 px, rounded to whole px (chrome_decor_thick.txt).
// The underline rows of 30 pt with 1em and 100% are skipped: the probe could not
// measure the band that leaves its row.
func TestM4DecorationThicknessValuesMatchChrome(t *testing.T) {
	kinds := []string{"underline", "line-through"}
	sizes := []string{"8pt", "14pt", "30pt"}
	thick := []string{"0", "0px", "0.2px", "0.4px", "0.6px", "1.5px", "2.5px", "1em", "0.5em", "100%"}
	type row struct{ kind, size, th string }
	var rows []row
	for _, k := range kinds {
		for _, s := range sizes {
			for _, th := range thick {
				rows = append(rows, row{k, s, th})
			}
		}
	}
	want := map[string]float64{}
	for _, r := range loadChromeThick(t) {
		if r.Line != nil {
			want[fmt.Sprintf("%g/%s/%s/%s", r.Scale, r.Kind, r.Size, r.Thickness)] = r.Line.H
		}
	}
	compared := 0
	for _, scale := range []float64{1, 0.9} {
		var html strings.Builder
		cols := make([]string, len(rows))
		for i, r := range rows {
			cols[i] = fmt.Sprintf("#%02x%02x%02x", 10+i%200, 20+(i*7)%200, 30+(i*13)%200)
			fmt.Fprintf(&html, `<div style="height:60px;margin:0 0 0 20px;font:%s 'DejaVu Sans';line-height:60px"><span style="color:%s;text-decoration:%s;text-decoration-thickness:%s;text-decoration-color:%s">Hxg%d</span></div>`, r.size, cols[i], r.kind, r.th, cols[i], i)
		}
		o := referenceOptions(t)
		o.Scale = scale
		o.ExtraCSS = "@page{size:A4;margin:0} body{margin:0}"
		_, data := renderBytes(t, mustNew(t, o), html.String())
		doc := pdftest.Validate(t, data)
		got := map[string][]float64{}
		for _, p := range doc.Pages {
			for c, hs := range fillRects(t, p.Content) {
				got[c] = append(got[c], hs...)
			}
		}
		for i, r := range rows {
			w, ok := want[fmt.Sprintf("%g/%s/%s/%s", scale, r.kind, r.size, r.th)]
			if !ok || r.kind == "underline" && r.size == "30pt" && (r.th == "1em" || r.th == "100%") {
				continue
			}
			hs := got[cols[i]]
			if len(hs) != 1 {
				t.Errorf("scale %g %s %s %s: %d rects", scale, r.kind, r.size, r.th, len(hs))
				continue
			}
			compared++
			if math.Abs(hs[0]-w) > decorTolPx*0.75*scale+1e-9 {
				t.Errorf("scale %g %s %s %s: %.3f pt, Chrome %.3f", scale, r.kind, r.size, r.th, hs[0], w)
			}
		}
	}
	if compared < 100 {
		t.Errorf("%d rows compared", compared)
	}
}
