package css

import "math"

// MaxGradientStops bounds the color stops of a gradient.
const MaxGradientStops = 64

// Corner is the target corner of a "to <corner>" gradient direction.
type Corner uint8

// Corners. The angle of a corner gradient depends on the box it paints, so it
// is left to layout.
const (
	CornerNone Corner = iota
	CornerTopRight
	CornerBottomRight
	CornerBottomLeft
	CornerTopLeft
)

// ColorStop is a color at a position along the gradient line, in [0, 1].
type ColorStop struct {
	Color Color
	Pos   float64
}

// GradientShape tells a linear gradient from a radial one.
type GradientShape uint8

// Gradient shapes. Radial gradients are always "farthest-corner at center".
const (
	GradientLinear GradientShape = iota
	GradientCircle
	GradientEllipse
)

// Gradient is a linear-gradient() or a radial-gradient().
type Gradient struct {
	// Shape is GradientLinear for every linear gradient.
	Shape GradientShape
	// Angle is the direction in degrees, CSS convention: 0 points up and
	// angles grow clockwise. It is in [0, 360) and unused when Corner is set or
	// Shape is not linear.
	Angle  float64
	Corner Corner
	// Stops has 2 to MaxGradientStops entries with non-decreasing positions.
	Stops []ColorStop
}

// ParseGradient parses linear-gradient() or radial-gradient(). A linear one has an optional angle (deg, rad, grad,
// turn or the unitless 0) or "to <side|corner>", then at least two color stops
// with optional percentage positions. A radial one has an optional
// "[circle | ellipse] [farthest-corner] [at center]" (other sizes and positions
// are rejected), then the stops. Colors may be translucent. Missing positions
// are spread as in CSS Images §3.5.3 and positions are made non-decreasing.
// Lengths as positions, interpolation hints, repeating gradients and other
// gradient types are rejected.
func ParseGradient(toks []Token) (Gradient, bool) {
	name, args, ok := function(toks)
	if !ok || (name != "linear-gradient" && name != "radial-gradient") {
		return Gradient{}, false
	}
	parts := splitTop(args)
	g := Gradient{Angle: 180}
	if name == "radial-gradient" {
		g.Angle = 0
		g.Shape = GradientEllipse
		if shape, isConfig, ok := radialConfig(parts[0]); isConfig {
			if !ok {
				return Gradient{}, false
			}
			g.Shape = shape
			parts = parts[1:]
		}
	} else if dir, ok := direction(parts[0]); ok {
		g.Angle, g.Corner = dir.angle, dir.corner
		parts = parts[1:]
	}
	if len(parts) < 2 || len(parts) > MaxGradientStops {
		return Gradient{}, false
	}
	g.Stops = make([]ColorStop, len(parts))
	positioned := make([]bool, len(parts))
	for i, p := range parts {
		stop, has, ok := colorStop(p)
		if !ok {
			return Gradient{}, false
		}
		g.Stops[i], positioned[i] = stop, has
	}
	placeStops(g.Stops, positioned)
	return g, true
}

// radialConfig parses the optional first argument of radial-gradient(). isConfig
// is false when the argument is not one (it is then the first color stop); ok is
// false when it is one but not of the supported form.
func radialConfig(toks []Token) (shape GradientShape, isConfig, ok bool) {
	fs := fields(toks)
	if len(fs) == 0 {
		return 0, false, false
	}
	shape = GradientEllipse
	seenShape, seenSize, seenAt := false, false, false
	for i := 0; i < len(fs); i++ {
		if len(fs[i]) != 1 || fs[i][0].Type != TIdent {
			return 0, isConfig, false
		}
		switch kw := lowerASCII(fs[i][0].Text); kw {
		case "circle", "ellipse":
			if seenShape || seenAt {
				return 0, true, false
			}
			seenShape = true
			shape = GradientCircle
			if kw == "ellipse" {
				shape = GradientEllipse
			}
		case "farthest-corner":
			if seenSize || seenAt {
				return 0, true, false
			}
			seenSize = true
		case "at":
			if seenAt || i+1 >= len(fs) {
				return 0, true, false
			}
			seenAt = true
			for _, f := range fs[i+1:] {
				if len(f) != 1 || f[0].Type != TIdent || lowerASCII(f[0].Text) != "center" {
					return 0, true, false
				}
			}
			if len(fs)-i-1 > 2 {
				return 0, true, false
			}
			i = len(fs)
		case "closest-side", "closest-corner", "farthest-side":
			return 0, true, false
		default:
			return 0, i > 0, false
		}
		isConfig = true
	}
	return shape, true, true
}

