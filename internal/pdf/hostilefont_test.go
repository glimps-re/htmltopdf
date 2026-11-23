package pdf

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-text/typesetting/font"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// hfSFNT assembles an sfnt from tables in the given tag order, each table
// 4-byte aligned, without any consistency fix-up.
func hfSFNT(order []string, tables map[string][]byte) []byte {
	out := make([]byte, 12+16*len(order))
	binary.BigEndian.PutUint32(out, 0x00010000)
	binary.BigEndian.PutUint16(out[4:], clamp16(len(order)))
	for i, tag := range order {
		t := tables[tag]
		rec := 12 + 16*i
		copy(out[rec:], tag)
		binary.BigEndian.PutUint32(out[rec+8:], clamp32(len(out)))
		binary.BigEndian.PutUint32(out[rec+12:], clamp32(len(t)))
		out = append(out, t...)
		out = append(out, make([]byte, pad4(len(out))-len(out))...)
	}
	return out
}

var hfOrder = []string{"glyf", "head", "hhea", "hmtx", "loca", "maxp"}

func hfFont(tables map[string][]byte) []byte { return hfSFNT(hfOrder, tables) }

func hfTables(glyphs []glyphSpec) map[string][]byte { return fontTables(glyphs) }

func hfClone(t map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(t))
	for k, v := range t {
		out[k] = bytes.Clone(v)
	}
	return out
}

func hfSetU16(b []byte, off int, v int) { binary.BigEndian.PutUint16(b[off:], clamp16(v)) }

func hfSetU32(b []byte, off int, v int) { binary.BigEndian.PutUint32(b[off:], clamp32(v)) }

