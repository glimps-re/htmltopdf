package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// layMono lays out b as a block whose border box is w wide, at x = 0 and y = 0
// on an unbounded page, and returns its whole fragment. Margins are not part of
// the fragment.
func (l *layouter) layMono(b *box.Box, w float64, decor style.TextDecoration) (*frag, error) {
	return l.layMonoIn(b, w, w, decor)
}

// monoBox is the box layMonoIn is laying out and the border box width it is
// forced to: layBlock reads it for that box only.
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

// layMonoIn is layMono for a box in a containing block of width cbw, which the
// percentages of its padding refer to.
func (l *layouter) layMonoIn(b *box.Box, w, cbw float64, decor style.TextDecoration) (*frag, error) {
	s := l.enterScratch()
	root, mono := l.root, l.mono
	// Making b the root gives it a block formatting context, which an atomic
	// inline or a flex item has: the margins of its children stay inside.
	l.root, l.mono = b, monoBox{box: b, w: w}
	defer func() {
		l.leaveScratch(s)
		l.root, l.mono = root, mono
	}()
	res, err := l.block(b, nil, cbox{w: cbw}, decor)
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
	base   float64
	valign style.VerticalAlign
	// mid is the height of the middle of the lowercase letters above the
	// baseline of the line, for vertical-align: middle.
	mid float64
}

// extents returns the height of the margin box above and below the baseline
// of the line, for the alignment of a.
func (a *atomBox) extents() (above, below float64) {
	h := a.f.rect.H
	switch a.valign {
	case style.VAlignMiddle:
		half := (a.mt + h + a.mb) / 2
		return half + a.mid, half - a.mid
	case style.VAlignTop, style.VAlignBottom:
		return a.mt + h + a.mb, 0
	}
	return a.base + a.mt, h - a.base + a.mb
}

// top returns the y of the border box for a line box of top y0, height h and
// baseline at y0 + up.
func (a *atomBox) top(y0, h, up float64) float64 {
	fh := a.f.rect.H
	switch a.valign {
	case style.VAlignTop:
		return y0 + a.mt
	case style.VAlignBottom:
		return y0 + h - a.mb - fh
	case style.VAlignMiddle:
		return y0 + up - a.mid - fh/2
	}
	return y0 + up - a.base
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
func (a *atomBox) clip(from, to float64) *atomBox {
	c := *a
	c.f = cutFrag(a.f, from, to)
	return &c
}

// cutLine returns the part of the line ln in the window [from, to), or nil.
// A line without atom belongs to the window of its baseline. A line with
// atoms can be taller than a page and is cut through: it is in every window it
// overlaps, its text in the one of its baseline, its atoms clipped to each.
func cutLine(ln *lineFrag, from, to float64) *lineFrag {
	atoms := false
	for i := range ln.items {
		if ln.items[i].atom != nil {
			atoms = true
			break
		}
	}
	if !atoms && (ln.baseline < from || ln.baseline >= to) || atoms && (ln.rect.Y+ln.rect.H <= from || ln.rect.Y >= to) {
		return nil
	}
	nl := *ln
	nl.items = make([]lineItem, 0, len(ln.items))
	own := ln.baseline >= from && ln.baseline < to
	for _, it := range ln.items {
		switch {
		case it.text != nil:
			if !atoms || own {
				c := *it.text
				it.text = &c
				nl.items = append(nl.items, it)
			}
		case it.atom != nil:
			it.atom = it.atom.clip(from, to)
			nl.items = append(nl.items, it)
		default:
			nl.items = append(nl.items, it)
		}
	}
	return &nl
}

func (a *atomBox) shift(dx, dy float64) {
	moveFrag(a.f, dx, dy)
	a.x += dx
	a.y += dy
}

// atomUnit lays out the atomic inline c in a containing block of width cbw.
func (l *layouter) atomUnit(p *paragraph, c *box.Box, decor style.TextDecoration, cbw float64) (unit, error) {
	st := c.Style
	a := &atomBox{
		ml: autoMargin(st.Margin[style.Left], cbw), mr: autoMargin(st.Margin[style.Right], cbw),
		mt: autoMargin(st.Margin[style.Top], cbw), mb: autoMargin(st.Margin[style.Bottom], cbw),
		valign: st.VerticalAlign, mid: p.mid,
	}
	bw, err := l.atomWidth(c, cbw)
	if err != nil {
		return unit{}, err
	}
	a.f, err = l.layMonoIn(c, bw, cbw, decor|st.TextDecoration)
	if err != nil {
		return unit{}, err
	}
	a.base = a.f.rect.H + a.mb
	if b, ok := lastBaseline(a.f); ok && !st.OverflowHidden {
		a.base = b
	}
	return unit{kind: uAtom, atom: a, w: a.ml + a.f.rect.W + a.mr}, nil
}

// atomWidth is the border box width of an atomic inline: the specified width, or
// shrink-to-fit in cbw (CSS 2.1 §10.3.9).
func (l *layouter) atomWidth(b *box.Box, cbw float64) (float64, error) {
	st := b.Style
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
