package pdf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"iter"
	"math"
	"math/bits"
	"sort"
)

// Bounds of the font subsetter.
const (
	// maxFontTables bounds the table directory of an accepted font.
	maxFontTables = 128
	// maxComponentsPerGlyph bounds the components of one composite glyph; real
	// fonts stay under about 20.
	maxComponentsPerGlyph = 256
	// maxClosureComponents bounds the component records read while collecting
	// the components of the drawn glyphs.
	maxClosureComponents = 1 << 20
	// MaxSubsetBytes bounds the embedded font program (the font size cap of
	// design §6.1).
	MaxSubsetBytes = 32 << 20
)

// ErrInvalidFont is returned for a font program that is not a usable TrueType
// font with glyf outlines.
var ErrInvalidFont = errors.New("pdf: invalid TrueType font")

func invalidFont(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidFont}, args...)...)
}

// glyphSet is a set of glyph ids.
type glyphSet [1024]uint64

func (s *glyphSet) add(g uint16)      { s[g>>6] |= 1 << (g & 63) }
func (s *glyphSet) has(g uint16) bool { return s[g>>6]>>(g&63)&1 != 0 }

// all yields the glyph ids of the set in increasing order.
func (s *glyphSet) all() iter.Seq[uint16] {
	return func(yield func(uint16) bool) {
		for i, w := range s {
			for ; w != 0; w &= w - 1 {
				if !yield(clamp16(i*64 + bits.TrailingZeros64(w))) {
					return
				}
			}
		}
	}
}

// max returns the highest glyph id of the set, or 0.
func (s *glyphSet) max() uint16 {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != 0 {
			return clamp16(i*64 + bits.Len64(s[i]) - 1)
		}
	}
	return 0
}

type tableRef struct{ off, len int }

// ttf is a validated view of a TrueType font file. Its slices alias data.
type ttf struct {
	data      []byte
	tables    map[string]tableRef
	numGlyphs int
	upem      int
	longLoca  bool
	numHM     int
	// advances holds the advance width of every glyph, in font units.
	advances []uint16
}

func clamp16(v int) uint16 { return uint16(min(max(v, 0), math.MaxUint16)) }

func clamp32(v int) uint32 { return uint32(min(max(v, 0), math.MaxUint32)) }

// u16 and u32 read big-endian fields as int; int is 64 bits wide on every
// platform the library targets, so u32 never overflows.
func u16(b []byte, off int) int { return int(binary.BigEndian.Uint16(b[off:])) }

func u32(b []byte, off int) int { return int(binary.BigEndian.Uint32(b[off:])) }

func (f *ttf) table(tag string) []byte {
	t, ok := f.tables[tag]
	if !ok {
		return nil
	}
	return f.data[t.off : t.off+t.len]
}