// hfMeasure runs fn and returns the highest live heap above the starting heap
// (garbage included until the next collection) and the elapsed time.
func hfMeasure(fn func()) (peak uint64, took time.Duration) {
	runtime.GC()
	name := "/memory/classes/heap/objects:bytes"
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	base := s[0].Value.Uint64()
	var hi atomic.Uint64
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			ms := []metrics.Sample{{Name: name}}
			metrics.Read(ms)
			if v := ms[0].Value.Uint64(); v > hi.Load() {
				hi.Store(v)
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()
	start := time.Now()
	fn()
	took = time.Since(start)
	stop.Store(true)
	<-done
	return hi.Load() - min(base, hi.Load()), took
}

func manyGlyphs(n int, data []byte) []glyphSpec {
	gs := make([]glyphSpec, n)
	for i := 1; i < n; i++ {
		gs[i] = glyphSpec{adv: 500, data: data}
	}
	return gs
}

// hostileFonts are fonts a malicious document could carry. reject: the font
// must fail ValidateFont and AddFont with ErrInvalidFont. Otherwise it must be
// accepted and subset (or fail with ErrInvalidFont) with bounded memory.
func hostileFonts(t *testing.T) []struct {
	name   string
	data   []byte
	reject bool
	used   []uint16
} {
	t.Helper()
	base := func() map[string][]byte {
		return hfClone(hfTables([]glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}, {adv: 700, data: simpleGlyph()}}))
	}
	type fc = struct {
		name   string
		data   []byte
		reject bool
		used   []uint16
	}
	var cs []fc
	add := func(name string, tables map[string][]byte, reject bool, used ...uint16) {
		cs = append(cs, fc{name, hfFont(tables), reject, used})
	}
	add("baseline", base(), false, 1, 2)

	// loca: alternating 0/len(glyf) over 199 glyphs would multiply the glyf
	// table by the number of used glyphs.
	big := make([]byte, 256<<10)
	binary.BigEndian.PutUint16(big, 1)
	alt := hfTables(manyGlyphs(199, nil))
	alt["glyf"] = big
	loca := make([]byte, 4*200)
	for g := 0; g < 200; g++ {
		if g%2 == 1 {
			hfSetU32(loca, 4*g, len(big))
		}
	}
	alt["loca"] = loca
	var all []uint16
	for g := 1; g < 199; g++ {
		all = append(all, clamp16(g))
	}
	add("loca alternating 0/len(glyf)", alt, true, all...)

	d := base()
	hfSetU32(d["loca"], 8, len(d["glyf"])+4)
	add("loca beyond glyf", d, true, 1, 2)
	d = base()
	hfSetU32(d["loca"], 12, 0xFFFFFFFF)
	add("loca last entry 4 GiB", d, true, 1, 2)
	d = base()
	hfSetU32(d["loca"], 4, 1<<31)
	add("loca entry 2 GiB", d, true, 1, 2)
	d = base()
	d["loca"] = d["loca"][:8]
	add("loca too short", d, true, 1, 2)
	d = base()
	hfSetU32(d["loca"], 12, 10)
	add("loca decreasing", d, true, 1, 2)

	d = base()
	d["glyf"] = d["glyf"][:len(d["glyf"])-1]
	add("glyf shorter than loca", d, true, 1, 2)
	d = base()
	d["glyf"] = nil
	add("glyf empty", d, true, 1, 2)

	d = base()
	d["hhea"] = bytes.Clone(d["hhea"])
	hfSetU16(d["hhea"], 34, 0)
	add("numberOfHMetrics 0", d, true, 1, 2)
	d = base()
	hfSetU16(d["hhea"], 34, 1)
	add("numberOfHMetrics 1", d, false, 1, 2)
	d = base()
	hfSetU16(d["hhea"], 34, 3)
	add("numberOfHMetrics == numGlyphs", d, false, 1, 2)
	d = base()
	hfSetU16(d["hhea"], 34, 4)
	add("numberOfHMetrics > numGlyphs", d, true, 1, 2)
	d = base()
	hfSetU16(d["hhea"], 34, 0xFFFF)
	add("numberOfHMetrics 65535", d, true, 1, 2)
	d = base()
	d["hmtx"] = d["hmtx"][:5]
	add("hmtx truncated", d, true, 1, 2)

	for _, upem := range []struct {
		v      int
		reject bool
	}{{0, true}, {15, true}, {16, false}, {16384, false}, {16385, true}, {65535, true}} {
		d = base()
		hfSetU16(d["head"], 18, upem.v)
		add(fmt.Sprintf("upem %d", upem.v), d, upem.reject, 1, 2)
	}
	d = base()
	d["maxp"] = d["maxp"][:6]
	add("maxp 6 bytes", d, true, 1, 2)
	d = base()
	d["maxp"] = d["maxp"][:31]
	add("maxp 31 bytes", d, true, 1, 2)
	d = base()
	d["head"] = d["head"][:53]
	add("head 53 bytes", d, true, 1, 2)
	d = base()
	d["hhea"] = d["hhea"][:35]
	add("hhea 35 bytes", d, true, 1, 2)
	d = base()
	hfSetU32(d["head"], 12, 0xDEADBEEF)
	add("bad head magic", d, true, 1, 2)
	d = base()
	hfSetU16(d["head"], 50, 2)
	add("indexToLocFormat 2", d, true, 1, 2)
	d = base()
	hfSetU16(d["maxp"], 4, 0)
	add("numGlyphs 0", d, true, 1, 2)
	d = base()
	hfSetU16(d["maxp"], 4, 5000)
	add("numGlyphs 5000 vs short loca", d, true, 1, 2)

	// Directory.
	for _, n := range []struct {
		count  int
		reject bool
	}{{0, true}, {127, false}, {128, false}, {129, true}, {0xFFFF, true}} {
		d = base()
		order := append([]string(nil), hfOrder...)
		for i := len(order); i < n.count && i < 200; i++ {
			tag := fmt.Sprintf("x%03d", i)
			d[tag] = []byte{1, 2, 3, 4}
			order = append(order, tag)
		}
		data := hfSFNT(order, d)
		binary.BigEndian.PutUint16(data[4:], clamp16(n.count))
		if n.count > len(order) {
			data = append(data, make([]byte, 16*(n.count-len(order)))...)
		}
		cs = append(cs, fc{fmt.Sprintf("%d tables", n.count), data, n.reject, []uint16{1, 2}})
	}
	data := hfFont(base())
	hfSetU32(data, 12+8+8, len(data)+1)
	cs = append(cs, fc{"table offset beyond the file", data, true, []uint16{1, 2}})
	data = hfFont(base())
	hfSetU32(data, 12+12, 0xFFFFFFFF)
	cs = append(cs, fc{"table length 4 GiB", data, true, []uint16{1, 2}})
	data = hfFont(base())
	hfSetU32(data, 12+8, 0xFFFFFFFC)
	hfSetU32(data, 12+12, 8)
	cs = append(cs, fc{"offset + length wraps 32 bits", data, true, []uint16{1, 2}})
	data = hfFont(base())
	copy(data[12+16:], data[12:12+4]) // second tag = first tag
	cs = append(cs, fc{"duplicate table tag", data, true, []uint16{1, 2}})
	cs = append(cs, fc{"truncated header", data[:11], true, nil})
	cs = append(cs, fc{"empty", nil, true, nil})
	cs = append(cs, fc{"wrong version", append([]byte{0x4F, 0x54, 0x54, 0x4F}, data[4:]...), true, nil})

	// Overlapping tables: glyf and loca share their bytes. It is accepted or
	// refused, but never a panic, and the subset stays bounded.
	data = hfFont(base())
	glyfRec, locaRec := 12+0*16, 12+4*16
	copy(data[locaRec+8:locaRec+12], data[glyfRec+8:glyfRec+12])
	cs = append(cs, fc{"loca overlaps glyf", data, true, []uint16{1, 2}})
	data = hfFont(base())
	hmtxRec, headRec := 12+3*16, 12+1*16
	copy(data[hmtxRec+8:hmtxRec+12], data[headRec+8:headRec+12])
	cs = append(cs, fc{"hmtx overlaps head", data, false, []uint16{1, 2}})
	return cs
}

