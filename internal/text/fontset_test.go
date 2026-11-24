package text

import (
	"encoding/binary"
	"errors"
	"slices"
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

// maxFuzzFontBytes bounds the fonts FuzzNewFontSet runs: twice the seed.
const maxFuzzFontBytes = 128 << 10

// FuzzNewFontSet feeds the font loader: it must neither panic nor hang. Fonts
// are trusted configuration, so the allocation a crafted font drives is not
// asserted (go-text parses a shared GSUB subtable once per reference).
//
// The seed is DejaVuSansMono cut to 200 glyphs with its GPOS, GSUB and kern
// whole (55 KB, TestTrimmedSeedFontKeepsTheLayoutTables checks that the layout parsers still
// see lookups): every seed is kept by each worker and every "interesting" mutant
// is a copy of it, so the 340 KB font filled a 2 GiB cap through the corpus alone
// (measured: coordinator at 650 MB after 780 entries, 814 MB on disk). Inputs
// over maxFuzzFontBytes are skipped for the same reason, and the shared-subtable
// amplification is quadratic in the input size. Run with -fuzzminimizetime 0s
// and a memory cap of at least 2 GiB (`systemd-run --user --scope -p
// MemoryMax=2G -p MemorySwapMax=0`); clear $GOCACHE/fuzz/.../FuzzNewFontSet
// after changing the seed.
func FuzzNewFontSet(f *testing.F) {
	f.Add(trimFont(f, referenceFont(f, "DejaVuSansMono.ttf"), 200))
	f.Add(referenceFont(f, "LiberationSans-Regular.ttf")[:512])
	f.Add([]byte("OTTO\x00\x01\x00\x00"))
	f.Add([]byte("ttcf\x00\x01\x00\x00"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > maxFuzzFontBytes {
			return
		}
		fs, err := NewFontSet([]Source{{Data: data}}, Generic{}, nil)
		if err == nil {
			for _, face := range fs.Faces() {
				face.UnitsPerEm()
			}
		}
	})
}

// trimFont returns a small font made of the first glyphs of ttf, keeping the
// layout tables (GPOS, GSUB, kern) whole: a seed that reaches the parsers
// without the 300 KB the fuzz workers and the coordinator would keep per
// mutant. The glyph ids the layout tables and cmap refer to may be beyond the
// kept glyphs: parsers must cope.
func trimFont(tb testing.TB, ttf []byte, glyphs int) []byte {
	tb.Helper()
	be16 := func(b []byte, o int) int { return int(binary.BigEndian.Uint16(b[o:])) }
	be32 := func(b []byte, o int) int { return int(binary.BigEndian.Uint32(b[o:])) }
	tables := map[string][]byte{}
	for i := range be16(ttf, 4) {
		rec := ttf[12+16*i:]
		off, n := be32(rec, 8), be32(rec, 12)
		tables[string(rec[:4])] = ttf[off : off+n]
	}
	head, hhea, maxp := append([]byte(nil), tables["head"]...), append([]byte(nil), tables["hhea"]...), append([]byte(nil), tables["maxp"]...)
	total := be16(maxp, 4)
	glyphs = min(glyphs, total)
	binary.BigEndian.PutUint16(maxp[4:], uint16(glyphs&0xFFFF))
	long := be16(head, 50) == 1
	entry := 2
	if long {
		entry = 4
	}
	loca := tables["loca"][:(glyphs+1)*entry]
	end := be16(loca, 2*glyphs)
	if long {
		end = be32(loca, 4*glyphs)
	} else {
		end *= 2
	}
	nh := min(be16(hhea, 34), glyphs)
	binary.BigEndian.PutUint16(hhea[34:], uint16(nh&0xFFFF))
	hmtx := tables["hmtx"][:nh*4+(glyphs-nh)*2]
	out := map[string][]byte{
		"head": head, "hhea": hhea, "maxp": maxp, "loca": loca, "glyf": tables["glyf"][:end], "hmtx": hmtx,
		"cmap": tables["cmap"], "name": tables["name"], "OS/2": tables["OS/2"],
	}
	for _, t := range []string{"GPOS", "GSUB", "kern"} {
		if b, ok := tables[t]; ok {
			out[t] = b
		}
	}
	tags := make([]string, 0, len(out))
	for t, b := range out {
		if b != nil {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	d := make([]byte, 12+16*len(tags))
	binary.BigEndian.PutUint32(d, 0x00010000)
	binary.BigEndian.PutUint16(d[4:], uint16(len(tags)&0xFFFF))
	for i, t := range tags {
		for len(d)%4 != 0 {
			d = append(d, 0)
		}
		rec := d[12+16*i:]
		copy(rec, t)
		binary.BigEndian.PutUint32(rec[8:], uint32(len(d)&0xFFFFFFFF))
		binary.BigEndian.PutUint32(rec[12:], uint32(len(out[t])&0xFFFFFFFF))
		d = append(d, out[t]...)
	}
	return d
}

// referenceFont returns the data of the reference font with the given file name.
func referenceFont(tb testing.TB, name string) []byte {
	tb.Helper()
	i := slices.Index(referenceFiles, name)
	if i < 0 {
		tb.Fatalf("%s is not a reference font", name)
	}
	return referenceSources(tb)[i].Data
}

// go-text drops the errors of the layout tables silently: the seed must still
// hold the same lookups as the full font, or the fuzz target stops exercising
// the layout parsers without a failure.
func TestTrimmedSeedFontKeepsTheLayoutTables(t *testing.T) {
	full := referenceFont(t, "DejaVuSansMono.ttf")
	seed := trimFont(t, full, 200)
	if len(seed) > maxFuzzFontBytes/2+8<<10 {
		t.Errorf("seed is %d bytes", len(seed))
	}
	lookups := func(data []byte) (gsub, gpos int) {
		fs, err := NewFontSet([]Source{{Data: data}}, Generic{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		ft, err := fs.Faces()[0].load()
		if err != nil {
			t.Fatal(err)
		}
		return len(ft.GSUB.Lookups), len(ft.GPOS.Lookups)
	}
	wantSub, wantPos := lookups(full)
	if wantSub == 0 || wantPos == 0 {
		t.Fatalf("the full font has %d GSUB and %d GPOS lookups", wantSub, wantPos)
	}
	if sub, pos := lookups(seed); sub != wantSub || pos != wantPos {
		t.Errorf("seed has %d GSUB and %d GPOS lookups, the font %d and %d", sub, pos, wantSub, wantPos)
	}
}
