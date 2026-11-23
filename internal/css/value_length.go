package css

import "math"

// MaxLengthPx bounds every length, in CSS px: larger values are clamped, so
// layout arithmetic stays finite.
const MaxLengthPx = 1e7

// Length is a CSS length. Token numbers are float32, so Value carries float32
// precision (1234.5678px is 1234.56775). Absolute units are converted to px at parse time;
// Em, Rem and Percent keep their unit until the computed style knows the font
// sizes and the percentage base.
type Length struct {
	Value float64
	// Unit is UnitPx, UnitEm, UnitRem or UnitPercent.
	Unit Unit
}

// absolutePx returns the number of px in one unit of an absolute length unit.
func absolutePx(u Unit) (float64, bool) {
	switch u {
	case UnitPx:
		return 1, true
	case UnitPt:
		return 96.0 / 72, true
	case UnitPc:
		return 16, true
	case UnitIn:
		return 96, true
	case UnitCm:
		return 96 / 2.54, true
	case UnitMm:
		return 96 / 25.4, true
	case UnitQ:
		return 96 / 101.6, true
	}
	return 0, false
}

// ParseLength parses a length or percentage: a dimension in px, pt, pc, in, cm,
// mm, Q, em or rem, a percentage, or the unitless number 0. Other units,
// functions such as calc() and anything else are rejected. The value is
// clamped to ±MaxLengthPx.
func ParseLength(toks []Token) (Length, bool) {
	toks = trimSpace(toks)
	if len(toks) != 1 {
		return Length{}, false
	}
	t := toks[0]
	v := float64(t.Num)
	switch t.Type {
	case TNumber:
		if v != 0 {
			return Length{}, false
		}
		return Length{Unit: UnitPx}, true
	case TPercentage:
		return Length{Value: clampLength(v), Unit: UnitPercent}, true
	case TDimension:
		switch t.Unit {
		case UnitEm, UnitRem:
			return Length{Value: clampLength(v), Unit: t.Unit}, true
		default:
			if f, ok := absolutePx(t.Unit); ok {
				return Length{Value: clampLength(v * f), Unit: UnitPx}, true
			}
		}
	}
	return Length{}, false
}

// clampLength clamps v to ±MaxLengthPx; NaN and -0 become 0.
func clampLength(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(-MaxLengthPx, math.Min(MaxLengthPx, v)) + 0
}

// Resolve returns the length in px. fontSize and rootFontSize are the em and
// rem bases and percentBase the base of a percentage, all in px. The result is
// finite and clamped to ±MaxLengthPx whatever the arguments.
func (l Length) Resolve(fontSize, rootFontSize, percentBase float64) float64 {
	switch l.Unit {
	case UnitEm:
		return clampLength(l.Value * fontSize)
	case UnitRem:
		return clampLength(l.Value * rootFontSize)
	case UnitPercent:
		return clampLength(l.Value / 100 * percentBase)
	}
	return clampLength(l.Value)
}
