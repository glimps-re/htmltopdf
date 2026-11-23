package pdf

import (
	"errors"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/textsafe"
)

// MaxCoord is the largest absolute value accepted by the number formatter.
// It keeps every operand far below the real-number range viewers accept.
const MaxCoord = 1e9

// MaxTitleBytes is the length of /Title after sanitizing, in bytes.
const MaxTitleBytes = 256

// ErrNumber is returned for a NaN, an infinity or a value beyond MaxCoord.
var ErrNumber = errors.New("pdf: number out of range")

// validNum reports whether v is finite and within MaxCoord.
func validNum(v float64) bool { return !math.IsNaN(v) && math.Abs(v) <= MaxCoord }

// appendNum is the only function that formats a real number into the PDF.
// It rejects non-finite values and writes at most four decimals, never an
// exponent.
func appendNum(dst []byte, v float64) ([]byte, error) {
	if !validNum(v) {
		return dst, ErrNumber
	}
	start := len(dst)
	dst = strconv.AppendFloat(dst, v, 'f', 4, 64)
	end := len(dst)
	for dst[end-1] == '0' {
		end--
	}
	if dst[end-1] == '.' {
		end--
	}
	dst = dst[:end]
	if string(dst[start:]) == "-0" {
		dst = append(dst[:start], '0')
	}
	return dst, nil
}

// hexDigits are the upper-case digits of PDF hex strings.
const hexDigits = "0123456789ABCDEF"

// appendHex16 appends v as four hexadecimal digits.
func appendHex16(dst []byte, v uint16) []byte {
	return append(dst, hexDigits[v>>12], hexDigits[v>>8&0xf], hexDigits[v>>4&0xf], hexDigits[v&0xf])
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// appendString is the only function that encodes a PDF text string: printable
// ASCII becomes an escaped literal string, anything else UTF-16BE with a BOM
// as a hex string. Invalid UTF-8 is replaced by U+FFFD.
func appendString(dst []byte, s string) []byte {
	if isPrintableASCII(s) {
		dst = append(dst, '(')
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(', ')', '\\':
				dst = append(dst, '\\')
			}
			dst = append(dst, s[i])
		}
		return append(dst, ')')
	}
	dst = append(dst, "<FEFF"...)
	var units [2]uint16
	for _, r := range s {
		for _, u := range utf16.AppendRune(units[:0], r) {
			dst = appendHex16(dst, u)
		}
	}
	return append(dst, '>')
}

// sanitizeTitle removes unsafe characters (textsafe), replaces invalid UTF-8
// with U+FFFD and stops once MaxTitleBytes are produced, on a rune boundary.
func sanitizeTitle(s string) string {
	out := make([]byte, 0, min(len(s), MaxTitleBytes))
	for _, r := range s {
		if textsafe.IsUnsafe(r) {
			continue
		}
		if len(out)+utf8.RuneLen(r) > MaxTitleBytes {
			break
		}
		out = utf8.AppendRune(out, r)
		if len(out) == MaxTitleBytes {
			break
		}
	}
	return string(out)
}
