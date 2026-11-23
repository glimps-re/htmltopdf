package css

import (
	"context"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func BenchmarkMatch(b *testing.B) {
	doc := siblings(b, 1000)
	for _, text := range []string{"i:last-child", ".k ~ .k", "i:nth-child(2n+1)", "div > i", "div i.k", "p"} {
		sels, _ := parseSel(b, text)
		b.Run(text, func(b *testing.B) {
			l := budget.Defaults()
			l[budget.Work] = 1 << 62
			bud := budget.New(context.Background(), l, nil)
			n := clampIdx(len(doc.Elements))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := sels[0].Match(doc, int32(i)%n, bud); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchWork runs match for b.N iterations and reports the time per Work unit
// charged: design §6.2 wants at most about 20 ns per unit on a quiet machine.
func benchWork(b *testing.B, match func(i int, bud *budget.Budget)) {
	l := budget.Defaults()
	l[budget.Work] = 1 << 62
	bud := budget.New(context.Background(), l, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		match(i, bud)
	}
	b.StopTimer()
	if units := bud.Used(budget.Work); units > 0 {
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(units), "ns/unit")
	}
}

// BenchmarkWorkCandidate: one Work unit per candidate of the index (a single
// compound that fails on the tag).
func BenchmarkWorkCandidate(b *testing.B) {
	doc := siblings(b, 1000)
	sels, _ := parseSel(b, "p")
	n := len(doc.Elements)
	benchWork(b, func(i int, bud *budget.Budget) { _, _ = sels[0].Match(doc, clampIdx(i%n), bud) })
}

// BenchmarkWorkCombinatorStep: ancestors visited by a descendant combinator.
func BenchmarkWorkCombinatorStep(b *testing.B) {
	doc := parseHTML(b, deep(300))
	sels, _ := parseSel(b, ".zz p")
	last := clampIdx(len(doc.Elements) - 1)
	benchWork(b, func(_ int, bud *budget.Budget) { _, _ = sels[0].Match(doc, last, bud) })
}

// BenchmarkWorkSiblingStep: previous siblings visited by "~".
func BenchmarkWorkSiblingStep(b *testing.B) {
	doc := siblings(b, 5000)
	sels, _ := parseSel(b, "b ~ i")
	last := clampIdx(len(doc.Elements) - 1)
	benchWork(b, func(_ int, bud *budget.Budget) { _, _ = sels[0].Match(doc, last, bud) })
}

// BenchmarkHostileSiblings is the hostile case of M1-05: 100 000 siblings
// tested against selectors until MaxWork (the default, 100 million) is
// exceeded. Design §6.2 wants each run under 2 seconds on a quiet machine.
func BenchmarkHostileSiblings(b *testing.B) {
	doc := siblings(b, 100_000)
	for _, text := range []string{"i:last-child", ".k ~ .k", "i:nth-child(2n+1)", ".k + .k", "i ~ b"} {
		sels, _ := parseSel(b, text)
		b.Run(text, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				bud := budget.New(context.Background(), budget.Defaults(), nil)
			run:
				for range 5000 {
					for e := range doc.Elements {
						if _, err := sels[0].Match(doc, clampIdx(e), bud); err != nil {
							break run
						}
					}
				}
			}
		})
	}
}
