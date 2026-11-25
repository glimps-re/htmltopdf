package box

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// Bounds of a link target.
const (
	// maxHrefBytes is the longest target kept, after percent-encoding.
	maxHrefBytes = 2048
	// maxSchemeBytes is the longest URI scheme read.
	maxSchemeBytes = 16
)

// cleanHref applies design §8.2 to the href attribute of an a element. It
// returns the target to draw. A link that is not drawn is either blocked, with
// a reason for the warning, or silently ignored (empty, or a fragment, which
// becomes an internal link in M3).
//
// Leading and trailing white space and C0 controls are trimmed; a target over
// maxHrefBytes is refused at once (the encoded form is never shorter); a control
// character left inside blocks the link (browsers would strip some of them and
// read "java\tscript:" as a script); the scheme is at most maxSchemeBytes
// long, lowercased, and must be http, https or mailto; bytes outside printable
// ASCII and spaces are percent-encoded. Nothing is done in proportion to more
// than maxHrefBytes of the attribute.
func cleanHref(raw string) (href, blocked string) {
	s := strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	if s == "" || s[0] == '#' {
		return "", ""
	}
	if len(s) > maxHrefBytes {
		return "", fmt.Sprintf("target over %d bytes", maxHrefBytes)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return "", "control character in the target"
		}
	}
	colon := strings.IndexByte(s[:min(len(s), maxSchemeBytes+1)], ':')
	if colon < 0 || !validScheme(s[:colon]) {
		return "", "no scheme"
	}
	scheme, rest := strings.ToLower(s[:colon]), s[colon+1:]
	switch scheme {
	case "http", "https", "mailto":
	default:
		return "", "scheme " + scheme
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(scheme + ":")
	const upperHex = "0123456789ABCDEF"
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c <= 0x20 || c >= 0x7f {
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xf])
		} else {
			b.WriteByte(c)
		}
		if b.Len() > maxHrefBytes {
			return "", fmt.Sprintf("%s target over %d bytes once encoded", scheme, maxHrefBytes)
		}
	}
	return b.String(), ""
}

// validScheme reports whether s is a URI scheme: a letter, then letters,
// digits, "+", "-" or ".".
func validScheme(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

// fragmentID returns the id an href "#id" points to, percent-decoded as
// browsers do: ok is false for any other href, and for an empty, over-long or
// control-character id (such a link is dropped).
func fragmentID(raw string) (id string, ok bool) {
	s := strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	if len(s) < 2 || s[0] != '#' || len(s) > maxHrefBytes {
		return "", false
	}
	id = s[1:]
	if strings.IndexByte(id, '%') >= 0 {
		if dec, err := url.PathUnescape(id); err == nil {
			id = dec
		}
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x20 || id[i] == 0x7f {
			return "", false
		}
	}
	return id, id != ""
}

// resolveLinks sets Anchor on the first box with each targeted id, in document
// order, and drops the internal links that point to no box. It visits each box
// at most twice, only when the document has internal links.
func (bd *builder) resolveLinks(root *Box) error {
	if len(bd.targets) == 0 {
		return nil
	}
	found := make(map[string]struct{}, len(bd.targets))
	var mark func(*Box) error
	mark = func(b *Box) error {
		if err := bd.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		if _, want := bd.targets[b.ID]; want && b.ID != "" {
			if _, dup := found[b.ID]; !dup {
				found[b.ID] = struct{}{}
				b.Anchor = true
			}
		}
		for _, c := range b.Children {
			if err := mark(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := mark(root); err != nil {
		return err
	}
	var drop func(*Box) error
	drop = func(b *Box) error {
		if err := bd.b.Charge(budget.Work, 1); err != nil {
			return err
		}
		if b.Href != "" && b.Href[0] == '#' {
			if _, ok := found[b.Href[1:]]; !ok {
				b.Href = ""
				bd.b.Warn(budget.WarnBlockedLink, "internal link without a target")
			}
		}
		for _, c := range b.Children {
			if err := drop(c); err != nil {
				return err
			}
		}
		return nil
	}
	return drop(root)
}
