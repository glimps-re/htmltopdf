package pdf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func threeGlyphs() []glyphSpec {
	return []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}, {adv: 700, data: simpleGlyph()}}
}

func TestParseTTFRejects(t *testing.T) {
	put16 := func(tag string, off int, v int) func(map[string][]byte) {
		return func(m map[string][]byte) { binary.BigEndian.PutUint16(m[tag][off:], clamp16(v)) }
	}
	tests := []struct {
		name string
		mod  func(map[string][]byte)
	}{
		{"upem 15", put16("head", 18, 15)},
		{"upem 16385", put16("head", 18, 16385)},
		{"loca format 2", put16("head", 50, 2)},
		{"no glyph", put16("maxp", 4, 0)},
		{"loca too short", func(m map[string][]byte) { m["loca"] = m["loca"][:8] }},
		{"hmtx too short", func(m map[string][]byte) { m["hmtx"] = m["hmtx"][:8] }},
		{"no horizontal metric", put16("hhea", 34, 0)},
		{"more metrics than glyphs", put16("hhea", 34, 4)},
		{"head too short", func(m map[string][]byte) { m["head"] = m["head"][:50] }},
		{"missing glyf", func(m map[string][]byte) { delete(m, "glyf") }},
		{"missing hmtx", func(m map[string][]byte) { delete(m, "hmtx") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := fontTables(threeGlyphs())
			tt.mod(tables)
			if _, err := parseTTF(assemble(tables)); !errors.Is(err, ErrInvalidFont) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if _, err := parseTTF(buildFont(t, threeGlyphs())); err != nil {
		t.Fatalf("control font rejected: %v", err)
	}
}

func TestParseTTFUpemBounds(t *testing.T) {
	for _, upem := range []int{16, 16384} {
		tables := fontTables(threeGlyphs())
		binary.BigEndian.PutUint16(tables["head"][18:], clamp16(upem))
		if _, err := parseTTF(assemble(tables)); err != nil {
			t.Errorf("upem %d: %v", upem, err)
		}
	}
}

func TestParseTTFDirectory(t *testing.T) {
	good := buildFont(t, threeGlyphs())
	mutate := func(f func(b []byte)) []byte {
		b := bytes.Clone(good)
		f(b)
		return b
	}
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short", good[:11]},
		{"bad version", mutate(func(b []byte) { b[0] = 'O' })},
		{"zero tables", mutate(func(b []byte) { b[4], b[5] = 0, 0 })},
		{"table count beyond the file", mutate(func(b []byte) { b[4], b[5] = 0, 100 })},
		{"table beyond the file", mutate(func(b []byte) { binary.BigEndian.PutUint32(b[12+8:], clamp32(len(b))) })},
		{"huge table length", mutate(func(b []byte) { binary.BigEndian.PutUint32(b[12+12:], 0xffffffff) })},
		{"duplicate tag", mutate(func(b []byte) { copy(b[12+16:], b[12:12+4]) })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseTTF(tt.data); !errors.Is(err, ErrInvalidFont) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestParseTTFTableCountLimit(t *testing.T) {
	for _, n := range []int{maxFontTables - 1, maxFontTables, maxFontTables + 1} {
		tables := fontTables(threeGlyphs())
		for i := len(tables); i < n; i++ {
			tables[fmt.Sprintf("x%03d", i)] = []byte{0, 0, 0, 0}
		}
		_, err := parseTTF(assemble(tables))
		if ok := n <= maxFontTables; ok != (err == nil) {
			t.Errorf("%d tables: err = %v", n, err)
		}
	}
}

func TestSubsetRoundTrip(t *testing.T) {
	glyphs := []glyphSpec{{adv: 100}, {adv: 200, data: simpleGlyph()}, {adv: 300, data: simpleGlyph()}, {adv: 400, data: compositeGlyph(1, 2)}, {adv: 500, data: simpleGlyph()}, {adv: 600, data: simpleGlyph()}}
	f, err := parseTTF(buildFont(t, glyphs))
	if err != nil {
		t.Fatal(err)
	}
	var used glyphSet
	used.add(3)
	out, err := f.subset(used)
	if err != nil {
		t.Fatal(err)
	}
	if sum := checksum(out); sum != 0xB1B0AFBA {
		t.Fatalf("whole-font checksum %#x", sum)
	}
	sub, err := parseTTF(out)
	if err != nil {
		t.Fatal(err)
	}
	if sub.numGlyphs != 4 {
		t.Fatalf("numGlyphs = %d, want 4 (highest kept glyph + 1)", sub.numGlyphs)
	}
	for g, want := range []struct {
		kept bool
		adv  uint16
	}{{true, 100}, {true, 200}, {true, 300}, {true, 400}} {
		data, err := sub.glyph(g)
		if err != nil {
			t.Fatal(err)
		}
		orig, _ := f.glyph(g)
		if want.kept && (g == 0 && len(data) != len(orig) || g > 0 && !bytes.Equal(data, orig)) {
			t.Errorf("glyph %d altered", g)
		}
		if sub.advances[g] != want.adv {
			t.Errorf("glyph %d advance %d, want %d", g, sub.advances[g], want.adv)
		}
	}
	for _, tag := range []string{"cmap", "head", "hhea", "maxp", "hmtx", "loca", "glyf"} {
		if sub.table(tag) == nil {
			t.Errorf("table %s missing", tag)
		}
	}
}

func TestSubsetEmptiesUnusedGlyphs(t *testing.T) {
	glyphs := []glyphSpec{{adv: 1}, {adv: 2, data: simpleGlyph()}, {adv: 3, data: simpleGlyph()}, {adv: 4, data: simpleGlyph()}}
	f, err := parseTTF(buildFont(t, glyphs))
	if err != nil {
		t.Fatal(err)
	}
	var used glyphSet
	used.add(3)
	out, err := f.subset(used)
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := parseTTF(out)
	for g, wantEmpty := range []bool{true, true, true, false} {
		data, _ := sub.glyph(g)
		if (len(data) == 0) != wantEmpty && g != 0 {
			t.Errorf("glyph %d: %d bytes", g, len(data))
		}
	}
}

func TestSubsetShortHmtx(t *testing.T) {
	tables := fontTables([]glyphSpec{{adv: 100}, {adv: 200, data: simpleGlyph()}, {adv: 300, data: simpleGlyph()}, {adv: 300, data: simpleGlyph()}})
	// Only the first two glyphs have a full metric: the others share the last
	// advance and keep only their left side bearing.
	hm := tables["hmtx"]
	short := append([]byte(nil), hm[:8]...)
	short = append(short, 0, 7, 0, 8)
	tables["hmtx"] = short
	binary.BigEndian.PutUint16(tables["hhea"][34:], 2)
	f, err := parseTTF(assemble(tables))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.advances; len(got) != 4 || got[2] != 200 || got[3] != 200 {
		t.Fatalf("advances = %v", got)
	}
	var used glyphSet
	used.add(3)
	out, err := f.subset(used)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := parseTTF(out)
	if err != nil {
		t.Fatal(err)
	}
	hmtx := sub.table("hmtx")
	if sub.advances[3] != 200 || binary.BigEndian.Uint16(hmtx[4*2+2:]) != 7 || binary.BigEndian.Uint16(hmtx[4*3+2:]) != 8 {
		t.Fatalf("advances %v, hmtx %x", sub.advances, hmtx)
	}
}

func TestCompositeClosure(t *testing.T) {
	tests := []struct {
		name   string
		glyphs []glyphSpec
		use    uint16
		kept   []uint16
		err    bool
	}{
		{"components kept", []glyphSpec{{}, {data: simpleGlyph()}, {data: simpleGlyph()}, {data: compositeGlyph(1, 2)}, {data: simpleGlyph()}}, 3, []uint16{0, 1, 2, 3}, false},
		{"nested composites", []glyphSpec{{}, {data: simpleGlyph()}, {data: compositeGlyph(1)}, {data: compositeGlyph(2)}}, 3, []uint16{0, 1, 2, 3}, false},
		{"cycle of two", []glyphSpec{{}, {data: compositeGlyph(2)}, {data: compositeGlyph(1)}}, 1, []uint16{0, 1, 2}, false},
		{"self reference", []glyphSpec{{}, {data: compositeGlyph(1)}}, 1, []uint16{0, 1}, false},
		{"component beyond the font", []glyphSpec{{}, {data: compositeGlyph(9)}}, 1, nil, true},
		{"truncated composite", []glyphSpec{{}, {data: compositeGlyph(0)[:12]}}, 1, nil, true},
		{"composite flag without a next component", []glyphSpec{{}, {data: append(compositeGlyph(0)[:10:10], 0, 0x20, 0, 0, 0, 0)}}, 1, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parseTTF(buildFont(t, tt.glyphs))
			if err != nil {
				t.Fatal(err)
			}
			var used glyphSet
			used.add(tt.use)
			if err := f.closure(&used); (err != nil) != tt.err || (err != nil && !errors.Is(err, ErrInvalidFont)) {
				t.Fatalf("err = %v", err)
			}
			if tt.err {
				return
			}
			for _, g := range tt.kept {
				if !used.has(g) {
					t.Errorf("glyph %d dropped", g)
				}
			}
		})
	}
}

func TestLongCompositeChain(t *testing.T) {
	const n = 3000
	glyphs := []glyphSpec{{}, {data: simpleGlyph()}}
	for i := 2; i < n; i++ {
		glyphs = append(glyphs, glyphSpec{data: compositeGlyph(clamp16(i - 1))})
	}
	f, err := parseTTF(buildFont(t, glyphs))
	if err != nil {
		t.Fatal(err)
	}
	var used glyphSet
	used.add(n - 1)
	out, err := f.subset(used)
	if err != nil {
		t.Fatal(err)
	}
	if sub, err := parseTTF(out); err != nil || sub.numGlyphs != n {
		t.Fatalf("subset: %v", err)
	}
}

func TestLocaMustBeMonotonicAndInsideGlyf(t *testing.T) {
	set := func(m map[string][]byte, g int, v int) { binary.BigEndian.PutUint32(m["loca"][4*g:], clamp32(v)) }
	tests := []struct {
		name string
		mod  func(map[string][]byte)
	}{
		{"entry beyond glyf", func(m map[string][]byte) { set(m, 1, 5000) }},
		{"last entry beyond glyf", func(m map[string][]byte) { set(m, 3, len(m["glyf"])+4) }},
		{"descending", func(m map[string][]byte) { set(m, 1, 30) }},
		{"alternating", func(m map[string][]byte) { set(m, 1, len(m["glyf"])); set(m, 2, 0); set(m, 3, len(m["glyf"])) }},
		{"first entry not before the next", func(m map[string][]byte) { set(m, 0, 40); set(m, 1, 10) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := fontTables(threeGlyphs())
			tt.mod(tables)
			if _, err := parseTTF(assemble(tables)); !errors.Is(err, ErrInvalidFont) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// At the limit: the last entry is exactly the size of glyf; every entry
	// equal to its neighbour (empty glyphs) is fine.
	tables := fontTables(threeGlyphs())
	set(tables, 3, len(tables["glyf"]))
	if _, err := parseTTF(assemble(tables)); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
}

// The alternating loca of the review: it made a 51 MiB subset of 199 glyphs.
func TestAlternatingLocaOfARealFontIsRefused(t *testing.T) {
	data := readFont(t, "LiberationSans-Regular.ttf")
	orig, err := parseTTF(data)
	if err != nil {
		t.Fatal(err)
	}
	loca, glyf := orig.table("loca"), orig.table("glyf")
	if !orig.longLoca {
		t.Skip("short loca")
	}
	damaged := bytes.Clone(data)
	off := orig.tables["loca"].off
	for g := 0; g <= orig.numGlyphs; g++ {
		v := 0
		if g%2 == 1 {
			v = len(glyf)
		}
		binary.BigEndian.PutUint32(damaged[off+4*g:], clamp32(v))
	}
	_ = loca
	if err := ValidateFont(damaged); !errors.Is(err, ErrInvalidFont) {
		t.Fatalf("err = %v", err)
	}
}

func TestGlyphBeyondTheFont(t *testing.T) {
	f, err := parseTTF(buildFont(t, threeGlyphs()))
	if err != nil {
		t.Fatal(err)
	}
	var beyond glyphSet
	beyond.add(7)
	if _, err := f.subset(beyond); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("glyph beyond the font: %v", err)
	}
	if _, err := f.glyph(3); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("glyph(3) of 3: %v", err)
	}
	if _, err := f.glyph(-1); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("glyph(-1): %v", err)
	}
}

func manyComponents(n int) []byte {
	ids := make([]uint16, n)
	for i := range ids {
		ids[i] = 1
	}
	return compositeGlyph(ids...)
}

func TestComponentsPerGlyphLimit(t *testing.T) {
	for _, tt := range []struct {
		n  int
		ok bool
	}{{maxComponentsPerGlyph - 1, true}, {maxComponentsPerGlyph, true}, {maxComponentsPerGlyph + 1, false}, {100_000, false}} {
		f, err := parseTTF(buildFont(t, []glyphSpec{{}, {data: simpleGlyph()}, {data: manyComponents(tt.n)}}))
		if err != nil {
			t.Fatal(err)
		}
		var used glyphSet
		used.add(2)
		_, err = f.subset(used)
		if tt.ok != (err == nil) || (!tt.ok && !errors.Is(err, ErrInvalidFont)) {
			t.Errorf("%d components: err = %v", tt.n, err)
		}
	}
}

func TestClosureWorkLimit(t *testing.T) {
	perGlyph := maxComponentsPerGlyph
	maxGlyphs := maxClosureComponents / perGlyph
	for _, tt := range []struct {
		composites int
		ok         bool
	}{{maxGlyphs - 1, true}, {maxGlyphs, true}, {maxGlyphs + 1, false}} {
		glyphs := []glyphSpec{{}, {data: simpleGlyph()}}
		var used glyphSet
		for i := 0; i < tt.composites; i++ {
			glyphs = append(glyphs, glyphSpec{data: manyComponents(perGlyph)})
			used.add(clamp16(i + 2))
		}
		f, err := parseTTF(buildFont(t, glyphs))
		if err != nil {
			t.Fatal(err)
		}
		err = f.closure(&used)
		if tt.ok != (err == nil) || (!tt.ok && !errors.Is(err, ErrInvalidFont)) {
			t.Errorf("%d composites of %d components: err = %v", tt.composites, perGlyph, err)
		}
	}
}

func TestSubsetSizeLimit(t *testing.T) {
	f, err := parseTTF(buildFont(t, []glyphSpec{{}, {data: simpleGlyph()}, {data: simpleGlyph()}}))
	if err != nil {
		t.Fatal(err)
	}
	var used glyphSet
	used.add(2)
	out, err := f.subset(used)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		limit int
		ok    bool
	}{{len(out) + 1, true}, {len(out), true}, {len(out) - 1, false}} {
		got, err := f.subsetLimit(used, tt.limit)
		if tt.ok != (err == nil) || (!tt.ok && !errors.Is(err, ErrInvalidFont)) || (tt.ok && !bytes.Equal(got, out)) {
			t.Errorf("limit %d (size %d): err = %v", tt.limit, len(out), err)
		}
	}
}

func TestHeadMagicAndMaxpAreChecked(t *testing.T) {
	for name, mod := range map[string]func(map[string][]byte){
		"magic":      func(m map[string][]byte) { m["head"][12] ^= 0xff },
		"short maxp": func(m map[string][]byte) { m["maxp"] = m["maxp"][:6] },
	} {
		tables := fontTables(threeGlyphs())
		mod(tables)
		if _, err := parseTTF(assemble(tables)); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestShortLoca(t *testing.T) {
	tables := fontTables(threeGlyphs())
	binary.BigEndian.PutUint16(tables["head"][50:], 0)
	short := make([]byte, 0, 8)
	off := 0
	for i := 0; i < 3; i++ {
		short = binary.BigEndian.AppendUint16(short, clamp16(off/2))
		if i > 0 {
			off += 24
		}
	}
	short = binary.BigEndian.AppendUint16(short, clamp16(off/2))
	tables["loca"] = short
	f, err := parseTTF(assemble(tables))
	if err != nil {
		t.Fatal(err)
	}
	if g, err := f.glyph(1); err != nil || len(g) != 24 {
		t.Fatalf("short loca glyph: %d bytes, %v", len(g), err)
	}
}

func TestGlyphSet(t *testing.T) {
	var s glyphSet
	if s.max() != 0 {
		t.Fatal("empty set has a maximum")
	}
	for _, g := range []uint16{0, 1, 63, 64, 1000, 65535} {
		s.add(g)
		if !s.has(g) || s.max() < g {
			t.Errorf("glyph %d", g)
		}
	}
	if s.has(2) || s.max() != 65535 {
		t.Fatal("set is wrong")
	}
	var one glyphSet
	one.add(64)
	if one.max() != 64 {
		t.Fatalf("max = %d", one.max())
	}
}

func TestAppendWidthsAndToUnicode(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}, {adv: 700, data: simpleGlyph()}, {adv: 800, data: simpleGlyph()}, {adv: 900, data: simpleGlyph()}})
	w := newTestWriter(&bytes.Buffer{})
	id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
	c := &Content{}
	c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Text: "a"}, {ID: 2}, {ID: 4, Text: "😀"}})
	if err := mustAddPage(t, w, c); err != nil {
		t.Fatal(err)
	}
	if got := string(appendWidths(nil, id.e)); got != "[1 [600 700] 4 [900]]" {
		t.Fatalf("W = %s", got)
	}
	cm := string(toUnicode(id.e))
	for _, want := range []string{"2 beginbfchar\n<0001> <0061>\n<0004> <D83DDE00>\nendbfchar\n", "<0000> <FFFF>", "begincmap", "endcmap"} {
		if !bytes.Contains([]byte(cm), []byte(want)) {
			t.Errorf("ToUnicode lacks %q:\n%s", want, cm)
		}
	}
}

