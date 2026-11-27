package layout

import (
	"cmp"
	"math"
	"slices"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// layMono lays out b as a block whose border box is w wide, at x = 0 and y = 0
// on an unbounded page, and returns its whole fragment. Margins are not part of
// the fragment. Percentages of its padding refer to w.
func (l *layouter) layMono(b *box.Box, w float64, decor style.TextDecoration) (*frag, error) {
	return l.layMonoIn(b, w, w, decor)
}

// monoBox is the box layMonoIn is laying out and the border box width it is
// forced to: layBlock reads it for that box only, the first time it lays it out.
// That is the only time: layMonoIn runs on a scratch page (enterScratch), where
// nothing is measured (block() does not lay a box out twice), so the forced
// width cannot be consumed by a measuring pass and missed by the real one.
type monoBox struct {
	box *box.Box
	w   float64
}

// fit forces the geometry of the box to the width w and no margins; the
// callers resolve both.
func (m monoBox) fit(g geom) geom {
	extra := g.pl + g.pr + g.bl + g.br
	g.mt, g.mb, g.ml, g.mr = 0, 0, 0, 0
	g.w = math.Max(0, m.w-extra)
	g.borderW = g.w + extra
	return g
}

// layMonoIn lays out b as a block whose border box is w wide, at x = 0 and y = 0
// on an unbounded page, and returns its whole fragment; margins are not part of
// it. cbw is the width of its containing block, which the percentages of its
// padding refer to.
func (l *layouter) layMonoIn(b *box.Box, w, cbw float64, decor style.TextDecoration) (*frag, error) {
	return l.layMonoCB(b, w, cbox{w: cbw}, decor)
}

// layMonoCB is layMonoIn in the containing block cb, whose height may be definite.
func (l *layouter) layMonoCB(b *box.Box, w float64, cb cbox, decor style.TextDecoration) (*frag, error) {
	s := l.enterScratch()
	root, mono := l.root, l.mono
	// Making b the root gives it a block formatting context, which an atomic
	// inline or a flex item has: the margins of its children stay inside.
	l.root, l.mono = b, monoBox{box: b, w: w}
	defer func() {
		l.leaveScratch(s)
		l.root, l.mono = root, mono
	}()
	res, err := l.block(b, nil, cb, decor)
	if err != nil {
		return nil, err
	}
	l.flush()
	if res.f == nil {
		return &frag{box: b, first: true, last: true, yset: true, rect: display.Rect{W: w}}, nil
	}
	return res.f, nil
}

// lastBaseline is the baseline of the last line box in f, in the coordinates
// of f; ok is false when f has none.
func lastBaseline(f *frag) (y float64, ok bool) {
	if n := len(f.lines); n > 0 {
		return f.lines[n-1].baseline, true
	}
	for i := len(f.children) - 1; i >= 0; i-- {
		if y, ok := lastBaseline(f.children[i]); ok {
			return y, true
		}
	}
	return 0, false
}

// atomBox is an atomic inline: a block laid out once at its own width. Its
// fragment is moved to the line it lands on.
type atomBox struct {
	f *frag
	// x and y are the current top left corner of the border box.
	x, y           float64
	ml, mr, mt, mb float64
	// base is the distance from the top of the border box to the baseline.
	base float64
	// rise is the vertical-align: <length> offset of the atom, up.
	rise   float64
	valign style.VerticalAlign
	// mid is the height of the middle of the lowercase letters above the
	// baseline of the line, for vertical-align: middle.
	mid float64
	// h is the height the atom takes in its line and in the fit and cut tests:
	// its border box height, or less when capped is set (see capAtom).
	h      float64
	capped bool
	// ink is the extent of the content of a capped atom: its height never goes
	// below it.
	ink float64
}

// extents returns the height of the margin box above and below the baseline
// of the line, for the alignment of a.
func (a *atomBox) extents() (above, below float64) {
	h := a.h
	switch a.valign {
	case style.VAlignMiddle:
		half := (a.mt + h + a.mb) / 2
		return half + a.mid, half - a.mid
	case style.VAlignTop, style.VAlignBottom:
		return a.mt + h + a.mb, 0
	}
	return a.base + a.mt + a.rise, h - a.base + a.mb - a.rise
}

// top returns the y of the border box for a line box of top y0, height h and
// baseline at y0 + up.
func (a *atomBox) top(y0, h, up float64) float64 {
	fh := a.h
	switch a.valign {
	case style.VAlignTop:
		return y0 + a.mt
	case style.VAlignBottom:
		return y0 + h - a.mb - fh
	case style.VAlignMiddle:
		return y0 + up - a.mid - fh/2
	}
	return y0 + up - a.base - a.rise
}

func (a *atomBox) moveTo(x, y float64) { a.shift(x-a.x, y-a.y) }

// spansLine is true for an atom aligned to the top or the bottom of the line
// box: it does not take part in the baseline alignment, it only makes the line
// at least as tall as itself.
func (a *atomBox) spansLine() bool {
	return a.valign == style.VAlignTop || a.valign == style.VAlignBottom
}

// clip returns an independent copy of a cut to the window [from, to): the
// fragments of a cut are moved on their own, so they must not share an atom.
func (a *atomBox) clip(c *cutter) *atomBox {
	ca := *a
	ca.f = c.frag(a.f)
	return &ca
}

// lineBottom returns the lowest edge a cut must not skip for the line ln: its
// box, its baseline (a line-height smaller than the font puts the glyphs below
// the box) and the in-flow content of its atoms, which a fixed-height
// inline-block can hold below the line.
func lineBottom(ln *lineFrag) float64 {
	bottom := math.Max(ln.rect.Y+ln.rect.H, ln.baseline)
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil {
			bottom = math.Max(bottom, a.bottom(reachBottom))
		} else if t := ln.items[i].text; t != nil {
			bottom = math.Max(bottom, t.Baseline)
		}
	}
	return bottom
}

