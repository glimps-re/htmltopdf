// Package pdf is a minimal streaming PDF 1.7 writer.
//
// Objects are written as soon as they are complete; only the xref offsets, the
// page list and the set of opacities used are kept in memory. Every byte goes
// through a counting writer that charges budget.OutputBytes, every number and
// string through one encoder each (see design §8.3).
package pdf

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// Limits enforced by the writer.
const (
	// MaxPageSize is the largest page side in points (200 inches, the PDF 1.7
	// implementation limit).
	MaxPageSize = 14400
	// MaxURIBytes is the maximum length of a link URI.
	MaxURIBytes = 2048
)

// Errors returned by the Writer.
var (
	ErrClosed          = errors.New("pdf: writer closed")
	ErrNoPages         = errors.New("pdf: no page")
	ErrPageSize        = errors.New("pdf: invalid page size")
	ErrInvalidLink     = errors.New("pdf: invalid link")
	ErrNotDeferred     = errors.New("pdf: page has no pending deferred content")
	ErrPendingContent  = errors.New("pdf: deferred content never written")
	ErrNilContent      = errors.New("pdf: nil content")
	errObjectRewritten = errors.New("pdf: object written twice")
)

// Ref is an indirect object number (generation is always 0).
type Ref uint32

const (
	catalogRef Ref = iota + 1
	pagesRef
	resourcesRef
)

// Info is the document information; /Producer is fixed.
type Info struct{ Title string }

// Link is a URI link annotation. Rect is [x0 y0 x1 y1] in default user space.
type Link struct {
	Rect [4]float64
	URI  string
}

type countingWriter struct {
	w   io.Writer
	b   *budget.Budget
	sum hash.Hash
	n   int64
	err error
}

