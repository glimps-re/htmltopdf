package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// M1-19 conformance: the acceptance bar of design §11.4 on the report corpus,
// rendered with the host-connector options. Page counts follow chrome09 (Chrome at
// the production parameters, un-injected fixtures): 00 small 4, 00 large 38.

// squash removes every white space and zero-width character: the text of
// separate runs touches in the PDF.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\u200b' {
			return -1
		}
		return r
	}, s)
}

// textOf is the visible text nodes of the body and the text of every thead, in
// document order, whitespace removed: style, script, title and template content
// are left out.
type textOf struct {
	strings []string
	heads   []string
}

func collectText(tb testing.TB, html []byte) textOf {
	tb.Helper()
	l := budget.Defaults()
	l[budget.Nodes] = 1 << 22
	d, err := dom.Parse(bytes.NewReader(html), budget.New(context.Background(), l, nil))
	if err != nil {
		tb.Fatal(err)
	}
	var out textOf
	var walk func(ref dom.NodeRef, skip bool, head *strings.Builder)
	walk = func(ref dom.NodeRef, skip bool, head *strings.Builder) {
		for ; ref != dom.NoNode; ref = d.NextSibling(ref) {
			if ti, ok := ref.Text(); ok {
				if s := squash(d.Texts[ti].Data); s != "" && !skip {
					out.strings = append(out.strings, s)
					if head != nil {
						head.WriteString(s)
					}
				}
				continue
			}
			ei, _ := ref.Element()
			e := &d.Elements[ei]
			hidden, h := skip, head
			own := false
			switch e.Tag.String() {
			case "style", "script", "title", "template", "head", "noscript":
				hidden = true
			case "thead":
				if h == nil {
					h, own = &strings.Builder{}, true
				}
			}
			walk(e.FirstChild, hidden, h)
			if own && h.Len() > 0 {
				out.heads = append(out.heads, h.String())
			}
		}
	}
	walk(d.Elements[0].FirstChild, false, nil)
	return out
}

var footerRE = regexp.MustCompile(`TLP:AMBER|Page\d+of\d+`)

// pageStream joins the pages of a document, each without its footer, with a
// NUL between pages: a cell cut at a page bottom continues after the NUL and the
// header group the page repeats.
func pageStream(pages []string) string {
	clean := make([]string, len(pages))
	for i, p := range pages {
		clean[i] = footerRE.ReplaceAllString(squash(p), "")
	}
	return strings.Join(clean, "\x00")
}

// splitWindow is how far from a page break the two halves of a cut cell may lie:
// a row cut at a page bottom paints the first lines of each of its cells at the
// end of the page and the rest at the start of the next one, after the header
// group the page repeats.
const splitWindow = 3000

// minPart is the shortest half of a cut string: shorter ones match by accident.
const minPart = 4

// splitAt reports whether s is cut by the page break at stream[k]: a first part in
// the last splitWindow bytes before it and the rest in the first splitWindow bytes
// after it.
func splitAt(stream string, k int, s string) bool {
	if len(s) < 2*minPart+4 {
		return false // too short: its halves would match by accident
	}
	tail := stream[max(0, k-splitWindow):k]
	head := stream[k+1 : min(len(stream), k+1+splitWindow)]
	for m := len(s) - minPart; m >= minPart; m-- {
		if strings.Contains(tail, s[:m]) && strings.Contains(head, s[m:]) {
			return true
		}
	}
	return false
}

// matchInOrder checks the text of a document against its source, tolerating the
// rows cut by a page break (NUL in stream). Presence: every string occurs as
// often as in want, whole or cut by a break. Order: the strings that occur once
// in want and are long enough to be unambiguous must appear in the same order,
// within splitWindow of slack (the cells of a cut row interleave around the
// break). It returns the strings that are missing and those out of order.
func matchInOrder(want []string, stream string) (absent, misordered []string) {
	var breaks []int
	for k := 0; k < len(stream); k++ {
		if stream[k] == 0 {
			breaks = append(breaks, k)
		}
	}
	mult := map[string]int{}
	for _, s := range want {
		mult[s]++
	}
	seen := map[string]bool{}
	for _, s := range want {
		if seen[s] {
			continue
		}
		seen[s] = true
		have := strings.Count(stream, s)
		for _, k := range breaks {
			if have >= mult[s] {
				break
			}
			if splitAt(stream, k, s) {
				have++
			}
		}
		if have < mult[s] {
			absent = append(absent, s)
		}
	}
	maxPos := 0
	for _, s := range want {
		if mult[s] != 1 || len(s) < 8 || strings.Count(stream, s) > 1 {
			continue
		}
		p := strings.Index(stream, s)
		if p < 0 {
			for _, k := range breaks {
				if splitAt(stream, k, s) {
					p = k
					break
				}
			}
		}
		if p < 0 {
			continue // reported as absent
		}
		if p+splitWindow < maxPos {
			misordered = append(misordered, s)
			continue
		}
		maxPos = max(maxPos, p)
	}
	return absent, misordered
}

