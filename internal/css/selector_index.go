package css

import (
	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// KeyKind is the kind of the index key of a selector.
type KeyKind uint8

// Key kinds, from the most to the least selective.
const (
	KeyUniversal KeyKind = iota
	KeyID
	KeyClass
	KeyTag
)

// NeverMatches reports whether the selector holds an interaction
// pseudo-class (:hover...), which never matches.
func (s *Selector) NeverMatches() bool {
	for i := range s.compounds {
		for _, p := range s.compounds[i].pseudos {
			if p.kind == pseudoNever {
				return true
			}
		}
	}
	return false
}

// Key returns the key under which the rule index files the selector: the id,
// else the first class, else the type name of its rightmost compound, else
// KeyUniversal with an empty string.
func (s *Selector) Key() (KeyKind, string) {
	kind, a, name := s.key()
	if kind == KeyTag && a != 0 {
		return kind, a.String()
	}
	return kind, name
}

func (s *Selector) key() (KeyKind, atom.Atom, string) {
	c := &s.compounds[len(s.compounds)-1]
	switch {
	case c.id != "":
		return KeyID, 0, c.id
	case len(c.classes) > 0:
		return KeyClass, 0, c.classes[0]
	case c.hasTag:
		return KeyTag, c.tag, c.name
	}
	return KeyUniversal, 0, ""
}

// Entry is a selector with the value the caller associates to it.
type Entry[T any] struct {
	Selector *Selector
	Value    T
}

// Index files selectors by the key of their rightmost compound, so that an
// element is only tested against the selectors that can match it. It is built
// once and only read afterwards: concurrent Candidates calls are safe.
type Index[T any] struct {
	byID      map[string][]Entry[T]
	byClass   map[string][]Entry[T]
	byTag     map[string][]Entry[T]
	universal []Entry[T]
	n         int
}

// Add files sel. A selector that never matches is not kept.
func (ix *Index[T]) Add(sel *Selector, v T) {
	if sel.NeverMatches() {
		return
	}
	ix.n++
	e := Entry[T]{Selector: sel, Value: v}
	kind, _, name := sel.key()
	switch kind {
	case KeyID:
		ix.byID = appendKey(ix.byID, name, e)
	case KeyClass:
		ix.byClass = appendKey(ix.byClass, name, e)
	case KeyTag:
		ix.byTag = appendKey(ix.byTag, name, e)
	default:
		ix.universal = append(ix.universal, e)
	}
}

func appendKey[K comparable, T any](m map[K][]Entry[T], k K, e Entry[T]) map[K][]Entry[T] {
	if m == nil {
		m = make(map[K][]Entry[T])
	}
	m[k] = append(m[k], e)
	return m
}

// Len returns the number of selectors in the index.
func (ix *Index[T]) Len() int { return ix.n }

// appendBatch is the number of entries one Work unit pays for when they are
// appended to the candidates (about 8 entries, 128 bytes, per 20 ns).
const appendBatch = 8

// Candidates appends to dst the entries whose key matches element e of d:
// those are the only selectors that can match e. They still have to be
// matched, and Match charges for it; their order is not specified. Every class
// looked up is charged workStep and the entries appended one unit per
// appendBatch, both before the append. On a budget error dst holds what was
// appended before it.
func (ix *Index[T]) Candidates(d *dom.Document, e int32, dst []Entry[T], b *budget.Budget) ([]Entry[T], error) {
	el := &d.Elements[e]
	add := func(list []Entry[T]) error {
		if err := b.Charge(budget.Work, workStep+int64(len(list)+appendBatch-1)/appendBatch); err != nil {
			return err
		}
		dst = append(dst, list...)
		return nil
	}
	if el.ID != "" {
		if err := add(ix.byID[el.ID]); err != nil {
			return dst, err
		}
	}
	for _, c := range el.Classes {
		if err := add(ix.byClass[c]); err != nil {
			return dst, err
		}
	}
	tag := el.Name
	if el.Tag != 0 {
		tag = el.Tag.String()
	}
	if err := add(ix.byTag[tag]); err != nil {
		return dst, err
	}
	return dst, add(ix.universal)
}