// lineTop returns the highest edge of the line ln a cut must look at: its box,
// its baseline and the tops of its glyphs, which a line-height smaller than
// the font puts above the box.
func lineTop(ln *lineFrag) float64 {
	top := math.Min(ln.rect.Y, ln.baseline)
	for i := range ln.items {
		if ln.items[i].text != nil {
			top = math.Min(top, ln.baseline-ln.items[i].ascent)
		}
	}
	return top
}

// lineFitBottom is the bottom of the line ln as the fit tests count it: its box
// and the overflow of its atoms, not its baseline (block flow fits line boxes).
func lineFitBottom(ln *lineFrag) float64 {
	bottom := ln.rect.Y + ln.rect.H
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil {
			bottom = math.Max(bottom, a.bottom(overflowBottom))
		}
	}
	return bottom
}

// bottom is the lowest edge of the atom for the fit and cut tests: of(a.f), or
// the bottom of its capped height. A cut copy of the atom keeps its top in a.y.
func (a *atomBox) bottom(of func(*frag) float64) float64 {
	if a.capped {
		return a.y + a.h
	}
	return of(a.f)
}

// capAtom sets a.h and a.base, the height the atom takes in its line and its
// baseline (base when hasBase, else the bottom margin edge), after clamping its
// vertical margins to ±page (its vertical-align offset is already within
// ±maxRiseFraction of a page, see clampRise), which lowerLift then lowers so
// that the text of the line stays on a page. It runs once a.f is
// laid out; page <= 0 caps nothing.
//
// An atom taller than the page takes at most the room a page leaves its line,
// and never less than the extent of its content (inkBottom): its fragment keeps
// its full height, drawn and clipped by the page, while the text of an
// inline-block still lands on pages. Without the cap, a hostile height, margin
// or offset of 1e8 px makes a line, a flex line or a table row sliced over one
// page per page height, and the page budget fails the render.
func (a *atomBox) capAtom(page, strut, base float64, hasBase bool) {
	if page > 0 {
		a.mt, a.mb = clampAbs(a.mt, page), clampAbs(a.mb, page)
	}
	h := a.f.rect.H
	a.h, a.base = h, h+a.mb
	if hasBase {
		a.base = base
	}
	defer a.lowerLift(page, strut)
	if page <= 0 || h <= page {
		return
	}
	room := page - math.Max(a.mt, 0) - math.Max(a.mb, 0)
	if !a.spansLine() {
		room -= strut
	}
	a.ink = inkBottom(a.f, page) - a.f.rect.Y
	if c := math.Max(room, a.ink); c < h {
		a.capped = true
		a.setCap(c)
	}
}

