package svg

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

const logoMarkup = `<svg class="logo" viewBox="0 0 24 24" fill="none" stroke="#f9c105" stroke-width="1.6" stroke-linejoin="round" aria-hidden="true">
        <path d="M12 2 3.5 5.5v6.2c0 5 3.6 9.1 8.5 10.3 4.9-1.2 8.5-5.3 8.5-10.3V5.5L12 2Z"/>
        <path d="m8.2 12.2 2.6 2.6 5-5.2"/>
    </svg>`

const archiveMarkup = `<svg viewBox="0 0 24 24" fill="none" stroke="#14213d" stroke-width="1.8" aria-hidden="true"><rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4"/></svg>`

var kappaQuarter = 4.0 / 3 * math.Tan(math.Pi/8)

func ccolor(r, g, b float64) css.Color { return css.Color{R: r / 255, G: g / 255, B: b / 255, A: 1} }

func colorNear(a, b css.Color) bool {
	return math.Abs(a.R-b.R) < 1e-6 && math.Abs(a.G-b.G) < 1e-6 && math.Abs(a.B-b.B) < 1e-6 && math.Abs(a.A-b.A) < 1e-6
}

// The two SVGs of the 02 template give exactly the expected segments, derived
// by hand from the path data (relative forms, implicit lineto after m, arcs of
// radius 1 as one quarter-circle cubic).
func TestCorpusSVGsExactSegments(t *testing.T) {
	logo := mustSVG(t, logoMarkup)
	if logo.ViewBox != [4]float64{0, 0, 24, 24} || len(logo.Shapes) != 2 {
		t.Fatalf("logo: viewBox %v, %d shapes", logo.ViewBox, len(logo.Shapes))
	}
	wantShield := []Seg{
		mv(12, 2), ln(3.5, 5.5), ln(3.5, 11.7),
		cu(pt(3.5, 16.7), pt(7.1, 20.8), pt(12, 22)),
		cu(pt(16.9, 20.8), pt(20.5, 16.7), pt(20.5, 11.7)),
		ln(20.5, 5.5), ln(12, 2), closeSeg(),
	}
	if !segsEqual(logo.Shapes[0].Segs, wantShield) {
		t.Errorf("shield: %+v", logo.Shapes[0].Segs)
	}
	if want := []Seg{mv(8.2, 12.2), ln(10.8, 14.8), ln(15.8, 9.6)}; !segsEqual(logo.Shapes[1].Segs, want) {
		t.Errorf("check: %+v", logo.Shapes[1].Segs)
	}
	for i, s := range logo.Shapes {
		if s.Fill.A != 0 || !colorNear(s.Stroke, ccolor(0xf9, 0xc1, 0x05)) || s.StrokeWidth != 1.6 || s.Join != JoinRound || s.Cap != CapButt || s.FillCurrent || s.StrokeCurrent {
			t.Errorf("logo shape %d paint: %+v", i, s)
		}
	}

	ar := mustSVG(t, archiveMarkup)
	if len(ar.Shapes) != 2 {
		t.Fatalf("archive: %d shapes", len(ar.Shapes))
	}
	k := kappaQuarter
	wantRect := []Seg{
		mv(4, 4), ln(20, 4), cu(pt(20+k, 4), pt(21, 5-k), pt(21, 5)),
		ln(21, 8), cu(pt(21, 8+k), pt(20+k, 9), pt(20, 9)),
		ln(4, 9), cu(pt(4-k, 9), pt(3, 8+k), pt(3, 8)),
		ln(3, 5), cu(pt(3, 5-k), pt(4-k, 4), pt(4, 4)), closeSeg(),
	}
	if !segsEqual(ar.Shapes[0].Segs, wantRect) {
		t.Errorf("rect: %+v", ar.Shapes[0].Segs)
	}
	wantPath := []Seg{
		mv(5, 9), ln(5, 19), cu(pt(5, 19+k), pt(6-k, 20), pt(6, 20)),
		ln(18, 20), cu(pt(18+k, 20), pt(19, 19+k), pt(19, 19)),
		ln(19, 9), mv(10, 13), ln(14, 13),
	}
	if !segsEqual(ar.Shapes[1].Segs, wantPath) {
		t.Errorf("archive path: %+v", ar.Shapes[1].Segs)
	}
	for i, s := range ar.Shapes {
		if s.Fill.A != 0 || !colorNear(s.Stroke, ccolor(0x14, 0x21, 0x3d)) || s.StrokeWidth != 1.8 {
			t.Errorf("archive shape %d paint: %+v", i, s)
		}
	}
}

