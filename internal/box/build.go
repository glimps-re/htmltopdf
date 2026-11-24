package box

import (
	"errors"
	"strings"

	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// tabSize is the width of a tab in a preserved white space, in characters.
const tabSize = 8

// ErrNoRoot is returned for a document without elements.
var ErrNoRoot = errors.New("box: document without elements")

// Styler computes the styles Build needs: *style.Resolver implements it.
type Styler interface {
	// Element returns the computed style of element e; parent is the style of
	// its parent element, nil for the root.
	Element(d *dom.Document, e int32, parent *style.Computed) (*style.Computed, error)
	// Anonymous returns the style of an anonymous box of the given display
	// under parent: inherited properties from parent, the others initial.
	Anonymous(parent *style.Computed, display style.Display) (*style.Computed, error)
}

// Build builds the box tree of d, rooted at its first element. Every box counts
// against budget.Boxes; each node visited costs one unit of Work. The result
// holds no pointer into d.
//
// Elements with display none, and their subtrees, produce no box. Elements of
// the SVG and MathML namespaces are skipped with an unsupported-element
// warning until M3-05. Table display values are built as ordinary blocks:
// Style.Display keeps the real value for M1-16.
func Build(d *dom.Document, st Styler, b *budget.Budget) (*Box, error) {
	if len(d.Elements) == 0 {
		return nil, ErrNoRoot
	}
	bd := &builder{d: d, st: st, b: b}
	cs, err := st.Element(d, 0, nil)
	if err != nil {
		return nil, err
	}
	return bd.block(0, KindBlock, cs, "")
}

type builder struct {
	d  *dom.Document
	st Styler
	b  *budget.Budget
}

// frame is an inline element being built; box is its current fragment.
type frame struct {
	box   *Box
	tag   atom.Atom
	style *style.Computed
	href  string
}

type item struct {
	block *Box
	run   []*Box
}

// flow is the state of the block container being filled: its finished blocks
// and runs of inline-level boxes, the inline elements open at this point, and
// the white space state of the current run.
type flow struct {
	container *Box
	style     *style.Computed
	items     []item
	run       []*Box
	open      []frame
	hasBlock  bool
	ws        wsState
	// trailing is the text box that ends with a collapsible space the end of the
	// line will remove.
	trailing *Box
	anon     *style.Computed
}

func newFlow(container *Box, cs *style.Computed) *flow {
	return &flow{container: container, style: cs, ws: wsState{space: true}}
}

// chargeBox counts a box against MaxBoxes.
func (bd *builder) chargeBox() error { return bd.b.Charge(budget.Boxes, 1) }

// elementBox returns the box of an element, charged to MaxBoxes.
func (bd *builder) elementBox(kind Kind, el *dom.Element, cs *style.Computed, href string) (*Box, error) {
	if err := bd.chargeBox(); err != nil {
		return nil, err
	}
	return &Box{Kind: kind, Tag: el.Tag, Style: cs, ID: el.ID, Href: href}, nil
}

// block builds the box of kind KindBlock or KindInlineBlock of element e and
// its children. Only the root can have display none here (element filters the
// others): it gives an empty box. A root whose display is inline is built as a
// block with its inline style.
func (bd *builder) block(e int32, kind Kind, cs *style.Computed, href string) (*Box, error) {
	el := &bd.d.Elements[e]
	bx, err := bd.elementBox(kind, el, cs, href)
	if err != nil || cs.Display == style.DisplayNone {
		return bx, err
	}
	f := newFlow(bx, cs)
	if err := bd.walk(f, el.FirstChild, cs, href); err != nil {
		return nil, err
	}
	if err := bd.finish(f); err != nil {
		return nil, err
	}
	switch cs.Display {
	case style.DisplayTable:
		return bx, bd.fixTable(bx)
	case style.DisplayTableCell:
		bd.cellSpans(el, bx)
	case style.DisplayTableColumn, style.DisplayTableColumnGroup:
		bd.spanAttr(el, bx, "span")
	}
	if !isTableContainer(cs.Display) {
		return bx, bd.wrapOrphans(bx)
	}
	return bx, nil
}

// walk adds the nodes of a sibling list, children of an element whose style is
// parent, to the flow.
func (bd *builder) walk(f *flow, first dom.NodeRef, parent *style.Computed, href string) error {
	for r := first; r != dom.NoNode; r = bd.d.NextSibling(r) {
		if err := bd.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		if ti, ok := r.Text(); ok {
			if err := bd.text(f, bd.d.Texts[ti].Data, parent, href); err != nil {
				return err
			}
			continue
		}
		e, _ := r.Element()
		if err := bd.element(f, e, parent, href); err != nil {
			return err
		}
	}
	return nil
}

func (bd *builder) element(f *flow, e int32, parent *style.Computed, href string) error {
	el := &bd.d.Elements[e]
	if el.NS != dom.NSHTML {
		bd.b.Warn(budget.WarnUnsupportedElement, foreignName(el.NS))
		return nil
	}
	cs, err := bd.st.Element(bd.d, e, parent)
	if err != nil {
		return err
	}
	if cs.Display == style.DisplayNone {
		return nil
	}
	href = bd.linkOf(el, href)
	switch {
	case el.Tag == atom.Br:
		return bd.lineBreak(f, &Box{Kind: KindLineBreak, Tag: el.Tag, Style: cs, ID: el.ID, Href: href})
	case cs.Display == style.DisplayInline:
		return bd.inline(f, el, cs, href)
	case cs.Display == style.DisplayInlineBlock:
		return bd.inlineBlock(f, e, cs, href)
	}
	bx, err := bd.block(e, KindBlock, cs, href)
	if err != nil {
		return err
	}
	return bd.addBlock(f, bx)
}

func foreignName(ns dom.Namespace) string {
	switch ns {
	case dom.NSSVG:
		return "svg"
	case dom.NSMathML:
		return "math"
	}
	return "foreign"
}

// linkOf returns the link target in effect inside el: the sanitized href of an
// a element (empty when it is blocked, a fragment or absent from a link), the
// enclosing one otherwise.
func (bd *builder) linkOf(el *dom.Element, outer string) string {
	if el.Tag != atom.A {
		return outer
	}
	v, ok := el.Attr("href")
	if !ok {
		return outer
	}
	href, why := cleanHref(v)
	if why != "" {
		bd.b.Warn(budget.WarnBlockedLink, why)
	}
	return href
}

func (bd *builder) inline(f *flow, el *dom.Element, cs *style.Computed, href string) error {
	ib, err := bd.elementBox(KindInline, el, cs, href)
	if err != nil {
		return err
	}
	f.add(ib)
	f.open = append(f.open, frame{box: ib, tag: el.Tag, style: cs, href: href})
	if err := bd.walk(f, el.FirstChild, cs, href); err != nil {
		return err
	}
	f.open = f.open[:len(f.open)-1]
	return nil
}

// inlineBlock adds the atomic inline of element e. It is content for the white
// space of the line: a space before it is kept, one after it is not collapsed
// into it.
func (bd *builder) inlineBlock(f *flow, e int32, cs *style.Computed, href string) error {
	bx, err := bd.block(e, KindInlineBlock, cs, href)
	if err != nil {
		return err
	}
	f.trailing = nil
	f.add(bx)
	f.ws = wsState{}
	return nil
}

// add puts an inline-level box in the innermost open inline box, or in the
// current run.
func (f *flow) add(b *Box) {
	if n := len(f.open); n > 0 {
		top := f.open[n-1].box
		top.Children = append(top.Children, b)
		return
	}
	f.run = append(f.run, b)
}

func (bd *builder) lineBreak(f *flow, br *Box) error {
	if err := bd.chargeBox(); err != nil {
		return err
	}
	f.trimTrailing()
	f.add(br)
	f.ws = wsState{space: true}
	return nil
}

func (f *flow) trimTrailing() {
	if f.trailing != nil {
		f.trailing.Text = strings.TrimSuffix(f.trailing.Text, " ")
		f.trailing = nil
	}
}

// addBlock adds a finished block-level child. Inline boxes open at this point
// are split around it: their fragment so far ends the current run, and empty
// continuations are opened after the block.
func (bd *builder) addBlock(f *flow, bx *Box) error {
	for i := range f.open {
		f.open[i].box.fragment = true
	}
	f.flushRun()
	f.items = append(f.items, item{block: bx})
	f.hasBlock = true
	f.ws = wsState{space: true}
	for i := range f.open {
		cont := &Box{Kind: KindInline, Tag: f.open[i].tag, Style: f.open[i].style, Href: f.open[i].href, Continuation: true, fragment: true}
		if err := bd.chargeBox(); err != nil {
			return err
		}
		if i == 0 {
			f.run = append(f.run, cont)
		} else {
			parent := f.open[i-1].box
			parent.Children = append(parent.Children, cont)
		}
		f.open[i].box = cont
	}
	return nil
}

// flushRun ends the current run of inline-level boxes.
func (f *flow) flushRun() {
	f.trimTrailing()
	f.run = prune(f.run)
	if len(f.run) > 0 {
		f.items = append(f.items, item{run: f.run})
	}
	f.run = nil
}

// prune removes the empty text boxes and the empty fragments of a split inline
// box.
func prune(boxes []*Box) []*Box {
	out := boxes[:0]
	for _, b := range boxes {
		switch b.Kind {
		case KindText:
			if b.Text == "" {
				continue
			}
		case KindInline:
			b.Children = prune(b.Children)
			if b.fragment && len(b.Children) == 0 {
				continue
			}
		}
		out = append(out, b)
	}
	clear(boxes[len(out):])
	return out
}

// finish sets the children of the container: inline-level boxes directly when
// there is no block-level child, otherwise blocks and anonymous blocks around
// the runs. A flex or grid container always gets block-level children: every
// run is wrapped, except those that are white space only, which are dropped
// (CSS Flexbox 1 §4).
func (bd *builder) finish(f *flow) error {
	f.flushRun()
	c := f.container
	wrap := f.hasBlock || isFlexOrGrid(f.style.Display)
	if !wrap {
		for _, it := range f.items {
			c.Children = append(c.Children, it.run...)
		}
		return nil
	}
	for _, it := range f.items {
		if it.block != nil {
			c.Children = append(c.Children, it.block)
			continue
		}
		if isFlexOrGrid(f.style.Display) && allBlank(it.run) {
			continue
		}
		if f.anon == nil {
			cs, err := bd.st.Anonymous(f.style, style.DisplayBlock)
			if err != nil {
				return err
			}
			f.anon = cs
		}
		if err := bd.chargeBox(); err != nil {
			return err
		}
		c.Children = append(c.Children, &Box{Kind: KindBlock, Anonymous: true, Style: f.anon, Children: it.run})
	}
	return nil
}

func isFlexOrGrid(d style.Display) bool {
	return d == style.DisplayFlex || d == style.DisplayGrid
}

// transformText applies text-transform with Go's simple case mapping, which
// keeps a ß as it is (Chrome gives SS).
func transformText(s string, t style.TextTransform) string {
	switch t {
	case style.TransformUppercase:
		return strings.ToUpper(s)
	case style.TransformLowercase:
		return strings.ToLower(s)
	}
	return s
}

// textBoxes is the textSink that turns processed text into boxes.
type textBoxes struct {
	bd       *builder
	f        *flow
	style    *style.Computed
	href     string
	collapse bool
	err      error
}

func (t *textBoxes) text(s string) {
	if t.err != nil {
		return
	}
	if t.err = t.bd.chargeBox(); t.err != nil {
		return
	}
	tb := &Box{Kind: KindText, Style: t.style, Href: t.href, Text: transformText(s, t.style.TextTransform)}
	t.f.add(tb)
	t.f.trailing = nil
	if t.collapse && strings.HasSuffix(s, " ") {
		t.f.trailing = tb
	}
}

func (t *textBoxes) lineBreak() error {
	if t.err != nil {
		return t.err
	}
	return t.bd.lineBreak(t.f, &Box{Kind: KindLineBreak, Style: t.style, Href: t.href})
}

// text adds the text of a node, its white space processed as cs.WhiteSpace
// says: nowrap collapses like normal, it only affects line breaking.
func (bd *builder) text(f *flow, data string, cs *style.Computed, href string) error {
	pre := cs.WhiteSpace == style.WhiteSpacePre
	sink := &textBoxes{bd: bd, f: f, style: cs, href: href, collapse: !pre}
	if err := processText(data, pre, &f.ws, sink); err != nil {
		return err
	}
	return sink.err
}