// lowerLift lowers the positive vertical-align offset, then the positive
// margins, that put the baseline of the line more than a page area below the
// top of a's margin box (page - strut for a baseline or middle atom, whose
// line keeps strut below its baseline; page for a bottom atom, which ends at
// the bottom of its line). Each is clamped to ±page already, but together
// they can still push the text of the line past the bottom of the page the
// line starts on, where it would be clipped out of sight. The height of a
// itself is not lowered: a capped atom is already shorter than the page.
func (a *atomBox) lowerLift(page, strut float64) {
	if page <= 0 {
		return
	}
	var ex float64
	switch a.valign {
	case style.VAlignTop:
		return
	case style.VAlignBottom:
		ex = a.mt + a.h + a.mb - page
	case style.VAlignMiddle:
		// Both margins lift the baseline by half their value.
		above, _ := a.extents()
		ex = 2 * (above - (page - strut))
	default:
		above, _ := a.extents()
		ex = above - (page - strut)
		a.rise -= takePositive(&ex, a.rise)
	}
	a.mt -= takePositive(&ex, a.mt)
	if (a.valign != style.VAlignBaseline && a.valign != style.VAlignLength) || a.base >= a.h+a.mb {
		// The bottom margin lifts a baseline atom only when the atom has no
		// baseline of its own: its baseline is then the bottom margin edge.
		a.mb -= takePositive(&ex, a.mb)
		a.base = math.Min(a.base, a.h+a.mb)
	}
}

// takePositive returns the part of the excess *ex that v, when positive, can
// absorb, and deducts it from *ex.
func takePositive(ex *float64, v float64) float64 {
	if *ex <= 0 || v <= 0 {
		return 0
	}
	d := math.Min(*ex, v)
	*ex -= d
	return d
}

// setCap sets the capped height c of the atom and moves what depends on it.
func (a *atomBox) setCap(c float64) {
	a.h = c
	// The baseline stays inside the capped box: below it, the extent under the
	// baseline (h - base + mb) would be negative.
	a.base = math.Min(a.base, c+a.mb)
	liftLayers(a.f, a.y+c)
}

// shrinkCapped lowers the capped atoms of the line ln by over, down to the
// extent of their content, so that a line placed below the top of an empty page
// (under a margin) ends on it. Nothing changes when an atom cannot fit room,
// the height left on the page, anyway (a capped one by its content): the line
// is sliced, and a shrunk image would only lose its height. It reports whether
// an atom changed: the line must then be built again.
func (ln *lineOut) shrinkCapped(over, room float64) bool {
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil && (a.capped && a.ink > room || !a.capped && a.h > room) {
			return false
		}
	}
	changed := false
	for i := range ln.items {
		if a := ln.items[i].atom; a != nil && a.capped {
			if c := math.Max(a.ink, a.h-over); c < a.h {
				a.setCap(c)
				changed = true
			}
		}
	}
	return changed
}

func clampAbs(v, limit float64) float64 { return math.Max(-limit, math.Min(limit, v)) }

