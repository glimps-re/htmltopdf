package htmltopdf

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// h4Watermark finds the first rotated text of content: the line of its rotation cm, the origin of its first
// glyph in pt from the page top-left, and the rotation in degrees (PDF space, y up).
func h4Watermark(content string, pageH float64) (line int, x, y, angle float64, ok bool) {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		f, isCm := strings.CutSuffix(l, " cm")
		m, okCm := h4Numbers(f, 6)
		if !isCm || !okCm || m[1] == 0 || math.Abs(m[0]-m[3]) > 1e-3 || math.Abs(m[1]+m[2]) > 1e-3 {
			continue
		}
		for _, td := range lines[i+1:] {
			t, isTd := strings.CutSuffix(td, " Td")
			if xy, okTd := h4Numbers(t, 2); isTd && okTd {
				return i, m[0]*xy[0] + m[2]*xy[1] + m[4], pageH - (m[1]*xy[0] + m[3]*xy[1] + m[5]), math.Atan2(m[1], m[0]) * 180 / math.Pi, true
			}
		}
	}
	return 0, 0, 0, 0, false
}

func h4Numbers(s string, n int) ([]float64, bool) {
	fields := strings.Fields(s)
	if len(fields) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// The watermark of template 03 on 03S page 1, with the options of the conformance suite, starts where
// chrome09's does (the origin of its first glyph within 0.7 pt) and turns by the same 18 degrees; the
// rotated text is painted before the first table rule.
func TestM4FixedWatermarkMatchesChrome09(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/chrome/03S_watermark.json")
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		OriginPt [2]float64 `json:"origin_pt"`
		Angle    float64    `json:"angle_deg"`
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	_, data := renderBytes(t, mustNew(t, m2Options(t)), string(m4HTML(t, testcorpus.Small)))
	doc := pdftest.Validate(t, data)
	page := doc.Pages[0]
	pageH := page.MediaBox[3] - page.MediaBox[1]
	line, x, y, angle, ok := h4Watermark(string(page.Content), pageH)
	if !ok {
		t.Fatal("no rotated text on page 1")
	}
	if math.Abs(x-want.OriginPt[0]) > 0.7 || math.Abs(y-want.OriginPt[1]) > 0.7 {
		t.Errorf("watermark origin (%.2f, %.2f) pt, chrome09 (%.2f, %.2f)", x, y, want.OriginPt[0], want.OriginPt[1])
	}
	if math.Abs(angle+want.Angle) > 0.05 {
		t.Errorf("watermark turns %.3f degrees, chrome09 %.3f in a y-down page", angle, want.Angle)
	}
	for _, l := range strings.Split(string(page.Content), "\n")[:line] {
		if strings.HasSuffix(l, " re") || strings.HasSuffix(l, " m") || strings.HasSuffix(l, " l") {
			t.Fatalf("%q is painted before the watermark", l)
		}
	}
}
