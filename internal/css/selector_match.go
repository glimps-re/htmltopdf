package css

import (
	"slices"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// Work charged by Match. Each unit must cost at most about 20 ns (design
// §6.2), so that the default MaxWork of 100 million bounds the time of a match
// at about 2 seconds. BenchmarkWork* and BenchmarkHostileSiblings report the
// time per unit; the weights are pessimistic on purpose (a unit of a candidate
// costs well under 20 ns) because over-charging costs nothing real: matching
// the whole 00-large report uses 0.1% of MaxWork.
const (
	// workCandidate is charged by Match: one candidate of the rule index, with
	// the test of its compounds.
	workCandidate = 8
	// workCombinator is charged each time a combinator is followed, for the
	// recursion and the second compound test that come with it.
	workCombinator = 6
	// workStep is charged for each ancestor tested by a descendant combinator and
	// for each previous sibling visited by "~", for the neighbour of ">" and "+",
	// and for each class looked up in the rule index.
	workStep = 1
)

// scanUnits is the Work charged for scanning bytes bytes: one unit per 64
// bytes plus one for the call, charged before the scan.
func scanUnits(bytes int) int64 { return 1 + int64(bytes)/64 }

// Match reports whether element e of d matches the selector. When the selector
// has a pseudo-element, it is the element the pseudo-element belongs to that
// is tested.
//
// Match charges budget.Work before every step: workCandidate for the call,
// workCombinator and workStep along combinators, and an amount proportional to
// the bytes it is about to scan for classes and attributes. :first-child,
// :last-child, :nth-child and + cost O(1) thanks to the sibling index of the
// DOM; ~ visits the previous siblings. A *budget.LimitError, or the context
// error, aborts the match.
func (s *Selector) Match(d *dom.Document, e int32, b *budget.Budget) (bool, error) {
	if err := b.Charge(budget.Work, workCandidate); err != nil {
		return false, err
	}
	return s.matchFrom(d, e, len(s.compounds)-1, b)
}

// matchFrom matches compounds[0..i] with compounds[i] on element e.
func (s *Selector) matchFrom(d *dom.Document, e int32, i int, b *budget.Budget) (bool, error) {
	ok, err := s.compounds[i].matches(d, e, b)
	if !ok || err != nil {
		return false, err
	}
	return s.matchRest(d, e, i, b)
}

// matchRest matches compounds[0..i-1] along the combinator of compounds[i],
// compounds[i] having matched e.
func (s *Selector) matchRest(d *dom.Document, e int32, i int, b *budget.Budget) (bool, error) {
	if i == 0 {
		return true, nil
	}
	if err := b.Charge(budget.Work, workCombinator); err != nil {
		return false, err
	}
	el := &d.Elements[e]
	switch s.compounds[i].comb {
	case CombChild:
		return s.step(d, el.Parent, i-1, b)
	case CombNext:
		return s.step(d, el.PrevElement, i-1, b)
	case CombDescendant:
		return s.search(d, el.Parent, i-1, b, CombDescendant, func(x *dom.Element) int32 { return x.Parent })
	case CombLater:
		return s.search(d, el.PrevElement, i-1, b, CombLater, func(x *dom.Element) int32 { return x.PrevElement })
	}
	return false, nil
}

// step moves to one neighbour and matches the rest of the selector there.
func (s *Selector) step(d *dom.Document, to int32, i int, b *budget.Budget) (bool, error) {
	if to < 0 {
		return false, nil
	}
	if err := b.Charge(budget.Work, workStep); err != nil {
		return false, err
	}
	return s.matchFrom(d, to, i, b)
}

// search tries every element reached from start by next (ancestors or previous
// siblings) as the match of compounds[i]. When compounds[i] is itself linked to
// the previous compound along the same axis, the nearest candidate that matches
// the compound decides alone: a farther one offers a subset of its options.
func (s *Selector) search(d *dom.Document, start int32, i int, b *budget.Budget, axis Combinator, next func(*dom.Element) int32) (bool, error) {
	greedy := i == 0 || s.compounds[i].comb == axis
	for x := start; x >= 0; x = next(&d.Elements[x]) {
		if err := b.Charge(budget.Work, workStep); err != nil {
			return false, err
		}
		ok, err := s.compounds[i].matches(d, x, b)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		if ok, err = s.matchRest(d, x, i, b); ok || err != nil || greedy {
			return ok, err
		}
	}
	return false, nil
}

func (c *compound) matches(d *dom.Document, e int32, b *budget.Budget) (bool, error) {
	el := &d.Elements[e]
	if c.hasTag {
		if c.tag != 0 {
			if el.Tag != c.tag {
				return false, nil
			}
		} else if el.Tag != 0 || el.Name != c.name {
			return false, nil
		}
	}
	if c.id != "" && el.ID != c.id {
		return false, nil
	}
	for _, cl := range c.classes {
		ok, err := hasClass(el, cl, b)
		if !ok || err != nil {
			return false, err
		}
	}
	for i := range c.attrs {
		ok, err := c.attrs[i].matches(el, b)
		if !ok || err != nil {
			return false, err
		}
	}
	for i := range c.pseudos {
		if !c.pseudos[i].matches(el) {
			return false, nil
		}
	}
	return true, nil
}

// hasClass reports whether el has the class; the scan is charged first, in
// proportion to the number of classes and to the length of the one searched.
func hasClass(el *dom.Element, class string, b *budget.Budget) (bool, error) {
	if err := b.Charge(budget.Work, scanUnits(len(el.Classes)*(8+len(class)))); err != nil {
		return false, err
	}
	return slices.Contains(el.Classes, class), nil
}

func (p pseudoClass) matches(el *dom.Element) bool {
	switch p.kind {
	case pseudoRoot:
		return el.Parent < 0
	case pseudoFirstChild:
		return el.SiblingIndex == 0
	case pseudoLastChild:
		return el.SiblingIndex == el.SiblingCount-1
	case pseudoNth:
		pos := int(el.SiblingIndex) + 1
		if p.a == 0 {
			return pos == p.b
		}
		diff := pos - p.b
		return diff%p.a == 0 && diff/p.a >= 0
	}
	return false
}

// attrValue returns the value of the attribute name of el, except for class
// (see matches). Charged by the caller.
func attrValue(el *dom.Element, name string) (string, bool) {
	switch name {
	case "id":
		return el.ID, el.ID != ""
	case "style":
		return el.Style, el.Style != ""
	}
	for i := range el.Attrs {
		if el.Attrs[i].Key == name {
			return el.Attrs[i].Val, true
		}
	}
	return "", false
}

func (a *attrSelector) matches(el *dom.Element, b *budget.Budget) (bool, error) {
	if err := b.Charge(budget.Work, scanUnits(len(el.Attrs)*8)); err != nil {
		return false, err
	}
	if a.name == "class" {
		return a.matchesClass(el, b)
	}
	v, ok := attrValue(el, a.name)
	if !ok {
		return false, nil
	}
	return a.compare(v, b)
}

// matchesClass tests the class attribute, which reads as the classes joined by
// single spaces, without joining them: nothing is allocated.
func (a *attrSelector) matchesClass(el *dom.Element, b *budget.Budget) (bool, error) {
	if a.op == attrExists {
		return len(el.Classes) > 0, nil
	}
	total := 0
	for _, c := range el.Classes {
		total += len(c) + 1
	}
	// One unit per two classes on top of the bytes: comparing a class costs about
	// 8 ns whatever its length.
	units := scanUnits(total+len(a.val)) + int64(len(el.Classes))/2
	if a.op == attrSubstr {
		// The search is naive over the classes: quadratic in the worst case.
		units += int64(total) * int64(len(a.val)) / 16
	}
	if err := b.Charge(budget.Work, units); err != nil {
		return false, err
	}
	if len(el.Classes) == 0 {
		return false, nil
	}
	cl := classText(el.Classes)
	switch a.op {
	case attrEquals:
		return a.classEquals(cl, a.val), nil
	case attrIncludes:
		if a.never {
			return false, nil
		}
		for _, c := range el.Classes {
			if a.eq(c) {
				return true, nil
			}
		}
		return false, nil
	case attrDash:
		return a.classEquals(cl, a.val) || a.classPrefix(cl, a.dash), nil
	case attrPrefix:
		return a.val != "" && a.classPrefix(cl, a.val), nil
	case attrSuffix:
		return a.val != "" && a.classSuffix(cl, total-2), nil
	case attrSubstr:
		return a.val != "" && a.classContains(cl), nil
	}
	return false, nil
}

const htmlSpace = " \t\n\f\r"

// classText is a class attribute: its classes, to be read joined by single
// spaces.
type classText []string

// matchAt reports whether val is the text starting at byte off of class ci, and
// where it ends: class index and offset after the last matched byte.
func (a *attrSelector) matchAt(cl classText, ci, off int, val string) (endCi, endOff int, ok bool) {
	for {
		rest := cl[ci][off:]
		n := min(len(rest), len(val))
		if !a.eqPrefix(rest[:n], val[:n]) {
			return 0, 0, false
		}
		val, off = val[n:], off+n
		if val == "" {
			return ci, off, true
		}
		// The class is exhausted; the text goes on with a space and the next class.
		if val[0] != ' ' || ci+1 >= len(cl) {
			return 0, 0, false
		}
		val, ci, off = val[1:], ci+1, 0
		if val == "" {
			return ci, 0, true
		}
	}
}

// eqPrefix reports whether seg equals val (same length), ignoring the ASCII
// case of seg when the i flag is set; val is already lowercase then.
func (a *attrSelector) eqPrefix(seg, val string) bool {
	if !a.ignoreCase {
		return seg == val
	}
	return foldPrefix(seg, val)
}

// classEquals reports whether the joined classes equal val.
func (a *attrSelector) classEquals(cl classText, val string) bool {
	ci, off, ok := a.matchAt(cl, 0, 0, val)
	return ok && ci == len(cl)-1 && off == len(cl[ci])
}

func (a *attrSelector) classPrefix(cl classText, val string) bool {
	_, _, ok := a.matchAt(cl, 0, 0, val)
	return ok
}

// classSuffix reports whether the joined classes, last is the index of their
// last byte, end with val.
func (a *attrSelector) classSuffix(cl classText, last int) bool {
	start := last + 1 - len(a.val)
	if start < 0 {
		return false
	}
	for ci := range cl {
		if start <= len(cl[ci]) {
			ci2, off, ok := a.matchAt(cl, ci, start, a.val)
			return ok && ci2 == len(cl)-1 && off == len(cl[ci2])
		}
		start -= len(cl[ci]) + 1
	}
	return false
}

func (a *attrSelector) classContains(cl classText) bool {
	for ci := range cl {
		for off := 0; off < len(cl[ci]); off++ {
			if a.first(cl[ci][off]) {
				if _, _, ok := a.matchAt(cl, ci, off, a.val); ok {
					return true
				}
			}
		}
	}
	return false
}

// first reports whether c can start the searched value.
func (a *attrSelector) first(c byte) bool {
	if a.ignoreCase {
		c = asciiLower(c)
	}
	return c == a.val[0]
}

// compare applies the operator to the value v. The scan is charged first, in
// proportion to the bytes of v and of the searched value: a unit per 64 bytes,
// plus, for the operators that really read all of v, a unit per 8 bytes of
// the word scan of ~= and of the substring search of *= (about 2.5 ns per byte
// in the worst case). A case-insensitive substring search is quadratic in the
// worst case and is charged for it.
func (a *attrSelector) compare(v string, b *budget.Budget) (bool, error) {
	units := scanUnits(len(v) + len(a.val))
	switch a.op {
	case attrIncludes:
		units += int64(len(v)) / 8
	case attrSubstr:
		units += int64(len(v)) / 8
		if a.ignoreCase {
			units += int64(len(v)) * int64(len(a.val)) / 16
		}
	}
	if err := b.Charge(budget.Work, units); err != nil {
		return false, err
	}
	switch a.op {
	case attrExists:
		return true, nil
	case attrEquals:
		return a.eq(v), nil
	case attrIncludes:
		return a.includes(v), nil
	case attrDash:
		return a.eq(v) || a.hasPrefix(v, a.dash), nil
	case attrPrefix:
		return a.val != "" && a.hasPrefix(v, a.val), nil
	case attrSuffix:
		return a.val != "" && a.hasSuffix(v), nil
	case attrSubstr:
		return a.val != "" && a.contains(v), nil
	}
	return false, nil
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// includes reports whether v holds val as a whitespace-separated word, in one
// pass over v.
func (a *attrSelector) includes(v string) bool {
	if a.never {
		return false
	}
	start := -1
	for i := 0; i <= len(v); i++ {
		if i == len(v) || isHTMLSpace(v[i]) {
			if start >= 0 && i-start == len(a.val) && a.eq(v[start:i]) {
				return true
			}
			start = -1
		} else if start < 0 {
			start = i
		}
	}
	return false
}

// eq, hasPrefix, hasSuffix and contains compare v with the searched value,
// folding ASCII case (and only ASCII) of v when the i flag is set: a.val is
// already lowercase. Nothing is allocated.
func (a *attrSelector) eq(v string) bool {
	if !a.ignoreCase {
		return v == a.val
	}
	return len(v) == len(a.val) && foldPrefix(v, a.val)
}

func (a *attrSelector) hasPrefix(v, prefix string) bool {
	if !a.ignoreCase {
		return strings.HasPrefix(v, prefix)
	}
	return len(v) >= len(prefix) && foldPrefix(v, prefix)
}

func (a *attrSelector) hasSuffix(v string) bool {
	if !a.ignoreCase {
		return strings.HasSuffix(v, a.val)
	}
	return len(v) >= len(a.val) && foldPrefix(v[len(v)-len(a.val):], a.val)
}

func (a *attrSelector) contains(v string) bool {
	if !a.ignoreCase {
		return strings.Contains(v, a.val)
	}
	for i := 0; i+len(a.val) <= len(v); i++ {
		if asciiLower(v[i]) == a.val[0] && foldPrefix(v[i:], a.val) {
			return true
		}
	}
	return false
}

func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// foldPrefix reports whether v starts with lower, ignoring the ASCII case of v;
// len(v) >= len(lower).
func foldPrefix(v, lower string) bool {
	for i := 0; i < len(lower); i++ {
		if asciiLower(v[i]) != lower[i] {
			return false
		}
	}
	return true
}
