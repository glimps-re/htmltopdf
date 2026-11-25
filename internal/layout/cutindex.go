package layout

import (
	"math"
	"sort"
)

// minIndexed is the number of children or lines from which a fragment cut on
// several pages keeps a cutIndex: fewer are scanned.
const minIndexed = 16

// span is an entry of a cutIndex, relative to the top of the indexed fragment:
// reach is the lowest bottom of the items up to it (overflow included), top
// the highest top of the items from it on.
type span struct{ reach, top float64 }

// cutIndex lets a cut skip, by binary search, the children and the lines of a
// fragment that end above the window or start below it. A band cut on every
// page it spans would otherwise scan all its items on each: size times pages.
// Tops are not sorted (a negative margin moves a box up), hence the running
// bounds.
type cutIndex struct {
	children, lines []span
}

func buildSpans(n int, base float64, top, bottom func(i int) float64) []span {
	sp := make([]span, n)
	reach := math.Inf(-1)
	for i := range n {
		reach = math.Max(reach, bottom(i)-base)
		sp[i].reach = reach
	}
	high := math.Inf(1)
	for i := n - 1; i >= 0; i-- {
		high = math.Min(high, top(i)-base)
		sp[i].top = high
	}
	return sp
}

// window returns the range [i, j) of the n items that can meet the window
// [from, to): before i, all end above from; from j on, all start at or below
// to. sp is nil for a fragment with few items. The bounds come back from
// relative values with rounding: the skips keep cutEps of margin, and the
// items kept decide exactly (a baseline on from belongs to the window).
func window(sp []span, n int, base, from, to float64) (i, j int) {
	if sp == nil {
		return 0, n
	}
	i = sort.Search(n, func(k int) bool { return sp[k].reach+base >= from-cutEps })
	j = sort.Search(n, func(k int) bool { return sp[k].top+base >= to+cutEps })
	return i, max(i, j)
}

// index returns the cut index of the complete fragment f, built on its first
// cut; nil when f has few items.
func (f *frag) index() *cutIndex {
	if f.cut == nil && (len(f.children) >= minIndexed || len(f.lines) >= minIndexed) {
		ix := &cutIndex{}
		if len(f.children) >= minIndexed {
			ix.children = buildSpans(len(f.children), f.rect.Y,
				func(i int) float64 { return f.children[i].rect.Y },
				func(i int) float64 { return reachBottom(f.children[i]) })
		}
		if len(f.lines) >= minIndexed {
			ix.lines = buildSpans(len(f.lines), f.rect.Y,
				func(i int) float64 { return math.Min(f.lines[i].rect.Y, f.lines[i].baseline) },
				func(i int) float64 { return lineBottom(f.lines[i]) })
		}
		f.cut = ix
	}
	return f.cut
}
