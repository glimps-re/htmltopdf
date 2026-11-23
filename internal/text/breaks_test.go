package text

import (
	"bufio"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/go-text/typesetting/segmenter"
)

func TestBreaks(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Break
	}{
		{"empty", "", nil},
		{"one word", "hello", nil},
		{"space", "a b", []Break{{2, false}}},
		{"two spaces", "ab cd ef", []Break{{3, false}, {6, false}}},
		{"newline", "a\nb", []Break{{2, true}}},
		{"hyphen", "well-known", []Break{{5, false}}},
		{"no break before nbsp", "a b", nil},
		{"ideographs", "日本語", []Break{{3, false}, {6, false}}},
		{"trailing space", "ab ", nil},
		{"invalid utf8 keeps byte offsets", "a \xff\xfe b", []Break{{2, false}, {5, false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Breaks(tt.text, nil); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Breaks(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestBreaksAppends(t *testing.T) {
	dst := []Break{{99, true}}
	got := Breaks("a b", dst)
	if len(got) != 2 || got[0].Offset != 99 || got[1].Offset != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestBreaksLong(t *testing.T) {
	text := strings.Repeat("word ", 10000)
	got := Breaks(text, nil)
	if len(got) != 9999 {
		t.Fatalf("%d breaks, want one after each word but the last", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Offset <= got[i-1].Offset {
			t.Fatal("offsets not increasing")
		}
	}
}

// segmentsOf cuts text at its break opportunities.
func segmentsOf(text string) []string {
	var segs []string
	prev := 0
	for _, b := range Breaks(text, nil) {
		segs = append(segs, text[prev:b.Offset])
		prev = b.Offset
	}
	return append(segs, text[prev:])
}

// probe is one line of testdata/chrome_breaks.txt: a string and the segments
// Chrome cut it in, joined by U+001F.
type probe struct {
	text string
	want []string
}

// readProbes reads testdata/chrome_breaks.txt, which holds one quoted string,
// a tab and the quoted segments per line; lines starting with # are comments.
func readProbes(t *testing.T) []probe {
	t.Helper()
	f, err := os.Open("testdata/chrome_breaks.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var probes []probe
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<16)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		q1, q2, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("line %d: no tab", n)
		}
		s, err1 := strconv.Unquote(q1)
		w, err2 := strconv.Unquote(q2)
		if err1 != nil || err2 != nil {
			t.Fatalf("line %d: bad quoting", n)
		}
		probes = append(probes, probe{s, strings.Split(w, "\x1f")})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return probes
}

// TestBreaksMatchChrome checks that the segments of every probe string are the
// ones Chrome gave.
func TestBreaksMatchChrome(t *testing.T) {
	probes := readProbes(t)
	bad := 0
	for _, p := range probes {
		if got, want := strings.Join(segmentsOf(p.text), "\x1f"), strings.Join(p.want, "\x1f"); got != want {
			bad++
			if bad <= 40 {
				t.Errorf("%q: got %q, Chrome %q", p.text, got, want)
			}
		}
	}
	if len(probes) < 1000 {
		t.Errorf("only %d probe strings", len(probes))
	}
	if bad > 0 {
		t.Errorf("%d of %d probe strings differ from Chrome", bad, len(probes))
	}
}

// TestBreakTableIsDerivedFromChromeData rebuilds asciiBreak from the class
// pairs found at every boundary of the probes and whether Chrome cut there:
// the table must equal it bit for bit, and every pair must be covered but the
// two the data cannot show (two spaces collapse; "-" before a digit is a
// context rule).
func TestBreakTableIsDerivedFromChromeData(t *testing.T) {
	rebuilt := map[int]uint64{}
	seen := map[[2]int]bool{}
	for _, p := range readProbes(t) {
		cut := map[int]bool{}
		pos := 0
		for _, seg := range p.want[:len(p.want)-1] {
			pos += len(seg)
			cut[pos] = true
		}
		var last rune
		prevClass := int(classNone)
		for i, r := range p.text {
			cls := int(classOf(r))
			hyphenDigit := last == '-' && cls == classDigit
			if i > 0 && prevClass != classNone && cls != classNone && !hyphenDigit {
				key := [2]int{prevClass, cls}
				broke := cut[i]
				if seen[key] && (rebuilt[prevClass]>>cls&1 == 1) != broke {
					t.Fatalf("%q: Chrome's decision for classes %d,%d is not a function of the pair", p.text, prevClass, cls)
				}
				seen[key] = true
				if broke {
					rebuilt[prevClass] |= 1 << cls
				}
			}
			last, prevClass = r, cls
		}
	}
	hyphen := int(classOf('-'))
	for a := range numClasses {
		for b := range numClasses {
			spaces := a == classSpace && b == classSpace
			hyphenDigit := a == hyphen && b == classDigit
			if !seen[[2]int{a, b}] && !spaces && !hyphenDigit {
				t.Errorf("classes %d,%d are not covered by the probes", a, b)
			}
		}
		if rebuilt[a] != asciiBreak[a] {
			t.Errorf("row %d: table %#x, Chrome data %#x", a, asciiBreak[a], rebuilt[a])
		}
	}
}

const benchText = "Un ou plusieurs fichiers ont été bloqués par les moteurs d'analyse. " +
	`C:\Users\victim\Downloads\invoice_0001_très_urgent_à_ouvrir.pdf.exe e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 ` +
	"Trojan:Win32/Wacatac.B!ml https://example.com/a/b?x=1&y=2#frag 3.2 GiB "

func BenchmarkBreaks(b *testing.B) {
	text := strings.Repeat(benchText, 20)
	var dst []Break
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		dst = Breaks(text, dst[:0])
	}
}

// BenchmarkBreaksUAX is the segmentation alone, without the Chrome tailoring.
func BenchmarkBreaksUAX(b *testing.B) {
	text := strings.Repeat(benchText, 20)
	var seg segmenter.Segmenter
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		if err := seg.InitWithString(text); err != nil {
			b.Fatal(err)
		}
		for it := seg.LineIterator(); it.Next(); {
			_ = it.Line()
		}
	}
}

func FuzzBreaks(f *testing.F) {
	for _, s := range []string{"", "a:b", `C:\x\y_z.exe`, "a b-c", "日本語", "a\nb", "é-1", "\xff\xfea"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		prev := 0
		for _, b := range Breaks(s, nil) {
			if b.Offset <= prev || b.Offset >= len(s) {
				t.Fatalf("%q: break at %d after %d (len %d)", s, b.Offset, prev, len(s))
			}
			prev = b.Offset
		}
	})
}
