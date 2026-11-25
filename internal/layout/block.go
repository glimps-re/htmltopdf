package layout

import (
	"context"
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// layouter holds the state of the page being laid out.
type layouter struct {
	sh     *text.Shaper
	b      *budget.Budget
	probe  *text.Shaper
	breaks []text.Break
	width  float64 // page area width in layout px
	bottom float64 // page area height in layout px

	// y is the cursor: the bottom of the last placed content. pend holds the
	// margins that adjoin the cursor and are not applied yet.
	y    float64
	pend margins
	// waiting are the fragments whose top edge is the next flushed position:
	// their top margin collapses with what follows.
	waiting []*frag
	// truncate makes the next flush discard the pending margins: they adjoin a
	// page break (CSS Fragmentation §5.5).
	truncate bool
	// pageEmpty is true until something is placed on the page: the first line
	// or box of a page is always placed, so every page makes progress.
	pageEmpty bool
	// forced is true when the page ended on a forced break: the margins after
	// it are kept (CSS Fragmentation §5.5).
	forced bool
	// Avoid measurement (breaks.go): measuring is true while a box is laid out
	// alone to see whether it fits a page, measures counts them on this page,
	// measured memoizes the results, onMeasure is a test hook.
	measuring bool
	// pageName is the used page name of the block being laid out.
	pageName string
	// unbounded is true while content is laid out on an unbounded scratch page
	// (a cell, an atomic inline): nothing breaks there, so a forced break is
	// ignored and the content stays whole.
	unbounded bool
	measures  int
	measured  map[measureKey]bool
	onMeasure func(*box.Box)
	// cbs are the containing blocks of the absolute boxes being laid out.
	cbs []*absCtx
	// fixedReqs are the fixed boxes met in the flow of the page being laid out.
	fixedReqs []absReq
	// cols is the number of multi-column containers being laid out around the
	// box: one inside another is a plain block. onColumns is a test hook called
	// for each layout of the content of a segment in columns.
	cols      int
	onColumns func()
	// sliced is set when a box with break-inside: avoid is split because it does
	// not fit a page (or a column): a column height that does that is too short.
	sliced bool

	root      *box.Box
	mono      monoBox
	para      *paragraph
	segs      []seg
	primaries map[*style.Computed]primary
	ext       map[extKey]extVal
	tables    map[*box.Box]*tableLayout
	stripes   map[*box.Box]*stripeState
	sizes     map[*box.Box]sizeEntry
}

func newLayouter(root *box.Box, cfg Config, width, height float64) *layouter {
	return &layouter{root: root, sh: cfg.Shaper, probe: newProbe(cfg), b: cfg.Budget, width: width, bottom: height, ext: make(map[extKey]extVal), primaries: make(map[*style.Computed]primary)}
}

func (l *layouter) startPage(resumed bool) {
	l.y, l.pend, l.waiting = 0, margins{}, l.waiting[:0]
	l.truncate, l.pageEmpty, l.measures = resumed, true, 0
}

// flush applies the pending margins at the cursor and fixes the top of the
// fragments that waited for it.
func (l *layouter) flush() {
	if l.truncate {
		l.pend, l.truncate = margins{}, false
	}
	l.y = clampCoord(l.y + l.pend.resolve())
	if l.y < 0 {
		// A negative margin moves a box up within its page, never above the top of
		// the page area (or of a cell or item laid out apart, which is placed on a
		// page): what is drawn there is on no page, so its text would be lost.
		// Chrome puts such a box on an earlier page, which the page-by-page layout
		// cannot do.
		l.y = 0
	}
	l.pend = margins{}
	for _, f := range l.waiting {
		f.rect.Y, f.yset = l.y, true
	}
	l.waiting = l.waiting[:0]
}

// hasInlineChildren reports whether b holds inline-level content: the box tree
// gives a block block-level children only or inline-level ones only, so the
// first child tells.
func hasInlineChildren(b *box.Box) bool {
	return len(b.Children) > 0 && !blockLevel(b.Children[0])
}

// newProbe returns the shaper layout uses to resolve fonts: same font set,
// scratch budget.
func newProbe(cfg Config) *text.Shaper {
	if cfg.Fonts == nil {
		return cfg.Shaper // the probe then counts in MaxGlyphs
	}
	var limits budget.Limits
	for i := range limits {
		limits[i] = math.MaxInt64
	}
	return text.NewShaper(cfg.Fonts, budget.New(context.Background(), limits, nil))
}

// layBlock lays out the block-level box b in the containing block cb. in holds
// the levels of the break token from b down when b is resumed. decor is the
// text decoration propagated from the ancestors.
func (l *layouter) layBlock(b *box.Box, in []level, cb cbox, decor style.TextDecoration) (result, error) {
	if err := l.b.Charge(budget.Work, 1); err != nil {
		return result{}, err
	}
	st := b.Style
	if st.Page != "" {
		outer := l.pageName
		l.pageName = st.Page
		defer func() { l.pageName = outer }()
	}
	resuming := len(in) > 0
	var lv level
	if resuming {
		lv = in[0]
	}
	g := geometry(st, cb)
	if b.Kind == box.KindSVG {
		g = svgGeom(b, g, cb)
	}
	if st.Display == style.DisplayTable && !st.TableLayoutFixed {
		var err error
		if g, err = l.fitTable(b, g, cb); err != nil {
			return result{}, err
		}
	}
	if b == l.mono.box {
		g = l.mono.fit(g)
		l.mono.box = nil
	}
	var (
		mc    colGeom
		multi bool
	)
	if !lv.childrenDone && b.Kind != box.KindSVG && st.Display != style.DisplayTable && st.Display != style.DisplayFlex && st.Display != style.DisplayGrid {
		mc, multi = l.columnsOf(st, g.w)
	}
	f := &frag{box: b, first: !resuming, last: true}
	f.border = [4]float64{g.bt, g.br, g.bb, g.bl}
	f.padding = [4]float64{g.pt, g.pr, g.pb, g.pl}
	if !resuming {
		l.pend.add(g.mt)
	}
	// The root element and boxes that clip establish a block formatting context:
	// the margins of their children do not collapse with their own.
	bfc := st.OverflowHidden || b == l.root || multi
	pre, post := g.pre(), g.post()
	emptyAtEntry := l.pageEmpty
	f.rect.X, f.rect.W = cb.x+g.ml, g.borderW

	switch {
	case resuming:
		f.rect.Y, f.yset = l.y, true
	case pre == 0 && !bfc:
		l.waiting = append(l.waiting, f)
	default:
		l.flush()
		f.rect.Y, f.yset = l.y, true
		if l.y > l.bottom {
			l.truncate = true
			return result{status: nothing}, nil
		}
	}

	// Top decoration (border and padding), sliced when it cannot fit.
	if rem := pre - lv.pre; rem > 0 {
		if room := l.bottom - l.y; rem > room {
			if !emptyAtEntry {
				l.unwait(f)
				return result{status: nothing}, nil
			}
			consume := math.Max(room, 1)
			l.y += consume
			l.pageEmpty = false
			f.last = false
			f.rect.H = math.Max(0, l.y-f.rect.Y)
			return result{f: f, status: partial, tok: []level{{child: lv.child, pre: lv.pre + consume, cur: lv.cur, used: lv.used}}}, nil
		}
		l.y += rem
		l.pageEmpty = false
	}

	cx := f.rect.X + g.bl + g.pl
	hh, hasH := usedHeight(st, g, cb)
	if b.Kind == box.KindSVG && !hasH {
		hh, hasH = svgAutoHeight(b, g.w, cb.h, cb.hdef), true
	}
	inner := cbox{x: cx, w: g.w, h: hh, hdef: hasH}
	if !resuming {
		for _, c := range b.OutOfFlow {
			l.registerAbs(c, inner)
		}
	}
	childDecor := decor | st.TextDecoration
	var (
		tok  []level
		stat = complete
	)
	switch {
	case lv.childrenDone:
	case b.Style.Display == style.DisplayTable:
		var err error
		stat, tok, err = l.tableRows(b, f, lv, inner, childDecor, resuming, emptyAtEntry)
		if err != nil {
			return result{}, err
		}
		if stat == nothing {
			return result{status: nothing}, nil
		}
	case b.Style.Display == style.DisplayFlex:
		var err error
		stat, tok, err = l.flex(b, f, lv, inner, childDecor, resuming, emptyAtEntry)
		if err != nil {
			return result{}, err
		}
		if stat == nothing {
			return result{status: nothing}, nil
		}
	case b.Style.Display == style.DisplayGrid:
		var err error
		stat, tok, err = l.grid(b, f, lv, inner, childDecor, resuming, emptyAtEntry)
		if err != nil {
			return result{}, err
		}
		if stat == nothing {
			return result{status: nothing}, nil
		}
	case multi:
		var err error
		stat, tok, err = l.multicol(b, f, in, lv, inner, mc, hh, hasH, childDecor, resuming, emptyAtEntry)
		if err != nil {
			return result{}, err
		}
		if stat == nothing {
			return result{status: nothing}, nil
		}
	case hasInlineChildren(b):
		var err error
		stat, tok, err = l.lines(b, f, lv, inner, childDecor, resuming, emptyAtEntry)
		if err != nil {
			return result{}, err
		}
		if stat == nothing {
			return result{status: nothing}, nil
		}
	default:
		var err error
		stat, tok, err = l.children(b, f, in, lv, inner, childDecor, resuming, emptyAtEntry)
		if err != nil {
			return result{}, err
		}
		if stat == nothing {
			return result{status: nothing}, nil
		}
	}
	if stat == partial && !f.yset {
		f.rect.Y, f.yset = l.y, true
	}
	here := 0.0
	if stat == partial && hasH {
		here = contentHere(f, l.y, pre, lv.pre)
	}
	if stat == partial && hasH && st.OverflowHidden && lv.used+here >= hh {
		// What is left would be clipped: the box ends at its height, and the
		// content that did not fit the page is dropped with the token.
		l.dropParagraph()
		l.pend, l.truncate = margins{}, false
		l.y = f.rect.Y + math.Max(0, pre-lv.pre) + math.Max(0, hh-lv.used)
		stat, tok = complete, nil
	}
	if stat == partial {
		tok[0].pre = pre
		f.last = false
		f.rect.H = math.Max(0, l.y-f.rect.Y)
		if hasH {
			// The fragments of the box together stay within its height: the
			// content past it overflows and goes on on the next pages.
			f.rect.H = math.Min(f.rect.H, math.Max(0, pre-lv.pre)+math.Max(0, hh-lv.used))
		}
		tok[0].used = lv.used + here
		return result{f: f, status: partial, tok: tok}, nil
	}

	// Bottom decoration; a box that clips, or has padding, border or a height,
	// contains the margins of its last child.
	if post > 0 || bfc || hasH {
		l.flush()
	}
	used := lv.used
	crossed := hasH && resuming && lv.used >= hh
	if hasH {
		if !f.yset {
			f.rect.Y, f.yset = l.y, true
			l.unwait(f)
		}
		used += contentHere(f, l.y, pre, lv.pre)
		if fill := hh - used; fill > 0 {
			room := l.bottom - l.y
			if b.Kind == box.KindSVG && fill > room && !l.pageEmpty {
				// A replaced box is not sliced: it moves whole to the next page; one
				// taller than a page stays and the page clips it.
				l.unwait(f)
				return result{status: nothing}, nil
			}
			if fill > room && b.Kind != box.KindSVG {
				consume := math.Max(room, 0)
				if l.pageEmpty {
					consume = math.Max(room, 1)
				}
				l.y += consume
				if consume > 0 {
					l.pageEmpty = false
				}
				f.last = false
				f.rect.H = math.Max(0, l.y-f.rect.Y)
				return result{f: f, status: partial, tok: []level{{childrenDone: true, pre: pre, post: lv.post, used: used + consume}}}, nil
			}
			l.y += fill
			used = hh
			l.pageEmpty = false
		} else if fill < 0 {
			// The content is taller than the height: it overflows, the box ends at the
			// height. Past a page break the overflow ended further down: the next
			// sibling follows it.
			if !crossed {
				l.y -= contentHere(f, l.y, pre, lv.pre) - (hh - lv.used)
			}
			used = math.Max(hh, lv.used)
		}
	}
	if rem := post - lv.post; rem > 0 {
		if room := l.bottom - l.y; rem > room {
			// Slice the decoration: what fits stays here, the rest goes on. When
			// the page already holds content, nothing of it stays: the
			// decoration starts the next page.
			consume := 0.0
			if l.pageEmpty {
				consume = math.Max(room, 1)
			}
			l.y += consume
			f.last = false
			f.rect.H = math.Max(0, l.y-f.rect.Y)
			return result{f: f, status: partial, tok: []level{{childrenDone: true, pre: pre, post: lv.post + consume, used: used}}}, nil
		}
		l.y += rem
		l.pageEmpty = false
	}
	if !f.yset {
		f.rect.Y, f.yset = l.y, true
		l.unwait(f)
	}
	f.rect.H = math.Max(0, l.y-f.rect.Y)
	if hasH && crossed {
		f.rect.H = math.Min(f.rect.H, math.Max(0, pre-lv.pre)+math.Max(0, post-lv.post))
	}
	l.pend.add(g.mb)
	return result{f: f, status: complete}, nil
}

// unwait drops f from the fragments waiting for a flush.
func (l *layouter) unwait(f *frag) {
	for i := len(l.waiting) - 1; i >= 0; i-- {
		if l.waiting[i] == f {
			l.waiting = append(l.waiting[:i], l.waiting[i+1:]...)
			return
		}
	}
}

// children lays out the block children of b from lv.child, applying the
// breaks between siblings (breaks.go).
func (l *layouter) children(b *box.Box, f *frag, in []level, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	var (
		chain    = -1
		prevLink bool
		snap     breakSnap
		kept     int
	)
	// backUp undoes the chain of avoided breaks that c is in and reports whether it
	// did. It needs content before the chain on this page: the chain does not
	// start it, or the block is not itself at the top of its page (which would
	// resume at the same child and make no progress).
	backUp := func() bool {
		chainStartsPage := chain == lv.child && (resuming || emptyAtEntry)
		if chain < 0 || l.measuring || snap.pageEmpty || chainStartsPage {
			return false
		}
		l.restoreBreak(&snap)
		clear(f.children[kept:])
		f.children = f.children[:kept]
		return true
	}
	// breakBefore ends the page before child i.
	breakBefore := func(i int) (status, []level, error) {
		if len(f.children) == 0 && !resuming && !emptyAtEntry {
			return nothing, nil, nil
		}
		l.pend, l.truncate = margins{}, true
		return partial, []level{{child: i}}, nil
	}
	for i := lv.child; i < len(b.Children); i++ {
		if pos := b.Children[i].Style.Position; pos == style.PositionAbsolute || pos == style.PositionFixed {
			l.registerAbs(b.Children[i], inner)
			continue
		}
		c := b.Children[i]
		if i > lv.child && !l.pageEmpty && !l.unbounded && l.pageBreakBetween(b.Children[i-1], c) {
			l.pend, l.forced = margins{}, true
			return partial, []level{{child: i}}, nil
		}
		link := i+1 < len(b.Children) && l.avoidBetween(c, b.Children[i+1])
		if !prevLink {
			chain = -1
		}
		if link && chain < 0 {
			chain, kept = i, len(f.children)
			l.saveBreak(&snap)
		}
		prevLink = link
		var childIn []level
		if resuming && i == lv.child && len(in) > 1 {
			childIn = in[1:]
		}
		cr, err := l.block(c, childIn, inner, decor)
		if err != nil {
			return complete, nil, err
		}
		switch cr.status {
		case complete:
			f.children = append(f.children, cr.f)
		case partial:
			// Only the bottom decoration of c goes on: the break that the chain avoids
			// is right after c, so the chain moves whole like for a block that did not fit.
			if link && len(cr.tok) == 1 && cr.tok[0].childrenDone && backUp() {
				return breakBefore(chain)
			}
			f.children = append(f.children, cr.f)
			return partial, append([]level{{child: i}}, cr.tok...), nil
		case nothing:
			if chain < i && backUp() {
				i = chain
			}
			return breakBefore(i)
		}
	}
	return complete, nil, nil
}

// usedHeight is the content height of a block with a specified height: a
// length, or a percentage of a containing block whose height is definite. ok is
// false for an auto height, and for a percentage of an auto one.
func usedHeight(st *style.Computed, g geom, cb cbox) (h float64, ok bool) {
	switch st.Height.Kind {
	case style.DimPx:
		h = st.Height.Value
	case style.DimPercent:
		if !cb.hdef {
			return 0, false
		}
		h = st.Height.Value / 100 * cb.h
	default:
		return 0, false
	}
	if st.BoxSizing == style.BorderBox {
		h -= g.pt + g.pb + g.bt + g.bb
	}
	return clampCoord(math.Max(0, h)), true
}

// contentHere is the height of the content of f on its page, from the end of the
// decoration at its top (pre of which lvPre is already behind) to y.
func contentHere(f *frag, y, pre, lvPre float64) float64 {
	return math.Max(0, y-f.rect.Y-math.Max(0, pre-lvPre))
}
