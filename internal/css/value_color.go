package css

import (
	"math"

	"golang.org/x/image/colornames"
)

// Color is a non-premultiplied sRGB color with every component in [0, 1].
type Color struct{ R, G, B, A float64 }

// ParseColor parses a hex color (#rgb, #rgba, #rrggbb, #rrggbbaa), rgb() and
// rgba() in the comma and the space/slash syntaxes, a named color or
// transparent. Components are clamped to their range. Anything else,
// currentcolor and var() included, is rejected.
func ParseColor(toks []Token) (Color, bool) {
	toks = trimSpace(toks)
	if len(toks) == 0 {
		return Color{}, false
	}
	switch toks[0].Type {
	case TIdent:
		if len(toks) == 1 {
			return namedColor(lowerASCII(toks[0].Text))
		}
	case THash:
		if len(toks) == 1 {
			return hexColor(toks[0].Text)
		}
	case TFunction:
		name, args, ok := function(toks)
		if ok && (name == "rgb" || name == "rgba") {
			return rgbColor(args)
		}
	}
	return Color{}, false
}

func namedColor(name string) (Color, bool) {
	if name == "transparent" {
		return Color{}, true
	}
	if name == "rebeccapurple" { // CSS Color 4 name, absent from colornames
		return Color{0x66 / 255.0, 0x33 / 255.0, 0x99 / 255.0, 1}, true
	}
	c, ok := colornames.Map[name]
	if !ok {
		return Color{}, false
	}
	return Color{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, 1}, true
}

func hexColor(h string) (Color, bool) {
	var d [8]float64
	n := len(h)
	if n != 3 && n != 4 && n != 6 && n != 8 {
		return Color{}, false
	}
	for i := 0; i < n; i++ {
		if !isHex(h[i]) {
			return Color{}, false
		}
	}
	if n <= 4 {
		for i := 0; i < n; i++ {
			d[i] = float64(hexVal(h[i])*17) / 255
		}
	} else {
		for i := 0; i < n/2; i++ {
			d[i] = float64(hexVal(h[2*i])<<4|hexVal(h[2*i+1])) / 255
		}
		n /= 2
	}
	c := Color{d[0], d[1], d[2], 1}
	if n == 4 {
		c.A = d[3]
	}
	return c, true
}

func rgbColor(args []Token) (Color, bool) {
	var comps [][]Token
	if parts := splitTop(args); len(parts) > 1 {
		if len(parts) != 3 && len(parts) != 4 {
			return Color{}, false
		}
		comps = parts
	} else {
		left, right := splitSlash(args)
		comps = fields(left)
		if len(comps) != 3 {
			return Color{}, false
		}
		if right != nil {
			comps = append(comps, right)
		}
	}
	var rgb [3]float64
	percent := false
	for i := range 3 {
		v, pct, ok := component(comps[i])
		if !ok || i > 0 && pct != percent {
			return Color{}, false
		}
		percent = pct
		if pct {
			v = v * 255 / 100
		}
		rgb[i] = clamp01(v / 255)
	}
	a := 1.0
	if len(comps) == 4 {
		v, pct, ok := component(comps[3])
		if !ok {
			return Color{}, false
		}
		if pct {
			v /= 100
		}
		a = clamp01(v)
	}
	return Color{rgb[0], rgb[1], rgb[2], a}, true
}

// component reads a number or a percentage from a single token.
func component(toks []Token) (v float64, percent, ok bool) {
	toks = trimSpace(toks)
	if len(toks) != 1 {
		return 0, false, false
	}
	switch toks[0].Type {
	case TNumber:
		return float64(toks[0].Num), false, true
	case TPercentage:
		return float64(toks[0].Num), true, true
	}
	return 0, false, false
}

// clamp01 clamps v to [0, 1]; NaN, which computed values can produce, is 0.
func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(1, v))
}

// splitSlash splits args at the first top-level "/". right is nil when there is
// none.
func splitSlash(args []Token) (left, right []Token) {
	for i := 0; i < len(args); {
		if args[i].Type == TDelim && args[i].Text == "/" {
			return args[:i], trimSpace(args[i+1:])
		}
		i = next(args, i)
	}
	return args, nil
}
