package svg

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

const kappa = 0.5522847498307936

func TestParsePath(t *testing.T) {
	tests := []struct {
		name, d string
		want    []Seg
	}{
		{"empty", "", nil},
		{"must start with moveto", "L1 1", nil},
		{"moveto lineto", "M1 2 L3 4", []Seg{mv(1, 2), ln(3, 4)}},
		{"relative moveto first is absolute", "m1 2 l1 1", []Seg{mv(1, 2), ln(2, 3)}},
		{"implicit lineto after M", "M0 0 1 1 2 2", []Seg{mv(0, 0), ln(1, 1), ln(2, 2)}},
		{"implicit lineto after m", "m0 0 1 1 1 1", []Seg{mv(0, 0), ln(1, 1), ln(2, 2)}},
		{"implicit repeat of l", "M0 0 l1 0 0 1", []Seg{mv(0, 0), ln(1, 0), ln(1, 1)}},
		{"h and v", "M1 1 H5 V7 h-2 v-3", []Seg{mv(1, 1), ln(5, 1), ln(5, 7), ln(3, 7), ln(3, 4)}},
		{"compact numbers", "M10-5L-.5.5", []Seg{mv(10, -5), ln(-.5, .5)}},
		{"two dots", "M.5.5.5.5", []Seg{mv(.5, .5), ln(.5, .5)}},
		{"exponents", "M1e1 2E-1 l1e0 0", []Seg{mv(10, .2), ln(11, .2)}},
		{"commas and spaces", " M 1 , 2 L3,4 ", []Seg{mv(1, 2), ln(3, 4)}},
		{"close then draw starts a subpath", "M1 1 L2 1 L2 2 Z L5 5", []Seg{mv(1, 1), ln(2, 1), ln(2, 2), closeSeg(), mv(1, 1), ln(5, 5)}},
		{"close then moveto", "M1 1 L2 1 z M5 5 L6 6", []Seg{mv(1, 1), ln(2, 1), closeSeg(), mv(5, 5), ln(6, 6)}},
		{"double close", "M1 1 L2 1 ZZ", []Seg{mv(1, 1), ln(2, 1), closeSeg()}},
		{"cubic", "M0 0 C1 2 3 4 5 6", []Seg{mv(0, 0), cu(pt(1, 2), pt(3, 4), pt(5, 6))}},
		{"relative cubic repeats", "M1 1 c1 0 1 1 0 1 1 0 1 1 0 1", []Seg{mv(1, 1), cu(pt(2, 1), pt(2, 2), pt(1, 2)), cu(pt(2, 2), pt(2, 3), pt(1, 3))}},
		{"smooth reflects the cubic", "M0 0 C0 1 1 2 2 2 S4 3 5 2", []Seg{mv(0, 0), cu(pt(0, 1), pt(1, 2), pt(2, 2)), cu(pt(3, 2), pt(4, 3), pt(5, 2))}},
		{"smooth after a line has no reflection", "M0 0 L2 2 S4 3 5 2", []Seg{mv(0, 0), ln(2, 2), cu(pt(2, 2), pt(4, 3), pt(5, 2))}},
		{"relative smooth", "M0 0 c0 1 1 2 2 2 s2 1 3 0", []Seg{mv(0, 0), cu(pt(0, 1), pt(1, 2), pt(2, 2)), cu(pt(3, 2), pt(4, 3), pt(5, 2))}},
		{"quadratic as cubic", "M0 0 Q3 3 6 0", []Seg{mv(0, 0), cu(pt(2, 2), pt(4, 2), pt(6, 0))}},
		{"smooth quadratic reflects", "M0 0 Q3 3 6 0 T12 0", []Seg{mv(0, 0), cu(pt(2, 2), pt(4, 2), pt(6, 0)), cu(pt(8, -2), pt(10, -2), pt(12, 0))}},
		{"T after a cubic has no reflection", "M0 0 C0 0 1 1 3 0 T6 0", []Seg{mv(0, 0), cu(pt(0, 0), pt(1, 1), pt(3, 0)), cu(pt(3, 0), pt(4, 0), pt(6, 0))}},
		{"error ends the path", "M0 0 L1 1 L2 x L9 9", []Seg{mv(0, 0), ln(1, 1)}},
		{"incomplete command ends the path", "M0 0 L1 1 L2", []Seg{mv(0, 0), ln(1, 1)}},
		{"unknown command ends the path", "M0 0 L1 1 B2 2 L5 5", []Seg{mv(0, 0), ln(1, 1)}},
		{"numbers after Z end the path", "M0 0 L1 1 Z 5 5", []Seg{mv(0, 0), ln(1, 1), closeSeg()}},
		{"comma before a command ends the path", "M0 0 L1 1, L2 2", []Seg{mv(0, 0), ln(1, 1)}},
		{"double comma", "M0,,0", nil},
		{"clamped", "M1e30 -1e30 l1e30 1e30", []Seg{mv(MaxCoord, -MaxCoord), ln(MaxCoord, 0)}},
		{"underflow is zero", "M1e-999 0 L1 1", []Seg{mv(0, 0), ln(1, 1)}},
		{"lone sign", "M- 1", nil},
		{"lone exponent letter", "M1e 2", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParsePath(tt.d)
			if !ok {
				t.Fatal("rejected")
			}
			if !segsEqual(got, tt.want) {
				t.Errorf("ParsePath(%q)\n got %+v\nwant %+v", tt.d, got, tt.want)
			}
		})
	}
}