func TestToUnicodeChunks(t *testing.T) {
	glyphs := make([]glyphSpec, 251)
	for i := 1; i < len(glyphs); i++ {
		glyphs[i] = glyphSpec{adv: 500, data: simpleGlyph()}
	}
	for _, tt := range []struct{ n, blocks int }{{99, 1}, {100, 1}, {101, 2}, {250, 3}} {
		w := newTestWriter(&bytes.Buffer{})
		id := addFont(t, w, &FontProgram{Data: buildFont(t, glyphs), Name: "T"})
		run := make([]Glyph, tt.n)
		for i := range run {
			run[i] = Glyph{ID: clamp16(i + 1), Text: string(rune('A' + i%26))}
		}
		c := &Content{}
		c.Glyphs(id, 10, 0, 0, run)
		if err := mustAddPage(t, w, c); err != nil {
			t.Fatal(err)
		}
		cm := string(toUnicode(id.e))
		if got := bytes.Count([]byte(cm), []byte("beginbfchar")); got != tt.blocks {
			t.Errorf("%d glyphs: %d blocks, want %d", tt.n, got, tt.blocks)
		}
		if bytes.Contains([]byte(cm), []byte(" "+strconv.Itoa(101)+" beginbfchar")) {
			t.Errorf("%d glyphs: a block exceeds 100 entries", tt.n)
		}
	}
}

