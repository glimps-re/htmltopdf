package dom

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// MaxHTMLDepth is the nesting bound of x/net/html: its parser panics past 512
// open elements and Parse recovers it into a plain error, which this package
// reports as a budget.LimitError of kind HTMLDepth.
const MaxHTMLDepth = 512

// Parse reads at most the InputBytes limit from src, rejects inputs whose
// estimated tree is far beyond the Nodes limit before building them, and
// returns the compact document (Convert enforces the limit exactly). Errors
// from the budget are returned unchanged. It reports the checkpoints dom.read,
// dom.prepass, dom.parse and dom.convert.
func Parse(src io.Reader, b *budget.Budget) (*Document, error) {
	root, err := parseTree(src, b)
	if err != nil {
		return nil, err
	}
	doc, err := Convert(root, b)
	b.Checkpoint("dom.convert")
	return doc, err
}

// parseTree runs the bounded read, the pre-pass and html.Parse. The input
// buffer is garbage once it returns.
func parseTree(src io.Reader, b *budget.Budget) (*html.Node, error) {
	if err := b.Poll(); err != nil {
		return nil, err
	}
	buf, err := readBounded(src, b)
	if err != nil {
		return nil, err
	}
	b.Checkpoint("dom.read")

	err = prepass(buf, b)
	b.Checkpoint("dom.prepass")
	if err != nil {
		return nil, err
	}

	// html.Parse only fails when its stack of open elements exceeds 512 (it
	// recovers that panic into an error): the reader is a bytes.Reader, which
	// cannot fail, and TestHTMLDepth pins the assumption on every x/net upgrade.
	root, err := html.Parse(bytes.NewReader(buf))
	if err != nil {
		return nil, &budget.LimitError{Kind: budget.HTMLDepth, Max: MaxHTMLDepth}
	}
	b.Checkpoint("dom.parse")
	if err := b.Poll(); err != nil {
		return nil, err
	}
	return root, nil
}

// firstChunk is what readBounded reads before it sizes the buffer for the rest:
// bytes.Buffer would otherwise double its way up to twice the input.
const firstChunk = 64 << 10

