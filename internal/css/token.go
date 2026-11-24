// Package css tokenizes and parses CSS into compact tokens and a rule tree.
//
// The lexer is github.com/tdewolff/parse/v2/css over parse.NewInputBytes; the
// parser follows the error recovery of CSS Syntax Level 3 §5 and never fails on
// malformed input: it drops the offending rule or declaration and reports it
// with budget.Warn, so a caller that must be strict (ExtraCSS, the UA sheet)
// only has to check that no warning was recorded.
//
// Input is hostile. Text is valid UTF-8 without controls other than tab, LF
// and CR; nesting of blocks and functions is bounded by MaxNesting; the size of
// every source is charged to budget.CSSBytes before it is read.
package css

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Type is the kind of a Token.
type Type uint8

// Token types. Brackets and parentheses are kept as tokens; for every opening
// token (TFunction, TLParen, TLBracket, TLBrace) Token.End gives the extent of
// the block.
const (
	TIdent Type = iota + 1
	TFunction
	TAtKeyword
	THash
	TString
	TBadString
	TURL
	TBadURL
	TDelim
	TNumber
	TPercentage
	TDimension
	TUnicodeRange
	TMatch
	TWhitespace
	TCDO
	TCDC
	TColon
	TSemicolon
	TComma
	TLBracket
	TRBracket
	TLParen
	TRParen
	TLBrace
	TRBrace
)

// Unit is the unit of a TDimension or TPercentage token.
type Unit uint8

// Units. UnitOther keeps the original spelling in Token.Text.
const (
	UnitNone Unit = iota
	UnitPx
	UnitPt
	UnitPc
	UnitIn
	UnitCm
	UnitMm
	UnitQ
	UnitEm
	UnitRem
	UnitEx
	UnitCh
	UnitVw
	UnitVh
	UnitVmin
	UnitVmax
	UnitPercent
	UnitDeg
	UnitRad
	UnitGrad
	UnitTurn
	UnitS
	UnitMs
	UnitFr
	UnitDpi
	UnitDpcm
	UnitDppx
	UnitOther
)

var unitNames = map[string]Unit{
	"px": UnitPx, "pt": UnitPt, "pc": UnitPc, "in": UnitIn, "cm": UnitCm, "mm": UnitMm, "q": UnitQ,
	"em": UnitEm, "rem": UnitRem, "ex": UnitEx, "ch": UnitCh, "vw": UnitVw, "vh": UnitVh,
	"vmin": UnitVmin, "vmax": UnitVmax, "deg": UnitDeg, "rad": UnitRad, "grad": UnitGrad,
	"turn": UnitTurn, "s": UnitS, "ms": UnitMs, "fr": UnitFr, "dpi": UnitDpi, "dpcm": UnitDpcm,
	"dppx": UnitDppx, "x": UnitDppx,
}

// Token is one CSS token, 32 bytes.
//
//   - Ident, Function, AtKeyword, Hash, String, URL, Delim, UnicodeRange, Match:
//     Text is the unescaped value without delimiters ("rgb" for "rgb(", "id" for
//     "#id", the target of url(...)). Hash has FlagID in Flags when it is a valid id.
//   - Number, Percentage, Dimension: Num is the value (finite, clamped to the
//     float32 range); Dimension carries Unit, and Text only for UnitOther.
//   - Opening tokens: End is the number of tokens from this one to just after the
//     matching closer (or to the end of the slice when unclosed).
type Token struct {
	Type  Type
	Unit  Unit
	Flags uint8
	Num   float32
	End   int32
	Text  string
}

// FlagID is set on a Hash token that is a valid identifier, usable as an id
// selector.
const FlagID = 1

// FlagInt is set on a Number token written as an integer: no fraction, no
// exponent (CSS Syntax 3 §4.3.12 "integer" type flag).
const FlagInt = 2

func closerOf(t Type) Type {
	switch t {
	case TLBracket:
		return TRBracket
	case TLBrace:
		return TRBrace
	}
	return TRParen
}

func isOpener(t Type) bool {
	return t == TFunction || t == TLParen || t == TLBracket || t == TLBrace
}

func isCloser(t Type) bool { return t == TRParen || t == TRBracket || t == TRBrace }

func isHex(c byte) bool { return hexVal(c) >= 0 }

// hexVal returns the value of the hexadecimal digit c, or -1.
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// unescape resolves CSS escapes (CSS Syntax §4.3.7). In a string, an escaped
// newline is a line continuation and is dropped.
func unescape(s string, inString bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			if !inString {
				b.WriteRune(utf8.RuneError)
			}
			break
		}
		if hexVal(s[i]) >= 0 {
			v, n := 0, 0
			for n < 6 && i < len(s) && hexVal(s[i]) >= 0 {
				v = v<<4 | hexVal(s[i])
				i++
				n++
			}
			if i < len(s) && isCSSSpace(s[i]) {
				if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
					i++
				}
				i++
			}
			if v == 0 || v > utf8.MaxRune || (v >= 0xd800 && v <= 0xdfff) {
				v = utf8.RuneError
			}
			b.WriteRune(rune(v))
			continue
		}
		if inString && (s[i] == '\n' || s[i] == '\f' || s[i] == '\r') {
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += w
	}
	return b.String()
}

// isControl reports whether r is a control character other than tab, LF and CR.
func isControl(r rune) bool {
	return r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r >= 0x7f && r <= 0x9f
}

// needsClean reports whether s holds invalid UTF-8 or a control character
// other than tab, LF and CR.
func needsClean(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 {
			if isControl(rune(c)) {
				return true
			}
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && w == 1 || isControl(r) {
			return true
		}
		i += w - 1
	}
	return false
}

// clean replaces invalid UTF-8 and controls other than tab, LF and CR by
// U+FFFD (design §8.4); bidi and other format characters are kept for the
// drawing stage.
func clean(s string) string {
	if !needsClean(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isControl(r) {
			r = utf8.RuneError
		}
		b.WriteRune(r)
	}
	return b.String()
}

// text unescapes raw when needed and cleans it.
func text(raw string, inString bool) string {
	if strings.IndexByte(raw, '\\') >= 0 {
		raw = unescape(raw, inString)
	}
	return clean(raw)
}

// numEnd returns the length of the numeric prefix of s.
func numEnd(s string) int {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i+1 < len(s) && s[i] == '.' && s[i+1] >= '0' && s[i+1] <= '9' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && s[j] >= '0' && s[j] <= '9' {
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			i = j
		}
	}
	return i
}

// parseNum converts the numeric text n to a finite float32.
func parseNum(n string) float32 {
	v, err := strconv.ParseFloat(n, 32)
	if err != nil && !math.IsInf(v, 0) {
		return 0
	}
	return float32(max(min(v, math.MaxFloat32), -math.MaxFloat32))
}

func unitOf(u string) (Unit, string) {
	u = text(u, false)
	if k, ok := unitNames[lowerASCII(u)]; ok {
		return k, ""
	}
	return UnitOther, u
}

func isIdentStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c == '-' {
		return len(s) > 1 && (s[1] == '-' || s[1] == '_' || s[1] >= 'a' && s[1] <= 'z' || s[1] >= 'A' && s[1] <= 'Z' || s[1] >= 0x80 || s[1] == '\\')
	}
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 || c == '\\'
}
