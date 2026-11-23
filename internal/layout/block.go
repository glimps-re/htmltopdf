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

	root      *box.Box
	para      *paragraph
	segs      []seg
	primaries map[*style.Computed]primary
	ext       map[extKey]extVal
	tables    map[*box.Box]*tableLayout
}

func newLayouter(root *box.Box, cfg Config, width, height float64) *layouter {
	return &layouter{root: root, sh: cfg.Shaper, probe: newProbe(cfg), b: cfg.Budget, width: width, bottom: height, ext: make(map[extKey]extVal), primaries: make(map[*style.Computed]primary)}
}

func (l *layouter) startPage(resumed bool) {
	l.y, l.pend, l.waiting = 0, margins{}, l.waiting[:0]
	l.truncate, l.pageEmpty = resumed, true
}

// flush applies the pending margins at the cursor and fixes the top of the
// fragments that waited for it.
func (l *layouter) flush() {
	if l.truncate {
		l.pend, l.truncate = margins{}, false
	}
	l.y = clampCoord(l.y + l.pend.resolve())
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
	return len(b.Children) > 0 && b.Children[0].Kind != box.KindBlock
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

// block lays out the block-level box b in the containing block cb. in holds
// the levels of the break token from b down when b is resumed. decor is the
// text decoration propagated from the ancestors.
func (l *layouter) block(b *box.Box, in []level, cb cbox, decor style.TextDecoration) (result, error) {
	if err := l.b.Charge(budget.Work, 1); err != nil {
		return result{}, err
	}
	st := b.Style
	resuming := len(in) > 0
	var lv level
	if resuming {
		lv = in[0]
	}
	g := geometry(st, cb)
	f := &frag{box: b, first: !resuming, last: true}
	f.border = [4]float64{g.bt, g.br, g.bb, g.bl}
	f.padding = [4]float64{g.pt, g.pr, g.pb, g.pl}
	if !resuming {
		l.pend.add(g.mt)
	}
	// The root element and boxes that clip establish a block formatting context:
	// the margins of their children do not collapse with their own.
	bfc := st.OverflowHidden || b == l.root
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
			return result{f: f, status: partial, tok: []level{{child: lv.child, pre: lv.pre + consume, cur: lv.cur}}}, nil
		}
		l.y += rem
		l.pageEmpty = false
	}

	cx := f.rect.X + g.bl + g.pl
	inner := cbox{x: cx, w: g.w}
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
	if stat == partial {
		tok[0].pre = pre
		f.last = false
		if !f.yset {
			f.rect.Y, f.yset = l.y, true
		}
		f.rect.H = math.Max(0, l.y-f.rect.Y)
		return result{f: f, status: partial, tok: tok}, nil
	}

	// Bottom decoration; a box that clips, or has padding or border, contains
	// the margins of its last child.
	if post > 0 || bfc {
		l.flush()
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
			return result{f: f, status: partial, tok: []level{{childrenDone: true, pre: pre, post: lv.post + consume}}}, nil
		}
		l.y += rem
		l.pageEmpty = false
	}
	if !f.yset {
		f.rect.Y, f.yset = l.y, true
		l.unwait(f)
	}
	f.rect.H = math.Max(0, l.y-f.rect.Y)
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

// children lays out the block children of b from lv.child.
func (l *layouter) children(b *box.Box, f *frag, in []level, lv level, inner cbox, decor style.TextDecoration, resuming, emptyAtEntry bool) (status, []level, error) {
	for i := lv.child; i < len(b.Children); i++ {
		var childIn []level
		if resuming && i == lv.child && len(in) > 1 {
			childIn = in[1:]
		}
		cr, err := l.block(b.Children[i], childIn, inner, decor)
		if err != nil {
			return complete, nil, err
		}
		switch cr.status {
		case complete:
			f.children = append(f.children, cr.f)
		case partial:
			f.children = append(f.children, cr.f)
			return partial, append([]level{{child: i}}, cr.tok...), nil
		case nothing:
			if len(f.children) == 0 && !resuming && !emptyAtEntry {
				return nothing, nil, nil
			}
			l.pend, l.truncate = margins{}, true
			return partial, []level{{child: i}}, nil
		}
	}
	return complete, nil, nil
}