func readBounded(src io.Reader, b *budget.Budget) ([]byte, error) {
	limit := min(b.Max(budget.InputBytes), math.MaxInt32-1) // +1 detects an excess
	var buf bytes.Buffer
	if l, ok := src.(interface{ Len() int }); ok {
		buf.Grow(min(l.Len(), int(limit)) + 1)
	}
	n, err := buf.ReadFrom(io.LimitReader(src, min(firstChunk, limit+1)))
	if err == nil && n == firstChunk && limit+1 > n {
		buf.Grow(int(limit + 1 - n))
		var m int64
		m, err = buf.ReadFrom(io.LimitReader(src, limit+1-n))
		n += m
	}
	if err != nil {
		return nil, fmt.Errorf("dom: reading input: %w", err)
	}
	if err := b.Charge(budget.InputBytes, n); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EstimateFactor is how many times the Nodes limit the pre-pass estimate may
// reach. The estimate is a loose upper bound (it charges every token the whole
// list of active formatting elements), so it only has to stop inputs whose
// worst case is hostile; Convert enforces the Nodes limit on the real count.
const EstimateFactor = 4

// prepass rejects inputs whose estimated tree exceeds EstimateFactor times what
// remains of the Nodes limit, before html.Parse builds it.
func prepass(buf []byte, b *budget.Budget) error {
	remaining := b.Max(budget.Nodes) - b.Used(budget.Nodes)
	limit := remaining
	if remaining <= math.MaxInt64/EstimateFactor {
		limit = remaining * EstimateFactor
	}
	est, err := estimateNodes(buf, limit, b.Poll)
	if err != nil {
		return err
	}
	if est > limit {
		return &budget.LimitError{Kind: budget.Nodes, Max: b.Max(budget.Nodes)}
	}
	return nil
}

// Convert builds the compact Document from a parsed tree. It charges the exact
// node count (nodes plus attributes) to the Nodes limit before allocating. The
// result holds no reference to root.
func Convert(root *html.Node, b *budget.Budget) (*Document, error) {
	c := converter{}
	nodes, elements, texts := c.count(root)
	if err := b.Charge(budget.Nodes, nodes); err != nil {
		return nil, err
	}
	// Node references are int32; Nodes is only a limit, not a type bound.
	if elements+texts > math.MaxInt32 {
		return nil, &budget.LimitError{Kind: budget.Nodes, Max: math.MaxInt32}
	}
	c.doc = &Document{
		Elements: make([]Element, 0, elements),
		Texts:    make([]Text, 0, texts),
	}
	c.children(root, -1)
	return c.doc, nil
}

// converter counts elements and texts in int32, which Convert guards against
// overflow before allocating.
type converter struct {
	doc            *Document
	nElems, nTexts int32
	titleSet       bool
}

// count returns the cost of the tree under root, root excluded: one per node
// plus one per attribute, and the numbers of elements and text nodes.
func (c *converter) count(root *html.Node) (nodes int64, elements, texts int) {
	for n := root.FirstChild; n != nil; {
		nodes += 1 + int64(len(n.Attr))
		switch n.Type {
		case html.ElementNode:
			elements++
		case html.TextNode:
			texts++
		case html.ErrorNode, html.DocumentNode, html.CommentNode, html.DoctypeNode, html.RawNode:
		}
		switch {
		case n.FirstChild != nil:
			n = n.FirstChild
		case n.NextSibling != nil:
			n = n.NextSibling
		default:
			n = nextUp(n, root)
		}
	}
	return nodes, elements, texts
}

func nextUp(n, root *html.Node) *html.Node {
	for n = n.Parent; n != nil && n != root; n = n.Parent {
		if n.NextSibling != nil {
			return n.NextSibling
		}
	}
	return nil
}

func (c *converter) children(parent *html.Node, parentIdx int32) NodeRef {
	elements := int32(0)
	first, prev := NoNode, NoNode
	for n := parent.FirstChild; n != nil; n = n.NextSibling {
		var ref NodeRef
		switch n.Type {
		case html.ElementNode:
			ref = ElementRef(c.element(n, parentIdx))
			elements++
		case html.TextNode:
			data := SanitizeText(n.Data)
			if data == "" {
				continue
			}
			ref = TextRef(c.nTexts)
			c.doc.Texts = append(c.doc.Texts, Text{Data: data, Parent: parentIdx, NextSibling: NoNode})
			c.nTexts++
		case html.ErrorNode, html.DocumentNode, html.CommentNode, html.DoctypeNode, html.RawNode:
			continue
		}
		if first == NoNode {
			first = ref
		} else {
			c.setNext(prev, ref)
		}
		prev = ref
	}
	c.numberSiblings(first, elements)
	return first
}

func (c *converter) setNext(prev, next NodeRef) {
	if i, ok := prev.Element(); ok {
		c.doc.Elements[i].NextSibling = next
	} else if i, ok := prev.Text(); ok {
		c.doc.Texts[i].NextSibling = next
	}
}

func (c *converter) numberSiblings(first NodeRef, count int32) {
	index, prevElem := int32(0), int32(-1)
	for r := first; r != NoNode; r = c.doc.NextSibling(r) {
		if i, ok := r.Element(); ok {
			e := &c.doc.Elements[i]
			e.SiblingIndex, e.SiblingCount, e.PrevElement = index, count, prevElem
			index++
			prevElem = i
		}
	}
}

func (c *converter) element(n *html.Node, parent int32) int32 {
	idx := c.nElems
	e := Element{
		Tag:         n.DataAtom,
		NS:          namespaceOf(n.Namespace),
		Parent:      parent,
		NextSibling: NoNode,
		PrevElement: -1,
	}
	if e.Tag == 0 {
		e.Name = n.Data
	}
	c.attributes(&e, n.Attr)
	c.doc.Elements = append(c.doc.Elements, e)
	c.nElems++

	first := c.children(n, idx)
	c.doc.Elements[idx].FirstChild = first

	if n.DataAtom == atom.Title && n.Namespace == "" && !c.titleSet {
		c.titleSet = true
		c.doc.Title = c.titleText(first)
	}
	return idx
}

func namespaceOf(ns string) Namespace {
	switch ns {
	case "svg":
		return NSSVG
	case "math":
		return NSMathML
	}
	return NSHTML
}

func (c *converter) attributes(e *Element, attrs []html.Attribute) {
	other := 0
	for i := range attrs {
		if !isReserved(&attrs[i]) {
			other++
		}
	}
	if other > 0 {
		e.Attrs = make([]Attr, 0, other)
	}
	var hasID, hasClass, hasStyle bool
	for i := range attrs {
		a := &attrs[i]
		switch {
		case !isReserved(a):
			key := a.Key
			if a.Namespace != "" {
				key = a.Namespace + ":" + key
			}
			e.Attrs = append(e.Attrs, Attr{Key: key, Val: a.Val})
		case a.Key == "id" && !hasID:
			hasID, e.ID = true, a.Val
		case a.Key == "class" && !hasClass:
			hasClass, e.Classes = true, splitClasses(a.Val)
		case a.Key == "style" && !hasStyle:
			hasStyle, e.Style = true, a.Val
		}
	}
}

// isReserved reports the id, class and style attributes, which have their own
// Element fields; a repeated one is dropped.
func isReserved(a *html.Attribute) bool {
	return a.Namespace == "" && (a.Key == "id" || a.Key == "class" || a.Key == "style")
}

// splitClasses splits a class attribute into its distinct tokens, in order of
// first occurrence: a class attribute is a set, and a million repeated classes
// must not become a million entries, not even transiently.
func splitClasses(s string) []string {
	var out []string
	var seen map[string]struct{}
	for i := 0; i < len(s); {
		for i < len(s) && isHTMLSpace(rune(s[i])) {
			i++
		}
		j := i
		for j < len(s) && !isHTMLSpace(rune(s[j])) {
			j++
		}
		if i == j {
			break
		}
		tok := s[i:j]
		i = j
		if seen != nil {
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
		} else if slices.Contains(out, tok) {
			continue
		}
		out = append(out, tok)
		if seen == nil && len(out) > smallClassList {
			seen = make(map[string]struct{}, 2*smallClassList)
			for _, c := range out {
				seen[c] = struct{}{}
			}
		}
	}
	return slices.Clip(out)
}

// smallClassList is the number of classes up to which a linear scan beats a
// map.
const smallClassList = 8

func isHTMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
}

func (c *converter) titleText(first NodeRef) string {
	var sb strings.Builder
	for r := first; r != NoNode; r = c.doc.NextSibling(r) {
		if i, ok := r.Text(); ok {
			sb.WriteString(c.doc.Texts[i].Data)
		}
	}
	return strings.Join(strings.FieldsFunc(sb.String(), isHTMLSpace), " ")
}
