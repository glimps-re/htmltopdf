package text

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-text/typesetting/shaping"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func hostileTexts() map[string]string {
	return map[string]string{
		"bidi-controls":   "abc\u202e\u2066\u2067\u2068\u2069\u202d\u202c\u200e\u200f def " + strings.Repeat("\u202e", 5000),
		"zwj-emoji":       "\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466" + strings.Repeat("\U0001F468\u200d", 10000),
		"zwnj-run":        strings.Repeat("a\u200c", 20000),
		"invalid-utf8":    "ab\xff\xfe\xc0\x80\xed\xa0\x80\xf4\x90\x80\x80cd\xe2\x82" + strings.Repeat("\xff", 10000),
		"truncated-seq":   strings.Repeat("\xf0\x9f", 5000),
		"nul-and-control": "a\x00b\x01\x7f\u0085\u2028\u2029c\r\n\r\n",
		"combining-1MB":   "e" + strings.Repeat("\u0301", (1<<20)/2),
		"only-combining":  strings.Repeat("\u0301", 100000),
		"surrogates-like": "\ufffe\uffff\U0010ffff\ufdd0",
		"arabic-hebrew":   strings.Repeat("\u0627\u05d0", 5000),
		"newlines":        strings.Repeat("\n", 50000),
		"spaces":          strings.Repeat(" ", 100000),
	}
}

func TestShapeBreaksHostile(t *testing.T) {
	for name, text := range hostileTexts() {
		t.Run(name, func(t *testing.T) {
			s := NewShaper(referenceSet(t), newBudget(budget.Defaults()[budget.Glyphs], nil))
			defer debug.SetGCPercent(debug.SetGCPercent(100))
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			runs, err := s.Shape(text, sans, nil)
			if err != nil {
				t.Fatalf("Shape: %v", err)
			}
			for _, r := range runs {
				for i, g := range r.Glyphs {
					if g.Cluster < 0 || int(g.Cluster) > len(text) {
						t.Fatalf("glyph %d cluster %d outside [0,%d]", i, g.Cluster, len(text))
					}
					if i > 0 && g.Cluster < r.Glyphs[i-1].Cluster {
						t.Fatalf("clusters decrease at glyph %d", i)
					}
				}
			}
			brk := Breaks(text, nil)
			prev := 0
			for _, b := range brk {
				if b.Offset <= prev && prev != 0 || b.Offset < 0 || b.Offset >= len(text) {
					t.Fatalf("bad break %+v after %d (len %d)", b, prev, len(text))
				}
				prev = b.Offset
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if d := after.HeapAlloc - min(after.HeapAlloc, before.HeapAlloc); d > 64<<20 {
				t.Fatalf("retained heap %d MiB", d>>20)
			}
		})
	}
}

func TestLongClusterChunksAreBounded(t *testing.T) {
	const marks = 3 * ChunkRunes
	chunks := 0
	b := newBudget(budget.Defaults()[budget.Glyphs], func(stage string) {
		if stage == "shape-chunk" {
			chunks++
		}
	})
	s := NewShaper(referenceSet(t), b)
	if _, err := s.Shape("e"+strings.Repeat("\u0301", marks), sans, nil); err != nil {
		t.Fatal(err)
	}
	if want := marks/ChunkRunes + 2; chunks > want {
		t.Fatalf("%d chunks for %d runes, want <= %d: a cluster longer than ChunkRunes degrades to one-rune chunks (quadratic)", chunks, marks, want)
	}
}

// Time and warnings of every hostile text, and the panic fallback in the
// middle of a chunked text.
func TestHostileTextCostAndWarnings(t *testing.T) {
	for name, text := range hostileTexts() {
		t.Run(name, func(t *testing.T) {
			b := newBudget(budget.Defaults()[budget.Glyphs], nil)
			s := NewShaper(referenceSet(t), b)
			start := time.Now()
			runs, err := s.Shape(text, sans, nil)
			took := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			ws, dropped := b.Warnings()
			glyphs := 0
			for _, r := range runs {
				glyphs += len(r.Glyphs)
			}
			t.Logf("%d bytes: %d runs, %d glyphs, %d warnings (+%d), %v", len(text), len(runs), glyphs, len(ws), dropped, took.Round(time.Millisecond))
			if len(ws) > 256 {
				t.Errorf("%d warnings kept", len(ws))
			}
			// The charge is made per rune, before shaping: it bounds the glyphs.
			if got := b.Used(budget.Glyphs); got < int64(glyphs) || got > int64(utf8.RuneCountInString(text)) {
				t.Errorf("charged %d glyphs for %d returned and %d runes", got, glyphs, utf8.RuneCountInString(text))
			}
		})
	}
}

func TestShaperPanicInTheMiddleOfAChunkedText(t *testing.T) {
	b := newBudget(budget.Defaults()[budget.Glyphs], nil)
	s := NewShaper(referenceSet(t), b)
	calls := 0
	real := s.shapeFn
	s.shapeFn = func(in shaping.Input) shaping.Output {
		calls++
		if calls%3 == 2 {
			panic("injected")
		}
		return real(in)
	}
	text := strings.Repeat("word ", 20000)
	runs, err := s.Shape(text, sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	glyphs := 0
	for _, r := range runs {
		glyphs += len(r.Glyphs)
		for _, g := range r.Glyphs {
			if g.Advance <= 0 && g.ID != 0 {
				t.Fatalf("glyph %+v without advance", g)
			}
		}
	}
	if glyphs < len(text)-1 {
		t.Errorf("%d glyphs for %d runes after the fallback", glyphs, len(text))
	}
	found := false
	ws, _ := b.Warnings()
	for _, w := range ws {
		found = found || w.Code == budget.WarnShapingFallback
	}
	if !found || calls < 3 {
		t.Errorf("calls %d, warnings %v", calls, ws)
	}
}