// parseTTF validates the directory and the tables needed to subset data. It
// never trusts an offset or a length, and checks that loca is monotonic and
// inside glyf, so that the glyph entries are disjoint slices of glyf.
func parseTTF(data []byte) (*ttf, error) {
	if len(data) < 12 {
		return nil, invalidFont("truncated header")
	}
	if v := u32(data, 0); v != 0x00010000 && v != 0x74727565 {
		return nil, invalidFont("not a TrueType sfnt (version %#x)", v)
	}
	n := u16(data, 4)
	if n == 0 || n > maxFontTables || 12+16*n > len(data) {
		return nil, invalidFont("bad table count %d", n)
	}
	f := &ttf{data: data, tables: make(map[string]tableRef, n)}
	for i := 0; i < n; i++ {
		rec := 12 + 16*i
		tag := string(data[rec : rec+4])
		off, length := u32(data, rec+8), u32(data, rec+12)
		if off > len(data) || length > len(data)-off {
			return nil, invalidFont("table %q out of bounds", tag)
		}
		if _, dup := f.tables[tag]; dup {
			return nil, invalidFont("duplicate table %q", tag)
		}
		f.tables[tag] = tableRef{off, length}
	}
	head, maxp, hhea := f.table("head"), f.table("maxp"), f.table("hhea")
	switch {
	case len(head) < 54, len(maxp) < 32, len(hhea) < 36:
		return nil, invalidFont("head, maxp (version 1.0) or hhea too short")
	case f.table("glyf") == nil || f.table("loca") == nil || f.table("hmtx") == nil:
		return nil, invalidFont("missing glyf, loca or hmtx")
	case u32(head, 12) != 0x5F0F3CF5:
		return nil, invalidFont("bad head magic number")
	}
	f.upem = u16(head, 18)
	if f.upem < 16 || f.upem > 16384 {
		return nil, invalidFont("units per em %d", f.upem)
	}
	switch format := u16(head, 50); format {
	case 0:
	case 1:
		f.longLoca = true
	default:
		return nil, invalidFont("indexToLocFormat %d", format)
	}
	f.numGlyphs = u16(maxp, 4)
	f.numHM = u16(hhea, 34)
	loca, hmtx := f.table("loca"), f.table("hmtx")
	entry := 2
	if f.longLoca {
		entry = 4
	}
	switch {
	case f.numGlyphs == 0:
		return nil, invalidFont("no glyph")
	case len(loca) < (f.numGlyphs+1)*entry:
		return nil, invalidFont("loca too short")
	case f.numHM < 1 || f.numHM > f.numGlyphs || len(hmtx) < 4*f.numHM+2*(f.numGlyphs-f.numHM):
		return nil, invalidFont("hmtx too short")
	}
	prev, glyfLen := 0, len(f.table("glyf"))
	for g := 0; g <= f.numGlyphs; g++ {
		off := f.locaAt(g)
		if off < prev || off > glyfLen {
			return nil, invalidFont("loca entry %d is %d (previous %d, glyf %d bytes)", g, off, prev, glyfLen)
		}
		prev = off
	}
	f.advances = make([]uint16, f.numGlyphs)
	for g := 0; g < f.numGlyphs; g++ {
		f.advances[g] = binary.BigEndian.Uint16(hmtx[4*min(g, f.numHM-1):])
	}
	return f, nil
}

// locaAt returns the byte offset of glyph g in glyf, g <= numGlyphs.
func (f *ttf) locaAt(g int) int {
	loca := f.table("loca")
	if f.longLoca {
		return u32(loca, 4*g)
	}
	return 2 * u16(loca, 2*g)
}

// glyph returns the glyf entry of g, empty for a glyph without outline.
func (f *ttf) glyph(g int) ([]byte, error) {
	if g < 0 || g >= f.numGlyphs {
		return nil, invalidFont("glyph %d beyond %d glyphs", g, f.numGlyphs)
	}
	return f.table("glyf")[f.locaAt(g):f.locaAt(g+1)], nil
}

// components appends the glyph ids a composite glyph is made of. A glyph with
// more than maxComponentsPerGlyph components is refused.
func components(dst []int, g []byte) ([]int, error) {
	if len(g) < 10 || u16(g, 0) < 0x8000 {
		return dst, nil
	}
	const (
		argsAreWords = 0x0001
		haveScale    = 0x0008
		moreComps    = 0x0020
		haveXYScale  = 0x0040
		have2x2      = 0x0080
	)
	off, n := 10, 0
	for {
		if off+4 > len(g) {
			return dst, invalidFont("truncated composite glyph")
		}
		if n++; n > maxComponentsPerGlyph {
			return dst, invalidFont("composite glyph with more than %d components", maxComponentsPerGlyph)
		}
		flags, idx := u16(g, off), u16(g, off+2)
		dst = append(dst, idx)
		off += 4
		if flags&argsAreWords != 0 {
			off += 4
		} else {
			off += 2
		}
		switch {
		case flags&haveScale != 0:
			off += 2
		case flags&haveXYScale != 0:
			off += 4
		case flags&have2x2 != 0:
			off += 8
		}
		if flags&moreComps == 0 {
			return dst, nil
		}
	}
}

