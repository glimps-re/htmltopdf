package text

import (
	"errors"
	"testing"
)

func TestNewFontSetRejects(t *testing.T) {
	good := referenceSources(t)[0].Data
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short", []byte("ab")},
		{"collection", append([]byte("ttcf"), make([]byte, 64)...)},
		{"garbage", []byte("this is not a font file at all, just text")},
		{"truncated", good[:200]},
		{"zeros", make([]byte, 4096)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewFontSet([]Source{{Data: tt.data}}, Generic{}, nil)
			if !errors.Is(err, ErrInvalidFont) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestNewFontSetCFFRejected(t *testing.T) {
	// A valid sfnt directory that has cmap but no glyf table.
	d := []byte{'O', 'T', 'T', 'O', 0, 1, 0, 0, 0, 0, 0, 0, 'c', 'm', 'a', 'p', 0, 0, 0, 0, 0, 0, 0, 28, 0, 0, 0, 4, 0, 0, 0, 0}
	if _, err := NewFontSet([]Source{{Data: d}}, Generic{}, nil); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("err = %v", err)
	}
}

func padded(t *testing.T, n int) []byte {
	t.Helper()
	good := referenceSources(t)[0].Data
	out := make([]byte, n)
	copy(out, good)
	return out
}

func TestFontSizeLimit(t *testing.T) {
	if _, err := NewFontSet([]Source{{Data: padded(t, MaxFontBytes)}}, Generic{}, nil); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	if _, err := NewFontSet([]Source{{Data: padded(t, MaxFontBytes-1)}}, Generic{}, nil); err != nil {
		t.Fatalf("just under: %v", err)
	}
	if _, err := NewFontSet([]Source{{Data: padded(t, MaxFontBytes+1)}}, Generic{}, nil); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("over: %v", err)
	}
}

func TestTotalFontLimit(t *testing.T) {
	big := padded(t, MaxFontBytes)
	atLimit := []Source{{Data: big}, {Data: big}, {Data: big}, {Data: big}}
	if _, err := NewFontSet(atLimit, Generic{}, nil); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	under := []Source{{Data: big}, {Data: big}, {Data: big}, {Data: big[:MaxFontBytes-1]}}
	if _, err := NewFontSet(under, Generic{}, nil); err != nil {
		t.Fatalf("just under: %v", err)
	}
	small := referenceSources(t)[0]
	if _, err := NewFontSet(append(atLimit, small), Generic{}, nil); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("over: %v", err)
	}
}

func TestFontCountLimit(t *testing.T) {
	one := referenceSources(t)[0]
	mk := func(n int) []Source {
		out := make([]Source, n)
		for i := range out {
			out[i] = one
		}
		return out
	}
	for _, n := range []int{MaxFonts - 1, MaxFonts} {
		if _, err := NewFontSet(mk(n), Generic{}, nil); err != nil {
			t.Fatalf("%d fonts: %v", n, err)
		}
	}
	if _, err := NewFontSet(mk(MaxFonts+1), Generic{}, nil); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("over: %v", err)
	}
}

func TestFamilyNameLimit(t *testing.T) {
	src := referenceSources(t)[0]
	for _, n := range []int{maxFamilyBytes - 1, maxFamilyBytes} {
		src.Family = string(make([]byte, 0)) + repeat('x', n)
		if _, err := NewFontSet([]Source{src}, Generic{}, nil); err != nil {
			t.Fatalf("%d bytes: %v", n, err)
		}
	}
	src.Family = repeat('x', maxFamilyBytes+1)
	if _, err := NewFontSet([]Source{src}, Generic{}, nil); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("over: %v", err)
	}
}

func repeat(b byte, n int) string {
	s := make([]byte, n)
	for i := range s {
		s[i] = b
	}
	return string(s)
}

func FuzzNewFontSet(f *testing.F) {
	good := referenceSources(f)[0].Data
	f.Add(good[:512])
	f.Add([]byte("OTTO\x00\x01\x00\x00"))
	f.Add([]byte("ttcf\x00\x01\x00\x00"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		fs, err := NewFontSet([]Source{{Data: data}}, Generic{}, nil)
		if err == nil {
			for _, face := range fs.Faces() {
				face.UnitsPerEm()
			}
		}
	})
}
