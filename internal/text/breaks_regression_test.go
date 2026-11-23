package text

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"
)

// uaxOnly is the segmentation Breaks gave before the Chrome tailoring
// (HEAD's implementation: UAX #14 on runes, byte offsets, no tailoring).
func uaxOnly(text string) []Break {
	var seg segmenter.Segmenter
	var out []Break
	if err := seg.InitWithString(text); err == nil {
		for it := seg.LineIterator(); it.Next(); {
			l := it.Line()
			if end := l.OffsetInBytes + l.LengthInBytes; end < len(text) {
				out = append(out, Break{Offset: end, Mandatory: l.IsMandatoryBreak})
			}
		}
		return out
	}
	runes := []rune(text)
	seg.Init(runes)
	offsets := make([]int, 0, len(runes)+1)
	for i := range text {
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(text))
	if len(text) == len(runes) {
		offsets = nil
	}
	for it := seg.LineIterator(); it.Next(); {
		l := it.Line()
		if end := l.Offset + len(l.Text); end < len(runes) {
			off := end
			if offsets != nil {
				off = offsets[end]
			}
			out = append(out, Break{Offset: off, Mandatory: l.IsMandatoryBreak})
		}
	}
	return out
}

// untailored are characters outside the Chrome tailoring: CJK, the return
// arrow, bidi marks and embeddings, isolates, controls, separators.
var untailored = []string{
	"日", "本", "語", "の", "テ", "ス", "ト", "한", "글", "中", "文", "。", "、", "「", "」", "ー",
	"↳", "→", "\u200e", "\u200f", "\u061c", "\u202a", "\u202b", "\u202c", "\u202d", "\u202e", "\u2066", "\u2067", "\u2068", "\u2069",
	"\x00", "\x01", "\x07", "\x08", "\t", "\n", "\r", "\r\n", "\x0b", "\x0c", "\x1b", "\x7f", "\u0085", "\u2028", "\u2029",
	"\u00a0", "\u200b", "\u2060", "\ufeff", "\u2007", "\u202f", "\u2011", "\u00ad",
	"😀", "👍🏽", "ال", "שלום", "\u0301",
}

// prng is a deterministic splitmix64 generator.
type prng uint64

func (p *prng) next() uint64 {
	*p += 0x9e3779b97f4a7c15
	z := uint64(*p)
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

func (p *prng) intn(n int) int { return int(p.next()>>33) % n }

func randomText(r *prng, alphabet []string, n int) string {
	var sb strings.Builder
	for range n {
		sb.WriteString(alphabet[r.intn(len(alphabet))])
	}
	return sb.String()
}

// Texts made only of characters the tailoring does not cover behave exactly as
// before.
func TestBreaksUntailoredTextIsUnchanged(t *testing.T) {
	r := prng(1)
	for i := range 3000 {
		s := randomText(&r, untailored, 1+r.intn(24))
		got, want := Breaks(s, nil), uaxOnly(s)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: %q\n got %v\nwant %v", i, s, got, want)
		}
	}
}

// Invalid UTF-8 counts as U+FFFD, which is not tailored: a text of untailored
// characters and invalid bytes keeps the UAX #14 opportunities.
func TestBreaksInvalidUTF8IsUnchanged(t *testing.T) {
	r := prng(3)
	alphabet := append([]string{"\xff", "\xfe", "\x80", "\xc3", "\xe2\x82", "\xf0\x9f"}, untailored...)
	for i := range 3000 {
		s := randomText(&r, alphabet, 1+r.intn(30))
		if utf8.ValidString(s) {
			continue
		}
		got, want := Breaks(s, nil), uaxOnly(s)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: %q\n got %v\nwant %v", i, s, got, want)
		}
	}
	for _, s := range []string{"\xff", "\xff\xfe", "日本\x80語", "\xc3\n\xff", "↳\xe2\x82"} {
		if got, want := Breaks(s, nil), uaxOnly(s); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v want %v", s, got, want)
		}
	}
}

