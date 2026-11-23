package dom

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/textsafe"
)

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "hello world", "hello world"},
		{"empty", "", ""},
		{"tab LF CR kept", "a\tb\nc\rd", "a\tb\nc\rd"},
		{"multibyte kept", "\u00E9\u4E16\U0001F600", "\u00E9\u4E16\U0001F600"},
		{"C0", "a\x00b\x01c\x08d\x0be\x0cf\x1fg", "a\uFFFDb\uFFFDc\uFFFDd\uFFFDe\uFFFDf\uFFFDg\uFFFD"[:0] + "a\uFFFDb\uFFFDc\uFFFDd\uFFFDe\uFFFDf\uFFFDg"},
		{"DEL", "a\x7fb", "a\uFFFDb"},
		{"C1", "a\u0080b\u0085c\u009fd", "a\uFFFDb\uFFFDc\uFFFDd"},
		{"just above C1", "\u00A0", "\u00A0"},
		{"invalid UTF-8", "a\xffb\xc3(c\xe2\x82", "a\uFFFDb\uFFFD(c\uFFFD\uFFFD"},
		{"literal replacement kept", "a\uFFFDb", "a\uFFFDb"},
		{"zero width dropped", "a\u200Bb\u200Ec\u200Fd", "abcd"},
		{"ZWNJ and ZWJ kept", "a\u200Cb\u200Dc", "a\u200Cb\u200Dc"},
		{"emoji ZWJ sequence", "\U0001F468\u200D\U0001F4BB", "\U0001F468\u200D\U0001F4BB"},
		{"Persian ZWNJ", "\u0645\u06CC\u200C\u062E\u0648\u0627\u0647\u0645", "\u0645\u06CC\u200C\u062E\u0648\u0627\u0647\u0645"},
		{"word joiner family dropped", "a\u2060b\u2061c\u2062d\u2063e\u2064f", "abcdef"},
		{"BOM and soft hyphen dropped", "a\uFEFFb\u00ADc", "abc"},
		{"neighbours of dropped ranges kept", "\u200A\u2010\u205F\u2065\u2070\u00AC\u00AE\u034F", "\u200A\u2010\u205F\u2065\u2070\u00AC\u00AE\u034F"},
		{"line and paragraph separators", "a\u2028b\u2029c", "a b c"},
		{"arabic letter mark dropped", "a\u061Cb", "ab"},
		{"mongolian vowel separator dropped", "a\u180Eb", "ab"},
		{"tag characters dropped", "a\U000E0041\U000E007Fb", "ab"},
		{"just outside tags kept", "\U000E0080", "\U000E0080"},
		{"LRE", "a\u202Ab", "a\u27E8LRE\u27E9b"},
		{"RLE", "a\u202Bb", "a\u27E8RLE\u27E9b"},
		{"PDF", "a\u202Cb", "a\u27E8PDF\u27E9b"},
		{"LRO", "a\u202Db", "a\u27E8LRO\u27E9b"},
		{"RLO", "invoice\u202Etxt.exe", "invoice\u27E8RLO\u27E9txt.exe"},
		{"LRI", "a\u2066b", "a\u27E8LRI\u27E9b"},
		{"RLI", "a\u2067b", "a\u27E8RLI\u27E9b"},
		{"FSI", "a\u2068b", "a\u27E8FSI\u27E9b"},
		{"PDI", "a\u2069b", "a\u27E8PDI\u27E9b"},
		{"mixed", "\u202E\x00\u200B\xff", "\u27E8RLO\u27E9\uFFFD\uFFFD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeText(tc.in)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(got) || len(got) > 3*len(tc.in) {
				t.Fatalf("invalid or too long result %q", got)
			}
		})
	}
}

func TestSanitizeTextNoAllocWhenClean(t *testing.T) {
	s := strings.Repeat("clean text\n", 100)
	if n := testing.AllocsPerRun(100, func() { _ = SanitizeText(s) }); n != 0 {
		t.Fatalf("allocs = %v, want 0", n)
	}
}

func TestSanitizeTextWorstExpansion(t *testing.T) {
	in := strings.Repeat("\u202E", 1000)
	if got := SanitizeText(in); len(got) != 3*len(in) {
		t.Fatalf("len = %d, want %d", len(got), 3*len(in))
	}
}

func FuzzSanitizeText(f *testing.F) {
	f.Add("a\u202E\xff\x00\u200B\u00ADz")
	f.Fuzz(func(t *testing.T, s string) {
		got := SanitizeText(s)
		if !utf8.ValidString(got) || len(got) > 3*len(s) {
			t.Fatalf("invalid or too long result %q", got)
		}
		for _, r := range got {
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r >= 0x7f && r <= 0x9f {
				t.Fatalf("control character in %q", got)
			}
			if textsafe.IsUnsafe(r) && r != '\t' && r != '\n' && r != '\r' && r != 0x200C && r != 0x200D {
				t.Fatalf("unsafe character in %q", got)
			}
		}
		if SanitizeText(got) != got {
			t.Fatalf("not idempotent on %q", got)
		}
	})
}
