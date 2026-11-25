package svg

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

var (
	black  = css.Color{A: 1}
	yellow = css.Color{R: 0xf9 / 255.0, G: 0xc1 / 255.0, B: 0x05 / 255.0, A: 1}
)

func TestCorpusLogo(t *testing.T) {
	img := mustSVG(t, `<svg class="logo" viewBox="0 0 24 24" fill="none" stroke="#f9c105" stroke-width="1.6" stroke-linejoin="round" aria-hidden="true">
        <path d="M12 2 3.5 5.5v6.2c0 5 3.6 9.1 8.5 10.3 4.9-1.2 8.5-5.3 8.5-10.3V5.5L12 2Z"/>
        <path d="m8.2 12.2 2.6 2.6 5-5.2"/>
    </svg>`)
	if img.ViewBox != [4]float64{0, 0, 24, 24} {
		t.Errorf("viewBox %v", img.ViewBox)
	}
	if len(img.Shapes) != 2 {
		t.Fatalf("%d shapes", len(img.Shapes))
	}
	want := [][]Seg{
		{
			mv(12, 2), ln(3.5, 5.5), ln(3.5, 11.7),
			cu(pt(3.5, 16.7), pt(7.1, 20.8), pt(12, 22)),
			cu(pt(16.9, 20.8), pt(20.5, 16.7), pt(20.5, 11.7)),
			ln(20.5, 5.5), ln(12, 2), closeSeg(),
		},
		{mv(8.2, 12.2), ln(10.8, 14.8), ln(15.8, 9.6)},
	}
	for i, sh := range img.Shapes {
		if !segsEqual(sh.Segs, want[i]) {
			t.Errorf("shape %d:\n got %+v\nwant %+v", i, sh.Segs, want[i])
		}
		if sh.Fill.A != 0 || sh.Stroke != yellow || sh.StrokeWidth != 1.6 || sh.Join != JoinRound || sh.Cap != CapButt {
			t.Errorf("shape %d paint: %+v", i, sh)
		}
	}
}

func TestCorpusIcon(t *testing.T) {
	img := mustSVG(t, `<svg viewBox="0 0 24 24" fill="none" stroke="#14213d" stroke-width="1.8" aria-hidden="true"><rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4"/></svg>`)
	if len(img.Shapes) != 2 {
		t.Fatalf("%d shapes", len(img.Shapes))
	}
	rect := []Seg{
		mv(4, 4), ln(20, 4), cu(pt(20+kappa, 4), pt(21, 5-kappa), pt(21, 5)),
		ln(21, 8), cu(pt(21, 8+kappa), pt(20+kappa, 9), pt(20, 9)),
		ln(4, 9), cu(pt(4-kappa, 9), pt(3, 8+kappa), pt(3, 8)),
		ln(3, 5), cu(pt(3, 5-kappa), pt(4-kappa, 4), pt(4, 4)), closeSeg(),
	}
	if !segsEqual(img.Shapes[0].Segs, rect) {
		t.Errorf("rect:\n got %+v\nwant %+v", img.Shapes[0].Segs, rect)
	}
	path := []Seg{
		mv(5, 9), ln(5, 19), cu(pt(5, 19+kappa), pt(6-kappa, 20), pt(6, 20)),
		ln(18, 20), cu(pt(18+kappa, 20), pt(19, 19+kappa), pt(19, 19)),
		ln(19, 9), mv(10, 13), ln(14, 13),
	}
	if !segsEqual(img.Shapes[1].Segs, path) {
		t.Errorf("path:\n got %+v\nwant %+v", img.Shapes[1].Segs, path)
	}
}

