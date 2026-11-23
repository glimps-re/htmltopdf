//go:build !race

package css_test

import (
	"context"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

// peakHeap runs fn while sampling the live heap (objects, garbage included
// until the next collection) and returns the highest sample above the heap
// at the start, GOGC=100.
func peakHeap(fn func()) uint64 {
	old := debug.SetGCPercent(100)
	defer debug.SetGCPercent(old)
	runtime.GC()
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	base := s[0].Value.Uint64()
	var peak atomic.Uint64
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			ms := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
			metrics.Read(ms)
			if v := ms[0].Value.Uint64(); v > peak.Load() {
				peak.Store(v)
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()
	fn()
	stop.Store(true)
	<-done
	return peak.Load() - min(base, peak.Load())
}

// Peak bounds of design §5.2.1: token-bound sheets are dominated by the token
// arena; rule- and declaration-heavy sheets add the rule and declaration tables.
const (
	maxTokenPeak = 24 << 20
	maxTablePeak = 56 << 20
)

func TestPeakHeapAtCSSLimit(t *testing.T) {
	limit := int(budget.Defaults()[budget.CSSBytes])
	pad := func(s string) string { return s + strings.Repeat(" ", limit-len(s)) }
	var decls strings.Builder
	decls.WriteString("a{")
	for i := 0; decls.Len() < limit-8; i++ {
		decls.WriteString("x:1;")
	}
	decls.WriteString("}")
	sheets := map[string]struct {
		src string
		max uint64
	}{
		"selector list": {pad(strings.Repeat("a,", (limit-12)/2) + "a{b:c}"), maxTokenPeak},
		"repeated rule": {pad(strings.Repeat("a{b:c}", limit/6)), maxTablePeak},
		"declarations":  {pad(decls.String()), maxTablePeak},
		"deep values":   {pad("a{x:" + strings.Repeat("f(1,", 60)), maxTokenPeak},
	}
	for name, sh := range sheets {
		src := sh.src
		t.Run(name, func(t *testing.T) {
			if len(src) > limit {
				t.Fatalf("sheet is %d bytes", len(src))
			}
			var ss *css.Stylesheet
			peak := peakHeap(func() {
				b := budget.New(context.Background(), budget.Defaults(), nil)
				var err error
				if ss, err = css.ParseStylesheet([]byte(src), css.OriginDocument, b); err != nil {
					t.Error(err)
				}
			})
			runtime.KeepAlive(ss)
			t.Logf("%d bytes: peak heap %.1f MiB", len(src), float64(peak)/(1<<20))
			if peak > sh.max {
				t.Errorf("peak %d MiB > %d MiB", peak>>20, sh.max>>20)
			}
		})
	}
}

func TestKelvinSignIsNotAKeyword(t *testing.T) {
	b0 := budget.New(context.Background(), budget.Defaults(), nil)
	ds, err := css.ParseDeclarations([]byte("color:Blac\u212A;background:BLACK"), b0)
	if err != nil || len(ds) != 2 {
		t.Fatal(err, ds)
	}
	if _, ok := css.ParseColor(ds[0].Value); ok {
		t.Error("BlacK (Kelvin sign) parsed as the color black")
	}
	if _, ok := css.ParseColor(ds[1].Value); !ok {
		t.Error("BLACK rejected")
	}

	// U+212A KELVIN SIGN lowercases to "k" in Unicode, but CSS keywords are
	// matched with ASCII case folding only.
	for _, c := range []struct{ src, what string }{
		{"a{Kolor:red}", "property"},
		{"@Keyframes x{}", "at-rule"},
		{"@page{@top-Keft{x:y}}", "margin box"},
		{"a{b:BlaK}", "value"},
	} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ss, err := css.ParseStylesheet([]byte(c.src), css.OriginDocument, b)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range ss.Rules {
			for _, d := range r.Decls {
				if strings.ContainsRune(d.Name, 'k') && strings.Contains(c.src, "K") && d.Name != "b" {
					t.Errorf("%s: Kelvin sign folded to k in %q", c.what, d.Name)
				}
				for _, v := range d.Value {
					if strings.Contains(v.Text, "k") && strings.Contains(c.src, "BlaK") {
						t.Errorf("%s: value %q folded", c.what, v.Text)
					}
				}
			}
			for _, m := range r.Rules {
				if m.Kind == css.RuleMarginBox {
					t.Errorf("%s: margin box %q recognised", c.what, m.Name)
				}
			}
		}
	}
}
