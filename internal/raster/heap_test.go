package raster

import (
	"image/color"
	"runtime"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// addFromDocument parses a document of about filler bytes holding one img and
// adds its src to a new Set. Nothing but the Set survives the call.
func addFromDocument(t *testing.T, src string, filler int) (*Set, *Source) {
	t.Helper()
	b := budget.New(t.Context(), budget.Defaults(), nil)
	input := "<p>" + strings.Repeat("x", filler) + `</p><img src="` + src + `">`
	doc, err := dom.Parse(strings.NewReader(input), b)
	if err != nil {
		t.Fatal(err)
	}
	set := NewSet(b)
	for i := range doc.Elements {
		if v, ok := doc.Elements[i].Attr("src"); ok {
			s, err := set.Add(v)
			if err != nil {
				t.Fatal(err)
			}
			return set, s
		}
	}
	t.Fatal("no img parsed")
	return nil, nil
}

// The Source of an img keeps only its src attribute alive: dom copies
// attribute values out of the input, so dropping the document and the input
// frees them.
func TestSourceDoesNotRetainInput(t *testing.T) {
	src := pngURL(encodePNG(t, gradient(4, 4, color.NRGBAModel, true)))
	const filler = 1 << 20
	before := liveHeap()
	set, s := addFromDocument(t, src, filler)
	after := liveHeap()
	retained := after - min(before, after)
	runtime.KeepAlive(set)
	t.Logf("retained %d bytes for a %d-byte src in a %d-byte input", retained, len(src), filler)
	if retained > filler/4 {
		t.Errorf("retained %d bytes: the Source keeps the input alive", retained)
	}
	if s.payload == "" || !strings.HasSuffix(src, s.payload) {
		t.Error("payload is not the src's")
	}
}
