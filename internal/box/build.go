package box

import (
	"errors"
	"strings"

	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/svg"
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
	// Pseudo returns the style of the ::before or ::after box of element e, whose
	// own style is elem, or nil when the element generates none.
	Pseudo(d *dom.Document, e int32, pe css.PseudoElement, elem *style.Computed) (*style.Computed, error)
}

// Build builds the box tree of d, rooted at its first element. Every box counts
// against budget.Boxes; each node visited costs one unit of Work. The result
// holds no pointer into d.
//
// Elements with display none, and their subtrees, produce no box. An svg element
// of an HTML document is one KindSVG box (its children are parsed, not walked);
// other foreign elements are skipped with an unsupported-element warning.
// ::before and ::after boxes come from Styler.Pseudo, with their generated text
// as a text child; counters are evaluated in document order. Table display
// values are built as ordinary blocks: Style.Display keeps the real value for
// M1-16. An href "#id" is kept only when a box has that id, which gets Anchor.
func Build(d *dom.Document, st Styler, b *budget.Budget) (*Box, error) {
	if len(d.Elements) == 0 {
		return nil, ErrNoRoot
	}
	bd := &builder{d: d, st: st, b: b}
	cs, err := st.Element(d, 0, nil)
	if err != nil {
		return nil, err
	}
	bd.enterScope()
	if err := bd.applyCounters(cs); err != nil {
		return nil, err
	}
	root, err := bd.block(0, KindBlock, cs, "")
	if err != nil {
		return nil, err
	}
	bd.leaveScope()
	return root, bd.resolveLinks(root)
}

type builder struct {
	d        *dom.Document
	st       Styler
	b        *budget.Budget
	counters counterSet
	svgs     *svg.Parser
	// generated counts the bytes of generated text against MaxGeneratedBytes.
	generated  int
	generating bool
	// targets are the ids that internal links point to.
	targets map[string]struct{}
}

// frame is an inline element being built; box is its current fragment.
type frame struct {
	box   *Box
	tag   atom.Atom
	style *style.Computed
	href  string
	// pending is set when a block-level child ended the fragment in box: the
	// continuation is only created, and charged, when content follows.
	pending bool
}

type item struct {
	block *Box
	run   []*Box
}

// flow is the state of the block container being filled: its finished blocks
// and runs of inline-level boxes, the inline elements open at this point, and
// the white space state of the current run.
type flow struct {
	bd        *builder
	container *Box
	style     *style.Computed
	items     []item
	run       []*Box
	open      []frame
	hasBlock  bool
	// pendingN counts the open frames with a pending continuation.
	pendingN int
	ws       wsState
	// trailing is the text box that ends with a collapsible space the end of the
	// line will remove.
	trailing *Box
	anon     *style.Computed
	// oof are the absolute boxes met in inline content.
	oof []*Box
}