func TestBasicShapes(t *testing.T) {
	tests := []struct {
		name, el string
		want     []Seg
	}{
		{"rect", `<rect x="1" y="2" width="3" height="4"/>`, []Seg{mv(1, 2), ln(4, 2), ln(4, 6), ln(1, 6), closeSeg()}},
		{"rect without size", `<rect x="1" y="2" width="0" height="4"/>`, nil},
		{"rect negative size", `<rect width="-1" height="4"/>`, nil},
		{"rect radius clamped to half the side", `<rect width="10" height="4" rx="50"/>`, []Seg{
			mv(5, 0), ln(5, 0), cu(pt(5+5*kappa, 0), pt(10, 2-2*kappa), pt(10, 2)), ln(10, 2),
			cu(pt(10, 2+2*kappa), pt(5+5*kappa, 4), pt(5, 4)), ln(5, 4),
			cu(pt(5-5*kappa, 4), pt(0, 2+2*kappa), pt(0, 2)), ln(0, 2),
			cu(pt(0, 2-2*kappa), pt(5-5*kappa, 0), pt(5, 0)), closeSeg(),
		}},
		{"line", `<line x1="1" y1="2" x2="3" y2="4"/>`, []Seg{mv(1, 2), ln(3, 4)}},
		{"polyline", `<polyline points="0,0 10,0 10,10"/>`, []Seg{mv(0, 0), ln(10, 0), ln(10, 10)}},
		{"polygon", `<polygon points="0 0 10 0 10 10"/>`, []Seg{mv(0, 0), ln(10, 0), ln(10, 10), closeSeg()}},
		{"polyline odd count", `<polyline points="0 0 10 0 10"/>`, []Seg{mv(0, 0), ln(10, 0)}},
		{"polyline of one point", `<polyline points="1 1"/>`, nil},
		{"polyline with a syntax error draws nothing", `<polyline points="0 0 1 1 x 2 2"/>`, nil},
		{"circle without radius", `<circle cx="5" cy="5"/>`, nil},
		{"ellipse with a zero radius", `<ellipse rx="0" ry="3"/>`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := mustSVG(t, `<svg>`+tt.el+`</svg>`)
			if tt.want == nil {
				if len(img.Shapes) != 0 {
					t.Errorf("%+v", img.Shapes)
				}
				return
			}
			if len(img.Shapes) != 1 || !segsEqual(img.Shapes[0].Segs, tt.want) {
				t.Errorf("got %+v\nwant %+v", img.Shapes, tt.want)
			}
		})
	}
}

func TestCircleAndEllipse(t *testing.T) {
	img := mustSVG(t, `<svg><circle cx="10" cy="20" r="5"/><ellipse cx="0" cy="0" rx="4" ry="2"/></svg>`)
	if len(img.Shapes) != 2 {
		t.Fatalf("%d shapes", len(img.Shapes))
	}
	circle := []Seg{
		mv(15, 20),
		cu(pt(15, 20+5*kappa), pt(10+5*kappa, 25), pt(10, 25)),
		cu(pt(10-5*kappa, 25), pt(5, 20+5*kappa), pt(5, 20)),
		cu(pt(5, 20-5*kappa), pt(10-5*kappa, 15), pt(10, 15)),
		cu(pt(10+5*kappa, 15), pt(15, 20-5*kappa), pt(15, 20)),
		closeSeg(),
	}
	if !segsEqual(img.Shapes[0].Segs, circle) {
		t.Errorf("circle:\n got %+v\nwant %+v", img.Shapes[0].Segs, circle)
	}
	e := img.Shapes[1].Segs
	if len(e) != 6 || e[1].P[2] != pt(0, 2) || e[2].P[2] != pt(-4, 0) {
		t.Errorf("ellipse: %+v", e)
	}
}