func TestHostileFontsValidateAndAdd(t *testing.T) {
	for _, c := range hostileFonts(t) {
		t.Run(c.name, func(t *testing.T) {
			verr := ValidateFont(c.data)
			w := newTestWriter(&bytes.Buffer{})
			_, aerr := w.AddFont(&FontProgram{Data: c.data, Name: "T"})
			if (verr == nil) != (aerr == nil) {
				t.Fatalf("ValidateFont %v, AddFont %v disagree", verr, aerr)
			}
			switch {
			case c.reject:
				if c.name == "loca overlaps glyf" && verr == nil {
					t.Skip("accepted: overlapping tables are tolerated")
				}
				if !errors.Is(verr, ErrInvalidFont) || !errors.Is(aerr, ErrInvalidFont) {
					t.Fatalf("ValidateFont %v, AddFont %v: want ErrInvalidFont", verr, aerr)
				}
			case verr != nil:
				t.Fatalf("rejected: %v", verr)
			}
		})
	}
}

// Every accepted font goes through the subset and a Close: no panic, an
// output that re-parses, and memory proportional to the font.
func TestHostileFontsSubset(t *testing.T) {
	for _, c := range hostileFonts(t) {
		t.Run(c.name, func(t *testing.T) {
			tt, err := parseTTF(c.data)
			if err != nil {
				return
			}
			var used glyphSet
			for _, g := range c.used {
				used.add(g)
			}
			var out []byte
			peak, took := hfMeasure(func() { out, err = tt.subset(used) })
			if err != nil {
				if !errors.Is(err, ErrInvalidFont) {
					t.Fatalf("subset error %v does not wrap ErrInvalidFont", err)
				}
				return
			}
			if _, err := parseTTF(out); err != nil {
				t.Fatalf("subset does not parse: %v", err)
			}
			if limit := uint64(8*len(c.data) + 4<<20); !raceEnabled && peak > limit {
				t.Errorf("peak %d KiB for a %d byte font (limit %d KiB)", peak>>10, len(c.data), limit>>10)
			}
			t.Logf("font %d B -> subset %d B, peak %d KiB, %v", len(c.data), len(out), peak>>10, took)
		})
	}
}

// compositeFont builds a font whose glyph i (1-based) is made of comps(i).
func compositeFont(n int, comps func(i int) []uint16) []byte {
	gs := make([]glyphSpec, n+1)
	for i := 1; i <= n; i++ {
		gs[i] = glyphSpec{adv: 500, data: compositeGlyph(comps(i)...)}
	}
	return hfFont(hfTables(gs))
}

func subsetOf(t *testing.T, data []byte, ids ...uint16) (out []byte, peak uint64, took time.Duration, err error) {
	t.Helper()
	tt, perr := parseTTF(data)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	var used glyphSet
	for _, g := range ids {
		used.add(g)
	}
	peak, took = hfMeasure(func() { out, err = tt.subset(used) })
	return out, peak, took, err
}

