package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/layout"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// Targets: one 2048² PNG with alpha in 400 ms and 64 MiB; every hostile document in 2 s and 96 MiB.
const (
	m6HostileHeap = 96 << 20
	m6HostileTime = 2 * time.Second
	m6PNGHeap     = 64 << 20
	m6PNGTime     = 400 * time.Millisecond
	// m6TargetsEnv enables the strict targets. A parallel go test ./... shares
	// the CPUs and the heap sampler with other packages, which makes both
	// measures noisy; by default the suite only holds a guard ten times looser.
	m6TargetsEnv = "HTMLTOPDF_TARGETS"
)

// m6Limits returns the heap and time a hostile document may take: the 2 s / 96 MiB and
// 400 ms / 64 MiB targets with HTMLTOPDF_TARGETS=1, a loose guard otherwise.
func m6Limits(hostile bool) (heap uint64, took time.Duration) {
	switch {
	case os.Getenv(m6TargetsEnv) == "1" && hostile:
		return m6HostileHeap, m6HostileTime
	case os.Getenv(m6TargetsEnv) == "1":
		return m6PNGHeap, m6PNGTime
	case hostile:
		return 256 << 20, 20 * time.Second
	}
	return 256 << 20, 10 * time.Second
}

// m6Doc is a document of the hostile suite.
type m6Doc struct {
	name, html string
	// maxPages bounds the pages when positive.
	maxPages int
	// wantLimit, when set, is the only outcome allowed besides success.
	wantLimit Limit
	// knownOutside names the documented limit that puts glyphs outside the page
	// area (an unbreakable word runs past the right edge, as in any browser; a
	// huge author length); the glyph check is then logged, not asserted.
	knownOutside string
}

// m6HostileDocs is every hostile document of M6: the crafted images between two
// words, the hostile image documents, the one-tag documents with a tall atom and
// the alt text spoofing.
func m6HostileDocs(tb testing.TB) []m6Doc {
	var docs []m6Doc
	for _, c := range testcorpus.ImageCases() {
		docs = append(docs, m6Doc{name: "case/" + c.Name, html: m6CaseDoc(c.Src)})
	}
	for _, d := range testcorpus.ImageDocs() {
		md := m6Doc{name: "doc/" + d.Name, html: string(d.HTML())}
		if d.Want == testcorpus.Limit {
			md.wantLimit = LimitWork
		}
		docs = append(docs, md)
	}
	for _, d := range testcorpus.TallAtomDocs() {
		docs = append(docs, m6Doc{name: "tall/" + d.Name, html: `<html><head><style>` + testcorpus.TallAtomPageCSS + `</style></head><body>` + d.HTML + `</body></html>`, maxPages: d.MaxPages})
	}
	for _, tc := range testcorpus.TextCases() {
		docs = append(docs, m6Doc{name: "alt/" + tc.Name, html: `<p>x<img src="http://attacker.example/a.png" alt="` + strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(tc.Raw) + `">y</p>`})
	}
	px := grayPNGURL(tb, 2, 2)
	docs = append(docs,
		m6Doc{name: "other/data_urls_not_drawn", html: `<svg width="20" height="20"><image href="` + px + `" width="20" height="20"/></svg><picture><source srcset="` + px + ` 1x"><img srcset="` + px + ` 2x" alt="ALT"></picture><input type="image" src="` + px + `" alt="in">`},
		m6Doc{name: "alt/long_200k", html: `<p>x<img src="x" alt="` + strings.Repeat("alt ", 50_000) + `">y</p>`},
		m6Doc{name: "alt/long_unbroken_200k", html: `<p>x<img src="x" alt="` + strings.Repeat("W", 200_000) + `">y</p>`, knownOutside: "unbreakable word overflows right"},
		m6Doc{name: "alt/bidi_flood", html: `<p>x<img src="x" alt="` + strings.Repeat("\u202e\u2066", 20_000) + `">y</p>`},
		m6Doc{name: "alt/in_tiny_box", html: `<p>x<img src="x" alt="` + strings.Repeat("alt ", 5000) + `" width="1" height="1" style="display:block">y</p>`},
	)
	return docs
}

