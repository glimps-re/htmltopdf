package layout

import (
	"math"
	"sort"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// maxTableCols mirrors box.MaxColSpan: cells that start past it are dropped.
const maxTableCols = box.MaxColSpan

type tableCell struct {
	box       *box.Box
	col, span int
	// t0 and t1 are the first and last+1 tracks of the cell.
	t0, t1 int
	// border and color are the used border widths (top, right, bottom, left)
	// and colors of the cell.
	border [4]float64
	color  [4]css.Color
}

// tableRow is a row, or a caption (cells is empty and box is the caption).
type tableRow struct {
	box     *box.Box
	group   *box.Box
	caption bool
	cells   []tableCell
}

// tableLayout is the grid of a table box, computed once for a content width.
type tableLayout struct {
	width    float64
	collapse bool
	spacing  [2]float64
	// colX is the offset of each track from the left of the grid, total its width.
	colX  []float64
	ncols int
	total float64
	// rows holds the captions, the header rows, the body rows and the footer
	// rows, in that order; hdrStart and hdrEnd delimit the header rows.
	rows              []tableRow
	hdrStart, hdrEnd  int
	firstRow, lastRow int
	// cur is the row being laid out over several pages, if any. The width of a
	// table is the same on every page of M1, so the cache key can include it.
	cur *rowState
	// colRuns are the widths given by col elements, in runs of equal columns;
	// colTotal is the number of columns they define.
	colRuns  []colRun
	colTotal int
	// hdr is the header as laid out once.
	hdr headerState
}

type headerState struct {
	measured bool
	height   float64
	rows     []*frag
}

// rowState is a row that continues on the next page: its fragment as laid out
// in one piece, the position where the next page resumes (relative to the top
// of the row) and the line boxes that no cut may cross.
type rowState struct {
	idx     int
	rf      *frag
	from    float64
	iv      []interval
	hasCuts bool
	// top guards the one re-layout of a row that breaks (cells top-aligned):
	// tl.cur carries the flag over the pages the row is cut on.
	top bool
}

func (tl *tableLayout) span(c *tableCell) float64 {
	return tl.colX[c.t1] - tl.colX[c.t0] - tl.gap()
}

// gap is the spacing that ends each track in the separated model.
func (tl *tableLayout) gap() float64 {
	if tl.collapse {
		return 0
	}
	return tl.spacing[0]
}

func (l *layouter) tableOf(b *box.Box, w float64) (*tableLayout, error) {
	if tl, ok := l.tables[b]; ok && tl.width == w {
		return tl, nil
	}
	tl, err := l.buildTable(b, w)
	if err != nil {
		return nil, err
	}
	if l.tables == nil {
		l.tables = make(map[*box.Box]*tableLayout)
	}
	l.tables[b] = tl
	return tl, nil
}

// rowCells returns the cells of a row; dropped is true when cells past column
// maxTableCols were left out or a colspan was shortened to end there.
func rowCells(r *box.Box) (cells []tableCell, dropped bool) {
	cells = make([]tableCell, 0, len(r.Children))
	col := 0
	for _, c := range r.Children {
		if c.Kind != box.KindBlock || c.Style.Display != style.DisplayTableCell {
			continue
		}
		if col >= maxTableCols {
			return cells, true
		}
		span := max(c.ColSpan, 1)
		if span > maxTableCols-col {
			span, dropped = maxTableCols-col, true
		}
		cells = append(cells, tableCell{box: c, col: col, span: span})
		col += span
	}
	return cells, dropped
}

func (l *layouter) buildTable(b *box.Box, w float64) (*tableLayout, error) {
	st := b.Style
	tl := &tableLayout{width: w, collapse: st.BorderCollapse}
	if !tl.collapse {
		tl.spacing = [2]float64{clampCoord(math.Max(0, st.BorderSpacing[0])), clampCoord(math.Max(0, st.BorderSpacing[1]))}
	}
	var caps, hdr, body, ftr []tableRow
	hasHdr, hasFtr := false, false
	addGroup := func(g *box.Box) {
		dst := &body
		switch {
		case g.Style.Display == style.DisplayTableHeaderGroup && !hasHdr:
			hasHdr, dst = true, &hdr
		case g.Style.Display == style.DisplayTableFooterGroup && !hasFtr:
			hasFtr, dst = true, &ftr
		}
		for _, r := range g.Children {
			if r.Kind == box.KindBlock && r.Style.Display == style.DisplayTableRow {
				*dst = append(*dst, tableRow{box: r, group: g})
			}
		}
	}
	for _, c := range b.Children {
		if c.Kind != box.KindBlock {
			continue
		}
		switch c.Style.Display {
		case style.DisplayTableCaption:
			caps = append(caps, tableRow{box: c, caption: true})
		case style.DisplayTableRowGroup, style.DisplayTableHeaderGroup, style.DisplayTableFooterGroup:
			addGroup(c)
		case style.DisplayTableRow:
			body = append(body, tableRow{box: c})
		}
	}
	tl.colRuns, tl.colTotal = columnRuns(b)
	tl.rows = make([]tableRow, 0, len(caps)+len(hdr)+len(body)+len(ftr))
	tl.rows = append(tl.rows, caps...)
	tl.hdrStart = len(tl.rows)
	tl.rows = append(tl.rows, hdr...)
	tl.hdrEnd = len(tl.rows)
	tl.rows = append(tl.rows, body...)
	tl.rows = append(tl.rows, ftr...)
	tl.firstRow, tl.lastRow = len(caps), len(tl.rows)-1

	ncols := 0
	cellCount := 0
	for i := tl.firstRow; i < len(tl.rows); i++ {
		r := &tl.rows[i]
		var dropped bool
		r.cells, dropped = rowCells(r.box)
		if dropped {
			l.b.Warn(budget.WarnClamped, "table columns > 1000")
		}
		cellCount += len(r.cells)
		if n := len(r.cells); n > 0 {
			ncols = max(ncols, r.cells[n-1].col+r.cells[n-1].span)
		}
		if err := l.b.Charge(budget.Work, int64(len(r.cells))+1); err != nil {
			return nil, err
		}
	}
	// Columns and borders are linear in the cells and the columns.
	if err := l.b.Charge(budget.Work, int64(cellCount+ncols)); err != nil {
		return nil, err
	}
	if err := l.b.Charge(budget.Work, int64(len(tl.colRuns))); err != nil {
		return nil, err
	}
	tl.columns(max(ncols, tl.colTotal))
	if tl.collapse {
		tl.resolveBorders(st)
	} else {
		for i := tl.firstRow; i < len(tl.rows); i++ {
			for j := range tl.rows[i].cells {
				c := &tl.rows[i].cells[j]
				for s := range c.border {
					c.border[s], c.color[s] = c.box.Style.Border[s].Width, c.box.Style.Border[s].Color
				}
			}
		}
	}
	return tl, nil
}

// columns sets the track widths from the cells of the first row (CSS 2.1
// §17.5.2.1): a cell width spreads over its columns, the other columns share
// what is left. Widths that do not fit are scaled down; free space goes to the
// specified columns when there are no others. Columns that no cell starts or
// ends at make one track, so a colspan of 1000 costs two tracks, not 1000.
func (tl *tableLayout) columns(n int) {
	tl.ncols = n
	if n == 0 {
		return
	}
	mark := make([]bool, n+1)
	mark[0], mark[n] = true, true
	for _, run := range tl.colRuns {
		if run.sized() {
			mark[run.start], mark[run.start+run.n] = true, true
		}
	}
	for r := tl.firstRow; r < len(tl.rows); r++ {
		for _, c := range tl.rows[r].cells {
			mark[c.col], mark[c.col+c.span] = true, true
		}
	}
	var bounds []int
	for col, m := range mark {
		if m {
			bounds = append(bounds, col)
		}
	}
	tracks := len(bounds) - 1
	for r := tl.firstRow; r < len(tl.rows); r++ {
		for i := range tl.rows[r].cells {
			c := &tl.rows[r].cells[i]
			c.t0, c.t1 = sort.SearchInts(bounds, c.col), sort.SearchInts(bounds, c.col+c.span)
		}
	}
	weight := func(t int) float64 { return float64(bounds[t+1] - bounds[t]) }
	gap := tl.gap()
	avail := math.Max(0, tl.width-gap*float64(n+1))
	spec := make([]float64, tracks) // specified width of one column of the track
	if tl.firstRow < len(tl.rows) {
		for i := range tl.rows[tl.firstRow].cells {
			c := &tl.rows[tl.firstRow].cells[i]
			st := c.box.Style
			if st.Width.Kind != style.DimPx && st.Width.Kind != style.DimPercent {
				continue
			}
			cw := math.Max(0, px(st.Width, tl.width))
			if st.BoxSizing != style.BorderBox {
				cw += padPx(st.Padding[style.Left], tl.width) + padPx(st.Padding[style.Right], tl.width)
				cw += st.Border[style.Left].Width + st.Border[style.Right].Width
			}
			per := cw / float64(c.span)
			for t := c.t0; t < c.t1; t++ {
				spec[t] = per
			}
		}
	}
	// The width of a col element wins over the cells.
	for _, run := range tl.colRuns {
		if !run.sized() {
			continue
		}
		w := math.Max(0, px(run.width, tl.width))
		for t := sort.SearchInts(bounds, run.start); t < sort.SearchInts(bounds, run.start+run.n); t++ {
			spec[t] = w
		}
	}
	sum, auto := 0.0, 0.0
	for t, v := range spec {
		if v > 0 {
			sum += v * weight(t)
		} else {
			auto += weight(t)
		}
	}
	colw := make([]float64, tracks)
	switch {
	case sum > 0 && (sum >= avail || auto == 0):
		for t, v := range spec {
			colw[t] = v * avail / sum
		}
	case auto > 0:
		share := (avail - sum) / auto
		for t, v := range spec {
			colw[t] = share
			if v > 0 {
				colw[t] = v
			}
		}
	}
	tl.colX = make([]float64, tracks+1)
	x := gap
	for t, w := range colw {
		tl.colX[t] = x
		x += w*weight(t) + (weight(t)-1)*gap + gap
	}
	tl.colX[tracks] = x
	tl.total = x
}

func (tl *tableLayout) coverAt(r, col int) *tableCell {
	if r < tl.firstRow || r >= len(tl.rows) {
		return nil
	}
	cells := tl.rows[r].cells
	i := sort.Search(len(cells), func(i int) bool { return cells[i].col+cells[i].span > col })
	if i < len(cells) && cells[i].col <= col {
		return &cells[i]
	}
	return nil
}

// edge resolves the border shared by a (the cell before, above or left) and b:
// the wider wins and a wins ties (CSS 2.1 §17.6.2.1, without rows and groups);
// hidden suppresses it.
func edge(a, b style.Border) (float64, css.Color) {
	if a.Style == style.BorderHidden || b.Style == style.BorderHidden {
		return 0, css.Color{}
	}
	if b.Width > a.Width {
		return b.Width, b.Color
	}
	return a.Width, a.Color
}

// resolveBorders sets the used borders of the cells in the collapsed model:
// each cell draws half of every border it shares. At the edge of the table the
// cell draws its full border where it is wider than the table's own, which the
// table box draws.
func (tl *tableLayout) resolveBorders(table *style.Computed) {
	ncols := tl.ncols
	for r := tl.firstRow; r < len(tl.rows); r++ {
		for i := range tl.rows[r].cells {
			c := &tl.rows[r].cells[i]
			own := &c.box.Style.Border
			var nb [4]*tableCell
			nb[style.Top] = tl.coverAt(r-1, c.col)
			nb[style.Bottom] = tl.coverAt(r+1, c.col)
			if i > 0 {
				nb[style.Left] = &tl.rows[r].cells[i-1]
			}
			if i+1 < len(tl.rows[r].cells) {
				nb[style.Right] = &tl.rows[r].cells[i+1]
			}
			for s := 0; s < 4; s++ {
				var w float64
				var col css.Color
				if nb[s] != nil {
					other := nb[s].box.Style.Border
					var first, second style.Border
					if s == style.Top || s == style.Left {
						first, second = other[opposite(s)], own[s]
					} else {
						first, second = own[s], other[opposite(s)]
					}
					w, col = edge(first, second)
					w /= 2
				} else {
					w, col = own[s].Width, own[s].Color
					if own[s].Style == style.BorderHidden {
						w = 0
					}
					outer := s == style.Top && r == tl.firstRow || s == style.Bottom && r == tl.lastRow ||
						s == style.Left && c.col == 0 || s == style.Right && c.col+c.span == ncols
					if outer && w <= table.Border[s].Width {
						w = 0
					}
				}
				c.border[s], c.color[s] = w, col
			}
		}
	}
}

func opposite(side int) int { return (side + 2) % 4 }

// scratch is the cursor state saved while a cell or a row is measured on an
// unbounded page.
type scratch struct {
	y         float64
	pend      margins
	waiting   []*frag
	truncate  bool
	pageEmpty bool
	bottom    float64
	para      *paragraph
	unbounded bool
}

func (l *layouter) enterScratch() scratch {
	s := scratch{y: l.y, pend: l.pend, waiting: l.waiting, truncate: l.truncate, pageEmpty: l.pageEmpty, bottom: l.bottom, para: l.para, unbounded: l.unbounded}
	l.unbounded = true
	l.y, l.pend, l.waiting, l.truncate = 0, margins{}, nil, false
	l.pageEmpty, l.bottom, l.para = true, css.MaxLengthPx, nil
	return s
}

func (l *layouter) leaveScratch(s scratch) {
	l.y, l.pend, l.waiting, l.truncate, l.pageEmpty, l.bottom, l.para = s.y, s.pend, s.waiting, s.truncate, s.pageEmpty, s.bottom, s.para
	l.unbounded = s.unbounded
}

// layCell lays out the content of a cell in a border box of width w at x, from
// y = 0 on an unbounded page. The height of the returned fragment is its
// natural one.
func (l *layouter) layCell(tl *tableLayout, c *tableCell, x, w float64, decor style.TextDecoration) (*frag, error) {
	st := c.box.Style
	f := &frag{box: c.box, first: true, last: true, yset: true}
	f.border = c.border
	pad := func(side int) float64 { return padPx(st.Padding[side], tl.width) }
	f.padding = [4]float64{pad(style.Top), pad(style.Right), pad(style.Bottom), pad(style.Left)}
	f.borderColor = &c.color
	f.rect = display.Rect{X: x, W: w}
	cw := math.Max(0, w-f.border[style.Left]-f.border[style.Right]-f.padding[style.Left]-f.padding[style.Right])
	inner := cbox{x: x + f.border[style.Left] + f.padding[style.Left], w: cw}
	s := l.enterScratch()
	defer l.leaveScratch(s)
	l.y = f.border[style.Top] + f.padding[style.Top]
	childDecor := decor | st.TextDecoration
	var err error
	if hasInlineChildren(c.box) {
		_, _, err = l.lines(c.box, f, level{}, inner, childDecor, false, true)
	} else {
		_, _, err = l.children(c.box, f, nil, level{}, inner, childDecor, false, true)
	}
	if err != nil {
		return nil, err
	}
	l.flush()
	f.rect.H = clampCoord(l.y + f.border[style.Bottom] + f.padding[style.Bottom])
	return f, nil
}

func shiftContent(f *frag, dy float64) {
	if dy == 0 {
		return
	}
	for _, c := range f.children {
		shiftFrag(c, dy)
	}
	for _, ln := range f.lines {
		ln.rect.Y += dy
		ln.baseline += dy
		for i := range ln.items {
			if t := ln.items[i].text; t != nil {
				t.Baseline += dy
			}
		}
	}
}

func shiftFrag(f *frag, dy float64) {
	f.rect.Y += dy
	shiftContent(f, dy)
}

// firstBaseline is the y of the first line of f, or the bottom of its content.
func firstBaseline(f *frag) float64 {
	if b, ok := findBaseline(f); ok {
		return b
	}
	return f.rect.H - f.border[style.Bottom] - f.padding[style.Bottom]
}

func findBaseline(f *frag) (float64, bool) {
	for _, c := range f.children {
		if b, ok := findBaseline(c); ok {
			return b, true
		}
	}
	if len(f.lines) > 0 {
		return f.lines[0].baseline, true
	}
	return 0, false
}

// layRow lays out the cells of a row at y = 0 and returns the row fragment
// with its height. Cells are aligned vertically (CSS 2.1 §17.5.3); with topAlign
// every cell starts at the top, whatever its vertical-align: Chrome does so in a
// row that crosses a page break.
func (l *layouter) layRow(tl *tableLayout, row *tableRow, inner cbox, decor style.TextDecoration, topAlign bool) (*frag, error) {
	if err := l.b.Charge(budget.Work, int64(len(row.cells)+1)); err != nil {
		return nil, err
	}
	rf := &frag{box: row.box, first: true, last: true, yset: true}
	rf.rect = display.Rect{X: inner.x, W: tl.total}
	cells := make([]*frag, len(row.cells))
	aligns := make([]style.VerticalAlign, len(row.cells))
	base := 0.0
	for i := range row.cells {
		c := &row.cells[i]
		f, err := l.layCell(tl, c, inner.x+tl.colX[c.t0], tl.span(c), decor)
		if err != nil {
			return nil, err
		}
		cells[i] = f
		aligns[i] = c.box.Style.VerticalAlign
		if topAlign {
			aligns[i] = style.VAlignTop
		}
		if aligns[i] == style.VAlignBaseline {
			base = math.Max(base, firstBaseline(f))
		}
	}
	h := 0.0
	for i, f := range cells {
		if aligns[i] == style.VAlignBaseline {
			h = math.Max(h, base-firstBaseline(f)+f.rect.H)
		} else {
			h = math.Max(h, f.rect.H)
		}
	}
	h = clampCoord(h)
	for i, f := range cells {
		var off float64
		switch aligns[i] {
		case style.VAlignMiddle:
			off = (h - f.rect.H) / 2
		case style.VAlignBottom:
			off = h - f.rect.H
		case style.VAlignBaseline:
			off = base - firstBaseline(f)
		}
		shiftContent(f, off)
		f.rect.H = h
		rf.children = append(rf.children, f)
	}
	rf.rect.H = h
	return rf, nil
}

func (l *layouter) placeRow(f *frag, row *tableRow, rf *frag, consumed float64) {
	shiftFrag(rf, l.y)
	l.y = clampCoord(l.y + consumed)
	if row.group == nil || row.caption {
		f.children = append(f.children, rf)
		return
	}
	if n := len(f.children); n > 0 && f.children[n-1].box == row.group {
		g := f.children[n-1]
		g.children = append(g.children, rf)
		g.rect.H = rf.rect.Y + rf.rect.H - g.rect.Y
		return
	}
	g := &frag{box: row.group, first: true, last: true, yset: true, children: []*frag{rf}}
	g.rect = rf.rect
	f.children = append(f.children, g)
}

// layItem lays out a row or a caption from y = 0 (a caption ignores topAlign) and returns its fragment and
// the height it takes, margins included for a caption.
func (l *layouter) layItem(tl *tableLayout, row *tableRow, inner cbox, decor style.TextDecoration, topAlign bool) (*frag, float64, error) {
	if !row.caption {
		rf, err := l.layRow(tl, row, inner, decor, topAlign)
		if err != nil {
			return nil, 0, err
		}
		return rf, rf.rect.H, nil
	}
	s := l.enterScratch()
	defer l.leaveScratch(s)
	res, err := l.block(row.box, nil, cbox{x: inner.x, w: inner.w}, decor)
	if err != nil {
		return nil, 0, err
	}
	l.flush()
	if res.f == nil {
		return &frag{box: row.box, first: true, last: true, yset: true, rect: display.Rect{X: inner.x, W: inner.w}}, l.y, nil
	}
	return res.f, l.y, nil
}

// interval is the vertical extent of lines of a row that no cut may cross.
type interval struct{ top, bottom float64 }

const cutEps = 1e-6

// lineIntervals returns the line boxes of the tree of f, merged and sorted:
// the places where a row cannot be cut. Lines are never split.
func lineIntervals(f *frag) []interval {
	var all []interval
	var walk func(*frag)
	walk = func(f *frag) {
		for _, ln := range f.lines {
			if ln.rect.H > 0 {
				all = append(all, interval{ln.rect.Y, ln.rect.Y + ln.rect.H})
			}
		}
		for _, c := range f.children {
			walk(c)
		}
	}
	walk(f)
	sort.Slice(all, func(i, j int) bool { return all[i].top < all[j].top })
	out := all[:0]
	for _, v := range all {
		if n := len(out); n > 0 && v.top < out[n-1].bottom-cutEps {
			out[n-1].bottom = math.Max(out[n-1].bottom, v.bottom)
			continue
		}
		out = append(out, v)
	}
	return out
}

// cutRow returns the position, relative to the top of the row, where a row
// resumed at from is cut so that what is before it fits in avail: the largest
// position inside no line box, once at least one line has been placed. ok is
// false when there is none: a row of single lines is indivisible.
func cutRow(iv []interval, from, avail float64) (float64, bool) {
	limit := from + avail
	first := math.Inf(1)
	for _, v := range iv {
		if v.bottom > from+cutEps {
			first = v.bottom
			break
		}
	}
	if limit < first-cutEps {
		return 0, false
	}
	i := sort.Search(len(iv), func(i int) bool { return iv[i].bottom > limit })
	c := limit
	if i < len(iv) && iv[i].top < limit-cutEps {
		c = iv[i].top
	}
	if c < first-cutEps || c <= from+cutEps {
		return 0, false
	}
	return c, true
}

// cutFrag walks the whole row, so a row cut over many pages costs its size on
// each (bounded by MaxPages). It returns a copy of the part of f between from and to, in the
// coordinates of f. A box that crosses a cut is split in two pieces; a line
// belongs to the piece that holds its baseline. The lines are copied, so the
// original can be cut again.
func cutFrag(f *frag, from, to float64) *frag {
	top, bottom := f.rect.Y, f.rect.Y+f.rect.H
	nf := *f
	nf.rect.Y = math.Max(top, from)
	nf.rect.H = math.Max(0, math.Min(bottom, to)-nf.rect.Y)
	nf.first = f.first && top >= from
	nf.last = f.last && bottom <= to
	nf.children, nf.lines = nil, nil
	for _, c := range f.children {
		ct, cb := c.rect.Y, c.rect.Y+c.rect.H
		if cb > from && ct < to || cb == ct && ct >= from && ct < to {
			nf.children = append(nf.children, cutFrag(c, from, to))
		}
	}
	for _, ln := range f.lines {
		if ln.baseline < from || ln.baseline >= to {
			continue
		}
		nl := *ln
		nl.items = append([]lineItem(nil), ln.items...)
		for i := range nl.items {
			if t := nl.items[i].text; t != nil {
				c := *t
				nl.items[i].text = &c
			}
		}
		nf.lines = append(nf.lines, &nl)
	}
	return &nf
}

// tableRows lays out the rows of table b from lv.child on the current page. A
// row that does not fit is cut between line boxes (all its cells continue on
// the next page); a row of single lines moves whole, and one taller than an
// empty page is sliced.
func (l *layouter) tableRows(b *box.Box, f *frag, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	tl, err := l.tableOf(b, inner.w)
	if err != nil {
		return complete, nil, err
	}
	rows := tl.rows
	next := lv.child
	if next >= len(rows) {
		delete(l.tables, b)
		return complete, nil, nil
	}
	// A spacing of a page or more would push every row off the page.
	sv := min(tl.spacing[1], l.bottom/4)
	if sv < tl.spacing[1] {
		l.b.Warn(budget.WarnClamped, "border-spacing")
	}
	l.flush()
	if resuming && next >= tl.hdrEnd && tl.hdrEnd > tl.hdrStart {
		if err := l.repeatHeader(tl, f, inner, decor, sv); err != nil {
			return complete, nil, err
		}
	}
	placed, placedBody := 0, 0
	for i := next; i < len(rows); i++ {
		row := &rows[i]
		var (
			rf       *frag
			consumed float64
			cur      *rowState
		)
		if tl.cur != nil && tl.cur.idx == i {
			cur = tl.cur
			rf = cur.rf
			consumed = rf.rect.H
		} else {
			if rf, consumed, err = l.layItem(tl, row, inner, decor, false); err != nil {
				return complete, nil, err
			}
			cur = &rowState{idx: i, rf: rf}
		}
		tl.cur = nil
		need := rowNeed(row, consumed-cur.from, sv, i == len(rows)-1)
		work, wrf, wconsumed := cur, rf, consumed
		if l.y+need > l.bottom {
			// A row that breaks has its cells flow from the top (as in Chrome):
			// centered cells would have lines on no common cut position. The
			// top-aligned row decides the cut; a row that finally is not cut
			// is placed as laid out first.
			if !row.caption && !cur.top {
				if wrf, wconsumed, err = l.layItem(tl, row, inner, decor, true); err != nil {
					return complete, nil, err
				}
				work = &rowState{idx: i, rf: wrf, top: true, from: cur.from}
			}
			cut, at := l.chooseCut(row, work, placed, placedBody, emptyAtEntry, sv)
			switch {
			case cut == cutNothing:
				delete(l.tables, b)
				return nothing, nil, nil
			case cut == cutBreak:
				l.pend, l.truncate = margins{}, true
				if work.from > 0 {
					tl.cur = work
				}
				return partial, []level{{child: i}}, nil
			case at < wconsumed-cutEps:
				l.y += sv
				piece := cutFrag(wrf, work.from, at)
				shiftFrag(piece, -work.from)
				l.placeRow(f, row, piece, piece.rect.H)
				l.pageEmpty = false
				work.from = at
				tl.cur = work
				l.pend, l.truncate = margins{}, true
				return partial, []level{{child: i}}, nil
			}
		}
		if work != cur && l.y+sv+consumed-cur.from > l.bottom {
			// Top-aligned, the row fits where it laid out first does not (baseline
			// cells make a row taller): it is the one to place.
			rf, consumed, cur = wrf, wconsumed, work
		}
		piece, advance := rf, consumed
		if !row.caption {
			l.y += sv
			if cur.from > 0 {
				piece = cutFrag(rf, cur.from, math.Inf(1))
				shiftFrag(piece, -cur.from)
			}
			advance = piece.rect.H
		}
		l.placeRow(f, row, piece, advance)
		l.pageEmpty = false
		placed++
		if i >= tl.hdrEnd {
			placedBody++
		}
	}
	l.y += sv
	delete(l.tables, b)
	return complete, nil, nil
}

// rowNeed is the height a row still takes: what is left of it plus the border
// spacing around it (after the last row too).
func rowNeed(row *tableRow, left, sv float64, last bool) float64 {
	if row.caption {
		return left
	}
	if last {
		return left + 2*sv
	}
	return left + sv
}

type cutKind uint8

const (
	cutHere cutKind = iota
	cutBreak
	cutNothing
)

// chooseCut decides what to do with a row that does not fit: cut it (cutHere,
// at the returned position), end the page before it (cutBreak) or move the
// whole table to the next page (cutNothing).
func (l *layouter) chooseCut(row *tableRow, cur *rowState, placed, placedBody int, emptyAtEntry bool, sv float64) (cutKind, float64) {
	if row.caption {
		if placed == 0 && emptyAtEntry {
			return cutHere, math.Inf(1)
		}
		if placedBody == 0 && !emptyAtEntry {
			return cutNothing, 0
		}
		return cutBreak, 0
	}
	if row.box.Style.BreakInside == style.BreakAvoid && cur.from == 0 && cur.rf.rect.H+2*sv <= l.bottom {
		switch {
		case placedBody > 0:
			return cutBreak, 0
		case !emptyAtEntry:
			return cutNothing, 0
		}
	}
	if !cur.hasCuts {
		cur.iv, cur.hasCuts = lineIntervals(cur.rf), true
	}
	avail := l.bottom - l.y - sv
	if c, ok := cutRow(cur.iv, cur.from, avail); ok {
		return cutHere, c
	}
	switch {
	case placed == 0 && emptyAtEntry:
		l.b.Warn(budget.WarnClamped, "table row sliced")
		return cutHere, math.Min(cur.rf.rect.H, cur.from+math.Max(avail, 1))
	case placedBody == 0 && !emptyAtEntry:
		return cutNothing, 0
	}
	return cutBreak, 0
}

// headerRows lays the header rows out once, at y = 0: the continuation pages
// place copies of them. A header too tall to repeat is measured but not kept.
func (l *layouter) headerRows(tl *tableLayout, inner cbox, decor style.TextDecoration, sv float64) error {
	tl.hdr.measured = true
	frs := make([]*frag, 0, tl.hdrEnd-tl.hdrStart)
	for i := tl.hdrStart; i < tl.hdrEnd; i++ {
		rf, err := l.layRow(tl, &tl.rows[i], inner, decor, false)
		if err != nil {
			return err
		}
		frs = append(frs, rf)
		tl.hdr.height += rf.rect.H + sv
	}
	if tl.hdr.height <= l.bottom/2 {
		tl.hdr.rows = frs
	}
	return nil
}

// repeatHeader places the header rows again at the top of a continuation page,
// when they take at most half of what is left.
func (l *layouter) repeatHeader(tl *tableLayout, f *frag, inner cbox, decor style.TextDecoration, sv float64) error {
	if !tl.hdr.measured {
		if err := l.headerRows(tl, inner, decor, sv); err != nil {
			return err
		}
	}
	if tl.hdr.rows == nil || tl.hdr.height > (l.bottom-l.y)/2 {
		return nil
	}
	if err := l.b.Charge(budget.Work, int64(len(tl.hdr.rows))); err != nil {
		return err
	}
	for i, rf := range tl.hdr.rows {
		l.y += sv
		piece := cutFrag(rf, 0, math.Inf(1))
		l.placeRow(f, &tl.rows[tl.hdrStart+i], piece, piece.rect.H)
	}
	l.pageEmpty = false
	return nil
}

// recolorBorder sets the colors of the border at the end of out: the colors of
// a collapsed border are resolved per edge, not taken from the style.
func recolorBorder(out display.List, colors *[4]css.Color) display.List {
	if b, ok := out[len(out)-1].(display.Border); ok {
		for i := range b.Sides {
			if b.Sides[i].Width > 0 {
				b.Sides[i].Color = colors[i]
			}
		}
		out[len(out)-1] = b
	}
	return out
}

// colRun is n consecutive columns of the same width, from column start.
type colRun struct {
	width    style.Dim
	start, n int
}

func (r colRun) sized() bool { return r.width.Kind == style.DimPx || r.width.Kind == style.DimPercent }

// columnRuns returns the widths of the columns defined by the col and colgroup
// children of a table (a colgroup with col children is defined by them), up to
// maxTableCols columns.
func columnRuns(b *box.Box) (runs []colRun, total int) {
	add := func(c *box.Box) {
		if n := min(c.ColSpan, maxTableCols-total); n > 0 {
			runs = append(runs, colRun{c.Style.Width, total, n})
			total += n
		}
	}
	for _, c := range b.Children {
		if c.Kind != box.KindBlock {
			continue
		}
		switch c.Style.Display {
		case style.DisplayTableColumn:
			add(c)
		case style.DisplayTableColumnGroup:
			cols := false
			for _, cc := range c.Children {
				if cc.Kind == box.KindBlock && cc.Style.Display == style.DisplayTableColumn {
					add(cc)
					cols = true
				}
			}
			if !cols {
				add(c)
			}
		}
	}
	return runs, total
}
