package style

import "github.com/glimps-re/htmltopdf/internal/css"

// shadowSpec is one outer shadow of a box-shadow value before resolution.
type shadowSpec struct {
	x, y, blur, spread css.Length
	color              css.Color
	cur                bool // currentcolor, also the default colour
}

// shadowList is a parsed box-shadow value: the outer shadows kept, and what
// parsing left out so that the caller can warn once per declaration.
type shadowList struct {
	shadows []shadowSpec
	inset   bool // an inset shadow was dropped
	clamped bool // the list was cut at MaxShadows
}

// parseBoxShadow parses none or a comma-separated list of shadows,
// inset? && <length>{2,4} && <color>?: the lengths are contiguous, the colour
// and inset come before or after them. Inset shadows are dropped, and a list longer than MaxShadows
// keeps its first MaxShadows. A negative blur, a percentage or a length that
// is not contiguous makes the value invalid.
func parseBoxShadow(toks []css.Token) (shadowList, bool) {
	if kw, ok := ident(toks); ok && kw == "none" {
		return shadowList{}, true
	}
	var out shadowList
	for _, part := range splitOn(toks, isComma) {
		s, inset, ok := parseOneShadow(part)
		if !ok {
			return shadowList{}, false
		}
		switch {
		case inset:
			out.inset = true
		case len(out.shadows) == MaxShadows:
			out.clamped = true
		default:
			out.shadows = append(out.shadows, s)
		}
	}
	return out, true
}

func parseOneShadow(toks []css.Token) (s shadowSpec, inset, ok bool) {
	fs := fields(trim(toks))
	if len(fs) < 2 {
		return s, false, false
	}
	var lengths []css.Length
	s.cur = true
	haveColor, lengthsDone := false, false
	for _, f := range fs {
		if kw, isKw := ident(f); isKw && kw == "inset" {
			if inset {
				return s, false, false
			}
			inset = true
			lengthsDone = lengthsDone || len(lengths) > 0
			continue
		}
		if l, isLen := css.ParseLength(f); isLen {
			if lengthsDone || l.Unit == css.UnitPercent || len(lengths) == 4 {
				return s, false, false
			}
			lengths = append(lengths, l)
			continue
		}
		cs, isColor := parseColorSpec(f)
		if !isColor || haveColor {
			return s, false, false
		}
		haveColor = true
		lengthsDone = lengthsDone || len(lengths) > 0
		s.color, s.cur = cs.color, cs.cur
	}
	if len(lengths) < 2 || (len(lengths) > 2 && lengths[2].Value < 0) {
		return s, false, false
	}
	s.x, s.y = lengths[0], lengths[1]
	if len(lengths) > 2 {
		s.blur = lengths[2]
	}
	if len(lengths) > 3 {
		s.spread = lengths[3]
	}
	return s, inset, true
}

// resolveShadows returns the used shadows of the style being computed: lengths
// in px, currentcolor taken from the un-faded colour, and the alpha of every
// colour multiplied by the opacity in effect. It must run after opacity is
// applied. The result is nil for none.
func (x *computeCtx) resolveShadows(l []shadowSpec) []Shadow {
	if len(l) == 0 {
		return nil
	}
	keep := 1 - x.c.fade
	out := make([]Shadow, len(l))
	for i := range l {
		col := l[i].color
		if l[i].cur {
			col = x.c.rawColor()
		}
		col.A *= keep
		out[i] = Shadow{X: x.px(l[i].x), Y: x.px(l[i].y), Blur: x.px(l[i].blur), Spread: x.px(l[i].spread), Color: col}
	}
	return out
}
