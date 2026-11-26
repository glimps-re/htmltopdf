package pdf

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// MaxClusterText is the longest source text kept for one glyph in the
// ToUnicode map, in bytes; longer clusters are cut on a rune boundary.
const MaxClusterText = 64

// Errors recorded by Content.Glyphs or returned for fonts.
var (
	ErrFontID  = errors.New("pdf: unknown font")
	ErrGlyphID = errors.New("pdf: glyph id beyond the font")
)

// FontProgram is a TrueType font to embed. Its metrics repeat text.FontInfo on
// purpose: pdf imports neither text nor css, and the caller copies the fields. Data holds the whole font file and
// must not be modified. The metrics are in font units; Name is sanitized
// before it appears in the PDF.
type FontProgram struct {
	Data       []byte
	Name       string
	UnitsPerEm uint16
	Ascent     int16
	Descent    int16
	CapHeight  int16
	BBox       [4]int16
	Italic     bool
	Fixed      bool
}

// FontID identifies a font added with Writer.AddFont. The zero value is
// invalid.
type FontID struct{ e *fontEntry }

type fontEntry struct {
	owner *Writer
	index int
	prog  *FontProgram
	tt    *ttf
	used  glyphSet
	uni   map[uint16]string
}

// Glyph is one glyph to draw. Advance is the distance to the next glyph at the
// run's size, in user space units. Text is the source text of the glyph's
// cluster, set on the first glyph of the cluster only: it feeds the ToUnicode
// map (one mapping per glyph id; on a conflict the smallest text wins).
type Glyph struct {
	ID      uint16
	Advance float64
	Text    string
}

type fontUse struct {
	e   *fontEntry
	uni map[uint16]string
}

func (e *fontEntry) width(g uint16) int {
	return int(math.Round(float64(e.tt.advances[g]) * 1000 / float64(e.tt.upem)))
}

func (e *fontEntry) name() string { return "F" + strconv.Itoa(e.index+1) }

// clusterText bounds a cluster text: it is cut early on a rune start so that
// validating it stays O(MaxClusterText), made valid UTF-8, then cut again.
func clusterText(s string) string {
	if pre := MaxClusterText + utf8.UTFMax; len(s) > pre {
		s = s[:runeStartAtOrBefore(s, pre)]
	}
	s = strings.ToValidUTF8(s, "\uFFFD")
	if len(s) <= MaxClusterText {
		return s
	}
	return s[:runeStartAtOrBefore(s, MaxClusterText)]
}

