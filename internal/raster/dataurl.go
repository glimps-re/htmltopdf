package raster

import (
	"bufio"
	"encoding/base64"
	"io"
	"strings"
)

// parseDataURL splits src into the base64 payload of a data: URL, following
// the data: URL processor of the Fetch standard: the scheme and the media type
// are case-insensitive, parameters are ignored and ";base64" is required. The
// payload is a substring of src: nothing is copied.
func parseDataURL(src string) (string, Reason) {
	s := trimSpace(src)
	const scheme = "data:"
	if len(s) < len(scheme) || !strings.EqualFold(s[:len(scheme)], scheme) {
		return "", ReasonScheme
	}
	meta, payload, found := strings.Cut(s[len(scheme):], ",")
	if !found {
		return "", ReasonEncoding
	}
	mediaType, _, _ := strings.Cut(meta, ";")
	switch strings.ToLower(trimSpace(mediaType)) {
	case "image/png", "image/jpeg", "image/jpg":
	default:
		return "", ReasonType
	}
	if !hasBase64Suffix(meta) {
		return "", ReasonEncoding
	}
	return payload, 0
}

// hasBase64Suffix reports whether meta ends with ";", spaces and "base64"
// (any case), white space trimmed, like the Fetch standard.
func hasBase64Suffix(meta string) bool {
	meta = trimSpace(meta)
	const b64 = "base64"
	if len(meta) < len(b64) || !strings.EqualFold(meta[len(meta)-len(b64):], b64) {
		return false
	}
	meta = strings.TrimRight(meta[:len(meta)-len(b64)], " ")
	return strings.HasSuffix(meta, ";")
}

// trimSpace trims the ASCII white space of HTML: unlike strings.TrimSpace it
// leaves the Unicode spaces, which a URL does not ignore.
func trimSpace(s string) string {
	return strings.Trim(s, " \t\n\f\r")
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

var base64Alphabet = func() (t [256]bool) {
	for _, c := range []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/") {
		t[c] = true
	}
	return t
}()

// checkBase64 validates p as forgiving-base64 (WHATWG Infra): white space is
// ignored, padding is optional but at most two '=' may only end a complete
// quantum. It returns the number of '=' to append so that a padded decoder
// accepts the text.
func checkBase64(p string) (pad int, ok bool) {
	n, eq := 0, 0
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case isSpace(c):
		case c == '=':
			eq++
		case eq > 0 || !base64Alphabet[c]:
			return 0, false
		default:
			n++
		}
	}
	switch {
	case eq > 2, eq > 0 && (n+eq)%4 != 0, n%4 == 1:
		return 0, false
	case eq > 0:
		return 0, true
	}
	return (4 - n%4) % 4, true
}

// payloadReader yields the payload without its white space, then pad '='.
type payloadReader struct {
	s   string
	pad int
}

func (r *payloadReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) && len(r.s) > 0 {
		i := 0
		for i < len(r.s) && n < len(p) && !isSpace(r.s[i]) {
			p[n] = r.s[i]
			n++
			i++
		}
		for i < len(r.s) && isSpace(r.s[i]) {
			i++
		}
		r.s = r.s[i:]
	}
	for n < len(p) && r.pad > 0 {
		p[n] = '='
		n++
		r.pad--
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// readBufSize is the buffer between the base64 decoder and the image
// decoders, which issue many small reads.
const readBufSize = 4096

// open returns a reader of the decoded bytes of s. Each call decodes the
// payload again from its start: nothing but the buffer is allocated.
func (s *Source) open() *bufio.Reader {
	return bufio.NewReaderSize(base64.NewDecoder(base64.StdEncoding, &payloadReader{s: s.payload, pad: s.pad}), readBufSize)
}
