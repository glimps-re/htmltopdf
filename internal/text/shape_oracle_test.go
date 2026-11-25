package text

import (
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// wrapperChunk is the oracle of the differential tests: shapeChunk as it was
// before the engine buffer was driven directly, through shaping.HarfbuzzShaper.
func wrapperChunk() func(*Shaper, []Glyph, *font.Face, []rune, []int32, language.Script, float32) []Glyph {
	hb := &shaping.HarfbuzzShaper{}
	return func(s *Shaper, dst []Glyph, ff *font.Face, runes []rune, offs []int32, script language.Script, scale float32) []Glyph {
		in := shaping.Input{
			Text: runes, RunStart: 0, RunEnd: len(runes), Direction: di.DirectionLTR,
			Face: ff, Size: fixed.Int26_6(ff.Upem()) << 6, Script: script,
		}
		out := hb.ShapeNoExtents(in)
		appended := 0
		for _, g := range out.Glyphs {
			if g.TextIndex() >= len(runes) {
				return s.nominal(dst[:len(dst)-appended], ff, runes, offs, scale)
			}
			dst = append(dst, Glyph{ID: glyphID(g.GlyphID), Advance: float32(g.Advance) / 64 * scale, Cluster: offs[g.TextIndex()]})
			appended++
		}
		return dst
	}
}