func runeStartAtOrBefore(s string, i int) int {
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// offer records t as the text of glyph g in m. An empty t only records that
// the glyph is used; otherwise the smallest text in byte order wins, which is
// independent of the order in which pages are written.
func offer(m map[uint16]string, g uint16, t string) {
	old, ok := m[g]
	if !ok || t != "" && (old == "" || t < old) {
		m[g] = t
	}
}

func (c *Content) fontUse(e *fontEntry) map[uint16]string {
	for _, u := range c.fonts {
		if u.e == e {
			return u.uni
		}
	}
	m := map[uint16]string{}
	c.fonts = append(c.fonts, fontUse{e: e, uni: m})
	return m
}

// Glyphs draws a run of glyphs of font f at size, with its origin at (x, y)
// of the current user space, which must have y pointing up: the text is not
// mirrored. The positions of the glyphs follow Advance, whatever the widths of
// the font. A run that would grow the stream past MaxContentBytes records
// ErrContentSize and is not written.
func (c *Content) Glyphs(f FontID, size, x, y float64, run []Glyph) {
	if c.err != nil || len(run) == 0 {
		return
	}
	e := f.e
	if e == nil {
		c.fail(ErrFontID)
		return
	}
	if !(size > 0) || !validNum(size) {
		c.fail(ErrNumber)
		return
	}
	tmp, err := c.appendRun(c.tmp[:0], e, size, x, y, run)
	if err != nil {
		c.fail(err)
	} else {
		c.buf = append(c.buf, tmp...)
	}
	// A huge run must not stay allocated in the scratch buffer.
	c.tmp = nil
	if cap(tmp) <= maxScratch {
		c.tmp = tmp[:0]
	}
}

// maxScratch is the largest scratch buffer a Content keeps between runs.
const maxScratch = 1 << 20

// appendRun encodes the text operators of run, recording the glyphs used.
func (c *Content) appendRun(dst []byte, e *fontEntry, size, x, y float64, run []Glyph) ([]byte, error) {
	uni := c.fontUse(e)
	room := MaxContentBytes - len(c.buf)
	dst = append(dst, "BT\n/"...)
	dst = append(dst, e.name()...)
	dst = append(dst, ' ')
	var err error
	if dst, err = appendNum(dst, size); err != nil {
		return dst, err
	}
	dst = append(dst, " Tf\n"...)
	for _, v := range []float64{x, y} {
		if dst, err = appendNum(dst, v); err != nil {
			return dst, err
		}
		dst = append(dst, ' ')
	}
	dst = append(dst, "Td\n[<"...)
	for i, g := range run {
		if int(g.ID) >= e.tt.numGlyphs {
			return dst, ErrGlyphID
		}
		dst = appendHex16(dst, g.ID)
		offer(uni, g.ID, clusterText(g.Text))
		if i < len(run)-1 {
			// TJ moves the next glyph by -n/1000 of the size: n corrects the
			// width the viewer knows (/W) into the advance the layout chose.
			if adj := float64(e.width(g.ID)) - g.Advance/size*1000; !(math.Abs(adj) < 0.05) {
				dst = append(dst, "> "...)
				if dst, err = appendNum(dst, adj); err != nil {
					return dst, err
				}
				dst = append(dst, " <"...)
			}
		}
		if len(dst) > room {
			return dst, ErrContentSize
		}
	}
	dst = append(dst, ">] TJ\nET\n"...)
	if len(dst) > room {
		return dst, ErrContentSize
	}
	return dst, nil
}

// ValidateFont reports whether data is a TrueType font that AddFont accepts:
// an sfnt with glyf outlines whose directory and tables are consistent. It also
// reads every glyph once, in time proportional to the size of glyf, so that a
// composite glyph that the subsetter would refuse (more than 256 components,
// a component beyond the font, truncated data) is refused here, once, and not
// when a page draws it. The error wraps ErrInvalidFont.
func ValidateFont(data []byte) error {
	f, err := parseTTF(data)
	if err != nil {
		return err
	}
	var comps []int
	for g := 0; g < f.numGlyphs; g++ {
		glyph, err := f.glyph(g)
		if err != nil {
			return err
		}
		if comps, err = components(comps[:0], glyph); err != nil {
			return invalidFont("glyph %d: %v", g, err)
		}
		for _, c := range comps {
			if c >= f.numGlyphs {
				return invalidFont("glyph %d: component %d beyond %d glyphs", g, c, f.numGlyphs)
			}
		}
	}
	return nil
}

// AddFont registers font f for embedding and returns its identifier. Only the
// glyphs drawn with Content.Glyphs are embedded, at Close. A nil or invalid
// font, or a UnitsPerEm that disagrees with the font, is refused with an error
// wrapping ErrInvalidFont.
//
// The caller owns the licence of the fonts it embeds: the OS/2 fsType
// embedding permissions are not checked, and the OS/2 table is not embedded.
func (w *Writer) AddFont(f *FontProgram) (FontID, error) {
	if w.closed {
		return FontID{}, ErrClosed
	}
	if f == nil {
		return FontID{}, fmt.Errorf("%w: nil font", ErrInvalidFont)
	}
	tt, err := parseTTF(f.Data)
	if err != nil {
		return FontID{}, err
	}
	if f.UnitsPerEm != 0 && int(f.UnitsPerEm) != tt.upem {
		return FontID{}, fmt.Errorf("%w: units per em %d, font says %d", ErrInvalidFont, f.UnitsPerEm, tt.upem)
	}
	e := &fontEntry{owner: w, index: len(w.fonts), prog: f, tt: tt, uni: map[uint16]string{}}
	w.fonts = append(w.fonts, e)
	return FontID{e}, nil
}

// mergeFonts adds the glyphs drawn by c to the fonts of w.
func (w *Writer) mergeFonts(c *Content) error {
	if err := w.checkShadings(c); err != nil {
		return err
	}
	if err := w.checkImages(c); err != nil {
		return err
	}
	for _, u := range c.fonts {
		if u.e.owner != w {
			return ErrFontID
		}
	}
	for _, u := range c.fonts {
		for g, t := range u.uni {
			u.e.used.add(g)
			offer(u.e.uni, g, t)
		}
	}
	return nil
}

func safeFontName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	if b.Len() == 0 {
		return "Font"
	}
	return b.String()
}

// subsetTag returns the six capital letters that prefix an embedded font name,
// derived from the font and its glyph set so that output is reproducible.
func (e *fontEntry) subsetTag(name string) string {
	h := sha256.New()
	h.Write([]byte(name))
	var word [8]byte
	for _, w := range e.used {
		binary.LittleEndian.PutUint64(word[:], w)
		h.Write(word[:])
	}
	sum := h.Sum(nil)
	tag := make([]byte, 6)
	for i := range tag {
		tag[i] = 'A' + sum[i]%26
	}
	return string(tag)
}

func scaled(v int16, upem int) float64 { return float64(v) * 1000 / float64(upem) }

// nameRef is an entry of a resource dictionary: a name and the object it names.
type nameRef struct {
	name string
	ref  Ref
}

// closeFonts embeds every font that drew a glyph and returns the entries of
// the shared /Font resource.
func (w *Writer) closeFonts() ([]nameRef, error) {
	var refs []nameRef
	for _, e := range w.fonts {
		if e.used == (glyphSet{}) {
			continue
		}
		ref, err := w.closeFont(e)
		if err != nil {
			return nil, err
		}
		refs = append(refs, nameRef{e.name(), ref})
	}
	return refs, nil
}

