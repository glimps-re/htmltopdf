//go:build !race

package css

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// rvPeak runs fn and returns the highest live heap above the starting heap.
func rvPeak(fn func()) uint64 {
	runtime.GC()
	name := "/memory/classes/heap/objects:bytes"
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	base := s[0].Value.Uint64()
	var hi atomic.Uint64
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			ms := []metrics.Sample{{Name: name}}
			metrics.Read(ms)
			if v := ms[0].Value.Uint64(); v > hi.Load() {
				hi.Store(v)
			}
			time.Sleep(100 * time.Microsecond)
		}
	}()
	fn()
	stop.Store(true)
	<-done
	return hi.Load() - min(base, hi.Load())
}

// rvRun tests every candidate of every element with the default budget and
// returns the tests done, the matches, the work used and the final error.
func rvRun(tb testing.TB, d *dom.Document, ix *Index[int]) (tests, matches int, work int64, err error) {
	tb.Helper()
	b := budget.New(context.Background(), budget.Defaults(), nil)
	var buf []Entry[int]
	for e := range d.Elements {
		if buf, err = ix.Candidates(d, clampIdx(e), buf[:0], b); err != nil {
			break
		}
		for _, c := range buf {
			var ok bool
			if ok, err = c.Selector.Match(d, clampIdx(e), b); err != nil {
				break
			}
			tests++
			if ok {
				matches++
			}
		}
		if err != nil {
			break
		}
	}
	return tests, matches, b.Used(budget.Work), err
}

func rvIndex(tb testing.TB, text func(i int) string) *Index[int] {
	const n = 5000
	tb.Helper()
	var ix Index[int]
	for i := range n {
		ss, w := parseSel(tb, text(i))
		if len(ss) != 1 {
			tb.Fatalf("%q: %v", text(i), w)
		}
		ix.Add(ss[0], i)
	}
	return &ix
}

func rvCheckErr(t *testing.T, err error) {
	t.Helper()
	var le *budget.LimitError
	if err != nil && (!errors.As(err, &le) || le.Kind != budget.Work) {
		t.Fatalf("error %v", err)
	}
}

// 1-1.5 MiB attribute values x each operator, with and without i, x 5 000
// attribute-only selectors (every element is a candidate): LimitWork or done,
// bounded heap; the twin with a 1 KiB value completes.
func TestHostileAttributeScans(t *testing.T) {
	big := strings.Repeat("ab ", 400_000)
	d := parseHTML(t, `<!doctype html><p x="`+big+`" lang="`+strings.ToUpper(big)+`">x</p><p x="en-US">y</p><p x="`+strings.Repeat("a", 1<<20)+`">z</p>`)
	small := parseHTML(t, `<!doctype html><p x="`+big[:1024]+`">x</p><p x="en-US">y</p>`)
	for _, op := range []string{"=", "~=", "|=", "^=", "$=", "*="} {
		for _, flag := range []string{"", " i"} {
			t.Run(op+flag, func(t *testing.T) {
				ix := rvIndex(t, func(i int) string { return fmt.Sprintf("[x%s\"zz%d\"%s]", op, i, flag) })
				var tests int
				var work int64
				var err error
				start := time.Now()
				peak := rvPeak(func() { tests, _, work, err = rvRun(t, d, ix) })
				took := time.Since(start)
				rvCheckErr(t, err)
				t.Logf("%d tests, work %d, %v, limit hit %v, peak %d MiB", tests, work, took.Round(time.Millisecond), err != nil, peak>>20)
				if peak > 64<<20 {
					t.Errorf("peak heap %d MiB", peak>>20)
				}
				if _, _, _, err := rvRun(t, small, ix); err != nil {
					t.Errorf("1 KiB twin: %v", err)
				}
			})
		}
	}
}

func TestHostileClasses(t *testing.T) {
	var cl strings.Builder
	for i := range 100_000 {
		fmt.Fprintf(&cl, "c%d ", i)
	}
	d := parseHTML(t, `<!doctype html><p class="`+cl.String()+`">x</p>`)
	if n := len(d.Elements[len(d.Elements)-1].Classes); n != 100_000 {
		t.Fatalf("%d classes", n)
	}
	t.Run("100k classes x 5000 .cN.zz", func(t *testing.T) {
		ix := rvIndex(t, func(i int) string { return fmt.Sprintf(".c%d.zz", i) })
		var err error
		var tests int
		var work int64
		start := time.Now()
		peak := rvPeak(func() { tests, _, work, err = rvRun(t, d, ix) })
		rvCheckErr(t, err)
		t.Logf("%d tests, work %d, %v, limit hit %v, peak %d MiB", tests, work, time.Since(start).Round(time.Millisecond), err != nil, peak>>20)
		if peak > 64<<20 {
			t.Errorf("peak %d MiB", peak>>20)
		}
	})
	t.Run("100k classes x 5000 [class~=x]", func(t *testing.T) {
		ix := rvIndex(t, func(i int) string { return fmt.Sprintf("[class~=zz%d]", i) })
		start := time.Now()
		_, _, work, err := rvRun(t, d, ix)
		rvCheckErr(t, err)
		t.Logf("work %d, %v, limit hit %v", work, time.Since(start).Round(time.Millisecond), err != nil)
	})
	t.Run("100k classes x universal .zz", func(t *testing.T) {
		ix := rvIndex(t, func(i int) string { return fmt.Sprintf("*.zz%d", i) })
		start := time.Now()
		_, _, work, err := rvRun(t, d, ix)
		rvCheckErr(t, err)
		t.Logf("work %d, %v, limit hit %v", work, time.Since(start).Round(time.Millisecond), err != nil)
	})
	t.Run("2 MiB of x is one class", func(t *testing.T) {
		d := parseHTML(t, `<!doctype html><p class="`+strings.Repeat("x ", 1<<20)+`">x</p>`)
		if got := d.Elements[len(d.Elements)-1].Classes; len(got) != 1 {
			t.Fatalf("%d classes", len(got))
		}
		ix := rvIndex(t, func(i int) string { return fmt.Sprintf(".x:nth-child(%dn+1)", i+1) })
		start := time.Now()
		_, matches, work, err := rvRun(t, d, ix)
		rvCheckErr(t, err)
		t.Logf("%d matches, work %d, %v", matches, work, time.Since(start).Round(time.Millisecond))
	})
}

