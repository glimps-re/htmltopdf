package text

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestBreaksMatchChromeIndependentOracle compares Breaks with Chrome on strings
// that are not in chrome_breaks.txt: URLs, Windows and UNC paths, emails,
// versions, hashes, French punctuation, "-" before digits, brackets. The data
// (testdata/chrome_breaks_extra.txt, same format) was recorded from Chromium
// in a 1px-wide box, 16px DejaVu Sans, with a probe written by the tester.
func TestBreaksMatchChromeIndependentOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/chrome_breaks_extra.txt")
	if err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile("testdata/chrome_breaks.txt")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, line := range strings.Split(string(main), "\n") {
		if q, _, ok := strings.Cut(line, "\t"); ok {
			known[q] = true
		}
	}
	n, bad := 0, 0
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		q1, q2, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("line %d: no tab", i+1)
		}
		if known[q1] {
			t.Errorf("line %d: %s is already in chrome_breaks.txt", i+1, q1)
		}
		s, err1 := strconv.Unquote(q1)
		want, err2 := strconv.Unquote(q2)
		if err1 != nil || err2 != nil {
			t.Fatalf("line %d: bad quoting", i+1)
		}
		n++
		if got := strings.Join(segmentsOf(s), "\x1f"); got != want {
			bad++
			t.Errorf("%q: got %q, Chrome %q", s, strings.ReplaceAll(got, "\x1f", "|"), strings.ReplaceAll(want, "\x1f", "|"))
		}
	}
	if n < 100 {
		t.Errorf("only %d strings", n)
	}
	t.Logf("%d strings, %d differ from Chrome", n, bad)
}
