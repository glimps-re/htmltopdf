package pdf

import (
	"encoding/binary"
	"math"
	"testing"
)

func nan() float64 { return math.NaN() }

type glyphSpec struct {
	adv  uint16
	data []byte
}

// simpleGlyph is a glyph entry the subsetter copies verbatim.
func simpleGlyph() []byte {
	g := make([]byte, 24)
	binary.BigEndian.PutUint16(g, 1)
	return g
}

// compositeGlyph references the given component glyphs.
func compositeGlyph(comps ...uint16) []byte {
	g := make([]byte, 10)
	binary.BigEndian.PutUint16(g, 0xffff)
	for i, c := range comps {
		flags := uint16(0)
		if i < len(comps)-1 {
			flags = 0x0020
		}
		g = binary.BigEndian.AppendUint16(g, flags)
		g = binary.BigEndian.AppendUint16(g, c)
		g = append(g, 0, 0)
	}
	return g
}

// buildFont assembles a minimal TrueType font (long loca) around the glyphs.
func buildFont(tb testing.TB, glyphs []glyphSpec) []byte {
	tb.Helper()
	return assemble(fontTables(glyphs))
}

// fontTables returns the tables of buildFont, for tests that damage them.
func fontTables(glyphs []glyphSpec) map[string][]byte {
	var glyf, loca, hmtx []byte
	for _, g := range glyphs {
		loca = binary.BigEndian.AppendUint32(loca, clamp32(len(glyf)))
		glyf = append(glyf, g.data...)
		glyf = append(glyf, make([]byte, pad4(len(glyf))-len(glyf))...)
		hmtx = binary.BigEndian.AppendUint16(hmtx, g.adv)
		hmtx = binary.BigEndian.AppendUint16(hmtx, 0)
	}
	loca = binary.BigEndian.AppendUint32(loca, clamp32(len(glyf)))
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head, 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(head[18:], 1000)
	binary.BigEndian.PutUint16(head[50:], 1)
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[4:], 800)
	binary.BigEndian.PutUint16(hhea[34:], clamp16(len(glyphs)))
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp, 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], clamp16(len(glyphs)))
	return map[string][]byte{"head": head, "hhea": hhea, "maxp": maxp, "hmtx": hmtx, "loca": loca, "glyf": glyf}
}

func addFont(tb testing.TB, w *Writer, p *FontProgram) FontID {
	tb.Helper()
	id, err := w.AddFont(p)
	if err != nil {
		tb.Fatal(err)
	}
	return id
}

// assemble writes an sfnt from complete tables.
func assemble(tables map[string][]byte) []byte {
	var out []sfntTable
	for tag, data := range tables {
		out = append(out, sfntTable{tag, len(data), func(dst []byte) { copy(dst, data) }})
	}
	return writeSFNT(out)
}
