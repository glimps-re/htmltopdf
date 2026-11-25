package layout

import (
	"math"
	"sort"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// Positioning (design §5.2.3): absolute boxes are out of flow. Each one is
// registered where its static position is, and laid out with layMono when the
// fragment of its containing block ends, once; the result is a layer attached
// to that fragment. Layers are painted per page (emitStack), atomically.

// layer is an absolute box laid out in the page coordinates.
type layer struct {
	f *frag
	// z is the z-index, auto for the tree order among the 0 ones.
	z    int32
	auto bool
}

// absReq is an absolute box waiting for its containing block: sx and sy are
// its static position, the top left of its margin box where it would have been.
type absReq struct {
	b      *box.Box
	sx, sy float64
	cbw    float64
}

// absCtx is the containing block fragment being laid out.
type absCtx struct {
	pending []absReq
	// page is true for the root block: its containing block is the page area.
	page bool
}

// isContainingBlock reports whether b is the containing block of the absolute
// boxes inside it: a positioned box, the root block of a page, or a box laid out
// as a unit (an atomic inline or a flex item, which keeps its content together).
func (l *layouter) isContainingBlock(b *box.Box) (page, ok bool) {
	switch {
	case b == l.mono.box:
		return false, true
	case b == l.root:
		return true, true
	case b.Style.Position != style.PositionStatic:
		return false, true
	}
	return false, false
}

// pushCB opens the containing block of a table cell, which block does not lay
// out. A cell is laid out row-local, before the row is placed: its absolute boxes
// are resolved against the cell and move with it, whether or not it is positioned
// (as for an atomic inline).
func (l *layouter) pushCB() *absCtx {
	ctx := &absCtx{}
	l.cbs = append(l.cbs, ctx)
	return ctx
}

// popCB closes ctx and places its absolute boxes in the padding box of f.
func (l *layouter) popCB(ctx *absCtx, f *frag) error {
	if ctx == nil {
		return nil
	}
	l.cbs = l.cbs[:len(l.cbs)-1]
	if len(ctx.pending) == 0 {
		return nil
	}
	return l.placeAbs(f, ctx)
}

// block lays out the block-level box b. It adds, to blockBase, the positioning of
// b: the containing block of the absolute boxes inside it, its relative offset
// and its rotation.
func (l *layouter) block(b *box.Box, in []level, cb cbox, decor style.TextDecoration) (result, error) {
	st := b.Style
	page, isCB := l.isContainingBlock(b)
	var outer *absCtx
	mark := 0
	if n := len(l.cbs); n > 0 {
		outer = l.cbs[n-1]
		mark = len(outer.pending)
	}
	if isCB {
		l.cbs = append(l.cbs, &absCtx{page: page})
	}
	res, err := l.blockBase(b, in, cb, decor)
	var ctx *absCtx
	if isCB {
		ctx = l.cbs[len(l.cbs)-1]
		l.cbs = l.cbs[:len(l.cbs)-1]
	}
	if err != nil {
		return res, err
	}
	if res.status == nothing {
		if outer != nil && len(outer.pending) > mark {
			clear(outer.pending[mark:])
			outer.pending = outer.pending[:mark]
		}
		return res, nil
	}
	if res.f == nil {
		return res, nil
	}
	if st.Position == style.PositionRelative {
		res.f.dx, res.f.dy = relativeOffset(st, cb)
	}
	res.f.rot = st.Rotate
	if ctx != nil && len(ctx.pending) > 0 {
		err = l.placeAbs(res.f, ctx)
	}
	return res, err
}

// relativeOffset is the shift of a relative box: left else −right, top else
// −bottom, percentages of the containing block; those of top and bottom only
// when its height is definite, else 0.
func relativeOffset(st *style.Computed, cb cbox) (dx, dy float64) {
	left, right := st.Inset[style.Left], st.Inset[style.Right]
	top, bottom := st.Inset[style.Top], st.Inset[style.Bottom]
	switch {
	case left.Kind != style.DimAuto:
		dx = px(left, cb.w)
	case right.Kind != style.DimAuto:
		dx = -px(right, cb.w)
	}
	vert := func(d style.Dim) float64 {
		if d.Kind == style.DimPercent && !cb.hdef {
			return 0
		}
		return px(d, cb.h)
	}
	switch {
	case top.Kind != style.DimAuto:
		dy = vert(top)
	case bottom.Kind != style.DimAuto:
		dy = -vert(bottom)
	}
	return clampCoord(dx), clampCoord(dy)
}

// registerAbs records the absolute box c, a child of a box whose content box is
// inner, at the static position of the cursor.
func (l *layouter) registerAbs(c *box.Box, inner cbox) {
	if l.measuring {
		return
	}
	if len(l.cbs) == 0 {
		// Unreachable while the root block is laid out; kept as a bound.
		l.b.Warn(budget.WarnClamped, "absolute box")
		return
	}
	ctx := l.cbs[len(l.cbs)-1]
	ctx.pending = append(ctx.pending, absReq{
		b: c, sx: inner.x, sy: clampCoord(l.y + l.pend.resolve()), cbw: inner.w,
	})
}

// placeAbs lays out the absolute boxes of ctx in the padding box of f (the page
// area for the root block) and attaches them to f as layers.
func (l *layouter) placeAbs(f *frag, ctx *absCtx) error {
	var cb display.Rect
	if ctx.page {
		cb = display.Rect{W: l.width, H: l.bottom}
	} else {
		cb = paddingBoxAll(f)
	}
	for i := range ctx.pending {
		r := &ctx.pending[i]
		if err := l.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		af, err := l.layAbs(r, cb)
		if err != nil {
			return err
		}
		st := r.b.Style
		f.abs = append(f.abs, &layer{f: af, z: st.ZIndex, auto: st.ZAuto})
	}
	clear(ctx.pending)
	ctx.pending = ctx.pending[:0]
	return nil
}

// paddingBoxAll is the padding box of f with all its borders: the containing
// block of the absolute boxes of this fragment.
func paddingBoxAll(f *frag) display.Rect {
	r := f.rect
	return display.Rect{
		X: r.X + f.border[style.Left], Y: r.Y + f.border[style.Top],
		W: math.Max(0, r.W-f.border[style.Left]-f.border[style.Right]),
		H: math.Max(0, r.H-f.border[style.Top]-f.border[style.Bottom]),
	}
}

// layAbs lays out the absolute box of r in the containing block cb (CSS 2.1
// §10.3.7 and §10.6.4, with shrink-to-fit widths).
func (l *layouter) layAbs(r *absReq, cb display.Rect) (*frag, error) {
	b := r.b
	st := b.Style
	left, right := st.Inset[style.Left], st.Inset[style.Right]
	top, bottom := st.Inset[style.Top], st.Inset[style.Bottom]
	hasL, hasR := left.Kind != style.DimAuto, right.Kind != style.DimAuto
	hasT, hasB := top.Kind != style.DimAuto, bottom.Kind != style.DimAuto
	ml, mr := autoMargin(st.Margin[style.Left], cb.W), autoMargin(st.Margin[style.Right], cb.W)
	mt, mb := autoMargin(st.Margin[style.Top], cb.W), autoMargin(st.Margin[style.Bottom], cb.W)
	autoML, autoMR := st.Margin[style.Left].Kind == style.DimAuto, st.Margin[style.Right].Kind == style.DimAuto
	autoMT, autoMB := st.Margin[style.Top].Kind == style.DimAuto, st.Margin[style.Bottom].Kind == style.DimAuto
	l0, r0 := clampCoord(px(left, cb.W)), clampCoord(px(right, cb.W))
	t0, b0 := clampCoord(px(top, cb.H)), clampCoord(px(bottom, cb.H))

	var bw float64
	var err error
	switch {
	case st.Width.Kind == style.DimAuto && hasL && hasR:
		extra := borderPadding(st, cb.W)
		bw = capWidth(st, math.Max(0, cb.W-l0-r0-ml-mr-extra), cb.W, extra) + extra
	default:
		avail := cb.W
		switch {
		case hasL:
			avail -= l0
		case !hasR:
			// Left auto too: the box starts at its static position.
			avail -= math.Max(0, r.sx-cb.X)
		}
		if hasR {
			avail -= r0
		}
		if bw, err = l.absWidth(b, math.Max(0, avail-ml-mr), cb.W); err != nil {
			return nil, err
		}
	}
	bw = clampCoord(bw)
	f, err := l.layMonoCB(b, bw, cbox{w: cb.W, h: cb.H, hdef: true}, 0)
	if err != nil {
		return nil, err
	}
	if st.Height.Kind == style.DimAuto && hasT && hasB {
		vert := f.border[style.Top] + f.border[style.Bottom] + f.padding[style.Top] + f.padding[style.Bottom]
		f.rect.H = math.Max(vert, cb.H-t0-b0-mt-mb)
	}
	if hasL && hasR && st.Width.Kind != style.DimAuto {
		ml, mr = shareFree(cb.W-l0-r0-f.rect.W-ml-mr, ml, mr, autoML, autoMR)
	}
	if hasT && hasB && st.Height.Kind != style.DimAuto {
		mt, mb = shareFree(cb.H-t0-b0-f.rect.H-mt-mb, mt, mb, autoMT, autoMB)
	}
	var x, y float64
	switch {
	case hasL:
		x = cb.X + l0 + ml
	case hasR:
		x = cb.X + cb.W - r0 - mr - f.rect.W
	default:
		x = r.sx + ml
	}
	switch {
	case hasT:
		y = cb.Y + t0 + mt
	case hasB:
		y = cb.Y + cb.H - b0 - mb - f.rect.H
	default:
		y = r.sy + mt
	}
	moveFrag(f, clampCoord(x), clampCoord(y))
	return f, nil
}

// shareFree gives the free space of an over-specified axis to its auto margins
// (CSS 2.1 §10.3.7, §10.6.4): both share it equally, a negative amount goes to
// the second one.
func shareFree(free, m1, m2 float64, auto1, auto2 bool) (float64, float64) {
	switch {
	case auto1 && auto2:
		if free < 0 {
			return 0, m2 + free
		}
		return m1 + free/2, m2 + free/2
	case auto1:
		return m1 + free, m2
	case auto2:
		return m1, m2 + free
	}
	return m1, m2
}

// absWidth is the border box width of an absolute box that does not stretch: the
// specified width, or shrink-to-fit in avail.
func (l *layouter) absWidth(b *box.Box, avail, cbw float64) (float64, error) {
	st := b.Style
	if st.Width.Kind == style.DimAuto {
		extra := borderPadding(st, cbw)
		pct := extra - borderPadding(st, 0)
		w, err := l.shrinkToFit(b, avail-pct)
		return capWidth(st, math.Max(0, w+pct-extra), cbw, extra) + extra, err
	}
	return l.atomWidth(b, cbw)
}

// emitStack emits f with the layers of its subtree: those with a negative
// z-index first, then f, then the z auto and 0 ones in tree order, then the
// positive ones by z-index. Each layer is emitted as a unit by emitStack in turn.
// The overflow clips around the layers are kept open from one layer to the next
// as far as they are the same: only the changed suffix is emitted.
func (e *emitter) emitStack(f *frag, out display.List) display.List {
	var ls []placed
	collect(f, nil, nil, 0, 0, &ls)
	if len(ls) == 0 {
		return e.emit(f, out)
	}
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].rank() < ls[j].rank() })
	var open []display.PushClip
	i := 0
	for ; i < len(ls) && ls[i].rank() < 0; i++ {
		out, open = e.emitLayer(ls[i], out, open)
	}
	out, open = setClips(out, open, nil)
	out = e.emit(f, out)
	for ; i < len(ls); i++ {
		out, open = e.emitLayer(ls[i], out, open)
	}
	out, _ = setClips(out, open, nil)
	return out
}