func reportOrder(t *testing.T, name string, want textOf, pages []string, assert bool) {
	t.Helper()
	absent, mis := matchInOrder(want.strings, pageStream(pages))
	t.Logf("%s: %d strings: %d absent, %d out of order", name, len(want.strings), len(absent), len(mis))
	if !assert {
		return
	}
	for _, s := range append(absent[:min(3, len(absent))], mis[:min(3, len(mis))]...) {
		t.Errorf("%s: %.70q is absent or out of order", name, s)
	}
	if len(absent)+len(mis) > 0 {
		t.Errorf("%s: %d absent and %d out of order strings", name, len(absent), len(mis))
	}
}

// Every string of the template data is in the PDF text, in document order, even
// when a row is cut at a page bottom (asserted for template 00, the M1 target;
// reported for 01-03, which use grid and flex).
func TestM119TemplateStringsInOrder(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(name+"/"+string(size), func(t *testing.T) {
				html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
				if err != nil {
					t.Fatal(err)
				}
				_, _, doc := renderReport(t, name, size)
				pages := make([]string, len(doc.Pages))
				for i, p := range doc.Pages {
					pages[i] = strings.Join(p.Text, "")
				}
				reportOrder(t, name, collectText(t, html), pages, name == "00-host-connector")
			})
		}
	}
}

// The same test on the Chrome references, whose text was extracted by
// tmp/visual.sh with poppler (CHROME_TEXT_DIR holds <report>.chrome.txt, pages
// separated by form feeds): Chrome must pass the matcher the same way.
func TestM119ChromeReferenceStringsInOrder(t *testing.T) {
	dir := os.Getenv("CHROME_TEXT_DIR")
	if dir == "" {
		t.Skip("CHROME_TEXT_DIR is not set (tmp/visual.sh sets it)")
	}
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		t.Run(string(size), func(t *testing.T) {
			name := testcorpus.FixtureName("00-host-connector", size)
			html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "00-host-connector", size)
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatalf("CHROME_TEXT_DIR=%q is not a usable directory: %v", dir, err)
			}
			defer func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			}()
			f, err := root.Open(name + ".chrome.txt")
			if err != nil {
				t.Fatalf("CHROME_TEXT_DIR=%q has no %s.chrome.txt: %v", dir, name, err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, io.LimitReader(f, 8<<20)); err != nil {
				t.Fatal(err)
			}
			data := buf.Bytes()
			reportOrder(t, "chrome09 "+name, collectText(t, html), strings.Split(string(data), "\f"), true)
		})
	}
}

var pageOfRE = regexp.MustCompile(`Page(\d+)of(\d+)`)

// The footer of every page: TLP:AMBER once, and "Page N of M" with N the page
// number and M the page count.
func TestM119FooterOnEveryPage(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		t.Run(string(size), func(t *testing.T) {
			res, _, doc := renderReport(t, "00-host-connector", size)
			if len(doc.Pages) != res.Pages {
				t.Fatalf("%d pages in the PDF, %d in the Result", len(doc.Pages), res.Pages)
			}
			for i, p := range doc.Pages {
				text := squash(strings.Join(p.Text, ""))
				if n := strings.Count(text, "TLP:AMBER"); n != 1 {
					t.Errorf("page %d: %d TLP:AMBER", i+1, n)
				}
				m := pageOfRE.FindAllStringSubmatch(text, -1)
				if len(m) != 1 || m[0][1] != strconv.Itoa(i+1) || m[0][2] != strconv.Itoa(res.Pages) {
					t.Errorf("page %d: footers %v, want Page %d of %d", i+1, m, i+1, res.Pages)
				}
			}
		})
	}
}

// Acceptance bar: page counts against Chrome at the production parameters.
func TestM119PageCounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     testcorpus.Size
		chrome   int
		tol      float64 // fraction, or absolute pages when abs
		absolute bool
	}{
		{"00-host-connector", testcorpus.Small, 4, 1, true},
		{"00-host-connector", testcorpus.Large, 38, 0.15, false},
	} {
		t.Run(tc.name+"/"+string(tc.size), func(t *testing.T) {
			res, _, _ := renderReport(t, tc.name, tc.size)
			lo, hi := float64(tc.chrome)*(1-tc.tol), float64(tc.chrome)*(1+tc.tol)
			if tc.absolute {
				lo, hi = float64(tc.chrome)-tc.tol, float64(tc.chrome)+tc.tol
			}
			t.Logf("%d pages (Chrome %d, allowed %.1f–%.1f)", res.Pages, tc.chrome, lo, hi)
			if float64(res.Pages) < lo || float64(res.Pages) > hi {
				t.Errorf("%d pages outside %.1f–%.1f", res.Pages, lo, hi)
			}
		})
	}
}