func TestPresentationInheritance(t *testing.T) {
	img := mustSVG(t, `<svg fill="red" stroke="blue" stroke-width="3" stroke-linecap="round" fill-rule="evenodd">
		<path d="M0 0 L1 1"/>
		<g fill="#00f" stroke-linejoin="bevel"><path d="M0 0 L2 2" stroke-width="5"/><g stroke="none"><path d="M0 0 L3 3" fill="currentColor"/></g></g>
		<path d="M0 0 L4 4" fill="nope" stroke-width="-1" stroke-linecap="weird"/>
	</svg>`)
	if len(img.Shapes) != 4 {
		t.Fatalf("%d shapes", len(img.Shapes))
	}
	red, blue := css.Color{R: 1, A: 1}, css.Color{B: 1, A: 1}
	s := img.Shapes
	if s[0].Fill != red || s[0].Stroke != blue || s[0].StrokeWidth != 3 || s[0].Cap != CapRound || !s[0].FillEvenOdd || s[0].Join != JoinMiter {
		t.Errorf("shape 0: %+v", s[0])
	}
	if s[1].Fill != blue || s[1].StrokeWidth != 5 || s[1].Join != JoinBevel || s[1].Cap != CapRound {
		t.Errorf("shape 1: %+v", s[1])
	}
	if !s[2].FillCurrent || s[2].Fill.A != 1 || s[2].Stroke.A != 0 || s[2].StrokeCurrent {
		t.Errorf("shape 2: %+v", s[2])
	}
	if s[3].Fill != red || s[3].StrokeWidth != 3 || s[3].Cap != CapRound {
		t.Errorf("an invalid value must keep the inherited one: %+v", s[3])
	}
}

func TestDefaultsAndPaints(t *testing.T) {
	tests := []struct {
		name, attrs string
		fill        css.Color
		stroke      css.Color
		curFill     bool
		curStroke   bool
	}{
		{"defaults", ``, black, css.Color{}, false, false},
		{"none", `fill="none" stroke="none"`, css.Color{}, css.Color{}, false, false},
		{"currentColor both", `fill="currentColor" stroke="CURRENTCOLOR"`, black, black, true, true},
		{"rgba", `fill="rgba(255,0,0,.5)"`, css.Color{R: 1, A: .5}, css.Color{}, false, false},
		{"url is none", `fill="url(#g)" stroke="url(#g) red"`, css.Color{}, css.Color{}, false, false},
		{"named", `stroke="Yellow"`, black, css.Color{R: 1, G: 1, A: 1}, false, false},
		{"transparent", `fill="transparent" stroke="blue"`, css.Color{}, css.Color{B: 1, A: 1}, false, false},
		{"two declarations are invalid", `fill="red; stroke: blue" stroke="blue"`, black, css.Color{B: 1, A: 1}, false, false},
		{"at the paint limit", `fill="rgb(0,0,0` + strings.Repeat(" ", maxPaintBytes-len("rgb(0,0,0)")) + `)"`, black, css.Color{}, false, false},
		{"over the paint limit is invalid", `fill="rgb(255,0,0` + strings.Repeat(" ", maxPaintBytes-len("rgb(255,0,0)")+1) + `)"`, black, css.Color{}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := mustSVG(t, `<svg><path d="M0 0 L1 1" `+tt.attrs+`/></svg>`)
			if len(img.Shapes) != 1 {
				if tt.fill.A == 0 && tt.stroke.A == 0 {
					return
				}
				t.Fatalf("%d shapes", len(img.Shapes))
			}
			sh := img.Shapes[0]
			if sh.Fill != tt.fill || sh.Stroke != tt.stroke || sh.FillCurrent != tt.curFill || sh.StrokeCurrent != tt.curStroke {
				t.Errorf("%+v", sh)
			}
		})
	}
}

func TestInvisibleShapesAreDropped(t *testing.T) {
	img := mustSVG(t, `<svg><path d="M0 0 L1 1" fill="none"/><path d="M0 0 L1 1" display="none"/><g display="none"><path d="M0 0 L1 1"/></g></svg>`)
	if len(img.Shapes) != 0 {
		t.Errorf("%+v", img.Shapes)
	}
}