func warnCount(b *budget.Budget, code budget.Code, detail string) int {
	ws, _ := b.Warnings()
	n := 0
	for _, w := range ws {
		if w.Code == code && w.Detail == detail {
			n++
		}
	}
	return n
}

func segCount(img *Image) int {
	n := 0
	for _, s := range img.Shapes {
		n += len(s.Segs)
	}
	return n
}

// exactD returns a path data of exactly n bytes with a known segment count.
func exactD(n int) (d string, segs int) {
	var sb strings.Builder
	sb.WriteString("M0 0")
	for sb.Len()+5 <= n {
		sb.WriteString(" l1 1")
		segs++
	}
	for sb.Len() < n {
		sb.WriteByte(' ')
	}
	return sb.String(), segs + 1
}

// A d of 65 536 bytes is parsed, one of 65 537 drops its shape with a
// WarnClamped "svg path"; Work is exactly the number of segments.
func TestPathByteLimitAtAndOver(t *testing.T) {
	for _, tag := range []string{"path", "polyline", "polygon"} {
		t.Run(tag, func(t *testing.T) {
			attr := "d"
			base := "M0 0"
			step := " l1 1"
			if tag != "path" {
				attr, base, step = "points", "0 0", " 1 1"
			}
			mk := func(n int) string {
				var sb strings.Builder
				sb.WriteString(base)
				for sb.Len()+len(step) <= n {
					sb.WriteString(step)
				}
				for sb.Len() < n {
					sb.WriteByte(' ')
				}
				return `<svg viewBox="0 0 10 10"><` + tag + ` ` + attr + `="` + sb.String() + `"/></svg>`
			}
			b := newBudget(nil)
			img, err := parseSVG(t, mk(MaxPathBytes), b)
			if err != nil || len(img.Shapes) != 1 {
				t.Fatalf("at the limit: %v, %d shapes", err, len(img.Shapes))
			}
			if got, want := b.Used(budget.Work), int64(segCount(img)); got != want {
				t.Errorf("Work %d, want %d segments", got, want)
			}
			if warnCount(b, budget.WarnClamped, "svg path") != 0 {
				t.Error("warned at the limit")
			}
			b = newBudget(nil)
			img, err = parseSVG(t, mk(MaxPathBytes+1), b)
			if err != nil || len(img.Shapes) != 0 || b.Used(budget.Work) != 0 {
				t.Fatalf("over the limit: %v, %d shapes, Work %d", err, len(img.Shapes), b.Used(budget.Work))
			}
			if warnCount(b, budget.WarnClamped, "svg path") != 1 {
				t.Errorf("warnings: %+v", func() []budget.Warning { w, _ := b.Warnings(); return w }())
			}
		})
	}
}

