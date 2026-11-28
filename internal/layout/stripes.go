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

// overflowBottom returns the lowest edge reached by f and the in-flow content
// under it, as the fit tests count it: the boxes and line boxes that overflow
// a box of fixed height, which the border box of f does not include. Content
// under overflow: hidden is clipped to its box, and absolute boxes are placed
// by the author: neither counts. f must be complete: its content is measured
// once, then O(1), since pages cut and test the same band or row again.
func overflowBottom(f *frag) float64 {
	measure(f)
	return f.rect.Y + math.Max(f.rect.H, f.ovf)
}

// reachBottom is overflowBottom with the baselines: a line whose box ends above
// a cut but whose baseline is below it belongs to the piece below, so a cut
// must not skip f before this.
func reachBottom(f *frag) float64 {
	measure(f)
	return f.rect.Y + math.Max(f.rect.H, f.reach)
}

// layerSpan returns the lowest and highest top of the layers under f, relative
// to f.rect.Y: its own, and those under its children and atoms; lo > hi when
// there is none. The span of a child or atom with overflow: hidden is left out:
// it clips them, and a cut that does not hold it cannot show them. That of f
// itself counts whatever its overflow, for a cut of its own content. f must be
// complete: the span is computed once, then O(1), like measure.
func layerSpan(f *frag) (lo, hi float64) {
	if f.laysOK {
		return f.layLo, f.layHi
	}
	lo, hi = math.Inf(1), math.Inf(-1)
	add := func(l, h, dy float64) {
		l, h = l+dy, h+dy
		// A NaN would make every test on the span false and hide the layers
		// under it: the span then covers everything.
		if math.IsNaN(l) {
			l = math.Inf(-1)
		}
		if math.IsNaN(h) {
			h = math.Inf(1)
		}
		lo, hi = math.Min(lo, l), math.Max(hi, h)
	}
	inner := func(c *frag) {
		if !clips(c) {
			l, h := layerSpan(c)
			if l <= h || math.IsNaN(l) || math.IsNaN(h) {
				add(l, h, c.rect.Y-f.rect.Y)
			}
		}
	}
	for _, a := range f.abs {
		add(0, 0, a.f.rect.Y-f.rect.Y)
	}
	for _, c := range f.children {
		inner(c)
	}
	for _, ln := range f.lines {
		for i := range ln.items {
			if a := ln.items[i].atom; a != nil {
				inner(a.f)
			}
		}
	}
	f.layLo, f.layHi, f.laysOK = lo, hi, true
	return lo, hi
}

// clips reports whether f has overflow: hidden.
func clips(f *frag) bool {
	return f.box != nil && f.box.Style != nil && f.box.Style.OverflowHidden
}

func measure(f *frag) {
	if f.ovfOK {
		return
	}
	fit, cut := f.rect.Y, f.rect.Y
	if f.box == nil || f.box.Style == nil || !f.box.Style.OverflowHidden {
		for _, ln := range f.lines {
			fit, cut = math.Max(fit, lineFitBottom(ln)), math.Max(cut, lineBottom(ln))
		}
		for _, c := range f.children {
			fit, cut = math.Max(fit, overflowBottom(c)), math.Max(cut, reachBottom(c))
		}
	}
	f.ovf, f.reach, f.ovfOK = fit-f.rect.Y, cut-f.rect.Y, true
}

// chargedCut is cutCounted for a band or line cut again on every page it spans:
// what the cut looks at is charged to Work. first and last tell the first and
// last piece of the slice, which also keep the layers above and below it.
func (l *layouter) chargedCut(f *frag, from, to float64, first, last bool) (*frag, error) {
	piece, visits := cutCounted(f, from, to, first, last)
	return piece, l.b.Charge(budget.Work, visits)
}

// cutEnd is textCutEnd for the fragments frags cut together in the window
// [from, to): their common end, which leaves no text of any of them astride it
// above the middle of the window. lift is the room above the window that its
// start left to the glyphs cut by the previous end (see cutStart): the middle
// is that of the window and lift, so that every page still takes half of it.
//
// What it looks at is charged to Work.
func (l *layouter) cutEnd(frags []*frag, from, to, lift float64) (float64, error) {
	page := l.cutPage(to - from)
	// The floor comes from the whole window: it must not halve with every pass.
	floor := from + math.Max((to-from+math.Min(lift, to-from))/2, cutEps)
	for changed := true; changed; {
		changed = false
		for _, f := range frags {
			end, visits := textCutEnd(f, from, to, floor, page)
			if err := l.b.Charge(budget.Work, visits); err != nil {
				return to, err
			}
			if end < to {
				to, changed = end, true
			}
		}
	}
	return to, nil
}

