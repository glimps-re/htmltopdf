package css

import (
	"strings"
	"testing"
)

// A prelude or at-rule without a block inside a block must not swallow the
// closing brace and the rules that follow.
func TestRecoveryInsideBlocks(t *testing.T) {
	tests := []struct {
		name, src, suffix, warn string
	}{
		{"at-rule in a style rule", "p{@foo} q{color:red}", "q{color:red}", "@foo"},
		{"margin box without a block", "@page{@bottom-left} q{color:red}", "q{color:red}", "@bottom-left"},
		{"at-rule in @media", "@media screen{ @foo } q{color:red}", "q{color:red}", "@foo"},
		{"prelude without a block in @media", "@media screen{ a } q{color:red} p{color:red}", "q{color:red} p{color:red}", ""},
		{"at-rule in a margin box", "@page{@top-left{@foo} margin:0} q{color:red}", "q{color:red}", "@foo"},
		{"nested media unterminated rule", "@media a{@media b{ c } d{x:y}} q{color:red}", "q{color:red}", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warn := parseSheet(t, tt.src)
			if !strings.HasSuffix(got, tt.suffix) {
				t.Fatalf("got %q, want suffix %q", got, tt.suffix)
			}
			if len(warn) == 0 || !strings.Contains(strings.Join(warn, "\n"), tt.warn) {
				t.Fatalf("warnings %q, want one containing %q", warn, tt.warn)
			}
		})
	}
}
