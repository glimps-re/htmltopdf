package pdftest

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Font is an embedded Type0/CIDFontType2 font as read back from the PDF.
type Font struct {
	// BaseFont is the font name with its subset tag ("ABCDEF+Name").
	BaseFont string
	// Program is the decoded FontFile2.
	Program []byte
	// ToUnicode maps glyph ids to text.
	ToUnicode map[uint16]string
	// Widths maps glyph ids to their /W width, in thousandths of the size.
	Widths map[uint16]int
}

func (r *reader) stream(v any) (*Stream, error) {
	o, err := r.resolve(v)
	if err != nil {
		return nil, err
	}
	st, ok := o.(*Stream)
	if !ok {
		return nil, fmt.Errorf("expected a stream, got %T", o)
	}
	return st, nil
}

// fonts reads the /Font resources of res.
func (r *reader) fonts(res Dict) (map[string]*Font, error) {
	fd, ok := res["Font"]
	if !ok {
		return nil, nil
	}
	dict, err := r.dict(fd)
	if err != nil {
		return nil, err
	}
	out := map[string]*Font{}
	for name, ref := range dict {
		if f, ok := r.fontCache[name]; ok {
			out[name] = f
			continue
		}
		f, err := r.font(ref)
		if err != nil {
			return nil, fmt.Errorf("font %s: %w", name, err)
		}
		r.fontCache[name], out[name] = f, f
	}
	return out, nil
}

func (r *reader) font(ref any) (*Font, error) {
	t0, err := r.dict(ref)
	if err != nil {
		return nil, err
	}
	if t0["Type"] != Name("Font") || t0["Subtype"] != Name("Type0") || t0["Encoding"] != Name("Identity-H") {
		return nil, errors.New("not a Type0 font with Identity-H")
	}
	kids, ok := t0["DescendantFonts"].([]any)
	if !ok || len(kids) != 1 {
		return nil, errors.New("Type0 font needs one descendant")
	}
	cid, err := r.dict(kids[0])
	if err != nil {
		return nil, err
	}
	if cid["Subtype"] != Name("CIDFontType2") || cid["CIDToGIDMap"] != Name("Identity") || t0["BaseFont"] != cid["BaseFont"] {
		return nil, errors.New("descendant is not a CIDFontType2 with the same BaseFont and an identity CIDToGIDMap")
	}
	f := &Font{}
	base, _ := t0["BaseFont"].(Name)
	f.BaseFont = string(base)
	if len(f.BaseFont) < 8 || f.BaseFont[6] != '+' || strings.Trim(f.BaseFont[:6], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return nil, fmt.Errorf("BaseFont %q has no subset tag", f.BaseFont)
	}
	desc, err := r.dict(cid["FontDescriptor"])
	if err != nil {
		return nil, err
	}
	if desc["Type"] != Name("FontDescriptor") || desc["FontName"] != t0["BaseFont"] {
		return nil, errors.New("bad font descriptor")
	}
	ff, err := r.stream(desc["FontFile2"])
	if err != nil {
		return nil, err
	}
	if f.Program, err = inflate(ff.Raw); err != nil {
		return nil, err
	}
	if ff.Dict["Length1"] != float64(len(f.Program)) {
		return nil, errors.New("/Length1 does not match the font program")
	}
	if f.Widths, err = r.widths(cid["W"]); err != nil {
		return nil, err
	}
	tu, err := r.stream(t0["ToUnicode"])
	if err != nil {
		return nil, err
	}
	cmap, err := inflate(tu.Raw)
	if err != nil {
		return nil, err
	}
	if f.ToUnicode, err = parseToUnicode(cmap); err != nil {
		return nil, err
	}
	return f, nil
}

