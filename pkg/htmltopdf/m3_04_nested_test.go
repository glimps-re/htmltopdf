package htmltopdf

import (
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func TestNestedPositionedBoxesNeverHideText(t *testing.T) {
	for _, depth := range []int{300, 500} {
		for name, style := range map[string]string{
			"relative": "position:relative;left:1px;top:1px",
			"rotate":   "transform:rotate(1deg)",
		} {
			html := strings.Repeat(`<div style="`+style+`">`, depth) + "MARKERTEXT" + strings.Repeat("</div>", depth)
			res, data := renderBytes(t, mustNew(t, referenceOptions(t)), html)
			doc := pdftest.Validate(t, data)
			text := ""
			for _, p := range doc.Pages {
				text += strings.Join(p.Text, "")
			}
			if !strings.Contains(text, "MARKERTEXT") {
				t.Errorf("%s x %d: the text is absent from the PDF (warnings %+v)", name, depth, res.Warnings)
			}
			if name == "relative" && len(res.Warnings) != 0 {
				t.Errorf("relative x %d: warnings %+v", depth, res.Warnings)
			}
		}
	}
}
