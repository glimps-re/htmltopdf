package style

import (
	"math"
	"strconv"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

func trim(toks []css.Token) []css.Token { return css.TrimSpace(toks) }

func fields(toks []css.Token) [][]css.Token { return css.Fields(toks) }

func lowerASCII(s string) string { return css.LowerASCII(s) }

func skip(toks []css.Token, i int) int { return css.SkipBlock(toks, i) }

// maxParts bounds the parts splitOn makes: the rest of a hostile value comes
// back as one more part, which every caller rejects or ignores.
const maxParts = 128

// splitOn splits toks at the top-level tokens for which sep is true.
func splitOn(toks []css.Token, sep func(css.Token) bool) [][]css.Token {
	var out [][]css.Token
	start := 0
	for i := 0; i < len(toks) && len(out) < maxParts; {
		if sep(toks[i]) {
			out = append(out, trim(toks[start:i]))
			start = i + 1
			i++
			continue
		}
		i = skip(toks, i)
	}
	return append(out, trim(toks[start:]))
}

func isComma(t css.Token) bool { return t.Type == css.TComma }

func isSlash(t css.Token) bool { return t.Type == css.TDelim && t.Text == "/" }

// hasVar reports whether toks holds a var() function at any depth.
func hasVar(toks []css.Token) bool {
	for i := range toks {
		if toks[i].Type == css.TFunction && eqFold(toks[i].Text, "var") {
			return true
		}
	}
	return false
}

// eqFold compares s with the lowercase ASCII word lower, ignoring ASCII case
// only (no Unicode folding: U+212A KELVIN SIGN must not match "k").
func eqFold(s, lower string) bool {
	if len(s) != len(lower) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}

// ident returns the ASCII-lowercased text of a lone identifier token.
func ident(toks []css.Token) (string, bool) {
	toks = trim(toks)
	if len(toks) != 1 || toks[0].Type != css.TIdent {
		return "", false
	}
	return lowerASCII(toks[0].Text), true
}

// withEnds returns toks with the End of every opening token recomputed, for a
// slice assembled by var() substitution.
func withEnds(toks []css.Token) []css.Token {
	var stack []int
	for i := range toks {
		switch toks[i].Type {
		case css.TFunction, css.TLParen, css.TLBracket, css.TLBrace:
			stack = append(stack, i)
		case css.TRParen, css.TRBracket, css.TRBrace:
			if n := len(stack); n > 0 {
				toks[stack[n-1]].End = clamp32(i - stack[n-1] + 1)
				stack = stack[:n-1]
			}
		}
	}
	for _, i := range stack {
		toks[i].End = clamp32(len(toks) - i)
	}
	return toks
}

func clamp32(i int) int32 { return int32(min(max(i, 0), math.MaxInt32)) }

// describe renders the start of a value for a warning detail.
func describe(toks []css.Token) string {
	var sb strings.Builder
	for i := range toks {
		if sb.Len() >= budget.MaxDetailBytes {
			break
		}
		t := &toks[i]
		switch t.Type {
		case css.TWhitespace:
			sb.WriteByte(' ')
		case css.THash:
			sb.WriteString("#" + t.Text)
		case css.TFunction:
			sb.WriteString(t.Text + "(")
		case css.TString:
			sb.WriteString(strconv.Quote(t.Text))
		case css.TNumber, css.TPercentage, css.TDimension:
			var buf [32]byte
			sb.Write(strconv.AppendFloat(buf[:0], float64(t.Num), 'g', -1, 32))
			if t.Type == css.TPercentage {
				sb.WriteByte('%')
			}
			sb.WriteString(t.Text)
		case css.TComma:
			sb.WriteByte(',')
		case css.TRParen:
			sb.WriteByte(')')
		default:
			sb.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}