// widths decodes a /W array: "first [w1 w2 ...]" groups.
func (r *reader) widths(v any) (map[uint16]int, error) {
	o, err := r.resolve(v)
	if err != nil {
		return nil, err
	}
	arr, ok := o.([]any)
	if !ok {
		return nil, errors.New("/W is not an array")
	}
	out := map[uint16]int{}
	for i := 0; i < len(arr); i += 2 {
		first, ok1 := arr[i].(float64)
		if i+1 >= len(arr) || !ok1 || !(first >= 0 && first <= 0xffff) || first != math.Trunc(first) {
			return nil, errors.New("malformed /W")
		}
		ws, ok := arr[i+1].([]any)
		if !ok {
			return nil, errors.New("malformed /W group")
		}
		for k, w := range ws {
			n, ok := w.(float64)
			g := int(first) + k
			if !ok || g < 0 || g > 0xffff {
				return nil, errors.New("malformed /W entry")
			}
			out[uint16(g)] = int(n)
		}
	}
	return out, nil
}

// parseToUnicode reads the bfchar entries of a ToUnicode CMap.
func parseToUnicode(cmap []byte) (map[uint16]string, error) {
	out := map[uint16]string{}
	if !bytes.Contains(cmap, []byte("begincmap")) || !bytes.Contains(cmap, []byte("endcmap")) {
		return nil, errors.New("ToUnicode is not a CMap")
	}
	in := false
	for _, line := range strings.Split(string(cmap), "\n") {
		switch {
		case strings.HasSuffix(line, " beginbfchar"):
			in = true
		case line == "endbfchar":
			in = false
		case in:
			var src, dst string
			if _, err := fmt.Sscanf(line, "<%4s> <%s", &src, &dst); err != nil || !strings.HasSuffix(dst, ">") {
				return nil, fmt.Errorf("bad bfchar line %q", line)
			}
			g, err := strconv.ParseUint(src, 16, 16)
			if err != nil {
				return nil, err
			}
			dst = strings.TrimSuffix(dst, ">")
			if len(dst)%4 != 0 {
				return nil, fmt.Errorf("bad UTF-16 in %q", line)
			}
			var units []uint16
			for i := 0; i < len(dst); i += 4 {
				u, err := strconv.ParseUint(dst[i:i+4], 16, 16)
				if err != nil {
					return nil, err
				}
				units = append(units, uint16(u))
			}
			out[uint16(g)] = string(utf16.Decode(units))
		}
	}
	return out, nil
}

// extractText interprets the text operators of a content stream and returns
// the text of each BT...ET block.
func extractText(content []byte, fonts map[string]*Font) ([]string, error) {
	p := &parser{data: content}
	var (
		operands []any
		cur      *Font
		blocks   []string
		block    strings.Builder
	)
	show := func(s string) error {
		if cur == nil || len(s)%2 != 0 {
			return errors.New("text shown without a font or with an odd code length")
		}
		for i := 0; i < len(s); i += 2 {
			g := uint16(s[i])<<8 | uint16(s[i+1])
			block.WriteString(cur.ToUnicode[g])
		}
		return nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.data) {
			return blocks, nil
		}
		c := p.data[p.pos]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '\'' || c == '"' {
			op := p.token()
			switch op {
			case "true", "false", "null":
				operands = append(operands, op)
				continue
			case "BT":
				block.Reset()
			case "ET":
				blocks = append(blocks, block.String())
			case "Tf":
				if len(operands) < 2 {
					return nil, errors.New("operator Tf needs two operands")
				}
				n, _ := operands[len(operands)-2].(Name)
				if cur = fonts[string(n)]; cur == nil {
					return nil, fmt.Errorf("operator Tf selects unknown font %q", n)
				}
			case "Tj":
				if s, ok := lastString(operands); ok {
					if err := show(s); err != nil {
						return nil, err
					}
				} else {
					return nil, errors.New("operator Tj without a string")
				}
			case "TJ":
				var arr []any
				if len(operands) > 0 {
					arr, _ = operands[len(operands)-1].([]any)
				}
				if arr == nil {
					return nil, errors.New("operator TJ without an array")
				}
				for _, e := range arr {
					if s, ok := e.(string); ok {
						if err := show(s); err != nil {
							return nil, err
						}
					}
				}
			}
			operands = operands[:0]
			continue
		}
		v, err := p.value(0)
		if err != nil {
			return nil, fmt.Errorf("content: %w", err)
		}
		if len(operands) < 64 {
			operands = append(operands, v)
		}
	}
}

func lastString(ops []any) (string, bool) {
	if len(ops) == 0 {
		return "", false
	}
	s, ok := ops[len(ops)-1].(string)
	return s, ok
}