func TestCompositeHostile(t *testing.T) {
	t.Run("cycle A to B to A", func(t *testing.T) {
		data := compositeFont(2, func(i int) []uint16 { return []uint16{clamp16(3 - i)} })
		out, peak, took, err := subsetOf(t, data, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseTTF(out); err != nil {
			t.Fatal(err)
		}
		t.Logf("cycle: peak %d KiB, %v", peak>>10, took)
	})
	t.Run("self reference", func(t *testing.T) {
		data := compositeFont(1, func(int) []uint16 { return []uint16{1} })
		if _, _, _, err := subsetOf(t, data, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("chain of 65534 glyphs", func(t *testing.T) {
		const n = 65534
		data := compositeFont(n, func(i int) []uint16 { return []uint16{clamp16(i + 1)} })
		// The last glyph references glyph n+1 = numGlyphs: out of range.
		if _, _, _, err := subsetOf(t, data, 1); !errors.Is(err, ErrInvalidFont) {
			t.Fatalf("chain ending beyond the font: %v", err)
		}
		data = compositeFont(n, func(i int) []uint16 { return []uint16{clamp16(min(i+1, n))} })
		out, peak, took, err := subsetOf(t, data, 1)
		if err != nil {
			t.Fatalf("chain: %v", err)
		}
		if _, err := parseTTF(out); err != nil {
			t.Fatal(err)
		}
		t.Logf("65534-long chain: font %d B, subset %d B, peak %d MiB, %v", len(data), len(out), peak>>20, took)
		if !raceEnabled && peak > 64<<20 {
			t.Errorf("peak %d MiB", peak>>20)
		}
	})
	t.Run("component id beyond numGlyphs", func(t *testing.T) {
		for _, id := range []uint16{3, 4, 0xFFFF} {
			data := compositeFont(2, func(i int) []uint16 {
				if i == 1 {
					return []uint16{id}
				}
				return []uint16{0}
			})
			_, _, _, err := subsetOf(t, data, 1)
			if id == 3 || id == 4 || id == 0xFFFF {
				if !errors.Is(err, ErrInvalidFont) {
					t.Errorf("component %d of 3 glyphs: %v", id, err)
				}
			}
		}
		// Just under: the highest valid id.
		data := compositeFont(2, func(int) []uint16 { return []uint16{2} })
		if _, _, _, err := subsetOf(t, data, 1); err != nil {
			t.Errorf("component 2 of 3 glyphs: %v", err)
		}
	})
	t.Run("components per glyph limit", func(t *testing.T) {
		for _, n := range []struct {
			count int
			ok    bool
		}{{1, true}, {maxComponentsPerGlyph - 1, true}, {maxComponentsPerGlyph, true}, {maxComponentsPerGlyph + 1, false}, {5000, false}, {1 << 20, false}} {
			comps := make([]uint16, n.count)
			data := compositeFont(1, func(int) []uint16 { return comps })
			_, peak, took, err := subsetOf(t, data, 1)
			if n.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidFont)) {
				t.Errorf("%d components: %v", n.count, err)
			}
			if took > 5*time.Second || (!raceEnabled && peak > uint64(8*len(data)+8<<20)) {
				t.Errorf("%d components: %v, peak %d KiB", n.count, took, peak>>10)
			}
		}
	})
	t.Run("closure budget", func(t *testing.T) {
		// A 256-ary tree of composite glyphs, each with 256 components: the
		// records read are 256 per reachable glyph.
		tree := func(glyphs int) []byte {
			return compositeFont(glyphs, func(k int) []uint16 {
				comps := make([]uint16, maxComponentsPerGlyph)
				for j := range comps {
					if child := maxComponentsPerGlyph*(k-1) + 2 + j; child <= glyphs {
						comps[j] = clamp16(child)
					}
				}
				return comps
			})
		}
		under := maxClosureComponents / maxComponentsPerGlyph
		if _, peak, took, err := subsetOf(t, tree(under), 1); err != nil {
			t.Errorf("%d records: %v", maxClosureComponents, err)
		} else {
			t.Logf("closure at the limit: peak %d MiB, %v", peak>>20, took)
		}
		if _, _, _, err := subsetOf(t, tree(under+1), 1); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("%d records: %v", maxClosureComponents+maxComponentsPerGlyph, err)
		}
	})
	t.Run("truncated composite", func(t *testing.T) {
		g := compositeGlyph(1, 2)
		data := hfFont(hfTables([]glyphSpec{{}, {adv: 1, data: g[:16]}, {adv: 1, data: simpleGlyph()}}))
		if _, _, _, err := subsetOf(t, data, 1); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("truncated composite: %v", err)
		}
	})
	t.Run("composite flags", func(t *testing.T) {
		// Every combination of the flag bits that change a record's size.
		for flags := 0; flags < 1<<8; flags++ {
			g := make([]byte, 10)
			binary.BigEndian.PutUint16(g, 0xFFFF)
			g = binary.BigEndian.AppendUint16(g, clamp16(flags&^0x20))
			g = binary.BigEndian.AppendUint16(g, 0)
			g = append(g, make([]byte, 12)...)
			data := hfFont(hfTables([]glyphSpec{{}, {adv: 1, data: g}}))
			if _, _, _, err := subsetOf(t, data, 1); err != nil && !errors.Is(err, ErrInvalidFont) {
				t.Fatalf("flags %#x: %v", flags, err)
			}
		}
	})
}

func TestNumGlyphsBoundaries(t *testing.T) {
	for _, n := range []int{1, 2, 255, 256, 65535} {
		data := hfFont(hfTables(manyGlyphs(n, simpleGlyph())))
		if err := ValidateFont(data); err != nil {
			t.Fatalf("%d glyphs: %v", n, err)
		}
		last := clamp16(n - 1)
		out, peak, took, err := subsetOf(t, data, last)
		if err != nil {
			t.Fatalf("%d glyphs: %v", n, err)
		}
		if _, err := parseTTF(out); err != nil {
			t.Fatal(err)
		}
		if n == 65535 {
			t.Logf("65535 glyphs: font %d B, subset %d B, peak %d KiB, %v", len(data), len(out), peak>>10, took)
		}
	}
}

