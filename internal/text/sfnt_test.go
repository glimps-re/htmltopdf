package text

import (
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
)

// sfntWith builds a font of size bytes with the given magic and table records
// (tag, offset, length).
func sfntWith(magic uint32, size int, tables ...[3]uint32) []byte {
	d := make([]byte, size)
	binary.BigEndian.PutUint32(d, magic)
	binary.BigEndian.PutUint16(d[4:], uint16(min(len(tables), 0xFFFF)))
	for i, tb := range tables {
		rec := d[12+16*i:]
		binary.BigEndian.PutUint32(rec, tb[0])
		binary.BigEndian.PutUint32(rec[8:], tb[1])
		binary.BigEndian.PutUint32(rec[12:], tb[2])
	}
	return d
}

func tag(s string) uint32 { return binary.BigEndian.Uint32([]byte(s)) }

// base are the tables describe requires (glyf, cmap) plus name, small and
// disjoint, so that a fixture reaches the loader's table reads.
func base() [][3]uint32 {
	return [][3]uint32{{tag("glyf"), 100, 4}, {tag("cmap"), 104, 4}, {tag("name"), 108, 4}}
}

// with replaces the table of the given tag in base by rec.
func with(rec [3]uint32) [][3]uint32 {
	ts := base()
	for i := range ts {
		if ts[i][0] == rec[0] {
			ts[i] = rec
		}
	}
	return ts
}

// wOFFBomb is a WOFF with glyf, cmap and a name table that claims to inflate to
// 4 GiB behind a valid zlib header.
func wOFFBomb() []byte {
	const dir, body = 44, 44 + 3*20
	d := make([]byte, body+2)
	copy(d, "wOFF")
	copy(d[4:], "\x00\x01\x00\x00")
	binary.BigEndian.PutUint16(d[12:], 3) // numTables
	for i, name := range []string{"cmap", "glyf", "name"} {
		e := d[dir+20*i:]
		copy(e, name)
		binary.BigEndian.PutUint32(e[4:], body) // offset
		binary.BigEndian.PutUint32(e[8:], 2)    // compLength
		orig := uint32(4)
		if name == "name" {
			orig = 0xFFFFFF00
		}
		binary.BigEndian.PutUint32(e[12:], orig) // origLength
	}
	d[body], d[body+1] = 0x78, 0x9c
	return d
}

// Every fixture has glyf and cmap and a family, so that describe does not refuse
// it before or after the loader reads a table: each case must fail with
// checkSfnt removed.
func TestHostileTableDirectoriesAreRefusedWithoutAllocating(t *testing.T) {
	huge := uint32(0xFFFFFF00)
	tests := []struct {
		name string
		data []byte
	}{
		{"table longer than the data", sfntWith(sfntTrueType, 200, with([3]uint32{tag("name"), 100, huge})...)},
		{"offset and length overflow 32 bits", sfntWith(sfntTrueType, 200, with([3]uint32{tag("name"), 0xFFFFFFF0, 0xFFFFFFF0})...)},
		{"tables overlapping over a large font", sfntWith(sfntTrueType, 8<<20,
			[][3]uint32{{tag("glyf"), 0, 8 << 20}, {tag("cmap"), 0, 8 << 20}, {tag("name"), 0, 8 << 20}, {tag("head"), 0, 8 << 20}, {tag("OS/2"), 0, 8 << 20}, {tag("GSUB"), 0, 8 << 20}}...)},
		{"WOFF inflating to 4 GiB", wOFFBomb()},
		{"OpenType CFF", sfntWith(tag("OTTO"), 200, base()...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := NewFontSet([]Source{{Data: tt.data, Family: "F"}}, Generic{}, nil)
			runtime.ReadMemStats(&after)
			if !errors.Is(err, ErrInvalidFont) {
				t.Fatalf("err = %v, want ErrInvalidFont", err)
			}
			if got := after.TotalAlloc - before.TotalAlloc; got > 1<<20 {
				t.Errorf("allocated %d bytes for a %d byte font", got, len(tt.data))
			}
		})
	}
}

