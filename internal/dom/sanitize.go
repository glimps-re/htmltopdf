package dom

import (
	"strings"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/textsafe"
)

const replacement = "\uFFFD"

// bidiMarkers maps the bidi embedding, override and isolate controls to the
// visible text drawn in their place.
var bidiMarkers = map[rune]string{
	0x202A: "\u27E8LRE\u27E9",
	0x202B: "\u27E8RLE\u27E9",
	0x202C: "\u27E8PDF\u27E9",
	0x202D: "\u27E8LRO\u27E9",
	0x202E: "\u27E8RLO\u27E9",
	0x2066: "\u27E8LRI\u27E9",
	0x2067: "\u27E8RLI\u27E9",
	0x2068: "\u27E8FSI\u27E9",
	0x2069: "\u27E8PDI\u27E9",
}

type runeAction uint8

const (
	keep runeAction = iota
	drop
	replace
	marker
	space
)

// classify decides what to do with r. The order matters: the replace, marker,
// space and keep cases come before the generic drop of every unsafe rune.
func classify(r rune, size int) runeAction {
	switch {
	case r == utf8.RuneError && size == 1:
		return replace
	case r == '\t' || r == '\n' || r == '\r':
		return keep
	case r < 0x20, r >= 0x7F && r <= 0x9F:
		return replace
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return marker
	case r == 0x2028 || r == 0x2029:
		return space
	case r == 0x200C || r == 0x200D:
		return keep // ZWNJ and ZWJ shape Persian words and emoji sequences
	case textsafe.IsUnsafe(r):
		return drop
	}
	return keep
}

// SanitizeText returns s with the text sanitation of design section 8.4:
// bidi controls become visible markers, line and paragraph separators become
// a space, C0/C1 controls and DEL (other than tab, LF, CR) and invalid UTF-8
// bytes become U+FFFD, and every other unsafe character (textsafe.IsUnsafe:
// zero-width, soft hyphen, tags...) is dropped, except U+200C and U+200D. It returns s itself, without
// allocating, when nothing needs to change. The result is at most three times
// longer than s.
func SanitizeText(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if classify(r, size) != keep {
			break
		}
		i += size
	}
	if i == len(s) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 16)
	sb.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch classify(r, size) {
		case keep:
			sb.WriteString(s[i : i+size])
		case replace:
			sb.WriteString(replacement)
		case marker:
			sb.WriteString(bidiMarkers[r])
		case space:
			sb.WriteByte(' ')
		case drop:
		}
		i += size
	}
	return sb.String()
}
