package pdf

import (
	"bufio"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// ImageFilter is the encoding of an image's Data.
type ImageFilter uint8

// Image encodings.
const (
	// ImageFlate means Data writes raw 8-bit samples; the writer compresses them.
	ImageFlate ImageFilter = iota
	// ImageDCT means Data writes a complete JPEG stream, written as is. The
	// caller guarantees a baseline JPEG it encoded itself (never input bytes)
	// whose size and components match Width, Height and Gray.
	ImageDCT
)

// Limits of images.
const (
	// MaxImageSide bounds Image.Width and Image.Height.
	MaxImageSide = 16384
	// MaxImages bounds the distinct images of a document.
	MaxImages = 64
	// maxImagePixels bounds Width x Height: the deflate CPU is not otherwise
	// bounded here, as constant samples compress far below OutputBytes. It
	// equals raster.MaxPixels.
	maxImagePixels = 4 << 20
)

// Errors returned for images.
var (
	ErrImage         = errors.New("pdf: invalid image")
	ErrImageData     = errors.New("pdf: image data size mismatch")
	ErrTooManyImages = errors.New("pdf: too many images")
	ErrForeignImage  = errors.New("pdf: image of another writer")
)

// Image is an image XObject. Data and Alpha are called once each, by AddImage.
type Image struct {
	Width, Height int
	// Gray selects DeviceGray, else DeviceRGB.
	Gray   bool
	Filter ImageFilter
	// Data writes Height rows of Width x (1 or 3) bytes for ImageFlate, a
	// complete JPEG stream for ImageDCT.
	Data func(w io.Writer) error
	// Alpha, when non-nil, writes Height rows of Width bytes: an 8-bit /SMask.
	Alpha func(w io.Writer) error
}

// ImageID identifies an image added with Writer.AddImage. The zero value is
// invalid.
type ImageID struct{ e *imageEntry }

type imageEntry struct {
	owner *Writer
	index int
	ref   Ref
	// seen is the last content that drew the image (see shadingEntry.seen).
	seen *Content
}

func (e *imageEntry) name() string { return "Im" + strconv.Itoa(e.index) }

// outWriter adapts the counting writer, which charges the budget, to io.Writer.
type outWriter struct{ c *countingWriter }

func (o outWriter) Write(p []byte) (int, error) {
	if err := o.c.write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// exactWriter passes at most limit bytes to w and counts them.
type exactWriter struct {
	w     io.Writer
	limit int64
	n     int64
	over  bool
	// b is charged one Work unit per 64 bytes, which polls the context.
	b   *budget.Budget
	err error
}

func (e *exactWriter) Write(p []byte) (int, error) {
	if err := e.b.Charge(budget.Work, int64(len(p)+63)/64); err != nil {
		e.err = err
		return 0, err
	}
	if e.n+int64(len(p)) > e.limit {
		e.over = true
		return 0, ErrImageData
	}
	n, err := e.w.Write(p)
	e.n += int64(n)
	return n, err
}

// soiWriter checks that a JPEG stream starts with the SOI marker.
type soiWriter struct {
	w    io.Writer
	head [2]byte
	n    int64
}

func (s *soiWriter) Write(p []byte) (int, error) {
	for i := 0; i < len(p) && s.n+int64(i) < 2; i++ {
		s.head[s.n+int64(i)] = p[i]
	}
	n, err := s.w.Write(p)
	s.n += int64(n)
	return n, err
}

func (s *soiWriter) valid() bool { return s.n >= 2 && s.head == [2]byte{0xff, 0xd8} }

func validateImage(img Image) error {
	switch {
	case img.Width < 1 || img.Width > MaxImageSide || img.Height < 1 || img.Height > MaxImageSide:
		return fmt.Errorf("%w: %dx%d", ErrImage, img.Width, img.Height)
	case int64(img.Width)*int64(img.Height) > maxImagePixels:
		return fmt.Errorf("%w: %dx%d pixels", ErrImage, img.Width, img.Height)
	case img.Data == nil:
		return fmt.Errorf("%w: no data", ErrImage)
	case img.Filter != ImageFlate && img.Filter != ImageDCT:
		return fmt.Errorf("%w: filter %d", ErrImage, img.Filter)
	}
	return nil
}

// AddImage writes the image, and its soft mask when Alpha is set, now, without
// holding the compressed image in memory. The bytes are charged to
// OutputBytes and the samples to Work. Nothing is written when an argument is
// invalid; after a write, budget or Data/Alpha error the Writer is unusable:
// the error is sticky. Images are named in the order they are added. Data and
// Alpha must not call the Writer.
func (w *Writer) AddImage(img Image) (ImageID, error) {
	if w.closed {
		return ImageID{}, ErrClosed
	}
	if err := validateImage(img); err != nil {
		return ImageID{}, err
	}
	if len(w.images) >= MaxImages {
		return ImageID{}, ErrTooManyImages
	}
	if err := w.begin(); err != nil {
		return ImageID{}, err
	}
	e := &imageEntry{owner: w, index: len(w.images)}
	e.ref = w.reserve()
	lenRef := w.reserve()
	var maskRef, maskLenRef Ref
	if img.Alpha != nil {
		maskRef = w.reserve()
		maskLenRef = w.reserve()
	}
	w.images = append(w.images, e)
	if err := w.writeImage(e.ref, lenRef, maskRef, img); err != nil {
		return ImageID{}, w.poison(err)
	}
	if maskRef != 0 {
		mask := Image{Width: img.Width, Height: img.Height, Gray: true, Data: img.Alpha}
		if err := w.writeImage(maskRef, maskLenRef, 0, mask); err != nil {
			return ImageID{}, w.poison(err)
		}
	}
	return ImageID{e}, nil
}

// poison makes err sticky: a half-written object cannot be recovered.
// A write or budget error already recorded wins: a callback that ignores the
// error of its Write must not hide it.
func (w *Writer) poison(err error) error {
	if w.out.err == nil {
		w.out.err = err
	}
	return w.out.err
}

// The stream length is an indirect object written after the stream: the
// compressed size is unknown until the encoder has finished.
func (w *Writer) writeImage(ref, lenRef, maskRef Ref, img Image) error {
	if err := w.mark(ref); err != nil {
		return err
	}
	w.frame = append(w.frame, "<< /Type /XObject /Subtype /Image /Width "...)
	w.frame = strconv.AppendInt(w.frame, int64(img.Width), 10)
	w.frame = append(w.frame, " /Height "...)
	w.frame = strconv.AppendInt(w.frame, int64(img.Height), 10)
	channels := int64(3)
	if img.Gray {
		w.frame = append(w.frame, " /ColorSpace /DeviceGray"...)
		channels = 1
	} else {
		w.frame = append(w.frame, " /ColorSpace /DeviceRGB"...)
	}
	w.frame = append(w.frame, " /BitsPerComponent 8"...)
	if img.Filter == ImageDCT {
		w.frame = append(w.frame, " /Filter /DCTDecode"...)
	} else {
		w.frame = append(w.frame, " /Filter /FlateDecode"...)
	}
	if maskRef != 0 {
		w.frame = append(w.frame, " /SMask "...)
		w.frame = appendRef(w.frame, maskRef)
	}
	w.frame = append(w.frame, " /Length "...)
	w.frame = appendRef(w.frame, lenRef)
	w.frame = append(w.frame, " >>\nstream\n"...)
	if err := w.out.write(w.frame); err != nil {
		return err
	}
	start := w.out.n
	var err error
	if img.Filter == ImageDCT {
		err = w.streamDCT(img.Data)
	} else {
		err = w.streamFlate(img.Data, int64(img.Width)*int64(img.Height)*channels)
	}
	if err != nil {
		return err
	}
	length := w.out.n - start
	if err := w.out.write([]byte("\nendstream\nendobj\n")); err != nil {
		return err
	}
	w.obj = strconv.AppendInt(w.obj[:0], length, 10)
	return w.emit(lenRef, w.obj)
}

func (w *Writer) streamDCT(data func(io.Writer) error) error {
	s := soiWriter{w: outWriter{&w.out}}
	if err := data(&s); err != nil {
		return err
	}
	if !s.valid() {
		return fmt.Errorf("%w: not a JPEG stream", ErrImageData)
	}
	return nil
}

func (w *Writer) streamFlate(data func(io.Writer) error, want int64) error {
	zw, ok := zlibPool.Get().(*zlib.Writer)
	if !ok {
		return errors.New("pdf: zlib pool corrupted")
	}
	// Buffered: the deflate encoder flushes tiny chunks, each of which would
	// cost a budget charge, a hash update and a write.
	if w.imgBuf == nil {
		w.imgBuf = bufio.NewWriterSize(outWriter{&w.out}, 32<<10)
	}
	w.imgBuf.Reset(outWriter{&w.out})
	defer func() {
		zw.Reset(io.Discard)
		zlibPool.Put(zw)
	}()
	zw.Reset(w.imgBuf)
	ew := exactWriter{w: zw, limit: want, b: w.out.b}
	err := data(&ew)
	if ew.err != nil {
		return ew.err
	}
	if err != nil {
		return err
	}
	if ew.over || ew.n != want {
		return fmt.Errorf("%w: %d bytes, want %d", ErrImageData, ew.n, want)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return w.imgBuf.Flush()
}

// checkImages refuses a content that draws an image of another writer.
func (w *Writer) checkImages(c *Content) error {
	for _, e := range c.images {
		if e.owner != w {
			return ErrForeignImage
		}
	}
	return nil
}

func (w *Writer) imageRefs() []nameRef {
	if len(w.images) == 0 {
		return nil
	}
	refs := make([]nameRef, len(w.images))
	for i, e := range w.images {
		refs[i] = nameRef{name: e.name(), ref: e.ref}
	}
	return refs
}