// setClips closes and opens clips so that exactly want are open, given that
// open are.
func setClips(out display.List, open, want []display.PushClip) (display.List, []display.PushClip) {
	common := 0
	for common < len(open) && common < len(want) && open[common] == want[common] {
		common++
	}
	for range len(open) - common {
		out = append(out, display.PopClip{})
	}
	out = append(out, clipsToItems(want[common:])...)
	return out, want
}

func clipsToItems(cs []display.PushClip) display.List {
	l := make(display.List, len(cs))
	for i, c := range cs {
		l[i] = c
	}
	return l
}

// placed is a layer found in the tree of a stack with what applies to it: the
// overflow clips of the boxes that contain it and the offset of the relative
// boxes it is in.
type placed struct {
	*layer
	clips  []display.PushClip
	dx, dy float64
	// rots are the rotations of the boxes it is in, which turn it with them.
	rots []display.PushTransform
}

// rank orders layers: negative z first, auto and 0 together, then positive.
func (p placed) rank() int64 {
	if p.auto {
		return 0
	}
	return int64(p.z)
}

// maxLayerClips bounds the clips kept around a layer: paint draws no more.
const maxLayerClips = 200

// collect walks the in-flow tree of f, not entering the layers, and appends the
// layers it finds.
func collect(f *frag, clips []display.PushClip, rots []display.PushTransform, dx, dy float64, out *[]placed) {
	dx, dy = dx+f.dx, dy+f.dy
	if f.rot != 0 {
		rots = append(rots[:len(rots):len(rots)], rotation(f.rot, f.rect.Translate(dx, dy)))
	}
	if f.box.Style.OverflowHidden && f.rect.W > 0 && f.rect.H > 0 && len(clips) < maxLayerClips {
		pb := paddingBox(f)
		rad := radiiOf(f.box.Style, f.rect, f.first, f.last)
		clips = append(clips[:len(clips):len(clips)], display.PushClip{Rect: pb.Translate(dx, dy), Radii: innerRadii(rad, f)})
	}
	for _, a := range f.abs {
		*out = append(*out, placed{layer: a, clips: clips, dx: dx, dy: dy, rots: rots})
	}
	for _, c := range f.children {
		collect(c, clips, rots, dx, dy, out)
	}
	for _, ln := range f.lines {
		for i := range ln.items {
			if a := ln.items[i].atom; a != nil {
				collect(a.f, clips, rots, dx, dy, out)
			}
		}
	}
}