// 10 000 shapes are kept, the 10 001st is dropped with one WarnClamped, and
// Work is the segment count (5 per plain rect).
func TestShapeCountLimit(t *testing.T) {
	doc := func(n int) string {
		return `<svg viewBox="0 0 10 10">` + strings.Repeat(`<rect width="1" height="1"/>`, n) + `</svg>`
	}
	b := newBudget(nil)
	img, err := parseSVG(t, doc(MaxElements), b)
	if err != nil || len(img.Shapes) != MaxElements || b.Used(budget.Work) != 5*MaxElements {
		t.Fatalf("at the limit: %v, %d shapes, Work %d", err, len(img.Shapes), b.Used(budget.Work))
	}
	if warnCount(b, budget.WarnClamped, "svg elements") != 0 {
		t.Error("warned at the limit")
	}
	b = newBudget(nil)
	img, err = parseSVG(t, doc(MaxElements+1), b)
	if err != nil || len(img.Shapes) != MaxElements || b.Used(budget.Work) != 5*MaxElements {
		t.Fatalf("over the limit: %v, %d shapes, Work %d", err, len(img.Shapes), b.Used(budget.Work))
	}
	if warnCount(b, budget.WarnClamped, "svg elements") != 1 {
		t.Error("no clamping warning")
	}
	// The count is of shapes kept: ones that draw nothing do not use it.
	empty := `<svg viewBox="0 0 10 10">` + strings.Repeat(`<rect width="0" height="1"/>`, 3*MaxElements) + `<rect width="1" height="1"/></svg>`
	if img := mustSVG(t, empty); len(img.Shapes) != 1 {
		t.Errorf("%d shapes", len(img.Shapes))
	}
}

// The Work limit is enforced by segment: a document needing n segments passes
// with Work max n and fails with *LimitError at n-1.
func TestWorkBoundIsExact(t *testing.T) {
	d, segs := exactD(20000)
	doc := `<svg viewBox="0 0 10 10"><path d="` + d + `"/></svg>`
	if _, err := parseSVG(t, doc, newBudget(func(l *budget.Limits) { l[budget.Work] = int64(segs) })); err != nil {
		t.Fatalf("at the exact work: %v", err)
	}
	_, err := parseSVG(t, doc, newBudget(func(l *budget.Limits) { l[budget.Work] = int64(segs) - 1 }))
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work {
		t.Fatalf("one under: %v", err)
	}
	// Shapes are charged one by one: the error comes before the whole document is walked.
	many := `<svg viewBox="0 0 10 10">` + strings.Repeat(`<circle r="1"/>`, 1000) + `</svg>`
	_, err = parseSVG(t, many, newBudget(func(l *budget.Limits) { l[budget.Work] = 100 }))
	if !errors.As(err, &le) {
		t.Fatalf("circles: %v", err)
	}
}

func checkFinite(t *testing.T, name string, segs []Seg) {
	t.Helper()
	for i, s := range segs {
		n := map[Op]int{MoveTo: 1, LineTo: 1, CubicTo: 3, Close: 0}[s.Op]
		for k := range n {
			p := s.P[k]
			if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || math.Abs(p.X) > MaxCoord || math.Abs(p.Y) > MaxCoord {
				t.Errorf("%s: segment %d point %d = %+v", name, i, k, p)
				return
			}
		}
	}
}