func TestViewBox(t *testing.T) {
	tests := map[string][4]float64{
		`viewBox="0 0 24 24"`:     {0, 0, 24, 24},
		`viewBox="-5,-5,10,20"`:   {-5, -5, 10, 20},
		`viewBox="0 0 0 24"`:      {},
		`viewBox="0 0 24 -1"`:     {},
		`viewBox="0 0 24"`:        {},
		`viewBox="a b c d"`:       {},
		`viewBox=""`:              {},
		``:                        {},
		`viewBox="1e30 0 1e30 5"`: {MaxCoord, 0, MaxCoord, 5},
	}
	for attr, want := range tests {
		if got := mustSVG(t, `<svg `+attr+`></svg>`).ViewBox; got != want {
			t.Errorf("%s: %v, want %v", attr, got, want)
		}
	}
}

func TestUnsupportedContentWarns(t *testing.T) {
	b := newBudget(nil)
	img, err := parseSVG(t, `<svg style="x:y"><defs><linearGradient id="g"/></defs><text>hi</text><text>again</text>
		<title>t</title><desc>d</desc><use href="#a"/><path d="M0 0 L1 1" transform="scale(2)"/><foreignObject><div></div></foreignObject></svg>`, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Shapes) != 1 {
		t.Fatalf("%d shapes", len(img.Shapes))
	}
	warns, _ := b.Warnings()
	got := map[string]int{}
	for _, w := range warns {
		got[string(w.Code)+" "+w.Detail] = w.Count
	}
	want := map[string]int{
		"unsupported-element svg defs":          1,
		"unsupported-element svg text":          2,
		"unsupported-element svg use":           1,
		"unsupported-element svg foreignObject": 1,
		"unsupported-property svg style":        1,
		"unsupported-property svg transform":    1,
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%q: %d, want %d (all: %v)", k, got[k], n, got)
		}
	}
}

func manyShapes(n int) string {
	return `<svg>` + strings.Repeat(`<path d="M0 0 L1 1"/>`, n) + `</svg>`
}

func TestMaxElements(t *testing.T) {
	b := newBudget(nil)
	img, err := parseSVG(t, manyShapes(MaxElements), b)
	if err != nil || len(img.Shapes) != MaxElements {
		t.Fatalf("at the limit: %d shapes, %v", len(img.Shapes), err)
	}
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Errorf("warnings at the limit: %+v", w)
	}
	b = newBudget(nil)
	img, err = parseSVG(t, manyShapes(MaxElements+1), b)
	if err != nil || len(img.Shapes) != MaxElements {
		t.Fatalf("over the limit: %d shapes, %v", len(img.Shapes), err)
	}
	w, _ := b.Warnings()
	if len(w) != 1 || w[0].Code != budget.WarnClamped || w[0].Detail != "svg elements" {
		t.Errorf("warnings %+v", w)
	}
}

func TestPathTooLongDropsTheShape(t *testing.T) {
	atLimit := "M0 0 L3 3" + strings.Repeat(" ", MaxPathBytes-len("M0 0 L3 3"))
	b := newBudget(nil)
	img, err := parseSVG(t, `<svg><path d="`+atLimit+`"/><polygon points="0 0 1 1`+strings.Repeat(" ", MaxPathBytes-len("0 0 1 1"))+`"/></svg>`, b)
	if err != nil || len(img.Shapes) != 2 || len(img.Shapes[0].Segs) != 2 {
		t.Fatalf("at the limit: %+v %v", img.Shapes, err)
	}
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Errorf("warnings at the limit: %+v", w)
	}
	b = newBudget(nil)
	img, err = parseSVG(t, `<svg><path d="`+atLimit+` "/><path d="M0 0 L2 2"/><polygon points="0 0 1 1`+strings.Repeat(" ", MaxPathBytes-len("0 0 1 1")+1)+`"/></svg>`, b)
	if err != nil || len(img.Shapes) != 1 || img.Shapes[0].Segs[1].P[0] != pt(2, 2) {
		t.Fatalf("over the limit: %+v %v", img.Shapes, err)
	}
	if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != budget.WarnClamped || w[0].Detail != "svg path" || w[0].Count != 2 {
		t.Errorf("warnings %+v", w)
	}
}