// inkBottom is the lowest point, in the coordinates of f, of the content of f
// that must land on a page: the glyphs of its lines (see textInk), the
// atoms in them as their fit tests count them, the boxes of a specified height
// to their bottom and, recursively, the content of the other boxes. The height
// of f itself does not count, nor that of a replaced box, which the page clips.
// It cannot reuse measure, which counts both. Nested atoms are not walked: their
// bottom is cached or capped, so each fragment is walked once.
func inkBottom(f *frag, page float64) float64 {
	bottom := f.rect.Y
	for _, ln := range f.lines {
		for i := range ln.items {
			switch it := &ln.items[i]; {
			case it.text != nil:
				bottom = math.Max(bottom, textInk(it.text, ln.rect.Y, page))
			case it.atom != nil:
				bottom = math.Max(bottom, it.atom.bottom(reachBottom))
			}
		}
	}
	for _, c := range f.children {
		switch {
		case c.box != nil && c.box.Replaced():
			bottom = math.Max(bottom, c.rect.Y)
		case c.box != nil && c.box.Style != nil && c.box.Style.Height.Kind != style.DimAuto:
			bottom = math.Max(bottom, reachBottom(c))
		default:
			bottom = math.Max(bottom, inkBottom(c, page))
		}
	}
	return bottom
}

// textInk is the bottom of the glyphs of t, in a line box of top top: its
// baseline plus its size, when the baseline is within a page plus that size of
// the line top. Past it, the leading of a hostile line-height (empty) put the
// baseline there: the line counts one page, and its text is drawn where it is
// only when the atom is placed whole.
func textInk(t *display.Text, top, page float64) float64 {
	if t.Baseline-top > page+t.Size {
		return top + page
	}
	return t.Baseline + t.Size
}

// liftLayers moves up the absolute boxes in f that end below limit, the bottom
// of a capped atom, so that they end at limit (or start at the top of f): the
// page clips the atom there, and a layer is carried by the window of its top,
// so one below it would never be drawn.
func liftLayers(f *frag, limit float64) {
	f.laysOK = false
	for _, ly := range f.abs {
		top := ly.f.rect.Y
		if to := math.Max(f.rect.Y, limit-math.Max(ly.f.rect.H, 1)); to < top {
			moveFrag(ly.f, 0, to-top)
		}
	}
	for _, c := range f.children {
		liftLayers(c, limit)
	}
	for _, ln := range f.lines {
		for i := range ln.items {
			if a := ln.items[i].atom; a != nil && !a.capped {
				liftLayers(a.f, limit)
			}
		}
	}
}

// line returns the part of the line ln in the window [from, to), or nil.
// Each text item belongs to the window of its own baseline (a vertical-align
// offset moves it away from that of the line); the other items of a line
// without atom go with the window of the line's baseline. A line with atoms
// can be taller than a page and is cut through: it is in every window it
// overlaps, its atoms clipped to each.
func (c *cutter) line(ln *lineFrag) *lineFrag {
	from, to := c.from, c.to
	in := func(y float64) bool { return y >= from && y < to }
	atoms, own, text := false, in(ln.baseline), false
	for i := range ln.items {
		if ln.items[i].atom != nil {
			atoms = true
		} else if t := ln.items[i].text; t != nil && in(t.Baseline) {
			text = true
		}
	}
	if !atoms && !own && !text || atoms && (lineBottom(ln) <= from || ln.rect.Y >= to) {
		return nil
	}
	nl := *ln
	nl.items = make([]lineItem, 0, len(ln.items))
	for _, it := range ln.items {
		switch {
		case it.text != nil:
			if in(it.text.Baseline) {
				c := *it.text
				it.text = &c
				nl.items = append(nl.items, it)
			}
		case it.atom != nil:
			it.atom = it.atom.clip(c)
			nl.items = append(nl.items, it)
		case atoms || own:
			nl.items = append(nl.items, it)
		}
	}
	return &nl
}

