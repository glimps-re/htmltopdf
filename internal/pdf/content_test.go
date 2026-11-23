package pdf

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestContentOperators(t *testing.T) {
	c := &Content{}
	c.Save()
	c.Concat(1, 0, 0, -1, 0, 842)
	c.FillColor(Color{R: 1, G: 0.5, B: 0, A: 1})
	c.StrokeColor(Color{R: -3, G: 2, B: 0, A: 1})
	c.LineWidth(0.5)
	c.Rect(1, 2, 3, 4)
	c.Fill()
	c.Clip()
	c.Stroke()
	c.Restore()
	want := "q\n1 0 0 -1 0 842 cm\n1 0.5 0 rg\n0 1 0 RG\n0.5 w\n1 2 3 4 re\nf\nW n\nS\nQ\n"
	if got := string(c.Bytes()); got != want || c.Err() != nil || c.Depth() != 0 {
		t.Fatalf("got %q err=%v depth=%d", got, c.Err(), c.Depth())
	}
}

func TestSaveDepth(t *testing.T) {
	for _, tt := range []struct {
		name  string
		depth int
		err   error
	}{
		{"just under", MaxSaveDepth - 1, nil},
		{"at the limit", MaxSaveDepth, nil},
		{"over", MaxSaveDepth + 1, ErrSaveDepth},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Content{}
			for i := 0; i < tt.depth; i++ {
				c.Save()
			}
			if !errors.Is(c.Err(), tt.err) {
				t.Fatalf("err = %v", c.Err())
			}
			if c.Depth() > MaxSaveDepth {
				t.Fatalf("depth %d", c.Depth())
			}
		})
	}
}

func TestRestoreWithoutSave(t *testing.T) {
	c := &Content{}
	c.Restore()
	if !errors.Is(c.Err(), ErrUnbalanced) || len(c.Bytes()) != 0 {
		t.Fatalf("err=%v bytes=%q", c.Err(), c.Bytes())
	}
}