func (bd *builder) newFlow(container *Box, cs *style.Computed) *flow {
	return &flow{bd: bd, container: container, style: cs, ws: wsState{space: true}}
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
	f := bd.newFlow(bx, cs)
	bd.enterScope()
	if err := bd.pseudo(f, e, css.PseudoBefore, cs, href); err != nil {
		return nil, err
	}
	if err := bd.walk(f, el.FirstChild, cs, href); err != nil {
		return nil, err
	}
	if err := bd.pseudo(f, e, css.PseudoAfter, cs, href); err != nil {
		return nil, err
	}
	bd.leaveScope()
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
	isSVG := el.NS == dom.NSSVG && el.Tag == atom.Svg
	if el.NS != dom.NSHTML && !isSVG {
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
	if err := bd.applyCounters(cs); err != nil {
		return err
	}
	href = bd.linkOf(el, href)
	switch {
	case isSVG:
		return bd.svgBox(f, e, cs, href)
	case el.Tag == atom.Br:
		return bd.lineBreak(f, &Box{Kind: KindLineBreak, Tag: el.Tag, Style: cs, ID: el.ID, Href: href})
	case cs.Display == style.DisplayInline:
		return bd.inline(f, e, el, cs, href)
	case cs.Display == style.DisplayInlineBlock:
		return bd.inlineBlock(f, e, cs, href)
	}
	bx, err := bd.block(e, KindBlock, cs, href)
	if err != nil {
		return err
	}
	if cs.Position == style.PositionAbsolute && ((len(f.run) > 0 || len(f.open) > 0) && inlineByDefault(el.Tag) || isFlexOrGrid(f.style.Display)) {
		// Out of flow in the middle of inline content, where an element that is
		// inline by default stays on its line (a block one starts a new line), or
		// in a flex or grid container (it is not an item).
		f.oof = append(f.oof, bx)
		return nil
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
	if id, ok := fragmentID(v); ok {
		if bd.targets == nil {
			bd.targets = make(map[string]struct{})
		}
		bd.targets[id] = struct{}{}
		return "#" + id
	}
	href, why := cleanHref(v)
	if why != "" {
		bd.b.Warn(budget.WarnBlockedLink, why)
	}
	return href
}

func (bd *builder) inline(f *flow, e int32, el *dom.Element, cs *style.Computed, href string) error {
	ib, err := bd.elementBox(KindInline, el, cs, href)
	if err != nil {
		return err
	}
	if err := f.add(ib); err != nil {
		return err
	}
	f.open = append(f.open, frame{box: ib, tag: el.Tag, style: cs, href: href})
	bd.enterScope()
	if err := bd.pseudo(f, e, css.PseudoBefore, cs, href); err != nil {
		return err
	}
	if err := bd.walk(f, el.FirstChild, cs, href); err != nil {
		return err
	}
	if err := bd.pseudo(f, e, css.PseudoAfter, cs, href); err != nil {
		return err
	}
	bd.leaveScope()
	f.popFrame()
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
	if err := f.add(bx); err != nil {
		return err
	}
	f.ws = wsState{}
	return nil
}

// popFrame closes the innermost open inline box.
func (f *flow) popFrame() {
	n := len(f.open) - 1
	if f.open[n].pending {
		f.pendingN--
	}
	f.open = f.open[:n]
}

// add puts an inline-level box in the innermost open inline box, or in the
// current run, after opening the continuations that a block-level child left
// pending.
func (f *flow) add(b *Box) error {
	for i := 0; f.pendingN > 0 && i < len(f.open); i++ {
		fr := &f.open[i]
		if !fr.pending {
			continue
		}
		if err := f.bd.chargeBox(); err != nil {
			return err
		}
		cont := &Box{Kind: KindInline, Tag: fr.tag, Style: fr.style, Href: fr.href, Continuation: true, fragment: true}
		if i == 0 {
			f.run = append(f.run, cont)
		} else {
			parent := f.open[i-1].box
			parent.Children = append(parent.Children, cont)
		}
		fr.box, fr.pending = cont, false
		f.pendingN--
	}
	if n := len(f.open); n > 0 {
		top := f.open[n-1].box
		top.Children = append(top.Children, b)
		return nil
	}
	f.run = append(f.run, b)
	return nil
}

func (bd *builder) lineBreak(f *flow, br *Box) error {
	if err := bd.chargeBox(); err != nil {
		return err
	}
	f.trimTrailing()
	if err := f.add(br); err != nil {
		return err
	}
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
	f.pendingN = 0
	for i := range f.open {
		f.open[i].pending = true
		f.pendingN++
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
	c.OutOfFlow = f.oof
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
	tt := transformText(s, t.style.TextTransform)
	if t.bd.generating {
		if tt = t.bd.takeGenerated(tt); tt == "" {
			return
		}
	}
	tb := &Box{Kind: KindText, Style: t.style, Href: t.href, Text: tt}
	if t.err = t.f.add(tb); t.err != nil {
		return
	}
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

// pseudo adds the ::before or ::after box of element e, whose style is elem,
// to the flow: an inline box, an inline-block or a block by its display, with
// the generated text as its only child. Its own counter properties apply
// first, as the box is a child of the element.
func (bd *builder) pseudo(f *flow, e int32, pe css.PseudoElement, elem *style.Computed, href string) error {
	pcs, err := bd.st.Pseudo(bd.d, e, pe, elem)
	if err != nil || pcs == nil {
		return err
	}
	if err := bd.applyCounters(pcs); err != nil {
		return err
	}
	txt, err := bd.generatedText(pcs)
	if err != nil {
		return err
	}
	bd.generating = true
	defer func() { bd.generating = false }()
	if err := bd.chargeBox(); err != nil {
		return err
	}
	switch pcs.Display {
	case style.DisplayInline:
		ib := &Box{Kind: KindInline, Style: pcs, Href: href}
		if err := f.add(ib); err != nil {
			return err
		}
		f.open = append(f.open, frame{box: ib, style: pcs, href: href})
		if err := bd.text(f, txt, pcs, href); err != nil {
			return err
		}
		f.popFrame()
		return nil
	case style.DisplayInlineBlock:
		bx, err := bd.generatedBlock(KindInlineBlock, pcs, txt, href)
		if err != nil {
			return err
		}
		f.trailing = nil
		if err := f.add(bx); err != nil {
			return err
		}
		f.ws = wsState{}
		return nil
	}
	bx, err := bd.generatedBlock(KindBlock, pcs, txt, href)
	if err != nil {
		return err
	}
	return bd.addBlock(f, bx)
}

// generatedBlock builds the box of a pseudo-element that is a block container.
func (bd *builder) generatedBlock(kind Kind, cs *style.Computed, txt, href string) (*Box, error) {
	bx := &Box{Kind: kind, Style: cs, Href: href}
	f := bd.newFlow(bx, cs)
	if err := bd.text(f, txt, cs, href); err != nil {
		return nil, err
	}
	if err := bd.finish(f); err != nil {
		return nil, err
	}
	if cs.Display == style.DisplayTable {
		return bx, bd.fixTable(bx)
	}
	if !isTableContainer(cs.Display) {
		return bx, bd.wrapOrphans(bx)
	}
	return bx, nil
}

// svgParser returns the parser shared by every svg element of the build, which
// share the svg.MaxSegments bound.
func (bd *builder) svgParser() *svg.Parser {
	if bd.svgs == nil {
		bd.svgs = svg.NewParser(bd.d, bd.b)
	}
	return bd.svgs
}

// svgBox adds the replaced box of the svg element e: inline-level, or a block
// when its display says so.
func (bd *builder) svgBox(f *flow, e int32, cs *style.Computed, href string) error {
	el := &bd.d.Elements[e]
	bx, err := bd.elementBox(KindSVG, el, cs, href)
	if err != nil {
		return err
	}
	if bx.SVG, err = bd.svgParser().Parse(e); err != nil {
		return err
	}
	if cs.Display == style.DisplayInline || cs.Display == style.DisplayInlineBlock {
		f.trailing = nil
		if err := f.add(bx); err != nil {
			return err
		}
		f.ws = wsState{}
		return nil
	}
	return bd.addBlock(f, bx)
}

// inlineByDefault reports whether an element is display: inline in the user agent
// style sheet. The computed display of an absolute box is blockified and no longer
// tells.
func inlineByDefault(t atom.Atom) bool {
	switch t {
	case atom.A, atom.Abbr, atom.B, atom.Bdi, atom.Bdo, atom.Cite, atom.Code, atom.Data, atom.Dfn, atom.Em, atom.I, atom.Kbd,
		atom.Mark, atom.Q, atom.S, atom.Samp, atom.Small, atom.Span, atom.Strong, atom.Sub, atom.Sup, atom.Time, atom.U, atom.Var, atom.Img, atom.Svg:
		return true
	}
	return false
}
