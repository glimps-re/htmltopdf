package layout

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// sizes are the min-content and max-content contributions of a box: its margin
// box width, specified width and max-width applied (CSS Sizing 3 §5). Percent
// margins, padding and widths count as 0, as sizing against an unknown
// containing block does.
type sizes struct{ min, max float64 }

// intrinsic returns the contributions of b, cached per box for the render. A
// cache miss charges 1 Work unit per box visited; text is shaped through the
// main shaper. A table contributes its specified width, else 0 (an
// approximation until its automatic layout can be measured).
func (l *layouter) intrinsic(b *box.Box) (sizes, error) {
	e, err := l.sizeEntry(b)
	return e.outer, err
}

// sizeEntry is the cached contributions of a box: of its margin box, and of its
// content alone, before width and max-width apply (the automatic minimum of a
// flex item needs them).
type sizeEntry struct{ outer, content sizes }

func (l *layouter) sizeEntry(b *box.Box) (sizeEntry, error) {
	if e, ok := l.sizes[b]; ok {
		return e, nil
	}
	if err := l.b.Charge(budget.Work, 1); err != nil {
		return sizeEntry{}, err
	}
	var c sizes
	var err error
	switch {
	case b.Replaced():
		c = replacedSizes(b)
	case b.Style.Display == style.DisplayTable:
		c, err = l.tableSizes(b)
	case hasInlineChildren(b):
		c, err = l.inlineSizes(b)
	default:
		c, err = l.blockSizes(b)
	}
	if err != nil {
		return sizeEntry{}, err
	}
	content := c
	st := b.Style
	extra := borderPadding(st, 0)
	if st.Width.Kind == style.DimPx {
		w := contentWidth(st, st.Width.Value, extra)
		c = sizes{w, w}
	}
	if st.MaxWidth.Kind == style.DimPx {
		w := contentWidth(st, st.MaxWidth.Value, extra)
		c = sizes{math.Min(c.min, w), math.Min(c.max, w)}
	}
	if st.MinWidth.Kind == style.DimPx && st.MinWidth.Value > 0 {
		w := contentWidth(st, st.MinWidth.Value, extra)
		c = sizes{math.Max(c.min, w), math.Max(c.max, w)}
	}
	edge := extra + autoMargin(st.Margin[style.Left], 0) + autoMargin(st.Margin[style.Right], 0)
	s := sizes{clampCoord(c.min + edge), clampCoord(c.max + edge)}
	if l.sizes == nil {
		l.sizes = make(map[*box.Box]sizeEntry)
	}
	e := sizeEntry{outer: s, content: content}
	l.sizes[b] = e
	return e, nil
}

// shrinkToFit is the border box width of b in avail px of room for its margin
// box: min(max(min-content, avail), max-content) (CSS 2.1 §10.3.5).
func (l *layouter) shrinkToFit(b *box.Box, avail float64) (float64, error) {
	s, err := l.intrinsic(b)
	if err != nil {
		return 0, err
	}
	st := b.Style
	margins := autoMargin(st.Margin[style.Left], 0) + autoMargin(st.Margin[style.Right], 0)
	return math.Max(0, math.Min(math.Max(s.min, avail), s.max)-margins), nil
}

// blockSizes are the contributions of the content of a block with block-level
// children: the widest of them.
func (l *layouter) blockSizes(b *box.Box) (sizes, error) {
	if b.Style.Display == style.DisplayFlex {
		return l.flexSizes(b)
	}
	var out sizes
	for _, c := range b.Children {
		s, err := l.intrinsic(c)
		if err != nil {
			return sizes{}, err
		}
		out = sizes{math.Max(out.min, s.min), math.Max(out.max, s.max)}
	}
	return out, nil
}

// flexSizes are the contributions of a flex row: the sum of those of the items
// plus the gaps; with flex-wrap the min-content is that of the widest item.
func (l *layouter) flexSizes(b *box.Box) (sizes, error) {
	var out sizes
	gap := math.Max(0, px(b.Style.ColumnGap, 0))
	for i, c := range b.Children {
		s, err := l.intrinsic(c)
		if err != nil {
			return sizes{}, err
		}
		if i > 0 {
			out.max += gap
			if !b.Style.FlexWrap {
				out.min += gap
			}
		}
		out.max += s.max
		if b.Style.FlexWrap {
			out.min = math.Max(out.min, s.min)
		} else {
			out.min += s.min
		}
	}
	return out, nil
}

