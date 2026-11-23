package pdf

import (
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-text/typesetting/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomediumitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/gofont/gosmallcaps"
	"golang.org/x/image/font/gofont/gosmallcapsitalic"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
)

// s16 reads v, an unsigned 16-bit value, as a signed one.
func s16(v int) int16 {
	if v >= 0x8000 {
		v -= 0x10000
	}
	return int16(max(min(v, math.MaxInt16), math.MinInt16))
}

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

// testFont is a real font with the go-text face used to look glyphs up.
type testFont struct {
	prog *FontProgram
	face *font.Face
	upem float64
}

func loadTestFont(tb testing.TB, file string) *testFont {
	tb.Helper()
	data := readFont(tb, file)
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		tb.Fatal(err)
	}
	tt, err := parseTTF(data)
	if err != nil {
		tb.Fatal(err)
	}
	ext, _ := face.FontHExtents()
	head := tt.table("head")
	bbox := [4]int16{}
	for i := range bbox {
		bbox[i] = s16(u16(head, 36+2*i))
	}
	return &testFont{
		prog: &FontProgram{
			Data: data, Name: strings.TrimSuffix(file, ".ttf"), UnitsPerEm: clamp16(tt.upem),
			Ascent: int16(ext.Ascender), Descent: int16(ext.Descender), CapHeight: int16(ext.Ascender * 0.7), BBox: bbox,
		},
		face: face,
		upem: float64(tt.upem),
	}
}

// run shapes text one glyph per rune, with the font's own advances.
func (f *testFont) run(tb testing.TB, text string, size float64) []Glyph {
	tb.Helper()
	var out []Glyph
	for _, r := range text {
		gid, ok := f.face.NominalGlyph(r)
		if !ok {
			tb.Fatalf("%s has no glyph for %q", f.prog.Name, r)
		}
		out = append(out, Glyph{ID: clamp16(int(gid)), Advance: float64(f.face.HorizontalAdvance(gid)) * size / f.upem, Text: string(r)})
	}
	return out
}

func embeddedDoc(tb testing.TB, f *testFont, texts ...string) (*pdftest.Doc, []byte) {
	tb.Helper()
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id := addFont(tb, w, f.prog)
	c := &Content{}
	for i, t := range texts {
		c.Glyphs(id, 12, 50, 700-float64(i)*20, f.run(tb, t, 12))
	}
	if _, err := w.AddPage(595, 842, c, false, nil); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(Info{Title: "t"}); err != nil {
		tb.Fatal(err)
	}
	return pdftest.Validate(tb, buf.Bytes()), buf.Bytes()
}

func TestEmbedFrenchText(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	const text = "Été à Noël : «Où êtes-vous ?» — ça coûte 12,50 € … <RLO>"
	doc, _ := embeddedDoc(t, lib, text)
	if got := doc.Pages[0].Text; len(got) != 1 || got[0] != text {
		t.Fatalf("text = %q", got)
	}
}

func TestEmbedArrowFromDejaVu(t *testing.T) {
	dv := loadTestFont(t, "DejaVuSans.ttf")
	const text = "↳ … — « » ⟨RLO⟩"
	doc, _ := embeddedDoc(t, dv, text)
	if got := doc.Pages[0].Text; len(got) != 1 || got[0] != text {
		t.Fatalf("text = %q", got)
	}
}

func TestSubsetKeepsOutlinesAndFitsBudget(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	var sb strings.Builder
	for r := rune(0x20); sb.Len() < 100 && r < 0x17f; r++ {
		if _, ok := lib.face.NominalGlyph(r); ok {
			sb.WriteRune(r)
		}
	}
	text := sb.String()
	doc, _ := embeddedDoc(t, lib, text)
	f := doc.Fonts["F1"]
	if f == nil {
		t.Fatal("no font F1")
	}
	if len(f.Program) >= 30<<10 {
		t.Fatalf("subset of %d glyphs is %d bytes, want < 30 KiB", len(text), len(f.Program))
	}
	sub, err := font.ParseTTF(bytes.NewReader(f.Program))
	if err != nil {
		t.Fatalf("go-text cannot parse the subset: %v", err)
	}
	used := map[uint16]bool{}
	for _, g := range lib.run(t, text, 1) {
		used[g.ID] = true
	}
	same := 0
	for g := range used {
		a, b := lib.face.GlyphData(font.GID(g)), sub.GlyphData(font.GID(g))
		if a == nil || b == nil || len(outlineOf(a)) != len(outlineOf(b)) {
			t.Fatalf("glyph %d outline differs", g)
		}
		same++
	}
	t.Logf("%d distinct glyphs, program %d bytes (original %d)", same, len(f.Program), len(lib.prog.Data))
	for g := 0; g < 300; g++ {
		if g == 0 || used[uint16(g)] {
			continue
		}
		if o := sub.GlyphData(font.GID(g)); o != nil && len(outlineOf(o)) != 0 && !isComponent(t, f.Program, g, used) {
			t.Fatalf("unused glyph %d kept its outline", g)
		}
	}
	for g, w := range f.Widths {
		if want := int(float64(lib.face.HorizontalAdvance(font.GID(g)))*1000/lib.upem + 0.5); w != want {
			t.Errorf("glyph %d width %d, want %d", g, w, want)
		}
	}
}