func TestStickyError(t *testing.T) {
	c := &Content{}
	c.Rect(math.NaN(), 0, 1, 1)
	c.Restore()
	if !errors.Is(c.Err(), ErrNumber) {
		t.Fatalf("first error lost: %v", c.Err())
	}
	for name, f := range map[string]func(*Content){
		"fill":   func(c *Content) { c.FillColor(Color{R: math.NaN()}) },
		"alpha":  func(c *Content) { c.StrokeColor(Color{A: math.NaN()}) },
		"width":  func(c *Content) { c.LineWidth(-1) },
		"inf":    func(c *Content) { c.Concat(0, 0, 0, 0, math.Inf(1), 0) },
		"rrect":  func(c *Content) { c.RoundedRect(0, math.Inf(1), 10, 10, [4]float64{}) },
		"rrect2": func(c *Content) { c.RoundedRect(math.NaN(), 0, 10, 10, [4]float64{}) },
	} {
		c := &Content{}
		f(c)
		if c.Err() == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestOpacityStack(t *testing.T) {
	c := &Content{}
	c.FillColor(Color{A: 1})
	c.Save()
	c.FillColor(Color{A: 0.5})
	c.StrokeColor(Color{A: 0.5})
	c.Restore()
	c.FillColor(Color{A: 1})
	c.Save()
	c.FillColor(Color{A: 0.5})
	c.Restore()
	c.FillColor(Color{A: 2})
	c.FillColor(Color{A: -1})
	got := string(c.Bytes())
	if n := strings.Count(got, " gs"); n != 3 {
		t.Fatalf("gs count = %d in %q", n, got)
	}
	if !strings.Contains(got, "/GT127 gs") || !strings.HasSuffix(got, "/GT255 gs\n0 0 0 rg\n") {
		t.Fatalf("content = %q", got)
	}
	if c.opacities[1]>>63 == 0 || c.opacities[3]>>63 == 0 {
		t.Fatalf("opacities = %v", c.opacities)
	}
}

func TestRoundedRect(t *testing.T) {
	tests := []struct {
		name  string
		w, h  float64
		radii [4]float64
		want  string
	}{
		{"zero size falls back", 0, 10, [4]float64{1, 1, 1, 1}, "0 0 0 10 re\n"},
		{"negative size falls back", 10, -5, [4]float64{1, 1, 1, 1}, "0 0 10 -5 re\n"},
		{"clamped to half side", 10, 20, [4]float64{99, 0, 0, 0}, "5 0 m\n"},
		{"negative and nan radius", 10, 20, [4]float64{-4, math.NaN(), 0, 0}, "0 0 m\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Content{}
			c.RoundedRect(0, 0, tt.w, tt.h, tt.radii)
			if c.Err() != nil || !strings.HasPrefix(string(c.Bytes()), tt.want) {
				t.Fatalf("err=%v got %q", c.Err(), c.Bytes())
			}
		})
	}
	c := &Content{}
	c.RoundedRect(0, 0, 10, 10, [4]float64{1, 2, 3, 4})
	if strings.Count(string(c.Bytes()), " c\n") != 4 || !strings.HasSuffix(string(c.Bytes()), "h\n") {
		t.Fatalf("path = %q", c.Bytes())
	}
}

func contentOfSize(n int) *Content {
	return &Content{buf: bytes.Repeat([]byte("f\n"), n/2)}
}

func TestContentSizeCap(t *testing.T) {
	atCap := contentOfSize(MaxContentBytes)
	atCap.Fill()
	if !errors.Is(atCap.Err(), ErrContentSize) || len(atCap.Bytes()) != MaxContentBytes {
		t.Fatalf("over: err=%v len=%d", atCap.Err(), len(atCap.Bytes()))
	}
	atCap.Rect(1, 2, 3, 4)
	atCap.Save()
	atCap.FillColor(Color{R: 1, A: 0.5})
	if len(atCap.Bytes()) != MaxContentBytes || atCap.Depth() != 0 {
		t.Fatalf("content kept growing after the error: len=%d depth=%d", len(atCap.Bytes()), atCap.Depth())
	}
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	if _, err := w.AddPage(10, 10, atCap, false, nil); !errors.Is(err, ErrContentSize) {
		t.Fatalf("AddPage accepted it: %v", err)
	}
	under := contentOfSize(MaxContentBytes - 2)
	under.Fill()
	if under.Err() != nil || len(under.Bytes()) != MaxContentBytes {
		t.Fatalf("just under, reaching the cap: err=%v len=%d", under.Err(), len(under.Bytes()))
	}
}

func TestContentSizeCapOperand(t *testing.T) {
	const operand = "1234567 "
	fits := &Content{buf: make([]byte, MaxContentBytes-len(operand))}
	fits.num(1234567)
	if fits.Err() != nil || len(fits.Bytes()) != MaxContentBytes {
		t.Fatalf("at the cap: err=%v len=%d", fits.Err(), len(fits.Bytes()))
	}
	over := &Content{buf: make([]byte, MaxContentBytes-len(operand)+1)}
	over.num(1234567)
	if !errors.Is(over.Err(), ErrContentSize) || len(over.Bytes()) != MaxContentBytes-len(operand)+1 {
		t.Fatalf("over: err=%v len=%d", over.Err(), len(over.Bytes()))
	}
	gs := &Content{buf: make([]byte, MaxContentBytes-2)}
	gs.FillColor(Color{A: 0.5})
	if !errors.Is(gs.Err(), ErrContentSize) || gs.transparency != 0 || gs.opacities.has(127) {
		t.Fatalf("gs over the cap: err=%v", gs.Err())
	}
}

func TestOpacitySet(t *testing.T) {
	var a, b opacitySet
	for _, v := range []uint8{0, 63, 64, 127, 128, 255} {
		if a.has(v) {
			t.Errorf("%d in empty set", v)
		}
		a.add(v)
		if !a.has(v) {
			t.Errorf("%d missing", v)
		}
	}
	b.add(7)
	b.merge(a)
	if !b.has(7) || !b.has(255) || !b.has(64) || b.has(8) || a.has(7) {
		t.Fatalf("merge: %v %v", a, b)
	}
}
