package budget

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestWarnSameDetailDifferentCodesAreDistinct(t *testing.T) {
	b := newBudget(Limits{})
	b.Warn("a", "x")
	b.Warn("b", "x")
	if w, _ := b.Warnings(); len(w) != 2 {
		t.Fatalf("got %+v", w)
	}
}

func TestWarningsEmpty(t *testing.T) {
	w, dropped := newBudget(Limits{}).Warnings()
	if len(w) != 0 || dropped != 0 {
		t.Fatalf("got %+v dropped=%d", w, dropped)
	}
}

func TestWarnCapDedupsOnSanitizedKey(t *testing.T) {
	b := newBudget(Limits{})
	for i := range MaxWarnings {
		b.Warn("c", strings.Repeat("a", i%70)+string(rune('A'+i/70))+strings.Repeat("b", i%7))
	}
	b.Warn("c", "new\x00")
	b.Warn("c", "new\n")
	if w, dropped := b.Warnings(); len(w) != MaxWarnings || dropped != 2 {
		t.Fatalf("%d entries, %d dropped", len(w), dropped)
	}
}

func TestWarnHugeHostileDetailIsBounded(t *testing.T) {
	huge := strings.Repeat("\xe2\x80\xae", 8<<20)
	b := newBudget(Limits{})
	for range 1000 {
		b.Warn("c", huge)
	}
	w, _ := b.Warnings()
	if len(w) != 1 || w[0].Count != 1000 || len(w[0].Detail) > MaxDetailBytes {
		t.Fatalf("got %d entries, first %+v", len(w), w[0])
	}
}

func TestLimitErrorWinsOverContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Work: WorkPollInterval}, nil)
	cancel()
	err := b.Charge(Work, WorkPollInterval+1)
	var le *LimitError
	if !errors.As(err, &le) || le.Kind != Work {
		t.Fatalf("got %v, want *LimitError for Work", err)
	}
}

func TestContextErrorIsUnwrapped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Work: 1 << 40}, nil)
	cancel()
	if err := b.Charge(Work, WorkPollInterval); !errors.Is(err, context.Canceled) || errors.Unwrap(err) != nil {
		t.Fatalf("got %#v, want unwrapped context.Canceled", err)
	}
}

func TestPollNilOnLiveContext(t *testing.T) {
	if err := newBudget(Limits{}).Poll(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkPollAtExactMultiple(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Work: 1 << 40}, nil)
	if err := b.Charge(Work, WorkPollInterval); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := b.Charge(Work, WorkPollInterval-1); err != nil {
		t.Fatalf("still below the next multiple: %v", err)
	}
	if err := b.Charge(Work, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func BenchmarkWarnHugeDetail(b *testing.B) {
	huge := strings.Repeat("\xe2\x80\xae", 8<<20)
	bg := newBudget(Limits{})
	b.ReportAllocs()
	for b.Loop() {
		bg.Warn("c", huge)
	}
}

func TestWarnDetailDoesNotPinCallerBuffer(t *testing.T) {
	buf := strings.Repeat("x", 1<<20)
	b := newBudget(Limits{})
	b.Warn("c", buf[:40])
	w, _ := b.Warnings()
	if reflect.ValueOf(w[0].Detail).Pointer() == reflect.ValueOf(buf).Pointer() {
		t.Fatal("stored detail shares memory with the caller's buffer")
	}
}

func TestWarnRepeatDoesNotAllocate(t *testing.T) {
	b := newBudget(Limits{})
	b.Warn("c", "detail")
	if n := testing.AllocsPerRun(100, func() { b.Warn("c", "detail") }); n != 0 {
		t.Fatalf("repeat allocs = %v, want 0", n)
	}
}

func TestWarnDetailReplacesZeroWidthJoiners(t *testing.T) {
	b := newBudget(Limits{})
	b.Warn("c", "a\u200Cb\u200Dc")
	if w, _ := b.Warnings(); w[0].Detail != "a?b?c" {
		t.Fatalf("got %q", w[0].Detail)
	}
}
