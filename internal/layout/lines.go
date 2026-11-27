package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// lines lays out the inline content of b line by line from lv.cur. A line that
// does not fit ends the fragment: the token resumes at that line.
func (l *layouter) lines(b *box.Box, f *frag, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	p, err := l.paragraphOf(b, decor, inner.w)
	if err != nil {
		return complete, nil, err
	}
	cur, off := lv.cur, lv.off
	placed, flushed := 0, false
	startLines, startEmpty, startCur := len(f.lines), l.pageEmpty, cur
	var prev struct {
		cur    cursor
		off, h float64
		ok     bool
	}
	// The margins adjoining the first line are applied before it is known to
	// fit: if it does not, they belong to the break, and the state the flush
	// changed is put back.
	var saved flushState
	l.save(&saved)
	for {
		ln, next, ok, err := l.nextLine(p, cur, inner.w)
		if err != nil {
			return complete, nil, err
		}
		if !ok {
			break
		}
		if !flushed {
			l.flush()
			flushed = true
		}
		ext := l.lineExtent(&ln)
		if over := l.y + ext - l.bottom; !l.unbounded && l.pageEmpty && over > cutEps && ln.shrinkCapped(over, l.bottom-l.y) {
			if ln, next, _, err = l.nextLine(p, cur, inner.w); err != nil {
				return complete, nil, err
			}
			ext = l.lineExtent(&ln)
		}
		// A margin can push the first line of a page past its bottom: it goes on the
		// next page, where the margin is truncated, not out of sight. So does a line
		// without atom whose text, fitting a page, ends past the bottom of this one
		// (a line with atoms is sliced).
		if !l.unbounded && (l.y > l.bottom || l.y+ext > l.bottom && !l.pageEmpty || l.textPastBottom(&ln, off)) {
			if placed == 0 && !resuming && !emptyAtEntry {
				l.restore(&saved)
				return nothing, nil, nil
			}
			pulledOff := 0.0
			if l.cols > 0 {
				switch {
				case next.unit >= len(p.units) && prev.ok && placed-1 >= keptLines(startCur):
					// Widows: the last line would be alone on the next column.
					f.lines = f.lines[:len(f.lines)-1]
					l.y -= prev.h
					cur, pulledOff = prev.cur, prev.off
				case placed > 0 && (placed < minOrphans || next.unit >= len(p.units)) && startCur == (cursor{}) && !startEmpty:
					// Orphans, or a last line that cannot be pulled over: the paragraph
					// moves whole to the next column.
					f.lines = f.lines[:startLines]
					l.restore(&saved)
					l.pageEmpty = startEmpty
					return nothing, nil, nil
				default:
					l.sliced = l.sliced || next.unit >= len(p.units) && placed > 0 || placed > 0 && placed < minOrphans && startCur == (cursor{})
				}
			}
			l.pend, l.truncate = margins{}, true
			p.releaseBefore(cur)
			return partial, []level{{cur: cur, off: pulledOff}}, nil
		}
		if l.needsSlice(&ln, off) {
			text, first := false, off == 0
			if from, to, ok := ln.sliceRange(l.bottom); ok {
				// The baseline is past a page of empty leading: its text goes with
				// the first slice, where the line starts, as on a line without atom.
				text = off == 0 && (ln.top < from || ln.top >= to)
				off, ext = math.Max(off, from), to
			}
			start := l.y
			more, stop, err := l.sliceLine(f, &ln, inner, p.align, start-off, ext, text, first)
			if err != nil {
				return complete, nil, err
			}
			if more {
				l.pend, l.truncate = margins{}, true
				p.releaseBefore(cur)
				// At least 1 px per page: a slice of no height would not progress.
				return partial, []level{{cur: cur, off: off + math.Max(stop-start, 1)}}, nil
			}
		} else if raise := l.raiseFor(&ln, off); raise > 0 {
			l.placeRaised(f, &ln, inner, p.align, raise)
		} else {
			l.place(f, &ln, inner, p.align)
		}
		prev.cur, prev.off, prev.h, prev.ok = cur, off, ln.height(), true
		off = 0
		placed++
		cur = next
		if cur.unit >= len(p.units) {
			break
		}
	}
	l.dropParagraph()
	return complete, nil, nil
}

