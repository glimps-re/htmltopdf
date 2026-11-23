package css

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// lvDoc holds an element with a 1 MiB attribute, one with 150 000 classes and
// one with repeated classes.
func lvDoc(tb testing.TB) (d *dom.Document, big, classy, dup int32) {
	tb.Helper()
	var classes strings.Builder
	for i := range 150_000 {
		fmt.Fprintf(&classes, "c%d ", i)
	}
	value := strings.Repeat("ab ", 350_000)
	d = parseHTML(tb, `<!doctype html><p data-k="`+value+`" lang="`+strings.ToUpper(value)+`">x</p><p class="`+classes.String()+`">y</p><p class="a a a a">z</p>`)
	n := clampIdx(len(d.Elements))
	return d, n - 3, n - 2, n - 1
}

// Attribute operators on a 1 MiB value and elements with 150 000 classes: a
// Match must not copy the value (the allocation count is the load-independent
// proxy of the time) and must charge for the scan.
func TestLargeValuesDoNotAllocate(t *testing.T) {
	d, big, classy, _ := lvDoc(t)
	for _, tc := range []struct {
		sel string
		e   int32
	}{
		{"[data-k~=zz]", big},
		{"[data-k*=zz]", big},
		{"[data-k^=zz]", big},
		{"[data-k$=zz]", big},
		{"[data-k|=zz]", big},
		{"[data-k=zz]", big},
		{"[data-k]", big},
		{"[data-k~=zz i]", big},
		{"[data-k*=zz i]", big},
		{"[data-k^=zz i]", big},
		{"[data-k$=zz i]", big},
		{"[data-k|=zz i]", big},
		{"[data-k=zz i]", big},
		{"[lang~=zz i]", big},
		{"[class~=nope]", classy},
		{"[class*=nope]", classy},
		{"[class=nope]", classy},
		{"[class^=nope]", classy},
		{".nope", classy},
		{".c149999", classy},
	} {
		t.Run(tc.sel, func(t *testing.T) {
			sels, _ := parseSel(t, tc.sel)
			l := budget.Defaults()
			l[budget.Work] = 1 << 60
			b := budget.New(context.Background(), l, nil)
			if allocs := testing.AllocsPerRun(5, func() { _, _ = sels[0].Match(d, tc.e, b) }); allocs != 0 {
				t.Errorf("%d allocations per Match on a large value", int(allocs))
			}
			before := b.Used(budget.Work)
			_, _ = sels[0].Match(d, tc.e, b)
			if used := b.Used(budget.Work) - before; used < 1000 {
				t.Errorf("only %d work units charged for a scan of a large value", used)
			}
		})
	}
}

func TestDuplicateClassesMatch(t *testing.T) {
	d, _, _, dup := lvDoc(t)
	if got := d.Elements[dup].Classes; len(got) != 1 || got[0] != "a" {
		t.Fatalf("classes %v", got)
	}
	for _, s := range []string{".a", ".a.a", ".a.a.a.a.a", "p.a.a", "[class=a]", "[class~=a]", "[class='a a a a']"} {
		sels, _ := parseSel(t, s)
		ok, err := sels[0].Match(d, dup, oracleBudget())
		want := s != "[class='a a a a']"
		if err != nil || ok != want {
			t.Errorf("%s: %v %v, want %v", s, ok, err, want)
		}
	}
	sels, _ := parseSel(t, ".a.a.a")
	if sp := sels[0].Specificity(); sp.Classes() != 3 {
		t.Errorf("specificity of .a.a.a: %d classes, want 3", sp.Classes())
	}
}

// Time per Work unit on large values: design §6.2 wants about 20 ns; a factor
// of 10 keeps the test valid on a loaded machine.
func TestLargeValueWorkIsHonest(t *testing.T) {
	if testing.Short() || raceOn {
		t.Skip("timing")
	}
	d, big, classy, _ := lvDoc(t)
	for _, tc := range []struct {
		sel string
		e   int32
	}{{"[data-k~=zz]", big}, {"[data-k~=zz i]", big}, {"[data-k*=zz i]", big}, {"[class=nope]", classy}, {"[class*=nope]", classy}} {
		sels, _ := parseSel(t, tc.sel)
		l := budget.Defaults()
		l[budget.Work] = 1 << 60
		b := budget.New(context.Background(), l, nil)
		start := time.Now()
		for range 5 {
			_, _ = sels[0].Match(d, tc.e, b)
		}
		perUnit := float64(time.Since(start).Nanoseconds()) / float64(b.Used(budget.Work))
		t.Logf("%-16s %.1f ns/unit", tc.sel, perUnit)
		if perUnit > 200 {
			t.Errorf("%s: %.0f ns per Work unit (target 20): MaxWork would allow %.0f s", tc.sel, perUnit, perUnit*float64(budget.Defaults()[budget.Work])/1e9)
		}
	}
}

func BenchmarkLargeValueWork(b *testing.B) {
	d, big, classy, _ := lvDoc(b)
	for _, tc := range []struct {
		sel string
		e   int32
	}{{"[data-k~=zz]", big}, {"[data-k*=zz i]", big}, {"[data-k*=zz]", big}, {"[class=nope]", classy}, {".nope", classy}} {
		sels, _ := parseSel(b, tc.sel)
		b.Run(tc.sel, func(b *testing.B) {
			benchWork(b, func(_ int, bud *budget.Budget) { _, _ = sels[0].Match(d, tc.e, bud) })
		})
	}
}
