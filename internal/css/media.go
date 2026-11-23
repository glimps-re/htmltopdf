package css

import "github.com/glimps-re/htmltopdf/internal/budget"

// MediaEnv describes the medium a document is rendered for: the PDF is always
// paged ("print") and its page area gives the viewport size, in CSS px.
type MediaEnv struct {
	// Print is true for the print medium, false for screen.
	Print bool
	// Width and Height are the viewport size in px.
	Width, Height float64
}

// maxMediaQueries bounds the queries of one list; the rest is dropped with one
// warning, which keeps a hostile list of empty queries from costing a warning
// each.
const maxMediaQueries = 32

// emPx is the em size media queries use: the initial font size (CSS Media
// Queries 4 section 2.3).
const emPx = 16

type mediaType uint8

const (
	mediaAny mediaType = iota
	mediaPrint
	mediaScreen
	// mediaNone is an unknown media type, which matches nothing.
	mediaNone
)

type mediaFeature uint8

const (
	featMinWidth mediaFeature = iota + 1
	featMaxWidth
	featWidth
	featMinHeight
	featMaxHeight
	featHeight
	featOrientation
)

type mediaCondition struct {
	feature mediaFeature
	value   float64 // px, or 1 for portrait and 2 for landscape
}

type mediaAlt struct {
	typ   mediaType
	not   bool
	conds []mediaCondition
	// invalid alternatives never match (CSS Media Queries 4 section 3.2).
	invalid bool
}

// MediaQuery is a parsed media query list. The zero value, like an empty
// prelude, matches every medium.
type MediaQuery struct {
	alts []mediaAlt
}

// ParseMediaQuery parses the prelude of an @media rule: a comma-separated list
// of [not|only] type [and (feature: value)]* or (feature: value) [and ...]*,
// with the types all, print and screen and the features width, min-width,
// max-width, height, min-height, max-height and orientation. A query that is
// malformed or uses anything else evaluates to false, as the specification
// requires, and is reported with an invalid-value warning whose detail is the
// query text; the others of the list still count. An unknown media type is
// valid and matches nothing, without a warning.
func ParseMediaQuery(prelude []Token, b *budget.Budget) MediaQuery {
	prelude = trimSpace(prelude)
	if len(prelude) == 0 {
		return MediaQuery{}
	}
	var q MediaQuery
	for lo, hi := range commaRanges(prelude) {
		if len(q.alts) == maxMediaQueries {
			b.Warn(budget.WarnInvalidValue, "more than 32 media queries")
			break
		}
		part := trimSpace(prelude[lo:hi])
		alt := parseMediaAlt(part)
		if alt.invalid {
			b.Warn(budget.WarnInvalidValue, "media query: "+describe(part))
		}
		q.alts = append(q.alts, alt)
	}
	return q
}

func parseMediaAlt(toks []Token) mediaAlt {
	alt := mediaAlt{typ: mediaAny}
	fs := fields(toks)
	i := 0
	if len(fs) == 0 {
		return mediaAlt{invalid: true}
	}
	if len(fs[0]) == 1 && fs[0][0].Type == TIdent {
		switch lowerASCII(fs[0][0].Text) {
		case "not":
			alt.not = true
			i++
		case "only":
			i++
			if i >= len(fs) || len(fs[i]) != 1 || fs[i][0].Type != TIdent {
				return mediaAlt{invalid: true}
			}
		}
	}
	if i < len(fs) && len(fs[i]) == 1 && fs[i][0].Type == TIdent {
		switch lowerASCII(fs[i][0].Text) {
		case "all":
			alt.typ = mediaAny
		case "print":
			alt.typ = mediaPrint
		case "screen":
			alt.typ = mediaScreen
		default:
			alt.typ = mediaNone
		}
		i++
		if i < len(fs) && !isAnd(fs[i]) {
			return mediaAlt{invalid: true}
		}
		if i < len(fs) {
			i++
		} else {
			return alt
		}
	} else if alt.not || i >= len(fs) {
		return mediaAlt{invalid: true}
	}
	for {
		if i >= len(fs) {
			return mediaAlt{invalid: true}
		}
		cond, ok := parseMediaCondition(fs[i])
		if !ok {
			return mediaAlt{invalid: true}
		}
		alt.conds = append(alt.conds, cond)
		i++
		if i == len(fs) {
			return alt
		}
		if !isAnd(fs[i]) {
			return mediaAlt{invalid: true}
		}
		i++
	}
}

func isAnd(f []Token) bool {
	return len(f) == 1 && f[0].Type == TIdent && lowerASCII(f[0].Text) == "and"
}

func parseMediaCondition(f []Token) (mediaCondition, bool) {
	if len(f) == 0 || f[0].Type != TLParen {
		return mediaCondition{}, false
	}
	inner := f[1:]
	if closed(inner) {
		inner = inner[:len(inner)-1]
	}
	inner = trimSpace(inner)
	if len(inner) == 0 || inner[0].Type != TIdent {
		return mediaCondition{}, false
	}
	var c mediaCondition
	switch lowerASCII(inner[0].Text) {
	case "min-width":
		c.feature = featMinWidth
	case "max-width":
		c.feature = featMaxWidth
	case "width":
		c.feature = featWidth
	case "min-height":
		c.feature = featMinHeight
	case "max-height":
		c.feature = featMaxHeight
	case "height":
		c.feature = featHeight
	case "orientation":
		c.feature = featOrientation
	default:
		return mediaCondition{}, false
	}
	rest := trimSpace(inner[1:])
	if len(rest) == 0 || rest[0].Type != TColon {
		return mediaCondition{}, false
	}
	rest = trimSpace(rest[1:])
	if c.feature == featOrientation {
		if len(rest) != 1 || rest[0].Type != TIdent {
			return mediaCondition{}, false
		}
		switch lowerASCII(rest[0].Text) {
		case "portrait":
			c.value = 1
		case "landscape":
			c.value = 2
		default:
			return mediaCondition{}, false
		}
		return c, true
	}
	l, ok := ParseLength(rest)
	if !ok || l.Unit == UnitPercent || l.Value < 0 {
		return mediaCondition{}, false
	}
	c.value = l.Resolve(emPx, emPx, 0)
	return c, true
}

// Match reports whether the query list matches env.
func (q MediaQuery) Match(env MediaEnv) bool {
	if len(q.alts) == 0 {
		return true
	}
	for i := range q.alts {
		if q.alts[i].match(env) {
			return true
		}
	}
	return false
}

func (a *mediaAlt) match(env MediaEnv) bool {
	if a.invalid {
		return false
	}
	ok := a.typ == mediaAny || a.typ == mediaPrint && env.Print || a.typ == mediaScreen && !env.Print
	for _, c := range a.conds {
		ok = ok && c.match(env)
	}
	return ok != a.not
}

func (c mediaCondition) match(env MediaEnv) bool {
	switch c.feature {
	case featMinWidth:
		return env.Width >= c.value
	case featMaxWidth:
		return env.Width <= c.value
	case featWidth:
		return env.Width == c.value
	case featMinHeight:
		return env.Height >= c.value
	case featMaxHeight:
		return env.Height <= c.value
	case featHeight:
		return env.Height == c.value
	case featOrientation:
		portrait := env.Height >= env.Width
		return portrait == (c.value == 1)
	}
	return false
}
