package css

import (
	"reflect"
	"testing"
)

func TestExportedHelpers(t *testing.T) {
	toks := valueTokens(t, "  a  f(b c)  [d e] ")
	if got := dumpTokens(TrimSpace(toks)); got != "a_f(b_c)_[d_e]" {
		t.Fatalf("TrimSpace: %q", got)
	}
	var fields []string
	for _, f := range Fields(toks) {
		fields = append(fields, dumpTokens(f))
	}
	if want := []string{"a", "f(b_c)", "[d_e]"}; !reflect.DeepEqual(fields, want) {
		t.Fatalf("Fields: %q, want %q", fields, want)
	}
	if LowerASCII("AbCK") != "abcK" {
		t.Fatal("LowerASCII folds more than ASCII")
	}
	fn := TrimSpace(toks)
	idx := 0
	for fn[idx].Type != TFunction {
		idx++
	}
	if after := SkipBlock(fn, idx); fn[after].Type != TWhitespace || after-idx != int(fn[idx].End) {
		t.Fatalf("SkipBlock = %d", after)
	}
	if SkipBlock(fn, 0) != 1 {
		t.Fatal("SkipBlock of a plain token")
	}
}