// Every hostile document of M6 through Render with the default options: no
// panic, success or a LimitError and nothing else, no text lost, every glyph
// reaching a page area, a valid PDF; within the heap and time targets
// (2 s, 96 MiB) with HTMLTOPDF_TARGETS=1 (a loose guard otherwise, and none under the race
// detector, which multiplies both).
func TestM6HostileSuite(t *testing.T) {
	var slow []string
	for _, d := range m6HostileDocs(t) {
		t.Run(d.name, func(t *testing.T) {
			r := m6Render(t, Options{}, d.html)
			if le := m6HostileGlyphs(t, r, d.knownOutside); le != nil && le.Limit != d.wantLimit {
				t.Fatalf("LimitError %v, want %q", le, d.wantLimit)
			} else if le == nil && d.wantLimit != "" && r.err == nil {
				t.Fatalf("succeeded, want LimitError %q", d.wantLimit)
			}
			if r.err == nil && d.maxPages > 0 && r.res.Pages > d.maxPages {
				t.Errorf("%d pages, want at most %d", r.res.Pages, d.maxPages)
			}
			took, heap := r.took, r.peak+uint64(len(d.html))
			t.Logf("%v, heap-max %.1f MiB, %d bytes out", took.Round(time.Millisecond), float64(heap)/(1<<20), len(r.data))
			if raceDetectorOn {
				return
			}
			maxHeap, maxTook := m6Limits(true)
			if heap > maxHeap {
				t.Errorf("heap-max %.1f MiB, want at most %d", float64(heap)/(1<<20), maxHeap>>20)
			}
			if took > maxTook {
				t.Errorf("%v, want at most %v", took, maxTook)
			}
			if took > m6HostileTime/4 || heap > m6HostileHeap/2 {
				slow = append(slow, d.name)
			}
		})
	}
	slices.Sort(slow)
	t.Logf("above a quarter of the time or half of the heap target: %v", slow)
}

