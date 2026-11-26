package layout

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// textClass tells how the text of a box is checked.
type textClass uint8

const (
	// classFlow is in-flow text: it must be drawn on a page.
	classFlow textClass = iota
	// classPositioned is the text of an absolute or fixed box, or of a rotated
	// one: it must be emitted, wherever the author put it.
	classPositioned
	// classHidden is under overflow: hidden: what does not fit is dropped.
	classHidden
	// classNone is text that is never drawn (font size 0).
	classNone
)

func classOf(st *style.Computed, parent textClass, rotatable bool) textClass {
	switch {
	case parent == classNone || st.FontSize == 0:
		return classNone
	case parent == classHidden || st.OverflowHidden:
		return classHidden
	case parent == classPositioned || st.Position == style.PositionAbsolute || st.Position == style.PositionFixed || rotatable && st.Rotate != 0:
		return classPositioned
	}
	return classFlow
}

func countRunes(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// treeText is what the box tree says must be drawn: the letters and digits of
// each text box that is checked, and the boxes that clip with overflow: hidden.
type treeText struct {
	need   map[*box.Box]int
	hidden []*box.Box
}

func (t *treeText) walk(b *box.Box, parent textClass) {
	class := parent
	if b.Style != nil {
		// Only a box with a fragment of its own is rotated, not an inline box.
		class = classOf(b.Style, parent, b.Kind == box.KindBlock || b.Kind == box.KindInlineBlock || b.Replaced())
		if b.Style.OverflowHidden && parent != classNone && b.Kind != box.KindText {
			t.hidden = append(t.hidden, b)
		}
	}
	if b.Kind == box.KindText && (class == classFlow || class == classPositioned) {
		if n := countRunes(b.Text); n > 0 {
			t.need[b] = n
		}
	}
	for _, c := range b.Children {
		t.walk(c, class)
	}
	for _, c := range b.OutOfFlow {
		t.walk(c, class)
	}
}

// firstText returns the first text box with letters under b that is drawn, in
// tree order; parent is the class of b's parent.
func firstText(b *box.Box, parent textClass) *box.Box {
	class := parent
	if b.Style != nil {
		class = classOf(b.Style, parent, b.Kind == box.KindBlock || b.Kind == box.KindInlineBlock || b.Replaced())
	}
	if class == classNone {
		return nil
	}
	if b.Kind == box.KindText && countRunes(b.Text) > 0 {
		return b
	}
	for _, c := range b.Children {
		if t := firstText(c, class); t != nil {
			return t
		}
	}
	for _, c := range b.OutOfFlow {
		if t := firstText(c, class); t != nil {
			return t
		}
	}
	return nil
}

// pageText is what the pages draw: per text box, the letters and digits of the
// runs that count, and the boxes whose clip has an area on some page.
type pageText struct {
	got     map[*box.Box]int
	clipped map[*box.Box]bool
}

// descentFraction is the part of the font size that a glyph reaches below its
// baseline, for the test of a run above the page.
const descentFraction = 0.3

// maxReported bounds the losses a TextLost report lists.
const maxReported = 10

// tallLine reports a line box taller than the page that starts on it: the
// author put the text in it with line-height, and the text is clipped by the
// page as in Chrome (design ruling: author lengths that place text outside
// the page are documented, not fixed).
func tallLine(ln *lineFrag, height float64) bool {
	return ln.rect.H > height && ln.rect.Y >= 0 && ln.rect.Y <= height
}

// visible reports whether the glyphs of t reach the page area: its baseline is
// below the top and its top above the bottom (a font as tall as the page covers
// it). A line box taller than the page counts too: the author put the text in it
// with line-height.
func visible(t *display.Text, height float64) bool {
	return t.Baseline+descentFraction*t.Size >= 0 && t.Baseline-t.Size <= height
}

// walk records the text drawn by the fragment f. Flow text counts when its line
// is on the page (before any relative offset, which the author chose); positioned
// text counts wherever it is. The runs of a text box add up, whatever the page
// that repeats them.
func (p *pageText) walk(f *frag, parent textClass, height float64) {
	class := parent
	if f.box != nil && f.box.Style != nil {
		class = classOf(f.box.Style, parent, true)
		if f.box.Style.OverflowHidden {
			if r := paddingBox(f); r.W > 0 && r.H > 0 {
				p.clipped[f.box] = true
			}
		}
	}
	if f.rot != 0 && class == classFlow {
		class = classPositioned
	}
	for _, ln := range f.lines {
		for i := range ln.items {
			it := &ln.items[i]
			if t := it.text; t != nil && it.box != nil {
				if class != classFlow || tallLine(ln, height) || visible(t, height) {
					p.got[it.box] += countRunes(t.Text)
				}
			}
			if it.atom != nil {
				p.walk(it.atom.f, class, height)
			}
		}
	}
	for _, c := range f.children {
		p.walk(c, class, height)
	}
	for _, l := range f.abs {
		p.walk(l.f, classPositioned, height)
	}
}

// TextLost reports, sorted and capped, the text boxes of the tree that no page
// draws; "" when nothing is lost. It compares box by box, so copies of a repeated
// header or a fixed box cannot stand for text drawn nowhere. In-flow text must
// reach the page area, positioned and rotated text must be emitted wherever it
// is, the first text of each clip with an area must be drawn, and font size 0 is
// not checked. For tests and fuzzing: pages hold every page of the render, laid
// out with KeepTree.
func TextLost(root *box.Box, pages []*Page) string {
	tree := &treeText{need: map[*box.Box]int{}}
	tree.walk(root, classFlow)
	drawn := &pageText{got: map[*box.Box]int{}, clipped: map[*box.Box]bool{}}
	for _, pg := range pages {
		if pg.root != nil {
			drawn.walk(pg.root, classFlow, pg.Area.H/pg.Scale)
		}
	}
	var lost []string
	for b, n := range tree.need {
		if drawn.got[b] < n {
			lost = append(lost, fmt.Sprintf("%q: %d of %d", b.Text, drawn.got[b], n))
		}
	}
	for _, b := range tree.hidden {
		if t := firstText(b, classFlow); t != nil && drawn.clipped[b] && drawn.got[t] == 0 {
			lost = append(lost, fmt.Sprintf("first text of a clip: %q", t.Text))
		}
	}
	sort.Strings(lost)
	if len(lost) > maxReported {
		lost = append(lost[:maxReported], fmt.Sprintf("and %d more", len(lost)-maxReported))
	}
	return strings.Join(lost, ", ")
}