type dirResult struct {
	angle  float64
	corner Corner
}

// direction parses the optional first argument; ok is false when it is not a
// direction (it is then the first color stop).
func direction(toks []Token) (dirResult, bool) {
	fs := fields(toks)
	if len(fs) == 0 {
		return dirResult{}, false
	}
	if len(fs) == 1 && len(fs[0]) == 1 {
		return angle(fs[0][0])
	}
	if len(fs[0]) != 1 || fs[0][0].Type != TIdent || lowerASCII(fs[0][0].Text) != "to" {
		return dirResult{}, false
	}
	var sides [2]string
	if len(fs) < 2 || len(fs) > 3 {
		return dirResult{}, false
	}
	for i, f := range fs[1:] {
		if len(f) != 1 || f[0].Type != TIdent {
			return dirResult{}, false
		}
		sides[i] = lowerASCII(f[0].Text)
	}
	return sideDirection(sides[0], sides[1])
}

func sideDirection(a, b string) (dirResult, bool) {
	if b == "" {
		switch a {
		case "top":
			return dirResult{angle: 0}, true
		case "right":
			return dirResult{angle: 90}, true
		case "bottom":
			return dirResult{angle: 180}, true
		case "left":
			return dirResult{angle: 270}, true
		}
		return dirResult{}, false
	}
	if a == "left" || a == "right" {
		a, b = b, a
	}
	switch {
	case a == "top" && b == "right":
		return dirResult{corner: CornerTopRight}, true
	case a == "bottom" && b == "right":
		return dirResult{corner: CornerBottomRight}, true
	case a == "bottom" && b == "left":
		return dirResult{corner: CornerBottomLeft}, true
	case a == "top" && b == "left":
		return dirResult{corner: CornerTopLeft}, true
	}
	return dirResult{}, false
}

// ParseAngle parses an angle token (deg, rad, grad, turn, or the unitless 0)
// and returns it in degrees, normalised to [0, 360).
func ParseAngle(t Token) (float64, bool) {
	d, ok := angle(t)
	return d.angle, ok
}

func angle(t Token) (dirResult, bool) {
	v := float64(t.Num)
	switch {
	case t.Type == TNumber && v == 0:
		// The unitless 0 is a valid angle in gradients (CSS Images 3 section 3.1.1).
	case t.Type == TDimension && t.Unit == UnitDeg:
	case t.Type == TDimension && t.Unit == UnitRad:
		v = v * 180 / math.Pi
	case t.Type == TDimension && t.Unit == UnitGrad:
		v *= 0.9
	case t.Type == TDimension && t.Unit == UnitTurn:
		v *= 360
	default:
		return dirResult{}, false
	}
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	// A tiny negative angle rounds to exactly 360 after the addition.
	if v >= 360 {
		v = 0
	}
	return dirResult{angle: v}, true
}

// colorStop parses "<color> [<percentage>]". has reports whether a position
// was given.
func colorStop(toks []Token) (stop ColorStop, has, ok bool) {
	fs := fields(toks)
	if len(fs) == 0 || len(fs) > 2 {
		return ColorStop{}, false, false
	}
	c, ok := ParseColor(fs[0])
	if !ok {
		return ColorStop{}, false, false
	}
	stop.Color = c
	if len(fs) == 1 {
		return stop, false, true
	}
	l, ok := ParseLength(fs[1])
	if !ok || l.Unit != UnitPercent {
		return ColorStop{}, false, false
	}
	stop.Pos = clamp01(l.Value / 100)
	return stop, true, true
}

// placeStops fills (CSS Images 3 section 3.5.3) the positions that were not given and enforces that
// positions never decrease.
func placeStops(stops []ColorStop, positioned []bool) {
	last := len(stops) - 1
	if !positioned[0] {
		stops[0].Pos = 0
	}
	if !positioned[last] {
		stops[last].Pos = 1
	}
	prev := stops[0].Pos
	for i := 1; i <= last; i++ {
		if positioned[i] || i == last {
			stops[i].Pos = math.Max(stops[i].Pos, prev)
			spread(stops, prevPositioned(positioned, i), i)
			prev = stops[i].Pos
		}
	}
}

func prevPositioned(positioned []bool, i int) int {
	j := i - 1
	for j > 0 && !positioned[j] {
		j--
	}
	return j
}

// spread evenly places the unpositioned stops strictly between from and to.
func spread(stops []ColorStop, from, to int) {
	n := to - from
	for k := from + 1; k < to; k++ {
		stops[k].Pos = stops[from].Pos + (stops[to].Pos-stops[from].Pos)*float64(k-from)/float64(n)
	}
}
