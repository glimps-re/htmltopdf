package raster

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// configDecoders is indexed by Format. It is a variable so that the tests can
// inject a panicking header decoder.
var configDecoders = [...]func(io.Reader) (image.Config, error){
	PNG:  png.DecodeConfig,
	JPEG: jpeg.DecodeConfig,
}

const (
	pngMagic  = "\x89PNG\r\n\x1a\n"
	jpegMagic = "\xff\xd8\xff"
	// pngHeadLen covers the signature and the IHDR chunk with its CRC.
	pngHeadLen = 8 + 8 + 13 + 4
)

// readHeader fills every field of s but the payload, or says why the image is
// rejected. The format comes from the magic bytes, not the declared type. Only
// the header is read, except for a JPEG, whose markers are walked to the end
// to count its scans. A panic of the stdlib header code rejects the image.
func readHeader(s *Source) (reason Reason) {
	defer func() {
		if recover() != nil {
			reason = ReasonFormat
		}
	}()
	r := s.open()
	var head [pngHeadLen]byte
	n, err := io.ReadFull(r, head[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ReasonFormat
	}
	switch {
	case bytes.HasPrefix(head[:n], []byte(pngMagic)):
		s.Format = PNG
		return readPNGHeader(s, head[:n], io.MultiReader(bytes.NewReader(head[:n]), r))
	case bytes.HasPrefix(head[:n], []byte(jpegMagic)):
		s.Format = JPEG
		return readJPEGHeader(s, io.MultiReader(bytes.NewReader(head[:n]), r))
	}
	return ReasonFormat
}

// PNG colour types and the interlace method of the IHDR chunk.
const (
	pngGray      = 0
	pngPaletted  = 3
	pngGrayAlpha = 4
	pngAdam7     = 1
)

func readPNGHeader(s *Source, head []byte, r io.Reader) Reason {
	if len(head) < pngHeadLen || string(head[12:16]) != "IHDR" {
		return ReasonFormat
	}
	ihdr := head[16:29]
	w, h := binary.BigEndian.Uint32(ihdr[0:4]), binary.BigEndian.Uint32(ihdr[4:8])
	cfg, err := configDecoders[PNG](r)
	if err != nil || cfg.Width != int(w) || cfg.Height != int(h) {
		return ReasonFormat
	}
	if !sizeOK(int64(w), int64(h)) {
		return ReasonSize
	}
	depth, colorType, interlace := ihdr[8], ihdr[9], ihdr[12]
	s.Width, s.Height = cfg.Width, cfg.Height
	s.gray = colorType == pngGray || colorType == pngGrayAlpha
	s.deep = depth == 16
	s.cost = pngCost(int64(w)*int64(h), depth, colorType, interlace)
	if s.cost > MaxDecodeBytes {
		return ReasonSize
	}
	return 0
}

// sizeOK checks the dimensions of a header against MaxSide and MaxPixels.
// A zero side is left to the decoder's header check (format); so are PNG
// sides above 2^31-1, which png.DecodeConfig rejects first.
func sizeOK(w, h int64) bool {
	return w <= MaxSide && h <= MaxSide && w*h <= MaxPixels
}

// pngCost is the memory image/png allocates for an image of px pixels: the
// decoded image, whose type is that of the worst case because a tRNS chunk,
// which DecodeConfig does not read, turns a gray or RGB image into NRGBA. An
// Adam7 image is decoded pass by pass into images merged into the result,
// which doubles the peak. The two row buffers, the zlib state and the output
// row of the writers (< 300 KiB at MaxSide) are left to the margin.
func pngCost(px int64, depth, colorType, interlace uint8) int64 {
	bpp := int64(4)
	switch {
	case colorType == pngPaletted:
		bpp = 1
	case depth == 16:
		bpp = 8
	}
	c := px * bpp
	if interlace == pngAdam7 {
		c *= 2
	}
	return c
}

func readJPEGHeader(s *Source, r io.Reader) Reason {
	info, reason := scanJPEG(r)
	if reason != 0 {
		return reason
	}
	if !sizeOK(int64(info.width), int64(info.height)) {
		return ReasonSize
	}
	s.cost = info.cost()
	_, _, s.area = info.mcus()
	if s.cost > MaxDecodeBytes {
		return ReasonSize
	}
	// The stdlib cross-check rejects the sampling ratios image/jpeg refuses;
	// cost assumes the largest factors are those of component 0.
	cfg, err := configDecoders[JPEG](s.open())
	if err != nil || cfg.Width != info.width || cfg.Height != info.height {
		return ReasonFormat
	}
	s.Width, s.Height = cfg.Width, cfg.Height
	s.scans = info.scans
	s.gray = info.nComp == 1
	return 0
}
