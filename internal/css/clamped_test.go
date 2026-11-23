package css

import (
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestHasClampedLength(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"10000000px", false},
		{"10000001px", true},
		{"-10000001px", true},
		{"9999999px", false},
		{"200000in", true},
		{"1e8%", true},
		{"1e9em", true},
		{"1px 2px", false},
		{"10000001deg", false},
		{"auto", false},
	}
	for _, tt := range tests {
		ss, err := ParseStylesheet([]byte("p{margin:"+tt.in+"}"), OriginDocument, budget.New(t.Context(), budget.Defaults(), nil))
		if err != nil {
			t.Fatal(err)
		}
		if len(ss.Rules) != 1 || len(ss.Rules[0].Decls) != 1 {
			t.Fatalf("%s: parsed %+v", tt.in, ss.Rules)
		}
		if got := HasClampedLength(ss.Rules[0].Decls[0].Value); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.in, got, tt.want)
		}
	}
}
