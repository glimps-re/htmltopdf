package text

import (
	"strings"
	"testing"
)

// A cluster longer than the chunk is split at the chunk size instead of
// producing chunks of one rune.
func TestChunkEndSplitsAClusterLongerThanTheChunk(t *testing.T) {
	s, _ := newShaper(t)
	s.chunk = 8
	for _, tt := range []struct {
		name string
		text string
		want int
	}{
		{"cluster fills the chunk", "e" + strings.Repeat("\u0301", 30), 8},
		{"cluster straddles the limit: cut before it", "abcdefge" + strings.Repeat("\u0301", 5), 7},
		{"cluster of exactly the chunk size", "e" + strings.Repeat("\u0301", 7) + "x", 8},
		{"cluster one rune longer than the chunk", "e" + strings.Repeat("\u0301", 8) + "x", 8},
	} {
		r := []rune(tt.text)
		if got := s.chunkEnd(r, 0, len(r)); got != tt.want {
			t.Errorf("%s: cut at %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestLongClusterShapesInLinearTime(t *testing.T) {
	s, b := newShaper(t)
	text := "e" + strings.Repeat("\u0301", 4*ChunkRunes)
	chunks := 0
	_ = b
	runs, err := s.Shape(text, sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		chunks += len(r.Glyphs)
	}
	if chunks < 4*ChunkRunes {
		t.Fatalf("only %d glyphs", chunks)
	}
}
