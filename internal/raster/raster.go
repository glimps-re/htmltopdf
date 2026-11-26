// Package raster validates and decodes the images of a document: data: URLs
// holding PNG or JPEG, bounded before any decoding.
//
// Set.Add reads only the header of an image and estimates the memory its
// decoding needs; Decode decodes the pixels once the image is drawn. Both
// treat the src as hostile: every size read from a header is checked against
// the caps below before any pixel buffer is allocated.
package raster

import (
	"fmt"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// Format is the encoding of an image, decided by its magic bytes.
type Format uint8

// Supported formats.
const (
	PNG Format = iota + 1
	JPEG
)

// Image limits. They are internal constants: the public Limits still bound a
// render through MaxWork, Timeout and MaxInputBytes.
const (
	// MaxSide bounds each side of an image, in pixels: it bounds per-row buffers.
	MaxSide = 16384
	// MaxPixels bounds the pixels of one image.
	MaxPixels = 4 << 20
	// MaxDecodeBytes bounds the memory that decoding one image needs, as
	// estimated from its header.
	MaxDecodeBytes = 32 << 20
	// MaxImages bounds the distinct images of a render; duplicates are free.
	MaxImages = 64
	// MaxTotalPixels bounds the sum of the pixels of the distinct images of a render.
	MaxTotalPixels = 16 << 20
	// MaxJPEGScans bounds the SOS markers of a JPEG: the CPU of a progressive
	// decode grows with W×H×scans.
	MaxJPEGScans = 32
	// MaxJPEGMarkers bounds the markers of a JPEG, restart markers excluded.
	MaxJPEGMarkers = 4096
	// WorkPerPixel is the Work charged per pixel for decoding and re-encoding
	// an image, multiplied for a JPEG by ⌈scans/4⌉ and for a 16-bit PNG by 2.
	// Calibrated so that one unit stays below 20 ns on every variant.
	WorkPerPixel = 5
	// JPEGQuality is the quality of the re-encoded JPEG images.
	JPEGQuality = 90
)

// headerWork is the Work charged by Add for reading a header, on top of the
// unit per 64 bytes of src.
const headerWork = 16

// payloadBytesPerWork and rowWork add to the Work of Decode the cost of
// inflating or Huffman-decoding the payload and the per-row overhead of the
// decoders and writers.
const (
	payloadBytesPerWork = 1
	rowWork             = 6
)

// Reason says why an image is not drawn; String is the warning detail.
type Reason uint8

// Rejection reasons.
const (
	ReasonScheme   Reason = iota + 1 // not a data: URL
	ReasonType                       // media type other than image/png, image/jpeg, image/jpg
	ReasonEncoding                   // not ;base64, or invalid base64
	ReasonFormat                     // unknown magic, unsupported variant, header error, scan/marker cap
	ReasonSize                       // MaxSide, MaxPixels or MaxDecodeBytes
	ReasonCount                      // MaxImages distinct images
	ReasonTotal                      // MaxTotalPixels
	ReasonCorrupt                    // decoding failed or panicked after a valid header
)

var reasonNames = [...]string{
	ReasonScheme:   "scheme",
	ReasonType:     "type",
	ReasonEncoding: "encoding",
	ReasonFormat:   "format",
	ReasonSize:     "size",
	ReasonCount:    "count",
	ReasonTotal:    "total",
	ReasonCorrupt:  "corrupt",
}

// String returns the keyword of r: scheme, type, encoding, format, size,
// count, total or corrupt.
func (r Reason) String() string {
	if r == 0 || int(r) >= len(reasonNames) {
		return fmt.Sprintf("Reason(%d)", uint8(r))
	}
	return reasonNames[r]
}

// RejectError reports an image that is not drawn. It is not a budget error.
type RejectError struct{ Reason Reason }

// Error implements error.
func (e *RejectError) Error() string {
	return "raster: image rejected: " + e.Reason.String()
}

func reject(r Reason) error { return &RejectError{Reason: r} }

// Source is an image whose header was validated; its pixels are not decoded.
// Immutable; shared by every box with the same src.
type Source struct {
	Format        Format
	Width, Height int // pixels, in [1, MaxSide], Width*Height <= MaxPixels

	payload string // base64 text, white space included; a substring of src
	pad     int    // '=' appended to payload to complete the last quantum
	cost    int64  // estimated bytes allocated by the decoder
	area    int64  // pixels the decoder reconstructs: a JPEG's whole MCUs
	scans   int    // SOS markers of a JPEG
	deep    bool   // 16-bit PNG samples
	gray    bool
}

// work is the Work charged by Decode for s: the pixels the decoder
// reconstructs, plus the entropy-coded bytes and the rows, whose cost does not
// depend on the pixel count (a 1×16384 image, a JPEG of noise at q100).
func (s *Source) work() int64 {
	w := max(int64(s.Width)*int64(s.Height), s.area) * WorkPerPixel
	switch {
	case s.Format == JPEG:
		w *= int64(max(1, (s.scans+3)/4))
	case s.deep:
		// image/png stores 16-bit pixels one Set call at a time: 2.5 times
		// slower per pixel than 8-bit, measured.
		w *= 2
	}
	return w + int64(len(s.payload)+payloadBytesPerWork-1)/payloadBytesPerWork + int64(s.Height)*rowWork
}

// Set holds the images of one render. It is not safe for concurrent use.
type Set struct {
	b      *budget.Budget
	bySrc  map[string]*Source
	pixels int64
}

// NewSet returns an empty Set charging its Work to b.
func NewSet(b *budget.Budget) *Set {
	return &Set{b: b, bySrc: make(map[string]*Source)}
}

// Add validates src (an img src attribute). An identical src returns the same *Source
// and is never counted twice. A *RejectError means "do not draw; warn with its Reason";
// any other error is a budget error to propagate unchanged. Once MaxImages
// sources are held, any new src is rejected with ReasonCount before it is read.
// Each call with a new src charges Work for the base64 pass and the header read:
// 1 unit per 64 bytes of src plus a fixed amount. A rejected src is not
// remembered: adding it again charges and rejects it again.
func (s *Set) Add(src string) (*Source, error) {
	if old, ok := s.bySrc[src]; ok {
		return old, nil
	}
	if len(s.bySrc) >= MaxImages {
		return nil, reject(ReasonCount)
	}
	if err := s.b.Charge(budget.Work, int64(len(src)+63)/64+headerWork); err != nil {
		return nil, err
	}
	payload, r := parseDataURL(src)
	if r != 0 {
		return nil, reject(r)
	}
	pad, ok := checkBase64(payload)
	if !ok {
		return nil, reject(ReasonEncoding)
	}
	src0 := &Source{payload: payload, pad: pad}
	if r := readHeader(src0); r != 0 {
		return nil, reject(r)
	}
	px := int64(src0.Width) * int64(src0.Height)
	if s.pixels+px > MaxTotalPixels {
		return nil, reject(ReasonTotal)
	}
	s.pixels += px
	s.bySrc[src] = src0
	return src0, nil
}