// place puts the line at the cursor, aligned in the content box.
func (l *layouter) place(f *frag, ln *lineOut, inner cbox, align style.TextAlign) {
	// Lines past the clamp pile up inside it, overlapping, instead of leaving the
	// extent of their box; none starts above that box.
	y := math.Max(f.rect.Y, math.Min(l.y, math.Max(0, css.MaxLengthPx-ln.height())))
	f.lines = append(f.lines, l.build(ln, inner, align, y))
	l.y = clampCoord(l.y + ln.height())
	l.pageEmpty = false
}

// raiseFor returns how far above the cursor the line ln starts when the
// leading pushes a capped atom of it past the bottom of this empty page: the
// capped box then ends on the page, or starts at its top when it is the taller.
// A capped atom is not sliced (what its cap leaves out would be dropped), and
// placed at the cursor its text would be past the page area. 0 for any other
// line.
func (l *layouter) raiseFor(ln *lineOut, off float64) float64 {
	if l.unbounded || !l.pageEmpty || off > 0 {
		return 0
	}
	capped := false
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil && a.capped {
			capped = true
			break
		}
	}
	if !capped {
		return 0
	}
	top, bottom := ln.atomSpan()
	if l.y+bottom <= l.bottom+cutEps {
		return 0
	}
	return math.Max(0, math.Min(top, l.y+bottom-l.bottom))
}

// placeRaised places the line ln raise above the cursor, its atoms on the
// page. Its text, when that puts its glyphs above the page, starts at the
// cursor instead, where the line would start unraised.
func (l *layouter) placeRaised(f *frag, ln *lineOut, inner cbox, align style.TextAlign, raise float64) {
	y := l.y - raise
	lf := l.build(ln, inner, align, y)
	if top, _, ok := ln.textSpan(); ok && y+top < 0 {
		f.lines = append(f.lines, movedText(lf, l.y-(y+top)))
		lf = withoutText(lf)
	}
	f.lines = append(f.lines, lf)
	l.y = clampCoord(y + ln.height())
	l.pageEmpty = false
}

// sliceLine places on the page the part of the line ln that fits it, for a line
// whose atoms are taller than a page area or overflow past its bottom, or
// whose text is past it (see needsSlice). The part ends at the page bottom, or
// higher, above a text that would straddle it (see textCutEnd): stop. The line
// starts at y0, above the page when it resumes; ext is its extent, overflow of
// its atoms included. With text, the text of the line is placed too, its
// glyphs from the top of the slice: a slice range leaves it out of every
// slice, and at its own baseline it would be above or below the page. The
// first slice (first) and the last keep the layers above and below them. It
// reports whether part of it is left for the next page.
func (l *layouter) sliceLine(f *frag, ln *lineOut, inner cbox, align style.TextAlign, y0, ext float64, text, first bool) (more bool, stop float64, err error) {
	end := y0 + ext
	lf := l.build(ln, inner, align, y0)
	whole := &frag{lines: []*lineFrag{lf}}
	stop = l.bottom
	if end > stop+cutEps {
		if stop, err = l.cutEnd([]*frag{whole}, l.y, stop); err != nil {
			return false, stop, err
		}
	}
	piece, err := l.chargedCut(whole, l.y, math.Min(end, stop), first, end <= stop+cutEps)
	if err != nil {
		return false, stop, err
	}
	f.lines = append(f.lines, piece.lines...)
	l.pageEmpty = false
	if top, _, ok := ln.textSpan(); text && ok {
		f.lines = append(f.lines, movedText(lf, l.y-(y0+top)))
	}
	if end > stop+cutEps {
		l.y = l.bottom
		return true, stop, nil
	}
	// Past an overflow slice, the line box itself can end above the page.
	l.y = math.Max(l.y, y0+ln.height())
	return false, stop, nil
}

// withoutText returns a copy of the line lf without its text items.
func withoutText(lf *lineFrag) *lineFrag {
	nl := *lf
	nl.items = make([]lineItem, 0, len(lf.items))
	for _, it := range lf.items {
		if it.text == nil {
			nl.items = append(nl.items, it)
		}
	}
	return &nl
}

