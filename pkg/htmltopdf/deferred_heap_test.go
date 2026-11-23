package htmltopdf

import (
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"testing"
)

const deferredPageParagraphs = 26

// textPages returns a document of about n pages of text, each page holding
// several dozen lines.
func textPages(n int) string {
	var sb strings.Builder
	sb.WriteString("<html><body>")
	for i := range n * deferredPageParagraphs {
		fmt.Fprintf(&sb, "<p style='margin:0'>Paragraph %d: the analysis report lists every sample, its verdict, the engines that flagged it and the quarantine location of the file.</p>", i)
	}
	sb.WriteString("</body></html>")
	return sb.String()
}

func liveHeap() int64 {
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return int64(min(ms.HeapAlloc, math.MaxInt64))
}

// heapAt renders html and returns the live heap (after a GC) at the given
// event indices of the hook sequence, and the pages written.
func heapAt(t *testing.T, html string, at map[int]bool) (map[int]int64, int) {
	t.Helper()
	c := mustNew(t, referenceOptions(t))
	got := map[int]int64{}
	n := 0
	c.hook = func(string) {
		if at[n] {
			got[n] = liveHeap()
		}
		n++
	}
	res, err := c.Render(context.Background(), strings.NewReader(html), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return got, res.Pages
}

func stageSequence(t *testing.T, html string) []string {
	t.Helper()
	c := mustNew(t, referenceOptions(t))
	var seq []string
	c.hook = func(s string) { seq = append(seq, s) }
	if _, err := c.Render(context.Background(), strings.NewReader(html), io.Discard); err != nil {
		t.Fatal(err)
	}
	return seq
}

// Pages whose margin boxes wait for the page count must not keep their layout
// alive: the live heap during the margin pass stays close to what it was when
// the box tree was built, whatever the number of pages.
func TestDeferredPagesDoNotRetainLayout(t *testing.T) {
	if testing.Short() || raceDetectorOn {
		t.Skip("500 pages with forced collections")
	}
	one := textPages(1)
	big := textPages(500)

	// Retention of one page, measured on a single-page document: the live heap
	// when its only page is complete, above the heap when the box tree was built.
	seq1 := stageSequence(t, one)
	box1, page1 := -1, -1
	for i, s := range seq1 {
		switch s {
		case "box":
			box1 = i
		case "page":
			page1 = i
		}
	}
	h1, pages1 := heapAt(t, one, map[int]bool{box1: true, page1: true})
	if pages1 != 1 {
		t.Fatalf("the single-page document has %d pages", pages1)
	}
	t.Logf("single-page document: %d KiB above the box tree at its page", (h1[page1]-h1[box1])>>10)
	perPage := h1[page1] - h1[box1] // fixed cost plus one page
	if perPage < 1 {
		perPage = 1
	}

	seq := stageSequence(t, big)
	boxIdx, lastPage, marginIdx := -1, -1, -1
	for i, s := range seq {
		switch s {
		case "box":
			boxIdx = i
		case "page":
			lastPage = i
		case "margin":
			marginIdx = i
		}
	}
	if boxIdx < 0 || lastPage < 0 || marginIdx < 0 {
		t.Fatalf("stages: box %d, last page %d, margin %d", boxIdx, lastPage, marginIdx)
	}
	at := map[int]bool{boxIdx: true}
	for i := lastPage; i <= marginIdx; i++ {
		at[i] = true
	}
	heap, pages := heapAt(t, big, at)
	if pages < 400 {
		t.Fatalf("only %d pages", pages)
	}

	base := heap[boxIdx]
	var peak int64
	for i := lastPage; i <= marginIdx; i++ {
		peak = max(peak, heap[i]-base)
	}
	// The single-page figure is mostly fixed cost (shaper and writer buffers):
	// allow it once, plus 8 KiB per page for the deferred headers and links.
	// A page that keeps its layout alive costs about 45 KiB here.
	bound := perPage + int64(pages)*8192
	t.Logf("%d pages; single-page overhead %d KiB; heap above the box tree during the margin pass: %d KiB (bound %d KiB)",
		pages, perPage>>10, peak>>10, bound>>10)
	if peak > bound {
		t.Errorf("the margin pass retains %d KiB above the box tree, bound %d KiB: deferred pages keep their layout alive", peak>>10, bound>>10)
	}
}
