package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// trackEps is the free space below which tracks stop growing.
const trackEps = 1e-9

// gridCols is the resolved column tracks of a grid container: the width and
// the offset from the left of the content box of each.
type gridCols struct {
	w, x []float64
}

// gridPlan is the resolved columns and row count of a grid container.
type gridPlan struct {
	cols   gridCols
	rows   int
	rowGap float64
}

// gapsOf resolves column-gap and row-gap against the content width w.
func gapsOf(st *style.Computed, w float64) (col, row float64) {
	return math.Max(0, px(st.ColumnGap, w)), math.Max(0, px(st.RowGap, w))
}

// planGrid sizes the columns of the grid container b in a content box of
// width w. Items are placed one per cell in row-major order; the rows are
// implicit and auto.
func (l *layouter) planGrid(b *box.Box, w float64) (*gridPlan, error) {
	colGap, rowGap := gapsOf(b.Style, w)
	cols, err := l.gridColumns(b, w, colGap)
	if err != nil {
		return nil, err
	}
	n := len(cols.w)
	return &gridPlan{cols: cols, rows: (len(b.Children) + n - 1) / n, rowGap: rowGap}, nil
}

// grid lays the rows of the grid container b out from the row lv.child, as
// monolithic stripes. A row that continues on the next page keeps its plan and
// its layout (stripeState), so it is not laid out again for each page.
func (l *layouter) grid(b *box.Box, f *frag, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	st := l.stripes[b]
	if !resuming || st == nil || st.w != inner.w || st.decor != decor {
		p, err := l.planGrid(b, inner.w)
		if err != nil {
			return complete, nil, err
		}
		st = &stripeState{w: inner.w, decor: decor, plan: p}
	}
	p := st.plan.(*gridPlan)
	n := len(p.cols.w)
	stat, tok, err := l.placeStripes(f, lv, p.rows, p.rowGap, func(i int) (stripe, error) {
		if st.have && st.i == i {
			return st.s, nil
		}
		s, err := l.layGridRow(b.Children[i*n:min((i+1)*n, len(b.Children))], p.cols, inner, decor, b.Style.AlignItems)
		st.i, st.s, st.have = i, s, err == nil
		return s, err
	}, resuming, emptyAtEntry)
	return l.keepStripes(b, st, stat, tok, err)
}

// gridColumns sizes the column tracks of b in a content box of width w
// (CSS Grid §12 for one item per cell): initial sizes from the kind of each
// track and the contributions of its items, then free space shared among the
// tracks that can grow (maximize), then the fr tracks, then what is left is
// shared among the auto tracks (stretch).
func (l *layouter) gridColumns(b *box.Box, w, gap float64) (gridCols, error) {
	tracks := b.Style.GridColumns
	if len(tracks) == 0 {
		tracks = []style.Track{{Kind: style.TrackAuto}}
	}
	n := len(tracks)
	minC, maxC := make([]float64, n), make([]float64, n)
	for i, c := range b.Children {
		col := i % n
		if tracks[col].Kind == style.TrackPx || tracks[col].Kind == style.TrackPercent {
			continue
		}
		e, err := l.sizeEntry(c)
		if err != nil {
			return gridCols{}, err
		}
		minC[col] = math.Max(minC[col], e.outer.min)
		maxC[col] = math.Max(maxC[col], e.outer.max)
	}
	if err := l.b.Charge(budget.Work, int64(n)); err != nil {
		return gridCols{}, err
	}
	base, limit := make([]float64, n), make([]float64, n)
	for i, t := range tracks {
		switch t.Kind {
		case style.TrackPx:
			base[i] = math.Max(0, t.Value)
			limit[i] = base[i]
		case style.TrackPercent:
			base[i] = math.Max(0, t.Value/100*w)
			limit[i] = base[i]
		case style.TrackMinContent:
			base[i], limit[i] = minC[i], minC[i]
		case style.TrackMaxContent:
			base[i], limit[i] = maxC[i], maxC[i]
		case style.TrackFr:
			base[i], limit[i] = minC[i], minC[i]
		default:
			base[i], limit[i] = minC[i], math.Max(minC[i], maxC[i])
		}
	}
	avail := w - gap*float64(n-1)
	if err := l.growTracks(base, limit, avail); err != nil {
		return gridCols{}, err
	}
	if err := expandFlexible(l, tracks, base, avail); err != nil {
		return gridCols{}, err
	}
	stretchAuto(tracks, base, avail)
	x := make([]float64, n)
	pos := 0.0
	for i := range base {
		x[i] = clampCoord(pos)
		pos += base[i] + gap
	}
	return gridCols{w: base, x: x}, nil
}

