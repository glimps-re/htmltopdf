package pdf

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

func shadePage(t testing.TB, g Gradient) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id, err := w.AddGradient(g)
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{}
	c.Save()
	c.Rect(0, 0, 50, 50)
	c.Clip()
	c.Concat(25, 0, 0, 25, 25, 25)
	c.Shade(id)
	c.Restore()
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRadialShading(t *testing.T) {
	data := shadePage(t, Gradient{Radial: true, Stops: []Stop{{0, Color{R: 1, A: 1}}, {1, Color{B: 1, A: 1}}}})
	for _, want := range []string{"/ShadingType 3", "/Coords [0 0 0 0 0 1]", "/Extend [true true]"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("missing %q", want)
		}
	}
	if bytes.Contains(data, []byte("SMask")) {
		t.Error("an opaque gradient has a soft mask")
	}
	pdftest.Validate(t, data)
	if err := pdftest.ValidatePDFCPU(data); err != nil {
		t.Fatal(err)
	}
}

func TestTranslucentGradientSoftMask(t *testing.T) {
	for _, radial := range []bool{false, true} {
		g := Gradient{Radial: radial, Stops: []Stop{{0, Color{R: 1, A: 0.5}}, {0.7, Color{R: 1, A: 0}}, {1, Color{R: 1, A: 0}}}}
		data := shadePage(t, g)
		doc := pdftest.Validate(t, data)
		if err := pdftest.ValidatePDFCPU(data); err != nil {
			t.Fatalf("radial=%v: %v", radial, err)
		}
		for _, want := range []string{
			"/SMask << /Type /Mask /S /Luminosity /G ", "/Subtype /Form", "/Group << /S /Transparency /CS /DeviceGray >>",
			"/ColorSpace /DeviceGray", "/ColorSpace /DeviceRGB",
		} {
			if !bytes.Contains(data, []byte(want)) {
				t.Errorf("radial=%v: missing %q", radial, want)
			}
		}
		if n := bytes.Count(data, []byte("/ShadingType")); n != 2 {
			t.Errorf("radial=%v: %d shading objects, want 2", radial, n)
		}
		content := string(doc.Pages[0].Content)
		if !strings.Contains(content, "/SM1 gs\n/Sh1 sh\n") {
			t.Errorf("radial=%v: content:\n%s", radial, content)
		}
	}
}

func TestGradientKeyDistinguishesShapeAndAlpha(t *testing.T) {
	w := newTestWriter(&bytes.Buffer{})
	add := func(radial bool, a float64) ShadingID {
		id, err := w.AddGradient(Gradient{Radial: radial, Stops: []Stop{{0, Color{R: 1, A: a}}, {1, Color{B: 1, A: 1}}}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	base := add(false, 1)
	for name, id := range map[string]ShadingID{"radial": add(true, 1), "alpha .5": add(false, 0.5), "alpha .25": add(false, 0.25)} {
		if id == base {
			t.Errorf("%s shares the shading of the opaque linear gradient", name)
		}
	}
	half := add(false, 0.5)
	if add(false, 0.5) != half || add(false, 1) != base || add(false, 1.7) != base {
		t.Error("equal gradients got different shadings")
	}
	if add(false, 0.50001) != half {
		t.Error("alphas that print the same got two shadings")
	}
}

func translucent(i int) Gradient {
	return Gradient{Stops: []Stop{{0, Color{R: float64(i) / ticks, A: 0.5}}, {1, Color{A: 1}}}}
}

func TestTranslucentGradientsCostTwoShadings(t *testing.T) {
	w := newTestWriter(&bytes.Buffer{})
	for i := range MaxShadings / 2 {
		if _, err := w.AddGradient(translucent(i)); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := w.AddGradient(translucent(MaxShadings / 2)); !errors.Is(err, ErrTooManyGradients) {
		t.Fatalf("2049th translucent gradient: %v", err)
	}
	if _, err := w.AddGradient(translucent(0)); err != nil {
		t.Fatalf("existing gradient refused: %v", err)
	}
	if _, err := w.AddGradient(Gradient{Stops: []Stop{{0, Color{A: 1}}, {1, Color{R: 1, A: 1}}}}); !errors.Is(err, ErrTooManyGradients) {
		t.Fatalf("opaque gradient over the limit: %v", err)
	}
}

func TestShadingLimitMixesBothKinds(t *testing.T) {
	w := newTestWriter(&bytes.Buffer{})
	for i := range MaxShadings - 2 {
		g := Gradient{Stops: []Stop{{0, Color{R: float64(i) / ticks, A: 1}}, {1, Color{A: 1}}}}
		if _, err := w.AddGradient(g); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := w.AddGradient(translucent(9000)); err != nil {
		t.Fatalf("translucent at the limit: %v", err)
	}
	if _, err := w.AddGradient(translucent(9001)); !errors.Is(err, ErrTooManyGradients) {
		t.Fatalf("translucent over the limit: %v", err)
	}
}

func TestManyTranslucentGradientsValidate(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	c := &Content{}
	for i := range 40 {
		id, err := w.AddGradient(translucent(i))
		if err != nil {
			t.Fatal(err)
		}
		c.Save()
		c.Rect(0, 0, 50, 50)
		c.Clip()
		c.Concat(25, 0, 0, 25, 25, 25)
		c.Shade(id)
		c.Restore()
	}
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	pdftest.Validate(t, buf.Bytes())
	if err := pdftest.ValidatePDFCPU(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestCurveJoinCapOperators(t *testing.T) {
	c := &Content{}
	c.LineWidth(2)
	c.LineJoin(1)
	c.LineCap(2)
	c.MoveTo(1, 2)
	c.CurveTo(3, 4, 5, 6, 7, 8)
	c.ClosePath()
	c.FillStrokeEvenOdd()
	c.FillStroke()
	if got, want := string(c.Bytes()), "2 w\n1 j\n2 J\n1 2 m\n3 4 5 6 7 8 c\nh\nB*\nB\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if c.Err() != nil {
		t.Fatal(c.Err())
	}
	for _, f := range []func(*Content){func(c *Content) { c.LineJoin(3) }, func(c *Content) { c.LineCap(-1) }, func(c *Content) { c.CurveTo(math.NaN(), 0, 0, 0, 0, 0) }} {
		c := &Content{}
		f(c)
		if c.Err() == nil {
			t.Error("invalid operand accepted")
		}
	}
}

func TestPathPageValidates(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	c := &Content{}
	c.Save()
	c.Concat(0.7, 0.7, -0.7, 0.7, 10, 10)
	c.FillColor(Color{R: 1, A: 0.5})
	c.StrokeColor(Color{B: 1, A: 0.5})
	c.LineWidth(3)
	c.LineJoin(1)
	c.LineCap(1)
	c.MoveTo(10, 10)
	c.CurveTo(20, 10, 30, 20, 30, 30)
	c.LineTo(10, 30)
	c.ClosePath()
	c.FillStroke()
	c.Restore()
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	pdftest.Validate(t, buf.Bytes())
	if err := pdftest.ValidatePDFCPU(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestSoftMaskDoesNotLeakTransparency(t *testing.T) {
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id, err := w.AddGradient(translucent(1))
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{}
	c.FillColor(Color{G: 1, A: 0.3})
	c.Save()
	c.Concat(10, 0, 0, 10, 0, 0)
	c.Shade(id)
	c.Restore()
	c.Rect(0, 0, 5, 5)
	c.Fill()
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	pdftest.Validate(t, buf.Bytes())
}
