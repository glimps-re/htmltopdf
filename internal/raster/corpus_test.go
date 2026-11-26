package raster

import (
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// Every crafted case of the image corpus gets its expected
// reason, from Add or, for corrupt data, from Decode.
func TestImageCases(t *testing.T) {
	for _, c := range testcorpus.ImageCases() {
		t.Run(c.Name, func(t *testing.T) {
			s, err := NewSet(newBudget()).Add(c.Src)
			if err == nil {
				_, err = Decode(s, newBudget())
			}
			got := ""
			if err != nil {
				r := reasonOf(err)
				if r == 0 {
					t.Fatalf("non-reject error %v", err)
				}
				got = r.String()
			}
			if got != c.Want {
				t.Errorf("got %q, want %q", got, c.Want)
			}
		})
	}
}