// emitLayer emits p with the clips that surround it, given the open ones, and
// returns the clips left open. The offset of the relative boxes that contain it
// is carried by the emitter.
func (e *emitter) emitLayer(p placed, out display.List, open []display.PushClip) (display.List, []display.PushClip) {
	ox, oy := e.ax, e.ay
	want := p.clips
	if ox != 0 || oy != 0 {
		want = make([]display.PushClip, len(p.clips))
		for i, c := range p.clips {
			c.Rect = c.Rect.Translate(ox, oy)
			want[i] = c
		}
	}
	out, open = setClips(out, open, want)
	for _, r := range p.rots {
		out = append(out, r.Translate(ox, oy))
	}
	e.ax, e.ay = ox+p.dx, oy+p.dy
	out = e.emitStack(p.f, out)
	e.ax, e.ay = ox, oy
	for range p.rots {
		out = append(out, display.PopTransform{})
	}
	return out, open
}

// rotation returns the transform of rotate(angle) about the center of r.
func rotation(angle float64, r display.Rect) display.PushTransform {
	sin, cos := math.Sincos(angle)
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	return display.PushTransform{M: [6]float64{cos, sin, -sin, cos, cx - (cos*cx - sin*cy), cy - (sin*cx + cos*cy)}}
}

// cutLayers returns the layers of a fragment that the piece of a row between from
// and to holds: those whose top is in it, as copies, so that the pieces do not
// share them (moving one piece would move them all).
func cutLayers(ls []*layer, from, to float64) []*layer {
	var out []*layer
	for _, a := range ls {
		if y := a.f.rect.Y; y >= from && y < to {
			out = append(out, &layer{f: cloneFrag(a.f), z: a.z, auto: a.auto})
		}
	}
	return out
}

// cloneFrag copies f and its content, layers included.
func cloneFrag(f *frag) *frag {
	return cutFrag(f, math.Inf(-1), math.Inf(1))
}
