package css

// TrimSpace returns toks without its leading and trailing whitespace tokens.
func TrimSpace(toks []Token) []Token { return trimSpace(toks) }

// Fields splits toks at top-level whitespace, ignoring whitespace inside
// blocks. At most 128 fields are split: the rest comes back as one more field,
// so that a hostile value costs a bounded number of slices.
func Fields(toks []Token) [][]Token { return fields(toks) }

// LowerASCII lowercases the ASCII letters of s only. It is the one fold CSS
// keywords and names use: Unicode folding would read U+212A (Kelvin sign) as k.
func LowerASCII(s string) string { return lowerASCII(s) }

// SkipBlock returns the index of the token after toks[i], past the whole block
// when toks[i] opens one (a function, parenthesis, bracket or brace).
func SkipBlock(toks []Token, i int) int { return next(toks, i) }