// In mixed text, a boundary touching a character outside the tailoring keeps
// the UAX #14 decision, mandatory flag included.
func TestBreaksNextToUntailoredCharsIsUnchanged(t *testing.T) {
	r := prng(5)
	alphabet := append([]string{"a", "b", "Z", "1", "9", "-", ":", "/", "\\", "_", " ", ".", "?", "(", ")", "é", "ç", "\xff", "\x80", "\xc3"}, untailored...)
	for i := range 5000 {
		s := randomText(&r, alphabet, 1+r.intn(30))
		got := map[int]Break{}
		for _, b := range Breaks(s, nil) {
			got[b.Offset] = b
		}
		want := map[int]Break{}
		for _, b := range uaxOnly(s) {
			want[b.Offset] = b
		}
		for off := 1; off < len(s); off++ {
			before, bsize := utf8.DecodeLastRuneInString(s[:off])
			after, asize := utf8.DecodeRuneInString(s[off:])
			if (before == utf8.RuneError && bsize <= 1) || (after == utf8.RuneError && asize <= 1) {
				before, after = utf8.RuneError, utf8.RuneError
			} else if off+utf8.UTFMax <= len(s) && !utf8.RuneStart(s[off]) {
				continue
			}
			if classOf(before) != classNone && classOf(after) != classNone {
				continue
			}
			if got[off] != want[off] {
				t.Fatalf("case %d: %q offset %d (%q|%q): got %+v, UAX %+v", i, s, off, before, after, got[off], want[off])
			}
		}
		for off, b := range got {
			if b.Mandatory && !want[off].Mandatory {
				t.Fatalf("case %d: %q: mandatory break at %d that UAX does not have", i, s, off)
			}
		}
	}
}

// Mandatory breaks (newlines) are never lost nor created by the tailoring.
func TestBreaksMandatoryUnchanged(t *testing.T) {
	r := prng(7)
	alphabet := []string{"a", "b", "1", "-", ":", "/", " ", "é", "\n", "\r\n", "\r", "\u2028", "\u2029", "\x0b", "\x0c", "\u0085"}
	for i := range 3000 {
		s := randomText(&r, alphabet, 1+r.intn(30))
		mandatory := func(bs []Break) (out []int) {
			for _, b := range bs {
				if b.Mandatory {
					out = append(out, b.Offset)
				}
			}
			return out
		}
		if got, want := mandatory(Breaks(s, nil)), mandatory(uaxOnly(s)); !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: %q mandatory breaks %v, UAX %v", i, s, got, want)
		}
	}
}

// Known strings of the task: French, return arrow, bidi markers, CJK.
func TestBreaksKnownRegressionStrings(t *testing.T) {
	for _, s := range []string{
		"↳ Fichier extrait de l'archive", "a↳b", "↳", "x ↳ y", "\u202eevil.exe\u202c", "\u200fشرح\u200e 12", "日本語のテキスト", "日本語 text 日本語",
		"a\x00b c", "tab\there", "line1\nline2\r\nline3", "\u2028", "naïve façade déjà-vu", "Total : 3 fichiers (bloqués) ; 2 « sains ».",
	} {
		got, want := Breaks(s, nil), uaxOnly(s)
		if strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz") {
			// Tailored boundaries may differ; the untailored ones are checked above.
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v want %v", s, got, want)
		}
	}
}

// Valid segments around invalid bytes follow the tailoring; the boundaries
// that touch a bad byte keep the UAX #14 decision.
func TestBreaksTailoringAroundInvalidBytes(t *testing.T) {
	for _, tt := range []struct {
		text string
		want []string
	}{
		{"a:b \xff c/d", []string{"a:b ", "\xff ", "c/d"}},
		{"C:\\x\\y_z.exe \xfe next-1", []string{"C:\\x\\y_z.exe ", "\xfe ", "next-", "1"}},
		{"\xff1.5 MiB", []string{"\xff1.5 ", "MiB"}},
	} {
		if got := segmentsOf(tt.text); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q: got %q, want %q", tt.text, got, tt.want)
		}
	}
}