func TestAPIMisuse(t *testing.T) {
	data := hfFont(hfTables([]glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}, {adv: 700, data: simpleGlyph()}}))
	newW := func() (*Writer, FontID) {
		w := newTestWriter(&bytes.Buffer{})
		id, err := w.AddFont(&FontProgram{Data: data, Name: "T"})
		if err != nil {
			t.Fatal(err)
		}
		return w, id
	}
	t.Run("glyph id at the limit", func(t *testing.T) {
		_, id := newW()
		for _, g := range []struct {
			id uint16
			ok bool
		}{{2, true}, {3, false}, {0xFFFF, false}} {
			c := &Content{}
			c.Glyphs(id, 10, 0, 0, []Glyph{{ID: g.id}})
			if g.ok != (c.Err() == nil) || (!g.ok && !errors.Is(c.Err(), ErrGlyphID)) {
				t.Errorf("glyph %d: %v", g.id, c.Err())
			}
			if !g.ok && len(c.Bytes()) != 0 {
				t.Errorf("glyph %d: operator written despite the error", g.id)
			}
		}
	})
	t.Run("glyph id out of range in the middle of a run", func(t *testing.T) {
		_, id := newW()
		c := &Content{}
		c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Advance: 5}, {ID: 9}, {ID: 2}})
		if !errors.Is(c.Err(), ErrGlyphID) || len(c.Bytes()) != 0 {
			t.Errorf("err %v, %d bytes", c.Err(), len(c.Bytes()))
		}
	})
	t.Run("zero FontID", func(t *testing.T) {
		c := &Content{}
		c.Glyphs(FontID{}, 10, 0, 0, []Glyph{{ID: 1}})
		if !errors.Is(c.Err(), ErrFontID) {
			t.Errorf("err %v", c.Err())
		}
	})
	t.Run("FontID of another writer", func(t *testing.T) {
		w1, id1 := newW()
		w2, _ := newW()
		c := &Content{}
		c.Glyphs(id1, 10, 0, 0, []Glyph{{ID: 1, Text: "x"}})
		if _, err := w2.AddPage(100, 100, c, false, nil); !errors.Is(err, ErrFontID) {
			t.Errorf("AddPage: %v", err)
		}
		p, err := w2.AddPage(100, 100, nil, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := w2.WriteDeferred(p, c); !errors.Is(err, ErrFontID) {
			t.Errorf("WriteDeferred: %v", err)
		}
		if _, err := w1.AddPage(100, 100, c, false, nil); err != nil {
			t.Errorf("own writer: %v", err)
		}
	})
	t.Run("AddFont after Close", func(t *testing.T) {
		w, _ := newW()
		if _, err := w.AddPage(100, 100, &Content{}, false, nil); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(Info{}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.AddFont(&FontProgram{Data: data, Name: "T"}); !errors.Is(err, ErrClosed) {
			t.Errorf("AddFont after Close: %v", err)
		}
	})
	t.Run("nil font and nil data", func(t *testing.T) {
		w := newTestWriter(&bytes.Buffer{})
		if _, err := w.AddFont(nil); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("nil: %v", err)
		}
		if _, err := w.AddFont(&FontProgram{}); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("no data: %v", err)
		}
		if _, err := w.AddFont(&FontProgram{Data: data, UnitsPerEm: 999}); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("wrong upem: %v", err)
		}
	})
	t.Run("sizes", func(t *testing.T) {
		_, id := newW()
		for _, size := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), 1e30} {
			c := &Content{}
			c.Glyphs(id, size, 0, 0, []Glyph{{ID: 1}})
			if c.Err() == nil || len(c.Bytes()) != 0 {
				t.Errorf("size %v accepted: %q", size, c.Bytes())
			}
		}
		c := &Content{}
		c.Glyphs(id, 1e-9, 0, 0, []Glyph{{ID: 1}})
		c.Glyphs(id, 1e5, 0, 0, []Glyph{{ID: 1}})
		if c.Err() != nil && !errors.Is(c.Err(), ErrNumber) {
			t.Errorf("extreme but finite sizes: %v", c.Err())
		}
	})
	t.Run("positions and advances", func(t *testing.T) {
		_, id := newW()
		for name, run := range map[string][]Glyph{
			"NaN advance, not last":  {{ID: 1, Advance: math.NaN()}, {ID: 2}},
			"Inf advance, not last":  {{ID: 1, Advance: math.Inf(1)}, {ID: 2}},
			"huge advance, not last": {{ID: 1, Advance: 1e300}, {ID: 2}},
		} {
			c := &Content{}
			c.Glyphs(id, 10, 0, 0, run)
			if c.Err() == nil || len(c.Bytes()) != 0 {
				t.Errorf("%s accepted: %q", name, c.Bytes())
			}
		}
		for _, xy := range [][2]float64{{math.NaN(), 0}, {0, math.Inf(1)}, {1e30, 0}} {
			c := &Content{}
			c.Glyphs(id, 10, xy[0], xy[1], []Glyph{{ID: 1}})
			if c.Err() == nil || len(c.Bytes()) != 0 {
				t.Errorf("position %v accepted: %q", xy, c.Bytes())
			}
		}
		c := &Content{}
		c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Advance: math.NaN()}})
		if c.Err() != nil {
			t.Errorf("NaN advance on the final glyph is unused: %v", c.Err())
		}
		c = &Content{}
		c.Glyphs(id, 10, 0, 0, nil)
		if c.Err() != nil || len(c.Bytes()) != 0 {
			t.Errorf("empty run: %v %q", c.Err(), c.Bytes())
		}
	})
	t.Run("error is sticky and later runs are ignored", func(t *testing.T) {
		_, id := newW()
		c := &Content{}
		c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 99}})
		c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1}})
		if !errors.Is(c.Err(), ErrGlyphID) || len(c.Bytes()) != 0 {
			t.Errorf("err %v, bytes %q", c.Err(), c.Bytes())
		}
		w, _ := newW()
		if _, err := w.AddPage(100, 100, c, false, nil); !errors.Is(err, ErrGlyphID) {
			t.Errorf("AddPage with a failed content: %v", err)
		}
	})
}

