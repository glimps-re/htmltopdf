package css

import (
	"math"
	"slices"
	"strings"

	"github.com/tdewolff/parse/v2"
	tcss "github.com/tdewolff/parse/v2/css"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// MaxNesting is the maximum depth of nested blocks and functions inside one
// prelude or declaration value, and of nested @media rules. Deeper items are
// dropped with a warning.
const MaxNesting = 64

// Warning codes recorded in the budget (the public WarningCode values).
const (
	WarnInvalidValue       = "invalid-value"
	WarnUnsupportedAtRule  = "unsupported-at-rule"
	detailNesting          = "nesting too deep"
	detailUnclosedBlock    = "unclosed block"
	detailRuleWithoutBlock = "rule without a block"
)

// Origin tells where a stylesheet comes from.
type Origin uint8

// Origins, from lowest to highest priority of the cascade.
const (
	OriginUA Origin = iota
	OriginDocument
	OriginExtra
)

// RuleKind is the kind of a Rule.
type RuleKind uint8

// Rule kinds.
const (
	// RuleStyle is a qualified rule: Prelude holds the selector tokens.
	RuleStyle RuleKind = iota + 1
	// RuleMedia is an @media rule: Prelude holds the query tokens, Rules its body.
	RuleMedia
	// RulePage is an @page rule: Prelude holds the page selector tokens, Decls
	// its declarations and Rules its margin boxes.
	RulePage
	// RuleMarginBox is a margin at-rule of an @page rule; Name is its name
	// without "@", lowercase.
	RuleMarginBox
)

// Declaration is a property declaration. Name is lowercase except for custom
// properties ("--x"), which keep their case. Value is trimmed of whitespace
// and of "!important".
type Declaration struct {
	Name      string
	Value     []Token
	Important bool

	va, vb int32
}

// Rule is a style rule, an @media rule, an @page rule or a margin box.
type Rule struct {
	Kind    RuleKind
	Name    string
	Prelude []Token
	Decls   []Declaration
	Rules   []Rule

	pa, pb int32
}

// Stylesheet is an immutable parsed stylesheet.
type Stylesheet struct {
	Origin Origin
	Rules  []Rule

	tokens []Token
}

var marginBoxes = map[string]bool{
	"top-left-corner": true, "top-left": true, "top-center": true, "top-right": true, "top-right-corner": true,
	"bottom-left-corner": true, "bottom-left": true, "bottom-center": true, "bottom-right": true,
	"bottom-right-corner": true, "left-top": true, "left-middle": true, "left-bottom": true,
	"right-top": true, "right-middle": true, "right-bottom": true,
}

// stopMode is the set of tokens that end an item at nesting depth 0.
type stopMode uint8

const (
	stopSemi stopMode = 1 << iota
	stopLBrace
	stopRBrace
)

// Modes of the items the parser collects. Inside a block, "}" ends every item
// without being consumed (CSS Syntax §5: the item is dropped, the block goes on).
const (
	stopQualified       = stopLBrace
	stopQualifiedNested = stopLBrace | stopRBrace
	stopAtRule          = stopSemi | stopLBrace
	stopAtRuleNested    = stopSemi | stopLBrace | stopRBrace
	stopDecl            = stopSemi | stopRBrace
	stopDeclTop         = stopSemi
)

func (m stopMode) stops(t Type) bool {
	switch t {
	case TSemicolon:
		return m&stopSemi != 0
	case TLBrace:
		return m&stopLBrace != 0
	case TRBrace:
		return m&stopRBrace != 0
	}
	return false
}

// workBatch is the number of lexer tokens charged to budget.Work at once.
const workBatch = 256

type opener struct {
	idx    int32
	closer Type
}

type parser struct {
	lex *tcss.Lexer
	str string
	off int
	b   *budget.Budget
	// err is the first budget error; the parser then sees the end of the input.
	err     error
	pending int

	arena []Token

	// Scratch stacks: a block's declarations and rules are collected here and
	// copied out once, with their exact size.
	declStack []Declaration
	ruleStack []Rule

	cur   Token
	have  bool
	eof   bool
	atBeg bool

	stack     [MaxNesting]opener
	sp        int
	overflow  int
	deep      bool
	unmatched bool

	mediaDepth int
}

func newParser(src []byte, b *budget.Budget) (*parser, error) {
	if len(src) > math.MaxInt32/2 {
		return nil, &budget.LimitError{Kind: budget.CSSBytes, Max: math.MaxInt32 / 2}
	}
	if err := b.Charge(budget.CSSBytes, int64(len(src))); err != nil {
		return nil, err
	}
	if err := b.Poll(); err != nil {
		return nil, err
	}
	// NewInputBytes appends a NUL by overwriting the byte after len(buf) when
	// there is room: give it a private buffer so that it never writes into the
	// caller's memory.
	buf := make([]byte, len(src), len(src)+1)
	copy(buf, src)
	return &parser{
		lex: tcss.NewLexer(parse.NewInputBytes(buf)),
		str: string(buf), b: b, atBeg: true,
		// One token per byte is the worst case: the arena never regrows.
		arena: make([]Token, 0, len(src)+1),
	}, nil
}

// charge pays n lexer tokens to budget.Work, which also polls the context.
func (p *parser) charge(n int) {
	if p.err != nil || n == 0 {
		return
	}
	if err := p.b.Charge(budget.Work, int64(n)); err != nil {
		p.err = err
		p.eof = true
	}
}

// fill loads the next significant token into p.cur.
func (p *parser) fill() {
	for {
		tt, data := p.lex.Next()
		if tt == tcss.ErrorToken {
			p.charge(p.pending)
			p.pending = 0
			p.eof = true
			return
		}
		if p.pending++; p.pending == workBatch {
			p.charge(workBatch)
			p.pending = 0
			if p.eof {
				return
			}
		}
		start := p.off
		p.off += len(data)
		if tt == tcss.CommentToken {
			continue
		}
		convert(&p.cur, tt, p.str[start:p.off])
		p.have = true
		return
	}
}

// peek loads the next token into p.cur and reports whether there is one.
func (p *parser) peek() bool {
	if !p.have && !p.eof {
		p.fill()
	}
	return p.have
}

func (p *parser) advance() {
	if p.cur.Type != TWhitespace {
		p.atBeg = false
	}
	p.have = false
}

// convert writes the token of lexer type tt and source raw into dst.
func convert(dst *Token, tt tcss.TokenType, raw string) {
	switch tt {
	case tcss.IdentToken, tcss.CustomPropertyNameToken:
		*dst = Token{Type: TIdent, Text: text(raw, false)}
		return
	case tcss.FunctionToken:
		*dst = Token{Type: TFunction, Text: text(raw[:len(raw)-1], false)}
		return
	case tcss.AtKeywordToken:
		*dst = Token{Type: TAtKeyword, Text: text(raw[1:], false)}
		return
	case tcss.HashToken:
		*dst = Token{Type: THash, Text: text(raw[1:], false)}
		if isIdentStart(raw[1:]) {
			dst.Flags = FlagID
		}
		return
	case tcss.StringToken:
		*dst = Token{Type: TString, Text: text(raw[1:len(raw)-closingQuote(raw)], true)}
		return
	case tcss.BadStringToken:
		*dst = Token{Type: TBadString}
		return
	case tcss.URLToken:
		target := urlTarget(raw)
		if n := len(target); n >= 2 && (target[0] == '"' || target[0] == '\'') && target[n-1] == target[0] {
			*dst = Token{Type: TURL, Text: text(target[1:n-1], true)}
			return
		}
		*dst = Token{Type: TURL, Text: text(target, false)}
		return
	case tcss.BadURLToken:
		*dst = Token{Type: TBadURL}
		return
	case tcss.NumberToken:
		*dst = Token{Type: TNumber, Num: parseNum(raw)}
		return
	case tcss.PercentageToken:
		*dst = Token{Type: TPercentage, Unit: UnitPercent, Num: parseNum(raw[:len(raw)-1])}
		return
	case tcss.DimensionToken:
		n := numEnd(raw)
		u, name := unitOf(raw[n:])
		*dst = Token{Type: TDimension, Unit: u, Num: parseNum(raw[:n]), Text: name}
		return
	case tcss.UnicodeRangeToken:
		*dst = Token{Type: TUnicodeRange, Text: raw}
		return
	case tcss.IncludeMatchToken, tcss.DashMatchToken, tcss.PrefixMatchToken, tcss.SuffixMatchToken, tcss.SubstringMatchToken:
		*dst = Token{Type: TMatch, Text: raw}
		return
	case tcss.WhitespaceToken:
		*dst = Token{Type: TWhitespace}
		return
	case tcss.CDOToken:
		*dst = Token{Type: TCDO}
		return
	case tcss.CDCToken:
		*dst = Token{Type: TCDC}
		return
	case tcss.ColonToken:
		*dst = Token{Type: TColon}
		return
	case tcss.SemicolonToken:
		*dst = Token{Type: TSemicolon}
		return
	case tcss.CommaToken:
		*dst = Token{Type: TComma}
		return
	case tcss.LeftBracketToken:
		*dst = Token{Type: TLBracket}
		return
	case tcss.RightBracketToken:
		*dst = Token{Type: TRBracket}
		return
	case tcss.LeftParenthesisToken:
		*dst = Token{Type: TLParen}
		return
	case tcss.RightParenthesisToken:
		*dst = Token{Type: TRParen}
		return
	case tcss.LeftBraceToken:
		*dst = Token{Type: TLBrace}
		return
	case tcss.RightBraceToken:
		*dst = Token{Type: TRBrace}
		return
	}
	*dst = Token{Type: TDelim, Text: clean(raw)}
}

// closingQuote is 1 when the string token raw ends with its opening quote.
func closingQuote(raw string) int {
	if len(raw) >= 2 && raw[len(raw)-1] == raw[0] && !escapedEnd(raw) {
		return 1
	}
	return 0
}

// escapedEnd reports whether the last byte of raw is preceded by an odd number
// of backslashes.
func escapedEnd(raw string) bool {
	n := 0
	for i := len(raw) - 2; i > 0 && raw[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// urlTarget extracts the target of an unquoted url(...) token.
func urlTarget(raw string) string {
	s := raw[min(4, len(raw)):]
	s = strings.TrimSuffix(s, ")")
	return strings.Trim(s, " \t\r\n\f")
}

// push appends t to the arena and tracks block nesting.
func (p *parser) push(t *Token) {
	idx := len(p.arena)
	p.arena = append(p.arena, *t)
	switch {
	case isOpener(t.Type):
		if p.sp < MaxNesting {
			p.stack[p.sp] = opener{idx: clampIdx(idx), closer: closerOf(t.Type)}
			p.sp++
		} else {
			p.overflow++
			p.deep = true
		}
	case isCloser(t.Type):
		switch {
		case p.overflow > 0:
			p.overflow--
		case p.sp > 0 && p.stack[p.sp-1].closer == t.Type:
			o := p.stack[p.sp-1].idx
			p.arena[o].End = clampIdx(idx - int(o) + 1)
			p.sp--
		default:
			p.unmatched = true
		}
	}
}

// closeOpen ends the blocks left open at the end of an item.
func (p *parser) closeOpen() {
	for ; p.sp > 0; p.sp-- {
		o := p.stack[p.sp-1].idx
		p.arena[o].End = clampIdx(len(p.arena) - int(o))
	}
	p.overflow = 0
}

// collect appends the component values of an item to the arena, up to (not
// including) a token that ends it at nesting depth 0.
func (p *parser) collect(m stopMode) {
	p.deep, p.unmatched = false, false
	for p.peek() {
		if p.sp == 0 && p.overflow == 0 && m.stops(p.cur.Type) {
			break
		}
		p.advance()
		p.push(&p.cur)
	}
	p.closeOpen()
}

// skipBlock consumes a block through its matching "}" without storing it. The
// opening "{" is the current token, or has just been consumed when inside is
// true.
func (p *parser) skipBlock(inside bool) {
	var storage [MaxNesting]Type
	closers := storage[:0]
	over := 0
	if inside {
		closers = append(closers, TRBrace)
	}
	for p.peek() {
		t := p.cur.Type
		p.advance()
		switch {
		case isOpener(t):
			if len(closers) < MaxNesting {
				closers = append(closers, closerOf(t))
			} else {
				over++
			}
		case isCloser(t):
			if over > 0 {
				over--
			} else if n := len(closers); n > 0 && closers[n-1] == t {
				closers = closers[:n-1]
			}
		}
		if len(closers) == 0 && over == 0 {
			return
		}
	}
}

// trimSpace returns toks without its leading and trailing whitespace tokens.
func trimSpace(toks []Token) []Token {
	for len(toks) > 0 && toks[0].Type == TWhitespace {
		toks = toks[1:]
	}
	for len(toks) > 0 && toks[len(toks)-1].Type == TWhitespace {
		toks = toks[:len(toks)-1]
	}
	return toks
}

// trimRange returns the range [lo, hi) of the arena without leading and
// trailing whitespace tokens, within [lo, hi).
func (p *parser) trimRange(lo, hi int) (int, int) {
	for lo < hi && p.arena[lo].Type == TWhitespace {
		lo++
	}
	for hi > lo && p.arena[hi-1].Type == TWhitespace {
		hi--
	}
	return lo, hi
}

// prelude returns the range of the trimmed tokens collected since start.
func (p *parser) prelude(start int) (pa, pb int32) {
	lo, hi := p.trimRange(start, len(p.arena))
	clampEnds(p.arena[lo:hi])
	return clampIdx(lo), clampIdx(hi)
}

func short(s string) string { return s[:min(len(s), budget.MaxDetailBytes)] }

// makeDecl turns the tokens collected since start into a declaration, or
// drops them and warns. The value is recorded as a range of the arena.
func (p *parser) makeDecl(start int) (Declaration, bool) {
	end := len(p.arena)
	drop := func(detail string) (Declaration, bool) {
		p.arena = p.arena[:start]
		p.b.Warn(WarnInvalidValue, detail)
		return Declaration{}, false
	}
	i := start
	for i < end && p.arena[i].Type == TWhitespace {
		i++
	}
	if i >= end || p.arena[i].Type != TIdent {
		return drop("malformed declaration")
	}
	name := p.arena[i].Text
	i++
	for i < end && p.arena[i].Type == TWhitespace {
		i++
	}
	if i >= end || p.arena[i].Type != TColon {
		return drop("declaration " + short(name) + " has no colon")
	}
	custom := strings.HasPrefix(name, "--")
	if !custom {
		name = lowerASCII(name)
	}
	lo, hi := p.trimRange(i+1, end)
	important := false
	if hi-lo >= 2 {
		k := hi - 2
		for k > lo && p.arena[k].Type == TWhitespace {
			k--
		}
		if last := p.arena[hi-1]; last.Type == TIdent && lowerASCII(last.Text) == "important" &&
			p.arena[k].Type == TDelim && p.arena[k].Text == "!" && !p.insideBlock(lo, k) {
			important = true
			lo, hi = p.trimRange(lo, k)
		}
	}
	switch {
	case p.deep:
		return drop(detailNesting + " in " + short(name))
	case p.unmatched:
		return drop("unmatched bracket in " + short(name))
	case lo == hi && !custom:
		return drop("declaration " + short(name) + " has no value")
	}
	for _, t := range p.arena[lo:hi] {
		if t.Type == TBadString || t.Type == TBadURL || (!custom && t.Type == TLBrace) {
			return drop("invalid token in " + short(name))
		}
	}
	clampEnds(p.arena[lo:hi])
	return Declaration{Name: name, Important: important, va: clampIdx(lo), vb: clampIdx(hi)}, true
}

// insideBlock reports whether arena[k] belongs to a block that is still open
// at the end of the value starting at lo.
func (p *parser) insideBlock(lo, k int) bool {
	for i := lo; i < k; i++ {
		if isOpener(p.arena[i].Type) && i+int(p.arena[i].End) > k {
			return true
		}
	}
	return false
}

// clampEnds shortens the extent of blocks left open so that it does not reach
// past the end of toks (trimming may have removed their trailing whitespace).
func clampEnds(toks []Token) {
	for i := range toks {
		if isOpener(toks[i].Type) && i+int(toks[i].End) > len(toks) {
			toks[i].End = clampIdx(len(toks) - i)
		}
	}
}

func clampIdx(i int) int32 { return int32(min(max(i, 0), math.MaxInt32)) }

// lowerASCII lowercases the ASCII letters of s only: strings.ToLower would
// also fold non-ASCII letters (the Kelvin sign U+212A is "k"), which CSS
// keywords and property names must not.
func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// pushDouble appends v, doubling the capacity when full: append grows large
// slices by 25%, which allocates about five times the final size in total.
func pushDouble[T any](s []T, v T) []T {
	if len(s) == cap(s) {
		s = slices.Grow(s, max(len(s), 16))
	}
	return append(s, v)
}

// takeFrom returns an exact-size copy of stack[base:], nil when empty, that
// cannot be appended to into shared memory.
func takeFrom[T any](stack []T, base int) []T {
	if len(stack) == base {
		return nil
	}
	return slices.Clip(slices.Clone(stack[base:]))
}

// parseDeclBlock consumes the declarations of a block whose "{" was just
// consumed, through its "}". When margins is true, margin at-rules are parsed
// into the returned rules.
func (p *parser) parseDeclBlock(margins bool) (decls []Declaration, boxes []Rule) {
	base := len(p.declStack)
	for {
		switch {
		case !p.peek():
			p.b.Warn(WarnInvalidValue, detailUnclosedBlock)
			decls = takeFrom(p.declStack, base)
			p.declStack = p.declStack[:base]
			return decls, boxes
		case p.cur.Type == TWhitespace || p.cur.Type == TSemicolon:
			p.advance()
		case p.cur.Type == TRBrace:
			p.advance()
			decls = takeFrom(p.declStack, base)
			p.declStack = p.declStack[:base]
			return decls, boxes
		case p.cur.Type == TAtKeyword:
			if r, ok := p.parseNestedAtRule(margins); ok {
				boxes = append(boxes, r)
			}
		default:
			start := len(p.arena)
			p.collect(stopDecl)
			if d, ok := p.makeDecl(start); ok {
				p.declStack = pushDouble(p.declStack, d)
			}
		}
	}
}

// parseNestedAtRule handles an at-rule inside a declaration block: a margin
// box when allowed, otherwise it is skipped with a warning.
func (p *parser) parseNestedAtRule(margins bool) (Rule, bool) {
	name := lowerASCII(p.cur.Text)
	p.advance()
	start := len(p.arena)
	p.collect(stopAtRuleNested)
	p.arena = p.arena[:start]
	switch {
	case !p.peek():
	case p.cur.Type == TSemicolon:
		p.advance()
	case p.cur.Type == TLBrace && margins && marginBoxes[name]:
		p.advance()
		decls, _ := p.parseDeclBlock(false)
		return Rule{Kind: RuleMarginBox, Name: name, Decls: decls}, true
	case p.cur.Type == TLBrace:
		p.skipBlock(false)
	}
	p.b.Warn(WarnUnsupportedAtRule, "@"+short(name))
	return Rule{}, false
}

func (p *parser) parseRules(top bool) []Rule {
	base := len(p.ruleStack)
	for {
		switch {
		case !p.peek():
			if !top {
				p.b.Warn(WarnInvalidValue, detailUnclosedBlock)
			}
			return p.takeRules(base)
		case p.cur.Type == TWhitespace || (top && (p.cur.Type == TCDO || p.cur.Type == TCDC)):
			p.advance()
		case p.cur.Type == TRBrace && !top:
			p.advance()
			return p.takeRules(base)
		case p.cur.Type == TAtKeyword:
			if r, ok := p.parseAtRule(!top); ok {
				p.ruleStack = pushDouble(p.ruleStack, r)
			}
		default:
			if r, ok := p.parseQualified(!top); ok {
				p.ruleStack = pushDouble(p.ruleStack, r)
			}
		}
	}
}

func (p *parser) takeRules(base int) []Rule {
	rules := takeFrom(p.ruleStack, base)
	p.ruleStack = p.ruleStack[:base]
	return rules
}

func (p *parser) parseQualified(nested bool) (Rule, bool) {
	start := len(p.arena)
	if nested {
		p.collect(stopQualifiedNested)
	} else {
		p.collect(stopQualified)
	}
	deep := p.deep
	pa, pb := p.prelude(start)
	if !p.peek() || p.cur.Type != TLBrace {
		p.arena = p.arena[:start]
		p.b.Warn(WarnInvalidValue, detailRuleWithoutBlock)
		return Rule{}, false
	}
	p.advance()
	decls, _ := p.parseDeclBlock(false)
	if deep {
		p.arena = p.arena[:start]
		p.b.Warn(WarnInvalidValue, detailNesting+" in selector")
		return Rule{}, false
	}
	if len(decls) == 0 {
		p.arena = p.arena[:start]
		return Rule{}, false
	}
	return Rule{Kind: RuleStyle, Decls: decls, pa: pa, pb: pb}, true
}

// parseAtRule handles an at-rule of the top level or of an @media body.
func (p *parser) parseAtRule(nested bool) (Rule, bool) {
	first := p.atBeg
	name := lowerASCII(p.cur.Text)
	p.advance()
	start := len(p.arena)
	if nested {
		p.collect(stopAtRuleNested)
	} else {
		p.collect(stopAtRule)
	}
	deep := p.deep
	pa, pb := p.prelude(start)
	hasBlock := p.peek() && p.cur.Type == TLBrace
	if p.have && !hasBlock && p.cur.Type == TSemicolon {
		p.advance()
	}
	drop := func(code, detail string) (Rule, bool) {
		p.arena = p.arena[:start]
		p.b.Warn(code, detail)
		return Rule{}, false
	}
	switch name {
	case "media", "page":
		return p.parseBlockRule(name, hasBlock, deep, start, pa, pb)
	case "charset":
		if hasBlock {
			p.skipBlock(false)
		}
		if first {
			p.arena = p.arena[:start]
			return Rule{}, false
		}
		return drop(WarnUnsupportedAtRule, "@charset")
	}
	if hasBlock {
		p.skipBlock(false)
	}
	return drop(WarnUnsupportedAtRule, "@"+short(name))
}

// parseBlockRule parses the block of an @media or @page rule; the current token
// is its "{" when hasBlock.
func (p *parser) parseBlockRule(name string, hasBlock, deep bool, start int, pa, pb int32) (Rule, bool) {
	drop := func(detail string) (Rule, bool) {
		p.arena = p.arena[:start]
		p.b.Warn(WarnInvalidValue, detail)
		return Rule{}, false
	}
	switch {
	case !hasBlock:
		return drop("@" + name + " without a block")
	case deep:
		p.skipBlock(false)
		return drop(detailNesting + " in @" + name)
	}
	p.advance()
	if name == "page" {
		decls, boxes := p.parseDeclBlock(true)
		return Rule{Kind: RulePage, Decls: decls, Rules: boxes, pa: pa, pb: pb}, true
	}
	if p.mediaDepth >= MaxNesting {
		p.skipBlock(true)
		return drop(detailNesting + " in @media")
	}
	p.mediaDepth++
	body := p.parseRules(false)
	p.mediaDepth--
	return Rule{Kind: RuleMedia, Rules: body, pa: pa, pb: pb}, true
}

// finish points every Prelude and Value at the final token arena.
func (p *parser) finish(rules []Rule) {
	for i := range rules {
		r := &rules[i]
		r.Prelude = p.arena[r.pa:r.pb:r.pb]
		for j := range r.Decls {
			d := &r.Decls[j]
			d.Value = p.arena[d.va:d.vb:d.vb]
		}
		p.finish(r.Rules)
	}
}

// ParseStylesheet parses src. It charges len(src) to CSSBytes and one unit of
// Work per lexer token, and returns only budget errors: malformed input is
// dropped, each dropped rule or declaration recorded with b.Warn. Preludes are
// raw tokens; selectors and media queries are parsed by the callers. Empty
// style rules are omitted.
func ParseStylesheet(src []byte, origin Origin, b *budget.Budget) (*Stylesheet, error) {
	p, err := newParser(src, b)
	if err != nil {
		return nil, err
	}
	rules := p.parseRules(true)
	if p.err != nil {
		return nil, p.err
	}
	p.finish(rules)
	return &Stylesheet{Origin: origin, Rules: rules, tokens: p.arena}, nil
}

// ParseDeclarations parses a declaration list such as a style attribute,
// charging len(src) to CSSBytes and one unit of Work per lexer token.
func ParseDeclarations(src []byte, b *budget.Budget) ([]Declaration, error) {
	p, err := newParser(src, b)
	if err != nil {
		return nil, err
	}
	for p.peek() {
		if p.cur.Type == TWhitespace || p.cur.Type == TSemicolon {
			p.advance()
			continue
		}
		start := len(p.arena)
		p.collect(stopDeclTop)
		if d, ok := p.makeDecl(start); ok {
			p.declStack = pushDouble(p.declStack, d)
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	decls := takeFrom(p.declStack, 0)
	for i := range decls {
		d := &decls[i]
		d.Value = p.arena[d.va:d.vb:d.vb]
	}
	return decls, nil
}
