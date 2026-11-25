package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// flexItem is a flex item while its line is resolved. Sizes are border box
// widths; the margins are outside them.
type flexItem struct {
	box            *box.Box
	ml, mr, mt, mb float64
	// auto margins: they absorb the free space of the line (main axis) or of the
	// line cross size (cross axis); the fields above are 0 for them.
	autoL, autoR, autoT, autoB bool
	// min and max bound the main size: the automatic minimum (Flexbox §4.5) and
	// max-width.
	min, max float64
	base     float64
	// hyp is the base size clamped to min and max.
	hyp          float64
	size         float64
	grow, shrink float64
	frozen       bool
	f            *frag
}

func (it *flexItem) outer(size float64) float64 { return size + it.ml + it.mr }

// flexPlan is the resolved flex lines of a container: the main sizes of their
// items are known, the items are not laid out yet.
type flexPlan struct {
	lines          [][]*flexItem
	colGap, rowGap float64
}

// planFlex resolves the flex lines of the container b in a content box of width w.
func (l *layouter) planFlex(b *box.Box, w float64) (*flexPlan, error) {
	st := b.Style
	p := &flexPlan{rowGap: math.Max(0, px(st.RowGap, w)), colGap: math.Max(0, px(st.ColumnGap, w))}
	items := make([]flexItem, len(b.Children))
	for i, c := range b.Children {
		if err := l.flexItemOf(&items[i], c, w); err != nil {
			return nil, err
		}
	}
	p.lines = flexLines(items, w, p.colGap, st.FlexWrap)
	for _, line := range p.lines {
		if err := l.resolveFlexible(line, w-p.colGap*float64(len(line)-1)); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// flex lays the lines of the flex container b out from the line lv.child, as
// monolithic stripes.
func (l *layouter) flex(b *box.Box, f *frag, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	st := l.stripes[b]
	if !resuming || st == nil || st.w != inner.w || st.decor != decor {
		p, err := l.planFlex(b, inner.w)
		if err != nil {
			return complete, nil, err
		}
		st = &stripeState{w: inner.w, x: inner.x, decor: decor, plan: p}
	}
	p := st.plan.(*flexPlan)
	stat, tok, err := l.placeStripes(f, lv, len(p.lines), p.rowGap, func(i int) (stripe, error) {
		if st.have && st.i == i {
			st.moveTo(inner.x)
			return st.s, nil
		}
		s, err := l.layLine(p.lines[i], inner, p.colGap, b.Style, decor, len(p.lines) == 1)
		st.i, st.s, st.have, st.x = i, s, err == nil, inner.x
		return s, err
	}, resuming, emptyAtEntry)
	return l.keepStripes(b, st, stat, tok, err)
}

// flexItemOf fills it from the box c: its flex base size, bounds and factors.
func (l *layouter) flexItemOf(it *flexItem, c *box.Box, w float64) error {
	if err := l.b.Charge(budget.Work, 1); err != nil {
		return err
	}
	st := c.Style
	it.box = c
	it.ml, it.mr = autoMargin(st.Margin[style.Left], w), autoMargin(st.Margin[style.Right], w)
	it.mt, it.mb = autoMargin(st.Margin[style.Top], w), autoMargin(st.Margin[style.Bottom], w)
	it.autoL, it.autoR = st.Margin[style.Left].Kind == style.DimAuto, st.Margin[style.Right].Kind == style.DimAuto
	it.autoT, it.autoB = st.Margin[style.Top].Kind == style.DimAuto, st.Margin[style.Bottom].Kind == style.DimAuto
	it.grow, it.shrink = st.FlexGrow, st.FlexShrink
	e, err := l.sizeEntry(c)
	if err != nil {
		return err
	}
	extra := borderPadding(st, w)
	minContent, maxContent := e.content.min+extra, e.content.max+extra
	specified := -1.0
	if st.Width.Kind != style.DimAuto {
		specified = borderBoxWidth(st, st.Width, w, extra)
	}
	switch {
	case st.FlexBasis.Kind != style.DimAuto:
		it.base = borderBoxWidth(st, st.FlexBasis, w, extra)
	case specified >= 0:
		it.base = specified
	default:
		it.base = maxContent
	}
	it.min = minContent
	if specified >= 0 {
		it.min = math.Min(minContent, specified)
	}
	if st.MinWidth.Kind != style.DimAuto {
		// An explicit min-width replaces the automatic minimum (Flexbox §4.5).
		it.min = borderBoxWidth(st, st.MinWidth, w, extra)
	}
	it.max = math.Inf(1)
	if st.MaxWidth.Kind != style.DimNone {
		it.max = borderBoxWidth(st, st.MaxWidth, w, extra)
	}
	it.hyp = math.Max(it.min, math.Min(it.base, it.max))
	return nil
}

// flexLines splits the items in lines: one line, or with wrap as many as the
// hypothetical outer sizes need (Flexbox §9.3). A line holds an item at least.
func flexLines(items []flexItem, w, gap float64, wrap bool) [][]*flexItem {
	var lines [][]*flexItem
	var cur []*flexItem
	sum := 0.0
	for i := range items {
		it := &items[i]
		outer := it.outer(it.hyp)
		if wrap && len(cur) > 0 && sum+gap+outer > w+1e-6 {
			lines = append(lines, cur)
			cur, sum = nil, 0
		}
		if len(cur) > 0 {
			sum += gap
		}
		cur = append(cur, it)
		sum += outer
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// resolveFlexible sets the main size of the items of a line in avail px for
// their margin boxes (Flexbox §9.7).
func (l *layouter) resolveFlexible(line []*flexItem, avail float64) error {
	sumHyp := 0.0
	for _, it := range line {
		sumHyp += it.outer(it.hyp)
	}
	growing := sumHyp < avail
	factor := func(it *flexItem) float64 {
		if growing {
			return it.grow
		}
		return it.shrink
	}
	for _, it := range line {
		it.frozen = factor(it) == 0 || growing && it.base > it.hyp || !growing && it.base < it.hyp
		it.size = it.base
		if it.frozen {
			it.size = it.hyp
		}
	}
	// The initial free space (step 3): the frozen items at their target size, the
	// others at their base size.
	initial := avail
	for _, it := range line {
		if it.frozen {
			initial -= it.outer(it.size)
		} else {
			initial -= it.outer(it.base)
		}
	}
	target := make([]float64, len(line))
	for range line {
		// Each pass freezes an item at least, so a line of n items costs up to n² here.
		if err := l.b.Charge(budget.Work, int64(len(line))); err != nil {
			return err
		}
		unfrozen := 0
		used, factors, scaled := 0.0, 0.0, 0.0
		for _, it := range line {
			if it.frozen {
				used += it.outer(it.size)
				continue
			}
			unfrozen++
			used += it.outer(it.base)
			factors += factor(it)
			scaled += it.shrink * it.base
		}
		if unfrozen == 0 {
			return nil
		}
		free := avail - used
		// Step 4b: with factors that sum to less than one, the initial free space
		// scaled by their sum, when it is smaller than what is left.
		if scaled := initial * factors; factors < 1 && math.Abs(scaled) < math.Abs(free) {
			free = scaled
		}
		total := 0.0
		for i, it := range line {
			if it.frozen {
				continue
			}
			t := it.base
			switch {
			case growing:
				t += free * it.grow / factors
			case scaled > 0:
				t += free * it.shrink * it.base / scaled
			}
			target[i] = t
			total += math.Max(it.min, math.Min(t, it.max)) - t
		}
		for i, it := range line {
			if it.frozen {
				continue
			}
			c := math.Max(it.min, math.Min(target[i], it.max))
			if total == 0 || total > 0 && c > target[i] || total < 0 && c < target[i] {
				it.frozen, it.size = true, c
			}
		}
		if total == 0 {
			return nil
		}
	}
	return nil
}

// layLine lays the items of a resolved line out and positions them: the main
// axis by auto margins, else justify-content, the cross axis by align-items and
// auto margins. The only line of a container whose height is definite has that
// height as cross size (Flexbox §9.4), which the percentage heights of its items
// refer to.
func (l *layouter) layLine(line []*flexItem, inner cbox, gap float64, st *style.Computed, decor style.TextDecoration, single bool) (stripe, error) {
	baseline := st.AlignItems == style.ItemBaseline
	outerSum := gap * float64(len(line)-1)
	var above, below, cross float64
	var bases []float64
	if baseline {
		bases = make([]float64, len(line))
	}
	for i, it := range line {
		f, err := l.layMonoCB(it.box, it.size, cbox{w: inner.w, h: inner.h, hdef: inner.hdef}, decor)
		if err != nil {
			return stripe{}, err
		}
		it.f = f
		cross = math.Max(cross, it.mt+f.rect.H+it.mb)
		outerSum += it.outer(f.rect.W)
		if baseline {
			// The first baseline, else synthesized from the border box (Flexbox §8.5).
			bases[i] = f.rect.H
			if b, ok := findBaseline(f); ok {
				bases[i] = b
			}
			up := it.mt + bases[i]
			above, below = math.Max(above, up), math.Max(below, it.mt+f.rect.H+it.mb-up)
		}
	}
	if baseline {
		cross = above + below
	}
	if single && inner.hdef {
		cross = inner.h
	}
	free := inner.w - outerSum
	x, between := justifyOffsets(st.JustifyContent, free, len(line))
	// The margins of the line stay in the plan as measured: the auto ones are
	// resolved here, for this layout only.
	ml, mr := make([]float64, len(line)), make([]float64, len(line))
	for i, it := range line {
		ml[i], mr[i] = it.ml, it.mr
	}
	if autos := autoMainMargins(line); autos > 0 && free > 0 {
		x, between = 0, 0
		share := clampCoord(free / float64(autos))
		for i, it := range line {
			if it.autoL {
				ml[i] += share
			}
			if it.autoR {
				mr[i] += share
			}
		}
	}
	out := stripe{frags: make([]*frag, 0, len(line)), h: cross}
	for i, it := range line {
		f := it.f
		var y, h float64
		switch {
		case it.autoT || it.autoB:
			room := cross - it.mt - it.mb - f.rect.H
			mt := it.mt
			switch {
			case room <= 0:
			case it.autoT && it.autoB:
				mt += room / 2
			case it.autoT:
				mt += room
			}
			y, h = mt, f.rect.H
		case baseline:
			y, h = above-bases[i], f.rect.H
		case st.AlignItems == style.ItemStretch:
			// Only an item whose height is auto stretches, to the line cross
			// size (Flexbox §8.3), even when its content is taller.
			y, h = it.mt, f.rect.H
			if it.box.Style.Height.Kind == style.DimAuto {
				h = math.Max(0, cross-it.mt-it.mb)
			}
		default:
			y, h = alignItem(st.AlignItems, it.mt, it.mb, f.rect.H, cross)
		}
		f.rect.H = h
		moveFrag(f, inner.x+x+ml[i], y)
		out.frags = append(out.frags, f)
		x += ml[i] + f.rect.W + mr[i] + gap + between
	}
	return out, nil
}

// autoMainMargins counts the auto margins of the items on the main axis.
func autoMainMargins(line []*flexItem) int {
	n := 0
	for _, it := range line {
		if it.autoL {
			n++
		}
		if it.autoR {
			n++
		}
	}
	return n
}

// justifyOffsets returns the offset of the first item and the extra space
// between two items for justify-content with free px of free space among n
// items. Negative free space is not distributed: the items start at the start.
func justifyOffsets(j style.Justify, free float64, n int) (start, between float64) {
	if free <= 0 || n == 0 {
		return 0, 0
	}
	switch j {
	case style.JustifyEnd:
		return free, 0
	case style.JustifyCenter:
		return free / 2, 0
	case style.JustifySpaceBetween:
		if n > 1 {
			return 0, free / float64(n-1)
		}
	case style.JustifySpaceAround:
		return free / float64(n) / 2, free / float64(n)
	case style.JustifySpaceEvenly:
		return free / float64(n+1), free / float64(n+1)
	}
	return 0, 0
}