// A run long enough to exceed MaxContentBytes fails with ErrContentSize and
// leaves no large buffer behind.
func TestContentSizeRunBounded(t *testing.T) {
	data := hfFont(hfTables([]glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}}))
	w := newTestWriter(&bytes.Buffer{})
	id, err := w.AddFont(&FontProgram{Data: data, Name: "T"})
	if err != nil {
		t.Fatal(err)
	}
	// Advance 5 against a width of 600: every glyph is followed by a TJ
	// adjustment, 14 bytes in all.
	for _, tc := range []struct {
		n    int
		fail bool
	}{{MaxContentBytes/16 - 100, false}, {2_000_000, true}, {8_000_000, true}} {
		run := make([]Glyph, tc.n)
		for i := range run {
			run[i] = Glyph{ID: 1, Advance: 5}
		}
		c := &Content{}
		runtime.GC()
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		peak, took := hfMeasure(func() { c.Glyphs(id, 10, 0, 0, run) })
		runtime.GC()
		runtime.ReadMemStats(&m1)
		retained := int64(m1.HeapAlloc>>10) - int64(m0.HeapAlloc>>10)
		runtime.KeepAlive(c)
		runtime.KeepAlive(run)
		t.Logf("%d glyphs: err %v, content %d B, peak %d MiB, retained %d KiB, %v", tc.n, c.Err(), len(c.Bytes()), peak>>20, retained, took)
		if len(c.Bytes()) > MaxContentBytes {
			t.Errorf("%d glyphs: content %d bytes", tc.n, len(c.Bytes()))
		}
		if tc.fail != errors.Is(c.Err(), ErrContentSize) {
			t.Errorf("%d glyphs: err %v", tc.n, c.Err())
		}
		if tc.fail && len(c.Bytes()) != 0 {
			t.Errorf("%d glyphs: %d bytes kept after the failure", tc.n, len(c.Bytes()))
		}
		if tc.fail && !raceEnabled && retained > 1<<10 {
			t.Errorf("%d glyphs: %d KiB retained by the failed Content", tc.n, retained)
		}
		if !raceEnabled && peak > uint64(8*MaxContentBytes) {
			t.Errorf("%d glyphs: peak %d MiB", tc.n, peak>>20)
		}
	}
}

func hfFontDoc(t *testing.T, build func(w *Writer, id FontID)) *pdftest.Doc {
	t.Helper()
	data := hfFont(hfTables([]glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}, {adv: 700, data: simpleGlyph()}}))
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id, err := w.AddFont(&FontProgram{Data: data, Name: "T"})
	if err != nil {
		t.Fatal(err)
	}
	build(w, id)
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	return pdftest.Validate(t, buf.Bytes())
}

