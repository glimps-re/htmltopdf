package text

import (
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"
)

// Break is a line-break opportunity before the byte at Offset. Mandatory is
// true for a forced break (newline).
type Break struct {
	Offset    int
	Mandatory bool
}

// maxPooledBytes bounds the text whose segmenter goes back to the pool.
const maxPooledBytes = 64 << 10

var segmenters = sync.Pool{New: func() any { return new(segmenter.Segmenter) }}

// Breaks appends to dst the break opportunities of text, in increasing offset
// order; the caller bounds len(text). The end of the text is not reported.
// They are the UAX #14 ones, except between two ASCII or Latin characters,
// where they are the ones Chrome offers (its tailored ICU rules: no break in
// paths, URLs and identifiers, a break after "-" before a letter, after a
// space before anything). Invalid UTF-8 counts as U+FFFD, which is not
// tailored; offsets stay in the original bytes.
func Breaks(text string, dst []Break) []Break {
	seg := segmenters.Get().(*segmenter.Segmenter)
	defer func() {
		// A segmenter that handled a huge text keeps its buffers: do not pool it.
		if len(text) <= maxPooledBytes {
			segmenters.Put(seg)
		}
	}()
	if err := seg.InitWithString(text); err != nil {
		return tailor(text, invalidOpportunities(seg, text), dst)
	}
	it := seg.LineIterator()
	return tailor(text, func() (Break, bool) {
		for it.Next() {
			l := it.Line()
			if end := l.OffsetInBytes + l.LengthInBytes; end < len(text) {
				return Break{Offset: end, Mandatory: l.IsMandatoryBreak}, true
			}
		}
		return Break{}, false
	}, dst)
}

// Character classes of the Chrome tailoring: ASCII letters, digits, the space,
// the Latin letters beyond ASCII and the 32 ASCII punctuation marks in code
// point order.
const (
	classLetter = iota
	classDigit
	classSpace
	classLatin
	classPunct // first punctuation class
	numClasses = classPunct + 32
	classNone  = 0xff // not tailored: the UAX #14 decision stands
)

// asciiClassOf gives the class of each ASCII character; classNone for the
// control characters.
var asciiClassOf = func() (t [utf8.RuneSelf]uint8) {
	n := uint8(0)
	for r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			t[r] = classLetter
		case r >= '0' && r <= '9':
			t[r] = classDigit
		case r == ' ':
			t[r] = classSpace
		case r > ' ' && r < 0x7f:
			t[r] = classPunct + n
			n++
		default:
			t[r] = classNone
		}
	}
	return t
}()

// classOf returns the class of r, or classNone for what the tailoring does not
// cover: control characters and everything beyond ASCII and the Latin letters.
func classOf(r rune) uint8 {
	if r < utf8.RuneSelf {
		return asciiClassOf[r]
	}
	// Fullwidth Latin letters are ideographic for UAX #14: not tailored.
	if unicode.Is(unicode.Latin, r) && unicode.IsLetter(r) && (r < 0xff00 || r > 0xffef) {
		return classLatin
	}
	return classNone
}

// tailor appends to dst the opportunities of text: Chrome's between two
// tailored characters, the UAX #14 ones elsewhere; uax yields those in offset
// order, one at a time, so that they are not kept.
func tailor(text string, uax func() (Break, bool), dst []Break) []Break {
	prev, before := uint8(classNone), uint8(classNone)
	prevLast := rune(0)
	cur, have := uax()
	for i := 0; i < len(text); {
		r, size := rune(text[i]), 1
		var cls uint8
		if r < utf8.RuneSelf {
			cls = asciiClassOf[r]
		} else {
			r, size = utf8.DecodeRuneInString(text[i:])
			cls = classOf(r)
		}
		// Letters side by side, and a letter or digit before a space, never
		// break: the common cases cost one test.
		if i > 0 && (prev != classLetter || cls != classLetter && cls != classSpace) {
			for have && cur.Offset < i {
				cur, have = uax()
			}
			switch {
			case prev == classSpace && cls == classNone && breaksAfterSpace(r):
				// Chrome breaks after a space before anything, closing
				// punctuation included (UAX #14 LB13 would not).
				dst = append(dst, Break{Offset: i})
			case prev == classNone || cls == classNone:
				if have && cur.Offset == i {
					dst = append(dst, cur)
				}
			case chromeBreak(prev, cls, prevLast, before):
				dst = append(dst, Break{Offset: i})
			}
		}
		before, prev, prevLast = prev, cls, r
		i += size
	}
	return dst
}

// breaksAfterSpace reports whether a break after a space before r (which is
// not ASCII) is Chrome's: it is for final quotes (» ” ’), which UAX #14 (LB13)
// keeps attached to what precedes even after spaces.
func breaksAfterSpace(r rune) bool {
	return unicode.Is(unicode.Pf, r)
}

// chromeBreak reports whether Chrome offers a break between last (class prev)
// and a character of class cls; before is the class of the character before last.
func chromeBreak(prev, cls uint8, last rune, before uint8) bool {
	if last == '-' && cls == classDigit {
		// "1-2" breaks after the hyphen, "?-2" does not: the hyphen is a sign.
		return before == classLetter || before == classDigit
	}
	return asciiBreak[prev]>>cls&1 == 1
}

// invalidOpportunities is the path of texts that are not valid UTF-8, which
// InitWithString rejects: they are segmented as runes, and the opportunities
// come back in bytes of the original text.
func invalidOpportunities(seg *segmenter.Segmenter, text string) func() (Break, bool) {
	runes := []rune(text)
	seg.Init(runes)
	offsets := byteOffsets(text, runes)
	it := seg.LineIterator()
	return func() (Break, bool) {
		for it.Next() {
			l := it.Line()
			if end := l.Offset + len(l.Text); end < len(runes) {
				return Break{Offset: int(clusterOffset(offsets, end)), Mandatory: l.IsMandatoryBreak}, true
			}
		}
		return Break{}, false
	}
}

// byteOffsets maps rune indexes to byte offsets; nil means they are equal.
func byteOffsets(text string, runes []rune) []int32 {
	if len(text) == len(runes) {
		return nil
	}
	out := make([]int32, 0, len(runes)+1)
	for i := range text {
		out = append(out, clampInt32(i))
	}
	return append(out, clampInt32(len(text)))
}

// clusterOffset clamps like clampInt32: a text is bounded by MaxInputBytes, far
// below 2 GiB.
func clusterOffset(offsets []int32, runeIdx int) int32 {
	if offsets == nil {
		return clampInt32(runeIdx)
	}
	return offsets[runeIdx]
}
