package layout

import (
	"math"
	"strconv"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// measureWidth is the line width a piece of a margin box is laid out at: wide
// enough that its text (at most style.MaxContentBytes) does not wrap.
const measureWidth = 1e6

// maxCachedPieces bounds the shaped pieces kept for a render: the counter
// values change on every page, so they would otherwise grow with the page count.
const maxCachedPieces = 4096

// marginRows are the rows of margin boxes: the top one sits in the top margin,
// the bottom one in the bottom margin.
var marginRows = [2]struct {
	bottom bool
	boxes  [3]style.MarginPos
}{
	{false, [3]style.MarginPos{style.MarginTopLeft, style.MarginTopCenter, style.MarginTopRight}},
	{true, [3]style.MarginPos{style.MarginBottomLeft, style.MarginBottomCenter, style.MarginBottomRight}},
}

// pieceKey identifies a shaped piece of text of a margin box.
type pieceKey struct {
	pos style.MarginPos
	// ms tells the styles apart: one position has a different style on each
	// named page.
	ms   *marginStyles
	text string
}

// marginStyles are the styles the text of a margin box is laid out with, built
// once per render so that the style-keyed caches of the layouter keep hitting.
type marginStyles struct {
	// oneLine preserves white space, so that the spaces at the ends of a piece
	// keep their width; wrapped is the style of a text laid out in the box width.
	oneLine, wrapped *style.Computed
}

// wrapKey identifies a static text laid out wrapped at a width.
type wrapKey struct {
	pos   style.MarginPos
	ms    *marginStyles
	text  string
	width float64
}

// piece is one string of a margin box laid out on one line: its display items
// at the origin, the width of its line (extent) and its height.
type piece struct {
	items  display.List
	extent float64
	height float64
}

// marginSlot is one margin box of a row.
type marginSlot struct {
	box    *style.MarginBox
	pos    style.MarginPos
	pieces []*piece
	// static is true when the content has no counter: the same text on every
	// page, which wraps in its box; text is that text.
	static bool
	text   string
	// max is the max-content outer width: text, padding, border and margins.
	max float64
	// extent and height are those of the line made of the pieces.
	extent, height float64
}

// MarginBoxes lays out the margin boxes of page pg, once total, the page
// count, is known. The items are in page coordinates (origin at the top-left
// corner of the page, scale 1): paint them with a Geometry whose Area is the
// whole page. The result is nil when the page has no margin box.
//
// Widths follow CSS Paged Media 3 §5.3 for auto widths: the width of the row
// is shared in proportion to the max-content widths. When the center box
// exists, the outer boxes weigh as much as the wider of them, so that it
// stays centered. The text of a margin box stays on one line and is clipped to
// its box when it holds a page counter (a deviation: wrapping would shape the
// whole text again on every page), starting at the box start when it overflows;
// a text without counter wraps in its box and is laid out once per render. Each
// string is shaped once per render; only the page counters are shaped again on
// every page. pg.Number is the page number.
func (p *Paginator) MarginBoxes(pg *Page, total int) (display.List, error) {
	ps := p.pageStyle(pg.Name)
	if p.mb == nil {
		p.mb = newLayouter(nil, p.cfg, measureWidth, math.MaxInt32)
		p.pieces = make(map[pieceKey]*piece)
		p.mstyles = make(map[*style.MarginBox]*marginStyles)
		p.wraps = make(map[wrapKey]*wrapped)
	}
	availW := ps.Width - ps.Margin[style.Left] - ps.Margin[style.Right]
	var out display.List
	for _, row := range marginRows {
		y, h := 0.0, ps.Margin[style.Top]
		if row.bottom {
			y, h = ps.Height-ps.Margin[style.Bottom], ps.Margin[style.Bottom]
		}
		if h <= 0 || availW <= 0 {
			continue
		}
		var slots [3]marginSlot
		for i, pos := range row.boxes {
			mb := ps.Boxes[pos]
			if mb == nil {
				continue
			}
			s := &slots[i]
			s.box, s.pos = mb, pos
			if err := p.measureMargin(s, pg.Number, total, availW); err != nil {
				return nil, err
			}
		}
		widths := distribute(&slots, availW)
		x := ps.Margin[style.Left]
		for i := range slots {
			if s := &slots[i]; s.box != nil {
				var err error
				if out, err = p.layoutMargin(out, s, display.Rect{X: x, Y: y, W: widths[i], H: h}); err != nil {
					return nil, err
				}
			}
			x += widths[i]
		}
	}
	return out, nil
}

// pieceTexts splits the content of mb in the strings shaped separately: the
// static strings and the counter values. It reports whether the content has a
// counter.
func pieceTexts(mb *style.MarginBox, number, total int, dst []string) ([]string, bool) {
	counters := false
	for _, it := range mb.Content {
		var s string
		switch it.Kind {
		case style.ContentText:
			s = strings.Map(func(r rune) rune {
				if r == '\n' || r == '\r' || r == '\f' {
					return ' '
				}
				return r
			}, it.Text)
		case style.ContentPage:
			s, counters = strconv.Itoa(number), true
		case style.ContentPages:
			s, counters = strconv.Itoa(total), true
		}
		if s != "" {
			dst = append(dst, s)
		}
	}
	return dst, counters
}

// distribute returns the outer width of the three slots of a row, absent ones
// included: an absent outer box still takes room when the center box exists.
// The widths are in [0, avail] and add up to avail.
func distribute(slots *[3]marginSlot, avail float64) [3]float64 {
	present := [3]bool{slots[0].box != nil, slots[1].box != nil, slots[2].box != nil}
	var w [3]float64
	if present[1] {
		outer := math.Max(slots[0].max, slots[2].max)
		w = [3]float64{outer, slots[1].max, outer}
	} else {
		w = [3]float64{slots[0].max, 0, slots[2].max}
	}
	for i := range w {
		w[i] = math.Max(0, w[i])
	}
	sum := w[0] + w[1] + w[2]
	if sum <= 0 {
		// Nothing to measure: the boxes share the row equally; with a center box
		// the outer ones count too, so that it stays centered.
		for i := range w {
			if present[i] || present[1] {
				w[i] = 1
			}
		}
		sum = w[0] + w[1] + w[2]
	}
	for i := range w {
		w[i] = avail * w[i] / sum
	}
	return w
}

// stylesOf returns the styles of mb, built once.
func (p *Paginator) stylesOf(mb *style.MarginBox) *marginStyles {
	if ms, ok := p.mstyles[mb]; ok {
		return ms
	}
	one := mb.Style.ContentOnly()
	one.WhiteSpace = style.WhiteSpacePre
	wrap := mb.Style.ContentOnly()
	wrap.TextAlign = mb.Style.TextAlign
	ms := &marginStyles{oneLine: one, wrapped: wrap}
	p.mstyles[mb] = ms
	return ms
}

// textBox returns a block holding text in the style st.
func textBox(st *style.Computed, text string) *box.Box {
	return &box.Box{Kind: box.KindBlock, Style: st, Children: []*box.Box{{Kind: box.KindText, Style: st, Text: text}}}
}

// layoutBlock lays root out at width w and returns its display items at the
// origin and its height. p.mb lays out one margin box at a time.
func (p *Paginator) layoutBlock(root *box.Box, w float64) (display.List, float64, error) {
	l := p.mb
	l.root, l.width, l.bottom = root, w, math.MaxInt32
	l.startPage(false)
	res, err := l.block(root, nil, cbox{x: 0, w: w}, 0)
	if err != nil {
		return nil, 0, err
	}
	l.dropParagraph()
	if res.f == nil {
		return nil, 0, nil
	}
	return emit(res.f, nil), res.f.rect.H, nil
}

// piece returns the layout of text on one line, shaping it on a cache miss
// only.
func (p *Paginator) piece(pos style.MarginPos, ms *marginStyles, text string) (*piece, error) {
	key := pieceKey{pos, ms, text}
	if pc, ok := p.pieces[key]; ok {
		return pc, nil
	}
	items, h, err := p.layoutBlock(textBox(ms.oneLine, text), measureWidth)
	if err != nil {
		return nil, err
	}
	pc := &piece{items: items, height: h}
	for _, it := range items {
		if t, ok := it.(*display.Text); ok {
			pc.extent = math.Max(pc.extent, t.X+t.Width())
		}
	}
	if len(p.pieces) < maxCachedPieces {
		p.pieces[key] = pc
	}
	return pc, nil
}

func (p *Paginator) measureMargin(s *marginSlot, number, total int, availW float64) error {
	var buf [8]string
	texts, counters := pieceTexts(s.box, number, total, buf[:0])
	s.static = !counters
	ms := p.stylesOf(s.box)
	for _, text := range texts {
		pc, err := p.piece(s.pos, ms, text)
		if err != nil {
			return err
		}
		s.pieces = append(s.pieces, pc)
		s.extent += pc.extent
		s.height = math.Max(s.height, pc.height)
	}
	if s.static {
		s.text = strings.Join(texts, "")
	}
	g := geometry(s.box.Style, cbox{w: availW})
	s.max = s.extent + g.ml + g.mr + g.bl + g.br + g.pl + g.pr
	return nil
}

// wrapped is a static text laid out wrapped in a box width.
type wrapped struct {
	items  display.List
	height float64
}

// wrap lays the static text of s out wrapped at width, once per render.
func (p *Paginator) wrap(s *marginSlot, width float64) (*wrapped, error) {
	ms := p.stylesOf(s.box)
	key := wrapKey{s.pos, ms, s.text, width}
	if w, ok := p.wraps[key]; ok {
		return w, nil
	}
	items, h, err := p.layoutBlock(textBox(ms.wrapped, s.text), width)
	if err != nil {
		return nil, err
	}
	w := &wrapped{items: items, height: h}
	if len(p.wraps) < maxCachedPieces {
		p.wraps[key] = w
	}
	return w, nil
}

// layoutMargin draws one margin box in rect, the margin area it is given: the
// box fills it (inside its own margins), and the text is aligned inside by
// text-align and vertical-align.
func (p *Paginator) layoutMargin(out display.List, s *marginSlot, rect display.Rect) (display.List, error) {
	st := s.box.Style
	g := geometry(st, cbox{w: rect.W})
	bx := display.Rect{X: rect.X + g.ml, Y: rect.Y + g.mt, W: rect.W - g.ml - g.mr, H: rect.H - g.mt - g.mb}
	if bx.W <= 0 || bx.H <= 0 {
		return out, nil
	}
	rad := radiiOf(st, bx, true, true)
	out = appendBackground(out, st, bx, rad)
	out = appendBorder(out, st, bx, rad, [4]float64{g.bt, g.br, g.bb, g.bl})
	cw := bx.W - g.bl - g.br - g.pl - g.pr
	ch := bx.H - g.bt - g.bb - g.pt - g.pb
	if cw <= 0 || ch <= 0 {
		return out, nil
	}
	x0 := bx.X + g.bl + g.pl
	y0 := bx.Y + g.bt + g.pt
	out = append(out, display.PushClip{Rect: bx})
	if s.static && s.extent > cw {
		w, err := p.wrap(s, cw)
		if err != nil {
			return nil, err
		}
		out = appendShifted(out, w.items, x0, y0+verticalOffset(st.VerticalAlign, ch, w.height))
		return append(out, display.PopClip{}), nil
	}
	dx := x0
	if free := cw - s.extent; free > 0 {
		switch st.TextAlign {
		case style.AlignRight:
			dx += free
		case style.AlignCenter:
			dx += free / 2
		}
	}
	dy := y0 + verticalOffset(st.VerticalAlign, ch, s.height)
	start := len(out)
	for _, pc := range s.pieces {
		out = appendShifted(out, pc.items, dx, dy)
		dx += pc.extent
	}
	out = append(out[:start], mergeTexts(out[start:])...)
	return append(out, display.PopClip{}), nil
}

// verticalOffset is the offset of content of the given height in an area of
// height room, for a vertical-align.
func verticalOffset(va style.VerticalAlign, room, height float64) float64 {
	switch va {
	case style.VAlignMiddle:
		return (room - height) / 2
	case style.VAlignBottom:
		return room - height
	}
	return 0
}

// appendShifted appends the items moved by (dx, dy).
func appendShifted(out, items display.List, dx, dy float64) display.List {
	for _, it := range items {
		out = append(out, it.Translate(dx, dy))
	}
	return out
}

// mergeTexts joins consecutive text items of one face, size, color and
// decoration into one, so that the pieces of a margin box are drawn (and read
// back) as the one string they make. Only items that follow each other
// directly are joined.
func mergeTexts(items display.List) display.List {
	out := items[:0]
	for _, it := range items {
		t, ok := it.(*display.Text)
		if ok && len(out) > 0 {
			if prev, ok := out[len(out)-1].(*display.Text); ok && sameLook(prev, t) {
				prev.Glyphs = append(prev.Glyphs[:len(prev.Glyphs):len(prev.Glyphs)], t.Glyphs...)
				base := clamp32(len(prev.Text))
				for i := len(prev.Glyphs) - len(t.Glyphs); i < len(prev.Glyphs); i++ {
					prev.Glyphs[i].Cluster += base
				}
				prev.Text += t.Text
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

func sameLook(a, b *display.Text) bool {
	return a.Face == b.Face && a.Size == b.Size && a.Color == b.Color &&
		a.Underline == b.Underline && a.LineThrough == b.LineThrough && a.Baseline == b.Baseline
}