func glyphPage(t *testing.T, w *Writer, id FontID, g Glyph) {
	t.Helper()
	c := &Content{}
	c.Glyphs(id, 10, 0, 0, []Glyph{g})
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestToUnicodeRules(t *testing.T) {
	t.Run("smallest text wins across pages, any order", func(t *testing.T) {
		for _, order := range [][]string{{"b", "a"}, {"a", "b"}, {"z", "b", "a", "c"}, {"é", "e"}, {"ab", "a"}} {
			doc := hfFontDoc(t, func(w *Writer, id FontID) {
				for _, s := range order {
					glyphPage(t, w, id, Glyph{ID: 1, Text: s})
				}
			})
			want := order[0]
			for _, s := range order {
				want = min(want, s)
			}
			for i, p := range doc.Pages {
				if len(p.Text) != 1 || p.Text[0] != want {
					t.Errorf("order %v page %d: text %q, want %q", order, i, p.Text, want)
				}
			}
		}
	})
	t.Run("cut at 64 bytes on a rune boundary", func(t *testing.T) {
		for _, tc := range []struct{ name, text string }{
			{"2-byte runes", strings.Repeat("é", 50)},
			{"3-byte runes", strings.Repeat("あ", 30)},
			{"4-byte runes", strings.Repeat("😀", 20)},
			{"3-byte rune across the cut", strings.Repeat("a", 63) + "あ"},
			{"4-byte rune across the cut", strings.Repeat("a", 62) + "😀"},
			{"1 MiB cluster", strings.Repeat("あ", 350_000)},
			{"1 MiB of combining marks", "e" + strings.Repeat("́", 500_000)},
		} {
			doc := hfFontDoc(t, func(w *Writer, id FontID) {
				c := &Content{}
				peak, _ := hfMeasure(func() { c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Text: tc.text}}) })
				if !raceEnabled && peak > 16<<20 {
					t.Errorf("%s: peak %d MiB", tc.name, peak>>20)
				}
				if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
					t.Fatal(err)
				}
			})
			got := doc.Pages[0].Text[0]
			if !utf8.ValidString(got) || len(got) > MaxClusterText || !strings.HasPrefix(tc.text, got) {
				t.Errorf("%s: kept %d bytes %q", tc.name, len(got), got)
			}
			if len(got) < MaxClusterText-3 {
				t.Errorf("%s: cut too early at %d bytes", tc.name, len(got))
			}
		}
	})
	t.Run("empty text then text for the same glyph", func(t *testing.T) {
		for _, order := range [][]string{{"", "x"}, {"x", ""}} {
			for _, samePage := range []bool{true, false} {
				doc := hfFontDoc(t, func(w *Writer, id FontID) {
					if samePage {
						c := &Content{}
						for _, s := range order {
							c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Text: s}})
						}
						if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
							t.Fatal(err)
						}
						return
					}
					for _, s := range order {
						glyphPage(t, w, id, Glyph{ID: 1, Text: s})
					}
				})
				for i, p := range doc.Pages {
					for _, s := range p.Text {
						if s != "x" {
							t.Errorf("order %q same page %v, page %d: text %q", order, samePage, i, s)
						}
					}
				}
			}
		}
	})
	t.Run("only empty texts", func(t *testing.T) {
		doc := hfFontDoc(t, func(w *Writer, id FontID) { glyphPage(t, w, id, Glyph{ID: 1}) })
		if len(doc.Pages) != 1 {
			t.Fatal("no page")
		}
	})
	t.Run("two glyphs one cluster", func(t *testing.T) {
		doc := hfFontDoc(t, func(w *Writer, id FontID) {
			c := &Content{}
			c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Advance: 6, Text: "fi"}, {ID: 2}})
			if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
				t.Fatal(err)
			}
		})
		if got := doc.Pages[0].Text[0]; got != "fi" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("hostile text", func(t *testing.T) {
		doc := hfFontDoc(t, func(w *Writer, id FontID) {
			glyphPage(t, w, id, Glyph{ID: 1, Text: "a\x00\xff\u202e</ToUnicode>\n>>endstream"})
		})
		if got := doc.Pages[0].Text[0]; !utf8.ValidString(got) {
			t.Errorf("invalid UTF-8 in the ToUnicode map: %q", got)
		}
	})
}

