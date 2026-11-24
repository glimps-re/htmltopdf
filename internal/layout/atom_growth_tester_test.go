package layout

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

type growth struct {
	atoms, primaries int
	work, glyphs     int64
	peak             uint64
	elapsed          time.Duration
	pages            int
}

func measureGrowth(t *testing.T, html string, atoms int) growth {
	t.Helper()
	var g growth
	g.atoms = atoms
	g.peak = peakHeap(func() {
		start := time.Now()
		r := newRun(t, html, opts{extra: smallPage, css: flat, limits: func(l *budget.Limits) { l[budget.Boxes] = budget.Defaults()[budget.Boxes] }})
		pages, _ := r.drain(t)
		g.pages = len(pages)
		g.elapsed = time.Since(start)
		g.work, g.glyphs = r.b.Used(budget.Work), r.b.Used(budget.Glyphs)
		g.primaries = len(r.pag.l.primaries)
	})
	return g
}

// Run with the default Boxes limit. The per-render caches must not grow with the number of atoms: layMono lays
// each atom out under a style copy, and primaryFace keys its cache by style
// pointer.
func TestM203bPerAtomCacheGrowth(t *testing.T) {
	var gs []growth
	sizes := []int{5000, 20000, 50000, 65000}
	if raceEnabled {
		sizes = sizes[:2]
	}
	for _, n := range sizes {
		html := "<p>" + strings.Repeat(`<span style="display:inline-block">k</span> `, n) + "</p>"
		g := measureGrowth(t, html, n)
		t.Logf("%6d atoms: %d primaries entries, work %d, glyphs %d, peak heap %d MiB, %v, %d pages", n, g.primaries, g.work, g.glyphs, g.peak>>20, g.elapsed, g.pages)
		gs = append(gs, g)
	}
	for _, g := range gs {
		if g.primaries > 64 {
			t.Errorf("%d atoms leave %d entries in the primary-face cache", g.atoms, g.primaries)
		}
		if g.glyphs > int64(4*g.atoms) {
			t.Errorf("%d atoms: %d glyphs charged", g.atoms, g.glyphs)
		}
	}
	if last := gs[len(gs)-1]; last.peak > 256<<20 {
		t.Errorf("peak heap %d MiB", last.peak>>20)
	}
}

// Re-layouts (break-inside: avoid backs up and lays the block out again) must
// not multiply the cache either.
func TestM203bPerAtomCacheGrowthWithRelayouts(t *testing.T) {
	var sb strings.Builder
	atoms := 0
	for range 300 {
		sb.WriteString(`<div style="break-inside:avoid;height:auto;margin-bottom:7px">`)
		for range 40 {
			sb.WriteString(`<span style="display:inline-block;width:90px">word word</span> `)
			atoms++
		}
		sb.WriteString("</div>")
	}
	g := measureGrowth(t, sb.String(), atoms)
	t.Logf("%s", fmt.Sprintf("%d atoms (300 avoid blocks): %d primaries entries, work %d, glyphs %d, peak heap %d MiB, %v, %d pages", g.atoms, g.primaries, g.work, g.glyphs, g.peak>>20, g.elapsed, g.pages))
	if g.primaries > 64 {
		t.Errorf("%d atoms leave %d entries in the primary-face cache", g.atoms, g.primaries)
	}
}