func outlineOf(d font.GlyphData) []font.Segment {
	if o, ok := d.(font.GlyphOutline); ok {
		return o.Segments
	}
	return nil
}

// isComponent reports whether glyph g is a component of a kept composite glyph.
func isComponent(tb testing.TB, program []byte, g int, used map[uint16]bool) bool {
	tb.Helper()
	tt, err := parseTTF(program)
	if err != nil {
		tb.Fatal(err)
	}
	for u := range used {
		data, err := tt.glyph(int(u))
		if err != nil {
			continue
		}
		comps, _ := components(nil, data)
		for _, c := range comps {
			if c == g {
				return true
			}
		}
	}
	return false
}

func TestCompositeComponentsKept(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	orig, err := parseTTF(lib.prog.Data)
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := lib.face.NominalGlyph('é')
	data, _ := orig.glyph(int(gid))
	comps, err := components(nil, data)
	if err != nil || len(comps) < 2 {
		t.Skipf("é is not composite in this font (components %v, %v)", comps, err)
	}
	doc, _ := embeddedDoc(t, lib, "é")
	sub, err := parseTTF(doc.Fonts["F1"].Program)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range comps {
		d, err := sub.glyph(c)
		want, _ := orig.glyph(c)
		if err != nil || len(want) > 0 && !bytes.Equal(d, want) {
			t.Fatalf("component %d of é lost or altered", c)
		}
	}
	if got := doc.Pages[0].Text; len(got) != 1 || got[0] != "é" {
		t.Fatalf("text = %q", got)
	}
}

func TestEmbeddingIsDeterministic(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	_, a := embeddedDoc(t, lib, "Bonjour le monde", "déjà vu")
	_, b := embeddedDoc(t, lib, "Bonjour le monde", "déjà vu")
	if !bytes.Equal(a, b) {
		t.Fatal("two runs differ")
	}
}

func TestGlyphsOperatorFormat(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}, {adv: 700, data: simpleGlyph()}})
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
	c := &Content{}
	// At size 10 the font widths are 6 and 7 units; the second glyph is 1.5 wider.
	c.Glyphs(id, 10, 1, 2, []Glyph{{ID: 1, Advance: 6}, {ID: 2, Advance: 8.5}, {ID: 1, Advance: 6}})
	want := "BT\n/F1 10 Tf\n1 2 Td\n[<00010002> -150 <0001>] TJ\nET\n"
	if got := string(c.Bytes()); got != want || c.Err() != nil {
		t.Fatalf("got %q err=%v\nwant %q", got, c.Err(), want)
	}
}

func TestGlyphsErrors(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}})
	w := newTestWriter(&bytes.Buffer{})
	id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
	tests := []struct {
		name string
		f    func(c *Content)
		want error
	}{
		{"zero font id", func(c *Content) { c.Glyphs(FontID{}, 10, 0, 0, []Glyph{{ID: 0}}) }, ErrFontID},
		{"glyph beyond the font", func(c *Content) { c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 2}}) }, ErrGlyphID},
		{"last glyph of the font is fine", func(c *Content) { c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1}}) }, nil},
		{"zero size", func(c *Content) { c.Glyphs(id, 0, 0, 0, []Glyph{{ID: 0}}) }, ErrNumber},
		{"nan position", func(c *Content) { c.Glyphs(id, 10, nan(), 0, []Glyph{{ID: 0}}) }, ErrNumber},
		{"nan advance", func(c *Content) { c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 0, Advance: nan()}, {ID: 0}}) }, ErrNumber},
		{"empty run draws nothing", func(c *Content) { c.Glyphs(id, 10, 0, 0, nil) }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Content{}
			tt.f(c)
			if !errors.Is(c.Err(), tt.want) {
				t.Fatalf("err = %v, want %v", c.Err(), tt.want)
			}
		})
	}
	foreign := newTestWriter(&bytes.Buffer{})
	c := &Content{}
	c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 0}})
	if _, err := foreign.AddPage(10, 10, c, false, nil); !errors.Is(err, ErrFontID) {
		t.Fatalf("font of another writer accepted: %v", err)
	}
}

