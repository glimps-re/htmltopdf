package css

import (
	"fmt"
	"iter"
	"math"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html/atom"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// Limits of one selector (design §6.1).
const (
	// MaxCompounds is the number of compound selectors of a selector.
	MaxCompounds = 32
	// MaxSimple is the number of simple selectors of a compound selector.
	MaxSimple = 32
)

// Combinator links a compound selector to the one on its left.
type Combinator uint8

// Combinators; CombNone is the first compound of a selector.
const (
	CombNone Combinator = iota
	// CombDescendant is " ".
	CombDescendant
	// CombChild is ">".
	CombChild
	// CombNext is "+": the next sibling.
	CombNext
	// CombLater is "~": any later sibling.
	CombLater
)

// PseudoElement is the pseudo-element a selector targets, if any. A selector
// with a pseudo-element is matched against the originating element: the style
// stage applies the rule to the generated box.
type PseudoElement uint8

// Pseudo-elements.
const (
	PseudoNone PseudoElement = iota
	PseudoBefore
	PseudoAfter
)

type pseudoKind uint8

const (
	pseudoRoot pseudoKind = iota + 1
	pseudoFirstChild
	pseudoLastChild
	pseudoNth
	// pseudoNever is an interaction pseudo-class (:hover...) or a condition that
	// cannot hold (two different ids): it never matches on paper.
	pseudoNever
)

type pseudoClass struct {
	kind pseudoKind
	// a and b are the an+b of :nth-child.
	a, b int
}

type attrOp uint8

const (
	attrExists attrOp = iota + 1
	attrEquals
	attrIncludes
	attrDash
	attrPrefix
	attrSuffix
	attrSubstr
)

type attrSelector struct {
	name, val string
	// dash is val+"-", for |=.
	dash string
	// never is set for ~= with an empty value or a value with whitespace, which
	// no word equals.
	never      bool
	op         attrOp
	ignoreCase bool
}

// compound is a compound selector: the simple selectors that apply to one
// element, with the combinator that links it to the compound on its left.
type compound struct {
	comb    Combinator
	hasTag  bool
	tag     atom.Atom
	name    string // lowercase tag name
	id      string
	classes []string
	attrs   []attrSelector
	pseudos []pseudoClass
	// universal is set by "*", which only makes the compound non-empty.
	universal bool
}

func (c *compound) simples() int {
	n := len(c.classes) + len(c.attrs) + len(c.pseudos)
	if c.hasTag {
		n++
	}
	if c.id != "" {
		n++
	}
	if c.universal {
		n++
	}
	return n
}

// Specificity is the (a, b, c) specificity of a selector packed as
// a<<20 | b<<10 | c with every field saturated at 1023, so that integer
// comparison orders selectors.
type Specificity uint32

const specMax = 1023

// Fields of a Specificity, from the most significant.
const (
	specID = iota
	specClass
	specType
)

func (s Specificity) add(field int) Specificity {
	shift := uint(10 * (2 - field))
	if uint32(s)>>shift&specMax == specMax {
		return s
	}
	return s + Specificity(1)<<shift
}

// IDs returns the first field: id selectors.
func (s Specificity) IDs() int { return int(s >> 20 & specMax) }

// Classes returns the second field: classes, attributes and pseudo-classes.
func (s Specificity) Classes() int { return int(s >> 10 & specMax) }

// Types returns the third field: types and pseudo-elements.
func (s Specificity) Types() int { return int(s & specMax) }

// Selector is a parsed complex selector. It is immutable and shared by
// concurrent renders.
type Selector struct {
	compounds   []compound
	specificity Specificity
	pseudo      PseudoElement
}

// Specificity returns the specificity of s.
func (s *Selector) Specificity() Specificity { return s.specificity }

// Pseudo returns the pseudo-element s targets.
func (s *Selector) Pseudo() PseudoElement { return s.pseudo }

// ParseSelectors parses the prelude of a style rule: a comma separated list
// of selectors. Each member is charged to budget.Selectors before it is read,
// and a budget error is returned. A list with a member that is malformed or
// outside the supported subset is dropped as a whole, as CSS requires: the
// result is nil and an unsupported-selector warning is recorded.
//
// Type selectors are matched in lowercase against HTML tag names; foreign (SVG,
// MathML) subtrees are skipped by box.Build in M1, so their case-adjusted names
// need no folding.
//
// Supported: type, *, #id, .class, [attr], [attr=value] and the operators
// ~= |= ^= $= *= with the i and s flags, :root, :first-child, :last-child,
// :nth-child(an+b | odd | even), the interaction pseudo-classes that never
// match on paper (:hover, :focus, :active, :visited, :focus-within,
// :focus-visible, :target), the pseudo-elements ::before and ::after, and the
// combinators " ", ">", "+" and "~".
func ParseSelectors(prelude []Token, b *budget.Budget) ([]*Selector, error) {
	var out []*Selector
	reason := ""
	for lo, hi := range commaRanges(prelude) {
		if err := b.Charge(budget.Selectors, 1); err != nil {
			return nil, err
		}
		if reason == "" {
			sel, why := parseSelector(prelude[lo:hi])
			if sel == nil {
				reason = why
			} else {
				out = append(out, sel)
			}
		}
	}
	if reason != "" {
		b.Warn(budget.WarnUnsupportedSelector, reason+": "+describe(prelude))
		return nil, nil
	}
	return out, nil
}

// describe returns the start of the selector text for a warning detail.
func describe(toks []Token) string {
	var b strings.Builder
	for _, t := range toks {
		if b.Len() >= budget.MaxDetailBytes {
			break
		}
		switch t.Type {
		case TIdent, TDelim, TMatch:
			b.WriteString(short(t.Text))
		case THash:
			b.WriteString("#" + short(t.Text))
		case TString:
			b.WriteString(strconv.Quote(short(t.Text)))
		case TFunction:
			b.WriteString(short(t.Text) + "(")
		case TWhitespace:
			b.WriteByte(' ')
		case TColon:
			b.WriteByte(':')
		case TComma:
			b.WriteByte(',')
		case TLBracket:
			b.WriteByte('[')
		case TRBracket:
			b.WriteByte(']')
		case TLParen:
			b.WriteByte('(')
		case TRParen:
			b.WriteByte(')')
		case TNumber:
			b.WriteString(strconv.FormatFloat(float64(t.Num), 'g', -1, 32))
		case TPercentage:
			b.WriteString(strconv.FormatFloat(float64(t.Num), 'g', -1, 32) + "%")
		case TDimension:
			b.WriteString(strconv.FormatFloat(float64(t.Num), 'g', -1, 32) + unitText(t))
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

var neverMatching = map[string]bool{
	"hover": true, "focus": true, "active": true, "visited": true,
	"focus-within": true, "focus-visible": true, "target": true,
}

type selParser struct {
	comps []compound
	cur   compound
	comb  Combinator
	pseu  PseudoElement
	spec  Specificity
}

// flush ends the current compound selector; nextComb links the following one.
func (p *selParser) flush(nextComb Combinator) string {
	if p.cur.simples() == 0 {
		return "empty compound selector"
	}
	if len(p.comps) >= MaxCompounds {
		return fmt.Sprintf("more than %d compound selectors", MaxCompounds)
	}
	p.cur.comb = p.comb
	p.comps = append(p.comps, p.cur)
	p.cur, p.comb = compound{}, nextComb
	return ""
}

func (p *selParser) full() string {
	if p.cur.simples() >= MaxSimple {
		return fmt.Sprintf("more than %d simple selectors", MaxSimple)
	}
	return ""
}

func parseSelector(toks []Token) (*Selector, string) {
	toks = trimSpace(toks)
	var p selParser
	space := false
	for i := 0; i < len(toks); {
		t := toks[i]
		if p.pseu != PseudoNone {
			return nil, "pseudo-element is not last"
		}
		if t.Type == TWhitespace {
			space = true
			i++
			continue
		}
		if t.Type == TDelim && (t.Text == ">" || t.Text == "+" || t.Text == "~") {
			comb := map[string]Combinator{">": CombChild, "+": CombNext, "~": CombLater}[t.Text]
			if why := p.flush(comb); why != "" {
				return nil, why
			}
			space = false
			i++
			continue
		}
		if space && p.cur.simples() > 0 {
			if why := p.flush(CombDescendant); why != "" {
				return nil, why
			}
		}
		space = false
		n, why := p.simple(toks, i)
		if why != "" {
			return nil, why
		}
		i = n
	}
	if why := p.flush(CombNone); why != "" {
		return nil, why
	}
	// Exact size: a parsed stylesheet keeps thousands of selectors.
	return &Selector{compounds: slices.Clone(p.comps), specificity: p.spec, pseudo: p.pseu}, ""
}

// simple parses the simple selector starting at toks[i] and returns the index
// after it.
func (p *selParser) simple(toks []Token, i int) (int, string) {
	if why := p.full(); why != "" {
		return 0, why
	}
	t := toks[i]
	switch {
	case t.Type == TIdent:
		if p.cur.simples() > 0 {
			return 0, "type selector after another selector"
		}
		name := lowerASCII(t.Text)
		p.cur.hasTag, p.cur.name, p.cur.tag = true, name, atom.Lookup([]byte(name))
		p.spec = p.spec.add(specType)
		return i + 1, ""
	case t.Type == TDelim && t.Text == "*":
		if p.cur.simples() > 0 {
			return 0, "universal selector after another selector"
		}
		p.cur.universal = true
		return i + 1, ""
	case t.Type == THash:
		if t.Flags&FlagID == 0 {
			return 0, "invalid id selector"
		}
		p.spec = p.spec.add(specID)
		switch {
		case p.cur.id == "":
			p.cur.id = t.Text
		case p.cur.id != t.Text:
			p.cur.pseudos = append(p.cur.pseudos, pseudoClass{kind: pseudoNever})
		}
		return i + 1, ""
	case t.Type == TDelim && t.Text == ".":
		if i+1 >= len(toks) || toks[i+1].Type != TIdent {
			return 0, "class selector without a name"
		}
		p.cur.classes = append(p.cur.classes, toks[i+1].Text)
		p.spec = p.spec.add(specClass)
		return i + 2, ""
	case t.Type == TLBracket:
		end := i + int(t.End)
		if t.End < 2 || end > len(toks) || toks[end-1].Type != TRBracket {
			return 0, "unclosed attribute selector"
		}
		a, why := attrSel(toks[i+1 : end-1])
		if why != "" {
			return 0, why
		}
		p.cur.attrs = append(p.cur.attrs, a)
		p.spec = p.spec.add(specClass)
		return end, ""
	case t.Type == TColon:
		return p.pseudo(toks, i)
	}
	return 0, "unsupported selector syntax"
}

var attrOps = map[string]attrOp{"~=": attrIncludes, "|=": attrDash, "^=": attrPrefix, "$=": attrSuffix, "*=": attrSubstr}

func attrSel(inner []Token) (attrSelector, string) {
	inner = trimSpace(inner)
	if len(inner) == 0 || inner[0].Type != TIdent {
		return attrSelector{}, "attribute selector without a name"
	}
	a := attrSelector{name: lowerASCII(inner[0].Text), op: attrExists}
	rest := trimSpace(inner[1:])
	if len(rest) == 0 {
		return a, ""
	}
	switch {
	case rest[0].Type == TDelim && rest[0].Text == "=":
		a.op = attrEquals
	case rest[0].Type == TMatch && attrOps[rest[0].Text] != 0:
		a.op = attrOps[rest[0].Text]
	default:
		return attrSelector{}, "unsupported attribute operator"
	}
	val := trimSpace(rest[1:])
	if len(val) == 0 || (val[0].Type != TIdent && val[0].Type != TString) {
		return attrSelector{}, "unsupported attribute value"
	}
	a.val = val[0].Text
	switch flags := trimSpace(val[1:]); {
	case len(flags) == 0:
	case len(flags) == 1 && flags[0].Type == TIdent && lowerASCII(flags[0].Text) == "i":
		a.ignoreCase = true
		a.val = lowerASCII(a.val)
	case len(flags) == 1 && flags[0].Type == TIdent && lowerASCII(flags[0].Text) == "s":
	default:
		return attrSelector{}, "unsupported attribute flag"
	}
	switch a.op {
	case attrDash:
		a.dash = a.val + "-"
	case attrIncludes:
		a.never = a.val == "" || strings.ContainsAny(a.val, htmlSpace)
	}
	return a, ""
}

func (p *selParser) pseudo(toks []Token, i int) (int, string) {
	i++
	if i < len(toks) && toks[i].Type == TColon {
		if i+1 >= len(toks) || toks[i+1].Type != TIdent {
			return 0, "unsupported pseudo-element"
		}
		return p.pseudoElement(lowerASCII(toks[i+1].Text), i+2)
	}
	if i >= len(toks) {
		return 0, "pseudo-class without a name"
	}
	t := toks[i]
	if t.Type == TFunction {
		end := i + int(t.End)
		if lowerASCII(t.Text) != "nth-child" || t.End < 2 || end > len(toks) || toks[end-1].Type != TRParen {
			return 0, "unsupported pseudo-class :" + short(t.Text) + "()"
		}
		a, b, ok := parseNth(toks[i+1 : end-1])
		if !ok {
			return 0, "invalid :nth-child argument"
		}
		p.addPseudo(pseudoClass{kind: pseudoNth, a: int(a), b: int(b)})
		return end, ""
	}
	if t.Type != TIdent {
		return 0, "unsupported pseudo-class"
	}
	name := lowerASCII(t.Text)
	switch {
	case name == "before" || name == "after":
		return p.pseudoElement(name, i+1)
	case name == "root":
		p.addPseudo(pseudoClass{kind: pseudoRoot})
	case name == "first-child":
		p.addPseudo(pseudoClass{kind: pseudoFirstChild})
	case name == "last-child":
		p.addPseudo(pseudoClass{kind: pseudoLastChild})
	case neverMatching[name]:
		p.addPseudo(pseudoClass{kind: pseudoNever})
	default:
		return 0, "unsupported pseudo-class :" + short(name)
	}
	return i + 1, ""
}

func (p *selParser) addPseudo(c pseudoClass) {
	p.cur.pseudos = append(p.cur.pseudos, c)
	p.spec = p.spec.add(specClass)
}

func (p *selParser) pseudoElement(name string, nextTok int) (int, string) {
	switch name {
	case "before":
		p.pseu = PseudoBefore
	case "after":
		p.pseu = PseudoAfter
	default:
		return 0, "unsupported pseudo-element ::" + short(name)
	}
	p.spec = p.spec.add(specType)
	return nextTok, ""
}

// maxNth bounds the coefficients of an+b.
const maxNth = 1 << 30

// nthInt converts an integral token number within bounds.
func nthInt(f float32) (int32, bool) {
	v := float64(f)
	if v != math.Trunc(v) || math.Abs(v) > maxNth {
		return 0, false
	}
	return int32(v), true
}

// nthSuffix parses the "n" or "n-<digits>" part of an+b, lowercase, returning
// b and whether the text had a "-<digits>" tail.
func nthSuffix(s string) (b int32, hasB, ok bool) {
	if s == "n" {
		return 0, false, true
	}
	digits, found := strings.CutPrefix(s, "n-")
	if !found || digits == "" || len(digits) > 9 {
		return 0, false, false
	}
	v, err := strconv.ParseInt(digits, 10, 32)
	if err != nil {
		return 0, false, false
	}
	return -int32(v), true, true
}

// parseNth parses the argument of :nth-child: odd, even, an integer or an+b.
// The lexer folds the sign into a number token, so "2n+1" and "2n +1" read
// alike; "2n 1" is accepted too.
func parseNth(toks []Token) (a, b int32, ok bool) {
	toks = trimSpace(toks)
	if len(toks) == 0 {
		return 0, 0, false
	}
	t := toks[0]
	rest := toks[1:]
	var hasB bool
	switch {
	case len(toks) == 1 && t.Type == TIdent && lowerASCII(t.Text) == "odd":
		return 2, 1, true
	case len(toks) == 1 && t.Type == TIdent && lowerASCII(t.Text) == "even":
		return 2, 0, true
	case len(toks) == 1 && t.Type == TNumber:
		n, ok := nthInt(t.Num)
		return 0, n, ok
	case t.Type == TDimension && t.Unit == UnitOther:
		var okA bool
		if a, okA = nthInt(t.Num); !okA {
			return 0, 0, false
		}
		if b, hasB, ok = nthSuffix(lowerASCII(t.Text)); !ok {
			return 0, 0, false
		}
	case t.Type == TIdent:
		name := lowerASCII(t.Text)
		a = 1
		if strings.HasPrefix(name, "-") {
			a, name = -1, name[1:]
		}
		if b, hasB, ok = nthSuffix(name); !ok {
			return 0, 0, false
		}
	case t.Type == TDelim && t.Text == "+" && len(toks) > 1 && toks[1].Type == TIdent:
		a = 1
		if b, hasB, ok = nthSuffix(lowerASCII(toks[1].Text)); !ok {
			return 0, 0, false
		}
		rest = toks[2:]
	default:
		return 0, 0, false
	}
	rest = trimSpace(rest)
	if hasB {
		return a, b, len(rest) == 0
	}
	switch {
	case len(rest) == 0:
		return a, 0, true
	case len(rest) == 1 && rest[0].Type == TNumber:
		b, ok = nthInt(rest[0].Num)
		return a, b, ok
	case len(rest) >= 2 && rest[0].Type == TDelim && (rest[0].Text == "+" || rest[0].Text == "-"):
		num := trimSpace(rest[1:])
		if len(num) != 1 || num[0].Type != TNumber || num[0].Num < 0 {
			return 0, 0, false
		}
		b, ok = nthInt(num[0].Num)
		if rest[0].Text == "-" {
			b = -b
		}
		return a, b, ok
	}
	return 0, 0, false
}

// String returns the canonical text of s.
func (s *Selector) String() string {
	var b strings.Builder
	for i := range s.compounds {
		c := &s.compounds[i]
		switch c.comb {
		case CombDescendant:
			b.WriteByte(' ')
		case CombChild:
			b.WriteString(" > ")
		case CombNext:
			b.WriteString(" + ")
		case CombLater:
			b.WriteString(" ~ ")
		}
		switch {
		case c.hasTag:
			b.WriteString(c.name)
		case c.universal:
			b.WriteByte('*')
		}
		if c.id != "" {
			b.WriteString("#" + c.id)
		}
		for _, cl := range c.classes {
			b.WriteString("." + cl)
		}
		for _, a := range c.attrs {
			b.WriteString(a.String())
		}
		for _, p := range c.pseudos {
			b.WriteString(p.String())
		}
	}
	switch s.pseudo {
	case PseudoBefore:
		b.WriteString("::before")
	case PseudoAfter:
		b.WriteString("::after")
	}
	return b.String()
}

var attrOpText = map[attrOp]string{attrExists: "", attrEquals: "=", attrIncludes: "~=", attrDash: "|=", attrPrefix: "^=", attrSuffix: "$=", attrSubstr: "*="}

func (a attrSelector) String() string {
	if a.op == attrExists {
		return "[" + a.name + "]"
	}
	flag := ""
	if a.ignoreCase {
		flag = " i"
	}
	return "[" + a.name + attrOpText[a.op] + strconv.Quote(a.val) + flag + "]"
}

func (p pseudoClass) String() string {
	switch p.kind {
	case pseudoRoot:
		return ":root"
	case pseudoFirstChild:
		return ":first-child"
	case pseudoLastChild:
		return ":last-child"
	case pseudoNth:
		return ":nth-child(" + strconv.Itoa(p.a) + "n" + fmt.Sprintf("%+d", p.b) + ")"
	}
	return ":never"
}

// unitLabels maps a unit back to its (lowercase) spelling.
var unitLabels = func() map[Unit]string {
	m := make(map[Unit]string, len(unitNames))
	for name, u := range unitNames {
		if old, ok := m[u]; !ok || name < old {
			m[u] = name
		}
	}
	return m
}()

// unitText returns the spelling of the unit of a dimension token.
func unitText(t Token) string {
	if t.Unit == UnitOther {
		return short(t.Text)
	}
	return unitLabels[t.Unit]
}

// commaRanges yields the ranges [lo, hi) of toks separated by commas outside
// blocks. The last range comes after the last comma and is empty for a
// trailing comma; toks without a comma give one range.
func commaRanges(toks []Token) iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		lo := 0
		for i := 0; i < len(toks); {
			if toks[i].Type == TComma {
				if !yield(lo, i) {
					return
				}
				lo = i + 1
			}
			i = next(toks, i)
		}
		yield(lo, len(toks))
	}
}
