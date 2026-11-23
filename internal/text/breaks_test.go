package text

import (
	"reflect"
	"strings"
	"testing"
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
	if len(got) != 10000-1+0 && len(got) != 10000 {
		t.Fatalf("%d breaks", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Offset <= got[i-1].Offset {
			t.Fatal("offsets not increasing")
		}
	}
}
