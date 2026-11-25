package css

import "testing"

func TestParseRadialGradient(t *testing.T) {
	red, blue := rgba(255, 0, 0, 1), rgba(0, 0, 255, 1)
	tests := []struct {
		in    string
		shape GradientShape
		stops []ColorStop
	}{
		{"radial-gradient(red, blue)", GradientEllipse, []ColorStop{{red, 0}, {blue, 1}}},
		{"radial-gradient(circle, red, blue)", GradientCircle, []ColorStop{{red, 0}, {blue, 1}}},
		{"RADIAL-GRADIENT(Ellipse, red, blue)", GradientEllipse, []ColorStop{{red, 0}, {blue, 1}}},
		{"radial-gradient(circle farthest-corner, red, blue)", GradientCircle, []ColorStop{{red, 0}, {blue, 1}}},
		{"radial-gradient(farthest-corner circle at center, red, blue)", GradientCircle, []ColorStop{{red, 0}, {blue, 1}}},
		{"radial-gradient(at center, red, blue)", GradientEllipse, []ColorStop{{red, 0}, {blue, 1}}},
		{"radial-gradient(circle at center center, red 10%, blue 70%)", GradientCircle, []ColorStop{{red, 0.1}, {blue, 0.7}}},
		{"radial-gradient(circle, rgba(234, 91, 12, 0.5), transparent 70%)", GradientCircle, []ColorStop{{rgba(234, 91, 12, 0.5), 0}, {Color{}, 0.7}}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			g, ok := ParseGradient(valueTokens(t, tt.in))
			if !ok {
				t.Fatal("rejected")
			}
			if g.Shape != tt.shape || g.Angle != 0 || g.Corner != CornerNone || len(g.Stops) != len(tt.stops) {
				t.Fatalf("got %+v", g)
			}
			for i, s := range g.Stops {
				if !near(s.Color, tt.stops[i].Color) || s.Pos != tt.stops[i].Pos {
					t.Fatalf("stop %d: got %+v, want %+v", i, s, tt.stops[i])
				}
			}
		})
	}
}

func TestParseRadialGradientRejects(t *testing.T) {
	for _, in := range []string{
		"radial-gradient(circle closest-side, red, blue)",
		"radial-gradient(farthest-side, red, blue)",
		"radial-gradient(closest-corner, red, blue)",
		"radial-gradient(circle at top, red, blue)",
		"radial-gradient(circle at 10% 20%, red, blue)",
		"radial-gradient(at center center center, red, blue)",
		"radial-gradient(circle circle, red, blue)",
		"radial-gradient(circle farthest-corner farthest-corner, red, blue)",
		"radial-gradient(at, red, blue)",
		"radial-gradient(10px, red, blue)",
		"radial-gradient(10px 20px, red, blue)",
		"radial-gradient(circle)",
		"radial-gradient(circle, red)",
		"radial-gradient(90deg, red, blue)",
		"repeating-radial-gradient(red, blue)",
	} {
		t.Run(in, func(t *testing.T) {
			if g, ok := ParseGradient(valueTokens(t, in)); ok {
				t.Fatalf("accepted: %+v", g)
			}
		})
	}
}
