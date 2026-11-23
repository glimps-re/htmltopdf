package css

// maxParts bounds the parts splitTop and fields produce: no accepted value has
// more than a few arguments or MaxGradientStops stops, and a hostile function
// with 200 000 arguments must not allocate a slice per argument. Past the
// bound the rest is returned as one more part, so that every caller sees a
// count it rejects.
const maxParts = 2 * MaxGradientStops

// next returns the index of the token after toks[i], skipping the whole block
// when toks[i] opens one.
func next(toks []Token, i int) int {
	if isOpener(toks[i].Type) && toks[i].End > 1 {
		return i + int(toks[i].End)
	}
	return i + 1
}

// function splits a value made of exactly one function, such as rgb(0 0 0),
// into its lowercase name and the tokens of its arguments. An unclosed
// function at the end of the input is accepted as CSS Syntax §5 requires.
func function(toks []Token) (name string, args []Token, ok bool) {
	toks = trimSpace(toks)
	if len(toks) == 0 || toks[0].Type != TFunction || int(toks[0].End) != len(toks) {
		return "", nil, false
	}
	args = toks[1:]
	if closed(args) {
		args = args[:len(args)-1]
	}
	return lowerASCII(toks[0].Text), args, true
}

// closed reports whether the last token of args is the closer of the function
// they belong to, rather than the closer of a nested block.
func closed(args []Token) bool {
	for i := 0; i < len(args); {
		if i == len(args)-1 {
			return args[i].Type == TRParen
		}
		i = next(args, i)
	}
	return false
}

// splitTop splits args at the commas that are not inside a nested block. Each
// part is trimmed of whitespace.
func splitTop(args []Token) [][]Token {
	var parts [][]Token
	for lo, hi := range commaRanges(args) {
		if len(parts) == maxParts {
			return append(parts, trimSpace(args[lo:]))
		}
		parts = append(parts, trimSpace(args[lo:hi]))
	}
	return parts
}

// fields splits toks at top-level whitespace, ignoring whitespace inside
// nested blocks.
func fields(toks []Token) [][]Token {
	var out [][]Token
	start := -1
	i := 0
	for ; i < len(toks) && len(out) < maxParts; i++ {
		if toks[i].Type == TWhitespace {
			if start >= 0 {
				out = append(out, toks[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
		i = next(toks, i) - 1
	}
	if i < len(toks) {
		// Over the bound: the rest is one more part, whatever it holds.
		start = max(start, i)
		if rest := trimSpace(toks[start:]); len(rest) > 0 {
			out = append(out, rest)
		}
		return out
	}
	if start >= 0 {
		out = append(out, toks[start:])
	}
	return out
}