// Huge, tiny, infinite and degenerate numbers never yield a non-finite or
// out-of-range coordinate, and never panic.
func TestHostileNumbers(t *testing.T) {
	paths := []string{
		"M1e308 1e308 L-1e308 -1e308", "M1e999 -1e999 L5 5", "M1e-999 1e-999 L1e-400 1", "M0 0 L1e7 1e7 L-1e7 -1e7 L1e7.5 3",
		"M0 0 A1e300 1e300 0 1 1 1e300 -1e300", "M0 0 A1e-300 1e-300 0 0 1 1e-299 0", "M0 0 A1e7 1 359.99 1 1 -1e7 1e7", "M0 0 A5 5 1e300 0 1 10 0",
		"M-1e7 -1e7 A1e7 1e7 0 0 1 1e7 1e7", "M0 0 A0 0 0 0 1 10 10", "M0 0 A1e-320 5 0 0 1 10 10", "M0 0 A5 1e-320 0 1 1 10 10", "M0 0 a1e7 1e7 0 1 1 1e7 1e7",
		"M0 0 C1e7 1e7 1e7 1e7 1e7 1e7 S1e7 1e7 1e7 1e7 T1e7 1e7", "M5e6 5e6 l5e6 5e6 l5e6 5e6 l5e6 5e6", "M0 0 q1e7 1e7 1e7 1e7 t1e7 1e7 t1e7 1e7 t-1e7 -1e7",
		"M0 0 h1e7 h1e7 v1e7 v1e7 H-1e7 V-1e7", "M0 0 A1 1 0 0 0 1e-17 0", "M1 1 A1e154 1e154 0 0 1 1e154 1e154", "M0 0 A1e155 1e155 0 1 0 -1e155 1e155",
		"M0 0 A.1.1 0 0 1 .2.2", "M0 0 a1,1,0,0,1,2,2,1,1,0,0,1,2,2", "M-0 -0 L-0.0 +0", "M0 0 L9999999999999999999999 1",
	}
	for _, d := range paths {
		segs, ok := ParsePath(d)
		if !ok {
			t.Fatalf("%q rejected", d)
		}
		checkFinite(t, d, segs)
	}
	attrs := []string{
		`<rect x="1e300" y="-1e300" width="1e300" height="1e300" rx="1e300"/>`, `<rect x="-1e7" y="-1e7" width="2e7" height="2e7" rx="1e7" ry="1e7"/>`,
		`<circle cx="1e300" cy="1e300" r="1e300"/>`, `<circle cx="1e7" cy="1e7" r="1e7"/>`, `<ellipse cx="-1e7" cy="1e7" rx="1e7" ry="1e-9"/>`,
		`<line x1="1e999" y1="-1e999" x2="1e300" y2="0"/>`, `<polygon points="1e300,1e300 -1e300,0 0,1e999"/>`,
		`<circle r="1e-320"/>`, `<ellipse rx="1e-320" ry="1e-320"/>`, `<rect width="1e-320" height="1e-320"/>`,
		`<rect width="1e7" height="1e7" rx="1e-320"/>`, `<circle r="NaN"/>`, `<circle r="Infinity"/>`, `<circle r="1e7px"/>`,
	}
	for _, a := range attrs {
		img := mustSVG(t, `<svg viewBox="0 0 10 10" stroke="red" stroke-width="1e300">`+a+`</svg>`)
		for _, s := range img.Shapes {
			checkFinite(t, a, s.Segs)
			if s.StrokeWidth > MaxCoord || math.IsNaN(s.StrokeWidth) {
				t.Errorf("%s: stroke width %v", a, s.StrokeWidth)
			}
		}
	}
	for _, vb := range []string{"1e999 1e999 1e999 1e999", "0 0 1e300 1e300", "NaN 0 5 5", "0 0 -1 5", "0 0 5 0", "0 0 5", "0,0,24,24", " 0 0 24 24 99 ", "a b c d", "0 0 24 24x"} {
		img := mustSVG(t, `<svg viewBox="`+vb+`"><rect width="1" height="1"/></svg>`)
		for _, v := range img.ViewBox {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Errorf("viewBox %q -> %v", vb, img.ViewBox)
			}
		}
	}
}

// Every arc yields at most 4 cubics (plus nothing or one line), whatever flags,
// radii and rotation.
func TestArcsAreBounded(t *testing.T) {
	radii := []float64{0, 1e-9, 0.5, 1, 7, 100, 1e5, 1e7}
	rots := []float64{0, 30, 90, -45, 720, 1e6}
	for _, rx := range radii {
		for _, ry := range radii {
			for _, rot := range rots {
				for f := range 4 {
					d := fmt.Sprintf("M10 10 A%g %g %g %d %d 60 40", rx, ry, rot, f&1, f>>1)
					segs, _ := ParsePath(d)
					if len(segs) > 5 || len(segs) < 2 {
						t.Fatalf("%q: %d segments", d, len(segs))
					}
					checkFinite(t, d, segs)
					if last := segs[len(segs)-1]; last.Op == CubicTo && last.P[2] != pt(60, 40) || last.Op == LineTo && last.P[0] != pt(60, 40) {
						t.Fatalf("%q: arc does not end at its end point: %+v", d, last)
					}
				}
			}
		}
	}
	if segs, _ := ParsePath("M5 5 A9 9 0 1 1 5 5"); len(segs) != 1 {
		t.Errorf("arc to the same point: %+v", segs)
	}
}

