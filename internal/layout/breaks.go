package layout

import (
	"maps"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// Breaks apply between siblings only, never through a parent (design §5.2.2):
// a forced break ends the page, an avoided one is a preference that gives way
// when it cannot be satisfied.

// forcedBetween reports whether a forced break sits between the siblings prev
// and next.
func forcedBetween(prev, next *box.Box) bool {
	return prev.Style.BreakAfter == style.BreakPage || next.Style.BreakBefore == style.BreakPage
}

// avoidBetween reports whether the break between the siblings prev and next
// should be avoided. A forced break wins over an avoid.
func avoidBetween(prev, next *box.Box) bool {
	if forcedBetween(prev, next) {
		return false
	}
	return prev.Style.BreakAfter == style.BreakAvoid || next.Style.BreakBefore == style.BreakAvoid
}

// breakSnap is the layout state before a box, to undo it when an avoided
// break cannot be honoured there.
type breakSnap struct {
	flushState
	pageEmpty bool
	forced    bool
}

func (l *layouter) saveBreak(s *breakSnap) {
	s.tops, s.set = s.tops[:0], s.set[:0]
	l.save(&s.flushState)
	s.pageEmpty, s.forced = l.pageEmpty, l.forced
}

// restoreBreak puts the state of s back and forgets the paragraph in progress
// and the tables cut over pages since: their next layout starts afresh.
func (l *layouter) restoreBreak(s *breakSnap) {
	l.restore(&s.flushState)
	l.pageEmpty, l.forced = s.pageEmpty, s.forced
	l.dropParagraph()
	l.takeCutTables()
}

// takeCutTables removes from the cache the tables cut over pages (a row in
// progress) and returns them.
func (l *layouter) takeCutTables() map[*box.Box]*tableLayout {
	var cut map[*box.Box]*tableLayout
	for k, tl := range l.tables {
		if tl.cur != nil {
			if cut == nil {
				cut = make(map[*box.Box]*tableLayout)
			}
			cut[k] = tl
			delete(l.tables, k)
		}
	}
	return cut
}

// maxMeasures bounds the measurements of a page (design §5.2.2): past it, an
// avoid box that fails breaks normally.
const maxMeasures = 8

// measureKey identifies a measurement: a box in a containing block of width w
// on a page area of height h.
type measureKey struct {
	b    *box.Box
	w, h float64
}

// block lays out the block-level box b. A box with break-inside: avoid that
// starts on a page which already has content and does not complete there is
// undone and moved whole to the next page, provided it fits a page (measure);
// one that cannot is kept where it is and sliced, as Chrome does.
func (l *layouter) block(b *box.Box, in []level, cb cbox, decor style.TextDecoration) (result, error) {
	if b.Style.BreakInside != style.BreakAvoid || len(in) > 0 || l.pageEmpty || l.measuring {
		return l.layBlock(b, in, cb, decor)
	}
	var snap breakSnap
	l.saveBreak(&snap)
	res, err := l.layBlock(b, in, cb, decor)
	if err != nil || res.status != partial {
		return res, err
	}
	fits, err := l.measure(b, cb, decor)
	if err != nil || !fits {
		return res, err
	}
	l.restoreBreak(&snap)
	return result{status: nothing}, nil
}

// measure reports whether b, laid out alone at the top of an empty page area,
// completes on it. Inside, avoid and measuring are off and layout stops at the
// first overflow, so the layout of a measurement is at most one page (a table
// cut inside b is built again). It is memoized per box, width and page height;
// a page makes at most maxMeasures of them.
func (l *layouter) measure(b *box.Box, cb cbox, decor style.TextDecoration) (bool, error) {
	key := measureKey{b, cb.w, l.bottom}
	if fits, ok := l.measured[key]; ok {
		return fits, nil
	}
	if l.measures >= maxMeasures {
		return false, nil
	}
	l.measures++
	if l.onMeasure != nil {
		l.onMeasure(b)
	}
	if l.measured == nil {
		l.measured = make(map[measureKey]bool)
	}
	// The attempt that failed is kept if b does not fit: its cut tables stay.
	kept := l.takeCutTables()
	page, forced := l.bottom, l.forced
	l.dropParagraph()
	s := l.enterScratch()
	// The box would start the next page, which truncates its top margin.
	l.bottom, l.measuring, l.truncate, l.unbounded = page, true, true, false
	res, err := l.layBlock(b, nil, cb, decor)
	l.measuring = false
	l.leaveScratch(s)
	l.dropParagraph()
	l.forced = forced
	l.takeCutTables()
	maps.Copy(l.tables, kept)
	if err != nil {
		return false, err
	}
	fits := res.status == complete
	l.measured[key] = fits
	return fits, nil
}
