package htmltopdf

import (
	"fmt"
	"testing"
)

// Flow content pulled back up by a negative margin, after a block taller than the pages, is not dropped,
// including when it is pulled above the top of the page: every piece of text is in the PDF once.
func TestM4FlowNegativeMarginAcrossPagesKeepsText(t *testing.T) {
	for _, margin := range []int{-50, -250, -1000} {
		t.Run(fmt.Sprintf("margin-top %dpx", margin), func(t *testing.T) {
			doc := fmt.Sprintf(`<style>@page{margin:0}</style><div style="height:300px;break-inside:avoid">FLOWBLOCK</div>`+
				`<div>TEXTA</div><div style="margin-top:%dpx"><div>TEXTB</div></div><div style="height:50px">TAIL</div>`, margin)
			r := h4Render(t, h4PageSizeOpts(t, PageSize{Width: 400, Height: 100}, ""), doc, true)
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4TextOnce(t, r, []string{"FLOWBLOCK", "TEXTA", "TEXTB", "TAIL"})
		})
	}
}
