package layout

import (
	"math"
	"sort"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/text"
)

type extKey struct {
	face *text.Face
	size float32
}

type extVal struct{ asc, desc, gap float64 }

// extents returns the ascent, descent and line gap of a face at a size, in px,
// each rounded to a whole pixel as Chrome does before it sums them into the
// normal line height (a 0.6 px difference per line adds up over a report).
func (l *layouter) extents(face *text.Face, size float32) extVal {
	k := extKey{face, size}
	if v, ok := l.ext[k]; ok {
		return v
	}
	a, d, g := l.sh.Extents(face, size)
	v := extVal{math.Round(float64(a)), math.Round(float64(d)), math.Round(float64(g))}
	l.ext[k] = v
	return v
}

// shapedText is the shaping of one text box, kept for the paragraph. Its glyphs
// stay in the runs the shaper returned, numbered in one sequence; segments are
// the words between break opportunities, found on demand.
type shapedText struct {
	info *info
	text string
	runs []text.Run
	// first[i] is the number of glyphs before runs[i]; clusters[i] is the cluster
	// of its first glyph. A released run has no glyphs left.
	first    []int
	clusters []int32
	released int
	// ends are the end offsets of the segments, the last being len(text).
	// lastBreak: a line may end after the last one (it ends with a space).
	ends      []int32
	lastBreak bool
	ls        float64
	// nowrap: the text never wraps (white-space: nowrap or pre).
	nowrap bool
}

// locate returns the run and the index in it of the global glyph g.
func (s *shapedText) locate(g int) (run, i int) {
	run = sort.Search(len(s.runs), func(r int) bool { return s.first[r+1] > g })
	return run, g - s.first[run]
}

// glyphIndex returns the index of the first glyph whose cluster starts at or
// after byte offset off, among all the glyphs.
func (s *shapedText) glyphIndex(off int) int {
	r := sort.Search(len(s.runs), func(r int) bool { return int(s.clusters[r]) > off }) - 1
	if r < 0 {
		return 0
	}
	gl := s.runs[r].Glyphs
	i := sort.Search(len(gl), func(i int) bool { return int(gl[i].Cluster) >= off })
	return s.first[r] + i
}

// width returns the advance, letter-spacing included, of the glyphs of [a, b).
func (s *shapedText) width(a, b int) float64 {
	ga, gb := s.glyphIndex(a), s.glyphIndex(b)
	var w float64
	for g := ga; g < gb; {
		r, i := s.locate(g)
		gl := s.runs[r].Glyphs
		end := min(len(gl), i+gb-g)
		for _, gly := range gl[i:end] {
			w += float64(gly.Advance) + s.ls
		}
		g += end - i
	}
	return w
}

// widthWithin is width(a, b) stopped as soon as it exceeds limit: ok is false
// then and w is only a lower bound. A long word is never measured past the
// room of a line.
func (s *shapedText) widthWithin(a, b int, limit float64) (w float64, ok bool) {
	ga, gb := s.glyphIndex(a), s.glyphIndex(b)
	for g := ga; g < gb; {
		r, i := s.locate(g)
		gl := s.runs[r].Glyphs
		end := min(len(gl), i+gb-g)
		for _, gly := range gl[i:end] {
			if w += float64(gly.Advance) + s.ls; w > limit {
				return w, false
			}
		}
		g += end - i
	}
	return w, true
}

// glyph returns the global glyph g.
func (s *shapedText) glyph(g int) text.Glyph {
	r, i := s.locate(g)
	return s.runs[r].Glyphs[i]
}

// segment returns the bounds of the segment that contains offset off, and
// whether it is the last one.
func (s *shapedText) segment(off int) (start, end int, last bool) {
	i := sort.Search(len(s.ends), func(i int) bool { return int(s.ends[i]) > off })
	if i >= len(s.ends) {
		i = len(s.ends) - 1
	}
	if i > 0 {
		start = int(s.ends[i-1])
	}
	return start, int(s.ends[i]), i == len(s.ends)-1
}

// releaseBefore drops the glyphs of the runs that end before offset off: the
// break token has passed them.
func (s *shapedText) releaseBefore(off int) {
	for s.released < len(s.runs)-1 && int(s.clusters[s.released+1]) <= off {
		s.runs[s.released].Glyphs = nil
		s.released++
	}
}

