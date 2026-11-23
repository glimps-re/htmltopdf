// Package dom turns untrusted HTML into a compact, sanitized document tree.
//
// Parse bounds the input, estimates the size of the tree the HTML parser would
// build before building it, and converts the result into flat slices without
// pointers to the parser's nodes.
package dom

import (
	"strings"

	"golang.org/x/net/html/atom"
)

// NodeRef designates a child node: an element or a text node.
//
// A value >= 0 is an index into Document.Elements, a value <= -2 designates
// Document.Texts[-ref-2], and NoNode (-1) means no node.
type NodeRef int32

// NoNode is the NodeRef of a missing node.
const NoNode NodeRef = -1

// ElementRef returns the NodeRef of Document.Elements[i].
func ElementRef(i int32) NodeRef { return NodeRef(i) }

// TextRef returns the NodeRef of Document.Texts[i].
func TextRef(i int32) NodeRef { return NodeRef(-i - 2) }

// Element returns the index into Document.Elements and true when r designates
// an element.
func (r NodeRef) Element() (int32, bool) { return int32(r), r >= 0 }

// Text returns the index into Document.Texts and true when r designates a
// text node.
func (r NodeRef) Text() (int32, bool) { return int32(-r - 2), r <= -2 }

// Namespace is the namespace of an element.
type Namespace uint8

// Namespaces. Foreign (SVG and MathML) subtrees are kept in the Document; later
// stages decide what to do with them.
const (
	NSHTML Namespace = iota
	NSSVG
	NSMathML
)

// Attr is an attribute other than id, class and style. The key of an attribute
// in a namespace keeps its prefix ("xlink:href").
type Attr struct {
	Key, Val string
}

// Element is an element node. Elements are stored in document order; the
// root (html) is Elements[0] and has Parent -1.
type Element struct {
	// Tag is the atom of the tag name, 0 for names unknown to the atom table.
	Tag atom.Atom
	// NS is the namespace; NSHTML for HTML elements.
	NS Namespace
	// Name is the tag name as the parser reports it (lowercase for HTML, adjusted
	// for SVG: "foreignObject"), set only when Tag is 0.
	Name string
	// ID, Classes and Style hold the first id, class and style attributes.
	// An empty value is indistinguishable from an absent attribute.
	ID      string
	Classes []string
	Style   string
	// Attrs holds the other attributes in source order, duplicates included.
	Attrs []Attr
	// Parent is the index of the parent element, -1 for the root.
	Parent int32
	// FirstChild and NextSibling link the element and text children of Parent.
	FirstChild, NextSibling NodeRef
	// PrevElement is the previous element sibling, -1 for none.
	PrevElement int32
	// SiblingIndex is the 0-based position among the element siblings and
	// SiblingCount their number, itself included.
	SiblingIndex, SiblingCount int32
}

// Text is a text node. Its Data is sanitized and never empty.
type Text struct {
	Data        string
	Parent      int32
	NextSibling NodeRef
}

// Document is a parsed HTML document. Comments and doctypes are not stored.
type Document struct {
	Elements []Element
	Texts    []Text
	// Title is the whitespace-collapsed text of the first HTML title element.
	Title string
}

// Attr returns the value of the first attribute named name (lowercase). The
// class attribute is returned with its tokens joined by single spaces.
func (e *Element) Attr(name string) (string, bool) {
	switch name {
	case "id":
		return e.ID, e.ID != ""
	case "class":
		return strings.Join(e.Classes, " "), len(e.Classes) > 0
	case "style":
		return e.Style, e.Style != ""
	}
	for i := range e.Attrs {
		if e.Attrs[i].Key == name {
			return e.Attrs[i].Val, true
		}
	}
	return "", false
}

// NextSibling returns the next sibling of the node r.
func (d *Document) NextSibling(r NodeRef) NodeRef {
	if i, ok := r.Element(); ok {
		return d.Elements[i].NextSibling
	}
	if i, ok := r.Text(); ok {
		return d.Texts[i].NextSibling
	}
	return NoNode
}