func FuzzSubset(f *testing.F) {
	tb := testing.TB(f)
	f.Add(buildFont(tb, threeGlyphs()), uint16(2), uint16(1))
	f.Add(buildFont(tb, []glyphSpec{{}, {data: simpleGlyph()}, {data: compositeGlyph(1, 2)}, {data: compositeGlyph(3)}}), uint16(3), uint16(2))
	f.Add(buildFont(tb, []glyphSpec{{}, {data: compositeGlyph(2)}, {data: compositeGlyph(1)}}), uint16(1), uint16(0))
	real := loadTestFont(f, "LiberationSans-Regular.ttf")
	if tt, err := parseTTF(real.prog.Data); err == nil {
		var used glyphSet
		for g := uint16(1); g < 100; g++ {
			used.add(g)
		}
		if out, err := tt.subset(used); err == nil {
			f.Add(out, uint16(50), uint16(99))
		}
	}
	f.Fuzz(func(t *testing.T, data []byte, a, b uint16) {
		tt, err := parseTTF(data)
		if err != nil {
			return
		}
		var used glyphSet
		used.add(a)
		used.add(b)
		out, err := tt.subset(used)
		if err != nil {
			return
		}
		sub, err := parseTTF(out)
		if err != nil {
			t.Fatalf("subset does not parse: %v", err)
		}
		if sub.numGlyphs > tt.numGlyphs || sub.numGlyphs < 1 {
			t.Fatalf("numGlyphs %d from %d", sub.numGlyphs, tt.numGlyphs)
		}
		if checksum(out) != 0xB1B0AFBA {
			t.Fatal("bad whole-font checksum")
		}
		for _, g := range []uint16{a, b} {
			if int(g) >= sub.numGlyphs {
				continue
			}
			want, err1 := tt.glyph(int(g))
			got, err2 := sub.glyph(int(g))
			if err1 != nil || err2 != nil || !bytes.Equal(want, got[:len(want)]) {
				t.Fatalf("glyph %d altered", g)
			}
		}
	})
}