type unitKind uint8

const (
	uText unitKind = iota
	uOpen
	uClose
	uBreak
	// uAtom is an atomic inline: laid out as a block, placed as one box.
	uAtom
)

// info is what a text box or an inline box contributes to its lines, computed
// once.
type info struct {
	box   *box.Box
	href  string
	decor style.TextDecoration
	color css.Color
	// asc and desc are the content area; top and bottom the line box
	// contribution once the half-leading of the line-height is added.
	asc, desc, top, bottom float64
	// ml and mr are the horizontal margins of an inline box: outside its background.
	ml, mr float64
	// rise is the vertical-align: <length> offset, up, of the enclosing inline
	// boxes.
	rise float64
}

// unit is an element of a paragraph: a text box, an inline box edge, or a
// forced break. Units are immutable once built.
type unit struct {
	kind unitKind
	st   *shapedText
	info *info
	// w is the advance of an inline box edge, or the margin box width of an atom.
	w    float64
	atom *atomBox
}

// cursor is a position in a paragraph: a unit and, in a text unit, a byte
// offset.
type cursor struct{ unit, off int }

type paragraph struct {
	owner *box.Box
	units []unit
	// released is the number of leading units whose shaping was dropped.
	released int
	// strutTop and strutBottom are the line box contribution of the block's own
	// font and line-height: every line has at least that height.
	strutTop, strutBottom float64
	align                 style.TextAlign
	// breakWord: a word wider than the line may be broken anywhere.
	breakWord bool
	// collapsible: white space collapses and trailing spaces are removed
	// (white-space: normal and nowrap); false for pre.
	collapsible bool
	// nowrap: no soft wrap opportunity, around atomic inlines either.
	nowrap bool
	// mid is the height of the middle of the lowercase letters above the
	// baseline, for vertical-align: middle (half of an x-height of 0.5 em).
	mid float64
}

// lineHeightParts returns the contribution above and below the baseline of a
// font box of content area asc+desc under the computed line-height.
func lineHeightParts(st *style.Computed, asc, desc, gap float64) (top, bottom float64) {
	var lh float64
	switch st.LineHeight.Kind {
	case style.LineHeightNumber:
		lh = st.LineHeight.Value * st.FontSize
	case style.LineHeightPx:
		lh = st.LineHeight.Value
	default:
		lh = asc + desc + gap
	}
	leading := lh - (asc + desc)
	return asc + leading/2, desc + leading/2
}

func fontSpec(st *style.Computed) text.FontSpec {
	return text.FontSpec{
		Families: st.FontFamily,
		Weight:   st.FontWeight,
		Italic:   st.FontStyle != style.FontNormal,
		Size:     float32(st.FontSize),
	}
}

// paragraphOf returns the paragraph of block b, shaped once and kept while it
// is the one being laid out (it may span pages).
func (l *layouter) paragraphOf(b *box.Box, decor style.TextDecoration, cbw float64) (*paragraph, error) {
	if l.para != nil && l.para.owner == b {
		return l.para, nil
	}
	l.para = nil
	st := b.Style
	p := &paragraph{
		owner:       b,
		align:       st.TextAlign,
		breakWord:   st.OverflowWrap != style.WrapNormal,
		collapsible: st.WhiteSpace != style.WhiteSpacePre,
		nowrap:      st.WhiteSpace != style.WhiteSpaceNormal,
		mid:         st.FontSize / 4,
	}
	_, p.strutTop, p.strutBottom = l.boxMetrics(st)
	if err := l.flatten(p, b.Children, decor, cbw, 0); err != nil {
		return nil, err
	}
	l.para = p
	return p, nil
}

type primary struct {
	face *text.Face
	size float32
}

// primaryFace returns the face the font of st resolves to for a space, used
// for struts and inline box metrics; it is memoized per style. The probe is
// shaped on a scratch budget: it must not count in MaxGlyphs.
func (l *layouter) primaryFace(st *style.Computed) (*text.Face, float32) {
	if p, ok := l.primaries[st]; ok {
		return p.face, p.size
	}
	var p primary
	if runs, err := l.probe.Shape(" ", fontSpec(st), nil); err == nil && len(runs) > 0 {
		p = primary{runs[0].Face, runs[0].Size}
	}
	l.primaries[st] = p
	return p.face, p.size
}

