package text

import (
	"errors"
	"fmt"
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// ChunkRunes is the longest run shaped in one call of the shaping engine. The
// engine allocates about 100 bytes per rune per call: 16 Ki rather than 64 Ki
// keeps the heap of a 1.5 M-glyph word under 64 MiB (56 vs 69 MiB measured).
const ChunkRunes = 1 << 14

// MaxFontSize bounds FontSpec.Size, in CSS px.
const MaxFontSize = 1e7

// ErrInvalidSpec is returned for a non-finite or out-of-range FontSpec size.
var ErrInvalidSpec = errors.New("text: invalid font spec")

// Glyph is one shaped glyph. Cluster is the byte offset in the shaped text of
// the first rune of its cluster.
type Glyph struct {
	ID      uint16
	Advance float32 // CSS px at FontSpec.Size
	Cluster int32
}

// Run is a sequence of glyphs from one face at one size.
type Run struct {
	Face    *Face
	Size    float32
	Glyphs  []Glyph
	Advance float32 // sum of the glyph advances
}

// FontSpec is the font selection of a text.
type FontSpec struct {
	// Families is the CSS font-family list; generic names are resolved through
	// the FontSet.
	Families []string
	// Weight is the CSS weight, clamped to [1, 1000].
	Weight int
	Italic bool
	// Size is the font size in CSS px, in (0, MaxFontSize].
	Size float32
}

// Shaper shapes text for one render. It is not safe for concurrent use.
type Shaper struct {
	fs       *FontSet
	b        *budget.Budget
	hb       shaping.HarfbuzzShaper
	faces    map[*Face]*font.Face
	chain    []*Face
	resolved []*font.Face
	runs     []span
	win      []rune
	offs     []int32
	back     []Glyph
	// chunk and shapeFn are test seams: the chunk size and the shaping engine.
	chunk   int
	shapeFn func(shaping.Input) shaping.Output
	// last is the chain of the previous Shape call: consecutive texts of a
	// document mostly share their font.
	last    chainKey
	warned  map[rune]struct{}
	lastRn  rune
	lastIdx int
	haveRn  bool
}

type chainKey struct {
	families []string
	weight   int
	italic   bool
	valid    bool
}

func (k *chainKey) matches(families []string, weight int, italic bool) bool {
	if !k.valid || k.weight != weight || k.italic != italic || len(k.families) != len(families) {
		return false
	}
	for i, f := range families {
		if k.families[i] != f {
			return false
		}
	}
	return true
}

// Bounds of the spans of one Shape call.
const (
	// MaxSpans is the number of face or script changes of one text that are
	// shaped separately; the rest of the text joins the last span.
	MaxSpans = 1 << 16
	// workPerSpan is the Work charged per span: the overhead of an engine call.
	workPerSpan = 32
	// maxWarnedRunes bounds the missing-glyph runes remembered per Shaper.
	maxWarnedRunes = 256
)

// NewShaper returns a Shaper charging b.
func NewShaper(fs *FontSet, b *budget.Budget) *Shaper {
	s := &Shaper{fs: fs, b: b, faces: map[*Face]*font.Face{}, chunk: ChunkRunes}
	s.shapeFn = s.hb.ShapeNoExtents
	return s
}

// span is a run of clusters drawn by one face: runes [start, end), starting at
// byte offset bstart.
type span struct {
	start, end int
	bstart     int
	face       int
	script     language.Script
}

// face returns the render-private font.Face of f, or nil if f cannot be parsed.
func (s *Shaper) face(f *Face) *font.Face {
	if ff, ok := s.faces[f]; ok {
		return ff
	}
	var ff *font.Face
	if ft, err := f.load(); err == nil {
		ff = font.NewFace(ft)
	} else {
		s.b.Warn(budget.WarnShapingFallback, "unusable font "+f.Family)
	}
	s.faces[f] = ff
	return ff
}

// Extents returns the ascent, descent (positive) and line gap of f at size.
func (s *Shaper) Extents(f *Face, size float32) (ascent, descent, lineGap float32) {
	ff := s.face(f)
	if ff == nil {
		return 0, 0, 0
	}
	ext, ok := ff.FontHExtents()
	upem := float32(ff.Upem())
	if !ok || upem == 0 {
		return 0, 0, 0
	}
	k := size / upem
	return ext.Ascender * k, -ext.Descender * k, ext.LineGap * k
}

func extendsCluster(prev, r rune) bool {
	if r < 0x300 && prev != 0x200d {
		return false
	}
	return prev == 0x200d || r == 0x200d || (r >= 0x1f3fb && r <= 0x1f3ff) ||
		unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc)
}

