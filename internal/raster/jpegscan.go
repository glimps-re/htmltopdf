package raster

import (
	"bufio"
	"errors"
	"io"
)

// JPEG markers (ITU T.81 Table B.1).
const (
	mSOF0  = 0xc0 // baseline
	mSOF1  = 0xc1 // extended sequential, Huffman
	mSOF2  = 0xc2 // progressive, Huffman
	mDHT   = 0xc4
	mJPG   = 0xc8
	mDAC   = 0xcc
	mRST0  = 0xd0
	mRST7  = 0xd7
	mEOI   = 0xd9
	mSOS   = 0xda
	mAPP0  = 0xe0
	mAPP14 = 0xee
)

// jpegInfo is what the marker walk learns of a JPEG.
type jpegInfo struct {
	width, height int
	nComp         int
	id, h, v      [4]int // component identifiers and sampling factors
	progressive   bool
	scans         int
	jfif          bool
	adobe         bool
	transform     byte
}

// scanJPEG walks the markers of a JPEG to its EOI, or to the end of the data
// for a truncated one, skipping the entropy-coded segments as image/jpeg
// does. It rejects with ReasonFormat a SOF other than 8-bit Huffman
// baseline, extended or progressive (image/jpeg rejects arithmetic coding,
// lossless and 12-bit), a malformed SOF, more than MaxJPEGMarkers markers
// (marker floods) and more than MaxJPEGScans scans (progressive scan bombs).
func scanJPEG(r io.Reader) (jpegInfo, Reason) {
	br := bufio.NewReaderSize(r, readBufSize)
	var info jpegInfo
	if _, err := br.Discard(2); err != nil {
		return info, ReasonFormat
	}
	markers := 0
	for {
		marker, err := nextMarker(br)
		if err != nil {
			if errors.Is(err, io.EOF) && info.nComp > 0 {
				return info, 0
			}
			return info, ReasonFormat
		}
		if marker == mEOI {
			break
		}
		if markers++; markers > MaxJPEGMarkers {
			return info, ReasonFormat
		}
		var lb [2]byte
		if _, err := io.ReadFull(br, lb[:]); err != nil {
			return info, ReasonFormat
		}
		n := (int(lb[0])<<8 | int(lb[1])) - 2
		if n < 0 {
			return info, ReasonFormat
		}
		if r := segment(br, marker, n, &info); r != 0 {
			return info, r
		}
	}
	if info.nComp == 0 {
		return info, ReasonFormat
	}
	return info, 0
}

// nextMarker returns the next marker code, skipping what image/jpeg skips
// between segments: entropy-coded data, stuffed "\xff\x00", fill bytes and
// restart markers.
func nextMarker(br *bufio.Reader) (byte, error) {
	for {
		if _, err := br.ReadSlice(0xff); err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			return 0, err
		}
		c, err := br.ReadByte()
		for err == nil && c == 0xff {
			c, err = br.ReadByte()
		}
		if err != nil {
			return 0, err
		}
		if c != 0 && (c < mRST0 || c > mRST7) {
			return c, nil
		}
	}
}

// segment consumes a segment of n bytes, recording in info what the cost and
// the caps need.
func segment(br *bufio.Reader, marker byte, n int, info *jpegInfo) Reason {
	switch {
	case marker == mSOF0 || marker == mSOF1 || marker == mSOF2:
		return readSOF(br, marker, n, info)
	case marker >= mSOF0 && marker <= 0xcf && marker != mDHT && marker != mJPG && marker != mDAC:
		return ReasonFormat
	case marker == mSOS:
		if info.nComp == 0 {
			return ReasonFormat
		}
		if info.scans++; info.scans > MaxJPEGScans {
			return ReasonFormat
		}
	case marker == mAPP0 && n >= 5:
		var b [5]byte
		if _, err := io.ReadFull(br, b[:]); err != nil {
			return ReasonFormat
		}
		info.jfif = string(b[:]) == "JFIF\x00"
		n -= len(b)
	case marker == mAPP14 && n >= 12:
		var b [12]byte
		if _, err := io.ReadFull(br, b[:]); err != nil {
			return ReasonFormat
		}
		if string(b[:5]) == "Adobe" {
			info.adobe, info.transform = true, b[11]
		}
		n -= len(b)
	}
	if _, err := br.Discard(n); err != nil {
		return ReasonFormat
	}
	return 0
}

func readSOF(br *bufio.Reader, marker byte, n int, info *jpegInfo) Reason {
	var b [6 + 3*4]byte
	if info.nComp != 0 || n < 6 || n > len(b) {
		return ReasonFormat
	}
	if _, err := io.ReadFull(br, b[:n]); err != nil {
		return ReasonFormat
	}
	nComp := int(b[5])
	if b[0] != 8 || n != 6+3*nComp || (nComp != 1 && nComp != 3 && nComp != 4) {
		return ReasonFormat
	}
	info.height = int(b[1])<<8 | int(b[2])
	info.width = int(b[3])<<8 | int(b[4])
	if info.width == 0 || info.height == 0 {
		return ReasonFormat
	}
	for i := range nComp {
		c := b[6+3*i:]
		info.id[i], info.h[i], info.v[i] = int(c[0]), int(c[1]>>4), int(c[1]&0x0f)
		if info.h[i] < 1 || info.h[i] > 4 || info.v[i] < 1 || info.v[i] > 4 {
			return ReasonFormat
		}
	}
	info.nComp = nComp
	info.progressive = marker == mSOF2
	return 0
}

// isRGB mirrors image/jpeg: a 3-component image is RGB, not YCbCr, without
// JFIF marker when the Adobe transform is 0 or the components are named R, G, B.
func (j *jpegInfo) isRGB() bool {
	if j.jfif {
		return false
	}
	if j.adobe && j.transform == 0 {
		return true
	}
	return j.id[0] == 'R' && j.id[1] == 'G' && j.id[2] == 'B'
}

// mcus returns the MCU grid of the image and its area in pixels: image/jpeg
// allocates and reconstructs whole MCUs.
func (j *jpegInfo) mcus() (mxx, myy, area int64) {
	hMax, vMax := 1, 1
	if j.nComp > 1 {
		for i := range j.nComp {
			hMax, vMax = max(hMax, j.h[i]), max(vMax, j.v[i])
		}
	}
	mxx = int64((j.width + 8*hMax - 1) / (8 * hMax))
	myy = int64((j.height + 8*vMax - 1) / (8 * vMax))
	return mxx, myy, 8 * int64(hMax) * mxx * 8 * int64(vMax) * myy
}

// blockBytes is the size of the int32 coefficients image/jpeg keeps per 8×8
// block of a progressive image.
const blockBytes = 64 * 4

// cost is the memory image/jpeg allocates for the image: planes allocated on
// whole MCUs (a 3-component image counted as 4:4:4, an upper bound), the
// black plane and the CMYK image of a 4-component image, the RGBA image of an
// RGB one, and the coefficients of every block of a progressive one.
func (j *jpegInfo) cost() int64 {
	mxx, myy, area := j.mcus()
	px := int64(j.width) * int64(j.height)
	var c int64
	switch j.nComp {
	case 1:
		c = area
	case 3:
		c = 3 * area
		if j.isRGB() {
			c += 4 * px
		}
	case 4:
		c = 4*area + 4*px
	}
	if j.progressive {
		if j.nComp == 1 {
			c += mxx * myy * blockBytes
		} else {
			for i := range j.nComp {
				c += mxx * myy * int64(j.h[i]*j.v[i]) * blockBytes
			}
		}
	}
	return c
}