func (l *layouter) flatten(p *paragraph, boxes []*box.Box, decor style.TextDecoration, cbw, rise float64) error {
	for _, c := range boxes {
		switch c.Kind {
		case box.KindText:
			if err := l.textUnit(p, c, decor, rise); err != nil {
				return err
			}
		case box.KindLineBreak:
			p.units = append(p.units, unit{kind: uBreak})
		case box.KindInlineBlock, box.KindSVG:
			un, err := l.atomUnit(p, c, decor, cbw, rise)
			if err != nil {
				return err
			}
			p.units = append(p.units, un)
		case box.KindInline:
			cst := c.Style
			d := decor | cst.TextDecoration
			openW, closeW := inlineEdges(cst, cbw, !c.Continuation)
			inRise := rise
			if cst.VerticalAlign == style.VAlignLength {
				inRise = clampCoord(rise + cst.VAlignOffset.Value)
			}
			in := &info{box: c, href: c.Href, decor: d, color: cst.Color, rise: inRise}
			if !c.Continuation {
				in.ml = clampCoord(px(cst.Margin[style.Left], cbw))
			}
			in.mr = clampCoord(px(cst.Margin[style.Right], cbw))
			var e extVal
			e, in.top, in.bottom = l.boxMetrics(cst)
			in.asc, in.desc = e.asc, e.desc
			p.units = append(p.units, unit{kind: uOpen, info: in, w: openW})
			if err := l.flatten(p, c.Children, d, cbw, inRise); err != nil {
				return err
			}
			p.units = append(p.units, unit{kind: uClose, info: in, w: closeW})
		}
	}
	return nil
}

// boxMetrics returns the content area and the line box contribution (above and
// below the baseline) of the font of st.
func (l *layouter) boxMetrics(st *style.Computed) (e extVal, top, bottom float64) {
	if st.FontSize <= 0 {
		return extVal{}, 0, 0
	}
	face, size := l.primaryFace(st)
	if face == nil {
		return extVal{}, 0, 0
	}
	e = l.extents(face, size)
	top, bottom = lineHeightParts(st, e.asc, e.desc, e.gap)
	return e, top, bottom
}

// inlineEdges returns the horizontal margin, border and padding before and after
// the content of an inline box; the start edge exists on the first fragment only.
func inlineEdges(st *style.Computed, cbw float64, first bool) (open, closeW float64) {
	if first {
		open = clampCoord(px(st.Margin[style.Left], cbw)) + st.Border[style.Left].Width + padPx(st.Padding[style.Left], cbw)
	}
	closeW = clampCoord(px(st.Margin[style.Right], cbw)) + st.Border[style.Right].Width + padPx(st.Padding[style.Right], cbw)
	return open, closeW
}

// textUnit adds the shaped text box c to the paragraph.
func (l *layouter) textUnit(p *paragraph, c *box.Box, decor style.TextDecoration, rise float64) error {
	sh, err := l.shapeBox(c, decor)
	if err != nil || sh == nil {
		return err
	}
	sh.info.rise = rise
	sh.info.top += rise
	sh.info.bottom -= rise
	p.units = append(p.units, unit{kind: uText, st: sh, info: sh.info})
	return nil
}

