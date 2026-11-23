//go:build !race

package css

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

// TestSiblingSelectorsAreBoundedByWork is the hostile case of the done
// criterion: 100 000 siblings tested against 5 000 selectors end with a Work
// limit error, or succeed, at the default MaxWork (100 million units, each
// costing at most about 20 ns). The time is logged, and measured by
// BenchmarkHostileSiblings: the task rules keep wall-clock assertions out of go
// test.
func TestSiblingSelectorsAreBoundedByWork(t *testing.T) {
	const n = 100_000
	doc := siblings(t, n)
	for _, text := range []string{"i:last-child", ".k ~ .k", "i:nth-child(2n+1)", ".k + .k", "i ~ b"} {
		t.Run(text, func(t *testing.T) {
			sels, _ := parseSel(t, text)
			b := budget.New(context.Background(), budget.Defaults(), nil)
			start := time.Now()
			var err error
			checks := 0
		outer:
			for range 5000 {
				for e := range doc.Elements {
					if _, err = sels[0].Match(doc, int32(e), b); err != nil {
						break outer
					}
					checks++
				}
			}
			d := time.Since(start)
			var le *budget.LimitError
			if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
				t.Fatalf("err = %v", err)
			}
			t.Logf("%d checks in %v, error %v", checks, d, err)
		})
	}
}