func TestWorkIsTheSegmentCount(t *testing.T) {
	b := newBudget(nil)
	img, err := parseSVG(t, `<svg><path d="M0 0 L1 1 L2 2"/><rect width="2" height="2"/><circle r="1"/></svg>`, b)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, sh := range img.Shapes {
		n += len(sh.Segs)
	}
	if got := b.Used(budget.Work); got != int64(n) || n != 3+5+6 {
		t.Errorf("Work %d for %d segments", got, n)
	}
}

func TestWorkLimitStopsTheParse(t *testing.T) {
	for _, tc := range []struct {
		limit int64
		fails bool
	}{{3 * 10, false}, {3*10 - 1, true}} {
		b := newBudget(func(l *budget.Limits) { l[budget.Work] = tc.limit })
		_, err := parseSVG(t, `<svg>`+strings.Repeat(`<path d="M0 0 L1 1 L2 2"/>`, 10)+`</svg>`, b)
		if (err != nil) != tc.fails {
			t.Errorf("limit %d: %v", tc.limit, err)
		}
	}
}

func TestPaintAttributesDoNotChargeTheCSSBudget(t *testing.T) {
	b := newBudget(func(l *budget.Limits) { l[budget.CSSBytes] = 1 })
	img, err := parseSVG(t, `<svg><path d="M0 0 L1 1" fill="red" stroke="#123"/></svg>`, b)
	if err != nil || len(img.Shapes) != 1 {
		t.Fatalf("%v %+v", err, img)
	}
	if b.Used(budget.CSSBytes) != 0 {
		t.Errorf("%d CSS bytes charged", b.Used(budget.CSSBytes))
	}
}

func TestDeepGroupsInheritFromTheTop(t *testing.T) {
	const depth = 490
	html := `<svg>` + strings.Repeat(`<g fill="red">`, depth) + `<path d="M0 0 L1 1"/>` + strings.Repeat("</g>", depth) + `</svg>`
	img := mustSVG(t, html)
	if len(img.Shapes) != 1 || img.Shapes[0].Fill != (css.Color{R: 1, A: 1}) {
		t.Errorf("%+v", img.Shapes)
	}
}

func TestOtherNamespacesAreSkipped(t *testing.T) {
	img := mustSVG(t, `<svg><math><mi>x</mi></math><path d="M0 0 L1 1"/></svg>`)
	if len(img.Shapes) > 1 {
		t.Errorf("%+v", img.Shapes)
	}
}

