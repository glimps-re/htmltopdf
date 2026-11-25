package layout

import (
	"math"
	"sort"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// colMetric holds, for one column of a track, what the cells ask of it: its
// minimum and maximum width, a percentage when a cell or a col element gives
// one, and whether a width in px was given.
type colMetric struct {
	min, max float64
	pct      float64
	hasPct   bool
	fixed    bool
	// fixedMax is the widest width in px given to the column: it replaces max
	// (never under min) for a fixed column.
	fixedMax float64
}

// autoLayout is the state of a table laid out with automatic column widths
// (CSS 2.1 §17.5.2.2, as Blink distributes it): the contributions of the cells
// come from the layouter's intrinsic sizes.
type autoLayout struct {
	l *layouter
	// err is the first budget error met while measuring.
	err error
	// minW and maxW are the minimum and maximum widths of the table grid,
	// spacing included.
	minW, maxW float64
}

// widths returns the width of one column of each track in avail px (the table
// width less the spacing).
func (a *autoLayout) widths(tl *tableLayout, bounds []int, tracks int, weight func(int) float64, avail float64) []float64 {
	m := a.metrics(tl, bounds, tracks, weight)
	wt := make([]float64, tracks)
	for t := range wt {
		wt[t] = weight(t)
	}
	minW, maxW := tableMinMax(m, wt)
	spacing := tl.gap() * float64(tl.ncols+1)
	a.minW, a.maxW = minW+spacing, maxW+spacing
	return distributeColumns(m, wt, avail)
}

// tableSizes are the contributions of an auto table: its minimum and maximum
// width. A table laid out with fixed widths contributes its specified width, as
// before.
func (l *layouter) tableSizes(b *box.Box) (sizes, error) {
	if b.Style.TableLayoutFixed {
		return sizes{}, nil
	}
	tl, err := l.buildTable(b, 0)
	if err != nil {
		return sizes{}, err
	}
	return sizes{tl.auto.minW, tl.auto.maxW}, nil
}

// metrics measures the columns: single cells first, then the cells that span
// several tracks, which only ask for what the columns they cover lack.
func (a *autoLayout) metrics(tl *tableLayout, bounds []int, tracks int, weight func(int) float64) []colMetric {
	m := make([]colMetric, tracks)
	var spans []*tableCell
	for r := tl.firstRow; r < len(tl.rows); r++ {
		for i := range tl.rows[r].cells {
			c := &tl.rows[r].cells[i]
			if c.t1-c.t0 != 1 || weight(c.t0) != 1 {
				spans = append(spans, c)
				continue
			}
			cm := a.cell(tl, c, 1)
			col := &m[c.t0]
			col.min, col.max = math.Max(col.min, cm.min), math.Max(col.max, cm.max)
			if cm.fixed {
				col.fixed, col.fixedMax = true, math.Max(col.fixedMax, cm.max)
			}
			if cm.hasPct {
				col.pct, col.hasPct = math.Max(col.pct, cm.pct), true
			}
		}
	}
	for _, run := range tl.colRuns {
		if !run.sized() {
			continue
		}
		for t := sort.SearchInts(bounds, run.start); t < sort.SearchInts(bounds, run.start+run.n) && t < tracks; t++ {
			switch run.width.Kind {
			case style.DimPx:
				m[t].fixed, m[t].fixedMax = true, math.Max(m[t].fixedMax, run.width.Value)
			case style.DimPercent:
				m[t].pct, m[t].hasPct = run.width.Value, true
			}
		}
	}
	gap := tl.gap()
	for _, c := range spans {
		// Spreading over the tracks a cell covers costs one step per track, twice.
		if err := a.l.b.Charge(budget.Work, 2*int64(c.t1-c.t0)); err != nil {
			if a.err == nil {
				a.err = err
			}
			break
		}
		k := float64(c.span)
		cm := a.cell(tl, c, k)
		inner := gap * (k - 1)
		spread(m, weight, c.t0, c.t1, cm.min-inner, func(x *colMetric) *float64 { return &x.min }, func(x *colMetric) float64 { return x.max })
		spread(m, weight, c.t0, c.t1, cm.max-inner, func(x *colMetric) *float64 { return &x.max }, func(x *colMetric) float64 { return x.max })
		if cm.hasPct {
			// A percentage over several columns goes to them in proportion to
			// their maximum widths.
			total := 0.0
			for t := c.t0; t < c.t1; t++ {
				total += m[t].max * weight(t)
			}
			for t := c.t0; t < c.t1; t++ {
				share := cm.pct / k
				if total > 0 {
					share = cm.pct * m[t].max / total
				}
				m[t].pct, m[t].hasPct = math.Max(m[t].pct, share), true
			}
		}
	}
	for t := range m {
		if m[t].fixed {
			m[t].max = m[t].fixedMax
		}
		m[t].max = math.Max(m[t].max, m[t].min)
	}
	return m
}

// spread raises the tracks [t0, t1) so that their total reaches need: the
// shortfall goes to them in proportion to by (equally when all are 0).
func spread(m []colMetric, weight func(int) float64, t0, t1 int, need float64, field func(*colMetric) *float64, by func(*colMetric) float64) {
	have, byTotal, wTotal := 0.0, 0.0, 0.0
	for t := t0; t < t1; t++ {
		w := weight(t)
		have += *field(&m[t]) * w
		byTotal += by(&m[t]) * w
		wTotal += w
	}
	short := need - have
	if short <= 0 {
		return
	}
	for t := t0; t < t1; t++ {
		share := short * weight(t) / wTotal
		if byTotal > 0 {
			share = short * by(&m[t]) * weight(t) / byTotal
		}
		*field(&m[t]) += share / weight(t)
	}
}

// cell is the contribution of a cell to the k columns it covers, its border box.
func (a *autoLayout) cell(tl *tableLayout, c *tableCell, k float64) colMetric {
	var cm colMetric
	e, err := a.l.sizeEntry(c.box)
	if err != nil {
		if a.err == nil {
			a.err = err
		}
		return cm
	}
	st := c.box.Style
	edge := padPx(st.Padding[style.Left], tl.width) + padPx(st.Padding[style.Right], tl.width) +
		c.border[style.Left] + c.border[style.Right]
	cm.min, cm.max = e.content.min+edge, e.content.max+edge
	switch st.Width.Kind {
	case style.DimPx:
		w := contentWidth(st, st.Width.Value, edge) + edge
		cm.max, cm.fixed = math.Max(cm.min, w), true
	case style.DimPercent:
		cm.pct, cm.hasPct = st.Width.Value, true
	}
	if st.MinWidth.Kind != style.DimAuto {
		min := contentWidth(st, px(st.MinWidth, 0), edge) + edge
		cm.min, cm.max = math.Max(cm.min, min), math.Max(cm.max, min)
	}
	if k > 1 {
		cm.fixed = false
	}
	return cm
}

// distributeColumns shares target px among the columns, wt of each metric being
// the number of identical columns it stands for: every column gets its minimum,
// then percent columns their share, px columns up to their width, automatic
// columns up to their maximum in proportion to what they lack, and the rest goes
// to the automatic columns in proportion to their maximum.
func distributeColumns(m []colMetric, wt []float64, target float64) []float64 {
	out := make([]float64, len(m))
	rem := target
	for i := range m {
		out[i] = m[i].min
		rem -= m[i].min * wt[i]
	}
	if rem <= 0 {
		return out
	}
	grant := func(i int, want float64) {
		give := math.Min(math.Max(want, 0)*wt[i], rem)
		out[i] += give / wt[i]
		rem -= give
	}
	for i := range m {
		if m[i].hasPct {
			grant(i, m[i].pct/100*target-m[i].min)
		}
	}
	for i := range m {
		if !m[i].hasPct && m[i].fixed {
			grant(i, m[i].max-m[i].min)
		}
	}
	want := 0.0
	for i := range m {
		if !m[i].hasPct && !m[i].fixed {
			want += (m[i].max - m[i].min) * wt[i]
		}
	}
	if want > 0 {
		f := math.Min(1, rem/want)
		for i := range m {
			if !m[i].hasPct && !m[i].fixed {
				grant(i, (m[i].max-m[i].min)*f)
			}
		}
	}
	if rem > 0 {
		excess(m, wt, out, rem)
	}
	return out
}

// excess hands rem px to the columns once all want is met: to the automatic
// columns by maximum, else to the px columns, else to the percent ones.
func excess(m []colMetric, wt, out []float64, rem float64) {
	for _, pick := range []func(colMetric) bool{
		func(c colMetric) bool { return !c.hasPct && !c.fixed },
		func(c colMetric) bool { return !c.hasPct },
		func(colMetric) bool { return true },
	} {
		byTotal, wTotal := 0.0, 0.0
		for i := range m {
			if pick(m[i]) {
				byTotal += m[i].max * wt[i]
				wTotal += wt[i]
			}
		}
		if wTotal == 0 {
			continue
		}
		for i := range m {
			if !pick(m[i]) {
				continue
			}
			share := rem * wt[i] / wTotal
			if byTotal > 0 {
				share = rem * m[i].max * wt[i] / byTotal
			}
			out[i] += share / wt[i]
		}
		return
	}
}

// tableMinMax returns the minimum and maximum width of the columns of a table
// (CSS 2.1 §17.5.2.2): a percent column needs its maximum to be that share of
// the table, so the table must be as wide as the one that asks most.
func tableMinMax(m []colMetric, wt []float64) (minW, maxW float64) {
	pctSum, rest := 0.0, 0.0
	for i := range m {
		minW += m[i].min * wt[i]
		maxW += m[i].max * wt[i]
		if m[i].hasPct {
			pctSum += m[i].pct * wt[i]
		} else {
			rest += m[i].max * wt[i]
		}
	}
	for i := range m {
		if m[i].hasPct && m[i].pct > 0 {
			maxW = math.Max(maxW, m[i].max*100/m[i].pct)
		}
	}
	if pctSum > 0 && pctSum < 100 {
		maxW = math.Max(maxW, rest*100/(100-pctSum))
	}
	return minW, math.Max(maxW, minW)
}

// fitTable gives an auto table its width (CSS 2.1 §17.5.2.2): the specified one,
// but at least the minimum of its columns, or, when it has none, the
// shrink-to-fit width.
func (l *layouter) fitTable(b *box.Box, g geom, cb cbox) (geom, error) {
	st := b.Style
	e, err := l.sizeEntry(b)
	if err != nil {
		return g, err
	}
	extra := g.pl + g.pr + g.bl + g.br
	w := g.w
	if st.Width.Kind == style.DimAuto {
		avail := math.Max(0, cb.w-autoMargin(st.Margin[style.Left], cb.w)-autoMargin(st.Margin[style.Right], cb.w)-extra)
		w = math.Min(math.Max(e.content.min, avail), e.content.max)
	}
	return geometryW(st, cb, math.Max(w, math.Max(e.content.min, contentWidth(st, px(st.MinWidth, cb.w), extra)))), nil
}
