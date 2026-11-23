package css

import "math"

// TrimSpace returns toks without its leading and trailing whitespace tokens.
func TrimSpace(toks []Token) []Token { return trimSpace(toks) }

// Fields splits toks at top-level whitespace, ignoring whitespace inside
// blocks. At most 128 fields are split: the rest comes back as one more field,
// so that a hostile value costs a bounded number of slices.
func Fields(toks []Token) [][]Token { return fields(toks) }

// LowerASCII lowercases the ASCII letters of s only. It is the one fold CSS
// keywords and names use: Unicode folding would read U+212A (Kelvin sign) as k.
func LowerASCII(s string) string { return lowerASCII(s) }

// SkipBlock returns the index of the token after toks[i], past the whole block
// when toks[i] opens one (a function, parenthesis, bracket or brace).
func SkipBlock(toks []Token, i int) int { return next(toks, i) }

// HasClampedLength reports whether a dimension or percentage token of toks,
// at any depth, is beyond ±MaxLengthPx once converted to px, so that
// ParseLength clamps it.
func HasClampedLength(toks []Token) bool {
	for i := range toks {
		t := &toks[i]
		v := float64(t.Num)
		switch t.Type {
		case TPercentage:
		case TDimension:
			if f, ok := absolutePx(t.Unit); ok {
				v *= f
			} else if t.Unit != UnitEm && t.Unit != UnitRem {
				continue
			}
		default:
			continue
		}
		if math.Abs(v) > MaxLengthPx {
			return true
		}
	}
	return false
}