func BenchmarkParseIcons(b *testing.B) {
	html := fmt.Sprintf(`<svg viewBox="0 0 24 24" fill="none" stroke="#14213d">%s</svg>`,
		strings.Repeat(`<rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4"/>`, 500))
	b.ReportAllocs()
	for range b.N {
		if _, err := parseSVG(b, html, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDistinctColorsAreAllParsed(t *testing.T) {
	const n = 10000
	var sb strings.Builder
	sb.WriteString("<svg>")
	for i := range n {
		fmt.Fprintf(&sb, `<path d="M0 0 L1 1" fill="#%06x"/>`, i)
	}
	sb.WriteString("</svg>")
	b := newBudget(func(l *budget.Limits) { l[budget.CSSBytes] = 1 })
	img, err := parseSVG(t, sb.String(), b)
	if err != nil || len(img.Shapes) != n {
		t.Fatalf("%d shapes, %v", len(img.Shapes), err)
	}
	for i, sh := range img.Shapes {
		want := css.Color{R: float64(i>>16&0xff) / 255, G: float64(i>>8&0xff) / 255, B: float64(i&0xff) / 255, A: 1}
		if math.Abs(sh.Fill.R-want.R) > 1e-9 || math.Abs(sh.Fill.G-want.G) > 1e-9 || math.Abs(sh.Fill.B-want.B) > 1e-9 || sh.Fill.A != 1 {
			t.Fatalf("shape %d: %+v, want %+v", i, sh.Fill, want)
		}
	}
	if len(NewParser(nil, b).colors) > maxColors {
		t.Error("cache")
	}
}

func polyline(points int) string {
	return `<polyline points="` + strings.Repeat("0,0 ", points) + `"/>`
}

// segmentsDoc has shapes of 16 384 segments in several svg elements.
func segmentsDoc(shapes int, extra string) string {
	var sb strings.Builder
	for i := 0; i < shapes; i += 4 {
		sb.WriteString("<svg>" + strings.Repeat(polyline(MaxPathBytes/4), min(4, shapes-i)) + "</svg>")
	}
	return sb.String() + extra
}

func TestMaxSegmentsPerDocument(t *testing.T) {
	const per = MaxPathBytes / 4
	for _, tc := range []struct {
		name   string
		extra  string
		shapes int
		want   int
		warn   bool
	}{
		{"at the limit", "", MaxSegments / per, MaxSegments / per, false},
		{"one segment over", `<svg><path d="M0 0 L1 1"/></svg>`, MaxSegments / per, MaxSegments / per, true},
		{"a whole shape over", "", MaxSegments/per + 1, MaxSegments / per, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBudget(nil)
			d, err := dom.Parse(strings.NewReader(segmentsDoc(tc.shapes, tc.extra)), newBudget(nil))
			if err != nil {
				t.Fatal(err)
			}
			p := NewParser(d, b)
			got := 0
			for i := range d.Elements {
				if d.Elements[i].NS == dom.NSSVG && elementName(&d.Elements[i]) == "svg" {
					img, err := p.Parse(int32(i))
					if err != nil {
						t.Fatal(err)
					}
					got += len(img.Shapes)
				}
			}
			ws, _ := b.Warnings()
			warned := len(ws) == 1 && ws[0].Detail == "svg segments" && ws[0].Code == budget.WarnClamped
			if got != tc.want || warned != tc.warn || (!tc.warn && len(ws) != 0) {
				t.Errorf("%d shapes (want %d), warnings %+v", got, tc.want, ws)
			}
			if w := b.Used(budget.Work); w > MaxSegments {
				t.Errorf("Work %d", w)
			}
		})
	}
}

func TestParseHeapIsBounded(t *testing.T) {
	big := func(l *budget.Limits) { l[budget.InputBytes], l[budget.Nodes] = 64<<20, 1<<20 }
	d, err := dom.Parse(strings.NewReader(segmentsDoc(200, "")), newBudget(big))
	if err != nil {
		t.Fatal(err)
	}
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	p := NewParser(d, newBudget(big))
	var imgs []*Image
	for i := range d.Elements {
		if d.Elements[i].NS == dom.NSSVG && elementName(&d.Elements[i]) == "svg" {
			img, err := p.Parse(int32(i))
			if err != nil {
				t.Fatal(err)
			}
			imgs = append(imgs, img)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	runtime.KeepAlive(imgs)
	if live := m1.HeapAlloc - m0.HeapAlloc; live > 32<<20 {
		t.Errorf("%d MiB live after parsing %d MiB of shapes", live>>20, 200*MaxPathBytes>>20)
	}
}

func TestChromeParsingDifferences(t *testing.T) {
	if segs, _ := ParsePath("M5. 5 L1 1"); len(segs) != 0 {
		t.Errorf("a number ending in a dot: %+v", segs)
	}
	if segs, _ := ParsePath("M.5 5 L1.5 1"); len(segs) != 2 {
		t.Errorf("%+v", segs)
	}
	for el, want := range map[string]int{
		`<ellipse rx="4"/>`: 6, `<ellipse ry="4"/>`: 6, `<ellipse rx="4" ry="2"/>`: 6, `<ellipse/>`: 0, `<ellipse rx="0"/>`: 0,
		`<polyline points="0 0 1 1 x 2 2"/>`: 0, `<polygon points="0 0 1 1 2"/>`: 4, `<polyline points="0 0 1 1 2,x"/>`: 0,
	} {
		if got := len(mustSVG(t, "<svg>"+el+"</svg>").Shapes); (got != 0) != (want != 0) {
			t.Errorf("%s: %d shapes", el, got)
		}
	}
}
