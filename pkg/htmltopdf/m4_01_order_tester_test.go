package htmltopdf

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

var m4TdLine = regexp.MustCompile(`(?m)^-?[0-9.]+ -?[0-9.]+ Td$`)

// m4RotationOffset is the stream offset of the first cm operator that rotates
// by 18 degrees (the watermark), -1 when there is none.
func m4RotationOffset(content string) int {
	off := 0
	for _, l := range strings.SplitAfter(content, "\n") {
		if f, ok := strings.CutSuffix(strings.TrimSpace(l), " cm"); ok {
			if p := strings.Fields(f); len(p) == 6 {
				a, _ := strconv.ParseFloat(p[0], 64)
				b, _ := strconv.ParseFloat(p[1], 64)
				if math.Abs(math.Abs(math.Atan2(b, a)*180/math.Pi)-18) < 0.01 {
					return off
				}
			}
		}
		off += len(l)
	}
	return -1
}

// The watermark of template 03 (position: fixed, z-index: -1) is painted before
// the table on every page: its 18-degree rotation comes before the first text of
// the table (its header or its first file path) in the content stream.
func TestM4FixedWatermarkIsPaintedBeforeTheTable(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		_, data := renderBytes(t, mustNew(t, m2Options(t)), string(m4HTML(t, size)))
		doc := pdftest.Validate(t, data)
		checked := 0
		for i, p := range doc.Pages {
			content := string(p.Content)
			rot := m4RotationOffset(content)
			if rot < 0 {
				t.Errorf("%s page %d: no 18-degree rotation", size, i+1)
				continue
			}
			tds := m4TdLine.FindAllStringIndex(content, -1)
			for j, s := range p.Text {
				s = strings.TrimSpace(s)
				if j >= len(tds) || (!strings.HasPrefix(s, `C:\Users`) && !strings.EqualFold(s, "Chemin")) {
					continue
				}
				if rot > tds[j][0] {
					t.Errorf("%s page %d: the watermark (offset %d) comes after the table text %q (offset %d)", size, i+1, rot, s, tds[j][0])
				}
				checked++
				break
			}
		}
		if checked < len(doc.Pages)-1 {
			t.Errorf("%s: table text found on %d of %d pages", size, checked, len(doc.Pages))
		}
	}
}