func (c *countingWriter) write(p []byte) error {
	if c.err != nil {
		return c.err
	}
	if len(p) == 0 {
		return nil
	}
	if err := c.b.Charge(budget.OutputBytes, int64(len(p))); err != nil {
		c.err = err
		return err
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.sum.Write(p[:n])
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	c.err = err
	return err
}

var zlibPool = sync.Pool{New: func() any { return zlib.NewWriter(io.Discard) }}

// Writer writes one PDF document. It is not safe for concurrent use.
type Writer struct {
	out      countingWriter
	obj      []byte
	z        bytes.Buffer
	offsets  []int64 // by object number; 0 = not written yet
	next     Ref     // last object number handed out
	frame    []byte
	pages    []Ref
	deferred map[Ref]Ref // page -> reserved content stream
	opacity  opacitySet
	fonts    []*fontEntry
	fontRefs []nameRef
	shadings shadingSet
	started  bool
	closed   bool
}

// NewWriter returns a Writer that writes to w and charges every byte to b.
func NewWriter(w io.Writer, b *budget.Budget) *Writer {
	return &Writer{
		out:      countingWriter{w: w, b: b, sum: sha256.New()},
		offsets:  make([]int64, resourcesRef+1),
		next:     resourcesRef,
		deferred: map[Ref]Ref{},
	}
}

// Written returns the number of bytes written to the underlying writer.
func (w *Writer) Written() int64 { return w.out.n }

func (w *Writer) reserve() Ref {
	w.offsets = append(w.offsets, 0)
	w.next++
	return w.next
}

func (w *Writer) begin() error {
	if w.started {
		return nil
	}
	w.started = true
	return w.out.write([]byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"))
}

func appendRef(dst []byte, r Ref) []byte {
	dst = strconv.AppendUint(dst, uint64(r), 10)
	return append(dst, " 0 R"...)
}

func appendRect(dst []byte, r [4]float64) ([]byte, error) {
	dst = append(dst, '[')
	for i, v := range r {
		if i > 0 {
			dst = append(dst, ' ')
		}
		var err error
		if dst, err = appendNum(dst, v); err != nil {
			return dst, err
		}
	}
	return append(dst, ']'), nil
}

// mark records the offset of ref and starts its object header in w.frame.
func (w *Writer) mark(ref Ref) error {
	if w.offsets[ref] != 0 {
		return errObjectRewritten
	}
	w.offsets[ref] = w.out.n
	w.frame = strconv.AppendUint(w.frame[:0], uint64(ref), 10)
	w.frame = append(w.frame, " 0 obj\n"...)
	return nil
}

// emit writes the object ref with the given body in a single write.
func (w *Writer) emit(ref Ref, body []byte) error {
	if err := w.mark(ref); err != nil {
		return err
	}
	w.frame = append(w.frame, body...)
	w.frame = append(w.frame, "\nendobj\n"...)
	return w.out.write(w.frame)
}

// emitStream compresses data and writes it as stream object ref. The
// compressed bytes are written from w.z without another copy.
func (w *Writer) emitStream(ref Ref, data []byte, extra string) error {
	w.z.Reset()
	zw, ok := zlibPool.Get().(*zlib.Writer)
	if !ok {
		return errors.New("pdf: zlib pool corrupted")
	}
	defer func() {
		zw.Reset(io.Discard)
		zlibPool.Put(zw)
	}()
	zw.Reset(&w.z)
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := w.mark(ref); err != nil {
		return err
	}
	w.frame = append(w.frame, "<< /Filter /FlateDecode "...)
	w.frame = append(w.frame, extra...)
	w.frame = append(w.frame, "/Length "...)
	w.frame = strconv.AppendInt(w.frame, int64(w.z.Len()), 10)
	w.frame = append(w.frame, " >>\nstream\n"...)
	if err := w.out.write(w.frame); err != nil {
		return err
	}
	if err := w.out.write(w.z.Bytes()); err != nil {
		return err
	}
	return w.out.write([]byte("\nendstream\nendobj\n"))
}

func validateLink(l Link) error {
	for _, v := range l.Rect {
		if !validNum(v) {
			return ErrInvalidLink
		}
	}
	if len(l.URI) > MaxURIBytes || !isPrintableASCII(l.URI) {
		return ErrInvalidLink
	}
	scheme, _, ok := strings.Cut(l.URI, ":")
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto":
		if ok {
			return nil
		}
	}
	return ErrInvalidLink
}

func (w *Writer) emitLink(l Link) (Ref, error) {
	ref := w.reserve()
	w.obj = append(w.obj[:0], "<< /Type /Annot /Subtype /Link /F 4 /Border [0 0 0] /Rect "...)
	var err error
	if w.obj, err = appendRect(w.obj, l.Rect); err != nil {
		return 0, err
	}
	w.obj = append(w.obj, " /A << /S /URI /URI "...)
	w.obj = appendString(w.obj, l.URI)
	w.obj = append(w.obj, " >> >>"...)
	return ref, w.emit(ref, w.obj)
}

func validContent(c *Content) error {
	switch {
	case c == nil:
		return ErrNilContent
	case c.err != nil:
		return c.err
	case c.depth != 0:
		return ErrUnbalanced
	}
	return nil
}

// AddPage writes a page of widthPt x heightPt points with its link
// annotations and returns its reference. When deferred is false body is
// written now. When true the page gets a second content stream, supplied
// later with WriteDeferred: a non-nil body is written now as the first one
// (/Contents [body deferred]), a nil body leaves the deferred stream the only one.
// The content cap applies to each stream. Nothing is written when an argument is
// invalid. After a write or budget error the Writer is unusable: the error is
// sticky.
func (w *Writer) AddPage(widthPt, heightPt float64, body *Content, deferred bool, links []Link) (Ref, error) {
	if w.closed {
		return 0, ErrClosed
	}
	if !(widthPt > 0 && widthPt <= MaxPageSize && heightPt > 0 && heightPt <= MaxPageSize) {
		return 0, ErrPageSize
	}
	if !deferred || body != nil {
		if err := validContent(body); err != nil {
			return 0, err
		}
		if err := w.mergeFonts(body); err != nil {
			return 0, err
		}
	}
	for _, l := range links {
		if err := validateLink(l); err != nil {
			return 0, err
		}
	}
	if err := w.begin(); err != nil {
		return 0, err
	}
	annots := make([]Ref, 0, len(links))
	for _, l := range links {
		ref, err := w.emitLink(l)
		if err != nil {
			return 0, err
		}
		annots = append(annots, ref)
	}
	page, contents := w.reserve(), w.reserve()
	var first Ref
	if deferred {
		w.deferred[page] = contents
		if body != nil {
			first = w.reserve()
			if err := w.writeContent(first, body); err != nil {
				return 0, err
			}
		}
	} else if err := w.writeContent(contents, body); err != nil {
		return 0, err
	}
	w.obj = append(w.obj[:0], "<< /Type /Page /Parent "...)
	w.obj = appendRef(w.obj, pagesRef)
	w.obj = append(w.obj, " /MediaBox [0 0 "...)
	var err error
	if w.obj, err = appendNum(w.obj, widthPt); err != nil {
		return 0, err
	}
	w.obj = append(w.obj, ' ')
	if w.obj, err = appendNum(w.obj, heightPt); err != nil {
		return 0, err
	}
	w.obj = append(w.obj, "] /Resources "...)
	w.obj = appendRef(w.obj, resourcesRef)
	w.obj = append(w.obj, " /Contents "...)
	if first != 0 {
		w.obj = append(w.obj, '[')
		w.obj = appendRef(w.obj, first)
		w.obj = append(w.obj, ' ')
		w.obj = appendRef(w.obj, contents)
		w.obj = append(w.obj, ']')
	} else {
		w.obj = appendRef(w.obj, contents)
	}
	if len(annots) > 0 {
		w.obj = append(w.obj, " /Annots ["...)
		for i, a := range annots {
			if i > 0 {
				w.obj = append(w.obj, ' ')
			}
			w.obj = appendRef(w.obj, a)
		}
		w.obj = append(w.obj, ']')
	}
	w.obj = append(w.obj, " >>"...)
	if err := w.emit(page, w.obj); err != nil {
		return 0, err
	}
	w.pages = append(w.pages, page)
	return page, nil
}

func (w *Writer) writeContent(ref Ref, c *Content) error {
	w.opacity.merge(c.opacities)
	return w.emitStream(ref, c.buf, "")
}

// WriteDeferred writes the content stream of a page added with deferred set.
// Like AddPage, it leaves the Writer unusable after a write or budget error.
func (w *Writer) WriteDeferred(ref Ref, c *Content) error {
	if w.closed {
		return ErrClosed
	}
	contents, ok := w.deferred[ref]
	if !ok {
		return ErrNotDeferred
	}
	if err := validContent(c); err != nil {
		return err
	}
	if err := w.mergeFonts(c); err != nil {
		return err
	}
	delete(w.deferred, ref)
	return w.writeContent(contents, c)
}

// Close writes the page tree, the shared resources, the document information,
// the xref table and the trailer. The writer is unusable afterwards.
func (w *Writer) Close(info Info) error {
	if w.closed {
		return ErrClosed
	}
	w.closed = true
	switch {
	case len(w.pages) == 0:
		return ErrNoPages
	case len(w.deferred) > 0:
		return ErrPendingContent
	}
	infoRef := w.reserve()
	var err error
	if w.fontRefs, err = w.closeFonts(); err != nil {
		return err
	}
	if err = w.closeShadings(); err != nil {
		return err
	}
	if err := w.closeResources(); err != nil {
		return err
	}
	if err := w.closePages(); err != nil {
		return err
	}
	if err := w.closeCatalog(); err != nil {
		return err
	}
	if err := w.closeInfo(infoRef, info); err != nil {
		return err
	}
	return w.closeTrailer(infoRef)
}

func (w *Writer) closeCatalog() error {
	w.obj = append(w.obj[:0], "<< /Type /Catalog /Pages "...)
	w.obj = appendRef(w.obj, pagesRef)
	w.obj = append(w.obj, " >>"...)
	return w.emit(catalogRef, w.obj)
}

func (w *Writer) closeInfo(ref Ref, info Info) error {
	w.obj = append(w.obj[:0], "<< /Producer (htmltopdf)"...)
	if t := sanitizeTitle(info.Title); t != "" {
		w.obj = append(w.obj, " /Title "...)
		w.obj = appendString(w.obj, t)
	}
	w.obj = append(w.obj, " >>"...)
	return w.emit(ref, w.obj)
}

func (w *Writer) closeResources() error {
	w.obj = append(w.obj[:0], "<< /ProcSet [/PDF]"...)
	first := true
	var err error
	for t := uint8(0); ; t++ {
		if w.opacity.has(t) {
			if first {
				w.obj = append(w.obj, " /ExtGState <<"...)
				first = false
			}
			w.obj = append(w.obj, " /GT"...)
			w.obj = strconv.AppendInt(w.obj, int64(t), 10)
			w.obj = append(w.obj, " << /Type /ExtGState /ca "...)
			a := float64(255-int(t)) / 255
			if w.obj, err = appendNum(w.obj, a); err != nil {
				return err
			}
			w.obj = append(w.obj, " /CA "...)
			if w.obj, err = appendNum(w.obj, a); err != nil {
				return err
			}
			w.obj = append(w.obj, " >>"...)
		}
		if t == 255 {
			break
		}
	}
	if !first {
		w.obj = append(w.obj, " >>"...)
	}
	w.obj = appendNameDict(w.obj, "Shading", w.shadings.refs)
	w.obj = appendNameDict(w.obj, "Font", w.fontRefs)
	w.obj = append(w.obj, " >>"...)
	return w.emit(resourcesRef, w.obj)
}

func (w *Writer) closePages() error {
	w.obj = append(w.obj[:0], "<< /Type /Pages /Count "...)
	w.obj = strconv.AppendInt(w.obj, int64(len(w.pages)), 10)
	w.obj = append(w.obj, " /Kids ["...)
	for i, p := range w.pages {
		if i > 0 {
			w.obj = append(w.obj, ' ')
		}
		w.obj = appendRef(w.obj, p)
	}
	w.obj = append(w.obj, "] >>"...)
	return w.emit(pagesRef, w.obj)
}

func (w *Writer) closeTrailer(infoRef Ref) error {
	for ref := 1; ref < len(w.offsets); ref++ {
		if w.offsets[ref] == 0 {
			return fmt.Errorf("pdf: object %d never written", ref)
		}
	}
	xrefAt := w.out.n
	w.obj = append(w.obj[:0], "xref\n0 "...)
	w.obj = strconv.AppendInt(w.obj, int64(len(w.offsets)), 10)
	w.obj = append(w.obj, "\n0000000000 65535 f \n"...)
	for _, off := range w.offsets[1:] {
		w.obj = appendXrefEntry(w.obj, off)
		if len(w.obj) >= 4096 {
			if err := w.out.write(w.obj); err != nil {
				return err
			}
			w.obj = w.obj[:0]
		}
	}
	if err := w.out.write(w.obj); err != nil {
		return err
	}
	id := hex.EncodeToString(w.out.sum.Sum(nil)[:16])
	w.obj = append(w.obj[:0], "trailer\n<< /Size "...)
	w.obj = strconv.AppendInt(w.obj, int64(len(w.offsets)), 10)
	w.obj = append(w.obj, " /Root "...)
	w.obj = appendRef(w.obj, catalogRef)
	w.obj = append(w.obj, " /Info "...)
	w.obj = appendRef(w.obj, infoRef)
	w.obj = append(w.obj, " /ID [<"...)
	w.obj = append(w.obj, id...)
	w.obj = append(w.obj, "> <"...)
	w.obj = append(w.obj, id...)
	w.obj = append(w.obj, ">] >>\nstartxref\n"...)
	w.obj = strconv.AppendInt(w.obj, xrefAt, 10)
	w.obj = append(w.obj, "\n%%EOF\n"...)
	return w.out.write(w.obj)
}

func appendXrefEntry(dst []byte, off int64) []byte {
	var digits [20]byte
	d := strconv.AppendInt(digits[:0], off, 10)
	for i := len(d); i < 10; i++ {
		dst = append(dst, '0')
	}
	dst = append(dst, d...)
	return append(dst, " 00000 n \n"...)
}

// appendNameDict appends " /key << /name ref ... >>" to a resource dictionary;
// nothing when refs is empty.
func appendNameDict(dst []byte, key string, refs []nameRef) []byte {
	if len(refs) == 0 {
		return dst
	}
	dst = append(dst, " /"...)
	dst = append(dst, key...)
	dst = append(dst, " <<"...)
	for _, r := range refs {
		dst = append(dst, " /"...)
		dst = append(dst, r.name...)
		dst = append(dst, ' ')
		dst = appendRef(dst, r.ref)
	}
	return append(dst, " >>"...)
}