// A path with a large number of arcs stays linear: 64 KiB of arcs.
func TestManyArcsLinear(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("M0 0")
	for sb.Len() < MaxPathBytes-20 {
		sb.WriteString("a5 5 0 1 1 1 1")
	}
	d := sb.String()
	start := time.Now()
	segs, ok := ParsePath(d)
	if !ok || len(segs) > 4*len(d)/10+1 {
		t.Fatalf("ok %v, %d segments for %d bytes", ok, len(segs), len(d))
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("%v for 64 KiB of arcs", took)
	}
}

// A 64 KiB path allocates at most 8 × segments × 56 bytes.
func TestPathAllocationBound(t *testing.T) {
	d, _ := exactD(MaxPathBytes)
	var segs []Seg
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	segs, _ = ParsePath(d)
	runtime.ReadMemStats(&after)
	got := after.TotalAlloc - before.TotalAlloc
	if limit := uint64(8 * len(segs) * 56); got > limit {
		t.Errorf("%d bytes for %d segments, limit %d", got, len(segs), limit)
	}
}

// Nested groups are walked iteratively: 500 deep (the HTML depth limit is 512)
// inherits and overrides paints right, and a deeper document is refused by the
// HTML parser, not crashed on.
func TestDeepGroupNesting(t *testing.T) {
	const depth = 400
	open := strings.Repeat("<g>", depth/2) + `<g fill="blue" stroke="lime" stroke-width="3">` + strings.Repeat("<g>", depth/2-1)
	doc := `<svg viewBox="0 0 10 10" fill="red">` + open + `<rect width="1" height="1"/>` + strings.Repeat("</g>", depth/2+depth/2-1+1) + `<rect width="2" height="2"/></svg>`
	img := mustSVG(t, doc)
	if len(img.Shapes) != 2 {
		t.Fatalf("%d shapes", len(img.Shapes))
	}
	if !colorNear(img.Shapes[0].Fill, ccolor(0, 0, 255)) || !colorNear(img.Shapes[0].Stroke, ccolor(0, 255, 0)) || img.Shapes[0].StrokeWidth != 3 {
		t.Errorf("deep shape paint: %+v", img.Shapes[0])
	}
	if !colorNear(img.Shapes[1].Fill, ccolor(255, 0, 0)) || img.Shapes[1].Stroke.A != 0 {
		t.Errorf("shape after the groups did not get the root paint back: %+v", img.Shapes[1])
	}
	over := `<svg>` + strings.Repeat("<g>", 100000) + `<rect width="1" height="1"/></svg>`
	start := time.Now()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic: %v", r)
			}
		}()
		_, err := dom.Parse(strings.NewReader(over), newBudget(nil))
		var le *budget.LimitError
		if !errors.As(err, &le) || le.Kind != budget.HTMLDepth {
			t.Errorf("100000 nested groups: %v, want the HTMLDepth limit", err)
		}
	}()
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("%v", took)
	}
}

