package pdf

import (
	"bytes"
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
