package budget

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testLimits() Limits {
	return Limits{
		InputBytes: 10, Nodes: 11, CSSBytes: 12, Selectors: 13, VarBytes: 14,
		Styles: 15, Boxes: 16, Glyphs: 17, Pages: 18, OutputBytes: 19, Work: 20,
		HTMLDepth: 512,
	}
}

func newBudget(l Limits) *Budget {
	return New(context.Background(), l, nil)
}

func TestChargeEachKind(t *testing.T) {
	l := testLimits()
	for k := Kind(0); k < HTMLDepth; k++ {
		limit := l[k]
		t.Run(k.String(), func(t *testing.T) {
			b := newBudget(testLimits())
			if err := b.Charge(k, limit-1); err != nil {
				t.Fatalf("just under the limit: %v", err)
			}
			if err := b.Charge(k, 1); err != nil {
				t.Fatalf("at the limit: %v", err)
			}
			err := b.Charge(k, 1)
			var be *LimitError
			if !errors.As(err, &be) || be.Kind != k || be.Max != limit {
				t.Fatalf("over the limit: got %v, want *LimitError{%v, %d}", err, k, limit)
			}
			if got := b.Used(k); got != limit+1 {
				t.Fatalf("Used = %d, want %d (totals keep growing after a failure)", got, limit+1)
			}
		})
	}
}

func TestChargeAtOnce(t *testing.T) {
	b := newBudget(testLimits())
	if err := b.Charge(Nodes, 11); err != nil {
		t.Fatalf("exactly the limit in one charge: %v", err)
	}
	b = newBudget(testLimits())
	if err := b.Charge(Nodes, 12); err == nil {
		t.Fatal("limit+1 in one charge must fail")
	}
}

func TestChargeOverflow(t *testing.T) {
	b := newBudget(Limits{Boxes: math.MaxInt64})
	if err := b.Charge(Boxes, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err := b.Charge(Boxes, 1); err == nil {
		t.Fatal("charge past MaxInt64 must fail without wrapping")
	}
	if got := b.Used(Boxes); got != math.MaxInt64 {
		t.Fatalf("Used = %d, want saturation at MaxInt64", got)
	}
}

func TestChargeInvalid(t *testing.T) {
	b := newBudget(testLimits())
	for _, tc := range []struct {
		k Kind
		n int64
	}{{NumKinds, 1}, {Kind(255), 0}, {Nodes, -1}, {HTMLDepth, 1}} {
		if err := b.Charge(tc.k, tc.n); !errors.Is(err, ErrInvalidCharge) {
			t.Errorf("Charge(%v, %d) = %v, want ErrInvalidCharge", tc.k, tc.n, err)
		}
	}
	if err := b.Charge(Nodes, 0); err != nil {
		t.Errorf("zero charge: %v", err)
	}
}

func TestZeroLimitRejectsPositive(t *testing.T) {
	b := newBudget(Limits{})
	if err := b.Charge(Pages, 1); err == nil {
		t.Fatal("zero limit must reject a positive charge")
	}
	b = newBudget(Limits{})
	if err := b.Charge(Pages, 0); err != nil {
		t.Fatal(err)
	}
}

func TestErrorMessage(t *testing.T) {
	e := &LimitError{Kind: Pages, Max: 500}
	if got, want := e.Error(), "budget: MaxPages exceeded (max 500)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := HTMLDepth.String(); got != "HTMLDepth" {
		t.Fatalf("HTMLDepth: %q", got)
	}
	if got := Kind(200).String(); got != "Kind(200)" {
		t.Fatalf("unknown kind: %q", got)
	}
	if got := newBudget(Limits{}).Used(Kind(200)); got != 0 {
		t.Fatalf("Used(unknown) = %d", got)
	}
}

func TestContextSeenWithinPollInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Work: 1 << 40}, nil)
	cancel()
	for range WorkPollInterval {
		if err := b.Charge(Work, 1); err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want context.Canceled", err)
			}
			return
		}
	}
	t.Fatalf("cancellation not seen within %d Work units", WorkPollInterval)
}

func TestContextJustUnderInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Work: 1 << 40}, nil)
	cancel()
	if err := b.Charge(Work, WorkPollInterval-1); err != nil {
		t.Fatalf("no poll expected before the interval: %v", err)
	}
	if err := b.Charge(Work, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestContextDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), deadlinePassed())
	defer cancel()
	b := New(ctx, Limits{Work: 1 << 40}, nil)
	if err := b.Charge(Work, WorkPollInterval); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
	if err := b.Poll(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Poll = %v", err)
	}
}

func TestNonWorkKindsDoNotPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Nodes: 1 << 40}, nil)
	cancel()
	if err := b.Charge(Nodes, 1<<20); err != nil {
		t.Fatalf("Nodes must not poll the context: %v", err)
	}
	if err := b.Poll(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Poll = %v", err)
	}
}

func TestCheckpoint(t *testing.T) {
	newBudget(Limits{}).Checkpoint("none")
	var got []string
	b := New(context.Background(), Limits{}, func(s string) { got = append(got, s) })
	b.Checkpoint("dom")
	b.Checkpoint("css")
	if strings.Join(got, ",") != "dom,css" {
		t.Fatalf("got %v", got)
	}
}

func TestWarnDedupAndCount(t *testing.T) {
	b := newBudget(Limits{})
	b.Warn("clamped", "width")
	b.Warn("clamped", "width")
	b.Warn("clamped", "height")
	w, dropped := b.Warnings()
	if dropped != 0 || len(w) != 2 {
		t.Fatalf("got %+v dropped=%d", w, dropped)
	}
	if w[0] != (Warning{"clamped", "height", 1}) || w[1] != (Warning{"clamped", "width", 2}) {
		t.Fatalf("got %+v", w)
	}
}

func TestWarnSorted(t *testing.T) {
	b := newBudget(Limits{})
	b.Warn("b", "z")
	b.Warn("b", "a")
	b.Warn("a", "z")
	w, _ := b.Warnings()
	var got []string
	for _, x := range w {
		got = append(got, string(x.Code)+"/"+x.Detail)
	}
	if want := "a/z,b/a,b/z"; strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestWarnCap(t *testing.T) {
	b := newBudget(Limits{})
	for i := range MaxWarnings - 1 {
		b.Warn("c", fmt.Sprint(i))
	}
	b.Warn("c", "last")
	if w, dropped := b.Warnings(); len(w) != MaxWarnings || dropped != 0 {
		t.Fatalf("at the cap: %d entries, %d dropped", len(w), dropped)
	}
	b.Warn("c", "over1")
	b.Warn("c", "over2")
	b.Warn("c", "over1")
	b.Warn("c", "0")
	b.Warn("c", "0")
	w, dropped := b.Warnings()
	if len(w) != MaxWarnings || dropped != 3 {
		t.Fatalf("over the cap: %d entries, %d dropped, want %d and 3", len(w), dropped, MaxWarnings)
	}
	for _, x := range w {
		if x.Detail == "0" && x.Count != 3 {
			t.Fatalf("existing entry must keep counting: %+v", x)
		}
	}
}

func TestWarningsReturnsCopy(t *testing.T) {
	b := newBudget(Limits{})
	b.Warn("c", "d")
	w, _ := b.Warnings()
	w[0].Count = 99
	if w2, _ := b.Warnings(); w2[0].Count != 1 {
		t.Fatal("Warnings must return a copy")
	}
}

func TestSanitizeDetail(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "color", "color"},
		{"empty", "", ""},
		{"C0", "a\x00b\nc\x1b", "a?b?c?"},
		{"DEL and C1", "\x7f\u0085\u009f\u00a0", "???\u00a0"},
		{"bidi override", "a\u202eb", "a?b"},
		{"zero width", "a\u200bb", "a?b"},
		{"invalid utf8", "a\xffb\xc3", "a?b?"},
		{"real replacement char kept", "a\uFFFDb", "a\uFFFDb"},
		{"format chars", "\u200f\u2060\u2064\u2066\u2069\ufeff", "??????"},
		{"neighbours kept", "\u200a\u2010\u205f\u2065\u034f", "\u200a\u2010\u205f\u2065\u034f"},
		{"line and paragraph separators", "a\u2028b\u2029c", "a?b?c"},
		{"arabic letter mark", "a\u061cb", "a?b"},
		{"soft hyphen", "a\u00adb", "a?b"},
		{"mongolian vowel separator", "a\u180eb", "a?b"},
		{"word joiner family", "\u2060\u2064\u206a", "???"},
		{"tag characters", "a\U000E0041\U000E007Fb", "a??b"},
		{"just outside tags", "\U000E0080", "\U000E0080"},
		{"multibyte kept", "\u00e9\u4e16", "\u00e9\u4e16"},
		{"exactly 80", strings.Repeat("a", 80), strings.Repeat("a", 80)},
		{"81 cut", strings.Repeat("a", 81), strings.Repeat("a", 80)},
		{"cut on rune boundary", strings.Repeat("a", 79) + "é", strings.Repeat("a", 79)},
		{"replacement kept wide", strings.Repeat("\uFFFD", 40), strings.Repeat("\uFFFD", 26)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeDetail(tc.in)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if len(got) > MaxDetailBytes || !utf8.ValidString(got) {
				t.Fatalf("invalid result %q", got)
			}
		})
	}
}

