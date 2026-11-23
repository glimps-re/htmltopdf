package paint

import (
	"bytes"
	"context"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/text"
)

// a4 is a page of 800 x 1000 px (600 x 750 pt) whose area is the whole page.
var a4 = Geometry{Width: 800, Height: 1000, Area: display.Rect{W: 800, H: 1000}, Scale: 1}

var (
	black = css.Color{A: 1}
	red   = css.Color{R: 1, A: 1}
	blue  = css.Color{B: 1, A: 1}
)

func testBudget() *budget.Budget { return budget.New(context.Background(), budget.Defaults(), nil) }

func readFont(tb testing.TB, name string) []byte {
	tb.Helper()
	root, err := os.OpenRoot(filepath.Join("..", "..", "testdata", "fonts"))
	if err != nil {
		tb.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			tb.Error(err)
		}
	}()
	data, err := fs.ReadFile(root.FS(), name)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// env is a writer, a shaper over the reference fonts and a painter.
type env struct {
	buf     bytes.Buffer
	w       *pdf.Writer
	shaper  *text.Shaper
	painter *Painter
	b       *budget.Budget
}

func newEnv(tb testing.TB) *env {
	tb.Helper()
	e := &env{b: testBudget()}
	e.w = pdf.NewWriter(&e.buf, e.b)
	fs, err := text.NewFontSet([]text.Source{
		{Data: readFont(tb, "LiberationSans-Regular.ttf")},
		{Data: readFont(tb, "LiberationSans-Bold.ttf")},
		{Data: readFont(tb, "DejaVuSans.ttf")},
	}, text.Generic{SansSerif: "Liberation Sans"}, []string{"DejaVu Sans"})
	if err != nil {
		tb.Fatal(err)
	}
	e.shaper = text.NewShaper(fs, e.b)
	e.painter = NewPainter(e.w, e.shaper, e.b)
	return e
}

// page paints l on one page of geometry g and closes the document. It returns
// the content stream and the links.
func (e *env) page(tb testing.TB, g Geometry, l display.List) (content string, links []pdf.Link) {
	tb.Helper()
	c := &pdf.Content{}
	links, err := e.painter.Page(c, g, l)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := e.w.AddPage(g.Width*PxToPt, g.Height*PxToPt, c, false, links); err != nil {
		tb.Fatal(err)
	}
	return string(c.Bytes()), links
}

// finish closes the writer and validates the PDF (own reader and pdfcpu).
func (e *env) finish(tb testing.TB) *pdftest.Doc {
	tb.Helper()
	if err := e.w.Close(pdf.Info{}); err != nil {
		tb.Fatal(err)
	}
	return pdftest.Validate(tb, e.buf.Bytes())
}

// runs shapes txt and returns one display.Text per face run, as layout does:
// the Text of a run is its part of the text, and its glyphs keep their byte
// offsets in that part.
func (e *env) runs(tb testing.TB, txt string, size float64) []display.Text {
	tb.Helper()
	shaped, err := e.shaper.Shape(txt, text.FontSpec{Families: []string{"sans-serif"}, Weight: 400, Size: float32(size)}, nil)
	if err != nil {
		tb.Fatal(err)
	}
	var out []display.Text
	for i, r := range shaped {
		lo, hi := int(r.Glyphs[0].Cluster), len(txt)
		if i+1 < len(shaped) {
			hi = int(shaped[i+1].Glyphs[0].Cluster)
		}
		glyphs := append([]text.Glyph(nil), r.Glyphs...)
		for j := range glyphs {
			glyphs[j].Cluster -= int32(min(max(lo, 0), math.MaxInt32))
		}
		out = append(out, display.Text{Size: size, Color: black, Text: txt[lo:hi], Face: r.Face, Glyphs: glyphs})
	}
	return out
}

// run is runs for a text that one face covers.
func (e *env) run(tb testing.TB, txt string, size float64) display.Text {
	tb.Helper()
	runs := e.runs(tb, txt, size)
	if len(runs) != 1 {
		tb.Fatalf("text %q needs %d faces", txt, len(runs))
	}
	return runs[0]
}

var balancedRe = regexp.MustCompile(`(?m)^(q|Q)$`)

// balanced asserts that the content has as many Q as q and never more Q than q.
func balanced(tb testing.TB, content string) (saves int) {
	tb.Helper()
	depth := 0
	for _, m := range balancedRe.FindAllString(content, -1) {
		if m == "q" {
			depth++
			saves++
		} else if depth--; depth < 0 {
			tb.Fatal("Q without q")
		}
	}
	if depth != 0 {
		tb.Fatalf("%d q left open", depth)
	}
	return saves
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }
