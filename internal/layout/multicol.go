package layout

import (
	"maps"
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

const (
	// floatSlack keeps a count that fits exactly from being lost to rounding.
	floatSlack = 1e-9
	// minColumnWidth bounds columns: past it, the count is clamped.
	minColumnWidth = 1.0
	// maxBalancePasses bounds the layouts of the content of a balanced segment:
	// the one at the full height, the bisection and the final one.
	maxBalancePasses = 20
	// balanceEps ends the bisection and absorbs float noise in the overflow test.
	balanceEps = 0.01
)

// colGeom is the column geometry of a container: the count, the width of a
// column and the gap between two.
type colGeom struct {
	n      int
	w, gap float64
}

// columnGeometry returns the geometry of the columns of a container whose
// content box is w wide (Multicol 1 §3.4). The count is clamped so that every
// column is at least minColumnWidth wide; clamped tells it was.
func columnGeometry(st *style.Computed, w float64) (g colGeom, clamped bool) {
	gap := st.FontSize
	if !st.ColumnGapNormal {
		gap = px(st.ColumnGap, w)
	}
	gap = clampCoord(math.Max(0, gap))
	w = math.Max(0, w)
	fit := max(1, int(math.Floor((w+gap)/(minColumnWidth+gap)+floatSlack)))
	n := max(1, st.ColumnCount)
	if n > fit {
		n, clamped = fit, true
	}
	return colGeom{n: n, w: (w - float64(n-1)*gap) / float64(n), gap: gap}, clamped
}

// columnsOf returns the columns of a container whose content box is w wide;
// ok is false when it has none to lay out, which is the case of a container
// inside another.
func (l *layouter) columnsOf(st *style.Computed, w float64) (mc colGeom, ok bool) {
	if st.ColumnCount < 2 {
		return colGeom{}, false
	}
	if l.cols > 0 {
		l.b.Warn(budget.WarnUnsupportedProperty, "nested columns")
		return colGeom{}, false
	}
	mc, clamped := columnGeometry(st, w)
	if clamped {
		l.b.Warn(budget.WarnClamped, "columns")
	}
	return mc, mc.n > 1
}

// column is a laid out column: a fragment that holds its content and the
// bottom of that content.
type column struct {
	f      *frag
	bottom float64
}

func (c *column) empty() bool { return len(c.f.children) == 0 && len(c.f.lines) == 0 }

// colPass is one layout of the content in the columns of a segment.
type colPass struct {
	cols []column
	stat status
	tok  []level
	// forced is true when a forced page break ended the segment.
	forced bool
	// sliced is true when a box that avoids a break had to be split.
	sliced bool
	// endMargin is the margin that adjoined the end of the content, which a
	// column does not contain but the total height of the content does.
	endMargin float64
}

// fits reports whether the pass ended the content within its columns.
func (p *colPass) fits() bool { return p.stat != partial || p.forced }

// fitsHeight reports whether the pass ended the content within its columns
// without a column taller than h from y0: a box that cannot be broken makes it
// overflow.
func (p *colPass) fitsHeight(y0, h float64) bool {
	return p.fits() && !p.sliced && p.tallest() <= y0+h+balanceEps
}

// total is the height of the content laid out as a single column.
func (p *colPass) total(y0 float64) float64 {
	t := 0.0
	for _, c := range p.cols {
		t += c.bottom - y0
	}
	return t + p.endMargin
}

func (p *colPass) tallest() float64 {
	h := math.Inf(-1)
	for _, c := range p.cols {
		h = math.Max(h, c.bottom)
	}
	return h
}

// flow lays the in-flow content of b out in one fragmentainer.
func (l *layouter) flow(b *box.Box, f *frag, in []level, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	if hasInlineChildren(b) {
		return l.lines(b, f, lv, inner, decor, resuming, emptyAtEntry)
	}
	return l.children(b, f, in, lv, inner, decor, resuming, emptyAtEntry)
}

// colSnap is the state of the layouter that a pass in the columns changes.
type colSnap struct {
	breakSnap
	bottom   float64
	measures int
	tables   map[*box.Box]tableSnap
	stripes  map[*box.Box]*stripeState
	ctx      *absCtx
	pending  int
	fixed    int
}

// tableSnap is a table cached while it is laid out over several fragmentainers:
// the row it is cut in is changed in place by a pass.
type tableSnap struct {
	tl  *tableLayout
	cur *rowState
	val rowState
	hdr headerState
}

func (l *layouter) saveColumns(s *colSnap) {
	l.saveBreak(&s.breakSnap)
	s.bottom, s.measures = l.bottom, l.measures
	s.tables = make(map[*box.Box]tableSnap, len(l.tables))
	for b, tl := range l.tables {
		ts := tableSnap{tl: tl, cur: tl.cur, hdr: tl.hdr}
		if tl.cur != nil {
			ts.val = *tl.cur
		}
		s.tables[b] = ts
	}
	s.stripes = maps.Clone(l.stripes)
	s.fixed = len(l.fixedReqs)
	s.ctx, s.pending = nil, 0
	if n := len(l.cbs); n > 0 {
		s.ctx = l.cbs[n-1]
		s.pending = len(s.ctx.pending)
	}
}

// restoreColumns puts the state of s back: the layout of the next pass starts
// from the same point as the previous one did.
func (l *layouter) restoreColumns(s *colSnap) {
	l.restore(&s.flushState)
	l.pageEmpty, l.forced = s.pageEmpty, s.forced
	l.bottom, l.measures = s.bottom, s.measures
	l.dropParagraph()
	for b := range l.tables {
		if _, ok := s.tables[b]; !ok {
			delete(l.tables, b)
		}
	}
	for b, ts := range s.tables {
		if l.tables == nil {
			l.tables = make(map[*box.Box]*tableLayout)
		}
		l.tables[b] = ts.tl
		ts.tl.cur, ts.tl.hdr = ts.cur, ts.hdr
		if ts.cur != nil {
			*ts.cur = ts.val
		}
	}
	l.stripes = maps.Clone(s.stripes)
	if len(l.fixedReqs) > s.fixed {
		clear(l.fixedReqs[s.fixed:])
		l.fixedReqs = l.fixedReqs[:s.fixed]
	}
	if s.ctx != nil && len(s.ctx.pending) > s.pending {
		clear(s.ctx.pending[s.pending:])
		s.ctx.pending = s.ctx.pending[:s.pending]
	}
}

// fillColumns lays the content of b out in mc.n columns of height h from y0,
// from the level lv.
func (l *layouter) fillColumns(b *box.Box, in []level, lv level, resuming bool, inner cbox, mc colGeom, y0, h float64, decor style.TextDecoration) (p colPass, err error) {
	if err := l.b.Charge(budget.Work, 1); err != nil {
		return p, err
	}
	if l.onColumns != nil {
		l.onColumns()
	}
	l.cols++
	defer func() { l.cols-- }()
	l.bottom, l.measures, l.forced, l.sliced = y0+h, 0, false, false
	defer func() { p.sliced = l.sliced }()
	cur, curIn := lv, in
	for k := 0; k < mc.n; k++ {
		cf := &frag{box: b, rect: display.Rect{X: inner.x, Y: y0, W: mc.w}, yset: true, first: true, last: true}
		col := inner
		col.x += float64(k) * (mc.w + mc.gap)
		col.w, col.h, col.hdef = mc.w, 0, false
		l.y, l.pend, l.waiting = y0, margins{}, l.waiting[:0]
		l.truncate, l.pageEmpty = resuming || k > 0, true
		var stat status
		var tok []level
		if stat, tok, err = l.flow(b, cf, curIn, cur, col, decor, resuming || k > 0, true); err != nil {
			return p, err
		}
		if stat == nothing {
			// The first box cannot start in the column (its margin pushes it past
			// the bottom): the next one starts the content, margins truncated.
			p.cols = append(p.cols, column{f: cf, bottom: y0})
			p.stat, p.tok = partial, curIn
			continue
		}
		// The margin that adjoins the end of a column is not part of its height.
		if stat == complete {
			p.endMargin = l.pend.resolve()
		}
		l.pend, l.truncate = margins{}, stat == partial
		l.flush()
		p.cols = append(p.cols, column{f: cf, bottom: contentBottom(cf, y0)})
		p.stat, p.tok = stat, tok
		if stat == complete {
			break
		}
		cur, curIn = tok[0], tok
		if l.forced {
			p.forced = true
			break
		}
	}
	return p, nil
}

// contentBottom is the bottom edge of the content of the column fragment f, at
// least y0: unlike the cursor, it does not include the margin that a box which
// then moved to the next column had flushed.
func contentBottom(f *frag, y0 float64) float64 {
	bottom := y0
	for _, c := range f.children {
		bottom = math.Max(bottom, c.rect.Y+c.rect.H)
	}
	for _, ln := range f.lines {
		bottom = math.Max(bottom, ln.rect.Y+ln.rect.H)
	}
	return bottom
}

// multicol lays the content of the multi-column container b from the level lv:
// the columns of this page, balanced when the content ends in them.
func (l *layouter) multicol(b *box.Box, f *frag, in []level, lv level, inner cbox, mc colGeom, hh float64, hasH bool, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	y0 := l.y
	pageBottom := l.bottom
	room := math.Max(0, pageBottom-y0)
	if hasH && hh-lv.used > 0 {
		room = math.Min(room, hh-lv.used)
	}
	if room <= 0 && !resuming && !emptyAtEntry {
		return nothing, nil, nil
	}
	var entry colSnap
	l.saveColumns(&entry)
	defer func() {
		l.bottom = pageBottom
		l.dropParagraph()
	}()

	glyphs := l.b.Used(budget.Glyphs)
	pass, err := l.fillColumns(b, in, lv, resuming, inner, mc, y0, room, decor)
	if err != nil {
		return complete, nil, err
	}
	if !resuming && !emptyAtEntry && len(pass.cols) > 0 && pass.cols[0].bottom > y0+room+balanceEps {
		// Nothing of the content fits what is left of the page: the container
		// starts the next one.
		l.b.RewindGlyphs(glyphs)
		l.restoreColumns(&entry)
		return nothing, nil, nil
	}
	h := room
	if pass.fits() {
		l.b.RewindGlyphs(glyphs)
		oneColumn := len(pass.cols) < 2 || pass.cols[1].empty()
		total := pass.total(y0)
		pass = colPass{}
		if pass, h, err = l.balance(b, in, lv, resuming, inner, mc, y0, room, oneColumn, total, &entry, decor); err != nil {
			return complete, nil, err
		}
	}

	bottom := y0 + h
	placed := false
	for i := range pass.cols {
		c := &pass.cols[i]
		bottom = math.Max(bottom, c.bottom)
		f.children = append(f.children, c.f.children...)
		f.lines = append(f.lines, c.f.lines...)
		placed = placed || !c.empty()
	}
	f.rules = l.columnRules(b.Style, pass.cols, inner, mc, y0, bottom-y0)
	l.bottom = pageBottom
	l.y, l.pend, l.waiting = bottom, margins{}, l.waiting[:0]
	l.truncate = pass.stat == partial
	if placed {
		l.pageEmpty = false
	}
	return pass.stat, pass.tok, nil
}

// balance returns the pass that spreads the content that ended in the columns over
// the columns as Chrome does, and the column height used. It makes at most
// maxBalancePasses layouts, the first included (total is its height as one column, oneColumn tells it was laid out in one); the layouter is left in the state
// after the returned pass.
func (l *layouter) balance(b *box.Box, in []level, lv level, resuming bool, inner cbox, mc colGeom, y0, room float64, oneColumn bool, total float64, entry *colSnap, decor style.TextDecoration) (colPass, float64, error) {
	passes := 1
	if !oneColumn {
		l.restoreColumns(entry)
		glyphs := l.b.Used(budget.Glyphs)
		one, err := l.fillColumns(b, in, lv, resuming, inner, mc, y0, css.MaxLengthPx-y0, decor)
		if err != nil {
			return one, 0, err
		}
		total = one.total(y0)
		l.b.RewindGlyphs(glyphs)
		passes++
	}
	lo := total / float64(mc.n)
	hi := math.Min(room, total+balanceEps)
	best := hi
	try := lo
	for first := true; lo < hi && passes < maxBalancePasses-1; first = false {
		l.restoreColumns(entry)
		glyphs := l.b.Used(budget.Glyphs)
		p, err := l.fillColumns(b, in, lv, resuming, inner, mc, y0, try, decor)
		if err != nil {
			return p, 0, err
		}
		l.b.RewindGlyphs(glyphs)
		passes++
		if p.fitsHeight(y0, try) {
			hi, best = try, try
			if first {
				break
			}
		} else {
			lo = try
		}
		if hi-lo <= balanceEps {
			break
		}
		try = (lo + hi) / 2
	}
	l.restoreColumns(entry)
	p, err := l.fillColumns(b, in, lv, resuming, inner, mc, y0, best, decor)
	return p, best, err
}

// columnRules returns the rules between the columns that have content, over
// the height h of the segment (Multicol 1 §4).
func (l *layouter) columnRules(st *style.Computed, cols []column, inner cbox, mc colGeom, y0, h float64) []display.Rect {
	rw := st.ColumnRule.Width
	if rw <= 0 || h <= 0 {
		return nil
	}
	var rules []display.Rect
	for k := 0; k+1 < len(cols); k++ {
		if cols[k].empty() || cols[k+1].empty() {
			continue
		}
		x := inner.x + float64(k)*(mc.w+mc.gap) + mc.w + (mc.gap-rw)/2
		rules = append(rules, display.Rect{X: x, Y: y0, W: rw, H: h})
	}
	return rules
}

// cutRules returns the part of the rules in the window [from, to).
func cutRules(rules []display.Rect, from, to float64) []display.Rect {
	var out []display.Rect
	for _, r := range rules {
		top, bottom := math.Max(r.Y, from), math.Min(r.Y+r.H, to)
		if bottom > top {
			r.Y, r.H = top, bottom-top
			out = append(out, r)
		}
	}
	return out
}