func TestClusterTextLimit(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}})
	for _, tt := range []struct {
		name string
		text string
		want string
	}{
		{"just under", strings.Repeat("a", MaxClusterText-1), strings.Repeat("a", MaxClusterText-1)},
		{"at the limit", strings.Repeat("a", MaxClusterText), strings.Repeat("a", MaxClusterText)},
		{"over", strings.Repeat("a", MaxClusterText+1), strings.Repeat("a", MaxClusterText)},
		{"cut on a rune boundary", strings.Repeat("a", MaxClusterText-1) + "é", strings.Repeat("a", MaxClusterText-1)},
		{"invalid utf8", "a\xffb", "a�b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestWriter(&bytes.Buffer{})
			id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
			c := &Content{}
			c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Text: tt.text}})
			if err := mustAddPage(t, w, c); err != nil {
				t.Fatal(err)
			}
			if got := id.e.uni[1]; got != tt.want {
				t.Fatalf("kept %q, want %q", got, tt.want)
			}
		})
	}
}

func mustAddPage(tb testing.TB, w *Writer, c *Content) error {
	tb.Helper()
	_, err := w.AddPage(100, 100, c, false, nil)
	return err
}

func TestToUnicodeConflictIsDeterministic(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}})
	for _, order := range [][]string{{"b", "a", "c"}, {"c", "b", "a"}, {"a", "c", "b"}} {
		w := newTestWriter(&bytes.Buffer{})
		id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
		for _, txt := range order {
			c := &Content{}
			c.Glyphs(id, 10, 0, 0, []Glyph{{ID: 1, Text: txt}})
			if err := mustAddPage(t, w, c); err != nil {
				t.Fatal(err)
			}
		}
		if id.e.uni[1] != "a" {
			t.Fatalf("order %v kept %q", order, id.e.uni[1])
		}
	}
}

