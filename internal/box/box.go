// Package box builds the box tree of a document: the layout input.
//
// Build walks the DOM once, asks the style resolver for the computed style of
// each element, and produces blocks, inline boxes, text boxes and line breaks:
// anonymous blocks around runs of inline-level children (CSS 2.1 §9.2.1.1),
// inline boxes split around block-level children, collapsed white space
// (CSS Text 3 §4.1) and sanitized links (design §8.2). The tree keeps no pointer
// into the DOM, so the document can be dropped once Build returns.
package box

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/style"
)

// Kind is the kind of a Box.
type Kind uint8

// Box kinds.
const (
	// KindBlock is a block-level box; Style.Display keeps the computed display,
	// which tells list items and table parts apart (M1-16 reads it).
	KindBlock Kind = iota + 1
	// KindInline is an inline box.
	KindInline
	// KindText is a run of text without line breaks.
	KindText
	// KindLineBreak is a forced line break (br, or a preserved newline).
	KindLineBreak
)

// String returns the name of the kind, as the dump shows it.
func (k Kind) String() string {
	switch k {
	case KindBlock:
		return "block"
	case KindInline:
		return "inline"
	case KindText:
		return "text"
	case KindLineBreak:
		return "br"
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

// Box is a node of the box tree.
type Box struct {
	Kind Kind
	// Anonymous is true for a block generated around a run of inline-level
	// children. Its Style comes from Styler.Anonymous.
	Anonymous bool
	// Continuation is true for the second and later fragments of an inline box
	// that a block-level child split in several.
	Continuation bool
	// Tag is the tag atom of the element of an element box, 0 otherwise.
	Tag atom.Atom
	// Style is shared with every box of the same style: never modify it. Text
	// and line break boxes share the style of their parent element.
	Style *style.Computed
	// ID is the id of the element, set on its first box only.
	ID string
	// Href is the sanitized target of the link the box is in (design §8.2): the
	// box of an a element, and every box inside it, blocks included. The string
	// is shared, not copied. Empty when the box is not in a link or the link was
	// blocked.
	Href string
	// Text is the text of a text box: no newline, white space processed.
	Text string
	// ColSpan is the colspan of a table cell, in [1, MaxColSpan]; 0 on every
	// other box.
	ColSpan int
	// Children are the boxes inside. A block has either block children only or
	// inline-level children only.
	Children []*Box

	// fragment marks an inline box produced by a split: it is dropped if empty.
	fragment bool
}

// Dump writes an indented text form of the tree rooted at b, for tests and
// debugging: one box per line with its kind, tag and flags.
func Dump(w io.Writer, b *Box) error {
	return dump(w, b, 0)
}

func dump(w io.Writer, b *Box, depth int) error {
	var sb strings.Builder
	sb.WriteString(strings.Repeat("  ", depth))
	if b.Anonymous {
		sb.WriteString("anon-")
	}
	sb.WriteString(b.Kind.String())
	if b.Tag != 0 && b.Kind != KindLineBreak {
		sb.WriteString(" " + b.Tag.String())
	}
	if b.ID != "" {
		sb.WriteString(" #" + b.ID)
	}
	if b.Href != "" {
		sb.WriteString(" href=" + strconv.Quote(b.Href))
	}
	if b.ColSpan > 1 {
		sb.WriteString(" colspan=" + strconv.Itoa(b.ColSpan))
	}
	if b.Continuation {
		sb.WriteString(" (cont)")
	}
	if b.Kind == KindText {
		sb.WriteString(" " + strconv.Quote(b.Text))
	}
	sb.WriteByte('\n')
	if _, err := io.WriteString(w, sb.String()); err != nil {
		return fmt.Errorf("box: dump: %w", err)
	}
	for _, c := range b.Children {
		if err := dump(w, c, depth+1); err != nil {
			return err
		}
	}
	return nil
}
