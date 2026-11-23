package layout

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/text"
)

// idLike reports whether r is breakable on both sides under break-all: letters
// and digits, and the symbols of UAX #14 class AL, which break-all treats as
// ideographs (CSS Text 3 §5.2; observed in Chrome for "_").
func idLike(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("#&*<=>@^_`~", r)
}

// breaksAllBetween reports whether break-all adds an opportunity between the
// base rune p of a cluster and the rune r that starts the next one: between two
// ideograph-like characters, after a separator (. , : ; / -) or a closing
// parenthesis before one, and before a hyphen, an opening parenthesis or a prefix sign ($ + \\). Other punctuation keeps its normal rules (no break before
// ")" or "." or ":", none around a backslash, quote or "(" ).
func breaksAllBetween(p, r rune) bool {
	switch {
	case idLike(p) && idLike(r):
		return true
	case idLike(r) && strings.ContainsRune(".,:;/-", p):
		return true
	case idLike(p) && strings.ContainsRune("-($+\\", r), p == ')' && idLike(r):
		return true
	}
	return false
}

// appendBreakAll appends to dst the segment ends of text under word-break:
// break-all: the opportunities of base (sorted) plus those of breaksAllBetween.
// A cluster is never split (text.ExtendsCluster). Offsets are strictly
// increasing and inside the text.
func appendBreakAll(dst []int32, s string, base []text.Break) []int32 {
	push := func(off int) { dst = appendEnd(dst, off, len(s)) }
	bi := 0
	var prev, baseRune rune
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		for bi < len(base) && base[bi].Offset <= i {
			push(base[bi].Offset)
			bi++
		}
		if i > 0 && !text.ExtendsCluster(prev, r) {
			if breaksAllBetween(baseRune, r) {
				push(i)
			}
			baseRune = r
		} else if i == 0 {
			baseRune = r
		}
		prev = r
		i += w
	}
	for ; bi < len(base); bi++ {
		push(base[bi].Offset)
	}
	return dst
}

// appendEnd appends the segment end off of a text of n bytes when it is
// strictly inside the text and after the previous end.
func appendEnd(dst []int32, off, n int) []int32 {
	if off > 0 && off < n && (len(dst) == 0 || int(dst[len(dst)-1]) < off) {
		return append(dst, clamp32(off))
	}
	return dst
}