// textCutEnd returns where a cut of f in the window [from, to) ends: to, or
// higher, at the top of the glyphs of a text that the end would cut through
// (its line box crosses the end, its baseline is at or past it, its glyphs
// start above it), which would go to the next window with its top clipped.
// The end is never above floor, and a glyph taller than the window keeps the
// window of its baseline. page bounds how far below the window a glyph is
// looked for. The count returned, for Work, is the items looked at below the
// window: as for a cut, those in it are its output, paid once per page.
func textCutEnd(f *frag, from, to, floor, page float64) (float64, int64) {
	// Moving the end up can put it astride another text, so the end moves on,
	// in one sweep over the candidates, baselines down: a text whose baseline
	// is above the current end cannot straddle it, nor any end above it, nor can
	// the texts after it. The floor (callers pass the middle of the window)
	// keeps a pile of overlapping glyphs from shrinking the windows and
	// multiplying the pages.
	var cands []straddle
	visits := collectStraddles(f, from, to, page, &cands)
	slices.SortFunc(cands, func(a, b straddle) int { return cmp.Compare(b.baseline, a.baseline) })
	end := to
	for _, c := range cands {
		if c.baseline < end {
			break
		}
		if c.lo < end-cutEps && c.top >= floor && c.baseline-c.top <= end-from {
			end = c.top
		}
	}
	return end, visits
}

// textCutStart returns how far below the page top a window resuming f at from
// starts, so that the texts the previous end cut through (baseline at or past
// from, glyphs starting above it) are drawn whole: the depth of their highest
// glyph top above from, 0 when there is none. room is the height left on the
// page: a text deeper than half of it stays cut, so that the window keeps half
// of the page, and so does a glyph taller than room, cut whatever the start
// (as in textCutEnd). The count returned, for Work, is the items looked at in
// the page below from.
func textCutStart(f *frag, from, room, page float64) (float64, int64) {
	// A pile of overlapping lines has no end between glyphs: the floor of
	// textCutEnd then cuts through some, and their tops land here, above from.
	var cands []straddle
	visits := collectStraddles(f, from, from, page, &cands)
	depth := 0.0
	for _, c := range cands {
		if d := from - c.top; d <= room/2 && c.baseline-c.top <= room {
			depth = math.Max(depth, d)
		}
	}
	return depth, visits
}

// straddle is a text that a window end can go through: the top of its glyphs,
// lo the lowest end above which it does not straddle (its glyph top or its line
// box top, the lower), and its baseline.
type straddle struct{ top, lo, baseline float64 }

// collectStraddles appends to out the texts under f with a baseline in the
// window or below it and glyphs starting above to, and returns the items looked
// at below the window.
func collectStraddles(f *frag, from, to, page float64, out *[]straddle) int64 {
	var visits int64
	ci, cj, li, lj := 0, len(f.children), 0, len(f.lines)
	if ix := f.index(); ix != nil {
		ci, cj = window(ix.children, len(f.children), f.rect.Y, from, to+page)
		li, lj = window(ix.lines, len(f.lines), f.rect.Y, from, to+page)
	}
	for _, ln := range f.lines[li:lj] {
		if lineTop(ln) >= to {
			visits += int64(len(ln.items))
		}
		for i := range ln.items {
			it := &ln.items[i]
			if it.atom != nil {
				if mayStraddle(it.atom.f, from, to, page) {
					visits += collectStraddles(it.atom.f, from, to, page, out)
				}
				continue
			}
			if it.text == nil {
				continue
			}
			top := ln.baseline - it.ascent
			if it.text.Baseline >= from && top < to-cutEps && ln.rect.Y < to-cutEps {
				*out = append(*out, straddle{top: top, lo: math.Max(top, ln.rect.Y), baseline: it.text.Baseline})
			}
		}
	}
	for _, c := range f.children[ci:cj] {
		if c.rect.Y >= to {
			visits++
		}
		if mayStraddle(c, from, to, page) {
			visits += collectStraddles(c, from, to, page, out)
		}
	}
	return visits
}