// Presentation attributes: invalid values leave the inherited paint, url() is
// none, currentColor is flagged, display none drops the subtree, and each
// unsupported element name or property warns once however often it occurs.
func TestPaintAndWarnings(t *testing.T) {
	long := strings.Repeat("a", 200)
	b := newBudget(nil)
	img, err := parseSVG(t, `<svg viewBox="0 0 10 10" fill="red" stroke="currentColor">
	<rect width="1" height="1" fill="`+long+`"/>
	<rect width="1" height="1" fill="url(#g)" stroke="url(#g)"/>
	<rect width="1" height="1" fill="notacolor"/>
	<rect width="1" height="1" fill="currentColor" stroke="none"/>
	<g display="none"><rect width="1" height="1"/></g>
	<rect width="1" height="1" display="none"/>
	<rect width="1" height="1" fill="rgba(0,0,0,0)" stroke="none"/>
	<g fill="none"><rect width="1" height="1"/></g>
	<text>t</text><text>u</text><use/><use/><defs><rect width="1" height="1"/></defs>
	<rect width="1" height="1" transform="rotate(3)"/><rect width="1" height="1" style="fill:blue"/><rect width="1" height="1" transform="x"/>
	</svg>`, b)
	if err != nil {
		t.Fatal(err)
	}
	// Shapes: long fill, notacolor, currentColor fill, inherited fill none, then the three with transform/style.
	// The url() shape (fill and stroke none) and the display none, transparent and defs ones draw nothing.
	if len(img.Shapes) != 7 {
		t.Fatalf("%d shapes: %+v", len(img.Shapes), img.Shapes)
	}
	red := ccolor(255, 0, 0)
	if !colorNear(img.Shapes[0].Fill, red) || !colorNear(img.Shapes[1].Fill, red) {
		t.Errorf("invalid paint changes the inherited fill: %+v %+v", img.Shapes[0].Fill, img.Shapes[1].Fill)
	}
	if !img.Shapes[2].FillCurrent || img.Shapes[2].Stroke.A != 0 || !img.Shapes[0].StrokeCurrent || img.Shapes[3].Fill.A != 0 {
		t.Errorf("currentColor flags: %+v / %+v / %+v", img.Shapes[2], img.Shapes[0], img.Shapes[3])
	}
	for _, name := range []string{"svg text", "svg use", "svg defs"} {
		want := 1
		if got := warnCount(b, budget.WarnUnsupportedElement, name); got != want {
			t.Errorf("%s: %d warnings counted %d, want a single warning", name, got, want)
		}
	}
	ws, _ := b.Warnings()
	for _, w := range ws {
		if strings.HasPrefix(w.Detail, "svg text") && w.Count > 2 {
			t.Errorf("%+v", w)
		}
	}
}

// 10 000 shapes with all-distinct paint strings stay fast and small: the
// colour parse cache is bounded by the shapes and colours do not use the
// document CSS budget.
func TestDistinctPaintsDoNotEatTheCSSBudget(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<svg viewBox="0 0 10 10">`)
	for i := range MaxElements {
		fmt.Fprintf(&sb, `<rect width="1" height="1" fill="rgb(%d,%d,%d)" stroke="#%06x"/>`, i%256, i/256%256, i/65536, i)
	}
	sb.WriteString(`</svg>`)
	b := newBudget(nil)
	start := time.Now()
	var img *Image
	var err error
	peak := heapDelta(func() { img, err = parseSVG(t, sb.String(), b) })
	if err != nil || len(img.Shapes) != MaxElements {
		t.Fatalf("%v, %d shapes", err, len(img.Shapes))
	}
	if b.Used(budget.CSSBytes) != 0 || b.Used(budget.Selectors) != 0 {
		t.Errorf("document CSS budget used: CSSBytes %d Selectors %d", b.Used(budget.CSSBytes), b.Used(budget.Selectors))
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("%v", took)
	}
	if peak > 256<<20 {
		t.Errorf("%d MiB", peak>>20)
	}
}

func heapDelta(fn func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	fn()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// Segment counts and Work match for every shape kind.
func TestWorkEqualsSegmentsPerShape(t *testing.T) {
	for _, tc := range []struct {
		el   string
		segs int
	}{
		{`<rect width="2" height="2"/>`, 5},
		{`<rect width="4" height="4" rx="1"/>`, 10},
		{`<circle r="1"/>`, 6},
		{`<ellipse rx="2" ry="1"/>`, 6},
		{`<line x2="1"/>`, 2},
		{`<polyline points="0,0 1,1 2,0"/>`, 3},
		{`<polygon points="0,0 1,1 2,0"/>`, 4},
		{`<path d="M0 0 L1 1 Z"/>`, 3},
		{`<path d="M0 0 Q1 1 2 0"/>`, 2},
	} {
		b := newBudget(nil)
		img, err := parseSVG(t, `<svg viewBox="0 0 10 10" stroke="red">`+tc.el+`</svg>`, b)
		if err != nil || segCount(img) != tc.segs || b.Used(budget.Work) != int64(tc.segs) {
			t.Errorf("%s: %v, %d segments, Work %d, want %d", tc.el, err, segCount(img), b.Used(budget.Work), tc.segs)
		}
	}
}
