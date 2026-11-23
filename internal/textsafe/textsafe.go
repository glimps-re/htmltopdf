// Package textsafe classifies characters that must never reach a PDF string,
// a warning detail or a drawn text: controls, format characters (bidi, zero
// width, soft hyphen, tags) and line or paragraph separators.
package textsafe

import "unicode"

// IsUnsafe reports whether r is a control (Cc), format (Cf), line separator
// (Zl) or paragraph separator (Zp) character, or a tag character
// (U+E0000-U+E007F). U+034F COMBINING GRAPHEME JOINER is a mark and is kept.
func IsUnsafe(r rune) bool {
	if r < 0x80 {
		return r < 0x20 || r == 0x7f
	}
	if r >= 0xe0000 && r <= 0xe007f {
		return true
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}