func TestUnusedFontIsNotEmbedded(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	dv := loadTestFont(t, "DejaVuSans.ttf")
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	addFont(t, w, lib.prog)
	id2 := addFont(t, w, dv.prog)
	c := &Content{}
	c.Glyphs(id2, 12, 0, 0, dv.run(t, "ab", 12))
	if _, err := w.AddPage(100, 100, c, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if len(doc.Fonts) != 1 || doc.Fonts["F2"] == nil || doc.Pages[0].Text[0] != "ab" {
		t.Fatalf("fonts = %v, text = %v", doc.Fonts, doc.Pages[0].Text)
	}
}

func TestTwoFontsOnTwoPagesDeferred(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	dv := loadTestFont(t, "DejaVuSans.ttf")
	var buf bytes.Buffer
	w := newTestWriter(&buf)
	a, b := addFont(t, w, lib.prog), addFont(t, w, dv.prog)
	p, err := w.AddPage(100, 100, nil, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	c1 := &Content{}
	c1.Glyphs(a, 10, 0, 0, lib.run(t, "Hello", 10))
	if _, err := w.AddPage(100, 100, c1, false, nil); err != nil {
		t.Fatal(err)
	}
	c2 := &Content{}
	c2.Glyphs(b, 10, 0, 0, dv.run(t, "↳ ok", 10))
	if err := w.WriteDeferred(p, c2); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	doc := pdftest.Validate(t, buf.Bytes())
	if doc.Pages[0].Text[0] != "↳ ok" || doc.Pages[1].Text[0] != "Hello" || len(doc.Fonts) != 2 {
		t.Fatalf("pages: %q %q, fonts %d", doc.Pages[0].Text, doc.Pages[1].Text, len(doc.Fonts))
	}
}

func TestInvalidFontIsRefused(t *testing.T) {
	good := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}})
	tests := []struct {
		name string
		prog *FontProgram
	}{
		{"nil", nil},
		{"empty", &FontProgram{}},
		{"garbage", &FontProgram{Data: []byte("not a font at all, really not")}},
		{"wrong upem", &FontProgram{Data: good, UnitsPerEm: 2048}},
		{"truncated", &FontProgram{Data: good[:60]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestWriter(&bytes.Buffer{})
			id, err := w.AddFont(tt.prog)
			if !errors.Is(err, ErrInvalidFont) || id.e != nil {
				t.Fatalf("AddFont = %v, %v", id, err)
			}
			// The refusal leaves the writer usable.
			if _, err := w.AddPage(10, 10, &Content{}, false, nil); err != nil {
				t.Fatalf("AddPage after a refused font: %v", err)
			}
			if err := w.Close(Info{}); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	}
	w := newTestWriter(&bytes.Buffer{})
	if _, err := w.AddPage(10, 10, &Content{}, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(Info{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AddFont(&FontProgram{Data: good}); !errors.Is(err, ErrClosed) {
		t.Fatalf("AddFont after Close: %v", err)
	}
}

func TestValidateFont(t *testing.T) {
	fonts := map[string][]byte{}
	for _, name := range []string{
		"LiberationSans-Regular.ttf", "LiberationSans-Bold.ttf", "LiberationSans-Italic.ttf",
		"LiberationSans-BoldItalic.ttf", "DejaVuSans.ttf", "DejaVuSans-Bold.ttf", "DejaVuSansMono.ttf",
	} {
		fonts[name] = readFont(t, name)
	}
	for name, data := range map[string][]byte{
		"goregular": goregular.TTF, "goitalic": goitalic.TTF, "gobold": gobold.TTF, "gobolditalic": gobolditalic.TTF,
		"gomono": gomono.TTF, "gomonoitalic": gomonoitalic.TTF, "gomonobold": gomonobold.TTF, "gomonobolditalic": gomonobolditalic.TTF,
		"gomedium": gomedium.TTF, "gomediumitalic": gomediumitalic.TTF, "gosmallcaps": gosmallcaps.TTF, "gosmallcapsitalic": gosmallcapsitalic.TTF,
	} {
		fonts[name] = data
	}
	for name, data := range fonts {
		if err := ValidateFont(data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, data := range [][]byte{nil, []byte("junk"), fonts["goregular"][:100], fonts["LiberationSans-Regular.ttf"][:5000]} {
		if err := ValidateFont(data); !errors.Is(err, ErrInvalidFont) {
			t.Errorf("accepted %d bytes: %v", len(data), err)
		}
	}
}

func TestSafeFontName(t *testing.T) {
	for in, want := range map[string]string{
		"Liberation Sans": "LiberationSans", "A/B(C)<>%#": "A-B-C-----", "": "Font", "é": "-",
		strings.Repeat("x", 100): strings.Repeat("x", 48),
	} {
		if got := safeFontName(in); got != want {
			t.Errorf("safeFontName(%q) = %q, want %q", in, got, want)
		}
	}
}

// pdfcpu cannot extract Type0 fonts (ExtractFonts reports them as unsupported),
// so it is used to validate the structure strictly and to read the content
// streams back independently.
func TestPDFCPUReadsTheSameContent(t *testing.T) {
	lib := loadTestFont(t, "LiberationSans-Regular.ttf")
	doc, data := embeddedDoc(t, lib, "Où êtes-vous ?", "Deuxième ligne")
	got, err := pdftest.PDFCPUContent(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !bytes.Equal(got[1], doc.Pages[0].Content) {
		t.Fatalf("pdfcpu content differs from ours (%d pages)", len(got))
	}
}

func TestGlyphsRespectTheContentCap(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}})
	w := newTestWriter(&bytes.Buffer{})
	id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
	run := []Glyph{{ID: 1, Advance: 6}}
	probe := &Content{}
	probe.Glyphs(id, 10, 1, 2, run)
	n := len(probe.Bytes())
	for _, tt := range []struct {
		name string
		room int
		ok   bool
	}{{"just under", n + 1, true}, {"at the cap", n, true}, {"over", n - 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Content{buf: make([]byte, MaxContentBytes-tt.room)}
			c.Glyphs(id, 10, 1, 2, run)
			if tt.ok != (c.Err() == nil) || (!tt.ok && (!errors.Is(c.Err(), ErrContentSize) || len(c.Bytes()) != MaxContentBytes-tt.room)) {
				t.Fatalf("err = %v, len = %d", c.Err(), len(c.Bytes()))
			}
		})
	}
}

func TestHugeRunIsStoppedAtTheCapAndNotKept(t *testing.T) {
	data := buildFont(t, []glyphSpec{{adv: 500}, {adv: 600, data: simpleGlyph()}})
	w := newTestWriter(&bytes.Buffer{})
	id := addFont(t, w, &FontProgram{Data: data, Name: "T"})
	run := make([]Glyph, 2_000_000)
	for i := range run {
		run[i] = Glyph{ID: 1, Advance: 6}
	}
	c := &Content{buf: make([]byte, MaxContentBytes-1000)}
	c.Glyphs(id, 10, 0, 0, run)
	if !errors.Is(c.Err(), ErrContentSize) || len(c.Bytes()) != MaxContentBytes-1000 {
		t.Fatalf("err = %v, len = %d", c.Err(), len(c.Bytes()))
	}
	if cap(c.tmp) > maxScratch {
		t.Fatalf("scratch buffer of %d bytes kept", cap(c.tmp))
	}
	small := &Content{}
	small.Glyphs(id, 10, 0, 0, run[:10])
	if small.Err() != nil || cap(small.tmp) == 0 || cap(small.tmp) > maxScratch {
		t.Fatalf("small run: err %v, scratch %d", small.Err(), cap(small.tmp))
	}
}

func TestClusterTextIsBoundedWork(t *testing.T) {
	huge := strings.Repeat("\xff", 1<<20)
	if got := clusterText(huge); len(got) > MaxClusterText || !utf8.ValidString(got) {
		t.Fatalf("got %d bytes", len(got))
	}
	if allocs := testing.AllocsPerRun(5, func() { clusterText(huge) }); allocs > 3 {
		t.Fatalf("%v allocations: the whole text was processed", allocs)
	}
	if got := clusterText(strings.Repeat("é", 1<<20)); got != strings.Repeat("é", MaxClusterText/2) {
		t.Fatalf("got %d bytes", len(got))
	}
}

func TestOffer(t *testing.T) {
	m := map[uint16]string{}
	for _, in := range []struct{ t, want string }{{"", ""}, {"b", "b"}, {"c", "b"}, {"a", "a"}, {"", "a"}, {"ab", "a"}} {
		offer(m, 1, in.t)
		if m[1] != in.want {
			t.Fatalf("after %q: %q, want %q", in.t, m[1], in.want)
		}
	}
	if _, ok := m[2]; ok {
		t.Fatal("untouched glyph recorded")
	}
	offer(m, 2, "")
	if v, ok := m[2]; !ok || v != "" {
		t.Fatal("empty offer must record the glyph")
	}
}

func TestValidateFontReadsEveryGlyph(t *testing.T) {
	tests := []struct {
		name   string
		glyphs []glyphSpec
		ok     bool
	}{
		{"plain", []glyphSpec{{}, {data: simpleGlyph()}}, true},
		{"composite", []glyphSpec{{}, {data: simpleGlyph()}, {data: compositeGlyph(1, 1)}}, true},
		{"255 components", []glyphSpec{{}, {data: simpleGlyph()}, {data: manyComponents(maxComponentsPerGlyph - 1)}}, true},
		{"256 components", []glyphSpec{{}, {data: simpleGlyph()}, {data: manyComponents(maxComponentsPerGlyph)}}, true},
		{"257 components", []glyphSpec{{}, {data: simpleGlyph()}, {data: manyComponents(maxComponentsPerGlyph + 1)}}, false},
		{"component beyond the font", []glyphSpec{{}, {data: simpleGlyph()}, {data: compositeGlyph(3)}}, false},
		{"truncated composite", []glyphSpec{{}, {data: simpleGlyph()}, {data: compositeGlyph(1, 1)[:14]}}, false},
		{"bad glyph is never drawn", []glyphSpec{{}, {data: simpleGlyph()}, {data: compositeGlyph(9)}, {data: simpleGlyph()}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFont(buildFont(t, tt.glyphs))
			if tt.ok != (err == nil) || (!tt.ok && !errors.Is(err, ErrInvalidFont)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