// The directory checks themselves, whatever the loader would do.
func TestCheckSfntRefusals(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"shorter than a header", sfntWith(sfntTrueType, 200)[:8]},
		{"4 bytes", sfntWith(sfntTrueType, 200)[:4]},
		{"no table", sfntWith(sfntTrueType, 200)},
		{"65535 tables", func() []byte { d := sfntWith(sfntTrueType, 200); binary.BigEndian.PutUint16(d[4:], 0xFFFF); return d }()},
		{"truncated directory", sfntWith(sfntTrueType, 200, base()...)[:20]},
		{"offset past the end", sfntWith(sfntTrueType, 200, with([3]uint32{tag("name"), 0xFFFFFFF0, 32})...)},
		{"overlapping tables", sfntWith(sfntTrueType, 200, [][3]uint32{{tag("glyf"), 0, 200}, {tag("cmap"), 0, 200}}...)},
		{"WOFF", sfntWith(tag("wOFF"), 200, base()...)},
		{"OTTO", sfntWith(tag("OTTO"), 200, base()...)},
		{"ttcf", sfntWith(tag("ttcf"), 200, base()...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkSfnt(tt.data); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func TestReferenceFontsPassTheDirectoryCheck(t *testing.T) {
	for _, src := range referenceSources(t) {
		if err := checkSfnt(src.Data); err != nil {
			t.Errorf("%s: %v", src.Family, err)
		}
	}
}

func TestSfntWithTablesInsideTheDataIsAccepted(t *testing.T) {
	d := sfntWith(sfntTrueType, 200, [3]uint32{tag("glyf"), 100, 100}, [3]uint32{tag("cmap"), 100, 4})
	if err := checkSfnt(d); err != nil {
		t.Fatalf("a table ending at the data end must be accepted: %v", err)
	}
	if err := checkSfnt(sfntWith(sfntApple, 200, [3]uint32{tag("glyf"), 100, 4})); err != nil {
		t.Fatalf("Apple 'true' sfnt: %v", err)
	}
}

// tableOf returns the offset of the table with the given tag in a real font.
func tableOf(t *testing.T, d []byte, tg string) int {
	t.Helper()
	n := int(binary.BigEndian.Uint16(d[4:]))
	for i := range n {
		rec := d[12+16*i:]
		if string(rec[:4]) == tg {
			return int(binary.BigEndian.Uint32(rec[8:]))
		}
	}
	t.Fatalf("no %s table", tg)
	return 0
}

// A real font whose counts are set to 0xFFFF must not make the loader allocate
// more than a few times its size (pins go-text's behaviour across upgrades).
func TestHostileCountsInARealFontStayBounded(t *testing.T) {
	good := referenceSources(t)[0].Data
	put16 := func(d []byte, off int) { binary.BigEndian.PutUint16(d[off:], 0xFFFF) }
	tests := []struct {
		name  string
		patch func(d []byte)
	}{
		{"maxp numGlyphs", func(d []byte) { put16(d, tableOf(t, d, "maxp")+4) }},
		{"hhea numberOfHMetrics", func(d []byte) { put16(d, tableOf(t, d, "hhea")+34) }},
		{"cmap numTables", func(d []byte) { put16(d, tableOf(t, d, "cmap")+2) }},
		{"name count", func(d []byte) { put16(d, tableOf(t, d, "name")+2) }},
		{"GSUB lookupCount", func(d []byte) {
			g := tableOf(t, d, "GSUB")
			put16(d, g+int(binary.BigEndian.Uint16(d[g+8:])))
		}},
		{"GPOS lookupCount", func(d []byte) {
			g := tableOf(t, d, "GPOS")
			put16(d, g+int(binary.BigEndian.Uint16(d[g+8:])))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := append([]byte(nil), good...)
			tt.patch(d)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			if fs, err := NewFontSet([]Source{{Data: d}}, Generic{}, nil); err == nil {
				for _, f := range fs.Faces() {
					f.UnitsPerEm()
				}
			}
			runtime.ReadMemStats(&after)
			if got := after.TotalAlloc - before.TotalAlloc; got > uint64(8*len(d)) {
				t.Errorf("allocated %d bytes for a %d byte font", got, len(d))
			}
		})
	}
}

// A GSUB whose script records all point at the same script, whose language
// system declares 65535 feature indexes: go-text allocates the array once per
// record (found by fuzzing: 120 MB for a 400 KB font).
func TestOverlappingLanguageSystemsAreRefused(t *testing.T) {
	const scripts = 1000
	scriptList := 10
	scriptTable := scriptList + 2 + 6*scripts
	tbl := make([]byte, scriptTable+10+6)
	put := func(off, v int) { binary.BigEndian.PutUint16(tbl[off:], uint16(v&0xFFFF)) }
	put(0, 1)          // major version
	put(4, scriptList) // script list offset
	put(scriptList, scripts)
	for i := range scripts {
		put(scriptList+2+6*i+4, 2+6*scripts) // every record: the same script table
	}
	put(scriptTable+2, 1)         // one language system
	put(scriptTable+4+4, 10)      // at +10 from the script table
	put(scriptTable+10+4, 0xFFFF) // 65535 feature indexes
	if err := checkLayoutTable(tbl); err == nil {
		t.Fatal("a table declaring 130 MB of feature indexes must be refused")
	}
	tbl2 := append([]byte(nil), tbl...)
	binary.BigEndian.PutUint16(tbl2[scriptList:], 1)
	if err := checkLayoutTable(tbl2); err != nil {
		t.Fatalf("one script with 65535 feature indexes (128 KB) is within the bound: %v", err)
	}
}