func (w *Writer) closeFont(e *fontEntry) (Ref, error) {
	sub, err := e.tt.subset(e.used)
	if err != nil {
		return 0, err
	}
	base := e.subsetTag(e.prog.Name) + "+" + safeFontName(e.prog.Name)
	file, desc, cid, tou, top := w.reserve(), w.reserve(), w.reserve(), w.reserve(), w.reserve()
	if err := w.emitStream(file, sub, "/Length1 "+strconv.Itoa(len(sub))+" "); err != nil {
		return 0, err
	}
	if err := w.emitStream(tou, toUnicode(e), ""); err != nil {
		return 0, err
	}
	if w.obj, err = w.fontDescriptor(w.obj[:0], e, base, file); err != nil {
		return 0, err
	}
	if err := w.emit(desc, w.obj); err != nil {
		return 0, err
	}
	w.obj = append(w.obj[:0], "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /"...)
	w.obj = append(w.obj, base...)
	w.obj = append(w.obj, " /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor "...)
	w.obj = appendRef(w.obj, desc)
	w.obj = append(w.obj, " /DW 1000 /CIDToGIDMap /Identity /W "...)
	w.obj = appendWidths(w.obj, e)
	w.obj = append(w.obj, " >>"...)
	if err := w.emit(cid, w.obj); err != nil {
		return 0, err
	}
	w.obj = append(w.obj[:0], "<< /Type /Font /Subtype /Type0 /BaseFont /"...)
	w.obj = append(w.obj, base...)
	w.obj = append(w.obj, " /Encoding /Identity-H /DescendantFonts ["...)
	w.obj = appendRef(w.obj, cid)
	w.obj = append(w.obj, "] /ToUnicode "...)
	w.obj = appendRef(w.obj, tou)
	w.obj = append(w.obj, " >>"...)
	return top, w.emit(top, w.obj)
}

func (w *Writer) fontDescriptor(dst []byte, e *fontEntry, base string, file Ref) ([]byte, error) {
	p, upem := e.prog, e.tt.upem
	flags := 4
	if p.Fixed {
		flags |= 1
	}
	angle := 0.0
	if p.Italic {
		flags |= 64
		angle = -12
	}
	dst = append(dst, "<< /Type /FontDescriptor /FontName /"...)
	dst = append(dst, base...)
	dst = append(dst, " /Flags "...)
	dst = strconv.AppendInt(dst, int64(flags), 10)
	dst = append(dst, " /FontBBox ["...)
	var err error
	for i, v := range p.BBox {
		if i > 0 {
			dst = append(dst, ' ')
		}
		if dst, err = appendNum(dst, scaled(v, upem)); err != nil {
			return dst, err
		}
	}
	dst = append(dst, "] /ItalicAngle "...)
	if dst, err = appendNum(dst, angle); err != nil {
		return dst, err
	}
	for _, kv := range []struct {
		key string
		v   int16
	}{{" /Ascent ", p.Ascent}, {" /Descent ", p.Descent}, {" /CapHeight ", p.CapHeight}} {
		dst = append(dst, kv.key...)
		if dst, err = appendNum(dst, scaled(kv.v, upem)); err != nil {
			return dst, err
		}
	}
	dst = append(dst, " /StemV 80 /FontFile2 "...)
	dst = appendRef(dst, file)
	return append(dst, " >>"...), nil
}

// appendWidths writes /W for the drawn glyphs: runs of consecutive ids as
// "first [w1 w2 ...]".
func appendWidths(dst []byte, e *fontEntry) []byte {
	dst = append(dst, '[')
	open := false
	last := -2
	for g := range e.used.all() {
		id := int(g)
		if id != last+1 {
			if open {
				dst = append(dst, "] "...)
			}
			dst = strconv.AppendInt(dst, int64(id), 10)
			dst = append(dst, " ["...)
			open = true
		} else {
			dst = append(dst, ' ')
		}
		dst = strconv.AppendInt(dst, int64(e.width(g)), 10)
		last = id
	}
	if open {
		dst = append(dst, ']')
	}
	return append(dst, ']')
}

// toUnicode builds the ToUnicode CMap of e: one bfchar per drawn glyph that
// has a source text.
func toUnicode(e *fontEntry) []byte {
	out := []byte("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
		"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	var gids []uint16
	for g := range e.used.all() {
		if e.uni[g] != "" {
			gids = append(gids, g)
		}
	}
	for len(gids) > 0 {
		n := min(len(gids), 100)
		out = strconv.AppendInt(out, int64(n), 10)
		out = append(out, " beginbfchar\n"...)
		for _, g := range gids[:n] {
			out = append(out, '<')
			out = appendHex16(out, g)
			out = append(out, "> <"...)
			for _, r := range e.uni[g] {
				var buf [2]uint16
				for _, u := range utf16.AppendRune(buf[:0], r) {
					out = appendHex16(out, u)
				}
			}
			out = append(out, ">\n"...)
		}
		out = append(out, "endbfchar\n"...)
		gids = gids[n:]
	}
	return append(out, "endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"...)
}
