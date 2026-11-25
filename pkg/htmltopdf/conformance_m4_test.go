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

// M4 conformance of template 03 (landscape, two-column sections, counters)
// against chrome09. The paper is the caller's (Options.PageSize), as for 01 and 02;
// the template's own @page size turns it landscape.

// Gates: each flips to true when the task that closes it merges. Every page of
// 03L has its first and last file row within one row of Chrome's, and the idx
// numbers of the blocked-files table equal Chrome's, from M4-01.
const (
	// m4FlowGatesPages: 03L has Chrome's page count (M4-02).
	m4FlowGatesPages = false
	// m4WarningsGate: only string-set and content: ... string() are warned (M4-04).
	m4WarningsGate = false
)

// m4Today is the recorded state (M3 tree, design 11.4 rev. M4): a figure may
// not get worse, the gates above turn them into bars.
const (
	m4TodayPagesL  = 24
	m4TodayPagesS  = 2
	m4ChromePagesL = 23
	m4ChromePagesS = 2
	m4FlowBar      = 1
	m4BlockedPages = 17
)

// m4HTML is the fixture of the real template 03 (landscape, columns).
func m4HTML(tb testing.TB, size testcorpus.Size) []byte {
	tb.Helper()
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), analyst, size)
	if err != nil {
		tb.Fatal(err)
	}
	return html
}

type m4Render struct {
	res *Result
	doc *pdftest.Doc
}

// m4Renders memoises renderM4: the tests of this file do not run in parallel
// and none of them modifies a render.
var m4Renders = map[testcorpus.Size]m4Render{}

// renderM4 renders the real template 03 once per size.
func renderM4(tb testing.TB, size testcorpus.Size) (*Result, *pdftest.Doc) {
	tb.Helper()
	if r, ok := m4Renders[size]; ok {
		return r.res, r.doc
	}
	res, data := renderBytes(tb, mustNew(tb, m2Options(tb)), string(m4HTML(tb, size)))
	r := m4Render{res, pdftest.Validate(tb, data)}
	m4Renders[size] = r
	return r.res, r.doc
}

// m4Chrome03L is, per page of chrome09's 03L (23 pages), the file name and the
// row number (td.idx) of the first and the last file row (pdftotext -raw).
var m4Chrome03L = []struct {
	page                           int
	first, firstIdx, last, lastIdx string
}{
	{1, "invoice_0000", "01", "invoice_0010", "11"},
	{2, "invoice_0011", "12", "invoice_0025", "26"},
	{3, "invoice_0026", "27", "invoice_0040", "41"},
	{4, "invoice_0041", "42", "invoice_0055", "56"},
	{5, "invoice_0056", "57", "invoice_0070", "71"},
	{6, "invoice_0071", "72", "invoice_0085", "86"},
	{7, "invoice_0086", "87", "invoice_0100", "101"},
	{8, "invoice_0101", "102", "invoice_0115", "116"},
	{9, "invoice_0116", "117", "invoice_0130", "131"},
	{10, "invoice_0131", "132", "invoice_0145", "146"},
	{11, "invoice_0146", "147", "invoice_0160", "161"},
	{12, "invoice_0161", "162", "invoice_0175", "176"},
	{13, "invoice_0176", "177", "invoice_0190", "191"},
	{14, "invoice_0191", "192", "invoice_0205", "206"},
	{15, "invoice_0206", "207", "invoice_0220", "221"},
	{16, "invoice_0221", "222", "invoice_0235", "236"},
	{17, "invoice_0236", "237", "invoice_0249", "250"},
	{18, "invoice_0000", "01", "invoice_1001", "05"},
	{19, "invoice_1002", "06", "invoice_3000", "01"},
	{20, "invoice_3001", "05", "invoice_5000", "01"},
	{21, "invoice_5001", "05", "invoice_6003", "07"},
	{22, "invoice_7000", "01", "invoice_8002", "06"},
	{23, "invoice_8003", "07", "invoice_9003", "07"},
}

// m4Number is the td.idx of a row of the blocked-files table; the members of
// an archive carry the template's marker instead.
var m4Number = regexp.MustCompile(`^\d+$`)

