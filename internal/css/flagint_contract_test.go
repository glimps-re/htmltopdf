package css

import (
	"context"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestContractFlagInt(t *testing.T) {
	tests := []struct {
		in    string
		isInt bool
		num   float32
	}{
		{"1", true, 1},
		{"+1", true, 1},
		{"-1", true, -1},
		{"-0", true, 0},
		{"+0", true, 0},
		{"0", true, 0},
		{"00", true, 0},
		{"007", true, 7},
		{"1.0", false, 1},
		{".5", false, .5},
		{"+.5", false, .5},
		{"-.5", false, -.5},
		{"1e0", false, 1},
		{"1E3", false, 1000},
		{"1e+3", false, 1000},
		{"1e-3", false, .001},
		{"2E0", false, 2},
		{"0.0", false, 0},
		{"100000000000000000000000000000000000000", true, 1e38},
		{strings.Repeat("9", 400), true, 0},
		{"1" + strings.Repeat("0", 400) + ".5", false, 0},
	}
	for _, tt := range tests {
		name := tt.in
		if len(name) > 30 {
			name = name[:30]
		}
		t.Run(name, func(t *testing.T) {
			ds, err := ParseDeclarations([]byte("x:"+tt.in), budget.New(context.Background(), budget.Defaults(), nil))
			if err != nil {
				t.Fatal(err)
			}
			if len(ds) != 1 {
				t.Skipf("declaration dropped: %v", ds)
			}
			var n *Token
			for i := range ds[0].Value {
				if ds[0].Value[i].Type != TWhitespace {
					n = &ds[0].Value[i]
					break
				}
			}
			if n == nil || n.Type != TNumber {
				t.Fatalf("not a number token: %+v", ds[0].Value)
			}
			if got := n.Flags&FlagInt != 0; got != tt.isInt {
				t.Errorf("FlagInt = %v, want %v", got, tt.isInt)
			}
			if n.Flags&FlagID != 0 {
				t.Error("FlagID set on a number")
			}
			if len(tt.in) < 30 && n.Num != tt.num && !(tt.num == 0 && n.Num == 0) {
				t.Errorf("Num = %v, want %v", n.Num, tt.num)
			}
		})
	}
	t.Run("only on Number tokens", func(t *testing.T) {
		ds, err := ParseDeclarations([]byte("x: 1px 2% a #abc 3"), budget.New(context.Background(), budget.Defaults(), nil))
		if err != nil || len(ds) != 1 {
			t.Fatal(err, ds)
		}
		for _, tok := range ds[0].Value {
			if tok.Type != TNumber && tok.Flags&FlagInt != 0 {
				t.Errorf("FlagInt on %+v", tok)
			}
		}
	})
}
