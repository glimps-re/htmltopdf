package paint

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

type prng uint64

func (r *prng) n(m int) int {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return int(min((uint64(*r)>>11)%uint64(max(m, 1)), 1<<31))
}

// c is a color component: out of range but never NaN (NaN colors fail loudly, see TestHostileColors).
func (r *prng) c() float64 {
	vals := []float64{0, 0.5, 1, 1, 5, -5, 1e30, 3.4e38}
	return vals[r.n(len(vals))]
}

func (r *prng) f() float64 {
	vals := []float64{0, 1, -1, 0.5, 10, 100, 799.9, 1000, 1e6, -1e6, 1e30, -1e30, math.NaN(), math.Inf(1), math.Inf(-1), 1e-30, 3.4e38}
	if r.n(3) == 0 {
		return vals[r.n(len(vals))]
	}
	return float64(r.n(1200)) - 100
}

// A link target with ")" and "\" (and other PDF string delimiters) survives
// the writer: the extracted URI equals the source, and the file validates.
func TestLinkTargetWithDelimiters(t *testing.T) {
	for _, uri := range []string{
		`https://e.example/a)b\c(d`,
		`https://e.example/))))\\\\((((`,
		`https://e.example/%29%5C`,
		"https://e.example/a\x7f",
		`mailto:a@b.c?subject=(x)\y`,
		"https://e.example/" + strings.Repeat(`)\(`, 600),
		`https://e.example/ <>[]{}/%#`,
	} {
		t.Run(fmt.Sprintf("%.30s", uri), func(t *testing.T) {
			e := newEnv(t)
			c := &pdf.Content{}
			links, err := e.painter.Page(c, a4, display.List{display.Link{Rect: display.Rect{X: 10, Y: 10, W: 50, H: 10}, URI: uri}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.w.AddPage(600, 750, c, false, links); err != nil {
				if strings.Contains(uri, "\x7f") && errors.Is(err, pdf.ErrInvalidLink) {
					return // DEL is not printable: refused by the writer
				}
				t.Fatalf("AddPage: %v", err)
			}
			doc := e.finish(t)
			if len(doc.Pages[0].Links) != 1 || doc.Pages[0].Links[0].URI != uri {
				t.Fatalf("link in the PDF: %+v", doc.Pages[0].Links)
			}
		})
	}
}

// A link the writer refuses is an upstream bug (box.cleanHref only produces
// printable ASCII http, https and mailto targets of at most 2048 bytes): it
// fails loudly with pdf.ErrInvalidLink, never silently and never with a panic
// or an invalid file.
func TestInvalidLinkTargetsFailLoudly(t *testing.T) {
	for name, uri := range map[string]string{
		"control character": "https://e.example/a\x01b",
		"newline":           "https://e.example/a\nb",
		"non ascii":         "https://e.example/é",
		"javascript":        "javascript:alert(1)",
		"data":              "data:text/html,x",
		"no scheme":         "/relative",
		"huge":              "https://e.example/" + strings.Repeat("a", 100_000),
		"nul":               "https://e.example/\x00",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			c := &pdf.Content{}
			links, err := e.painter.Page(c, a4, display.List{display.Link{Rect: display.Rect{X: 10, Y: 10, W: 50, H: 10}, URI: uri}})
			if err == nil {
				_, err = e.w.AddPage(600, 750, c, false, links)
			}
			if !errors.Is(err, pdf.ErrInvalidLink) {
				t.Fatalf("err = %v, want pdf.ErrInvalidLink", err)
			}
		})
	}
}

