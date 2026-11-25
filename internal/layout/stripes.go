package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// stripe is one monolithic band of a flex or grid container (a flex line, a
// grid row): its item fragments relative to the band top, and its height.
type stripe struct {
	frags []*frag
	h     float64
}

// stripeState is a flex or grid container cut over pages: its resolved plan and
// the band laid out last, kept so that a band pushed to the next page or sliced
// over several is not laid out again for each of them. The state is valid for
// the width and decoration it was made for; it is dropped when the container
// completes, and by restoreBreak and measure.
type stripeState struct {
	w float64
	// x is the left edge of the content box the kept band s is laid out in.
	x     float64
	decor style.TextDecoration
	// plan is the container's own plan: *flexPlan or *gridPlan.
	plan any
	// s is the laid out band i, when have is set; its fragments stay relative to
	// the top of the band (placeStripes cuts them, it does not move them).
	i    int
	s    stripe
	have bool
}

// moveTo moves the kept band to a content box starting at x: a container that
// continues in another column has its plan kept but not its position.
func (st *stripeState) moveTo(x float64) {
	if dx := x - st.x; dx != 0 {
		for _, fr := range st.s.frags {
			moveFrag(fr, dx, 0)
		}
		st.x = x
	}
}

// keepStripes records st for b when the container continues on the next page,
// and forgets it otherwise; it returns its arguments for the caller to return.
func (l *layouter) keepStripes(b *box.Box, st *stripeState, stat status, tok []level, err error) (status, []level, error) {
	if stat != partial || err != nil {
		delete(l.stripes, b)
		return stat, tok, err
	}
	if l.stripes == nil {
		l.stripes = make(map[*box.Box]*stripeState)
	}
	l.stripes[b] = st
	return stat, tok, nil
}

// placeStripes places the bands lv.child..n-1 of the container fragment f at the
// cursor, gap apart, with the contract of children(): a band that does not fit
// moves to the next page; one taller than an empty page area is sliced with
// cutFrag and resumes at lv.off. lay lays out band i; the band it returns is
// kept by the caller while it is pushed or sliced, so its fragments must stay
// relative to the top of the band: they are cut, never moved, until the band
// is placed whole.
func (l *layouter) placeStripes(f *frag, lv level, n int, gap float64, lay func(i int) (stripe, error), resuming, emptyAtEntry bool) (status, []level, error) {
	l.flush()
	off := lv.off
	placed := 0
	for i := lv.child; i < n; i++ {
		if err := l.b.Charge(budget.Work, 1); err != nil {
			return complete, nil, err
		}
		s, err := lay(i)
		if err != nil {
			return complete, nil, err
		}
		y := l.y
		if placed > 0 {
			y = clampCoord(y + gap)
		}
		top := y - off
		if y+s.h-off > l.bottom+cutEps {
			if !l.pageEmpty {
				if placed == 0 && !resuming && !emptyAtEntry {
					return nothing, nil, nil
				}
				l.pend, l.truncate = margins{}, true
				return partial, []level{{child: i}}, nil
			}
			// A band taller than an empty page: the part that fits goes here.
			l.b.Warn(budget.WarnClamped, "flex or grid line sliced")
			room := l.bottom - y
			for _, fr := range s.frags {
				piece := cutFrag(fr, off, off+room)
				moveFrag(piece, 0, top)
				f.children = append(f.children, piece)
			}
			consumed := room
			l.y, l.pageEmpty = l.bottom, false
			l.pend, l.truncate = margins{}, true
			return partial, []level{{child: i, off: off + consumed}}, nil
		}
		for _, fr := range s.frags {
			if off > 0 {
				fr = cutFrag(fr, off, math.MaxFloat64)
			}
			moveFrag(fr, 0, top)
			f.children = append(f.children, fr)
		}
		l.y = clampCoord(top + s.h)
		l.pageEmpty = false
		off = 0
		placed++
	}
	return complete, nil, nil
}

// alignItem returns the top and the height, in a band of cross size cross, of an
// item of height h and vertical margins mt and mb under align-items a: stretch
// grows the item to the band, the others move it.
func alignItem(a style.ItemAlign, mt, mb, h, cross float64) (y, height float64) {
	switch a {
	case style.ItemStretch:
		return mt, math.Max(h, cross-mt-mb)
	case style.ItemEnd:
		return cross - mb - h, h
	case style.ItemCenter:
		return (cross-mt-h-mb)/2 + mt, h
	}
	return mt, h
}