var m4Row = regexp.MustCompile(`^C:\\Users\\victim\\Downloads\\(invoice_\d{4})`)

// m4FileRow is a file row of the PDF text: its name, its td.idx text and its page.
type m4FileRow struct {
	name, idx string
	page      int
}

// m4Marker is the td.idx of an archive member: the template draws it in a run of
// its own, before the path. chrome09's pdftotext merges it with the path, so the
// Chrome table (m4Chrome03L) lists the rows of the archive tables only, not their
// members.
const m4Marker = "↳"

// m4Rows lists the file rows of a document in text order, without the members of
// archives. The number of a row is the text run just before its path.
func m4Rows(doc *pdftest.Doc) []m4FileRow {
	var rows []m4FileRow
	for pi, p := range doc.Pages {
		for i, s := range p.Text {
			m := m4Row.FindStringSubmatch(strings.TrimSpace(s))
			if m == nil || i == 0 {
				continue
			}
			idx := strings.TrimSpace(p.Text[i-1])
			if idx == m4Marker {
				continue
			}
			rows = append(rows, m4FileRow{name: m[1], idx: idx, page: pi + 1})
		}
	}
	return rows
}

// m4Offset is, for one page of Chrome's 03L, how many file rows our first and
// last row of the same page are from Chrome's (positive: ours is later). A page
// of ours without file rows, or a Chrome row we do not have, is not ok.
type m4Offset struct {
	page        int
	first, last int
	ok          bool
}

// worst is the larger of the two offsets, math.MaxInt for a page without a match.
func (o m4Offset) worst() int {
	if !o.ok {
		return math.MaxInt
	}
	return max(m4Abs(o.first), m4Abs(o.last))
}

func m4Abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// m4FlowOffsets compares our file rows with chrome09's per page. Names repeat
// across the tables of 03, so the Chrome rows are searched forward in our
// sequence.
func m4FlowOffsets(rows []m4FileRow) []m4Offset {
	find := func(name string, from int) int {
		for i := from; i < len(rows); i++ {
			if rows[i].name == name {
				return i
			}
		}
		return -1
	}
	ours := func(page int) (first, last int) {
		first, last = -1, -1
		for i, r := range rows {
			if r.page == page {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		return first, last
	}
	out := make([]m4Offset, 0, len(m4Chrome03L))
	cur := 0
	for _, c := range m4Chrome03L {
		of, ol := ours(c.page)
		f := find(c.first, cur)
		l := -1
		if f >= 0 {
			l = find(c.last, f)
		}
		if of < 0 || f < 0 || l < 0 {
			out = append(out, m4Offset{page: c.page})
			continue
		}
		cur = l + 1
		out = append(out, m4Offset{page: c.page, first: of - f, last: ol - l, ok: true})
	}
	return out
}

func m4Summary(offs []m4Offset) string {
	var sb strings.Builder
	for _, o := range offs {
		if !o.ok {
			fmt.Fprintf(&sb, " p%d:none", o.page)
			continue
		}
		fmt.Fprintf(&sb, " p%d:%+d/%+d", o.page, o.first, o.last)
	}
	return strings.TrimSpace(sb.String())
}

func TestM4PageCounts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		size          testcorpus.Size
		chrome, today int
	}{
		{"03S", testcorpus.Small, m4ChromePagesS, m4TodayPagesS},
		{"03L", testcorpus.Large, m4ChromePagesL, m4TodayPagesL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := renderM4(t, tc.size)
			t.Logf("%s: %d pages (Chrome %d, recorded %d)", tc.name, res.Pages, tc.chrome, tc.today)
			if m4Abs(res.Pages-tc.chrome) > m4Abs(tc.today-tc.chrome) {
				t.Errorf("%s: %d pages, further from Chrome's %d than the recorded %d", tc.name, res.Pages, tc.chrome, tc.today)
			}
		})
	}
}

