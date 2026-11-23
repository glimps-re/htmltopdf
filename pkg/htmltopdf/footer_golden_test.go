package htmltopdf

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

// blankPages is a document of n pages that paints nothing: the content stream
// of each page is the footer alone.
func blankPages(t *testing.T, c *Converter, n int) []pdftest.Page {
	t.Helper()
	_, data := renderBytes(t, c, documentOfPages(t, c, n))
	doc := pdftest.Validate(t, data)
	if len(doc.Pages) != n {
		t.Fatalf("%d pages, want %d", len(doc.Pages), n)
	}
	return doc.Pages
}

// The footer of the first and last of 12 pages, in the host-connector
// configuration, is stable: its operators are compared with the golden files.
func TestFooterGoldens(t *testing.T) {
	c := mustNew(t, referenceOptions(t))
	pages := blankPages(t, c, 12)
	for _, n := range []int{1, 12} {
		name := "footer-page-" + strconv.Itoa(n) + "-of-12.txt"
		path := filepath.Join("testdata", "golden", name)
		got := pages[n-1].Content
		if *updateGolden {
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("%v (run with -update)", err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from its golden file:\n--- got\n%s--- want\n%s", name, got, want)
		}
	}
}

var (
	tfRe  = regexp.MustCompile(`/(F[0-9]+) ([0-9.]+) Tf`)
	tjRe  = regexp.MustCompile(`\[([^\]]*)\] TJ`)
	tokRe = regexp.MustCompile(`<([0-9A-Fa-f]+)>|(-?[0-9.]+)`)
)

// textExtent returns the width in pt of the last text block of a page, from the
// widths of its font and the kerning of the TJ array.
func textExtent(t *testing.T, doc *pdftest.Doc, content []byte) float64 {
	t.Helper()
	blocks := bytes.Split(content, []byte("BT"))
	blk := blocks[len(blocks)-1]
	tf := tfRe.FindSubmatch(blk)
	tj := tjRe.FindSubmatch(blk)
	if tf == nil || tj == nil {
		t.Fatalf("no Tf or TJ in %q", blk)
	}
	size, _ := strconv.ParseFloat(string(tf[2]), 64)
	font := doc.Fonts[string(tf[1])]
	if font == nil {
		t.Fatalf("font %s not found", tf[1])
	}
	var w float64
	for _, m := range tokRe.FindAllSubmatch(tj[1], -1) {
		if m[1] != nil {
			for i := 0; i+4 <= len(m[1]); i += 4 {
				gid, _ := strconv.ParseUint(string(m[1][i:i+4]), 16, 16)
				w += float64(font.Widths[uint16(gid)]) / 1000 * size
			}
			continue
		}
		k, _ := strconv.ParseFloat(string(m[2]), 64)
		w -= k / 1000 * size
	}
	return w
}

// Chrome's footer of the production references, tmp/spike/out/chrome09/
// report_00-host-connector_large.pdf (Scale 0.9, 595.92 x 841.92 pt pages,
// 38 pages, one footer layer), is drawn by the footerTemplate: bold sans, TLP:AMBER yellow on a
// white highlight at the left, "Page N of M" right-aligned. Measured with
// pdftotext -bbox, in pt from the page top-left:
//   - TLP:AMBER starts at x = 22.50;
//   - "Page N of M" ends at x = 573.50 and starts at 520.77 for one digit of
//     N, 515.55 for two (the total has two digits: 38, as in our 38-page render);
//   - the text box is y = 808.29 to 817.02 (7.8 pt bold), baseline 815.28 (the box bottom 817.02 minus the 1.74 pt descent of DejaVu Sans Bold at this size).
//
// Both strings must be within 0.1 pt of Chrome's position on every page (the
// page is 595.92 x 841.92 pt in both).
func TestFooterMatchesChrome(t *testing.T) {
	const (
		tlpX         = 22.50
		rightEdge    = 573.50
		startOneDig  = 520.77
		startTwoDig  = 515.55
		chromeBase   = 815.28
		pageHeightPt = 841.92
		tolerance    = 0.1
		pages        = 38
	)
	c := mustNew(t, referenceOptions(t))
	_, data := renderBytes(t, c, documentOfPages(t, c, pages))
	doc := pdftest.Validate(t, data)
	val := func(b []byte) float64 {
		v, err := strconv.ParseFloat(string(b), 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for n := 1; n <= pages; n++ {
		content := doc.Pages[n-1].Content
		xs := tdRe.FindAllSubmatch(content, -1)
		if len(xs) != 2 {
			t.Fatalf("page %d: %d text blocks in the footer, want 2", n, len(xs))
		}
		startWant := startTwoDig
		if n < 10 {
			startWant = startOneDig
		}
		var errs []string
		if x := val(xs[0][1]); math.Abs(x-tlpX) > tolerance {
			errs = append(errs, fmt.Sprintf("TLP:AMBER at x=%.2f, Chrome %.2f", x, tlpX))
		}
		start := val(xs[1][1])
		if math.Abs(start-startWant) > tolerance {
			errs = append(errs, fmt.Sprintf("Page N of M starts at x=%.2f, Chrome %.2f", start, startWant))
		}
		if edge := start + textExtent(t, doc, content); math.Abs(edge-rightEdge) > tolerance {
			errs = append(errs, fmt.Sprintf("Page N of M ends at x=%.2f, Chrome %.2f", edge, rightEdge))
		}
		for i, m := range xs {
			if base := pageHeightPt - val(m[2]); math.Abs(base-chromeBase) > tolerance {
				errs = append(errs, fmt.Sprintf("block %d: baseline %.2f from the top, Chrome %.2f", i, base, chromeBase))
			}
		}
		if len(errs) > 0 {
			t.Errorf("page %d: %s", n, strings.Join(errs, "; "))
			if n >= 3 {
				t.Fatalf("stopping after page %d: same deltas on the other pages", n)
			}
		}
	}
}