// cutStart is textCutStart for the fragments frags resumed together at from,
// with the room left on the page below the cursor y: how far below y their
// window starts, 0 in a scratch layout and at the start of the fragments.
// Texts deeper than half of the room are left cut. It marks the layout sliced
// when it leaves cut a text that a taller column (up to colRoom) would hold.
//
// What it looks at is charged to Work.
func (l *layouter) cutStart(frags []*frag, from, y float64) (float64, error) {
	room := l.bottom - y
	if l.unbounded || from <= 0 || room <= 0 {
		return 0, nil
	}
	page := l.cutPage(room)
	depth := 0.0
	for _, f := range frags {
		d, cutH, visits := textCutStart(f, from, room, page)
		if err := l.b.Charge(budget.Work, visits); err != nil {
			return 0, err
		}
		depth = math.Max(depth, d)
		l.sliced = l.sliced || cutH > 0 && cutH <= l.colRoom
	}
	return depth, nil
}

// cutPage is how far below a window the cut looks for glyphs: the page area
// height, or fallback without a page.
func (l *layouter) cutPage(fallback float64) float64 {
	if l.pageH <= 0 {
		return fallback
	}
	return l.pageH
}

// stripeExtent is the extent of band s for the fit and slice tests: its height,
// or the bottom of the content of its items that overflows it. A scratch
// layout runs no fit test.
func (l *layouter) stripeExtent(s stripe) float64 {
	ext := s.h
	if !l.unbounded {
		for _, fr := range s.frags {
			ext = math.Max(ext, overflowBottom(fr))
		}
	}
	return ext
}

// placeStripes places the bands lv.child..n-1 of the container fragment f at the
// cursor, gap apart, with the contract of children(): a band that does not fit
// moves to the next page; one taller than an empty page area is sliced with
// cutFrag and resumes at lv.off. A band fits by its overflow, not only by its
// height: text overflowing a fixed-height item would otherwise be drawn below
// the page (hostile text decides how much overflows). lay lays out band i; the
// band it returns is kept by the caller while it is pushed or sliced, so its
// fragments must stay relative to the top of the band: they are cut, never
// moved, until the band is placed whole.
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
		lift, err := l.cutStart(s.frags, off, y)
		if err != nil {
			return complete, nil, err
		}
		y += lift
		top := y - off
		ext := l.stripeExtent(s)
		if !l.unbounded && y+ext-off > l.bottom+cutEps {
			if !l.pageEmpty {
				if placed == 0 && !resuming && !emptyAtEntry {
					return nothing, nil, nil
				}
				l.pend, l.truncate = margins{}, true
				return partial, []level{{child: i}}, nil
			}
			// A band taller than an empty page: the part that fits goes here.
			l.b.Warn(budget.WarnClamped, "flex or grid line sliced")
			end, err := l.cutEnd(s.frags, off, off+l.bottom-y, lift)
			if err != nil {
				return complete, nil, err
			}
			room := end - off
			for _, fr := range s.frags {
				piece, err := l.chargedCut(fr, off, end, off == 0, false)
				if err != nil {
					return complete, nil, err
				}
				moveFrag(piece, 0, top)
				f.children = append(f.children, piece)
			}
			// Progress on every page, whatever the cursor: a slice of no height
			// would resume at the same offset for ever.
			consumed := math.Max(room, 1)
			l.y, l.pageEmpty = l.bottom, false
			l.pend, l.truncate = margins{}, true
			return partial, []level{{child: i, off: off + consumed}}, nil
		}
		for _, fr := range s.frags {
			if off > 0 {
				if fr, err = l.chargedCut(fr, off, math.MaxFloat64, false, true); err != nil {
					return complete, nil, err
				}
			}
			moveFrag(fr, 0, top)
			f.children = append(f.children, fr)
		}
		// Past an overflow slice, off can exceed the band height.
		l.y = clampCoord(math.Max(y, top+s.h))
		l.pageEmpty = false
		off = 0
		placed++
	}
	return complete, nil, nil
}

// alignItem returns the top and the height, in a band of cross size cross, of an
// item of height h and vertical margins mt and mb under align-items a: stretch
// grows the item to the band, the others move it. The top is never above the
// band: an item pulled up by a negative margin would leave the page.
func alignItem(a style.ItemAlign, mt, mb, h, cross float64) (y, height float64) {
	y, height = mt, h
	switch a {
	case style.ItemStretch:
		height = math.Max(h, cross-mt-mb)
	case style.ItemEnd:
		y = cross - mb - h
	case style.ItemCenter:
		y = (cross-mt-h-mb)/2 + mt
	}
	return clampInBand(y, height, cross), height
}

// clampInBand keeps an item of height h inside a band of cross size cross (at
// the top when it is taller): margins pull items out of it, off the page.
func clampInBand(y, h, cross float64) float64 {
	return math.Min(math.Max(0, y), math.Max(0, cross-h))
}
