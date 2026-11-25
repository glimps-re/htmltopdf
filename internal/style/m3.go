package style

import (
	"math"
	"slices"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// listCut is the n of a counter-reset, counter-increment or content spec whose
// list was cut at its bound.
const listCut = 1

var positionKeywords = map[string]int32{
	"static": int32(PositionStatic), "relative": int32(PositionRelative), "absolute": int32(PositionAbsolute),
	"fixed": int32(PositionFixed),
}

func parseM3Longhand(p prop, toks []css.Token) (spec, bool) {
	switch p {
	case pPosition:
		return keyword(toks, positionKeywords)
	case pTop, pRight, pBottom, pLeft:
		return parseLengthPercentage(toks, true, true)
	case pZIndex:
		return parseZIndex(toks)
	case pHeight:
		return parseHeight(toks)
	case pMinWidth:
		return parseMinWidth(toks)
	case pTransform:
		return parseTransform(toks)
	case pPage:
		return parsePage(toks)
	case pElementContent:
		return parseElementContent(toks)
	case pCounterReset:
		return parseCounters(toks, 0, false)
	case pCounterIncrement:
		return parseCounters(toks, 1, true)
	case pColumnCount:
		return parseColumnCount(toks)
	case pColumnWidth:
		// column-width is not supported (Q-M4-2): only auto, its initial value, parses.
		kw, ok := ident(toks)
		return spec{}, ok && kw == "auto"
	case pColumnRuleWidth:
		return parseBorderWidth(toks)
	case pColumnRuleStyle:
		return parseBorderStyle(toks)
	case pColumnRuleColor:
		return parseColorSpec(toks)
	case pTextDecorationThickness:
		return parseDecorationThickness(toks)
	}
	return spec{}, false
}

// parseVerticalAlign parses a keyword or a length; percentages are not supported.
func parseVerticalAlign(toks []css.Token) (spec, bool) {
	if s, ok := keyword(toks, verticalAlignKeywords); ok {
		return s, true
	}
	l, ok := css.ParseLength(trim(toks))
	if !ok || l.Unit == css.UnitPercent {
		return spec{}, false
	}
	return spec{len: l, n: int32(VAlignLength)}, true
}

func parseZIndex(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok && kw == "auto" {
		return spec{auto: true}, true
	}
	if n, ok := integer(toks); ok {
		return spec{n: n}, true
	}
	return spec{}, false
}

// integer parses a lone integer token, saturated to int32.
func integer(toks []css.Token) (int32, bool) {
	if len(toks) != 1 || toks[0].Type != css.TNumber || toks[0].Flags&css.FlagInt == 0 {
		return 0, false
	}
	return saturate(float64(toks[0].Num)), true
}

func saturate(v float64) int32 {
	return int32(math.Max(math.MinInt32, math.Min(math.MaxInt32, v)))
}

// parseHeight parses auto or a non-negative length or percentage.
func parseHeight(toks []css.Token) (spec, bool) {
	return parseLengthPercentage(toks, true, false)
}

func parseMinWidth(toks []css.Token) (spec, bool) {
	return parseLengthPercentage(toks, true, false)
}

// parseTransform parses none or a single rotate(<angle>).
func parseTransform(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok && kw == "none" {
		return spec{none: true}, true
	}
	if len(toks) < 2 || toks[0].Type != css.TFunction || !eqFold(toks[0].Text, "rotate") {
		return spec{}, false
	}
	args := toks[1:]
	if args[len(args)-1].Type == css.TRParen {
		args = args[:len(args)-1]
	}
	args = trim(args)
	if len(args) != 1 {
		return spec{}, false
	}
	deg, ok := css.ParseAngle(args[0])
	if !ok {
		return spec{}, false
	}
	return spec{f: deg * math.Pi / 180}, true
}

// customIdent returns the text of a lone identifier that is a valid custom
// identifier: not a CSS-wide keyword, none or auto, at most MaxIdentBytes.
func customIdent(toks []css.Token) (string, bool) {
	toks = trim(toks)
	if len(toks) != 1 || toks[0].Type != css.TIdent || len(toks[0].Text) > MaxIdentBytes {
		return "", false
	}
	switch lowerASCII(toks[0].Text) {
	case "inherit", "initial", "unset", "default", "revert", "revert-layer", "none", "auto":
		return "", false
	}
	return toks[0].Text, true
}

func parsePage(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok && kw == "auto" {
		return spec{auto: true}, true
	}
	name, ok := customIdent(toks)
	if !ok {
		return spec{}, false
	}
	return spec{list: []string{name}}, true
}

// parseCounters parses none or "[<ident> <integer>?]+"; def is the value of an
// entry without integer. A name listed twice keeps one entry, at the position of
// the first: the values are summed when sum is true (counter-increment), else
// the last wins (counter-reset), as in Chrome.
func parseCounters(toks []css.Token, def int32, sum bool) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok && kw == "none" {
		return spec{none: true}, true
	}
	var ops []CounterOp
	var cut int32
	fs := fields(toks)
	if len(fs) > maxParts {
		fs, cut = fs[:maxParts], listCut
	}
	for i := 0; i < len(fs); i++ {
		name, ok := customIdent(fs[i])
		if !ok {
			return spec{}, false
		}
		v := def
		if i+1 < len(fs) {
			if n, isInt := integer(fs[i+1]); isInt {
				v = n
				i++
			}
		}
		if j := slices.IndexFunc(ops, func(o CounterOp) bool { return o.Name == name }); j >= 0 {
			if sum {
				v = saturate(float64(ops[j].Value) + float64(v))
			}
			ops[j].Value = v
			continue
		}
		if len(ops) == MaxCounterOps {
			cut = listCut
			continue
		}
		ops = append(ops, CounterOp{Name: name, Value: v})
	}
	if len(ops) == 0 {
		return spec{}, false
	}
	return spec{counters: ops, n: cut}, true
}