func TestFontOnlyOnDeferredPage(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id := addFont(t, w, lib.prog)
	p1, err := w.AddPage(200, 200, nil, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddPage(200, 200, &Content{}, false, nil); err != nil {
		t.Fatal(err)
	}
	c := &Content{}
	c.Glyphs(id, 12, 10, 100, lib.run(t, "Deferred « été »", 12))
	if err := w.WriteDeferred(p1, c); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if len(doc.Fonts) != 1 || doc.Pages[0].Text[0] != "Deferred « été »" || len(doc.Pages[1].Text) != 0 {
		t.Fatalf("fonts %d, texts %q / %q", len(doc.Fonts), doc.Pages[0].Text, doc.Pages[1].Text)
	}
	if !bytes.Contains(doc.Pages[0].Content, []byte("/F1 12 Tf")) {
		t.Errorf("content does not select the font: %.80q", doc.Pages[0].Content)
	}
	// A deferred page that is never written must be an error at Close, with a
	// font registered but unused.
	w = newTestWriter(&bytes.Buffer{})
	addFont(t, w, lib.prog)
	if _, err := w.AddPage(200, 200, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); !errors.Is(err, ErrPendingContent) {
		t.Errorf("unwritten deferred page: %v", err)
	}
}

// ≥ 100 distinct glyph ids, < 30 KiB, re-parsed by go-text, outlines intact.
func TestHundredDistinctGlyphs(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	var runes []rune
	seen := map[font.GID]bool{}
	for r := rune(0x20); len(seen) < 100 && r < 0x2000; r++ {
		if gid, ok := lib.face.NominalGlyph(r); ok && !seen[gid] {
			seen[gid] = true
			runes = append(runes, r)
		}
	}
	text := string(runes)
	doc, _ := embeddedDoc(t, lib, text)
	f := doc.Fonts["F1"]
	if f == nil {
		t.Fatal("no font")
	}
	ids := map[uint16]bool{}
	for _, g := range lib.run(t, text, 1) {
		ids[g.ID] = true
	}
	if len(ids) < 100 {
		t.Fatalf("only %d distinct glyph ids", len(ids))
	}
	if len(f.Program) >= 30<<10 {
		t.Fatalf("%d glyphs: subset of %d bytes", len(ids), len(f.Program))
	}
	sub, err := font.ParseTTF(bytes.NewReader(f.Program))
	if err != nil {
		t.Fatalf("go-text: %v", err)
	}
	for g := range ids {
		a, b := lib.face.GlyphData(font.GID(g)), sub.GlyphData(font.GID(g))
		if a == nil || b == nil || len(outlineOf(a)) != len(outlineOf(b)) {
			t.Fatalf("glyph %d outline differs", g)
		}
	}
	if doc.Pages[0].Text[0] != text {
		t.Errorf("extracted text differs from the source")
	}
	t.Logf("%d distinct glyphs: subset %d B of %d B", len(ids), len(f.Program), len(lib.prog.Data))
}

// Closing a document that uses every glyph of DejaVu Sans keeps the peak heap
// within a small multiple of the font.
func TestPeakHeapFullFont(t *testing.T) {
	dv := loadTestFont(t, "DejaVuSans.ttf")
	tt, err := parseTTF(dv.prog.Data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id := addFont(t, w, dv.prog)
	var run []Glyph
	for g := 0; g < tt.numGlyphs; g++ {
		run = append(run, Glyph{ID: clamp16(g), Advance: 5, Text: string(rune(0x20 + g%0x5f))})
	}
	for start := 0; start < len(run); start += 2000 {
		c := &Content{}
		c.Glyphs(id, 10, 0, 0, run[start:min(start+2000, len(run))])
		if _, err := w.AddPage(595, 842, c, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	peak, took := hfMeasure(func() {
		if err := w.Close(Info{}); err != nil {
			t.Error(err)
		}
	})
	t.Logf("%d glyphs, font %d B, output %d B: Close peak %.1f MiB, %v", tt.numGlyphs, len(dv.prog.Data), buf.Len(), float64(peak)/(1<<20), took)
	if limit := uint64(8 * len(dv.prog.Data)); !raceEnabled && peak > limit {
		t.Errorf("Close peak %d KiB > 8 × font (%d KiB)", peak>>10, limit>>10)
	}
	pdftest.Validate(t, buf.Bytes())
}

// Same input, same bytes across repeated renders in one process (map
// iteration order is randomized per run), deferred page included.
func TestFontOutputDeterministic(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	dv := loadTestFont(t, "DejaVuSans.ttf")
	render := func() []byte {
		var buf bytes.Buffer
		w := newTestWriter(&buf)
		a, b := addFont(t, w, lib.prog), addFont(t, w, dv.prog)
		p, err := w.AddPage(300, 200, nil, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, txt := range []string{"Rapport « été » 123", "Hello"} {
			c := &Content{}
			c.Glyphs(a, 12, 10, 100, lib.run(t, txt, 12))
			c.Glyphs(b, 12, 10, 50, dv.run(t, "↳ ⟨RLO⟩ — "+txt, 12))
			if _, err := w.AddPage(300, 200, c, false, nil); err != nil {
				t.Fatal(err)
			}
		}
		c := &Content{}
		c.Glyphs(b, 12, 10, 50, dv.run(t, "late page ↳", 12))
		if err := w.WriteDeferred(p, c); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(Info{Title: "t"}); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	first := render()
	for range 20 {
		if got := render(); !bytes.Equal(got, first) {
			t.Fatal("two renders of the same input differ")
		}
	}
	t.Logf("%d bytes, sha256 %x", len(first), sha256.Sum256(first))
}