func TestParsePathArcs(t *testing.T) {
	t.Run("quarter circle", func(t *testing.T) {
		got, _ := ParsePath("M5 19 A1 1 0 0 0 6 20")
		want := []Seg{mv(5, 19), cu(pt(5, 19+kappa), pt(6-kappa, 20), pt(6, 20))}
		if !segsEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
	t.Run("sweep flag mirrors the arc", func(t *testing.T) {
		got, _ := ParsePath("M5 19 A1 1 0 0 1 6 20")
		want := []Seg{mv(5, 19), cu(pt(5+kappa, 19), pt(6, 20-kappa), pt(6, 20))}
		if !segsEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
	t.Run("large arc flag takes the long way", func(t *testing.T) {
		got, _ := ParsePath("M5 19 A1 1 0 1 0 6 20")
		if len(got) != 4 {
			t.Fatalf("%d segments, want a move and 3 cubics (270°)", len(got))
		}
	})
	t.Run("zero radius is a line", func(t *testing.T) {
		for _, d := range []string{"M0 0 A0 5 0 0 1 4 4", "M0 0 A5 0 0 0 1 4 4", "M0 0 a0 0 0 1 1 4 4"} {
			got, _ := ParsePath(d)
			if !segsEqual(got, []Seg{mv(0, 0), ln(4, 4)}) {
				t.Errorf("%s: %+v", d, got)
			}
		}
	})
	t.Run("same endpoints draw nothing", func(t *testing.T) {
		got, _ := ParsePath("M3 3 A5 5 0 0 1 3 3 L4 4")
		if !segsEqual(got, []Seg{mv(3, 3), ln(4, 4)}) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("radii too small are scaled up to a half circle", func(t *testing.T) {
		got, _ := ParsePath("M0 0 A1 1 0 0 1 10 0")
		if len(got) != 3 {
			t.Fatalf("%d segments, want 2 cubics (180°)", len(got))
		}
		mid := got[1].P[2]
		if !near(mid.X, 5) || math.Abs(math.Abs(mid.Y)-5) > 1e-9 {
			t.Errorf("half circle apex %+v, want (5, ±5)", mid)
		}
		if got[2].P[2] != pt(10, 0) {
			t.Errorf("ends at %+v", got[2].P[2])
		}
	})
	t.Run("huge radii stay finite and end at the endpoint", func(t *testing.T) {
		got, _ := ParsePath("M0 0 A1e30 1e30 0 1 1 10 10")
		assertSane(t, got)
		if last := got[len(got)-1]; last.P[2] != pt(10, 10) && last.P[0] != pt(10, 10) {
			t.Errorf("last %+v", last)
		}
	})
	t.Run("tiny radii", func(t *testing.T) {
		got, _ := ParsePath("M0 0 A1e-300 1e-300 0 0 1 10 10")
		assertSane(t, got)
		if len(got) < 2 {
			t.Errorf("%+v", got)
		}
	})
	t.Run("rotation", func(t *testing.T) {
		got, _ := ParsePath("M0 0 A10 5 45 0 1 8 8")
		assertSane(t, got)
		if len(got) < 2 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("never more than 4 cubics", func(t *testing.T) {
		for _, d := range []string{"M0 0 A5 5 0 1 1 0.0001 0", "M0 0 A5 5 30 1 0 0.0001 0.0001", "M0 0 A100 1 90 1 1 1 1"} {
			got, _ := ParsePath(d)
			if len(got) > 5 {
				t.Errorf("%s: %d segments", d, len(got))
			}
		}
	})
	t.Run("flags need no separator", func(t *testing.T) {
		a, _ := ParsePath("M0 0 a1 1 0 0110 10")
		b, _ := ParsePath("M0 0 a1 1 0 0 1 10 10")
		if len(a) == 0 || !segsEqual(a, b) {
			t.Errorf("%+v vs %+v", a, b)
		}
	})
	t.Run("a bad flag ends the path", func(t *testing.T) {
		got, _ := ParsePath("M0 0 L1 1 A1 1 0 2 1 5 5")
		if !segsEqual(got, []Seg{mv(0, 0), ln(1, 1)}) {
			t.Errorf("%+v", got)
		}
	})
	t.Run("implicit arc repeat", func(t *testing.T) {
		got, _ := ParsePath("M0 0 A1 1 0 0 1 2 0 1 1 0 0 1 4 0")
		if len(got) != 1+2+2 {
			t.Errorf("%d segments", len(got))
		}
	})
}

func assertSane(t *testing.T, segs []Seg) {
	t.Helper()
	for i, s := range segs {
		for _, p := range s.P {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.Abs(p.X) > MaxCoord || math.Abs(p.Y) > MaxCoord {
				t.Fatalf("segment %d has %+v", i, p)
			}
		}
	}
}

func TestParsePathLimit(t *testing.T) {
	pad := func(n int) string { return "M0 0 " + strings.Repeat("L1 1 ", (n-5)/5) }
	at := pad(MaxPathBytes)
	for len(at) < MaxPathBytes {
		at += " "
	}
	if len(at) != MaxPathBytes {
		t.Fatalf("setup: %d bytes", len(at))
	}
	if segs, ok := ParsePath(at); !ok || len(segs) == 0 {
		t.Errorf("a path of %d bytes: ok=%v, %d segments", len(at), ok, len(segs))
	}
	if segs, ok := ParsePath(at + " "); ok || segs != nil {
		t.Errorf("a path of %d bytes is kept: ok=%v", len(at)+1, ok)
	}
}

func TestParsePathSegmentsStayBounded(t *testing.T) {
	d := "M0 0" + strings.Repeat("Z", MaxPathBytes-4)
	segs, _ := ParsePath(d)
	if len(segs) != 2 {
		t.Errorf("%d segments for repeated closepaths", len(segs))
	}
	d = "M0 0" + strings.Repeat("A1 1 0 1 1 1 1", (MaxPathBytes-4)/14)
	segs, _ = ParsePath(d)
	if len(segs) > 5*MaxPathBytes {
		t.Errorf("%d segments", len(segs))
	}
}

func TestParsePathAllocations(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("M0 0")
	for i := 0; sb.Len() < MaxPathBytes-12; i++ {
		sb.WriteString("l" + strconv.Itoa(i%9) + " 1")
	}
	d := sb.String()
	want, _ := ParsePath(d)
	if len(want) < 10000 {
		t.Fatalf("only %d segments", len(want))
	}
	var before, after runtimeMem
	before.read()
	got, _ := ParsePath(d)
	after.read()
	if len(got) != len(want) {
		t.Fatal("not deterministic")
	}
	if used, limit := after.total-before.total, uint64(8*len(got)*56); used > limit {
		t.Errorf("%d bytes allocated for %d segments, limit %d", used, len(got), limit)
	}
}