// TestM4FlowMatchesChrome gates, per page of 03L, how far our first and last file
// row are from chrome09's: at most one row. The members of the archives are not
// rows (see m4Marker). The page count equals Chrome's from M4-02.
func TestM4FlowMatchesChrome(t *testing.T) {
	if len(m4Chrome03L) != m4ChromePagesL {
		t.Fatalf("%d pages in the Chrome table, want %d", len(m4Chrome03L), m4ChromePagesL)
	}
	res, doc := renderM4(t, testcorpus.Large)
	offs := m4FlowOffsets(m4Rows(doc))
	t.Logf("03L: %d pages (Chrome %d); row offsets first/last per page: %s", res.Pages, m4ChromePagesL, m4Summary(offs))
	for _, o := range offs {
		if o.worst() > m4FlowBar {
			t.Errorf("page %d: offset %+d/%+d rows (math.MaxInt: a page without file rows), bar %d", o.page, o.first, o.last, m4FlowBar)
		}
	}
	if m4FlowGatesPages && res.Pages != m4ChromePagesL {
		t.Errorf("%d pages, Chrome %d", res.Pages, m4ChromePagesL)
	}
}

// TestM4CountersContinuous checks the td.idx numbers of the blocked-files table
// (pages 1-17 of 03L): they run 01, 02, ... without a gap or a restart across
// the page breaks, and the first and last number of each page equal chrome09's.
// The table ends at the first row whose number is not numeric or restarts at 01
// (the next table); the archive tables number their members with the marker the
// template draws and are not compared.
func TestM4CountersContinuous(t *testing.T) {
	_, doc := renderM4(t, testcorpus.Large)
	var blocked []m4FileRow
	for _, r := range m4Rows(doc) {
		if !m4Number.MatchString(r.idx) || (len(blocked) > 0 && r.idx == "01") {
			break
		}
		blocked = append(blocked, r)
	}
	want, err := strconv.Atoi(m4Chrome03L[m4BlockedPages-1].lastIdx)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != want {
		t.Errorf("%d file rows numbered at the start of the document, the blocked table has %d (a restart or a gap of the counter ends it early)", len(blocked), want)
	}
	for i, r := range blocked {
		if num := fmt.Sprintf("%02d", i+1); r.idx != num {
			t.Errorf("row %d (page %d, %s) is numbered %q, want %q", i, r.page, r.name, r.idx, num)
		}
	}
	first, last := map[int]string{}, map[int]string{}
	for _, r := range blocked {
		if _, ok := first[r.page]; !ok {
			first[r.page] = r.idx
		}
		last[r.page] = r.idx
	}
	diff := 0
	for _, c := range m4Chrome03L[:m4BlockedPages] {
		if first[c.page] != c.firstIdx || last[c.page] != c.lastIdx {
			diff++
		}
	}
	if diff > 0 {
		t.Errorf("%d of %d pages with first/last idx numbers different from Chrome's", diff, m4BlockedPages)
	}
}

// TestM4Warnings03 logs the warnings of 03S and 03L; it gates at M4-04, when
// only string-set and content: ... string() may remain.
func TestM4Warnings03(t *testing.T) {
	for _, tc := range []struct {
		name string
		size testcorpus.Size
	}{{"03S", testcorpus.Small}, {"03L", testcorpus.Large}} {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := renderM4(t, tc.size)
			t.Logf("%d pages, %d warnings", res.Pages, len(res.Warnings))
			for _, w := range res.Warnings {
				t.Logf("  %s: %s (x%d)", w.Code, w.Detail, w.Count)
				if m4WarningsGate && !strings.HasPrefix(w.Detail, "string-set") && !strings.Contains(w.Detail, "string(") {
					t.Errorf("unexpected warning %s: %s", w.Code, w.Detail)
				}
			}
		})
	}
}

// BenchmarkM4Render reports the time and the heap-max of rendering the real 03
// (landscape) with the paper-only options.
func BenchmarkM4Render(b *testing.B) {
	for _, tc := range []struct {
		name string
		size testcorpus.Size
	}{{"03S", testcorpus.Small}, {"03L", testcorpus.Large}} {
		html := m4HTML(b, tc.size)
		c := mustNew(b, m2Options(b))
		b.Run(tc.name, func(b *testing.B) {
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
