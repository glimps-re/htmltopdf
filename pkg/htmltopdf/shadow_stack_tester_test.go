package htmltopdf

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// stackedBoxes is n empty boxes of pad px with a -pad px bottom margin: they
// all stay on one page whatever n.
func stackedBoxes(n, pad int, shadow string) string {
	p := strconv.Itoa(pad)
	return `<style>div{padding-top:` + p + `px;margin-bottom:-` + p + `px;` + shadow + `}</style>` + strings.Repeat("<div></div>", n)
}

// maxShadowItemsPerPage is the design bound (§6.1) of the shadow rectangles of
// one page.
const maxShadowItemsPerPage = 4096

type stackedRun struct {
	res   *Result
	data  []byte
	err   error
	peak  uint64
	took  time.Duration
	fills int
}

// runStackedTwice renders the page twice and returns the first run with the
// smaller and the larger peak heap of the two.
func runStackedTwice(t *testing.T, n int, shadow string) (first stackedRun, lo, hi uint64) {
	t.Helper()
	first = runStacked(t, n, 1, shadow)
	second := runStacked(t, n, 1, shadow)
	if first.err != nil || second.err != nil {
		t.Fatalf("render with %q: errors %v and %v", shadow, first.err, second.err)
	}
	return first, min(first.peak, second.peak), max(first.peak, second.peak)
}

func runStacked(t *testing.T, n, pad int, shadow string) stackedRun {
	t.Helper()
	c := mustNew(t, m2Options(t))
	var r stackedRun
	start := time.Now()
	r.peak = peakOf(func() {
		var buf bytes.Buffer
		r.res, r.err = c.Render(context.Background(), strings.NewReader(stackedBoxes(n, pad, shadow)), &buf)
		r.data = buf.Bytes()
	})
	r.took = time.Since(start)
	if r.err == nil {
		doc := pdftest.Validate(t, r.data)
		for _, p := range doc.Pages {
			r.fills += bytes.Count(p.Content, []byte("\nf\n"))
		}
	}
	return r
}

// The display list of a page is built whole before it is written: with the
// densest page the limits allow (99 000 boxes on one page, 8 blurred shadows
// each) the shadows must add a bounded amount of memory, and the render must
// succeed with one clamping warning. The item count and the warning are the
// guarantee; the heap figures are a guard. A run's peak only errs upwards (a late
// collection), so the twin counts its larger peak of two runs and the shadowed
// render its smaller one: the guard is as wide as the 16 MiB bound plus the
// spread of the twin, and the fill count is the real guarantee.
func TestShadowStackedBoxesOnOnePage(t *testing.T) {
	const n = 99000
	shadows := "box-shadow:" + strings.TrimSuffix(strings.Repeat("1px 2px 8px 1px red,", 8), ",")
	plain, _, plainHi := runStackedTwice(t, n, "")
	with, withLo, withHi := runStackedTwice(t, n, shadows)
	t.Logf("without shadows: %d pages, %d fills, peak heap %d MiB, %v", plain.res.Pages, plain.fills, plainHi>>20, plain.took)
	t.Logf("with shadows: peak heap %d..%d MiB, %d fills, %v", withLo>>20, withHi>>20, with.fills, with.took)
	if withHi > 256<<20 {
		t.Errorf("peak heap %d MiB above the 256 MiB ceiling", withHi>>20)
	}
	if withLo > plainHi+16<<20 {
		t.Errorf("shadows add %d MiB of heap (%d with, %d without)", (withLo-plainHi)>>20, withLo>>20, plainHi>>20)
	}
	if with.fills > maxShadowItemsPerPage {
		t.Errorf("%d fills on the page, bound %d", with.fills, maxShadowItemsPerPage)
	}
	if !hasClampWarning(with.res) {
		t.Errorf("no clamping warning for the shadow items: %+v", with.res.Warnings)
	}
}

// Under the bound the shadows are all drawn and nothing is warned.
func TestShadowItemsUnderTheBound(t *testing.T) {
	const n = 100 // 100 boxes x 8 shadows x 4 rectangles = 3200 < 4096
	shadows := "box-shadow:" + strings.TrimSuffix(strings.Repeat("1px 2px 8px 1px red,", 8), ",")
	r := runStacked(t, n, 20, shadows)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.fills != n*8*4 {
		t.Errorf("%d fills, want %d", r.fills, n*8*4)
	}
	for _, w := range r.res.Warnings {
		if strings.Contains(w.Detail, "box-shadow items per page") {
			t.Errorf("unexpected warning %+v", w)
		}
	}
}

const shadowClampDetail = "box-shadow items per page"

func hasClampWarning(res *Result) bool {
	for _, w := range res.Warnings {
		if w.Code == WarnClamped && strings.Contains(w.Detail, shadowClampDetail) {
			return true
		}
	}
	return false
}

// The bound is exact: 4096 rectangles per page are all drawn without a
// warning, the 4097th is dropped with one, and the counter restarts on the
// next page.
func TestShadowItemBoundary(t *testing.T) {
	blurred := "box-shadow:1px 2px 8px 1px red" // 4 rectangles on a 20px box
	flat := "box-shadow:1px 2px 0 1px red"      // 1 rectangle
	box := func(style string) string {
		return `<div style="padding-top:20px;margin-bottom:-20px;` + style + `">` + `</div>`
	}
	page := func(blurredBoxes, flatBoxes int) string {
		return strings.Repeat(box(blurred), blurredBoxes) + strings.Repeat(box(flat), flatBoxes)
	}
	next := `<div style="break-before:page"></div>`
	for _, tc := range []struct {
		name  string
		html  string
		pages int
		fills int
		warn  bool
	}{
		{"4095", page(1023, 0) + page(0, 3), 1, 4095, false},
		{"4096", page(1024, 0), 1, 4096, false},
		{"4097", page(1024, 1), 1, 4096, true},
		{"cut inside the shadows of one box", page(1023, 0) + box("box-shadow:"+strings.TrimSuffix(strings.Repeat("1px 2px 0 1px red,", 8), ",")), 1, 4096, true},
		{"two full pages", page(1024, 0) + next + page(1024, 0), 2, 8192, false},
		{"second page clamped", page(1024, 0) + next + page(1024, 1), 2, 8192, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, data := renderBytes(t, mustNew(t, m2Options(t)), tc.html)
			doc := pdftest.Validate(t, data)
			fills := 0
			for _, p := range doc.Pages {
				fills += bytes.Count(p.Content, []byte("\nf\n"))
			}
			if res.Pages != tc.pages || fills != tc.fills || hasClampWarning(res) != tc.warn {
				t.Errorf("%d pages, %d fills, warned %v; want %d, %d, %v (%+v)", res.Pages, fills, hasClampWarning(res), tc.pages, tc.fills, tc.warn, res.Warnings)
			}
			if tc.warn {
				n := 0
				for _, w := range res.Warnings {
					if strings.Contains(w.Detail, shadowClampDetail) {
						n++
					}
				}
				if n != 1 {
					t.Errorf("%d clamping warnings, want 1", n)
				}
			}
		})
	}
}
