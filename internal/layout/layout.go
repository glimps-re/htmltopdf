// Package layout lays the box tree out on pages.
//
// A Paginator lays out one page at a time (the WeasyPrint model): Next lays
// out from a BreakToken, returns the display list of the page and keeps the
// token of the next one. Block layout follows CSS 2.1 (margin collapsing,
// auto margins, percentages, box sizing); inline layout shapes text with
// internal/text, breaks it into lines with UAX #14 and overflow-wrap, and
// aligns it. Fragmentation happens between blocks, between lines, and inside
// top and bottom decorations that do not fit a page; the decorations of a
// fragmented box are sliced (design §3.2).
package layout

import (
	"io"
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// Scale bounds, as validated by the public options.
const (
	MinScale = 0.1
	MaxScale = 2
)

// Config is the input of a Paginator.
type Config struct {
	// Page gives the page size and margins.
	Page *style.PageStyle
	// Scale multiplies the content of the page area; 0 means 1, other values are
	// clamped to [MinScale, MaxScale]. The page area is laid out at 1/Scale of
	// its size, so more fits on the page.
	Scale float64
	// Fonts is the font set of Shaper: layout shapes a space on a scratch budget
	// to find the primary font of a style, which must not count in MaxGlyphs. When
	// nil, Shaper does it and each style costs one glyph.
	Fonts  *text.FontSet
	Shaper *text.Shaper
	Budget *budget.Budget
	// KeepTree keeps the fragment tree of each page for Dump. The tree holds
	// the glyphs of the page: leave it off in production.
	KeepTree bool
}

// Page is one laid out page.
type Page struct {
	// Number is the 1-based page number.
	Number int
	// Width and Height are the page size in CSS px.
	Width, Height float64
	// Area is the page area, inside the margins, in page px.
	Area display.Rect
	// Scale is the factor applied to the content: Items are in layout space,
	// relative to the origin of Area, and paint multiplies them by Scale.
	Scale float64
	Items display.List

	root *frag // kept only with Config.KeepTree
}

// Paginator lays a box tree out page by page. It is not safe for concurrent use.
type Paginator struct {
	cfg  Config
	root *box.Box
	l    *layouter
	// mb lays out the margin boxes; created on first use.
	mb      *layouter
	pieces  map[pieceKey]*piece
	mstyles map[*style.MarginBox]*marginStyles
	wraps   map[wrapKey]*wrapped
	area    display.Rect
	tok     []level
	// resumed is true once a page has been laid out: the next one starts at a
	// break and truncates the margins that adjoin it.
	resumed bool
	done    bool
	number  int
}

// NewPaginator returns a Paginator for the box tree rooted at root, which must
// be a block box (the html element).
func NewPaginator(root *box.Box, cfg Config) *Paginator {
	scale := cfg.Scale
	if scale == 0 || math.IsNaN(scale) {
		scale = 1
	}
	cfg.Scale = math.Max(MinScale, math.Min(MaxScale, scale))
	area := pageArea(cfg)
	return &Paginator{
		cfg:  cfg,
		root: root,
		area: area,
		l:    newLayouter(root, cfg, area.W/cfg.Scale, area.H/cfg.Scale),
	}
}

func pageArea(cfg Config) display.Rect {
	ps := cfg.Page
	return display.Rect{
		X: ps.Margin[style.Left],
		Y: ps.Margin[style.Top],
		W: math.Max(1, ps.Width-ps.Margin[style.Left]-ps.Margin[style.Right]),
		H: math.Max(1, ps.Height-ps.Margin[style.Top]-ps.Margin[style.Bottom]),
	}
}

// Next lays out the next page. The first page always exists, even for an empty
// document; io.EOF follows the last page. A page is charged to the Pages limit.
func (p *Paginator) Next() (*Page, error) {
	if p.done {
		return nil, io.EOF
	}
	if err := p.cfg.Budget.Charge(budget.Pages, 1); err != nil {
		return nil, err
	}
	if err := p.cfg.Budget.Poll(); err != nil {
		return nil, err
	}
	p.number++
	l := p.l
	l.startPage(p.resumed && !l.forced)
	l.forced = false
	res, err := l.block(p.root, p.tok, cbox{x: 0, w: l.width}, 0)
	if err != nil {
		return nil, err
	}
	pg := &Page{
		Number: p.number,
		Width:  p.cfg.Page.Width,
		Height: p.cfg.Page.Height,
		Area:   p.area,
		Scale:  p.cfg.Scale,
	}
	if p.cfg.KeepTree {
		pg.root = res.f
	}
	p.resumed = true
	switch res.status {
	case complete:
		p.done = true
	case partial:
		p.tok = res.tok
	case nothing:
		// The root could not start (a margin pushed it past the page): the same
		// position is retried on the next page, with the margins truncated.
	}
	if res.f != nil {
		e := &emitter{}
		pg.Items = e.emit(res.f, make(display.List, 0, 64))
		if err := p.cfg.Budget.Charge(budget.Work, int64(e.shadows)); err != nil {
			return nil, err
		}
		if e.clamped {
			p.cfg.Budget.Warn(budget.WarnClamped, "box-shadow items per page")
		}
	}
	return pg, nil
}

// cbox is the containing block of a box: the x and width of its content box.
type cbox struct{ x, w float64 }

type status uint8

const (
	complete status = iota
	partial
	nothing
)

// level is where the layout of one block resumes: the next child, the amount
// of its decorations already drawn, and the paragraph cursor.
type level struct {
	child        int
	pre, post    float64
	childrenDone bool
	cur          cursor
	// off is the height of the line, taller than a page, already placed on the
	// previous pages: the line at cur resumes there.
	off float64
}

type result struct {
	f      *frag
	status status
	// tok holds the levels from this block down, for a partial result.
	tok []level
}

// margins is a set of adjoining margins: the collapsed value is the largest
// positive plus the most negative one (CSS 2.1 §8.3.1).
type margins struct{ pos, neg float64 }

func (m *margins) add(v float64) {
	if v > 0 {
		m.pos = math.Max(m.pos, v)
	} else {
		m.neg = math.Min(m.neg, v)
	}
}

func (m margins) resolve() float64 { return m.pos + m.neg }