// movedText returns a copy of the line lf holding its text items only, moved
// down by dy.
func movedText(lf *lineFrag, dy float64) *lineFrag {
	text := textOnly(lf)
	text.rect.Y += dy
	text.baseline += dy
	for i := range text.items {
		text.items[i].text.Baseline += dy
	}
	return text
}

// textOnly returns a copy of the line lf holding its text items only.
func textOnly(lf *lineFrag) *lineFrag {
	nl := *lf
	nl.items = nil
	for _, it := range lf.items {
		if it.text != nil {
			c := *it.text
			it.text = &c
			nl.items = append(nl.items, it)
		}
	}
	return &nl
}

// lineExtent is the extent of the line ln for the fit and slice tests: its
// height, or the bottom, relative to its top, of the in-flow content of its
// atoms that overflows it (lines of a fixed-height inline-block would be drawn
// below the page). A scratch layout runs no fit test.
func (l *layouter) lineExtent(ln *lineOut) float64 {
	h := ln.height()
	ext := h
	if l.unbounded {
		return ext
	}
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil {
			ext = math.Max(ext, a.top(0, h, ln.top)+a.bottom(overflowBottom)-a.y)
		}
	}
	return ext
}

// needsSlice reports whether the line ln, resumed at off, is sliced on this
// empty page: a line that resumes (it goes on being sliced), one whose atoms
// span more than a page area, one with an atom that is not capped and does
// not end on the page (taller than the room, overflowing the line, or pushed
// down by the leading), or one whose atoms push its text, which fits a page,
// past the page bottom (the text then gets the next page, see textCutEnd).
// Only the atoms count: a line without atom past the page bottom is placed
// whole, and sliceRange keeps a line tall from its leading from being sliced
// over it. A capped atom that fits a page is placed whole and clipped: a slice
// would drop the text its cap leaves out.
func (l *layouter) needsSlice(ln *lineOut, off float64) bool {
	if l.unbounded || !l.pageEmpty || !ln.hasAtom() {
		return false
	}
	if off > 0 {
		return true
	}
	if l.textEndsPast(ln) {
		return true
	}
	h := ln.height()
	top, bottom := math.Inf(1), math.Inf(-1)
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil {
			t := a.top(0, h, ln.top)
			b := t + a.bottom(overflowBottom) - a.y
			if !a.capped && l.y+b > l.bottom+cutEps {
				return true
			}
			top, bottom = math.Min(top, t), math.Max(bottom, b)
		}
	}
	return bottom-top > l.bottom
}

// atomSpan returns the top and the bottom, overflow included, of the atoms of
// the line ln relative to its top.
func (ln *lineOut) atomSpan() (top, bottom float64) {
	h := ln.height()
	top, bottom = math.Inf(1), math.Inf(-1)
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil {
			t := a.top(0, h, ln.top)
			top, bottom = math.Min(top, t), math.Max(bottom, t+a.bottom(overflowBottom)-a.y)
		}
	}
	return top, bottom
}

// sliceRange returns the part of the line ln a slice covers, relative to its
// top, when its line-height leaves more than a page of empty leading around
// its atoms: the atoms and the text of the line when its baseline is within a
// page of them. ok is false for any other line, sliced over its whole extent.
// Without it, a hostile line-height around a tall inline-block is sliced over
// one page per page height of leading.
func (ln *lineOut) sliceRange(page float64) (from, to float64, ok bool) {
	top, bottom := ln.atomSpan()
	if ln.height()-(bottom-top) <= page {
		return 0, 0, false
	}
	// The whole glyphs, not the baseline alone: a range starting at the
	// baseline would put the text at the top edge of the page, out of sight.
	textTop, textBottom, hasText := ln.textSpan()
	if !hasText {
		textTop, textBottom = ln.top, ln.top+1
	}
	if b := ln.top; b < top && b > top-page {
		top = math.Min(textTop, b)
	} else if b > bottom && b < bottom+page {
		// The text of the line goes to the window that holds its baseline.
		bottom = math.Max(textBottom, b+1)
	}
	return math.Max(top, 0), bottom, true
}

