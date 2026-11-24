package style

import (
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

// decl is a compiled declaration.
type decl struct {
	important bool
	// custom marks a --custom property; name is its name.
	custom bool
	name   string
	// kw is inherit, initial or unset for a custom property that says so.
	kw specKind
	// deferred marks a value with var(): it is parsed after substitution, from
	// toks. ctx tells which properties are valid.
	deferred bool
	ctx      declCtx
	toks     []css.Token
	// specs holds the typed specified values of an ordinary declaration.
	specs []propSpec
}

// crule is a compiled style rule: its selectors, declarations and the media
// queries that enclose it.
type crule struct {
	sels  []*css.Selector
	decls []decl
	media []css.MediaQuery
}

// cpage is a compiled @page rule.
type cpage struct {
	decls []decl
	boxes map[string][]decl
	media []css.MediaQuery
}

// Sheet is a compiled stylesheet: immutable, and shared by concurrent renders.
type Sheet struct {
	origin css.Origin
	rules  []crule
	pages  []cpage
}

// Origin returns the origin the sheet was compiled for.
func (s *Sheet) Origin() css.Origin { return s.origin }

// supportedMarginBoxes are the margin boxes M1 draws, by name.
var supportedMarginBoxes = func() map[string]bool {
	m := make(map[string]bool, len(marginBoxNames))
	for _, n := range marginBoxNames {
		m[n] = true
	}
	return m
}()

// Compile compiles ss. Selectors are charged to the Selectors limit of b and
// every dropped declaration, selector or at-rule is recorded as a warning of b,
// so that a caller that must be strict (the UA sheet, ExtraCSS) only checks
// that b holds none. The error is a budget error.
func Compile(ss *css.Stylesheet, b *budget.Budget) (*Sheet, error) {
	c := compiler{b: b, sheet: &Sheet{origin: ss.Origin}}
	if err := c.rules(ss.Rules, nil); err != nil {
		return nil, err
	}
	return c.sheet, nil
}

type compiler struct {
	b     *budget.Budget
	sheet *Sheet
}

func (c *compiler) rules(rules []css.Rule, media []css.MediaQuery) error {
	for i := range rules {
		r := &rules[i]
		switch r.Kind {
		case css.RuleStyle:
			if err := c.styleRule(r, media); err != nil {
				return err
			}
		case css.RuleMedia:
			q := css.ParseMediaQuery(r.Prelude, c.b)
			if err := c.rules(r.Rules, append(media[:len(media):len(media)], q)); err != nil {
				return err
			}
		case css.RulePage:
			c.pageRule(r, media)
		}
	}
	return nil
}

func (c *compiler) styleRule(r *css.Rule, media []css.MediaQuery) error {
	sels, err := css.ParseSelectors(r.Prelude, c.b)
	if err != nil {
		return err
	}
	// Rules for ::before and ::after wait for M3-01.
	kept := sels[:0]
	for _, s := range sels {
		if s.Pseudo() == css.PseudoNone {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	decls := c.decls(r.Decls, ctxElement)
	if len(decls) == 0 {
		return nil
	}
	c.sheet.rules = append(c.sheet.rules, crule{sels: kept, decls: decls, media: media})
	return nil
}

func (c *compiler) pageRule(r *css.Rule, media []css.MediaQuery) {
	if len(trim(r.Prelude)) != 0 {
		c.b.Warn(budget.WarnUnsupportedAtRule, "@page selector")
		return
	}
	p := cpage{decls: c.decls(r.Decls, ctxPage), media: media}
	for i := range r.Rules {
		box := &r.Rules[i]
		if box.Kind != css.RuleMarginBox {
			continue
		}
		if !supportedMarginBoxes[box.Name] {
			c.b.Warn(budget.WarnUnsupportedAtRule, "@"+box.Name)
			continue
		}
		if p.boxes == nil {
			p.boxes = make(map[string][]decl)
		}
		p.boxes[box.Name] = append(p.boxes[box.Name], c.decls(box.Decls, ctxMarginBox)...)
	}
	c.sheet.pages = append(c.sheet.pages, p)
}

// decls compiles declarations.
func (c *compiler) decls(in []css.Declaration, ctx declCtx) []decl {
	out := make([]decl, 0, len(in))
	for i := range in {
		if d, ok := c.decl(&in[i], ctx); ok {
			out = append(out, d)
		}
	}
	return out
}

func (c *compiler) decl(in *css.Declaration, ctx declCtx) (decl, bool) {
	name := in.Name
	if len(name) > 2 && name[:2] == "--" {
		if ctx != ctxElement {
			c.b.Warn(budget.WarnUnsupportedProperty, name)
			return decl{}, false
		}
		d := decl{important: in.Important, custom: true, name: name, toks: trim(in.Value)}
		if kw, ok := ident(d.toks); ok {
			switch kw {
			case "inherit":
				d.kw = specInherit
			case "initial":
				d.kw = specInitial
			case "unset":
				d.kw = specUnset
			}
		}
		return d, true
	}
	name = lowerASCII(name)
	if hasVar(in.Value) {
		if ctx != ctxElement {
			// var() needs the custom properties of an element: it is invalid
			// here, and never silently.
			c.b.Warn(budget.WarnInvalidValue, name+": "+describe(in.Value))
			return decl{}, false
		}
		if _, ok := propsOf(name, ctx); !ok {
			c.b.Warn(budget.WarnUnsupportedProperty, name)
			return decl{}, false
		}
		return decl{important: in.Important, name: name, deferred: true, ctx: ctx, toks: in.Value}, true
	}
	specs, st := parseDecl(name, in.Value, ctx)
	switch st {
	case parseUnsupported:
		c.b.Warn(budget.WarnUnsupportedProperty, name)
		return decl{}, false
	case parseInvalid:
		c.b.Warn(budget.WarnInvalidValue, name+": "+describe(in.Value))
		return decl{}, false
	}
	warnTruncated(c.b, specs)
	if ctx == ctxElement {
		warnClampedLength(c.b, in.Value)
	}
	return decl{important: in.Important, name: name, specs: specs}, true
}

// warnTruncated records the font-family lists cut at MaxFontFamilies and the
// track lists cut at MaxGridTracks.
func warnTruncated(b *budget.Budget, specs []propSpec) {
	for i := range specs {
		switch {
		case specs[i].prop == pFontFamily && specs[i].spec.n == familiesCut:
			b.Warn(budget.WarnClamped, "font-family")
		case specs[i].prop == pGridColumns && specs[i].spec.n == gridCut:
			b.Warn(budget.WarnClamped, "grid tracks")
		}
	}
}

// warnClampedLength records a length of the declaration value that parsing
// clamped to ±css.MaxLengthPx.
func warnClampedLength(b *budget.Budget, toks []css.Token) {
	if css.HasClampedLength(toks) {
		b.Warn(budget.WarnClamped, "length beyond 1e7 px")
	}
}
