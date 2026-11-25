package htmltopdf

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

var gateCounter = regexp.MustCompile(`(\d+)/(\d+)$`)

// TestTemplate03ReleaseGate asserts the 03 acceptance bar on 03S and
// 03L at once: landscape A4 on every page, every template string present and in
// order, the watermark and the top-right/footer margin boxes on every page,
// the string() header absent (as in Chrome), and the page counter continuous.
func TestTemplate03ReleaseGate(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		t.Run(string(size), func(t *testing.T) {
			res, doc := renderM4(t, size)
			if len(doc.Pages) != res.Pages {
				t.Fatalf("%d pages in the PDF, %d in the Result", len(doc.Pages), res.Pages)
			}
			pages := make([]string, len(doc.Pages))
			for i, p := range doc.Pages {
				pages[i] = strings.Join(p.Text, "")
				w, h := p.MediaBox[2]-p.MediaBox[0], p.MediaBox[3]-p.MediaBox[1]
				if w <= h || math.Abs(w-841.89) > 1 || math.Abs(h-595.28) > 1 {
					t.Errorf("page %d: MediaBox %.2f x %.2f, want A4 landscape 841.89 x 595.28", i+1, w, h)
				}
				text := squash(pages[i])
				if n := strings.Count(text, "TLP:AMBER"); n < 2 {
					t.Errorf("page %d: %d TLP:AMBER (watermark and top-right box expected)", i+1, n)
				}
				if strings.Contains(text, "RAPPORTTECHNIQUE") {
					t.Errorf("page %d: top-left string() header drawn; Chrome omits it", i+1)
				}
				found := false
				for _, a := range cmAngles(string(p.Content)) {
					found = found || math.Abs(math.Abs(a)-18) < 0.01
				}
				if !found {
					t.Errorf("page %d: no 18-degree rotation (watermark missing)", i+1)
				}
				m := gateCounter.FindStringSubmatch(text)
				if m == nil {
					t.Errorf("page %d: no N/M footer counter at the end of the page text", i+1)
					continue
				}
				n, _ := strconv.Atoi(m[1])
				total, _ := strconv.Atoi(m[2])
				if n != i+1 || total != res.Pages {
					t.Errorf("page %d: footer reads %s/%s, want %d/%d", i+1, m[1], m[2], i+1, res.Pages)
				}
			}
			// The template uppercases some texts (text-transform) and the matcher
			// strips TLP:AMBER as a footer: fold the case and leave the watermark
			// to the per-page checks above.
			want := collectText(t, m4HTML(t, size))
			folded := want.strings[:0:0]
			for _, w := range want.strings {
				if w != "TLP:AMBER" {
					folded = append(folded, strings.ToUpper(w))
				}
			}
			want.strings = folded
			for i := range pages {
				pages[i] = strings.ToUpper(pages[i])
			}
			reportOrder(t, "03"+string(size), want, pages, true)
		})
	}
}