// Shape appends to dst the runs of text in logical order. Characters are
// drawn by the first face of the spec's chain that covers their cluster; a
// character no face covers is drawn as .notdef of the primary face with a
// budget.WarnMissingGlyph. Errors are budget errors (*budget.LimitError, context).
func (s *Shaper) Shape(text string, spec FontSpec, dst []Run) ([]Run, error) {
	if text == "" {
		return dst, nil
	}
	if !(spec.Size > 0 && spec.Size <= MaxFontSize) || math.IsNaN(float64(spec.Size)) {
		return dst, ErrInvalidSpec
	}
	if len(text) > math.MaxInt32 {
		return dst, ErrInvalidSpec
	}
	if err := s.b.Charge(budget.Glyphs, int64(utf8.RuneCountInString(text))); err != nil {
		return dst, err
	}
	weight := min(max(spec.Weight, 1), 1000)
	if !s.last.matches(spec.Families, weight, spec.Italic) {
		s.chain = s.fs.chain(s.chain[:0], spec.Families, weight, spec.Italic)
		s.resolved = s.resolved[:0]
		for _, f := range s.chain {
			s.resolved = append(s.resolved, s.face(f))
		}
		s.last = chainKey{families: append(s.last.families[:0], spec.Families...), weight: weight, italic: spec.Italic, valid: true}
		s.haveRn = false
	}
	s.itemize(text)
	if len(s.runs) == MaxSpans {
		s.b.Warn(budget.WarnClamped, "spans")
	}
	if err := s.b.Charge(budget.Work, int64(len(s.runs))*workPerSpan); err != nil {
		return dst, err
	}
	s.back = make([]Glyph, 0, utf8.RuneCountInString(text))
	defer func() { s.back = nil }()
	for _, sp := range s.runs {
		var err error
		if dst, err = s.shapeSpan(dst, text, sp, spec.Size); err != nil {
			return dst, err
		}
	}
	return dst, nil
}

// itemize splits text into spans of one face and one script. Clusters are
// approximated by a base rune plus its marks, joiners and emoji modifiers.
func (s *Shaper) itemize(text string) {
	s.runs = s.runs[:0]
	var cur span
	open := false
	ri := 0
	for i := 0; i < len(text); {
		r, w := utf8.DecodeRuneInString(text[i:])
		j, rj, prev := i+w, ri+1, r
		for j < len(text) {
			nr, nw := utf8.DecodeRuneInString(text[j:])
			if !extendsCluster(prev, nr) {
				break
			}
			prev, j, rj = nr, j+nw, rj+1
		}
		k := s.faceFor(r)
		sc := language.LookupScript(r)
		if open && len(s.runs) < MaxSpans-1 && (k != cur.face || (sc.Strong() && cur.script.Strong() && sc != cur.script)) {
			s.runs = append(s.runs, cur)
			open = false
		}
		if !open {
			cur, open = span{start: ri, bstart: i, face: k, script: sc}, true
		} else if !cur.script.Strong() {
			cur.script = sc
		}
		cur.end = rj
		i, ri = j, rj
	}
	if open {
		s.runs = append(s.runs, cur)
	}
}

// faceFor returns the chain index of the face drawing r.
func (s *Shaper) faceFor(r rune) int {
	if s.haveRn && s.lastRn == r {
		return s.lastIdx
	}
	k := s.faceIndex(r)
	s.lastRn, s.lastIdx, s.haveRn = r, k, true
	return k
}

func (s *Shaper) faceIndex(r rune) int {
	for k, ff := range s.resolved {
		if ff == nil {
			continue
		}
		if _, ok := ff.NominalGlyph(r); ok {
			return k
		}
	}
	if unicode.IsGraphic(r) && !unicode.IsSpace(r) {
		if _, seen := s.warned[r]; !seen {
			if s.warned == nil {
				s.warned = map[rune]struct{}{}
			}
			if len(s.warned) < maxWarnedRunes {
				s.warned[r] = struct{}{}
			}
			s.b.Warn(budget.WarnMissingGlyph, fmt.Sprintf("U+%04X", r))
		}
	}
	return 0
}

// decode fills s.win and s.offs with the next n runes of text from byte offset
// bpos; offs[k] is the byte offset of rune k and offs[n] the end of the window.
// Only one chunk of runes is ever materialized.
func (s *Shaper) decode(text string, bpos, n int) {
	s.win, s.offs = s.win[:0], s.offs[:0]
	for k := 0; k < n; k++ {
		r, w := utf8.DecodeRuneInString(text[bpos:])
		s.win = append(s.win, r)
		s.offs = append(s.offs, clampInt32(bpos))
		bpos += w
	}
	s.offs = append(s.offs, clampInt32(bpos))
}

