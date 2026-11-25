package pdf

import (
	"bytes"
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func TestPathOperators(t *testing.T) {
	c := &Content{}
	c.MoveTo(1, 2)
	c.LineTo(3, 4)
	c.LineTo(5, 6)
	c.ClosePath()
	c.Fill()
	c.Rect(0, 0, 10, 10)
	c.Rect(2, 2, 6, 6)
	c.FillEvenOdd()
	want := "1 2 m\n3 4 l\n5 6 l\nh\nf\n0 0 10 10 re\n2 2 6 6 re\nf*\n"
	if got := string(c.Bytes()); got != want || c.Err() != nil {
		t.Fatalf("got %q err=%v\nwant %q", got, c.Err(), want)
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

func TestDashOperators(t *testing.T) {
	c := &Content{}
	c.Dash(4.5, 2.875)
	c.Dash(0, 5.8125)
	want := "[4.5 2.875 ] 0 d\n[0 5.8125 ] 0 d\n"
	if got := string(c.Bytes()); got != want || c.Err() != nil {
		t.Errorf("got %q (%v), want %q", got, c.Err(), want)
	}
}

func TestDashRejectsInvalidPatterns(t *testing.T) {
	for name, f := range map[string]func(*Content){
		"negative on":      func(c *Content) { c.Dash(-1, 2) },
		"negative off":     func(c *Content) { c.Dash(1, -2) },
		"all zero":         func(c *Content) { c.Dash(0, 0) },
		"written as zeros": func(c *Content) { c.Dash(4e-5, 1e-5) },
		"NaN":              func(c *Content) { c.Dash(math.NaN(), 1) },
		"infinite":         func(c *Content) { c.Dash(1, math.Inf(1)) },
		"NaN off":          func(c *Content) { c.Dash(1, math.NaN()) },
	} {
		c := &Content{}
		f(c)
		if c.Err() == nil {
			t.Errorf("%s: accepted: %q", name, c.Bytes())
		}
	}
	c := &Content{}
	c.Dash(0, 1e-4)
	if c.Err() != nil {
		t.Errorf("a dot pattern written as [0 0.0001]: %v", c.Err())
	}
}

func TestDashedPageValidates(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	c := &Content{}
	c.Save()
	c.LineWidth(2)
	c.Dash(0, 5)
	c.LineCap(1)
	c.MoveTo(10, 10)
	c.LineTo(200, 10)
	c.Stroke()
	c.Restore()
	if _, err := w.AddPage(300, 300, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	pdftest.Validate(t, buf.Bytes())
}
