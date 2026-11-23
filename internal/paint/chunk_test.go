package paint

import (
	"runtime"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/text"
)

func longRun(tb testing.TB, e *env, n int) display.List {
	tb.Helper()
	base := e.run(tb, "a", 16)
	glyphs := make([]text.Glyph, n)
	for i := range glyphs {
		glyphs[i] = text.Glyph{ID: base.Glyphs[0].ID, Advance: 8, Cluster: int32(i)}
	}
	return display.List{&display.Text{X: 10, Baseline: 100, Size: 16, Color: black, Text: strings.Repeat("a", n), Face: base.Face, Glyphs: glyphs}}
}

func TestLongRunIsWrittenInChunks(t *testing.T) {
	e := newEnv(t)
	n := 2*glyphChunk + 5
	content, _ := e.page(t, a4, longRun(t, e, n))
	if got := strings.Count(content, " Tf"); got != 3 {
		t.Fatalf("%d text objects for %d glyphs, want 3", got, n)
	}
	if cap(e.painter.glyphs) > glyphChunk {
		t.Fatalf("glyph buffer of %d entries kept", cap(e.painter.glyphs))
	}
	e.finish(t)
}

func TestLongRunPaintAllocation(t *testing.T) {
	e := newEnv(t)
	l := longRun(t, e, 1_000_000)
	c := &pdf.Content{}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := e.painter.Page(c, a4, l); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	// The content stream is the writer's: what paint itself holds is one chunk.
	if got := cap(e.painter.glyphs) * 32; got > 4<<20 {
		t.Fatalf("paint holds %d bytes of glyph buffer", got)
	}
	t.Logf("1M glyphs: %d MiB allocated in total, content %d MiB", (after.TotalAlloc-before.TotalAlloc)>>20, len(c.Bytes())>>20)
}
