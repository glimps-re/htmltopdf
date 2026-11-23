package pdf

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func twoStops() Gradient {
	return Gradient{Stops: []Stop{{0, Color{R: 1, A: 1}}, {1, Color{B: 1, A: 1}}}}
}

func TestGradientDedupOneShadingObject(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	for range 1000 {
		id, err := w.AddGradient(twoStops())
		if err != nil {
			t.Fatal(err)
		}
		c := &Content{}
		c.Save()
		c.Rect(0, 0, 50, 50)
		c.Clip()
		c.Concat(50, 0, 0, 1, 0, 0)
		c.Shade(id)
		c.Restore()
		if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(buf.Bytes(), []byte("/ShadingType 2")); n != 1 {
		t.Fatalf("%d shading objects", n)
	}
	pdftest.Validate(t, buf.Bytes())
}

func TestGradientStopsForms(t *testing.T) {
	red, blue := Color{R: 1, A: 1}, Color{B: 1, A: 1}
	for name, stops := range map[string][]Stop{
		"inner":      {{0.2, red}, {0.8, blue}},
		"hard":       {{0, red}, {0.5, red}, {0.5, blue}, {1, blue}},
		"three":      {{0, red}, {0.5, Color{G: 1, A: 1}}, {1, blue}},
		"outOfRange": {{-3, red}, {7, blue}},
		"coincident": {{0.3, red}, {0.3, blue}, {0.3, red}},
		"hardStart":  {{0, red}, {0, blue}, {1, red}},
		"hardEnd":    {{0, red}, {1, blue}, {1, red}},
		"nearEdge":   {{0.00001, red}, {0.99999, blue}},
		"tooClose":   {{0.5, red}, {0.50001, blue}},
		"allEqual":   equalStops(0.5),
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			w := newTestWriter(&buf)
			id, err := w.AddGradient(Gradient{Stops: stops})
			if err != nil {
				t.Fatal(err)
			}
			c := &Content{}
			c.Rect(0, 0, 10, 10)
			c.Clip()
			c.Shade(id)
			if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(Info{}); err != nil {
				t.Fatal(err)
			}
			pdftest.Validate(t, buf.Bytes())
		})
	}
}

func TestGradientErrors(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	if _, err := w.AddGradient(Gradient{Stops: []Stop{{0, Color{}}}}); !errors.Is(err, ErrGradient) {
		t.Fatalf("one stop: %v", err)
	}
	if _, err := w.AddGradient(Gradient{Stops: make([]Stop, MaxGradientStops+1)}); !errors.Is(err, ErrGradient) {
		t.Fatalf("too many: %v", err)
	}
	if _, err := w.AddGradient(Gradient{Stops: make([]Stop, MaxGradientStops)}); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	nan := math.NaN()
	if _, err := w.AddGradient(Gradient{Stops: []Stop{{nan, Color{}}, {1, Color{}}}}); !errors.Is(err, ErrGradient) {
		t.Fatalf("nan: %v", err)
	}
	c := &Content{}
	c.Shade(ShadingID{})
	if !errors.Is(c.Err(), ErrShadingID) {
		t.Fatalf("zero id: %v", c.Err())
	}
	other := newTestWriter(&bytes.Buffer{})
	id, err := other.AddGradient(twoStops())
	if err != nil {
		t.Fatal(err)
	}
	c = &Content{}
	c.Shade(id)
	if _, err := w.AddPage(10, 10, c, false, nil); !errors.Is(err, ErrForeignShading) {
		t.Fatalf("foreign id: %v", err)
	}
}

func TestGradientShadingLimit(t *testing.T) {
	w := newTestWriter(&bytes.Buffer{})
	for i := range MaxShadings {
		g := Gradient{Stops: []Stop{{0, Color{R: float64(i%64) / 64, G: float64(i/64) / 64, A: 1}}, {1, Color{A: 1}}}}
		if _, err := w.AddGradient(g); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	over := Gradient{Stops: []Stop{{0, Color{R: 0.123, A: 1}}, {1, Color{A: 1}}}}
	if _, err := w.AddGradient(over); !errors.Is(err, ErrTooManyGradients) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := w.AddGradient(Gradient{Stops: []Stop{{0, Color{A: 1}}, {1, Color{A: 1}}}}); err != nil {
		t.Fatalf("existing gradient refused: %v", err)
	}
}

func TestEllipticRect(t *testing.T) {
	c := &Content{}
	c.EllipticRect(0, 0, 100, 50, [4]float64{10, 200, 0, -1}, [4]float64{5, 200, 0, 3})
	c.ClipEvenOdd()
	c.EllipticRect(0, 0, 0, 5, [4]float64{1}, [4]float64{1})
	if c.Err() != nil {
		t.Fatal(c.Err())
	}
	s := string(c.Bytes())
	if !strings.Contains(s, "h\nW* n\n") || !strings.Contains(s, " c\n") {
		t.Fatalf("%q", s)
	}
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	pdftest.Validate(t, buf.Bytes())
}

func equalStops(pos float64) []Stop {
	out := make([]Stop, MaxGradientStops)
	for i := range out {
		out[i] = Stop{Pos: pos, Color: Color{R: float64(i%2) / 2, A: 1}}
	}
	return out
}

func TestQuantizeKeepsBoundsStrictlyInside(t *testing.T) {
	for name, stops := range map[string][]Stop{
		"all at 0":   equalStops(0),
		"all at 1":   equalStops(1),
		"all at .5":  equalStops(0.5),
		"descending": {{0.9, Color{}}, {0.1, Color{}}, {0.5, Color{}}},
	} {
		var q [MaxGradientStops]qstop
		n, err := quantize(stops, &q)
		if err != nil {
			t.Fatal(err)
		}
		prev := -1
		for i, s := range q[:n] {
			if int(s.pos) <= prev && i > 0 || s.pos > ticks {
				t.Fatalf("%s: stop %d at %d after %d", name, i, s.pos, prev)
			}
			if i > 0 && i < n-1 && (s.pos == 0 || s.pos >= ticks) {
				t.Fatalf("%s: interior stop %d at %d", name, i, s.pos)
			}
			prev = int(s.pos)
		}
	}
}

func TestGradientsDifferingBelowPrintResolutionShareAShading(t *testing.T) {
	w := newTestWriter(&bytes.Buffer{})
	a, err := w.AddGradient(Gradient{Stops: []Stop{{0, Color{R: 0.50001, A: 1}}, {1, Color{A: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.AddGradient(Gradient{Stops: []Stop{{0, Color{R: 0.5, A: 1}}, {1, Color{A: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("gradients that print the same got two shadings")
	}
}

func BenchmarkAddGradient(b *testing.B) {
	w := newTestWriter(&bytes.Buffer{})
	g := twoStops()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := w.AddGradient(g); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzAddGradient(f *testing.F) {
	f.Add(0.0, 1.0, 0.5, 0.2)
	f.Add(math.NaN(), math.Inf(1), -1.0, 1e300)
	f.Fuzz(func(t *testing.T, a, b, c, d float64) {
		var buf bytes.Buffer
		w := newTestWriter(&buf)
		id, err := w.AddGradient(Gradient{Stops: []Stop{{a, Color{R: c, A: 1}}, {b, Color{G: d, A: 1}}, {a, Color{B: c, A: 1}}}})
		if err != nil {
			return
		}
		ct := &Content{}
		ct.Shade(id)
		if _, err := w.AddPage(10, 10, ct, false, nil); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(Info{}); err != nil {
			t.Fatal(err)
		}
		pdftest.Validate(t, buf.Bytes())
	})
}