// shapeBox shapes a text box. Its segments are not materialized: the result
// keeps the break offsets and the line breaker walks them. It is nil for a box
// without anything to draw.
func (l *layouter) shapeBox(c *box.Box, decor style.TextDecoration) (*shapedText, error) {
	st := c.Style
	if c.Text == "" || st.FontSize <= 0 {
		return nil, nil
	}
	// Shaping is charged by the shaper (glyphs); the rest of the work by length.
	if err := l.b.Charge(budget.Work, int64(len(c.Text)/64+1)); err != nil {
		return nil, err
	}
	runs, err := l.sh.Shape(c.Text, fontSpec(st), nil)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	sh := &shapedText{text: c.Text, runs: runs, ls: st.LetterSpacing, nowrap: st.WhiteSpace != style.WhiteSpaceNormal}
	sh.first = make([]int, len(runs)+1)
	sh.clusters = make([]int32, len(runs))
	var asc, desc, gap float64
	for i, r := range runs {
		e := l.extents(r.Face, r.Size)
		asc, desc, gap = math.Max(asc, e.asc), math.Max(desc, e.desc), math.Max(gap, e.gap)
		sh.first[i+1] = sh.first[i] + len(r.Glyphs)
		if len(r.Glyphs) > 0 {
			sh.clusters[i] = r.Glyphs[0].Cluster
		} else if i > 0 {
			sh.clusters[i] = sh.clusters[i-1]
		}
	}
	top, bottom := lineHeightParts(st, asc, desc, gap)
	sh.info = &info{box: c, href: c.Href, decor: decor, color: st.Color, asc: asc, desc: desc, top: top, bottom: bottom}

	wrap := !sh.nowrap
	l.breaks = l.breaks[:0]
	if wrap {
		l.breaks = text.Breaks(c.Text, l.breaks)
	}
	if err := l.b.Charge(budget.Work, int64(len(l.breaks))); err != nil {
		return nil, err
	}
	if wrap && st.WordBreak == style.WordBreakAll {
		// One end per rune at most.
		if err := l.b.Charge(budget.Work, int64(len(c.Text))); err != nil {
			return nil, err
		}
		sh.ends = appendBreakAll(make([]int32, 0, len(c.Text)+1), c.Text, l.breaks)
	} else {
		sh.ends = make([]int32, 0, len(l.breaks)+1)
		for _, br := range l.breaks {
			sh.ends = appendEnd(sh.ends, br.Offset, len(c.Text))
		}
	}
	sh.ends = append(sh.ends, clamp32(len(c.Text)))
	sh.lastBreak = wrap && c.Text[len(c.Text)-1] == ' '
	return sh, nil
}

// seg is a piece of the line being built.
type seg struct {
	un   *unit
	a, b int
	w    float64
}

// lineOut is a line before it is placed.
type lineOut struct {
	items  []lineItem
	w      float64
	top    float64
	bottom float64
}

func (ln *lineOut) height() float64 { return ln.top + ln.bottom }

// trailingSpaces returns the number of trailing space bytes of text[a:b].
func trailingSpaces(text string, a, b int) int {
	n := 0
	for i := b; i > a && text[i-1] == ' '; i-- {
		n++
	}
	return n
}