// widths accumulates the contributions of inline content: the widest line, and
// the widest unbreakable word.
type widths struct {
	min, max   float64
	line, word float64
	// trail is the collapsible space at the end of line, which hangs.
	trail  float64
	nowrap bool
}

func (w *widths) endWord() {
	w.min = math.Max(w.min, w.word)
	w.word = 0
}

func (w *widths) endLine() {
	w.endWord()
	w.max = math.Max(w.max, w.line-w.trail)
	w.line, w.trail = 0, 0
}

// inlineSizes are the contributions of the inline content of b. A line of
// nowrap or pre text cannot shrink: its min-content is its max-content.
func (l *layouter) inlineSizes(b *box.Box) (sizes, error) {
	w := widths{nowrap: b.Style.WhiteSpace != style.WhiteSpaceNormal}
	if err := l.walkInline(&w, b.Children, b.Style.WhiteSpace != style.WhiteSpacePre); err != nil {
		return sizes{}, err
	}
	w.endLine()
	if w.nowrap {
		return sizes{w.max, w.max}, nil
	}
	return sizes{w.min, w.max}, nil
}

func (l *layouter) walkInline(w *widths, boxes []*box.Box, collapse bool) error {
	for _, c := range boxes {
		switch c.Kind {
		case box.KindText:
			sh, err := l.shapeBox(c, 0)
			if err != nil {
				return err
			}
			if sh != nil {
				addText(w, sh, c.Style, collapse)
			}
		case box.KindLineBreak:
			w.endLine()
		case box.KindInlineBlock, box.KindSVG, box.KindImage:
			s, err := l.intrinsic(c)
			if err != nil {
				return err
			}
			if !w.nowrap {
				w.endWord()
			}
			w.word += s.min
			w.line += s.max
			w.trail = 0
			if !w.nowrap {
				w.endWord()
			}
		case box.KindInline:
			st := c.Style
			open, closeW := inlineEdges(st, 0, !c.Continuation)
			w.word += open
			w.line += open
			w.trail = 0
			if err := l.walkInline(w, c.Children, collapse); err != nil {
				return err
			}
			w.word += closeW
			w.line += closeW
			w.trail = 0
		}
	}
	return nil
}

// addText adds the shaped text sh to w, one segment at a time.
func addText(w *widths, sh *shapedText, st *style.Computed, collapse bool) {
	if st.OverflowWrap == style.WrapAnywhere && !sh.nowrap {
		w.min = math.Max(w.min, widestCluster(sh))
	}
	start := 0
	for i, e := range sh.ends {
		end := int(e)
		a := start
		start = end
		if collapse && w.line == 0 {
			for a < end && sh.text[a] == ' ' {
				a++
			}
		}
		if a >= end {
			continue
		}
		full := sh.width(a, end)
		trail := 0.0
		if collapse {
			if t := min(trailingSpaces(sh.text, a, end), end-a); t > 0 {
				trail = sh.width(end-t, end)
			}
		}
		w.line += full
		w.trail = trail
		if st.OverflowWrap != style.WrapAnywhere || sh.nowrap {
			w.word += full - trail
		}
		last := i == len(sh.ends)-1
		if !last || sh.lastBreak {
			w.endWord()
		}
	}
}

// widestCluster returns the advance of the widest cluster of sh: the min-content
// of text that may break anywhere.
func widestCluster(sh *shapedText) float64 {
	var widest, cur float64
	var cluster int32 = -1
	n := sh.first[len(sh.runs)]
	for g := range n {
		gl := sh.glyph(g)
		if gl.Cluster != cluster {
			cluster, cur = gl.Cluster, 0
		}
		cur += float64(gl.Advance) + sh.ls
		widest = math.Max(widest, cur)
	}
	return widest
}