// textPastBottom reports whether the line ln, laid at the cursor of an empty
// page from its top, has no atom and text that fits a page area but ends past
// the bottom of this one. In columns, the line must also fit a column from its
// top: otherwise the next column holds it no better, and the columns move or
// grow as a whole.
func (l *layouter) textPastBottom(ln *lineOut, off float64) bool {
	if l.unbounded || !l.pageEmpty || off > 0 || ln.hasAtom() {
		return false
	}
	if l.cols > 0 && l.colTop+ln.height() > l.bottom+cutEps {
		return false
	}
	return l.textEndsPast(ln)
}

// textEndsPast reports whether the text of the line ln, laid at the cursor,
// ends past the bottom while it fits a page area: the fresh page area it gets
// once moved or sliced holds it.
func (l *layouter) textEndsPast(ln *lineOut) bool {
	top, bottom, ok := ln.textSpan()
	return ok && bottom-top <= l.pageH && l.y+bottom > l.bottom+cutEps
}

// textSpan returns the top and the bottom of the glyphs of the line ln
// relative to its top; ok is false for a line without text.
func (ln *lineOut) textSpan() (top, bottom float64, ok bool) {
	top, bottom = math.Inf(1), math.Inf(-1)
	for i := range ln.items {
		if it := &ln.items[i]; it.text != nil {
			top, bottom = math.Min(top, ln.top-it.ascent), math.Max(bottom, ln.top+it.descent)
			ok = true
		}
	}
	return top, bottom, ok
}

func (ln *lineOut) hasAtom() bool {
	for i := range ln.items {
		if ln.items[i].atom != nil {
			return true
		}
	}
	return false
}

// build makes the line box at y, aligned in the content box.
func (l *layouter) build(ln *lineOut, inner cbox, align style.TextAlign, y float64) *lineFrag {
	h := ln.height()
	off := 0.0
	free := inner.w - ln.w
	switch align {
	case style.AlignRight:
		off = free
	case style.AlignCenter:
		off = free / 2
	}
	if off < 0 {
		off = 0
	}
	lf := &lineFrag{
		rect:     display.Rect{X: inner.x, Y: y, W: inner.w, H: h},
		baseline: y + ln.top,
		items:    ln.items,
	}
	for i := range lf.items {
		it := &lf.items[i]
		it.x += inner.x + off
		if it.text != nil {
			it.text.X, it.text.Baseline = it.x, lf.baseline-it.rise
		}
		if a := it.atom; a != nil {
			a.moveTo(it.x+a.ml, a.top(y, h, ln.top))
		}
	}
	return lf
}

// dropParagraph forgets the finished paragraph. The reused segment buffer is
// cleared too: its units point at the shaped glyphs, which must not stay live
// behind the last page of a long paragraph.
func (l *layouter) dropParagraph() {
	l.para = nil
	clear(l.segs[:cap(l.segs)])
}

// flushState is what flush changes: the cursor, the pending margins, the
// truncate flag and the top of the waiting fragments.
type flushState struct {
	y        float64
	pend     margins
	truncate bool
	waiting  []*frag
	tops     []float64
	set      []bool
}

func (l *layouter) save(s *flushState) {
	s.tops, s.set = s.tops[:0], s.set[:0]
	s.y, s.pend, s.truncate = l.y, l.pend, l.truncate
	s.waiting = append(s.waiting[:0], l.waiting...)
	for _, f := range l.waiting {
		s.tops = append(s.tops, f.rect.Y)
		s.set = append(s.set, f.yset)
	}
}

func (l *layouter) restore(s *flushState) {
	l.y, l.pend, l.truncate = s.y, s.pend, s.truncate
	l.waiting = append(l.waiting[:0], s.waiting...)
	for i, f := range s.waiting {
		f.rect.Y, f.yset = s.tops[i], s.set[i]
	}
}

// keptLines is the number of lines of a paragraph that stay on a column when
// its last one moves on: the orphans when the paragraph starts there, else one.
func keptLines(start cursor) int {
	if start == (cursor{}) {
		return minOrphans
	}
	return 1
}
