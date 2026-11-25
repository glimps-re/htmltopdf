package layout

import (
	"math"
	"sort"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// Fixed positioning (design §5.2.4): a fixed box has the page area as containing
// block, is laid out once for each page size and is a layer of every page from
// the first one, or, when it has no vertical inset, from the page of its static
// position (as Chrome prints it). Its layer is clipped by the page only.

// maxFixedItems bounds the display items of the fixed layers of a page: the
// boxes past it are emitted on the first page they appear on only, with one
// WarnClamped "fixed boxes", so that their text stays in the document once.
const maxFixedItems = 4096

// fixedLayout is a fixed box, and the fixed boxes without a vertical inset that
// it contains, laid out in one page area.
type fixedLayout struct {
	layers []*layer
	// items counts the display items, at least one per layer: an empty layer
	// costs as much as one with something to draw.
	items int
	// glyphs are the glyphs of the text: each emission after the first shapes
	// nothing but writes them again, which Glyphs bounds.
	glyphs int
}

// fixedLayer is a fixed box and the first page it is on. It is laid out once for
// each page area size it meets.
type fixedLayer struct {
	req   absReq
	start int
	sizes map[areaKey]*fixedLayout
	// dropped: a repeat of the box was refused, it stays on its first page: later
	// pages do not lay it out again.
	dropped bool
}

// areaKey identifies a page area size, in whole px as Chrome's viewport: the
// layout of a fixed box depends on it.
type areaKey struct{ w, h float64 }

// fixedSet is what a Paginator knows of the fixed boxes of its document.
type fixedSet struct {
	// pinned are the fixed boxes with a vertical inset: on every page from the first.
	pinned []*fixedLayer
	// positional are the boxes without a vertical inset, in the order they were met
	// in the flow, so that their start pages do not decrease.
	positional []*fixedLayer
	warned     bool
	// repeated counts the glyphs written again on the pages so far.
	repeated int64
	// cached counts the items of the layouts kept for later pages.
	cached int
}

// maxCachedFixedItems bounds the items of the layouts a Paginator keeps: past it
// a layout is made again for each page that needs it, which the per-page bound
// limits, instead of kept for every page size.
const maxCachedFixedItems = 1 << 16

// verticalAuto reports whether a fixed box has no vertical inset: its y is its
// static position.
func verticalAuto(st *style.Computed) bool {
	return st.Inset[style.Top].Kind == style.DimAuto && st.Inset[style.Bottom].Kind == style.DimAuto
}

// collectFixed returns the fixed boxes of the tree rooted at root with a vertical
// inset, in document order; each box visited costs one Work.
func collectFixed(root *box.Box, b *budget.Budget) ([]*fixedLayer, error) {
	var out []*fixedLayer
	stack := []*box.Box{root}
	for len(stack) > 0 {
		n := len(stack) - 1
		bx := stack[n]
		stack = stack[:n]
		if err := b.Charge(budget.Work, 1); err != nil {
			return nil, err
		}
		if isFixedBox(bx) && !verticalAuto(bx.Style) {
			out = append(out, &fixedLayer{req: absReq{b: bx}, start: 1})
		}
		for i := len(bx.OutOfFlow) - 1; i >= 0; i-- {
			stack = append(stack, bx.OutOfFlow[i])
		}
		for i := len(bx.Children) - 1; i >= 0; i-- {
			stack = append(stack, bx.Children[i])
		}
	}
	return out, nil
}

// isFixedBox reports whether bx is the box of a fixed element: its text and line
// breaks share its style, they are not fixed boxes.
func isFixedBox(bx *box.Box) bool {
	if bx.Style == nil || bx.Style.Position != style.PositionFixed || bx.Anonymous {
		return false
	}
	return bx.Kind == box.KindBlock || bx.Kind == box.KindInlineBlock || bx.Kind == box.KindSVG
}

// registerFixed records a fixed box met in the flow at its static position. A box
// with a vertical inset is laid out up front, not here.
func (l *layouter) registerFixed(c *box.Box, inner cbox) {
	if l.measuring || !verticalAuto(c.Style) {
		return
	}
	l.fixedReqs = append(l.fixedReqs, absReq{b: c, sx: inner.x, sy: clampCoord(l.y + l.pend.resolve()), cbw: inner.w})
}

// layout returns the layers of fl in the current page area.
func (fl *fixedLayer) layout(l *layouter, fs *fixedSet) (*fixedLayout, error) {
	key := areaKey{math.Floor(l.width), math.Floor(l.bottom)}
	if fx, ok := fl.sizes[key]; ok {
		return fx, nil
	}
	fx, err := l.layFixed(fl.req, key)
	if err != nil {
		return nil, err
	}
	if fl.sizes == nil {
		fl.sizes = make(map[areaKey]*fixedLayout, 1)
	}
	if len(fl.sizes) < maxPageStyles && fs.cached+fx.items <= maxCachedFixedItems {
		fl.sizes[key] = fx
		fs.cached += fx.items
	}
	return fx, nil
}

// layFixed lays out a fixed box in the page area (Chrome lays it out in the
// viewport, whose size is a whole number of px) with the fixed boxes without a
// vertical inset that it contains: their static position is inside it, and they
// are on the same pages.
func (l *layouter) layFixed(r absReq, key areaKey) (*fixedLayout, error) {
	cb := display.Rect{W: key.w, H: key.h}
	fx := &fixedLayout{}
	queue := []absReq{r}
	for i := 0; i < len(queue); i++ {
		q := queue[i]
		q.cbw = key.w
		mark := len(l.fixedReqs)
		f, err := l.layAbs(&q, cb)
		if err != nil {
			l.fixedReqs = l.fixedReqs[:mark]
			return nil, err
		}
		for _, n := range l.fixedReqs[mark:] {
			n.sx += f.rect.X
			n.sy += f.rect.Y
			queue = append(queue, n)
		}
		l.fixedReqs = l.fixedReqs[:mark]
		st := q.b.Style
		fx.layers = append(fx.layers, &layer{f: f, z: st.ZIndex, auto: st.ZAuto, fixed: true})
		list := (&emitter{}).emitStack(f, nil)
		fx.items += max(len(list), 1)
		for _, it := range list {
			if t, ok := it.(*display.Text); ok {
				fx.glyphs += len(t.Glyphs)
			}
		}
	}
	return fx, nil
}

// fixedLayers returns the layers to add to page number page, laid out for the
// current page area, and charges their items to Work and their repeated glyphs
// to Glyphs. Past maxFixedItems, or when the repeats would take more than a quarter
// of the Glyphs of the document, the layers that would repeat are dropped: each fixed box is on
// its first page only.
func (p *Paginator) fixedLayers(page int) ([]*layer, error) {
	fs := p.fixed
	if fs == nil {
		return nil, nil
	}
	l := p.l
	var out []*layer
	total, repeated, over := 0, 0, false
	// The repeats of the whole document may take a quarter of the Glyphs: the flow
	// needs the rest.
	room := p.cfg.Budget.Max(budget.Glyphs)/4 - fs.repeated
	take := func(fl *fixedLayer) error {
		if fl.dropped && fl.start != page {
			return nil
		}
		fx, err := fl.layout(l, fs)
		if err != nil {
			return err
		}
		if fx.items > maxFixedItems {
			// It can never repeat: it keeps no layout once this page has it.
			fl.dropped = true
		}
		defer func() {
			if fl.dropped {
				for _, kept := range fl.sizes {
					fs.cached -= kept.items
				}
				fl.sizes = nil
			}
		}()
		affordable := page == fl.start || int64(repeated+fx.glyphs) <= room
		switch {
		case !over && affordable && total+fx.items <= maxFixedItems:
		case fl.start == page:
			if !over {
				over = true
				p.warnFixed()
			}
		default:
			if !over {
				over = true
				p.warnFixed()
			}
			fl.dropped = true
			return nil
		}
		out = append(out, fx.layers...)
		total += fx.items
		if page > fl.start {
			repeated += fx.glyphs
		}
		return nil
	}
	for _, fl := range fs.pinned {
		if over && page > 1 {
			break
		}
		if err := take(fl); err != nil {
			return nil, err
		}
	}
	i := 0
	for i < len(fs.positional) && fs.positional[i].start <= page {
		if over {
			i = max(i, sort.Search(len(fs.positional), func(j int) bool { return fs.positional[j].start >= page }))
			for ; i < len(fs.positional) && fs.positional[i].start == page; i++ {
				if err := take(fs.positional[i]); err != nil {
					return nil, err
				}
			}
			break
		}
		if err := take(fs.positional[i]); err != nil {
			return nil, err
		}
		i++
	}
	fs.repeated += int64(repeated)
	if err := p.cfg.Budget.Charge(budget.Glyphs, int64(repeated)); err != nil {
		return nil, err
	}
	return out, p.cfg.Budget.Charge(budget.Work, int64(total))
}

func (p *Paginator) warnFixed() {
	if !p.fixed.warned {
		p.fixed.warned = true
		p.cfg.Budget.Warn(budget.WarnClamped, "fixed boxes")
	}
}

// placeFixedRequests records the fixed boxes met in the flow of the page just laid
// out: they are on this page and the following ones.
func (p *Paginator) placeFixedRequests() {
	l := p.l
	if len(l.fixedReqs) == 0 {
		return
	}
	if p.fixed == nil {
		p.fixed = &fixedSet{}
	}
	reqs := l.fixedReqs
	l.fixedReqs = nil
	// Boxes that start on this page come before those of the next, so that the
	// start pages of the list do not decrease.
	var next []*fixedLayer
	for i := range reqs {
		if reqs[i].sy >= l.bottom {
			// The static position is below this page: the box starts the next one, or,
			// when the document ends here, stays on this one: its text is not dropped.
			reqs[i].sy = math.Min(reqs[i].sy-l.bottom, math.Max(l.bottom-1, 0))
			if !p.done {
				next = append(next, &fixedLayer{req: reqs[i], start: p.number + 1})
				continue
			}
		}
		p.fixed.positional = append(p.fixed.positional, &fixedLayer{req: reqs[i], start: p.number})
	}
	p.fixed.positional = append(p.fixed.positional, next...)
}
