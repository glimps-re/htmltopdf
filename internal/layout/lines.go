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
		// A margin can push the first line of a page past its bottom: it goes on the
		// next page, where the margin is truncated, not out of sight.
		if !l.unbounded && (l.y > l.bottom || l.y+ln.height() > l.bottom && !l.pageEmpty) {
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
		if !l.unbounded && l.pageEmpty && ln.hasAtom() && ln.height() > l.bottom {
			start := l.y
			if l.sliceLine(f, &ln, inner, p.align, start-off) {
				l.pend, l.truncate = margins{}, true
				p.releaseBefore(cur)
				return partial, []level{{cur: cur, off: off + l.bottom - start}}, nil
			}
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

// sliceLine places on the page the part of the line ln that fits it, for a line
// taller than a page area (an atomic inline taller than a page: design §3.2,
// a monolithic box is sliced). The line starts at y0, above the page when it
// resumes. It reports whether part of it is left for the next page.
func (l *layouter) sliceLine(f *frag, ln *lineOut, inner cbox, align style.TextAlign, y0 float64) bool {
	end := y0 + ln.height()
	lf := l.build(ln, inner, align, y0)
	piece := cutFrag(&frag{lines: []*lineFrag{lf}}, l.y, math.Min(end, l.bottom))
	f.lines = append(f.lines, piece.lines...)
	l.pageEmpty = false
	if end > l.bottom+cutEps {
		l.y = l.bottom
		return true
	}
	l.y = end
	return false
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
