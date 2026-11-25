package style

import (
	"encoding/binary"
	"hash/maphash"
	"slices"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// Cascade levels of design §9.1, from the lowest: the UA sheet, the document
// (normal declarations, then its !important ones) and ExtraCSS.
const (
	levelUA uint8 = iota
	levelDocument
	levelExtra
)

type ruleRef struct {
	rule  *crule
	order uint32
	level uint8
	// pseudo is the pseudo-element of the selector that indexed the rule.
	pseudo css.PseudoElement
}

type matched struct {
	ref  ruleRef
	spec css.Specificity
}

type anonKey struct {
	parent  *Computed
	display Display
}

type cacheKey struct {
	parent *Computed
	hash   uint64
}

// Resolver computes the styles of one render. It is not safe for concurrent
// use; the compiled sheets it reads are.
type Resolver struct {
	b         *budget.Budget
	env       css.MediaEnv
	ix        css.Index[ruleRef]
	pix       css.Index[ruleRef] // selectors of pseudo-elements
	hasPseudo bool
	pcache    map[cacheKey]*Computed
	pages     []pageEntry            // rules without a name
	named     map[string][]pageEntry // rules by page name
	pageStyle *PageStyle
	pageNames map[string]*PageStyle
	paper     [2]float64
	pageOv    map[string]*pageOverlay
	cache     map[cacheKey]*Computed
	anon      map[anonKey]*Computed
	seed      maphash.Seed

	cands   []css.Entry[ruleRef]
	matched []matched
	seq     []*decl
	bk      buckets
	specs   [numProps]spec
	touched []prop
	grid    gridCache
}

// NewResolver builds the rule index of the render: ua and extra are compiled
// once by the caller, doc are the document stylesheets in order. Rules inside a
// @media that does not match env are left out. b is the budget of the render.
func NewResolver(ua, extra *Sheet, doc []*Sheet, env css.MediaEnv, b *budget.Budget) *Resolver {
	r := &Resolver{b: b, env: env, cache: make(map[cacheKey]*Computed), seed: maphash.MakeSeed()}
	order := uint32(0)
	add := func(s *Sheet, level uint8) {
		if s == nil {
			return
		}
		for i := range s.rules {
			rule := &s.rules[i]
			order++
			if !r.mediaMatches(rule.media) {
				continue
			}
			for _, sel := range rule.sels {
				if sel.Pseudo() != css.PseudoNone {
					r.pix.Add(sel, ruleRef{rule: rule, order: order, level: level, pseudo: sel.Pseudo()})
					r.hasPseudo = true
					continue
				}
				r.ix.Add(sel, ruleRef{rule: rule, order: order, level: level})
			}
		}
		r.addPages(s, level)
	}
	add(ua, levelUA)
	for _, s := range doc {
		add(s, levelDocument)
	}
	add(extra, levelExtra)
	return r
}

func (r *Resolver) mediaMatches(media []css.MediaQuery) bool {
	for _, q := range media {
		if !q.Match(r.env) {
			return false
		}
	}
	return true
}

// unsupportedElements are the elements that are never loaded or drawn.
var unsupportedElements = map[string]bool{
	"img": true, "link": true, "iframe": true, "object": true, "embed": true, "video": true,
	"audio": true, "canvas": true, "input": true, "textarea": true, "select": true, "button": true, "base": true,
}

// Element returns the computed style of element e of d; parent is the computed
// style of its parent element, nil for the root. Elements whose parent,
// matching rules and style attribute are the same share one *Computed. A new
// style is charged to the Styles limit; the error is a budget error.
func (r *Resolver) Element(d *dom.Document, e int32, parent *Computed) (*Computed, error) {
	el := &d.Elements[e]
	if el.NS == dom.NSHTML && el.Tag != 0 && unsupportedElements[el.Tag.String()] {
		r.b.Warn(budget.WarnUnsupportedElement, el.Tag.String())
	}
	m, err := r.match(d, e)
	if err != nil {
		return nil, err
	}
	var h maphash.Hash
	h.SetSeed(r.seed)
	var buf [4]byte
	for i := range m {
		binary.LittleEndian.PutUint32(buf[:], m[i].ref.order)
		_, _ = h.Write(buf[:])
	}
	_ = h.WriteByte(0xff)
	_, _ = h.WriteString(el.Style)
	key := cacheKey{parent: parent, hash: h.Sum64()}
	if c, ok := r.cache[key]; ok {
		return c, nil
	}
	if err := r.b.Charge(budget.Styles, 1); err != nil {
		return nil, err
	}
	var inline []decl
	if el.Style != "" {
		ds, err := css.ParseDeclarations([]byte(el.Style), r.b)
		if err != nil {
			return nil, err
		}
		inline = (&compiler{b: r.b}).decls(ds, ctxElement)
	}
	c, err := r.build(parent, m, inline)
	if err != nil {
		return nil, err
	}
	r.cache[key] = c
	return c, nil
}

// Anonymous returns the style of an anonymous box of the given display under
// parent: the inherited properties of parent, every other property initial. A
// new style is charged to the Styles limit; boxes with the same parent and
// display share one.
func (r *Resolver) Anonymous(parent *Computed, display Display) (*Computed, error) {
	key := anonKey{parent: parent, display: display}
	if c, ok := r.anon[key]; ok {
		return c, nil
	}
	if err := r.b.Charge(budget.Styles, 1); err != nil {
		return nil, err
	}
	r.touched = r.touched[:0]
	r.set(pDisplay, spec{kind: specValue, n: int32(display)})
	c, err := compute(parent, &r.specs, r.touched, r.b, nil)
	if err != nil {
		return nil, err
	}
	if parent != nil {
		c.vars = parent.vars
	}
	for _, p := range r.touched {
		r.specs[p] = spec{}
	}
	if r.anon == nil {
		r.anon = make(map[anonKey]*Computed)
	}
	r.anon[key] = c
	return c, nil
}

// match returns the rules matching e, ordered by specificity then source order.
func (r *Resolver) match(d *dom.Document, e int32) ([]matched, error) {
	return r.matchIn(&r.ix, d, e, css.PseudoNone)
}

// matchIn is match over the selectors of ix that target pe.
func (r *Resolver) matchIn(ix *css.Index[ruleRef], d *dom.Document, e int32, pe css.PseudoElement) ([]matched, error) {
	var err error
	if r.cands, err = ix.Candidates(d, e, r.cands[:0], r.b); err != nil {
		return nil, err
	}
	r.matched = r.matched[:0]
	for i := range r.cands {
		if r.cands[i].Selector.Pseudo() != pe {
			continue
		}
		ok, err := r.cands[i].Selector.Match(d, e, r.b)
		if err != nil {
			return nil, err
		}
		if ok {
			r.matched = append(r.matched, matched{ref: r.cands[i].Value, spec: r.cands[i].Selector.Specificity()})
		}
	}
	// UA rules come before document rules whatever their specificity (§9.1).
	slices.SortFunc(r.matched, func(a, b matched) int {
		switch {
		case a.ref.level != b.ref.level:
			if a.ref.level < b.ref.level {
				return -1
			}
			return 1
		case a.spec != b.spec:
			if a.spec < b.spec {
				return -1
			}
			return 1
		case a.ref.order < b.ref.order:
			return -1
		case a.ref.order > b.ref.order:
			return 1
		}
		return 0
	})
	return r.matched, nil
}

// Pseudo returns the computed style of the ::before or ::after box of element e,
// whose own style is elem, or nil when the element generates no such box: no rule
// gives it a content, the content is none or normal, or the pseudo-element or
// its element is display: none. A pseudo-element inherits
// from its element. A new style is charged to the Styles limit.
func (r *Resolver) Pseudo(d *dom.Document, e int32, pe css.PseudoElement, elem *Computed) (*Computed, error) {
	if !r.hasPseudo || elem == nil || elem.Display == DisplayNone {
		return nil, nil
	}
	m, err := r.matchIn(&r.pix, d, e, pe)
	if err != nil || len(m) == 0 {
		return nil, err
	}
	var h maphash.Hash
	h.SetSeed(r.seed)
	var buf [4]byte
	for i := range m {
		binary.LittleEndian.PutUint32(buf[:], m[i].ref.order)
		_, _ = h.Write(buf[:])
	}
	_ = h.WriteByte(byte(pe))
	// As in Element, the key is the parent style and a 64-bit hash with a
	// per-render random seed: a collision cannot be chosen by the document.
	key := cacheKey{parent: elem, hash: h.Sum64()}
	c, ok := r.pcache[key]
	if !ok {
		if err := r.b.Charge(budget.Styles, 1); err != nil {
			return nil, err
		}
		if c, err = r.buildFor(elem, m, nil, true); err != nil {
			return nil, err
		}
		if r.pcache == nil {
			r.pcache = make(map[cacheKey]*Computed)
		}
		r.pcache[key] = c
	}
	if c.Content == nil || c.Display == DisplayNone {
		return nil, nil
	}
	return c, nil
}

// build computes a style from the matched rules and the style attribute.
func (r *Resolver) build(parent *Computed, m []matched, inline []decl) (*Computed, error) {
	return r.buildFor(parent, m, inline, false)
}

func (r *Resolver) buildFor(parent *Computed, m []matched, inline []decl, pseudo bool) (*Computed, error) {
	seq := r.sequence(m, inline)
	if err := r.b.Charge(budget.Work, int64(len(seq))); err != nil {
		return nil, err
	}
	var parentVars *varSet
	if parent != nil {
		parentVars = parent.vars
	}
	pending := map[string]*decl(nil)
	for _, d := range seq {
		if d.custom {
			if pending == nil {
				pending = make(map[string]*decl)
			}
			pending[d.name] = d
		}
	}
	vars, err := r.resolveCustoms(parentVars, pending)
	if err != nil {
		return nil, err
	}
	lookup := func(name string) ([]css.Token, lookupResult, error) {
		if v, ok := vars.get(name); ok {
			return v, varFound, nil
		}
		return nil, varMissing, nil
	}
	r.touched = r.touched[:0]
	defer func() {
		for _, p := range r.touched {
			r.specs[p] = spec{}
		}
	}()
	for _, d := range seq {
		if d.custom {
			continue
		}
		if err := r.applyDecl(d, lookup); err != nil {
			return nil, err
		}
	}
	c, err := computeFor(parent, &r.specs, r.touched, r.b, &r.grid, pseudo)
	if err != nil {
		return nil, err
	}
	c.vars = vars
	return c, nil
}

// buckets order declarations by cascade priority (design §9.1): UA, document
// normal, document !important, ExtraCSS. ExtraCSS has one bucket: its
// declarations count in source order whatever their !important.
type buckets [4][]*decl

func bucketOf(level uint8, important bool) int {
	switch {
	case level == levelUA:
		return 0
	case level == levelExtra:
		return 3
	case important:
		return 2
	}
	return 1
}

func (b *buckets) reset() {
	for i := range b {
		b[i] = b[i][:0]
	}
}

func (b *buckets) add(level uint8, d *decl) {
	i := bucketOf(level, d.important)
	b[i] = append(b[i], d)
}

// flatten returns the declarations from the lowest to the highest priority.
func (b *buckets) flatten(dst []*decl) []*decl {
	dst = dst[:0]
	for i := range b {
		dst = append(dst, b[i]...)
	}
	return dst
}

// sequence orders the declarations of the matched rules (sorted by level, then
// specificity, then source order) and of the style attribute, which comes after
// the rules of its bucket.
func (r *Resolver) sequence(m []matched, inline []decl) []*decl {
	r.bk.reset()
	for i := range m {
		ref := m[i].ref
		for j := range ref.rule.decls {
			r.bk.add(ref.level, &ref.rule.decls[j])
		}
	}
	for i := range inline {
		r.bk.add(levelDocument, &inline[i])
	}
	r.seq = r.bk.flatten(r.seq)
	return r.seq
}

func (r *Resolver) set(p prop, s spec) {
	if r.specs[p].kind == specNone {
		r.touched = append(r.touched, p)
	}
	r.specs[p] = s
}

// applyDecl records the specified values of d.
func (r *Resolver) applyDecl(d *decl, lookup func(string) ([]css.Token, lookupResult, error)) error {
	if !d.deferred {
		for _, ps := range d.specs {
			r.set(ps.prop, ps.spec)
		}
		return nil
	}
	toks, ok, err := r.substitute(d.toks, lookup)
	if err != nil {
		return err
	}
	var specs []propSpec
	if ok {
		var st parseStatus
		if specs, st = parseDecl(d.name, toks, d.ctx); st != parseOK {
			ok = false
		}
	}
	if !ok {
		// Invalid at computed-value time: every longhand behaves as unset.
		props, _ := propsOf(d.name, d.ctx)
		for _, p := range props {
			r.set(p, spec{kind: specUnset})
		}
		return nil
	}
	warnTruncated(r.b, specs)
	warnClampedLength(r.b, toks)
	for _, ps := range specs {
		r.set(ps.prop, ps.spec)
	}
	return nil
}