// nextLine breaks the next line of p from cur in avail px. ok is false when no
// line is left. The returned cursor is where the following line starts.
func (l *layouter) nextLine(p *paragraph, cur cursor, avail float64) (ln lineOut, next cursor, ok bool, err error) {
	if err := l.b.Charge(budget.Work, 1); err != nil {
		return ln, cur, false, err
	}
	var (
		segs      = l.segs[:0]
		w         float64
		hasText   bool
		forced    bool
		lastBreak = -1
		lastCur   cursor
	)
	u, off := cur.unit, cur.off
loop:
	for u < len(p.units) {
		un := &p.units[u]
		switch un.kind {
		case uBreak:
			forced = true
			u++
			off = 0
			break loop
		case uOpen, uClose:
			segs = append(segs, seg{un: un, w: un.w})
			w += un.w
			u, off = u+1, 0
			continue
		case uAtom:
			if hasText && !p.nowrap && segs[len(segs)-1].un.kind != uOpen {
				lastBreak, lastCur = len(segs), cursor{unit: u}
			}
			if w+un.w > avail+1e-6 && lastBreak >= 0 {
				segs, u, off = segs[:lastBreak], lastCur.unit, lastCur.off
				break loop
			}
			segs = append(segs, seg{un: un, w: un.w})
			w += un.w
			hasText = true
			if !p.nowrap {
				lastBreak, lastCur = len(segs), cursor{unit: u + 1}
			}
			u, off = u+1, 0
			continue
		}
		st := un.st
		start, end, last := st.segment(off)
		a := max(start, off)
		if !hasText && p.collapsible {
			for a < end && st.text[a] == ' ' {
				a++
			}
		}
		// after is where the line continues once this segment is consumed.
		after := cursor{unit: u, off: end}
		if last {
			after = cursor{unit: u + 1}
		}
		if a >= end {
			u, off = after.unit, after.off
			continue
		}
		canBreak := !last || st.lastBreak
		trailW := 0.0
		if t := min(trailingSpaces(st.text, start, end), end-a); p.collapsible && t > 0 {
			trailW = st.width(end-t, end)
		}
		full, fits := st.widthWithin(a, end, avail+1e-6-w+trailW)
		if fits {
			segs = append(segs, seg{un: un, a: a, b: end, w: full})
			w += full
			hasText = true
			if canBreak {
				lastBreak, lastCur = len(segs), after
			}
			u, off = after.unit, after.off
			continue
		}
		// The word does not fit.
		if lastBreak >= 0 {
			segs, u, off = segs[:lastBreak], lastCur.unit, lastCur.off
			break loop
		}
		if p.breakWord && !st.nowrap {
			k, err := l.fitCluster(st, a, end, avail-w)
			if err != nil {
				return ln, cur, false, err
			}
			if k <= a && !hasText {
				k = l.nextCluster(st, a, end)
			}
			if k > a {
				segs = append(segs, seg{un: un, a: a, b: k, w: st.width(a, k)})
				hasText = true
				off = k
			}
			break loop
		}
		// No break opportunity: the word overflows the line.
		full = st.width(a, end)
		segs = append(segs, seg{un: un, a: a, b: end, w: full})
		w += full
		hasText = true
		if canBreak {
			lastBreak, lastCur = len(segs), after
		}
		u, off = after.unit, after.off
	}
	l.segs = segs
	next = cursor{unit: u, off: off}
	if !hasText && !forced {
		// Only inline box edges, or nothing: no line.
		return ln, next, false, nil
	}
	l.finishLine(p, &ln, segs, p.collapsible)
	return ln, next, true, nil
}

// fitCluster returns the largest cluster boundary k in (a, end] such that
// [a, k) fits in room, or a when not even one cluster does. Each cluster it
// walks is charged to Work: a giant word cut line after line must cost
// linearly, not once per line over the whole of it.
func (l *layouter) fitCluster(st *shapedText, a, end int, room float64) (int, error) {
	ga, gb := st.glyphIndex(a), st.glyphIndex(end)
	best := a
	var sum float64
	walked := int64(1)
	for g := ga; g < gb; {
		c := st.glyph(g).Cluster
		h := g
		var cw float64
		for h < gb && st.glyph(h).Cluster == c {
			cw += float64(st.glyph(h).Advance) + st.ls
			h++
		}
		walked++
		if sum+cw > room+1e-6 {
			break
		}
		sum += cw
		best = end
		if h < gb {
			best = int(st.glyph(h).Cluster)
		}
		g = h
	}
	return best, l.b.Charge(budget.Work, walked)
}

// nextCluster returns the end of the cluster that starts at a, within [a, end].
func (l *layouter) nextCluster(st *shapedText, a, end int) int {
	g, gb := st.glyphIndex(a), st.glyphIndex(end)
	if g >= gb {
		return end
	}
	c := st.glyph(g).Cluster
	for g < gb && st.glyph(g).Cluster == c {
		g++
	}
	if g >= gb {
		return end
	}
	return int(st.glyph(g).Cluster)
}

