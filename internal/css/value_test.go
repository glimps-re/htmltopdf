package css

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func valueTokens(t testing.TB, v string) []Token {
	t.Helper()
	b := budget.New(context.Background(), budget.Defaults(), nil)
	decls, err := ParseDeclarations([]byte("x:"+v), b)
	if err != nil || len(decls) != 1 {
		t.Fatalf("value %q: %d declarations, err %v", v, len(decls), err)
	}
	return decls[0].Value
}

func TestParseLength(t *testing.T) {
	tests := []struct {
		in   string
		want Length
		ok   bool
	}{
		{"0", Length{0, UnitPx}, true},
		{"10px", Length{10, UnitPx}, true},
		{"-2.5px", Length{-2.5, UnitPx}, true},
		{"12pt", Length{16, UnitPx}, true},
		{"1pc", Length{16, UnitPx}, true},
		{"1in", Length{96, UnitPx}, true},
		{"2.54cm", Length{96, UnitPx}, true},
		{"25.4mm", Length{96, UnitPx}, true},
		{"101.6Q", Length{96, UnitPx}, true},
		{"1.5em", Length{1.5, UnitEm}, true},
		{"2rem", Length{2, UnitRem}, true},
		{"50%", Length{50, UnitPercent}, true},
		{" 5PX ", Length{5, UnitPx}, true},
		{"1e30px", Length{MaxLengthPx, UnitPx}, true},
		{"-1e30px", Length{-MaxLengthPx, UnitPx}, true},
		{"1e30in", Length{MaxLengthPx, UnitPx}, true},
		{"1e30em", Length{MaxLengthPx, UnitEm}, true},
		{"1e30%", Length{MaxLengthPx, UnitPercent}, true},
		{"1e7px", Length{MaxLengthPx, UnitPx}, true},
		{"1e300px", Length{MaxLengthPx, UnitPx}, true},
		{"5", Length{}, false},
		{"5ex", Length{}, false},
		{"5vw", Length{}, false},
		{"5deg", Length{}, false},
		{"calc(1px + 2px)", Length{}, false},
		{"var(--x)", Length{}, false},
		{"auto", Length{}, false},
		{"1px 2px", Length{}, false},
		{"", Length{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var toks []Token
			if tt.in != "" {
				toks = valueTokens(t, tt.in)
			}
			got, ok := ParseLength(toks)
			if ok != tt.ok || got.Unit != tt.want.Unit || math.Abs(got.Value-tt.want.Value) > 1e-4*math.Max(1, math.Abs(tt.want.Value)) {
				t.Fatalf("got %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestLengthResolve(t *testing.T) {
	posInf, nan := math.Inf(1), math.NaN()
	tests := []struct {
		name string
		l    Length
		want float64
	}{
		{"px", Length{5, UnitPx}, 5},
		{"em", Length{2, UnitEm}, 32},
		{"rem", Length{2, UnitRem}, 20},
		{"percent", Length{50, UnitPercent}, 100},
		{"huge em", Length{MaxLengthPx, UnitEm}, MaxLengthPx},
		{"negative huge", Length{-MaxLengthPx, UnitEm}, -MaxLengthPx},
	}
	for _, tt := range tests {
		got := tt.l.Resolve(16, 10, 200)
		if got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
	for _, l := range []Length{{1, UnitEm}, {1, UnitRem}, {1, UnitPercent}, {0, UnitEm}, {-1, UnitPercent}} {
		for _, base := range []float64{nan, posInf, -posInf, 1e300} {
			if got := l.Resolve(base, base, base); math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got) > MaxLengthPx {
				t.Errorf("%+v with base %v: got %v", l, base, got)
			}
		}
	}
	if got := (Length{Value: nan, Unit: UnitPx}).Resolve(1, 1, 1); got != 0 {
		t.Errorf("NaN px: got %v", got)
	}
	if got := (Length{Value: posInf, Unit: UnitPx}).Resolve(1, 1, 1); got != MaxLengthPx {
		t.Errorf("Inf px: got %v", got)
	}
}

func rgba(r, g, b, a float64) Color { return Color{r / 255, g / 255, b / 255, a} }

func TestParseColor(t *testing.T) {
	tests := []struct {
		in   string
		want Color
		ok   bool
	}{
		{"#fff", rgba(255, 255, 255, 1), true},
		{"#F9C105", rgba(0xf9, 0xc1, 0x05, 1), true},
		{"#f9c105", rgba(0xf9, 0xc1, 0x05, 1), true},
		{"#abcd", rgba(0xaa, 0xbb, 0xcc, 0xdd/255.0), true},
		{"#11223344", rgba(0x11, 0x22, 0x33, 0x44/255.0), true},
		{"#12", Color{}, false},
		{"#12345", Color{}, false},
		{"#1234567", Color{}, false},
		{"#123456789", Color{}, false},
		{"#ggg", Color{}, false},
		{"red", rgba(255, 0, 0, 1), true},
		{"RebeccaPurple", rgba(0x66, 0x33, 0x99, 1), true},
		{"transparent", Color{}, true},
		{"Transparent", Color{}, true},
		{"notacolor", Color{}, false},
		{"currentcolor", Color{}, false},
		{"rgb(1, 2, 3)", rgba(1, 2, 3, 1), true},
		{"rgba(255, 255, 255, 0.95)", rgba(255, 255, 255, 0.95), true},
		{"rgba(234, 91, 12, 0)", rgba(234, 91, 12, 0), true},
		{"rgba(0,0,0,.08)", rgba(0, 0, 0, 0.08), true},
		{"RGB(1 2 3)", rgba(1, 2, 3, 1), true},
		{"rgb(1 2 3 / 0.5)", rgba(1, 2, 3, 0.5), true},
		{"rgb(1 2 3 / 50%)", rgba(1, 2, 3, 0.5), true},
		{"rgba(1 2 3/ 0.5)", rgba(1, 2, 3, 0.5), true},
		{"rgb(100%, 0%, 50%)", rgba(255, 0, 127.5, 1), true},
		{"rgb(100% 0% 0% / 25%)", rgba(255, 0, 0, 0.25), true},
		{"rgb(300, -5, 1e30)", rgba(255, 0, 255, 1), true},
		{"rgb(1, 2, 3, 7)", rgba(1, 2, 3, 1), true},
		{"rgb(1, 2, 3, -1)", rgba(1, 2, 3, 0), true},
		{"rgb(200%, 2, 3)", Color{}, false},
		{"rgb(1, 2%, 3)", Color{}, false},
		{"rgb(1, 2)", Color{}, false},
		{"rgb(1, 2, 3, 4, 5)", Color{}, false},
		{"rgb(1 2)", Color{}, false},
		{"rgb(1 2 3 4)", Color{}, false},
		{"rgb(1, 2, 3,)", Color{}, false},
		{"rgb(1 2 3 /)", Color{}, false},
		{"rgb(1, 2 3)", Color{}, false},
		{"rgb(a, b, c)", Color{}, false},
		{"rgb(calc(1), 2, 3)", Color{}, false},
		{"rgb()", Color{}, false},
		{"hsl(0, 100%, 50%)", Color{}, false},
		{"var(--c)", Color{}, false},
		{"red blue", Color{}, false},
		{"rgb(1, 2, 3) red", Color{}, false},
		{"rgb(1, 2, 3", rgba(1, 2, 3, 1), true},
		{"rgb(1, 2, calc(3)", Color{}, false},
		{"12px", Color{}, false},
		{"", Color{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var toks []Token
			if tt.in != "" {
				toks = valueTokens(t, tt.in)
			}
			got, ok := ParseColor(toks)
			if ok != tt.ok {
				t.Fatalf("got %+v, %v; want ok=%v", got, ok, tt.ok)
			}
			if ok && !near(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func near(a, b Color) bool {
	const eps = 1e-6
	return math.Abs(a.R-b.R) < eps && math.Abs(a.G-b.G) < eps && math.Abs(a.B-b.B) < eps && math.Abs(a.A-b.A) < eps
}

func TestParseGradient(t *testing.T) {
	red, blue, green := rgba(255, 0, 0, 1), rgba(0, 0, 255, 1), rgba(0, 128, 0, 1)
	tests := []struct {
		in     string
		angle  float64
		corner Corner
		stops  []ColorStop
	}{
		{"linear-gradient(red, blue)", 180, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(90deg, red, blue)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"LINEAR-GRADIENT(90DEG,#f00,#00f)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(120deg, #f00 0%, green 45%, #00f 100%)", 120, CornerNone, []ColorStop{{red, 0}, {green, 0.45}, {blue, 1}}},
		{"linear-gradient(0, red, blue)", 0, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(-90deg, red, blue)", 270, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(450deg, red, blue)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(0.25turn, red, blue)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(100grad, red, blue)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(1.5707963267948966rad, red, blue)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to top, red, blue)", 0, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to right, red, blue)", 90, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to bottom, red, blue)", 180, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to left, red, blue)", 270, CornerNone, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to top right, red, blue)", 0, CornerTopRight, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to right top, red, blue)", 0, CornerTopRight, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to bottom right, red, blue)", 0, CornerBottomRight, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to left bottom, red, blue)", 0, CornerBottomLeft, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(to top left, red, blue)", 0, CornerTopLeft, []ColorStop{{red, 0}, {blue, 1}}},
		{"linear-gradient(red 20%, blue 80%)", 180, CornerNone, []ColorStop{{red, 0.2}, {blue, 0.8}}},
		{"linear-gradient(red, green, blue)", 180, CornerNone, []ColorStop{{red, 0}, {green, 0.5}, {blue, 1}}},
		{"linear-gradient(red, green, green, blue)", 180, CornerNone, []ColorStop{{red, 0}, {green, 1.0 / 3}, {green, 2.0 / 3}, {blue, 1}}},
		{"linear-gradient(red 10%, green, green, blue 70%)", 180, CornerNone, []ColorStop{{red, 0.1}, {green, 0.3}, {green, 0.5}, {blue, 0.7}}},
		{"linear-gradient(red 50%, green 20%, blue)", 180, CornerNone, []ColorStop{{red, 0.5}, {green, 0.5}, {blue, 1}}},
		{"linear-gradient(red 120%, blue -5%)", 180, CornerNone, []ColorStop{{red, 1}, {blue, 1}}},
		{"linear-gradient(red, green 0%, blue)", 180, CornerNone, []ColorStop{{red, 0}, {green, 0}, {blue, 1}}},
		{"linear-gradient(red 90%, green, blue)", 180, CornerNone, []ColorStop{{red, 0.9}, {green, 0.95}, {blue, 1}}},
		{
			"linear-gradient(rgba(234, 91, 12, 0), rgb(1 2 3 / 50%) 100%)", 180, CornerNone,
			[]ColorStop{{rgba(234, 91, 12, 0), 0}, {rgba(1, 2, 3, 0.5), 1}},
		},
		{"linear-gradient(90deg, transparent, red)", 90, CornerNone, []ColorStop{{Color{}, 0}, {red, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			g, ok := ParseGradient(valueTokens(t, tt.in))
			if !ok {
				t.Fatal("rejected")
			}
			if math.Abs(g.Angle-tt.angle) > 1e-4 || g.Corner != tt.corner || len(g.Stops) != len(tt.stops) {
				t.Fatalf("got %+v, want angle %v corner %v stops %v", g, tt.angle, tt.corner, tt.stops)
			}
			for i, s := range g.Stops {
				if !near(s.Color, tt.stops[i].Color) || math.Abs(s.Pos-tt.stops[i].Pos) > 1e-6 {
					t.Fatalf("stop %d: got %+v, want %+v", i, s, tt.stops[i])
				}
			}
		})
	}
}

func TestParseGradientRejects(t *testing.T) {
	tests := []string{
		"linear-gradient()",
		"linear-gradient(red)",
		"linear-gradient(90deg)",
		"linear-gradient(90deg, red)",
		"linear-gradient(to top, red)",
		"linear-gradient(90deg, red, notacolor)",
		"linear-gradient(90deg, var(--a), var(--b))",
		"linear-gradient(red, 20%, blue)",
		"linear-gradient(red 10px, blue)",
		"linear-gradient(red 10% 20%, blue)",
		"linear-gradient(red 10% 20% 30%, blue)",
		"linear-gradient(to, red, blue)",
		"linear-gradient(to middle, red, blue)",
		"linear-gradient(to top top, red, blue)",
		"linear-gradient(to left right, red, blue)",
		"linear-gradient(to top right bottom, red, blue)",
		"linear-gradient(10px, red, blue)",
		"linear-gradient(1, red, blue)",
		"linear-gradient(90deg red, blue)",
		"linear-gradient(red,, blue)",
		"linear-gradient(red, blue,)",
		"repeating-linear-gradient(red, blue)",
		"radial-gradient(red, blue)",
		"conic-gradient(red, blue)",
		"red",
		"linear-gradient(red, blue) red",
		"linear-gradient(calc(1deg), red, blue)",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			if g, ok := ParseGradient(valueTokens(t, in)); ok {
				t.Fatalf("accepted: %+v", g)
			}
		})
	}
	if _, ok := ParseGradient(nil); ok {
		t.Fatal("nil accepted")
	}
}

func stopsList(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "red"
	}
	return "linear-gradient(" + strings.Join(parts, ",") + ")"
}

func TestGradientStopLimit(t *testing.T) {
	g, ok := ParseGradient(valueTokens(t, stopsList(MaxGradientStops)))
	if !ok || len(g.Stops) != MaxGradientStops {
		t.Fatalf("%d stops: ok=%v", MaxGradientStops, ok)
	}
	if g.Stops[0].Pos != 0 || g.Stops[MaxGradientStops-1].Pos != 1 {
		t.Fatalf("end positions %v %v", g.Stops[0].Pos, g.Stops[MaxGradientStops-1].Pos)
	}
	for i := 1; i < len(g.Stops); i++ {
		if g.Stops[i].Pos < g.Stops[i-1].Pos {
			t.Fatalf("positions decrease at %d", i)
		}
	}
	if _, ok := ParseGradient(valueTokens(t, stopsList(MaxGradientStops+1))); ok {
		t.Fatalf("%d stops accepted", MaxGradientStops+1)
	}
	if _, ok := ParseGradient(valueTokens(t, "linear-gradient(90deg,"+strings.TrimPrefix(stopsList(MaxGradientStops), "linear-gradient("))); !ok {
		t.Fatal("angle + 64 stops rejected")
	}
	if _, ok := ParseGradient(valueTokens(t, "linear-gradient(90deg,"+strings.TrimPrefix(stopsList(MaxGradientStops+1), "linear-gradient("))); ok {
		t.Fatal("angle + 65 stops accepted")
	}
}

func FuzzValues(f *testing.F) {
	for _, s := range []string{"rgb(1,2,3)", "linear-gradient(to top, red 1%, blue)", "#fff", "1e30px", "rgb(1 2 3 / 4)", "calc(1px)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		decls, err := ParseDeclarations([]byte("x:"+v), b)
		if err != nil {
			return
		}
		for _, d := range decls {
			if l, ok := ParseLength(d.Value); ok && (math.IsNaN(l.Value) || math.Abs(l.Value) > MaxLengthPx) {
				t.Fatalf("bad length %+v", l)
			}
			if c, ok := ParseColor(d.Value); ok {
				for _, x := range []float64{c.R, c.G, c.B, c.A} {
					if x < 0 || x > 1 || math.IsNaN(x) {
						t.Fatalf("bad color %+v", c)
					}
				}
			}
			if g, ok := ParseGradient(d.Value); ok {
				if len(g.Stops) < 2 || len(g.Stops) > MaxGradientStops || g.Angle < 0 || g.Angle >= 360 {
					t.Fatalf("bad gradient %+v", g)
				}
				for i, s := range g.Stops {
					if s.Pos < 0 || s.Pos > 1 || i > 0 && s.Pos < g.Stops[i-1].Pos {
						t.Fatalf("bad stops %+v", g.Stops)
					}
				}
			}
		}
	})
}

func TestGradientNoPositionedStop(t *testing.T) {
	g, ok := ParseGradient(valueTokens(t, "linear-gradient(red, red, red, red, red)"))
	if !ok {
		t.Fatal("rejected")
	}
	for i, s := range g.Stops {
		if want := float64(i) / 4; math.Abs(s.Pos-want) > 1e-9 {
			t.Errorf("stop %d at %v, want %v", i, s.Pos, want)
		}
	}
}

func TestLengthNormalisesNegativeZero(t *testing.T) {
	l, ok := ParseLength(valueTokens(t, "-0px"))
	if !ok || math.Signbit(l.Value) {
		t.Fatalf("got %+v, %v", l, ok)
	}
	if math.Signbit(Length{Value: math.Copysign(0, -1), Unit: UnitPx}.Resolve(1, 1, 1)) {
		t.Fatal("Resolve returned -0")
	}
}

func TestClamp01NaN(t *testing.T) {
	if got := clamp01(math.NaN()); got != 0 {
		t.Fatalf("clamp01(NaN) = %v", got)
	}
}

func BenchmarkParseColor(b *testing.B) {
	toks := valueTokens(b, "rgba(234, 91, 12, 0.5)")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := ParseColor(toks); !ok {
			b.Fatal("rejected")
		}
	}
}

func BenchmarkParseGradient(b *testing.B) {
	toks := valueTokens(b, "linear-gradient(120deg, #e52321 0%, #ea5b0c 45%, #f9c105 100%)")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := ParseGradient(toks); !ok {
			b.Fatal("rejected")
		}
	}
}
