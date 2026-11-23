// Package pdftest is an independent reader and validator for the PDFs the
// writer produces. It is imported by tests only.
//
// It understands the subset the writer emits (classic xref, no object streams,
// Flate only) and rejects everything else, so a structural mistake cannot
// hide behind a lenient parser. ValidatePDFCPU adds a second, unrelated check.
package pdftest

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	maxDepth    = 32
	maxInflated = 64 << 20
)

// Name is a PDF name without its leading slash.
type Name string

// Ref is an indirect reference.
type Ref int

// Dict is a PDF dictionary.
type Dict map[string]any

// Stream is a stream object with its raw (still encoded) data.
type Stream struct {
	Dict Dict
	Raw  []byte
}

// Link is a URI link annotation.
type Link struct {
	Rect [4]float64
	URI  string
}

// Page is a parsed page with its decoded content stream.
type Page struct {
	MediaBox [4]float64
	Content  []byte
	Links    []Link
}

// Doc is a structurally valid document.
type Doc struct {
	Pages []Page

	// Title is the decoded /Title, empty when absent.
	Title string
	// ID is the first element of the trailer /ID, hex encoded.
	ID string
	// Objects is the number of in-use objects.
	Objects int
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) skipWS() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\n', '\r', '\t', 0, '\f':
			p.pos++
		default:
			return
		}
	}
}

func isDelim(c byte) bool {
	return strings.IndexByte(" \n\r\t\x00\f()<>[]{}/%", c) >= 0
}

func (p *parser) token() string {
	p.skipWS()
	start := p.pos
	for p.pos < len(p.data) && !isDelim(p.data[p.pos]) {
		p.pos++
	}
	return string(p.data[start:p.pos])
}

func (p *parser) expect(s string) error {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(s)) {
		return fmt.Errorf("offset %d: expected %q", p.pos, s)
	}
	p.pos += len(s)
	return nil
}

func (p *parser) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("nesting too deep")
	}
	p.skipWS()
	if p.pos >= len(p.data) {
		return nil, io.ErrUnexpectedEOF
	}
	switch c := p.data[p.pos]; {
	case c == '<' && bytes.HasPrefix(p.data[p.pos:], []byte("<<")):
		return p.dict(depth)
	case c == '<':
		return p.hexString()
	case c == '[':
		p.pos++
		var arr []any
		for {
			p.skipWS()
			if p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.pos++
				return arr, nil
			}
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
	case c == '(':
		return p.literalString()
	case c == '/':
		p.pos++
		return p.name()
	default:
		return p.scalar()
	}
}

func (p *parser) name() (Name, error) {
	start := p.pos
	for p.pos < len(p.data) && !isDelim(p.data[p.pos]) {
		p.pos++
	}
	raw := string(p.data[start:p.pos])
	if strings.Contains(raw, "#") {
		return "", fmt.Errorf("unsupported name escape %q", raw)
	}
	return Name(raw), nil
}

func (p *parser) dict(depth int) (Dict, error) {
	p.pos += 2
	d := Dict{}
	for {
		p.skipWS()
		if bytes.HasPrefix(p.data[p.pos:], []byte(">>")) {
			p.pos += 2
			return d, nil
		}
		if p.pos >= len(p.data) || p.data[p.pos] != '/' {
			return nil, fmt.Errorf("offset %d: dictionary key must be a name", p.pos)
		}
		p.pos++
		k, err := p.name()
		if err != nil {
			return nil, err
		}
		if _, dup := d[string(k)]; dup {
			return nil, fmt.Errorf("duplicate key /%s", k)
		}
		if d[string(k)], err = p.value(depth + 1); err != nil {
			return nil, err
		}
	}
}

func (p *parser) hexString() (string, error) {
	end := bytes.IndexByte(p.data[p.pos:], '>')
	if end < 0 {
		return "", io.ErrUnexpectedEOF
	}
	hex := string(p.data[p.pos+1 : p.pos+end])
	p.pos += end + 1
	if len(hex)%2 != 0 {
		return "", errors.New("odd hex string")
	}
	out := make([]byte, len(hex)/2)
	for i := range out {
		v, err := strconv.ParseUint(hex[2*i:2*i+2], 16, 8)
		if err != nil {
			return "", err
		}
		out[i] = byte(v)
	}
	return string(out), nil
}

