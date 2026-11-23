package text

import (
	"sync"

	"github.com/go-text/typesetting/segmenter"
)

// Break is a line-break opportunity before the byte at Offset. Mandatory is
// true for a forced break (newline).
type Break struct {
	Offset    int
	Mandatory bool
}

// maxPooledBytes bounds the text whose segmenter goes back to the pool.
const maxPooledBytes = 64 << 10

var segmenters = sync.Pool{New: func() any { return new(segmenter.Segmenter) }}

// Breaks appends to dst the UAX #14 break opportunities of text, in increasing
// offset order; the caller bounds len(text). The end of the text is not reported. Invalid UTF-8 counts as
// U+FFFD, offsets stay in the original bytes.
func Breaks(text string, dst []Break) []Break {
	seg, ok := segmenters.Get().(*segmenter.Segmenter)
	if !ok {
		seg = new(segmenter.Segmenter)
	}
	defer func() {
		// A segmenter that handled a huge text keeps its buffers: do not pool it.
		if len(text) <= maxPooledBytes {
			segmenters.Put(seg)
		}
	}()
	if err := seg.InitWithString(text); err != nil {
		return invalidBreaks(seg, text, dst)
	}
	for it := seg.LineIterator(); it.Next(); {
		l := it.Line()
		if end := l.OffsetInBytes + l.LengthInBytes; end < len(text) {
			dst = append(dst, Break{Offset: end, Mandatory: l.IsMandatoryBreak})
		}
	}
	return dst
}

// invalidBreaks is the path of texts that are not valid UTF-8, which
// InitWithString rejects: they are segmented as runes.
func invalidBreaks(seg *segmenter.Segmenter, text string, dst []Break) []Break {
	runes := []rune(text)
	seg.Init(runes)
	offsets := byteOffsets(text, runes)
	for it := seg.LineIterator(); it.Next(); {
		l := it.Line()
		if end := l.Offset + len(l.Text); end < len(runes) {
			dst = append(dst, Break{Offset: int(clusterOffset(offsets, end)), Mandatory: l.IsMandatoryBreak})
		}
	}
	return dst
}

// byteOffsets maps rune indexes to byte offsets; nil means they are equal.
func byteOffsets(text string, runes []rune) []int32 {
	if len(text) == len(runes) {
		return nil
	}
	out := make([]int32, 0, len(runes)+1)
	for i := range text {
		out = append(out, clampInt32(i))
	}
	return append(out, clampInt32(len(text)))
}

func clusterOffset(offsets []int32, runeIdx int) int32 {
	if offsets == nil {
		return clampInt32(runeIdx)
	}
	return offsets[runeIdx]
}
