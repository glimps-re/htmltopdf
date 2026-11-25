package htmltopdf

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// M2 conformance of templates 01-03 against Chrome at the production
// parameters. They are rendered with the paper only (testcorpus.ChromePaper):
// Chrome keeps their own @page margins and margin boxes.

const analyst = "03-analyst"

func m2Options(tb testing.TB) Options {
	tb.Helper()
	o := referenceOptions(tb)
	o.ExtraCSS = ""
	return o
}

// m2HTML is the fixture of a report, as the template produces it (03 is landscape
// from its own @page rule, on the paper of Options.PageSize).
func m2HTML(tb testing.TB, template string, size testcorpus.Size) []byte {
	tb.Helper()
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), template, size)
	if err != nil {
		tb.Fatal(err)
	}
	return html
}

type m2Render struct {
	res  *Result
	data []byte
	doc  *pdftest.Doc
}

type m2Key struct {
	template string
	size     testcorpus.Size
}

// m2Renders memoises renderM2: the tests of this file do not run in parallel
// and none of them modifies a render.
var m2Renders = map[m2Key]m2Render{}

// renderM2 renders a report with the paper-only options, once per report.
func renderM2(tb testing.TB, template string, size testcorpus.Size) (*Result, []byte, *pdftest.Doc) {
	tb.Helper()
	k := m2Key{template, size}
	if r, ok := m2Renders[k]; ok {
		return r.res, r.data, r.doc
	}
	res, data := renderBytes(tb, mustNew(tb, m2Options(tb)), string(m2HTML(tb, template, size)))
	r := m2Render{res, data, pdftest.Validate(tb, data)}
	m2Renders[k] = r
	return r.res, r.data, r.doc
}

// m2Row is one template and size. chrome is the page count of the Chrome
// reference (production parameters). current and textLoss are the ratchet: the
// page count and the number of absent or out-of-order strings recorded at the
// last closure (closure notes); neither may get worse.
type m2Row struct {
	name     string
	template string
	size     testcorpus.Size
	chrome   int
	tol      float64
	absolute bool
	current  int
	textLoss int
	gating   bool
}

// bar returns the allowed page range.
func (r m2Row) bar() (lo, hi float64) {
	if r.absolute {
		return float64(r.chrome) - r.tol, float64(r.chrome) + r.tol
	}
	return float64(r.chrome) * (1 - r.tol), float64(r.chrome) * (1 + r.tol)
}

// excess is how many pages n lies outside the bar: 0 when inside.
func (r m2Row) excess(n int) float64 {
	lo, hi := r.bar()
	return math.Max(0, math.Max(lo-float64(n), float64(n)-hi))
}

// A row may not leave the bar nor move further from it than current; a gating
// row must be inside. Raise a row to gating the first time it is inside the
// bar and tighten current and textLoss at every closure; at the end all six
// rows are gating with no text loss.
//
// textLoss 1 on 03 is not a loss: its body shows "TLP:AMBER", which pageStream
// strips from the stream as a footer.
var m2Rows = []m2Row{
	{name: "01S", template: "01-modern", size: testcorpus.Small, chrome: 3, tol: 1, absolute: true, current: 3, gating: true},
	{name: "01L", template: "01-modern", size: testcorpus.Large, chrome: 21, tol: 0.15, current: 21, gating: true},
	{name: "02S", template: "02-executive", size: testcorpus.Small, chrome: 5, tol: 1, absolute: true, current: 5, gating: true},
	{name: "02L", template: "02-executive", size: testcorpus.Large, chrome: 37, tol: 0.15, current: 37, gating: true},
	{name: "03S", template: analyst, size: testcorpus.Small, chrome: 2, tol: 1, absolute: true, current: 2, textLoss: 1, gating: true},
	{name: "03L", template: analyst, size: testcorpus.Large, chrome: 23, tol: 0.15, current: 23, textLoss: 1, gating: true},
}

func TestM2PageCounts(t *testing.T) {
	for _, r := range m2Rows {
		t.Run(r.name, func(t *testing.T) {
			res, _, _ := renderM2(t, r.template, r.size)
			lo, hi := r.bar()
			ex, base := r.excess(res.Pages), r.excess(r.current)
			t.Logf("%s: %d pages (Chrome %d, bar %.2f-%.2f, recorded %d, %.2f outside)", r.name, res.Pages, r.chrome, lo, hi, r.current, ex)
			switch {
			case r.gating && ex > 0:
				t.Errorf("%s: %d pages, gating row outside the bar %.2f-%.2f (Chrome %d)", r.name, res.Pages, lo, hi, r.chrome)
			case ex > base:
				t.Errorf("%s: %d pages is %.2f outside the bar, further than the recorded %d (%.2f outside); Chrome %d", r.name, res.Pages, ex, r.current, base, r.chrome)
			case ex == 0 && !r.gating:
				t.Logf("%s is inside the bar: make it gating", r.name)
			}
		})
	}
}

