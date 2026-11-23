package dom

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

const inputMax = 2 << 20

// hostileUnits are the inputs that made an unguarded x/net/html tree 120-440
// MiB large at 8 MiB of input, and the formatting-element reconstruction bomb.
var hostileUnits = map[string]string{
	"x<p>":           "x<p>",
	"<p>":            "<p>",
	"<br>":           "<br>",
	"p8attrs":        "<p a b c d e f g h>",
	"emptyComment":   "<!---->",
	"reconstruct":    "<b><i><u><p>x",
	"adoption":       "<b><i><u><div>x</b>y</i>z</u></div>",
	"tableImplied":   "<td>x",
	"svgRawBreakout": "<svg><p><script><!-- </script><br>",
}

func repeatTo(unit string, size int) string {
	return strings.Repeat(unit, size/len(unit))
}

// maxRepeat returns the largest n such that unit repeated n times has an
// estimate within limit.
func maxRepeat(t testing.TB, unit string, limit int64) int {
	t.Helper()
	hi := min(64<<20/len(unit), int(limit)) + 1
	return sort.Search(hi, func(n int) bool {
		return estimate(t, strings.Repeat(unit, n+1)) > limit
	})
}

func isNodesLimit(err error) bool {
	var le *budget.LimitError
	return errors.As(err, &le) && le.Kind == budget.Nodes && le.Max == budget.Defaults()[budget.Nodes]
}

// Every hostile input at 2 MiB fails with the Nodes limit, in the pre-pass or in
// Convert.
func TestHostileFailsWithNodesLimit(t *testing.T) {
	for name, unit := range hostileUnits {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(repeatTo(unit, inputMax)), newBudget(budget.Defaults()))
			if !isNodesLimit(err) {
				t.Fatalf("got %v, want a MaxNodes LimitError", err)
			}
		})
	}
}

// The pre-pass boundary: an estimate of exactly 4 x MaxNodes passes, one more
// fails before html.Parse runs.
func TestPrepassBoundary(t *testing.T) {
	const comments = 4*100_000 - 3 // estimate is 3 implied elements + one per comment
	l := budget.Defaults()
	l[budget.InputBytes] = 8 << 20
	src := strings.Repeat("<!---->", comments)
	if est := estimate(t, src); est != 4*100_000 {
		t.Fatalf("estimate = %d, want %d", est, 4*100_000)
	}
	if err := prepass([]byte(src), newBudget(l)); err != nil {
		t.Fatalf("estimate at 4 x MaxNodes: %v", err)
	}
	if err := prepass([]byte(src+"<!---->"), newBudget(l)); !isNodesLimit(err) {
		t.Fatalf("estimate at 4 x MaxNodes + 1: got %v", err)
	}
}

// The exact boundary: 100 000 real nodes pass, 100 001 fail in Convert.
func TestConvertBoundary(t *testing.T) {
	const comments = 100_000 - 3
	l := budget.Defaults()
	var stages []string
	b := budget.New(t.Context(), l, func(s string) { stages = append(stages, s) })
	if _, err := Parse(strings.NewReader(strings.Repeat("<!---->", comments)), b); err != nil {
		t.Fatalf("100000 nodes: %v", err)
	}
	stages = nil
	b = budget.New(t.Context(), l, func(s string) { stages = append(stages, s) })
	_, err := Parse(strings.NewReader(strings.Repeat("<!---->", comments+1)), b)
	if !isNodesLimit(err) {
		t.Fatalf("100001 nodes: got %v", err)
	}
	if got := strings.Join(stages, ","); got != "dom.read,dom.prepass,dom.parse,dom.convert" {
		t.Fatalf("stages = %s: the exact limit must be enforced by Convert", got)
	}
}

// For every hostile unit, the largest repetition within the estimate limit is
// not rejected by the pre-pass, one more repetition is.
func TestHostilePrepassTwins(t *testing.T) {
	if raceEnabled {
		t.Skip("binary searches over estimates of up to 64 MiB: about 100 s under -race")
	}
	limit := EstimateFactor * budget.Defaults()[budget.Nodes]
	for name, unit := range hostileUnits {
		t.Run(name, func(t *testing.T) {
			n := maxRepeat(t, unit, limit)
			if n == 0 {
				t.Fatal("no repetition fits")
			}
			l := budget.Defaults()
			l[budget.InputBytes] = 64 << 20
			if err := prepass([]byte(strings.Repeat(unit, n)), newBudget(l)); err != nil {
				t.Fatalf("%d repetitions: %v", n, err)
			}
			if err := prepass([]byte(strings.Repeat(unit, n+1)), newBudget(l)); !isNodesLimit(err) {
				t.Fatalf("%d repetitions: got %v, want a MaxNodes LimitError", n+1, err)
			}
		})
	}
}

func TestEstimateTightness(t *testing.T) {
	// The estimate stays within a small factor of the real cost on inputs
	// without misnesting.
	const doc = `<html><head><title>t</title><style>p{}</style></head><body>
<div class="a"><p>Hello <b>bold</b> and <a href="x">link</a> text</p>
<table><tr><td>1</td><td><i>2</i></td></tr><tr><td>3</td><td>4</td></tr></table></div></body></html>`
	real, _ := actual(t, doc)
	est := estimate(t, doc)
	if est < real || est > 3*real {
		t.Fatalf("estimate %d, actual %d", est, real)
	}
}
