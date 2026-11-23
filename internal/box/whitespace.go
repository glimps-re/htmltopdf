package box

import (
	"strings"
	"unicode/utf8"
)

// wsState is the white space state of the line being built.
type wsState struct {
	// space is true when the last character of the line is a collapsible space,
	// or the line is empty: the next collapsible white space is dropped.
	space bool
	// col is the number of characters since the start of the line, for tab stops.
	col int
}

// tabStops is a slice source for the spaces a tab expands to.
const tabStops = "        "

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }

// textSink receives the result of processText.
type textSink interface {
	// text gets a piece of processed text, never empty and without newline.
	text(s string)
	// lineBreak gets a preserved newline.
	lineBreak() error
}

// processText applies the white-space property to data (CSS Text 3 §4.1.1).
//
// With normal (pre false), white space collapses into at most one space across
// the whole line, whatever the inline boxes in between, and the spaces at the
// start of the line are dropped (the end of the line is trimmed by the
// caller). With pre, spaces are kept, newlines (LF, CR LF and CR) become line
// breaks, and a tab moves to the next multiple of tabSize characters: a tab
// can expand to tabSize spaces, so the text of a pre grows at most 8 times.
//
// Text that needs no change goes through as it is, without a copy.
func processText(data string, pre bool, st *wsState, sink textSink) error {
	if unchanged(data, pre, st) {
		sink.text(data)
		if pre {
			st.col += utf8.RuneCountInString(data)
		} else {
			st.space = data[len(data)-1] == ' '
		}
		return nil
	}
	var seg strings.Builder
	seg.Grow(len(data))
	flush := func() {
		if seg.Len() > 0 {
			sink.text(seg.String())
			// The text box owns the buffer now: start another, small, one.
			seg = strings.Builder{}
		}
	}
	for i := 0; i < len(data); {
		r, w := utf8.DecodeRuneInString(data[i:])
		i += w
		switch {
		case pre && (r == '\n' || r == '\r'):
			if r == '\r' && i < len(data) && data[i] == '\n' {
				i++
			}
			flush()
			if err := sink.lineBreak(); err != nil {
				return err
			}
			st.space, st.col = false, 0
		case !pre && isSpace(r):
			if !st.space {
				seg.WriteByte(' ')
				st.space = true
			}
		case pre && r == '\t':
			n := tabSize - st.col%tabSize
			seg.WriteString(tabStops[:n])
			st.col += n
		default:
			seg.WriteRune(r)
			if pre {
				st.col++
			}
			st.space = false
		}
	}
	flush()
	return nil
}

// unchanged reports whether processText would emit data exactly as it is, as a
// single piece: it holds no tab and no newline (in pre) and no white space that
// collapses or is dropped (otherwise).
func unchanged(data string, pre bool, st *wsState) bool {
	if data == "" {
		return false
	}
	prevSpace := st.space
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case pre && (c == '\n' || c == '\r' || c == '\t'):
			return false
		case !pre && c == ' ':
			if prevSpace {
				return false
			}
			prevSpace = true
		case !pre && (c == '\t' || c == '\n' || c == '\r' || c == '\f'):
			return false
		default:
			prevSpace = false
		}
	}
	return true
}