func TestM2RowBar(t *testing.T) {
	for _, tc := range []struct {
		row    m2Row
		pages  int
		excess float64
	}{
		{m2Row{chrome: 3, tol: 1, absolute: true}, 2, 0},
		{m2Row{chrome: 3, tol: 1, absolute: true}, 4, 0},
		{m2Row{chrome: 3, tol: 1, absolute: true}, 5, 1},
		{m2Row{chrome: 3, tol: 1, absolute: true}, 1, 1},
		{m2Row{chrome: 37, tol: 0.15}, 32, 0},
		{m2Row{chrome: 37, tol: 0.15}, 31, 0.45},
		{m2Row{chrome: 37, tol: 0.15}, 43, 0.45},
		{m2Row{chrome: 37, tol: 0.15}, 0, 31.45},
	} {
		if got := tc.row.excess(tc.pages); math.Abs(got-tc.excess) > 1e-9 {
			t.Errorf("chrome %d tol %g: excess(%d) = %g, want %g", tc.row.chrome, tc.row.tol, tc.pages, got, tc.excess)
		}
	}
}

var m2FooterRE = regexp.MustCompile(`Page\s*\d+\s*/\s*\d+`)

func m2Pages(doc *pdftest.Doc) []string {
	pages := make([]string, len(doc.Pages))
	for i, p := range doc.Pages {
		pages[i] = m2FooterRE.ReplaceAllString(strings.Join(p.Text, ""), "")
	}
	return pages
}

// Every string of the data is in the PDF text, in document order, at most
// textLoss of them missing. Case is ignored: text-transform changes it (the
// transform has its own tests). reportOrder cannot be used: it only accepts 0.
func TestM2StringsInOrder(t *testing.T) {
	for _, r := range m2Rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, doc := renderM2(t, r.template, r.size)
			want := collectText(t, m2HTML(t, r.template, r.size)).strings
			for i := range want {
				want[i] = strings.ToLower(want[i])
			}
			absent, mis := matchInOrder(want, strings.ToLower(pageStream(m2Pages(doc))))
			t.Logf("%s: %d absent, %d out of order (recorded %d)", r.name, len(absent), len(mis), r.textLoss)
			if n := len(absent) + len(mis); n > r.textLoss {
				t.Errorf("%s: %d absent and %d out of order strings, recorded %d; first: %.70q", r.name, len(absent), len(mis), r.textLoss, append(absent, mis...)[0])
			}
		})
	}
}

var m2PageOfRE = regexp.MustCompile(`Page(\d+)/(\d+)`)

// The footer of 01 is its own margin boxes: "TLP:AMBER" and "Page N / M" on
// every page.
func TestM2FooterOnEveryPage(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		t.Run(string(size), func(t *testing.T) {
			res, _, doc := renderM2(t, "01-modern", size)
			if len(doc.Pages) != res.Pages {
				t.Fatalf("%d pages in the PDF, %d in the Result", len(doc.Pages), res.Pages)
			}
			for i, p := range doc.Pages {
				text := squash(strings.Join(p.Text, ""))
				m := m2PageOfRE.FindAllStringSubmatch(text, -1)
				if n := strings.Count(text, "TLP:AMBER"); n != 1 || len(m) != 1 || m[0][1] != strconv.Itoa(i+1) || m[0][2] != strconv.Itoa(res.Pages) {
					t.Errorf("page %d: %d TLP:AMBER, footers %v, want Page %d / %d", i+1, n, m, i+1, res.Pages)
				}
			}
		})
	}
}