func (p *parser) literalString() (string, error) {
	p.pos++
	var out []byte
	for nest := 1; p.pos < len(p.data); p.pos++ {
		switch c := p.data[p.pos]; c {
		case '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", io.ErrUnexpectedEOF
			}
			e := p.data[p.pos]
			if m := strings.IndexByte("nrtbf", e); m >= 0 {
				out = append(out, "\n\r\t\b\f"[m])
			} else {
				out = append(out, e)
			}
		case '(':
			nest++
			out = append(out, c)
		case ')':
			if nest--; nest == 0 {
				p.pos++
				return string(out), nil
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return "", io.ErrUnexpectedEOF
}

func (p *parser) scalar() (any, error) {
	tok := p.token()
	switch tok {
	case "":
		return nil, fmt.Errorf("offset %d: unexpected character %q", p.pos, p.data[p.pos])
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	n, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return nil, fmt.Errorf("bad token %q", tok)
	}
	if strings.ContainsAny(tok, ".eE") {
		return n, nil
	}
	save := p.pos
	if gen := p.token(); gen == "0" && p.token() == "R" {
		return Ref(n), nil
	}
	p.pos = save
	return n, nil
}

// Parse validates the structure of data and returns the document.
func Parse(data []byte) (*Doc, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-1.7\n%")) || !bytes.HasSuffix(data, []byte("\n%%EOF\n")) {
		return nil, errors.New("bad header or trailer marker")
	}
	r := &reader{data: data, cache: map[int]any{}, seen: map[int]bool{}}
	if err := r.readXref(); err != nil {
		return nil, fmt.Errorf("xref: %w", err)
	}
	doc, err := r.document()
	if err != nil {
		return nil, err
	}
	for n := 1; n < len(r.offsets); n++ {
		if !r.seen[n] {
			return nil, fmt.Errorf("object %d unreachable from the trailer", n)
		}
	}
	doc.Objects = len(r.offsets) - 1
	return doc, nil
}

type reader struct {
	data    []byte
	offsets []int
	trailer Dict
	cache   map[int]any
	seen    map[int]bool
}

func (r *reader) readXref() error {
	i := bytes.LastIndex(r.data, []byte("startxref\n"))
	if i < 0 {
		return errors.New("no startxref")
	}
	p := &parser{data: r.data, pos: i + len("startxref\n")}
	at, err := strconv.Atoi(p.token())
	if err != nil || at <= 0 || at >= i {
		return errors.New("bad startxref")
	}
	if err := p.expect("\n%%EOF"); err != nil {
		return err
	}
	p = &parser{data: r.data, pos: at}
	if err := p.expect("xref\n0 "); err != nil {
		return err
	}
	size, err := strconv.Atoi(p.token())
	if err != nil || size < 2 || size-1 > (len(r.data)-p.pos)/20 {
		return errors.New("bad xref size")
	}
	if !bytes.HasPrefix(r.data[p.pos:], []byte("\n0000000000 65535 f \n")) {
		return errors.New("bad free entry")
	}
	p.pos += len("\n0000000000 65535 f \n")
	r.offsets = make([]int, size)
	for n := 1; n < size; n++ {
		if p.pos+20 > len(r.data) {
			return io.ErrUnexpectedEOF
		}
		e := r.data[p.pos : p.pos+20]
		if string(e[10:]) != " 00000 n \n" {
			return fmt.Errorf("bad entry %d", n)
		}
		off, err := strconv.Atoi(string(e[:10]))
		if err != nil || off < 10 || off >= at {
			return fmt.Errorf("bad offset in entry %d", n)
		}
		r.offsets[n] = off
		p.pos += 20
	}
	if err := p.expect("trailer\n"); err != nil {
		return err
	}
	v, err := p.value(0)
	if err != nil {
		return err
	}
	var ok bool
	if r.trailer, ok = v.(Dict); !ok {
		return errors.New("trailer is not a dictionary")
	}
	if err := p.expect("\nstartxref\n"); err != nil || p.pos != i+len("startxref\n") {
		return errors.New("junk between trailer and startxref")
	}
	return nil
}

func (r *reader) object(n int) (any, error) {
	if n <= 0 || n >= len(r.offsets) {
		return nil, fmt.Errorf("reference to missing object %d", n)
	}
	if v, ok := r.cache[n]; ok {
		return v, nil
	}
	p := &parser{data: r.data, pos: r.offsets[n]}
	if err := p.expect(strconv.Itoa(n) + " 0 obj\n"); err != nil {
		return nil, fmt.Errorf("object %d: %w", n, err)
	}
	v, err := p.value(0)
	if err != nil {
		return nil, fmt.Errorf("object %d: %w", n, err)
	}
	if d, ok := v.(Dict); ok && bytes.HasPrefix(r.data[p.pos:], []byte("\nstream\n")) {
		p.pos += len("\nstream\n")
		length, _ := d["Length"].(float64)
		if !(length >= 0 && length <= float64(len(r.data)-p.pos)) || length != math.Trunc(length) {
			return nil, fmt.Errorf("object %d: bad /Length", n)
		}
		end := p.pos + int(length)
		v = &Stream{Dict: d, Raw: r.data[p.pos:end]}
		p.pos = end
		if err := p.expect("\nendstream"); err != nil {
			return nil, fmt.Errorf("object %d: /Length does not reach endstream: %w", n, err)
		}
	}
	if !bytes.HasPrefix(r.data[p.pos:], []byte("\nendobj\n")) {
		return nil, fmt.Errorf("object %d: missing endobj", n)
	}
	r.cache[n] = v
	return v, nil
}

// resolve follows a reference, marking the object reachable.
func (r *reader) resolve(v any) (any, error) {
	ref, ok := v.(Ref)
	if !ok {
		return v, nil
	}
	r.seen[int(ref)] = true
	return r.object(int(ref))
}

func (r *reader) dict(v any) (Dict, error) {
	o, err := r.resolve(v)
	if err != nil {
		return nil, err
	}
	d, ok := o.(Dict)
	if !ok {
		return nil, fmt.Errorf("expected a dictionary, got %T", o)
	}
	return d, nil
}

func (r *reader) rect(v any) ([4]float64, error) {
	var out [4]float64
	o, err := r.resolve(v)
	if err != nil {
		return out, err
	}
	arr, ok := o.([]any)
	if !ok || len(arr) != 4 {
		return out, errors.New("expected a 4-number array")
	}
	for i, e := range arr {
		if out[i], ok = e.(float64); !ok {
			return out, errors.New("rectangle holds a non-number")
		}
	}
	return out, nil
}

func (r *reader) document() (*Doc, error) {
	t := r.trailer
	if t == nil || t["Size"] != float64(len(r.offsets)) {
		return nil, errors.New("trailer /Size does not match the xref")
	}
	doc := &Doc{}
	id, ok := t["ID"].([]any)
	if !ok || len(id) != 2 || id[0] != id[1] {
		return nil, errors.New("trailer /ID must be two equal strings")
	}
	sum := sha256.Sum256(r.data[:bytes.LastIndex(r.data, []byte("trailer\n"))])
	if id[0] != string(sum[:16]) {
		return nil, errors.New("/ID is not the SHA-256 prefix of the bytes before the trailer")
	}
	doc.ID = fmt.Sprintf("%x", sum[:16])
	root, err := r.dict(t["Root"])
	if err != nil {
		return nil, err
	}
	if root["Type"] != Name("Catalog") {
		return nil, errors.New("root is not a /Catalog")
	}
	pagesRef := root["Pages"]
	tree, err := r.dict(pagesRef)
	if err != nil {
		return nil, err
	}
	kids, _ := tree["Kids"].([]any)
	if tree["Type"] != Name("Pages") || tree["Count"] != float64(len(kids)) || len(kids) == 0 {
		return nil, errors.New("bad /Pages node")
	}
	for _, k := range kids {
		pg, err := r.page(k, pagesRef)
		if err != nil {
			return nil, err
		}
		doc.Pages = append(doc.Pages, *pg)
	}
	if info, err := r.dict(t["Info"]); err != nil {
		return nil, err
	} else if s, ok := info["Title"].(string); ok {
		if doc.Title, err = decodeText(s); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

func (r *reader) page(k, parent any) (*Page, error) {
	d, err := r.dict(k)
	if err != nil {
		return nil, err
	}
	if d["Type"] != Name("Page") || d["Parent"] != parent {
		return nil, errors.New("bad /Page")
	}
	pg := &Page{}
	if pg.MediaBox, err = r.rect(d["MediaBox"]); err != nil {
		return nil, fmt.Errorf("MediaBox: %w", err)
	}
	if pg.MediaBox[0] != 0 || pg.MediaBox[1] != 0 || pg.MediaBox[2] <= 0 || pg.MediaBox[3] <= 0 {
		return nil, errors.New("degenerate MediaBox")
	}
	res, err := r.dict(d["Resources"])
	if err != nil {
		return nil, err
	}
	so, err := r.resolve(d["Contents"])
	if err != nil {
		return nil, err
	}
	st, ok := so.(*Stream)
	if !ok || st.Dict["Filter"] != Name("FlateDecode") {
		return nil, errors.New("/Contents is not a Flate stream")
	}
	if pg.Content, err = inflate(st.Raw); err != nil {
		return nil, err
	}
	if err := checkContent(pg.Content, res, r); err != nil {
		return nil, err
	}

	if annots, ok := d["Annots"].([]any); ok {
		for _, a := range annots {
			l, err := r.link(a)
			if err != nil {
				return nil, err
			}
			pg.Links = append(pg.Links, *l)
		}
	}
	return pg, nil
}

func (r *reader) link(a any) (*Link, error) {
	d, err := r.dict(a)
	if err != nil {
		return nil, err
	}
	if d["Type"] != Name("Annot") || d["Subtype"] != Name("Link") {
		return nil, errors.New("annotation is not a link")
	}
	l := &Link{}
	if l.Rect, err = r.rect(d["Rect"]); err != nil {
		return nil, err
	}
	act, err := r.dict(d["A"])
	if err != nil {
		return nil, err
	}
	if act["S"] != Name("URI") || len(act) != 2 {
		return nil, errors.New("link action is not a plain URI action")
	}
	uri, ok := act["URI"].(string)
	if !ok {
		return nil, errors.New("URI is not a string")
	}
	l.URI = uri
	return l, nil
}

func inflate(raw []byte) ([]byte, error) {
	fr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("inflate: %w", err)
	}
	var out bytes.Buffer
	n, err := io.Copy(&out, io.LimitReader(fr, maxInflated+1))
	if cerr := fr.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("inflate: %w", err)
	}
	if n > maxInflated {
		return nil, errors.New("inflated stream too large")
	}
	return out.Bytes(), nil
}

// checkContent verifies q/Q balance and that every gs name is a resource.
func checkContent(content []byte, res Dict, r *reader) error {
	gs := Dict{}
	if v, ok := res["ExtGState"]; ok {
		var err error
		if gs, err = r.dict(v); err != nil {
			return err
		}
	}
	depth := 0
	toks := bytes.Fields(content)
	for i, t := range toks {
		switch string(t) {
		case "q":
			depth++
		case "Q":
			if depth--; depth < 0 {
				return errors.New("content: Q without q")
			}
		case "gs":
			if i == 0 || !bytes.HasPrefix(toks[i-1], []byte("/")) {
				return errors.New("content: gs without a name")
			}
			if _, ok := gs[string(toks[i-1][1:])]; !ok {
				return fmt.Errorf("content: ExtGState %s missing from resources", toks[i-1])
			}
		}
	}
	if depth != 0 {
		return errors.New("content: unbalanced q/Q")
	}
	return nil
}

func decodeText(s string) (string, error) {
	if !strings.HasPrefix(s, "\xfe\xff") {
		return s, nil
	}
	b := []byte(s[2:])
	if len(b)%2 != 0 {
		return "", errors.New("odd UTF-16 length")
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(units)), nil
}
