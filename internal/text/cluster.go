package text

import "golang.org/x/text/unicode/norm"

// hangulJoins reports whether jamo r continues a syllable whose last rune is
// prev (UAX #29 GB6 to GB8, for the conjoining jamo).
func hangulJoins(prev, r rune) bool {
	isL := func(c rune) bool { return c >= 0x1100 && c <= 0x115f || c >= 0xa960 && c <= 0xa97c }
	isV := func(c rune) bool { return c >= 0x1160 && c <= 0x11a7 || c >= 0xd7b0 && c <= 0xd7c6 }
	isT := func(c rune) bool { return c >= 0x11a8 && c <= 0x11ff || c >= 0xd7cb && c <= 0xd7fb }
	switch {
	case isL(prev):
		return isL(r) || isV(r)
	case isV(prev):
		return isV(r) || isT(r)
	case isT(prev):
		return isT(r)
	}
	return false
}

// isVirama reports whether c is a virama (canonical combining class 9): the
// consonant after it joins the conjunct (UAX #29 GB9c).
func isVirama(c rune) bool {
	// Viramas live in the Indic blocks, U+0900-U+1DFF and U+A800-U+ABFF, and in
	// a few supplementary-plane scripts: CJK and other text skips the lookup.
	if inIndic := c >= 0x900 && c <= 0x1dff || c >= 0xa800 && c <= 0xabff || c >= 0x10000; !inIndic {
		return false
	}
	return norm.NFD.PropertiesString(string(c)).CCC() == 9
}
