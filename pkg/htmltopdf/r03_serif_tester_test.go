package htmltopdf

import (
	"bytes"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func TestDejaVuSerifEmbeddedOnlyWhereNamed(t *testing.T) {
	// A font name every template uses proves the names are readable in the bytes, so an
	// absence below cannot pass by accident.
	known := [][]byte{[]byte("DejaVuSans"), []byte("LiberationSans")}
	for _, tc := range []struct {
		template string
		present  []string
		absent   []string
	}{
		{"00-host-connector", nil, []string{"DejaVuSerif"}},
		{"01-modern", nil, []string{"DejaVuSerif"}},
		{"02-executive", []string{"DejaVuSerif-Regular", "DejaVuSerif-Italic"}, []string{"DejaVuSerif-Bold"}},
		{"03-analyst", nil, []string{"DejaVuSerif"}},
	} {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(tc.template+"/"+string(size), func(t *testing.T) {
				_, data, _ := renderM2(t, tc.template, size)
				if !bytes.Contains(data, known[0]) && !bytes.Contains(data, known[1]) {
					t.Fatal("no known font name found in the PDF: names are not readable")
				}
				for _, name := range tc.present {
					if !bytes.Contains(data, []byte(name)) {
						t.Errorf("font %s not found in the PDF", name)
					}
				}
				for _, name := range tc.absent {
					if bytes.Contains(data, []byte(name)) {
						t.Errorf("font %s found in the PDF", name)
					}
				}
			})
		}
	}
}