// closure adds to used the components of every composite glyph of used, and
// glyph 0 (.notdef). Each glyph is read once; at most maxClosureComponents
// component records are read in all.
func (f *ttf) closure(used *glyphSet) error {
	used.add(0)
	var work, comps []int
	for g := range used.all() {
		work = append(work, int(g))
	}
	total := 0
	for len(work) > 0 {
		g := work[len(work)-1]
		work = work[:len(work)-1]
		data, err := f.glyph(g)
		if err != nil {
			return err
		}
		if comps, err = components(comps[:0], data); err != nil {
			return err
		}
		if total += len(comps); total > maxClosureComponents {
			return invalidFont("more than %d component records", maxClosureComponents)
		}
		for _, c := range comps {
			if c >= f.numGlyphs {
				return invalidFont("component glyph %d beyond %d glyphs", c, f.numGlyphs)
			}
			if id := clamp16(c); !used.has(id) {
				used.add(id)
				work = append(work, c)
			}
		}
	}
	return nil
}

func pad4(n int) int { return (n + 3) &^ 3 }

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i+4 <= len(b); i += 4 {
		sum += binary.BigEndian.Uint32(b[i:])
	}
	if r := len(b) % 4; r != 0 {
		var last [4]byte
		copy(last[:], b[len(b)-r:])
		sum += binary.BigEndian.Uint32(last[:])
	}
	return sum
}

// emptyCmap maps nothing: glyph ids are used as is (CIDToGIDMap Identity), but
// readers such as go-text refuse a font without a usable cmap. It is a
// Windows Unicode BMP format 4 subtable with only the final segment.
var emptyCmap = []byte{
	0, 0, 0, 1, // version, one encoding record
	0, 3, 0, 1, 0, 0, 0, 12, // platform 3, encoding 1, offset 12
	0, 4, 0, 24, 0, 0, // format 4, length 24, language 0
	0, 2, 0, 2, 0, 0, 0, 0, // segCountX2, searchRange, entrySelector, rangeShift
	0xff, 0xff, 0, 0, // endCode, reservedPad
	0xff, 0xff, // startCode
	0, 1, // idDelta
	0, 0, // idRangeOffset
}

// sfntTable is a table to write: its size is known before its content, which
// fill writes straight into the final buffer.
type sfntTable struct {
	tag  string
	size int
	fill func(dst []byte)
}

// writeSFNT writes an sfnt with the tables sorted by tag, in one buffer, and
// fixes head.checkSumAdjustment.
func writeSFNT(tables []sfntTable) []byte {
	sort.Slice(tables, func(i, j int) bool { return tables[i].tag < tables[j].tag })
	dir := 12 + 16*len(tables)
	size := dir
	for _, t := range tables {
		size += pad4(t.size)
	}
	out := make([]byte, size)
	binary.BigEndian.PutUint32(out, 0x00010000)
	binary.BigEndian.PutUint16(out[4:], clamp16(len(tables)))
	entrySel := bits.Len(uint(len(tables))) - 1
	binary.BigEndian.PutUint16(out[6:], clamp16(16<<entrySel))
	binary.BigEndian.PutUint16(out[8:], clamp16(entrySel))
	binary.BigEndian.PutUint16(out[10:], clamp16(16*len(tables)-(16<<entrySel)))
	off, headAt := dir, 0
	for i, t := range tables {
		rec := 12 + 16*i
		copy(out[rec:], t.tag)
		t.fill(out[off : off+t.size])
		binary.BigEndian.PutUint32(out[rec+4:], checksum(out[off:off+t.size]))
		binary.BigEndian.PutUint32(out[rec+8:], clamp32(off))
		binary.BigEndian.PutUint32(out[rec+12:], clamp32(t.size))
		if t.tag == "head" {
			headAt = off
		}
		off += pad4(t.size)
	}
	binary.BigEndian.PutUint32(out[headAt+8:], 0xB1B0AFBA-checksum(out))
	return out
}