var numRE = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:e-?\d+)?`)

func floats(s string) []float64 {
	var out []float64
	for _, m := range numRE.FindAllString(s, -1) {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

type rgb [3]float64

func (c rgb) near(o rgb, tol float64) bool {
	for i := range c {
		if math.Abs(c[i]-o[i])*255 > tol {
			return false
		}
	}
	return true
}

func hex(v uint32) rgb {
	return rgb{float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255}
}

// stitched is an axial shading function: a stitching function of linear pieces.
type stitched struct {
	c0, c1 []rgb
	bounds []float64
}

var (
	stitchRE = regexp.MustCompile(`/Functions \[(.*?)\] /Bounds \[(.*?)\]`)
	pieceRE  = regexp.MustCompile(`/C0 \[([^\]]*)\] /C1 \[([^\]]*)\]`)
)

// parseStitched reads the first stitching function of a PDF produced by this
// converter (uncompressed dictionaries on one line).
func parseStitched(pdf []byte) (stitched, error) {
	m := stitchRE.FindSubmatch(pdf)
	if m == nil {
		return stitched{}, fmt.Errorf("no stitching function in the PDF")
	}
	var f stitched
	for _, p := range pieceRE.FindAllStringSubmatch(string(m[1]), -1) {
		a, b := floats(p[1]), floats(p[2])
		if len(a) != 3 || len(b) != 3 {
			return stitched{}, fmt.Errorf("piece with %d and %d components, want 3", len(a), len(b))
		}
		f.c0 = append(f.c0, rgb(a))
		f.c1 = append(f.c1, rgb(b))
	}
	f.bounds = floats(string(m[2]))
	if len(f.c0) != len(f.bounds)+1 {
		return stitched{}, fmt.Errorf("%d pieces for %d bounds", len(f.c0), len(f.bounds))
	}
	return f, nil
}

// at is the colour at t in [0, 1].
func (f stitched) at(t float64) rgb {
	t = math.Min(1, math.Max(0, t))
	k, lo := 0, 0.0
	for k < len(f.bounds) && t >= f.bounds[k] {
		lo = f.bounds[k]
		k++
	}
	hi := 1.0
	if k < len(f.bounds) {
		hi = f.bounds[k]
	}
	u := 0.0
	if hi > lo {
		u = (t - lo) / (hi - lo)
	}
	var c rgb
	for i := range c {
		c[i] = f.c0[k][i] + u*(f.c1[k][i]-f.c0[k][i])
	}
	return c
}

// shadedBox returns the bounding box (x0, y0, x1, y1) of the clip path and the
// gradient-line matrix of the first "/Sh1 sh" of a page content stream, where
// each operator is on its own line.
func shadedBox(content []byte) (box [4]float64, cm [6]float64, err error) {
	lines := strings.Split(string(content), "\n")
	sh := -1
	for i, l := range lines {
		if l == "/Sh1 sh" {
			sh = i
			break
		}
	}
	if sh < 2 || !strings.HasSuffix(lines[sh-1], " cm") {
		return box, cm, fmt.Errorf("no gradient painted by /Sh1 sh")
	}
	v := floats(lines[sh-1])
	if len(v) != 6 {
		return box, cm, fmt.Errorf("cm with %d operands", len(v))
	}
	copy(cm[:], v)
	start := sh - 1
	for start > 0 && lines[start] != "q" {
		start--
	}
	box = [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, l := range lines[start:sh] {
		if !strings.HasSuffix(l, " m") && !strings.HasSuffix(l, " l") && !strings.HasSuffix(l, " c") {
			continue
		}
		p := floats(l)
		for i := 0; i+1 < len(p); i += 2 {
			box[0], box[1] = math.Min(box[0], p[i]), math.Min(box[1], p[i+1])
			box[2], box[3] = math.Max(box[2], p[i]), math.Max(box[3], p[i+1])
		}
	}
	if box[0] > box[2] {
		return box, cm, fmt.Errorf("no clip path before /Sh1 sh")
	}
	return box, cm, nil
}

// gradientT is the position of (x, y) on the gradient line of cm (x axis of the
// shading space = the gradient line).
func gradientT(cm [6]float64, x, y float64) float64 {
	vx, vy := cm[0], cm[1]
	return ((x-cm[4])*vx + (y-cm[5])*vy) / (vx*vx + vy*vy)
}

// .cover: linear-gradient(120deg, #e52321 0%, #ea5b0c 45%, #f9c105 100%). The
// start of the gradient line is the top left corner of the box, the end the
// bottom right one, and the corners show the end stops (CSS Images 3 §3.1.1).
func TestM2CoverGradient(t *testing.T) {
	_, data, doc := renderM2(t, "01-modern", testcorpus.Small)
	f, err := parseStitched(data)
	if err != nil {
		t.Fatal(err)
	}
	box, cm, err := shadedBox(doc.Pages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.bounds) != 1 || math.Abs(f.bounds[0]-0.45) > 0.005 {
		t.Fatalf("stops at %v, want one at 0.45", f.bounds)
	}
	const tol = 2.0
	topLeft, bottomRight := gradientT(cm, box[0], box[3]), gradientT(cm, box[2], box[1])
	topRight, bottomLeft := gradientT(cm, box[2], box[3]), gradientT(cm, box[0], box[1])
	if math.Abs(topLeft) > 0.01 || math.Abs(bottomRight-1) > 0.01 {
		t.Errorf("gradient line runs from t=%.3f (top left) to t=%.3f (bottom right), want 0 and 1", topLeft, bottomRight)
	}
	if !(topRight > 0.3 && topRight < 0.95 && bottomLeft > 0.05 && bottomLeft < 0.7) {
		t.Errorf("120deg: top right t=%.3f, bottom left t=%.3f are not both between the corners", topRight, bottomLeft)
	}
	for _, c := range []struct {
		name string
		t    float64
		want uint32
	}{{"top left", topLeft, 0xe52321}, {"45%", f.bounds[0], 0xea5b0c}, {"bottom right", bottomRight, 0xf9c105}} {
		if got := f.at(c.t); !got.near(hex(c.want), tol) {
			t.Errorf("%s: colour %.4f, want #%06x within %g/255", c.name, got, c.want, tol)
		}
	}
}

func TestM2StitchedSelfCheck(t *testing.T) {
	pdf := []byte("<< /Functions [<< /FunctionType 2 /Domain [0 1] /N 1 /C0 [0 0 0] /C1 [1 0.5 0] >> << /FunctionType 2 /Domain [0 1] /N 1 /C0 [1 0.5 0] /C1 [1 1 1] >>] /Bounds [0.5] /Encode [0 1 0 1] >>")
	f, err := parseStitched(pdf)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		t    float64
		want rgb
	}{{-1, rgb{0, 0, 0}}, {0.25, rgb{0.5, 0.25, 0}}, {0.5, rgb{1, 0.5, 0}}, {0.75, rgb{1, 0.75, 0.5}}, {2, rgb{1, 1, 1}}} {
		if got := f.at(tc.t); !got.near(tc.want, 0.01) {
			t.Errorf("at(%g) = %v, want %v", tc.t, got, tc.want)
		}
	}
	for _, bad := range []string{"", "/Functions [] /Bounds [0.5]", "/Functions [/C0 [0 0] /C1 [1 1 1]] /Bounds []"} {
		if _, err := parseStitched([]byte(bad)); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
	if _, _, err := shadedBox([]byte("q\n0 0 m\nQ\n")); err == nil {
		t.Error("content without /Sh1 sh must fail")
	}
	if got := gradientT([6]float64{2, 0, 0, 1, 10, 0}, 11, 5); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("gradientT = %g, want 0.5", got)
	}
}

// fillPaths describes every path of a content stream filled with the colour
// line fill (the line right before the path): its non-degenerate curves (the
// square corners of a rounded-rect path are zero-length curves) and the radius
// of the first.
func fillPaths(content []byte, fill string) []pathInfo {
	lines := bytes.Split(content, []byte("\n"))
	var out []pathInfo
	for i := 0; i < len(lines); i++ {
		if string(lines[i]) != fill || i+1 >= len(lines) || !bytes.HasSuffix(lines[i+1], []byte(" m")) {
			continue
		}
		var prev []float64
		var info pathInfo
		for j := i + 1; j < len(lines); j++ {
			l := string(lines[j])
			if l == "f" {
				out = append(out, info)
				break
			}
			if l == "h" {
				continue
			}
			p := floats(l)
			switch {
			case strings.HasSuffix(l, " m"), strings.HasSuffix(l, " l"):
				prev = p
			case strings.HasSuffix(l, " c") && len(p) == 6:
				end := p[4:6]
				if prev != nil && (math.Abs(end[0]-prev[0]) > 1e-3 || math.Abs(end[1]-prev[1]) > 1e-3) {
					info.curves++
					if info.curves == 1 {
						info.radius = math.Max(math.Abs(end[0]-prev[0]), math.Abs(end[1]-prev[1]))
					}
				}
				prev = end
			}
		}
	}
	return out
}

type pathInfo struct {
	curves int
	radius float64
}

// th { background: var(--ink) } with border-top-left-radius on the first and
// border-top-right-radius on the last: 6px at Scale 0.9 is 4.05 pt, as in the
// Chrome reference (4.05 pt measured on chrome09 01 small).
func TestM2ThTopRadii(t *testing.T) {
	_, _, doc := renderM2(t, "01-modern", testcorpus.Small)
	const ink = "0.1059 0.1216 0.1412 rg"
	var rounded []pathInfo
	for _, p := range doc.Pages {
		for _, info := range fillPaths(p.Content, ink) {
			if info.curves > 0 {
				rounded = append(rounded, info)
			}
		}
	}
	if len(rounded) < 2 {
		t.Fatalf("%d rounded th backgrounds, want the first and last th of a header row", len(rounded))
	}
	for i, info := range rounded {
		if info.curves != 1 || math.Abs(info.radius-4.05) > 0.1 {
			t.Errorf("rounded th %d: %d curves, radius %.3f pt, want 1 curve of 4.05 pt", i, info.curves, info.radius)
		}
	}
}

func TestM2FillPathsSelfCheck(t *testing.T) {
	content := []byte("1 0 0 rg\n0 0 m\n10 0 l\n10 0 10 0 10 0 c\n10 10 l\n5 10 0 8 0 6 c\nh\nf\n1 0 0 rg\n0 0 10 10 re\nf\n")
	got := fillPaths(content, "1 0 0 rg")
	if len(got) != 1 || got[0].curves != 1 || math.Abs(got[0].radius-10) > 1e-9 {
		t.Errorf("fillPaths = %+v, want one path with one non-degenerate curve", got)
	}
}

var caRE = regexp.MustCompile(`/(GT\d+) << /Type /ExtGState /ca ([0-9.]+) /CA ([0-9.]+) >>`)

// .verdict { background: rgba(255, 255, 255, 0.95) }: a white fill painted
// under a translucent ExtGState (ca 0.95, within 0.01), not an opaque one.
func TestM2VerdictTranslucent(t *testing.T) {
	_, data, doc := renderM2(t, "01-modern", testcorpus.Small)
	alpha := map[string]float64{}
	for _, m := range caRE.FindAllSubmatch(data, -1) {
		a, err := strconv.ParseFloat(string(m[2]), 64)
		if err != nil {
			t.Fatal(err)
		}
		alpha[string(m[1])] = a
	}
	lines := strings.Split(string(doc.Pages[0].Content), "\n")
	found := false
	for i := 0; i+2 < len(lines); i++ {
		name, ok := strings.CutSuffix(lines[i], " gs")
		if !ok || lines[i+1] != "1 1 1 rg" || !strings.HasSuffix(lines[i+2], " m") {
			continue
		}
		a, known := alpha[strings.TrimPrefix(name, "/")]
		if !known {
			t.Errorf("%s is not an ExtGState with /ca", name)
			continue
		}
		if a < 1 {
			found = true
			if math.Abs(a-0.95) > 0.01 {
				t.Errorf("verdict fill alpha %.3f, want 0.95 within 0.01", a)
			}
		}
	}
	if !found {
		t.Error("no white fill under a translucent ExtGState on page 1")
	}
}

// BenchmarkM2Render reports the time and the heap-max of rendering 01-03 with
// the paper-only options (the baseline of the time and heap rows of the closure notes).
func BenchmarkM2Render(b *testing.B) {
	for _, r := range m2Rows {
		html := m2HTML(b, r.template, r.size)
		c := mustNew(b, m2Options(b))
		b.Run(r.name, func(b *testing.B) {
			b.ReportAllocs()
			var maxPeak uint64
			for b.Loop() {
				var buf bytes.Buffer
				peak := peakOf(func() {
					if _, err := c.Render(b.Context(), bytes.NewReader(html), &buf); err != nil {
						b.Fatal(err)
					}
				})
				maxPeak = max(maxPeak, peak+uint64(len(html)))
			}
			b.ReportMetric(float64(maxPeak)/(1<<20), "heap-max-MiB")
		})
	}
}

// M2_OUT_DIR=<dir> writes the PDFs of the report corpus the M2 way for the
// side-by-side review: 00 with the host-connector options, 01-03 with the
// paper-only ones (03 as the portrait one-column variant).
func TestM2WriteCorpusPDFs(t *testing.T) {
	dir := os.Getenv("M2_OUT_DIR")
	if dir == "" {
		t.Skip("M2_OUT_DIR is not set")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	write := func(name string, data []byte) {
		if err := root.WriteFile(name+".pdf", data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		_, data, _ := renderReport(t, "00-host-connector", size)
		write(testcorpus.FixtureName("00-host-connector", size), data)
	}
	for _, r := range m2Rows {
		_, data, _ := renderM2(t, r.template, r.size)
		write(testcorpus.FixtureName(r.template, r.size), data)
	}
}
