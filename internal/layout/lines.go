package layout

import (
	"github.com/glimps-re/htmltopdf/internal/box"
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
	cur := lv.cur
	placed, flushed := 0, false
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
		if l.y+ln.height() > l.bottom && !l.pageEmpty {
			if placed == 0 && !resuming && !emptyAtEntry {
				l.restore(&saved)
				return nothing, nil, nil
			}
			l.pend, l.truncate = margins{}, true
			p.releaseBefore(cur)
			return partial, []level{{cur: cur}}, nil
		}
		l.place(f, &ln, inner, p.align)
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
		rect:     display.Rect{X: inner.x, Y: l.y, W: inner.w, H: h},
		baseline: l.y + ln.top,
		items:    ln.items,
	}
	for i := range lf.items {
		it := &lf.items[i]
		it.x += inner.x + off
		if it.text != nil {
			it.text.X, it.text.Baseline = it.x, lf.baseline
		}
	}
	f.lines = append(f.lines, lf)
	l.y += h
	l.pageEmpty = false
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