func (s *Shaper) shapeSpan(dst []Run, text string, sp span, size float32) ([]Run, error) {
	face := s.chain[sp.face]
	ff := s.resolved[sp.face]
	if ff == nil {
		ff = s.face(s.chain[len(s.chain)-1])
	}
	run := Run{Face: face, Size: size}
	first := len(s.back)
	scale := size / float32(ff.Upem())
	bpos, remaining := sp.bstart, sp.end-sp.start
	for remaining > 0 {
		n := min(remaining, s.chunk+1)
		s.decode(text, bpos, n)
		cut := n
		if n > s.chunk {
			cut = s.chunkEnd(s.win, 0, n)
		}
		if err := s.b.Poll(); err != nil {
			return dst, err
		}
		before := len(s.back)
		s.back = s.shapeChunk(s.back, ff, s.win[:cut], s.offs[:cut+1], sp.script, scale)
		if extra := int64(len(s.back)-before) - int64(cut); extra > 0 {
			if err := s.b.Charge(budget.Glyphs, extra); err != nil {
				return dst, err
			}
		}
		s.b.Checkpoint("shape-chunk")
		bpos, remaining = int(s.offs[cut]), remaining-cut
	}
	run.Glyphs = s.back[first:len(s.back):len(s.back)]
	for _, g := range run.Glyphs {
		run.Advance += g.Advance
	}
	return append(dst, run), nil
}

// chunkEnd returns where the chunk starting at start ends: the end of the
// span, else the last line-break opportunity within s.chunk runes, else the
// last cluster boundary.
func (s *Shaper) chunkEnd(runes []rune, start, end int) int {
	if end-start <= s.chunk {
		return end
	}
	limit := start + s.chunk
	var seg segmenter.Segmenter
	seg.Init(runes[start:limit])
	cut := start
	for it := seg.LineIterator(); it.Next(); {
		if l := it.Line(); start+l.Offset+len(l.Text) < limit {
			cut = start + l.Offset + len(l.Text)
		}
	}
	if cut > start {
		return cut
	}
	orig := limit
	for limit > start+1 && extendsCluster(runes[limit-1], runes[limit]) {
		limit--
	}
	if limit == start+1 && extendsCluster(runes[limit-1], runes[limit]) {
		// One cluster fills the whole chunk (a base with thousands of marks):
		// walking back would make 1-rune chunks and a quadratic run time, so
		// the pathological cluster is split at the chunk size.
		return orig
	}
	return limit
}

// shapeChunk shapes runes, whose byte offsets are offs, and appends the glyphs.
func (s *Shaper) shapeChunk(dst []Glyph, ff *font.Face, runes []rune, offs []int32, script language.Script, scale float32) []Glyph {
	if !script.Strong() {
		script = language.Latin
	}
	in := shaping.Input{
		Text: runes, RunStart: 0, RunEnd: len(runes), Direction: di.DirectionLTR,
		Face: ff, Size: fixed.Int26_6(ff.Upem()) << 6, Script: script,
	}
	out, ok := s.safeShape(in)
	if !ok {
		return s.nominal(dst, ff, runes, offs, scale)
	}
	appended := 0
	for _, g := range out.Glyphs {
		if g.TextIndex() >= len(runes) {
			return s.nominal(dst[:len(dst)-appended], ff, runes, offs, scale)
		}
		dst = append(dst, Glyph{
			ID:      glyphID(g.GlyphID),
			Advance: float32(g.Advance) / 64 * scale,
			Cluster: offs[g.TextIndex()],
		})
		appended++
	}
	return dst
}

// clampInt32 never triggers: Shape rejects texts longer than MaxInt32 bytes.
func clampInt32(i int) int32 {
	return int32(min(max(i, 0), math.MaxInt32))
}

// glyphID maps a glyph beyond the TrueType range to .notdef.
func glyphID[T ~uint32](g T) uint16 {
	if g > math.MaxUint16 {
		return 0
	}
	return uint16(g)
}

func (s *Shaper) safeShape(in shaping.Input) (out shaping.Output, ok bool) {
	defer func() {
		if recover() != nil {
			out, ok = shaping.Output{}, false
		}
	}()
	return s.shapeFn(in), true
}

// nominal draws one glyph per rune from the cmap and hmtx, without any layout
// feature.
func (s *Shaper) nominal(dst []Glyph, ff *font.Face, runes []rune, offs []int32, scale float32) []Glyph {
	s.b.Warn(budget.WarnShapingFallback, "shaper failed")
	for i, r := range runes {
		gid, _ := ff.NominalGlyph(r)
		dst = append(dst, Glyph{ID: glyphID(gid), Advance: ff.HorizontalAdvance(gid) * scale, Cluster: offs[i]})
	}
	return dst
}
