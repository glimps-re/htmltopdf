package htmltopdf

import (
	"fmt"
	"strings"
	"testing"
)

// 1000 columns of 1e7px do not push a coordinate past what the PDF writer accepts:
// the widths are bounded to 1e7px in total, with a warning, and the PDF is valid.
func TestM207HostileColumnWidthsRenderValidPDF(t *testing.T) {
	for name, width := range map[string]string{"1e7px": "1e7px", "1e6 percent": "1000000%"} {
		t.Run(name, func(t *testing.T) {
			var cols, cells strings.Builder
			for range 1000 {
				fmt.Fprintf(&cols, `<col style="width:%s">`, width)
				cells.WriteString("<td>x</td>")
			}
			html := `<table style="table-layout:fixed;width:100%"><colgroup>` + cols.String() + `</colgroup><tr>` + cells.String() + `</tr></table>`
			res, doc := renderM2HTML(t, html)
			if doc == nil || res.Pages < 1 {
				t.Fatalf("no document: %+v", res)
			}
			clamped := false
			for _, w := range res.Warnings {
				clamped = clamped || w.Detail == "table width"
			}
			if want := width == "1e7px"; clamped != want {
				t.Errorf("table width warning %v, want %v: %+v", clamped, want, res.Warnings)
			}
		})
	}
}