// The PDF of 00 large is at most 400 KiB.
func TestM119OutputSize(t *testing.T) {
	for _, tc := range []struct {
		size  testcorpus.Size
		limit int
	}{{testcorpus.Small, 400 << 10}, {testcorpus.Large, 400 << 10}} {
		res, data, _ := renderReport(t, "00-host-connector", tc.size)
		t.Logf("00 %s: %d bytes, %d pages", tc.size, res.Bytes, res.Pages)
		if int64(len(data)) != res.Bytes || len(data) > tc.limit {
			t.Errorf("00 %s: %d bytes (Result %d), limit %d", tc.size, len(data), res.Bytes, tc.limit)
		}
	}
}

// goldenText compares got with testdata/golden/<name>. It skips while the
// golden is absent (generate it with -update once the text stage is final).
func goldenText(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		t.Skipf("%s does not exist yet: generate it with -update once the geometry of M1-08c/M1-19a is settled (ask the lead)", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from its golden file (rerun with -update and review the diff):\n--- got\n%.3000s\n--- want\n%.3000s", name, got, want)
	}
}

// Goldens of 00 small: the layout dump of every page and the text of every page
// as the PDF gives it back.
func TestM119Golden00Small(t *testing.T) {
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "00-host-connector", testcorpus.Small)
	if err != nil {
		t.Fatal(err)
	}
	c := mustNew(t, referenceOptions(t))
	b := budget.New(context.Background(), c.limits, nil)
	r := &renderer{c: c, b: b, ctx: context.Background()}
	root, _, err := r.build(bytes.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	shaper := text.NewShaper(c.fonts, b)
	pag := layout.NewPaginator(root, layout.Config{Page: r.page, Scale: c.scale, Fonts: c.fonts, Shaper: shaper, Budget: b, KeepTree: true})
	var dump strings.Builder
	for {
		pg, err := pag.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := layout.Dump(&dump, pg); err != nil {
			t.Fatal(err)
		}
	}
	goldenText(t, "00-small.layout.txt", dump.String())

	_, _, doc := renderReport(t, "00-host-connector", testcorpus.Small)
	var text strings.Builder
	for i, p := range doc.Pages {
		fmt.Fprintf(&text, "--- page %d\n", i+1)
		for _, s := range p.Text {
			text.WriteString(s)
			text.WriteByte('\n')
		}
	}
	goldenText(t, "00-small.text.txt", text.String())
}

// The matcher accepts a cut row and still fails on missing or reordered text.
func TestM119MatcherSelfCheck(t *testing.T) {
	filler := strings.Repeat("z", 2*splitWindow)
	stream := pageStream([]string{
		"alphaword1 " + filler + " betaword22 " + filler + " path_first_ MISS TLP:AMBER Page 1 of 2",
		"HEADER path_second ING1 ok " + filler + " gammaword3 " + filler + " deltaword4 Page 2 of 2 TLP:AMBER",
	})
	// "path_first_path_second" is a cell cut by the page break, and "ok" a cell
	// of the same cut row that interleaves.
	for name, tc := range map[string]struct {
		want            []string
		absent, reorder int
	}{
		"in order":            {[]string{"alphaword1", "betaword22", "path_first_path_second", "ok", "gammaword3", "deltaword4"}, 0, 0},
		"cut cell, whole row": {[]string{"betaword22", "path_first_path_second", "gammaword3"}, 0, 0},
		"missing string":      {[]string{"alphaword1", "betaword22", "path_first_path_second", "MISSING", "gammaword3"}, 1, 0},
		"swapped strings":     {[]string{"alphaword1", "gammaword3", "betaword22", "deltaword4"}, 0, 1},
		"lost second half":    {[]string{"alphaword1", "path_first_path_other", "gammaword3"}, 1, 0},
		"short string, fragments around the break": {[]string{"alphaword1", "MISSING1", "gammaword3"}, 1, 0},
		"wrong page order":                         {[]string{"deltaword4", "alphaword1"}, 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			absent, mis := matchInOrder(tc.want, stream)
			if len(absent) != tc.absent || len(mis) != tc.reorder {
				t.Errorf("absent %v out of order %v, want %d and %d", absent, mis, tc.absent, tc.reorder)
			}
		})
	}
}