// parseElementContent parses the content of a pseudo-element: none, normal, or
// strings and counter(<ident>[, decimal | decimal-leading-zero]). Every other
// form (attr(), counters(), leader(), target-*(), url(), quotes) makes the
// declaration invalid, as in Chrome.
func parseElementContent(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok && (kw == "none" || kw == "normal") {
		return spec{none: true}, true
	}
	var items []ContentItem
	var cut int32
	// Items need no whitespace between them: "a"counter(c) is valid.
	for i := 0; i < len(toks); {
		t := toks[i]
		if t.Type == css.TWhitespace {
			i++
			continue
		}
		end := skip(toks, i)
		switch {
		case t.Type == css.TString:
			items = append(items, ContentItem{Kind: ContentText, Text: dom.SanitizeText(t.Text)})
		case t.Type == css.TFunction && eqFold(t.Text, "counter"):
			it, ok := counterContent(toks[i:end])
			if !ok {
				return spec{}, false
			}
			items = append(items, it)
		default:
			return spec{}, false
		}
		i = end
		if len(items) > maxContentItems {
			items, cut = items[:maxContentItems], listCut
			break
		}
	}
	return spec{items: items, n: cut}, len(items) > 0
}

func counterContent(f []css.Token) (ContentItem, bool) {
	args := f[1:]
	if n := len(args); n > 0 && args[n-1].Type == css.TRParen {
		args = args[:n-1]
	}
	parts := splitOn(args, isComma)
	if len(parts) < 1 || len(parts) > 2 {
		return ContentItem{}, false
	}
	name, ok := customIdent(parts[0])
	if !ok {
		return ContentItem{}, false
	}
	it := ContentItem{Kind: ContentCounter, Text: name}
	if len(parts) == 2 {
		kw, ok := ident(parts[1])
		switch {
		case ok && kw == "decimal":
		case ok && kw == "decimal-leading-zero":
			it.Format = CounterDecimalLeadingZero
		default:
			return ContentItem{}, false
		}
	}
	return it, true
}