// Random display lists with NaN, infinite and absurd values: every outcome is
// either one of the documented errors or a balanced, valid PDF.
func TestRandomDisplayLists(t *testing.T) {
	rounds := 1500
	if testing.Short() {
		rounds = 200
	}
	r := prng(0x9E3779B97F4A7C15)
	okPages, errs := 0, map[string]int{}
	for round := range rounds {
		e := newEnv(t)
		var l display.List
		depth := 0
		for range 1 + r.n(40) {
			rect := display.Rect{X: r.f(), Y: r.f(), W: r.f(), H: r.f()}
			radii := display.Radii{{H: r.f(), V: r.f()}, {H: r.f(), V: r.f()}, {H: r.f(), V: r.f()}, {H: r.f(), V: r.f()}}
			col := css.Color{R: r.c(), G: r.c(), B: r.c(), A: r.c()}
			switch r.n(9) {
			case 0, 1:
				l = append(l, display.FillRect{Rect: rect, Radii: radii, Color: col})
			case 2:
				side := display.BorderSide{Width: r.f(), Color: col}
				l = append(l, display.Border{Rect: rect, Radii: radii, Sides: [4]display.BorderSide{side, {Width: r.f(), Color: col}, side, {Width: r.f()}}})
			case 3:
				l = append(l, display.Link{Rect: rect, URI: "https://r.example/?(" + fmt.Sprint(round) + `)\`})
			case 4, 5:
				l = append(l, display.PushClip{Rect: rect, Radii: radii})
				depth++
			case 6:
				if depth > 0 {
					l = append(l, display.PopClip{})
					depth--
				}
			case 7:
				run := e.run(t, "Été x", 8+float64(r.n(60)))
				run.X, run.Baseline, run.Underline, run.LineThrough = r.f(), r.f(), r.n(2) == 0, r.n(2) == 0
				run.Color = red
				l = append(l, &run)
			case 8:
				l = append(l, display.FillGradient{Rect: rect})
			}
		}
		for ; depth > 0; depth-- {
			l = append(l, display.PopClip{})
		}
		g := Geometry{Width: 800, Height: 1000, Area: display.Rect{X: r.f() / 10, Y: r.f() / 10, W: 600, H: 800}, Scale: r.f()}
		c := &pdf.Content{}
		links, err := e.painter.Page(c, g, l)
		if err != nil {
			errs[err.Error()]++
			if !errors.Is(err, ErrUnbalancedClip) && !errors.Is(err, pdf.ErrContentSize) {
				t.Fatalf("round %d: unexpected error %v\nminimal list: %s", round, err, shrink(t, l, g))
			}
			continue
		}
		balanced(t, string(c.Bytes()))
		if bytes.ContainsAny(c.Bytes(), "\x00") || bytes.Contains(c.Bytes(), []byte("NaN")) || bytes.Contains(c.Bytes(), []byte("Inf")) || bytes.Contains(c.Bytes(), []byte("e+")) {
			t.Fatalf("round %d: bad number in the content:\n%.300s", round, c.Bytes())
		}
		if _, err := e.w.AddPage(g.Width*PxToPt, g.Height*PxToPt, c, false, links); err != nil {
			t.Fatalf("round %d: AddPage: %v", round, err)
		}
		if err := e.w.Close(pdf.Info{}); err != nil {
			t.Fatalf("round %d: Close: %v", round, err)
		}
		if round%10 == 0 {
			pdftest.Validate(t, e.buf.Bytes())
		} else if _, err := pdftest.Parse(e.buf.Bytes()); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		okPages++
	}
	t.Logf("%d valid pages, errors %v", okPages, errs)
	if okPages < rounds/2 {
		t.Fatalf("only %d of %d lists painted", okPages, rounds)
	}
}

// Clips at the deepest allowed nesting with every kind of item inside: the
// graphics state stack never overflows, q and Q stay balanced.
func TestEveryItemInsideTheDeepestClip(t *testing.T) {
	e := newEnv(t)
	r := display.Rect{X: 5, Y: 5, W: 500, H: 500}
	rad := display.Radii{{H: 20, V: 20}, {H: 20, V: 20}, {H: 20, V: 20}, {H: 20, V: 20}}
	var l display.List
	for range MaxClipDepth + 20 {
		l = append(l, display.PushClip{Rect: r, Radii: rad})
	}
	run := e.run(t, "inside", 14)
	run.X, run.Baseline, run.Underline, run.LineThrough = 20, 100, true, true
	l = append(l,
		display.FillRect{Rect: r, Radii: rad, Color: red},
		display.Border{Rect: r, Radii: rad, Sides: [4]display.BorderSide{{Width: 3, Color: blue}, {Width: 1, Color: red}, {Width: 3, Color: blue}, {Width: 1, Color: red}}},
		display.Border{Rect: r, Sides: [4]display.BorderSide{{Width: 3, Color: blue}, {Width: 3, Color: blue}, {Width: 3, Color: blue}, {Width: 3, Color: blue}}},
		&run,
		display.Link{Rect: r, URI: "https://deep.example/"},
	)
	for range MaxClipDepth + 20 {
		l = append(l, display.PopClip{})
	}
	c := &pdf.Content{}
	links, err := e.painter.Page(c, a4, l)
	if err != nil {
		t.Fatal(err)
	}
	if saves := balanced(t, string(c.Bytes())); saves < MaxClipDepth {
		t.Errorf("%d saves", saves)
	}
	if c.Depth() != 0 {
		t.Errorf("content depth %d after the page", c.Depth())
	}
	if _, err := e.w.AddPage(600, 750, c, false, links); err != nil {
		t.Fatal(err)
	}
	e.finish(t)
}

// After the content cap is hit the writer and the painter keep working: the
// failed page is refused, the next one is written and validates.
func TestWriterSurvivesTheContentCap(t *testing.T) {
	e := newEnv(t)
	run := e.run(t, "lorem ipsum dolor sit amet", 12)
	var big display.List
	for i := 0; i < pdf.MaxContentBytes/40+2000; i++ {
		r := run
		r.X, r.Baseline = float64(i%700), float64(i%900)
		big = append(big, &r)
	}
	c := &pdf.Content{}
	_, err := e.painter.Page(c, a4, big)
	if !errors.Is(err, pdf.ErrContentSize) {
		t.Fatalf("err = %v", err)
	}
	if len(c.Bytes()) > pdf.MaxContentBytes {
		t.Fatalf("content of %d bytes", len(c.Bytes()))
	}
	if c.Depth() != 0 {
		t.Errorf("content left %d clips open", c.Depth())
	}
	good := e.run(t, "next page", 12)
	good.X, good.Baseline = 10, 50
	e.page(t, a4, display.List{&good})
	doc := e.finish(t)
	if len(doc.Pages) != 1 || strings.Join(doc.Pages[0].Text, "") != "next page" {
		t.Fatalf("pages %d, text %q", len(doc.Pages), doc.Pages[0].Text)
	}
}

// The text of the whole pipeline, with the characters the design cares about,
// comes back from the PDF as typed.
func TestPipelineTextExtraction(t *testing.T) {
	const french = "Résumé de l’analyse : détecté « Trojan.Win32 » — ça coûte 12,50 € …"
	const marks = "↳ … — « » ⟨RLO⟩ invoice⟨RLO⟩txt.exe"
	doc, _ := render(t, `<!doctype html><p>`+french+`</p><p>`+marks+`</p><p>invoice`+"\u202e"+`txt.exe</p><p>a`+"\u200b"+`b</p>`)
	var all strings.Builder
	for _, p := range doc.Pages {
		all.WriteString(strings.Join(p.Text, ""))
	}
	for _, want := range []string{french, "↳ … — « » ⟨RLO⟩ invoice⟨RLO⟩txt.exe", "invoice⟨RLO⟩txt.exe", "ab"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("extracted text lacks %q:\n%s", want, all.String())
		}
	}
}

func TestPipelineLinkWithDelimiters(t *testing.T) {
	doc, _ := render(t, `<p><a href="https://e.example/a)b\c(d?x=1&amp;y=%29">a link</a></p>`)
	if len(doc.Pages[0].Links) != 1 || doc.Pages[0].Links[0].URI != `https://e.example/a)b\c(d?x=1&y=%29` {
		t.Fatalf("links %+v", doc.Pages[0].Links)
	}
}

// shrink removes items while the painter still returns the same error and
// formats what is left.
func shrink(t *testing.T, l display.List, g Geometry) string {
	t.Helper()
	fails := func(l display.List) error {
		e := newEnv(t)
		_, err := e.painter.Page(&pdf.Content{}, g, l)
		return err
	}
	want := fails(l)
	for i := 0; i < len(l); {
		try := append(append(display.List{}, l[:i]...), l[i+1:]...)
		if err := fails(try); err != nil && err.Error() == want.Error() {
			l = try
			continue
		}
		i++
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "geometry %+v\n", g)
	for _, it := range l {
		switch v := it.(type) {
		case *display.Text:
			fmt.Fprintf(&sb, "  text X=%v Baseline=%v Size=%v ul=%v lt=%v\n", v.X, v.Baseline, v.Size, v.Underline, v.LineThrough)
		default:
			fmt.Fprintf(&sb, "  %#v\n", it)
		}
	}
	return sb.String()
}

// Colors reaching paint are finite by construction (css clamps them): out of
// range values are clamped, a NaN or infinite component is an upstream bug and
// fails loudly with pdf.ErrNumber, without a panic.
func TestHostileColors(t *testing.T) {
	for name, tc := range map[string]struct {
		col css.Color
		bad bool
	}{
		"NaN red":      {css.Color{R: math.NaN(), A: 1}, true},
		"NaN alpha":    {css.Color{A: math.NaN()}, true},
		"Inf green":    {css.Color{G: math.Inf(1), A: 1}, false}, // clamped to 1
		"huge blue":    {css.Color{B: 1e30, A: 1}, false},
		"negative":     {css.Color{R: -5, G: -5, B: -5, A: 1}, false},
		"alpha over 1": {css.Color{A: 7}, false},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			run := e.run(t, "color", 14)
			run.X, run.Baseline, run.Color = 10, 100, tc.col
			r := display.Rect{X: 10, Y: 10, W: 50, H: 50}
			c := &pdf.Content{}
			_, err := e.painter.Page(c, a4, display.List{
				display.FillRect{Rect: r, Color: tc.col},
				display.Border{Rect: r, Sides: [4]display.BorderSide{{Width: 2, Color: tc.col}, {Width: 2, Color: tc.col}, {Width: 2, Color: tc.col}, {Width: 2, Color: tc.col}}},
				&run,
			})
			switch {
			case tc.bad && !errors.Is(err, pdf.ErrNumber):
				t.Fatalf("err = %v, want pdf.ErrNumber", err)
			case !tc.bad && err != nil:
				t.Fatalf("err = %v", err)
			}
		})
	}
}
