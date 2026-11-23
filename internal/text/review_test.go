package text

import (
	"errors"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestShapeChargesExactlyTheGlyphs(t *testing.T) {
	for _, n := range []int{1, 100, 2 * ChunkRunes, 2*ChunkRunes + 1} {
		word := strings.Repeat("a", n)
		s := NewShaper(referenceSet(t), newBudget(int64(n), nil))
		if _, err := s.Shape(word, sans, nil); err != nil {
			t.Fatalf("%d glyphs at a limit of %d: %v", n, n, err)
		}
		s = NewShaper(referenceSet(t), newBudget(int64(n-1), nil))
		var le *budget.LimitError
		if _, err := s.Shape(word, sans, nil); !errors.As(err, &le) || le.Kind != budget.Glyphs {
			t.Fatalf("%d glyphs at a limit of %d: %v", n, n-1, err)
		}
	}
}

func TestAlternatingScriptsAreCappedAndCharged(t *testing.T) {
	text := strings.Repeat("aα", MaxSpans)
	b := newBudget(budget.Defaults()[budget.Glyphs], nil)
	s := NewShaper(referenceSet(t), b)
	runs, err := s.Shape(text, sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) > MaxSpans {
		t.Fatalf("%d runs, want <= %d", len(runs), MaxSpans)
	}
	if got := b.Used(budget.Work); got < int64(len(runs))*workPerSpan {
		t.Fatalf("work %d for %d spans", got, len(runs))
	}
	found := false
	ws, _ := b.Warnings()
	for _, w := range ws {
		found = found || (w.Code == budget.WarnClamped && w.Detail == "spans")
	}
	if !found {
		t.Fatalf("no clamped warning: %v", ws)
	}
	total := 0
	for _, r := range runs {
		total += len(r.Glyphs)
	}
	if total != 2*MaxSpans {
		t.Fatalf("%d glyphs, want %d", total, 2*MaxSpans)
	}
}

func TestMissingGlyphWarnedOncePerRune(t *testing.T) {
	b := newBudget(budget.Defaults()[budget.Glyphs], nil)
	s := NewShaper(referenceSet(t), b)
	for range 3 {
		if _, err := s.Shape(strings.Repeat("\U00012000", 50), sans, nil); err != nil {
			t.Fatal(err)
		}
	}
	ws, _ := b.Warnings()
	n := 0
	for _, w := range ws {
		if w.Code == budget.WarnMissingGlyph {
			n++
			if w.Count != 1 {
				t.Fatalf("warned %d times for one rune", w.Count)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d missing-glyph warnings, want 1", n)
	}
}

func TestChainCacheFollowsTheSpec(t *testing.T) {
	s, _ := newShaper(t)
	a, err := s.Shape("abc", FontSpec{Families: []string{"sans-serif"}, Weight: 400, Size: 16}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bold, err := s.Shape("abc", FontSpec{Families: []string{"sans-serif"}, Weight: 700, Size: 16}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mono, err := s.Shape("abc", FontSpec{Families: []string{"monospace"}, Weight: 700, Size: 16}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Face == bold[0].Face || bold[0].Face == mono[0].Face {
		t.Fatalf("faces %s %s %s: a stale chain was reused", a[0].Face.Family, bold[0].Face.Family, mono[0].Face.Family)
	}
}

func TestEveryCSSGenericResolves(t *testing.T) {
	fs := referenceSet(t)
	for _, g := range []string{"cursive", "fantasy", "math", "emoji", "fangsong", "ui-rounded", "system-ui", "ui-serif", "ui-monospace"} {
		if len(fs.lookup(g)) == 0 {
			t.Errorf("generic %q resolves to no face", g)
		}
	}
}

func TestFontSignaturesRejected(t *testing.T) {
	for _, sig := range []string{"OTTO", "wOFF", "wOF2", "ttcf"} {
		data := []byte(sig + strings.Repeat("\x00", 64))
		if _, err := NewFontSet([]Source{{Data: data, Family: "X"}}, Generic{}, nil); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("%s: %v", sig, err)
		}
	}
}

func TestBreaksOfHugeTextAreNotPooled(t *testing.T) {
	huge := strings.Repeat("a ", maxPooledBytes)
	if got := Breaks(huge, nil); len(got) != maxPooledBytes-1 {
		t.Fatalf("%d breaks", len(got))
	}
	if got := Breaks("a b", nil); len(got) != 1 {
		t.Fatalf("%d breaks", len(got))
	}
}