func (x *computeCtx) applyM3(p prop, s *spec) {
	c := x.c
	switch p {
	case pPosition:
		c.Position = Position(u8(s.n))
	case pTop, pRight, pBottom, pLeft:
		c.Inset[p-pTop] = x.lengthOrAuto(s)
	case pZIndex:
		c.ZIndex, c.ZAuto = 0, s.auto
		if !s.auto {
			c.ZIndex = s.n
		}
	case pHeight:
		c.Height = x.lengthOrAuto(s)
	case pMinWidth:
		c.MinWidth = Dim{Kind: DimAuto}
		if !s.auto {
			c.MinWidth = x.dim(s.len, c.FontSize)
		}
	case pTransform:
		c.Rotate = 0
		if !s.none {
			c.Rotate = s.f
		}
	case pPage:
		c.Page = ""
		if len(s.list) == 1 {
			c.Page = s.list[0]
		}
	case pElementContent:
		c.Content = nil
		if x.pseudo && !s.none {
			c.Content = s.items
		}
	case pCounterReset:
		c.CounterReset = s.counters
	case pCounterIncrement:
		c.CounterIncrement = s.counters
	case pColumnCount:
		c.ColumnCount = 0
		if !s.auto {
			c.ColumnCount = int(s.n)
		}
	case pColumnRuleWidth:
		c.ColumnRule.Width = snapBorderWidth(x.px(s.len))
	case pColumnRuleStyle:
		c.ColumnRule.Style = BorderStyle(u8(s.n))
	case pColumnRuleColor:
		x.ruleSet, x.ruleCur = true, s.cur
		c.ColumnRule.Color = s.color
	case pTextDecorationThickness:
		c.TextDecorationThickness = Dim{Kind: DimAuto}
		if !s.auto {
			c.TextDecorationThickness = x.dim(s.len, c.FontSize)
		}
	}
}

// parseColumnCount parses auto or a positive integer; one above MaxColumns is
// clamped (spec.f is 1, WarnClamped "columns").
func parseColumnCount(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok && kw == "auto" {
		return spec{auto: true}, true
	}
	n, ok := integer(toks)
	if !ok || n < 1 {
		return spec{}, false
	}
	if n > MaxColumns {
		return spec{n: MaxColumns, f: 1}, true
	}
	return spec{n: n}, true
}

// parseColumnsShorthand parses "columns": auto or an integer, alone or with
// auto. A length (column-width) is invalid (Q-M4-2).
func parseColumnsShorthand(toks []css.Token) (spec, bool) {
	fs := fields(trim(toks))
	if len(fs) < 1 || len(fs) > 2 {
		return spec{}, false
	}
	out := spec{auto: true}
	for _, f := range fs {
		s, ok := parseColumnCount(f)
		if !ok {
			return spec{}, false
		}
		if !s.auto {
			if !out.auto {
				return spec{}, false
			}
			out = s
		}
	}
	return out, true
}

// columnRuleShorthand parses column-rule like a border: width, style and colour.
func columnRuleShorthand(props []prop, toks []css.Token) ([]propSpec, bool) {
	vals, ok := parseWidthStyleColor(toks)
	if !ok {
		return nil, false
	}
	return []propSpec{{props[0], vals[0]}, {props[1], vals[1]}, {props[2], vals[2]}}, true
}

// parseDecorationThickness parses auto, from-font (the same), a length or a
// percentage of the font size.
func parseDecorationThickness(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok {
		return spec{auto: true}, kw == "auto" || kw == "from-font"
	}
	l, ok := css.ParseLength(toks)
	if !ok || l.Value < 0 {
		return spec{}, false
	}
	return spec{len: l}, true
}
