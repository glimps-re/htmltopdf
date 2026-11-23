package textsafe

import "testing"

func TestIsUnsafe(t *testing.T) {
	type span struct{ lo, hi rune }
	unsafe := map[string][]span{
		"C0":            {{0, 0x1f}},
		"DEL":           {{0x7f, 0x7f}},
		"C1":            {{0x80, 0x9f}},
		"soft hyphen":   {{0xad, 0xad}},
		"arabic letter": {{0x61c, 0x61c}},
		"mongolian":     {{0x180e, 0x180e}},
		"zero width":    {{0x200b, 0x200f}},
		"separators":    {{0x2028, 0x2029}},
		"bidi":          {{0x202a, 0x202e}, {0x2066, 0x2069}},
		"word joiner":   {{0x2060, 0x2064}},
		"bom":           {{0xfeff, 0xfeff}},
		"tags":          {{0xe0000, 0xe007f}},
	}
	for name, spans := range unsafe {
		for _, s := range spans {
			for r := s.lo; r <= s.hi; r++ {
				if !IsUnsafe(r) {
					t.Errorf("%s: U+%04X must be unsafe", name, r)
				}
			}
		}
	}
	safe := []rune{' ', 'a', '~', 0xa0, 0xa1, 0xe9, 0x34f, 0x2065, 0x2000, 0x3000, 0x1f600, 0xe0080, 0xdfff0, 0xfffd}
	for _, r := range safe {
		if IsUnsafe(r) {
			t.Errorf("U+%04X must be safe", r)
		}
	}
}

func BenchmarkIsUnsafeASCII(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IsUnsafe(rune('a' + i%26))
	}
}
