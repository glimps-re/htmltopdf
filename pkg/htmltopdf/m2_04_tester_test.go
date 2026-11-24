package htmltopdf

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// coverTolerance is the M2-04 criterion 1 tolerance, 0.5 pt raised to 0.7 pt by
// the lead (line-box half-leading, ticket for M3).
const coverTolerance = 0.7

type chromeWords struct {
	PageHeight float64 `json:"page_height"`
	Words      map[string]struct {
		Text        string  `json:"text"`
		X           float64 `json:"x"`
		BaselineTop float64 `json:"baseline_top"`
	} `json:"words"`
}

// M2-04 criterion 1: on page 1 of 01 small, the title, the scan id and the
// verdict of .cover start where chrome09 puts them, within coverTolerance.
// The reference is testdata/chrome/01S_cover_words.json (pdftotext -bbox of
// chrome09, baselines derived from the DejaVu ascent).
func TestM204CoverIsWhereChromeDrawsIt(t *testing.T) {
	data, err := os.ReadFile("../../testdata/chrome/01S_cover_words.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref chromeWords
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	_, _, doc := renderM2(t, "01-modern", testcorpus.Small)
	height := doc.Pages[0].MediaBox[3] - doc.Pages[0].MediaBox[1]
	if math.Abs(height-ref.PageHeight) > 0.1 {
		t.Fatalf("page height %v, Chrome %v", height, ref.PageHeight)
	}
	runs := pageRuns(t, doc, 0)
	for name, w := range ref.Words {
		var found *textRun
		for i := range runs {
			if strings.HasPrefix(strings.TrimSpace(runs[i].text), w.Text) && math.Abs(runs[i].x-w.X) < 40 {
				found = &runs[i]
				break
			}
		}
		if found == nil {
			t.Errorf("%s: no text run starts with %q near x = %.1f", name, w.Text, w.X)
			continue
		}
		dx, dy := found.x-w.X, (height-found.y)-w.BaselineTop
		t.Logf("%s: dx %+.2f dy %+.2f pt", name, dx, dy)
		if math.Abs(dx) > coverTolerance || math.Abs(dy) > coverTolerance {
			t.Errorf("%s: x %.2f baseline %.2f, Chrome %.2f %.2f (dx %+.2f dy %+.2f, tolerance %.1f pt)", name, found.x, height-found.y, w.X, w.BaselineTop, dx, dy, coverTolerance)
		}
	}
}