// mayStraddle reports whether f can hold a text straddling an end of the
// window [from, to): its content reaches the window, and starts less than a
// page below its end (a glyph straddling it that starts more than a page above
// its box is taller than the window).
func mayStraddle(f *frag, from, to, page float64) bool {
	return reachBottom(f) >= from && f.rect.Y < to+page
}

func (a *atomBox) shift(dx, dy float64) {
	moveFrag(a.f, dx, dy)
	a.x += dx
	a.y += dy
}

// atomUnit lays out the atomic inline c in a containing block of width cbw.
func (l *layouter) atomUnit(p *paragraph, c *box.Box, decor style.TextDecoration, cbw, rise float64) (unit, error) {
	st := c.Style
	a := &atomBox{
		ml: autoMargin(st.Margin[style.Left], cbw), mr: autoMargin(st.Margin[style.Right], cbw),
		mt: autoMargin(st.Margin[style.Top], cbw), mb: autoMargin(st.Margin[style.Bottom], cbw),
		valign: st.VerticalAlign, mid: p.mid, rise: rise,
	}
	if st.VerticalAlign == style.VAlignLength {
		a.rise = l.clampRise(a.rise + st.VAlignOffset.Value)
	}
	bw, err := l.atomWidth(c, cbw)
	if err != nil {
		return unit{}, err
	}
	a.f, err = l.layMonoIn(c, bw, cbw, decor|st.TextDecoration)
	if err != nil {
		return unit{}, err
	}
	base, hasBase := lastBaseline(a.f)
	a.capAtom(l.pageH, p.strutBottom, base, hasBase && !st.OverflowHidden)
	return unit{kind: uAtom, atom: a, w: a.ml + a.f.rect.W + a.mr}, nil
}

// atomWidth is the border box width of an atomic inline: the specified width, or
// shrink-to-fit in cbw (CSS 2.1 §10.3.9).
func (l *layouter) atomWidth(b *box.Box, cbw float64) (float64, error) {
	st := b.Style
	if w, ok := replacedAtomWidth(b, cbw); ok {
		return w, nil
	}
	if st.Width.Kind == style.DimAuto {
		// Percentage paddings count 0 in the contributions: they are added to
		// the shrink-to-fit width of the rest.
		pct := borderPadding(st, cbw) - borderPadding(st, 0)
		w, err := l.shrinkToFit(b, cbw-pct)
		return w + pct, err
	}
	extra := borderPadding(st, cbw)
	return capWidth(st, contentWidth(st, px(st.Width, cbw), extra), cbw, extra) + extra, nil
}

func borderPadding(st *style.Computed, base float64) float64 {
	return padPx(st.Padding[style.Left], base) + padPx(st.Padding[style.Right], base) +
		st.Border[style.Left].Width + st.Border[style.Right].Width
}

// moveFrag moves f and everything in it, atomic inlines included.
func moveFrag(f *frag, dx, dy float64) {
	f.rect.X += dx
	f.rect.Y += dy
	moveContent(f, dx, dy)
}

// moveContent moves the children and the lines of f, not f itself.
func moveContent(f *frag, dx, dy float64) {
	for _, c := range f.children {
		moveFrag(c, dx, dy)
	}
	for _, a := range f.abs {
		moveFrag(a.f, dx, dy)
	}
	for i := range f.rules {
		f.rules[i].X += dx
		f.rules[i].Y += dy
	}
	for _, ln := range f.lines {
		ln.rect.X += dx
		ln.rect.Y += dy
		ln.baseline += dy
		for i := range ln.items {
			it := &ln.items[i]
			it.x += dx
			if t := it.text; t != nil {
				t.X += dx
				t.Baseline += dy
			}
			if a := it.atom; a != nil {
				a.shift(dx, dy)
			}
		}
	}
}