// m6PNG2048Alpha is a 2048×2048 8-bit PNG with a varying alpha channel that fits
// MaxInputBytes.
func m6PNG2048Alpha(tb testing.TB) string {
	tb.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	for y := range 2048 {
		for x := range 2048 {
			img.SetNRGBA(x, y, color.NRGBA{byte(x >> 3 & 0xff), byte(y >> 3 & 0xff), byte((x + y) >> 4 & 0xff), byte((255 - (x*y)>>14) & 0xff)})
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		tb.Fatal(err)
	}
	if buf.Len() > 1<<20 {
		tb.Fatalf("test image of %d bytes", buf.Len())
	}
	return "data:image/png;base64," + base64Of(buf.Bytes())
}

// Target: one 2048×2048 PNG with alpha renders in at most 400 ms
// and 64 MiB of heap (the best of three, as the machine of the target is quiet).
func TestM6Target2048PNGWithAlpha(t *testing.T) {
	if testing.Short() || raceDetectorOn {
		t.Skip("timing target")
	}
	maxHeap, maxTook := m6Limits(false)
	html := `<p>a</p><img src="` + m6PNG2048Alpha(t) + `" width="600">`
	c := mustNew(t, Options{})
	best := time.Hour
	var heap uint64
	for range 3 {
		var buf bytes.Buffer
		var took time.Duration
		peak := peakOf(func() {
			start := time.Now()
			if _, err := c.Render(context.Background(), strings.NewReader(html), &buf); err != nil {
				t.Fatal(err)
			}
			took = time.Since(start)
		})
		best = min(best, took)
		heap = max(heap, peak+uint64(len(html)))
	}
	t.Logf("2048×2048 RGBA PNG: %v, heap-max %.1f MiB", best.Round(time.Millisecond), float64(heap)/(1<<20))
	if best > maxTook {
		t.Errorf("%v, target %v (lower the zlib level of the image streams)", best, maxTook)
	}
	if heap > maxHeap {
		t.Errorf("heap-max %.1f MiB, target %d", float64(heap)/(1<<20), maxHeap>>20)
	}
}

// BenchmarkM6RenderPNG2048Alpha reports the time and heap-max of the 2048×2048
// RGBA PNG target.
func BenchmarkM6RenderPNG2048Alpha(b *testing.B) {
	html := `<p>a</p><img src="` + m6PNG2048Alpha(b) + `" width="600">`
	c := mustNew(b, Options{})
	b.ReportAllocs()
	var maxPeak uint64
	for b.Loop() {
		var buf bytes.Buffer
		peak := peakOf(func() {
			if _, err := c.Render(context.Background(), strings.NewReader(html), &buf); err != nil {
				b.Fatal(err)
			}
		})
		maxPeak = max(maxPeak, peak+uint64(len(html)))
	}
	b.ReportMetric(float64(maxPeak)/(1<<20), "heap-max-MiB")
}

// BenchmarkM6Hostile reports the time and heap-max of the heaviest hostile
// documents.
func BenchmarkM6Hostile(b *testing.B) {
	heavy := map[string]bool{
		"case/png_rgba16_2048x2048_limit": true, "case/jpeg_ycc_2048x2048_baseline": true, "case/jpeg_gray_2048x2048_limit": true,
		"case/png_deflate_bomb_1x1": true, "case/png_text_chunks_1400k": true, "case/jpeg_progressive_ycc_1024x1024": true,
		"doc/img_total_4x2048sq": true, "doc/img_total_5x2048sq": true, "doc/img_dup_10000": true, "doc/img_distinct_65": true,
		"doc/img_progressive_32_scans_work": true, "doc/img_layout_contexts": true, "alt/long_unbroken_200k": true,
	}
	c := mustNew(b, Options{})
	for _, d := range m6HostileDocs(b) {
		if !heavy[d.name] {
			continue
		}
		b.Run(d.name, func(b *testing.B) {
			b.ReportAllocs()
			var maxPeak uint64
			for b.Loop() {
				var buf bytes.Buffer
				peak := peakOf(func() {
					if _, err := c.Render(context.Background(), strings.NewReader(d.html), &buf); err != nil {
						var le *LimitError
						if !errors.As(err, &le) {
							b.Fatal(err)
						}
					}
				})
				maxPeak = max(maxPeak, peak+uint64(len(d.html)))
			}
			b.ReportMetric(float64(maxPeak)/(1<<20), "heap-max-MiB")
		})
	}
}

// FuzzRenderImages: whatever the document, Render of one with images does not
// panic, fails with a LimitError or not at all, and loses no text. Limits are
// small so that each execution stays cheap.
func FuzzRenderImages(f *testing.F) {
	const fuzzTimeout = 5 * time.Second
	for _, c := range testcorpus.ImageCases() {
		if len(c.Src) < 64<<10 {
			f.Add([]byte(m6CaseDoc(c.Src)))
		}
	}
	for _, d := range m6HostileDocs(f) {
		if len(d.html) < 64<<10 && !strings.HasPrefix(d.name, "case/") {
			f.Add([]byte(d.html))
		}
	}
	for _, c := range testcorpus.ImageCases() {
		if strings.HasPrefix(c.Name, "png_header_") || strings.HasSuffix(c.Name, "_limit") || strings.HasSuffix(c.Name, "_over") {
			f.Add([]byte(m6CaseDoc(c.Src)))
		}
	}
	f.Add([]byte(`<p>a <img src="` + grayPNGURL(f, 3, 3) + `" width="50%" height="auto" style="max-width:10px;object-fit:cover" alt="x"><a href="#a"><img src="` + grayPNGURL(f, 1, 9) + `" align="right" hspace="4"></a></p>`))
	f.Add([]byte(`<table><tr><td><img src="` + grayPNGURL(f, 2, 2) + `" width="1e9"></td></tr></table><div style="columns:2;opacity:.5;transform:rotate(9deg)"><img src="` + grayPNGURL(f, 4, 4) + `" style="display:block;float:left;margin:-99px"></div>`))
	c := mustNew(f, Options{Limits: Limits{
		MaxInputBytes: 256 << 10, MaxNodes: 5000, MaxCSSBytes: 64 << 10, MaxSelectors: 500, MaxStyles: 2000,
		MaxBoxes: 10_000, MaxGlyphs: 100_000, MaxPages: 20, MaxOutputBytes: 8 << 20, MaxWork: 5_000_000,
		Timeout: fuzzTimeout,
	}})
	f.Fuzz(func(t *testing.T, data []byte) {
		var buf bytes.Buffer
		var tree *box.Box
		var pages []*layout.Page
		hooked := *c
		hooked.pageHook = func(root *box.Box, pg *layout.Page) { tree = root; pages = append(pages, pg) }
		start := time.Now()
		res, err := hooked.Render(context.Background(), bytes.NewReader(data), &buf)
		if res == nil {
			t.Fatal("nil Result")
		}
		if d := time.Since(start); d > fuzzTimeout+2*time.Second {
			t.Fatalf("render took %v with a %v timeout", d, fuzzTimeout)
		}
		var le *LimitError
		switch {
		case err == nil:
			if res.Pages < 1 || res.Pages != len(pages) || res.Bytes != int64(buf.Len()) || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
				t.Fatalf("bad success: %+v, %d pages laid out, %d bytes", res, len(pages), buf.Len())
			}
			if lost := layout.TextLost(tree, pages); lost != "" {
				t.Fatalf("text lost: %s", lost)
			}
		case errors.As(err, &le), errors.Is(err, context.DeadlineExceeded):
		default:
			t.Fatalf("unexpected error %T: %v", err, err)
		}
	})
}
