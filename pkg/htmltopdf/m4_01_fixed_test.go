package htmltopdf

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// cmAngles returns the rotation angle in degrees of every cm operator of content
// that is a rotation (a = d, b = -c).
func cmAngles(content string) (angles []float64) {
	for _, l := range strings.Split(content, "\n") {
		f, ok := strings.CutSuffix(l, " cm")
		if !ok {
			continue
		}
		parts := strings.Fields(f)
		if len(parts) != 6 {
			continue
		}
		var m [6]float64
		bad := false
		for i, p := range parts {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				bad = true
				break
			}
			m[i] = v
		}
		if bad {
			continue
		}
		if math.Abs(m[0]-m[3]) < 1e-3 && math.Abs(m[1]+m[2]) < 1e-3 && math.Abs(m[0]*m[0]+m[1]*m[1]-1) < 1e-3 && m[1] != 0 {
			angles = append(angles, math.Atan2(m[1], m[0])*180/math.Pi)
		}
	}
	return angles
}

// The watermark of template 03 (fixed, z-index -1, rotate(-18deg)) is on every
// page of 03S and 03L, rotated by 18 degrees, and painted before the table.
func TestM4FixedWatermarkOfTemplate03(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		_, data := renderBytes(t, mustNew(t, referenceOptions(t)), string(m4HTML(t, size)))
		doc := pdftest.Validate(t, data)
		if len(doc.Pages) < 2 {
			t.Fatalf("%s: %d pages", size, len(doc.Pages))
		}
		for i, p := range doc.Pages {
			found := false
			for _, a := range cmAngles(string(p.Content)) {
				found = found || math.Abs(math.Abs(a)-18) < 0.01
			}
			if !found {
				t.Errorf("%s page %d: no 18-degree rotation (the watermark is missing)", size, i+1)
			}
		}
	}
}
