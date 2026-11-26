package layout

import (
	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// pageNameAt returns the used page name (CSS Page 3 §3) of the page that starts
// at the break token tok: that of the first box placed on it, the nearest
// ancestor-or-self whose page is not auto. A nil tok is the first page. The
// start and end page names of a box (§3.3), which propagate from its first and
// last descendants, are not implemented: the name of a box is its own or that of
// its parent.
func pageNameAt(root *box.Box, tok []level) string {
	name := ""
	b := root
	for k := 0; ; k++ {
		if b.Style.Page != "" {
			name = b.Style.Page
		}
		next := firstPlaced(b, tok, k)
		if next == nil {
			return name
		}
		b = next
	}
}

// firstPlaced is the box that follows b on the path of the first box of the page:
// the child the level k of tok points to, or the first child when the token is
// exhausted. It is nil where the path ends: the box places its own content
// (lines, table rows, flex or grid lines), or it has no block children.
func firstPlaced(b *box.Box, tok []level, k int) *box.Box {
	st := b.Style
	if st.Display == style.DisplayTable || st.Display == style.DisplayFlex || st.Display == style.DisplayGrid ||
		len(b.Children) == 0 || !blockLevel(b.Children[0]) {
		return nil
	}
	i := 0
	if k < len(tok) {
		if tok[k].childrenDone {
			return nil
		}
		i = tok[k].child
	}
	if i < 0 || i >= len(b.Children) {
		return nil
	}
	return b.Children[i]
}

// usedPageName is the page name of box b among its siblings: its own, else that
// of the block being laid out.
func (l *layouter) usedPageName(b *box.Box) string {
	if b.Style.Page != "" {
		return b.Style.Page
	}
	return l.pageName
}

// pageBreakBetween reports whether the page ends between the siblings prev and
// next: a forced break, or boxes with different used page names (CSS Page 3
// §4.3).
func (l *layouter) pageBreakBetween(prev, next *box.Box) bool {
	return forcedBetween(prev, next) || l.usedPageName(prev) != l.usedPageName(next)
}

// setPageArea sets the size of the page area the next page is laid out in. What
// depends on the width (the paragraph in progress, the tables and the flex and
// grid stripes cut over pages) is forgotten when it changes.
//
// Cut tables and stripes are tied to the width they were laid out at and the
// break token of a page names the same box on the next one, so a width change
// can only happen between two boxes: nothing in progress is lost.
func (l *layouter) setPageArea(w, h float64) {
	if w != l.width {
		l.dropParagraph()
		clear(l.stripes)
		l.takeCutTables()
	}
	l.width, l.bottom, l.pageH = w, h, h
}