// growTracks shares the free space of avail px among the tracks below their
// growth limit, equally, until the space or the room is used up (CSS Grid
// §12.6). Each pass saturates a track at least, so n tracks take n passes.
func (l *layouter) growTracks(base, limit []float64, avail float64) error {
	free := avail
	for _, b := range base {
		free -= b
	}
	for free > trackEps {
		if err := l.b.Charge(budget.Work, int64(len(base))); err != nil {
			return err
		}
		open := 0
		for i := range base {
			if base[i] < limit[i] {
				open++
			}
		}
		if open == 0 {
			return nil
		}
		share := free / float64(open)
		used := 0.0
		for i := range base {
			if base[i] < limit[i] {
				g := math.Min(share, limit[i]-base[i])
				base[i] += g
				used += g
			}
		}
		free -= used
	}
	return nil
}

// expandFlexible sizes the fr tracks (CSS Grid §12.7): the space left by the
// other tracks goes to them in proportion to their factors, a track never
// below its minimum contribution (it then stops being flexible), and a sum of
// factors below 1 takes that fraction of the space only.
func expandFlexible(l *layouter, tracks []style.Track, base []float64, avail float64) error {
	flexible := make([]bool, len(tracks))
	sum := 0.0
	fixed := 0.0
	for i, t := range tracks {
		if t.Kind == style.TrackFr && t.Value > 0 {
			flexible[i] = true
			sum += t.Value
			continue
		}
		fixed += base[i]
	}
	if sum == 0 {
		return nil
	}
	var hyp float64
	for sum > 0 {
		// Each pass makes a track inflexible, so n tracks take n passes.
		if err := l.b.Charge(budget.Work, int64(len(tracks))); err != nil {
			return err
		}
		hyp = math.Max(0, avail-fixed) / math.Max(sum, 1)
		changed := false
		for i, t := range tracks {
			if flexible[i] && hyp*t.Value < base[i] {
				flexible[i] = false
				sum -= t.Value
				fixed += base[i]
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for i, t := range tracks {
		if flexible[i] && sum > 0 {
			base[i] = hyp * t.Value
		}
	}
	return nil
}

// stretchAuto shares the space left over among the auto tracks (CSS Grid
// §12.8, justify-content: normal).
func stretchAuto(tracks []style.Track, base []float64, avail float64) {
	free := avail
	autos := 0
	for i, t := range tracks {
		free -= base[i]
		if t.Kind == style.TrackAuto {
			autos++
		}
	}
	if free <= trackEps || autos == 0 {
		return
	}
	for i, t := range tracks {
		if t.Kind == style.TrackAuto {
			base[i] += free / float64(autos)
		}
	}
}

// layGridRow lays the items of a row out, each in its column, and aligns them
// in the row with align-items.
func (l *layouter) layGridRow(items []*box.Box, cols gridCols, inner cbox, decor style.TextDecoration, align style.ItemAlign) (stripe, error) {
	type placed struct {
		f      *frag
		mt, mb float64
		x      float64
	}
	row := make([]placed, len(items))
	height := 0.0
	for i, c := range items {
		if err := l.b.Charge(budget.Work, 1); err != nil {
			return stripe{}, err
		}
		st := c.Style
		cw := cols.w[i]
		ml, mr := autoMargin(st.Margin[style.Left], cw), autoMargin(st.Margin[style.Right], cw)
		mt, mb := autoMargin(st.Margin[style.Top], cw), autoMargin(st.Margin[style.Bottom], cw)
		extra := borderPadding(st, cw)
		bw := math.Max(0, cw-ml-mr)
		if st.Width.Kind != style.DimAuto {
			bw = borderBoxWidth(st, st.Width, cw, extra)
		}
		bw = capWidth(st, math.Max(0, bw-extra), cw, extra) + extra
		f, err := l.layMonoIn(c, bw, cw, decor)
		if err != nil {
			return stripe{}, err
		}
		row[i] = placed{f: f, mt: mt, mb: mb, x: inner.x + cols.x[i] + ml}
		height = math.Max(height, mt+f.rect.H+mb)
	}
	out := stripe{frags: make([]*frag, 0, len(row)), h: height}
	for _, p := range row {
		y, h := alignItem(align, p.mt, p.mb, p.f.rect.H, height)
		p.f.rect.H = h
		moveFrag(p.f, p.x, y)
		out.frags = append(out.frags, p.f)
	}
	return out, nil
}
