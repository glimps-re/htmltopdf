package style

import "github.com/glimps-re/htmltopdf/internal/css"

// fontShorthand parses the font shorthand (CSS Fonts 4 §2.8). small-caps,
// stretch keywords other than normal and system fonts are invalid. The
// longhands it covers and does not set take their initial value.
func fontShorthand(toks []css.Token) ([]propSpec, bool) {
	toks = trim(toks)
	style := spec{kind: specInitial}
	weight := spec{kind: specInitial}
	var haveStyle, haveWeight bool
	i, prefix := 0, 0
	for ; i < len(toks); i++ {
		t := toks[i]
		if t.Type == css.TWhitespace {
			continue
		}
		if prefix == 3 {
			break
		}
		if t.Type != css.TNumber && t.Type != css.TIdent {
			break
		}
		if t.Type == css.TIdent {
			kw := lowerASCII(t.Text)
			if kw == "normal" {
				prefix++
				continue
			}
			if n, ok := fontStyleKeywords[kw]; ok && !haveStyle {
				style, haveStyle = spec{kind: specValue, n: n}, true
				prefix++
				continue
			}
		}
		if s, ok := parseFontWeight(toks[i : i+1]); ok && !haveWeight {
			weight, haveWeight = s, true
			weight.kind = specValue
			prefix++
			continue
		}
		break
	}
	i = skipSpace(toks, i)
	if i >= len(toks) {
		return nil, false
	}
	size, ok := parseFontSize(toks[i : i+1])
	if !ok {
		return nil, false
	}
	size.kind = specValue
	i = skipSpace(toks, i+1)
	height := spec{kind: specInitial}
	if i < len(toks) && isSlash(toks[i]) {
		i = skipSpace(toks, i+1)
		if i >= len(toks) {
			return nil, false
		}
		h, ok := parseLineHeight(toks[i : i+1])
		if !ok {
			return nil, false
		}
		height, height.kind = h, specValue
		i = skipSpace(toks, i+1)
	}
	family, ok := parseFontFamily(toks[i:])
	if !ok {
		return nil, false
	}
	family.kind = specValue
	return []propSpec{
		{pFontStyle, style},
		{pFontWeight, weight},
		{pFontSize, size},
		{pLineHeight, height},
		{pFontFamily, family},
		{pFontVariantNumeric, spec{kind: specInitial}},
	}, true
}

func skipSpace(toks []css.Token, i int) int {
	for i < len(toks) && toks[i].Type == css.TWhitespace {
		i++
	}
	return i
}