func TestWarnSanitizesBeforeDedup(t *testing.T) {
	b := newBudget(Limits{})
	long := strings.Repeat("x", 200)
	b.Warn("c", long)
	b.Warn("c", long+"y")
	w, _ := b.Warnings()
	if len(w) != 1 || w[0].Count != 2 || len(w[0].Detail) != MaxDetailBytes {
		t.Fatalf("got %+v", w)
	}
}

func FuzzSanitizeDetail(f *testing.F) {
	f.Add("a\xff\u202e\x00é")
	f.Fuzz(func(t *testing.T, s string) {
		got := sanitizeDetail(s)
		if len(got) > MaxDetailBytes || !utf8.ValidString(got) {
			t.Fatalf("invalid %q", got)
		}
		for _, r := range got {
			if r < 0x20 || r >= 0x7f && r <= 0x9f {
				t.Fatalf("control char in %q", got)
			}
		}
	})
}

func BenchmarkCharge(b *testing.B) {
	bg := newBudget(Limits{Nodes: 1 << 62})
	b.ReportAllocs()
	for b.Loop() {
		if err := bg.Charge(Nodes, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChargeWork(b *testing.B) {
	bg := newBudget(Limits{Work: 1 << 62})
	b.ReportAllocs()
	for b.Loop() {
		if err := bg.Charge(Work, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func deadlinePassed() time.Time { return time.Now().Add(-time.Second) }

func TestDefaults(t *testing.T) {
	if HTMLDepth != NumKinds-1 {
		t.Fatal("HTMLDepth must be the last Kind: Charge relies on it")
	}
	d := Defaults()
	for k := Kind(0); k < NumKinds; k++ {
		if d[k] <= 0 {
			t.Errorf("%v default = %d, want > 0", k, d[k])
		}
	}
	if d[HTMLDepth] != 512 || d[Pages] != 500 {
		t.Errorf("unexpected defaults: %v", d)
	}
}

func TestWorkPollOncePerCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := New(ctx, Limits{Work: 1 << 40}, nil)
	cancel()
	if err := b.Charge(Work, 3*WorkPollInterval+5); !errors.Is(err, context.Canceled) {
		t.Fatalf("big charge crossing several multiples: %v", err)
	}
	if err := b.Charge(Work, 1); err != nil {
		t.Fatalf("no poll expected until the next multiple: %v", err)
	}
}

func TestMax(t *testing.T) {
	b := newBudget(testLimits())
	if got := b.Max(Nodes); got != 11 {
		t.Fatalf("Max(Nodes) = %d", got)
	}
	if got := b.Max(NumKinds); got != 0 {
		t.Fatalf("Max(unknown) = %d", got)
	}
}
