package text

import "testing"

func TestInfo(t *testing.T) {
	s, _ := newShaper(t)
	tests := []struct {
		family string
		weight int
		italic bool
		name   string
		fixed  bool
	}{
		{"Liberation Sans", 400, false, "LiberationSans-Regular", false},
		{"Liberation Sans", 700, true, "LiberationSans-BoldItalic", false},
		{"DejaVu Sans Mono", 400, false, "DejaVuSansMono-Regular", true},
	}
	for _, tt := range tests {
		f := s.fs.chain(nil, []string{tt.family}, tt.weight, tt.italic)[0]
		info, ok := s.Info(f)
		if !ok || info.Name != tt.name || info.Fixed != tt.fixed || info.Italic != tt.italic {
			t.Errorf("%s: %+v %v", tt.family, info, ok)
		}
		if info.UnitsPerEm == 0 || info.Ascent <= 0 || info.Descent >= 0 || info.CapHeight <= 0 || info.BBox[2] <= info.BBox[0] || info.BBox[3] <= info.BBox[1] {
			t.Errorf("%s: implausible metrics %+v", tt.family, info)
		}
	}
	if _, ok := s.Info(&Face{data: []byte("x")}); ok {
		t.Error("broken face has info")
	}
}