// finishLine strips the trailing spaces, accounts the vertical metrics and
// builds the line items with their x relative to the line start. Consecutive
// pieces of one text box become one run.
func (l *layouter) finishLine(p *paragraph, ln *lineOut, segs []seg, strip bool) {
	if strip {
		for i := len(segs) - 1; i >= 0; i-- {
			s := &segs[i]
			if s.un.kind == uAtom {
				break
			}
			if s.un.kind != uText {
				continue
			}
			if n := trailingSpaces(s.un.st.text, s.a, s.b); n > 0 {
				s.w -= s.un.st.width(s.b-n, s.b)
				s.b -= n
			}
			if s.b <= s.a {
				segs = append(segs[:i], segs[i+1:]...)
				continue
			}
			break
		}
	}
	ln.top, ln.bottom = p.strutTop, p.strutBottom
	var tallTop, tallBottom float64
	x := 0.0
	for i := 0; i < len(segs); i++ {
		s := segs[i]
		un := s.un
		switch un.kind {
		case uOpen, uClose:
			ln.items = append(ln.items, edgeItem(un, x, s.w))
			x += s.w
		case uAtom:
			a := un.atom
			above, below := a.extents()
			switch {
			case !a.spansLine():
				ln.top, ln.bottom = math.Max(ln.top, above), math.Max(ln.bottom, below)
			case a.valign == style.VAlignTop:
				tallTop = math.Max(tallTop, above+below)
			default:
				tallBottom = math.Max(tallBottom, above+below)
			}
			ln.items = append(ln.items, lineItem{kind: itemAtom, x: x, w: s.w, atom: a})
			x += s.w
		default:
			// Merge the following pieces that continue this one.
			for i+1 < len(segs) && segs[i+1].un.kind == uText && segs[i+1].un.st == un.st && segs[i+1].a == s.b {
				i++
				s.b = segs[i].b
				s.w += segs[i].w
			}
			ln.top, ln.bottom = math.Max(ln.top, un.info.top), math.Max(ln.bottom, un.info.bottom)
			n := len(ln.items)
			ln.items = l.appendTextItems(ln.items, un.st, s.a, s.b, x)
			for _, it := range ln.items[n:] {
				x += it.w
			}
		}
	}
	if h := ln.height(); tallTop > h {
		ln.bottom += tallTop - h
	}
	if h := ln.height(); tallBottom > h {
		ln.top += tallBottom - h
	}
	ln.w = x
}

// edgeItem builds the line item of an inline box edge.
func edgeItem(un *unit, x, w float64) lineItem {
	kind, margin := itemOpen, un.info.ml
	if un.kind == uClose {
		kind, margin = itemClose, un.info.mr
	}
	return lineItem{kind: kind, x: x, w: w, box: un.info.box, href: un.info.href, ascent: un.info.asc + un.info.rise, descent: un.info.desc - un.info.rise, rise: un.info.rise, margin: margin}
}

// appendTextItems appends the display text of [a, b) of a shaped text, one item
// per run.
func (l *layouter) appendTextItems(items []lineItem, st *shapedText, a, b int, x float64) []lineItem {
	ga, gb := st.glyphIndex(a), st.glyphIndex(b)
	in := st.info
	for g := ga; g < gb; {
		r, i := st.locate(g)
		gl := st.runs[r].Glyphs
		h := min(len(gl), i+gb-g)
		lo := int(gl[i].Cluster)
		hi := b
		if g+(h-i) < gb {
			hi = int(st.glyph(g + h - i).Cluster)
		}
		glyphs := make([]text.Glyph, h-i)
		var wsum float64
		for j := i; j < h; j++ {
			gly := gl[j]
			gly.Cluster -= clamp32(lo)
			gly.Advance += float32(st.ls)
			glyphs[j-i] = gly
			wsum += float64(gly.Advance)
		}
		run := st.runs[r]
		items = append(items, lineItem{
			kind: itemText, x: x, w: wsum, box: in.box, href: in.href, ascent: in.asc + in.rise, descent: in.desc - in.rise, rise: in.rise,
			text: &display.Text{
				Face: run.Face, Size: float64(run.Size), Color: in.color, Glyphs: glyphs, Text: st.text[lo:hi],
				Underline:   in.decor&style.DecorationUnderline != 0,
				LineThrough: in.decor&style.DecorationLineThrough != 0,
			},
		})
		x += wsum
		g += h - i
	}
	return items
}

// releaseBefore drops the shaping the break token has passed: the units before
// the cursor and, in its unit, the runs that end before its offset. A
// paragraph that spans many pages must not keep the glyphs of the pages behind.
func (p *paragraph) releaseBefore(cur cursor) {
	for ; p.released < cur.unit && p.released < len(p.units); p.released++ {
		p.units[p.released].st = nil
	}
	if cur.unit < len(p.units) && p.units[cur.unit].st != nil {
		p.units[cur.unit].st.releaseBefore(cur.off)
	}
}

func clamp32(i int) int32 { return int32(min(max(i, 0), math.MaxInt32)) }
