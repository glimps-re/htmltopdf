package text

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

// FontInfo is what a PDF font descriptor needs to know about a face, in font
// units.
type FontInfo struct {
	// Name is "Family-Style" without spaces, for example "LiberationSans-Bold".
	Name       string
	UnitsPerEm uint16
	Ascent     int16
	Descent    int16
	CapHeight  int16
	BBox       [4]int16
	Italic     bool
	Fixed      bool
}

// clampInt16 converts a font-unit metric; NaN gives 0.
func clampInt16(v float32) int16 {
	if v != v {
		return 0
	}
	return int16(max(min(float64(v), math.MaxInt16), math.MinInt16))
}

// boldWeight is the OS/2 weight class from which a face is named Bold.
const boldWeight = 600

// signed16 reads a big-endian two's complement 16-bit field; the clamp never
// triggers, it only keeps the conversion explicit.
func signed16(b []byte) int16 {
	v := int(binary.BigEndian.Uint16(b))
	if v >= 0x8000 {
		v -= 0x10000
	}
	return int16(max(min(v, math.MaxInt16), math.MinInt16))
}

// Info returns the descriptor metrics of f, or false when f cannot be parsed.
func (s *Shaper) Info(f *Face) (FontInfo, bool) {
	ff := s.face(f)
	if ff == nil {
		return FontInfo{}, false
	}
	info := FontInfo{
		UnitsPerEm: ff.Upem(),
		CapHeight:  clampInt16(ff.LineMetric(font.CapHeight)),
		Italic:     f.Italic,
		Fixed:      ff.IsMonospace(),
	}
	if ext, ok := ff.FontHExtents(); ok {
		info.Ascent, info.Descent = clampInt16(ext.Ascender), clampInt16(ext.Descender)
	}
	ld, err := ot.NewLoader(bytes.NewReader(f.data))
	if err != nil {
		return FontInfo{}, false
	}
	head, err := ld.RawTable(ot.MustNewTag("head"))
	if err != nil || len(head) < 44 {
		return FontInfo{}, false
	}
	for i := range info.BBox {
		info.BBox[i] = signed16(head[36+2*i:])
	}
	if info.CapHeight <= 0 {
		info.CapHeight = info.Ascent
	}
	style := "Regular"
	switch {
	case f.Weight >= boldWeight && f.Italic:
		style = "BoldItalic"
	case f.Weight >= boldWeight:
		style = "Bold"
	case f.Italic:
		style = "Italic"
	}
	info.Name = strings.ReplaceAll(f.Family, " ", "") + "-" + style
	return info, true
}
