package layout

import (
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func allText(pages []*Page) string {
	return strings.Join(lineTexts(pages), "\n")
}

func TestInlineBlockKeepsItsText(t *testing.T) {
	for name, tt := range map[string]struct {
		html string
		want []string
	}{
		"inline text": {`<p>a <span style="display:inline-block">pill</span> b</p>`, []string{"pill"}},
		"with blocks": {`<p>x</p><span style="display:inline-block"><div>one</div><div>two</div>three</span>`, []string{"one", "two", "three"}},
		"nested":      {`<div><span style="display:inline-block">in<b style="display:inline-block">ner</b></span></div>`, []string{"in", "ner"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := allText(newRun(t, tt.html, opts{extra: smallPage}).all(t))
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("%q missing from %.300q", w, got)
				}
			}
		})
	}
}

func TestInlineBlockPillsOfTemplate01(t *testing.T) {
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "01-modern", testcorpus.Small)
	if err != nil {
		t.Fatal(err)
	}
	got := allText(newRun(t, string(html), opts{extra: testcorpus.PaperCSS, scale: 0.9}).all(t))
	for _, w := range []string{"Trojan", "Emotet"} {
		if !strings.Contains(got, w) {
			t.Errorf("pill %q missing from %.300q", w, got)
		}
	}
}