// subset returns a TrueType font that keeps the glyph ids of the original; see
// subsetLimit.
func (f *ttf) subset(used glyphSet) ([]byte, error) { return f.subsetLimit(used, MaxSubsetBytes) }

// subsetLimit builds the subset and refuses one larger than limit bytes.
// Glyphs outside used (and outside the components of its composite glyphs) are
// emptied, and glyph ids above the highest kept one are dropped from maxp,
// loca and hmtx. Tables written: head hhea maxp hmtx loca glyf, an empty cmap
// and, when present, cvt, fpgm and prep. The font is assembled in a single
// buffer whose size is known beforehand.
func (f *ttf) subsetLimit(used glyphSet, limit int) ([]byte, error) {
	if err := f.closure(&used); err != nil {
		return nil, err
	}
	n := int(used.max()) + 1
	if n > f.numGlyphs {
		return nil, invalidFont("glyph %d beyond %d glyphs", n-1, f.numGlyphs)
	}
	// loca is monotonic, so the kept glyphs are disjoint slices of glyf: the
	// sum cannot exceed len(glyf) plus 3 bytes of padding per glyph.
	glyfSize := 0
	for g := range used.all() {
		data, err := f.glyph(int(g))
		if err != nil {
			return nil, err
		}
		glyfSize += pad4(len(data))
	}
	maxp, hmtx := f.table("maxp"), f.table("hmtx")
	tables := []sfntTable{
		{"cmap", len(emptyCmap), func(dst []byte) { copy(dst, emptyCmap) }},
		{"head", 54, func(dst []byte) {
			copy(dst, f.table("head")[:54])
			binary.BigEndian.PutUint32(dst[8:], 0)
			binary.BigEndian.PutUint16(dst[50:], 1)
		}},
		{"hhea", 36, func(dst []byte) {
			copy(dst, f.table("hhea")[:36])
			binary.BigEndian.PutUint16(dst[34:], clamp16(n))
		}},
		{"maxp", len(maxp), func(dst []byte) {
			copy(dst, maxp)
			binary.BigEndian.PutUint16(dst[4:], clamp16(n))
		}},
		{"hmtx", 4 * n, func(dst []byte) {
			for g := 0; g < n; g++ {
				off := 4*f.numHM + 2*(g-f.numHM)
				if g < f.numHM {
					off = 4*g + 2
				}
				binary.BigEndian.PutUint16(dst[4*g:], f.advances[g])
				copy(dst[4*g+2:4*g+4], hmtx[off:off+2])
			}
		}},
		{"loca", 4 * (n + 1), func(dst []byte) {
			off := 0
			for g := 0; g < n; g++ {
				binary.BigEndian.PutUint32(dst[4*g:], clamp32(off))
				if used.has(clamp16(g)) {
					off += pad4(f.locaAt(g+1) - f.locaAt(g))
				}
			}
			binary.BigEndian.PutUint32(dst[4*n:], clamp32(off))
		}},
		{"glyf", glyfSize, func(dst []byte) {
			off := 0
			for g := range used.all() {
				off += copy(dst[off:], f.table("glyf")[f.locaAt(int(g)):f.locaAt(int(g)+1)])
				off = pad4(off)
			}
		}},
	}
	for _, tag := range []string{"cvt ", "fpgm", "prep"} {
		if t := f.table(tag); t != nil {
			tables = append(tables, sfntTable{tag, len(t), func(dst []byte) { copy(dst, t) }})
		}
	}
	total := 12 + 16*len(tables)
	for _, t := range tables {
		total += pad4(t.size)
	}
	if total > limit {
		return nil, invalidFont("subset of %d bytes exceeds %d", total, limit)
	}
	return writeSFNT(tables), nil
}