// Candidates are charged before they are appended: many elements with huge
// class lists against a large index end in LimitWork with a bounded heap.
func TestCandidatesAreBoundedByWork(t *testing.T) {
	var cl strings.Builder
	for i := range 20_000 {
		fmt.Fprintf(&cl, "c%d ", i)
	}
	var doc strings.Builder
	doc.WriteString("<!doctype html>")
	for range 40 {
		doc.WriteString(`<p class="` + cl.String() + `"></p>`)
	}
	d := parseHTML(t, doc.String())
	// 5 000 selectors filed under class c0..c19999: each element has 20 000
	// classes, each class with an entry list.
	ix := rvIndex(t, func(i int) string { return fmt.Sprintf(".c%d", i*4) })
	var err error
	var work int64
	start := time.Now()
	peak := rvPeak(func() { _, _, work, err = rvRun(t, d, ix) })
	rvCheckErr(t, err)
	t.Logf("work %d, %v, limit hit %v, peak %d MiB", work, time.Since(start).Round(time.Millisecond), err != nil, peak>>20)
	if peak > 64<<20 {
		t.Errorf("peak heap %d MiB", peak>>20)
	}
	// Tight budget: the error comes before the slice grows.
	l := budget.Defaults()
	l[budget.Work] = 1000
	b := budget.New(context.Background(), l, nil)
	buf, err := ix.Candidates(d, clampIdx(len(d.Elements)-1), nil, b)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Work || len(buf) > 200 {
		t.Errorf("tight budget: %d entries, err %v", len(buf), err)
	}
}

func TestIFlagIsASCIIOnly(t *testing.T) {
	d := parseHTML(t, "<!doctype html><p id=i1 a=\"K\"></p><p id=i2 a=\"k\"></p><p id=i3 a=\"K\"></p><p id=i4 a=\"İ\"></p>")
	for _, tc := range []struct {
		sel  string
		want []int
	}{
		{`[a="k" i]`, []int{2, 3}},
		{`[a="K" i]`, []int{2, 3}},
		{"[a=\"K\" i]", []int{1}},
		{`[a*="k" i]`, []int{2, 3}},
		{`[a^="k" i]`, []int{2, 3}},
		{`[a~="k" i]`, []int{2, 3}},
		{`[a|="k" i]`, []int{2, 3}},
		{`[a="i" i]`, []int{}},
		{"[a=\"İ\" i]", []int{4}},
	} {
		got := matchingIDs(t, d, tc.sel)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v want %v", tc.sel, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v want %v", tc.sel, got, tc.want)
			}
		}
	}
}

func TestMediaCapAndOnly(t *testing.T) {
	l := budget.Defaults()
	b := budget.New(context.Background(), l, nil)
	src := "@media " + strings.Repeat("a,", 250_000) + "a{x{y:z}}"
	ss, err := ParseStylesheet([]byte(src), OriginDocument, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss.Rules) != 1 {
		t.Fatalf("%d rules", len(ss.Rules))
	}
	b = budget.New(context.Background(), l, nil)
	q := ParseMediaQuery(ss.Rules[0].Prelude, b)
	if got := len(q.alts); got > 32 {
		t.Errorf("%d alternatives kept", got)
	}
	w := warnings(b)
	count := 0
	for _, x := range w {
		if strings.Contains(x, "more than 32 media queries") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("warnings %q: want exactly one cap warning", w)
	}
	// 32 alternatives pass without a warning, 33 trigger it.
	for _, tc := range []struct {
		n    int
		warn bool
	}{{31, false}, {32, false}, {33, true}} {
		q, w := mediaQuery(t, strings.Repeat("print,", tc.n-1)+"screen")
		cap := false
		for _, x := range w {
			cap = cap || strings.Contains(x, "more than 32")
		}
		if cap != tc.warn {
			t.Errorf("%d alternatives: warnings %q", tc.n, w)
		}
		_ = q
	}
	for _, text := range []string{"only (min-width:1px)", "only (min-width: 1px) and (max-width: 2px)"} {
		q, w := mediaQuery(t, text)
		if q.Match(MediaEnv{Print: true, Width: 794, Height: 1123}) || len(w) != 1 || !strings.HasPrefix(w[0], "invalid-value:") {
			t.Errorf("%q: warnings %q", text, w)
		}
	}
}
