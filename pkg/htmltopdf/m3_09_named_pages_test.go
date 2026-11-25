package htmltopdf

import (
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// The cover of 02 is the named page "cover": no margin box on page 1 (its
// @page rule empties them), the default master with its header and footer on the
// pages that follow.
func TestM309CoverPageHasNoMarginBoxes(t *testing.T) {
	res, _, doc := renderM2(t, "02-executive", testcorpus.Small)
	if res.Pages != 5 {
		t.Fatalf("%d pages, Chrome 5", res.Pages)
	}
	text := func(p int) string {
		var sb strings.Builder
		for _, r := range pageRuns(t, doc, p) {
			sb.WriteString(r.text)
			sb.WriteByte(' ')
		}
		return sb.String()
	}
	if s := text(0); strings.Contains(s, "TLP:AMBER") || strings.Contains(s, "Synthèse d'analyse") {
		t.Errorf("page 1 has margin box text: %q", s)
	}
	for p := 1; p < res.Pages; p++ {
		if s := text(p); !strings.Contains(s, "TLP:AMBER") {
			t.Errorf("page %d has no footer", p+1)
		}
	}
}
