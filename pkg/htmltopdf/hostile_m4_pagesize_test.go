package htmltopdf

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf"
)

// h4PaperW and h4PaperH are the reference paper in CSS px.
const h4PaperW, h4PaperH = 794.56, 1122.56

func h4PageSizeOpts(t *testing.T, ps PageSize, extra string) Options {
	t.Helper()
	o := referenceOptions(t)
	o.PageSize = ps
	o.ExtraCSS = extra
	return o
}

// Each side of PageSize must lie in [96, 19200] CSS px: the bounds render with their exact paper on
// every page, and the hostile numbers give ErrInvalidOptions from New. The values just outside the bounds
// and a single zero side are pagesize_test.go's.
func TestM4PageSizeValidation(t *testing.T) {
	for _, ps := range []PageSize{{96, 96}, {19200, 19200}, {96, 19200}, {19200, 96}, {h4PaperW, h4PaperH}, {96.0000001, 19199.9999}} {
		t.Run(fmt.Sprintf("valid %v x %v", ps.Width, ps.Height), func(t *testing.T) {
			words, html := h4Markers("PSB", 3)
			r := h4Render(t, h4PageSizeOpts(t, ps, ""), html, false)
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4MediaBoxes(t, r, ps.Width, ps.Height)
			h4TextOnce(t, r, words)
		})
	}
	negZero := math.Copysign(0, -1)
	for name, ps := range map[string]PageSize{
		"NaN width": {math.NaN(), 100}, "NaN height": {100, math.NaN()}, "NaN both": {math.NaN(), math.NaN()},
		"+Inf width": {math.Inf(1), 100}, "-Inf height": {100, math.Inf(-1)}, "negative width": {-100, 100}, "negative height": {100, -100},
		"1e308": {1e308, 1e308}, "MaxFloat64": {math.MaxFloat64, math.MaxFloat64}, "denormal": {5e-324, 100}, "negative zero width": {negZero, 100},
	} {
		t.Run("invalid "+name, func(t *testing.T) {
			if _, err := New(h4PageSizeOpts(t, ps, "")); !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("PageSize %v: %v, want ErrInvalidOptions", ps, err)
			}
		})
	}
	t.Run("zero value leaves the page size to @page", func(t *testing.T) {
		if _, err := New(h4PageSizeOpts(t, PageSize{}, "")); err != nil {
			t.Error(err)
		}
	})
}

// The orientation of each page comes from the computed size of its @page rules (all origins); the paper
// is the one given, swapped for landscape. pagesize_test.go has the plain keyword and length rows; here are
// the origins and the hostile values, each with its text present once on every page of the right box.
func TestM4PageSizeOrientation(t *testing.T) {
	paper := PageSize{h4PaperW, h4PaperH}
	for _, tc := range []struct {
		name, doc, extra string
		landscape        bool
	}{
		{"no @page", "", "", false},
		{"size: 100px 50px", "@page{size:100px 50px}", "", true},
		{"size: 100px", "@page{size:100px}", "", false},
		{"size: portrait", "@page{size:portrait}", "", false},
		{"size 1e999px 1px (clamped to 1e7px: wider than tall)", "@page{size:1e999px 1px}", "", true},
		{"ExtraCSS portrait beats the document", "@page{size:landscape}", "@page{size:portrait}", false},
		{"ExtraCSS landscape beats the document", "@page{size:portrait}", "@page{size:landscape}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			words, html := h4Markers("ORI", 60)
			r := h4Render(t, h4PageSizeOpts(t, paper, tc.extra), "<style>"+tc.doc+"</style>"+html, false)
			if r.err != nil {
				t.Fatal(r.err)
			}
			w, h := paper.Width, paper.Height
			if tc.landscape {
				w, h = h, w
			}
			h4MediaBoxes(t, r, w, h)
			h4TextOnce(t, r, words)
		})
	}
}

// Orientation is per page: named pages alternating landscape and portrait give alternating media boxes.
// 500 pages render and 501 end in a pages LimitError; the 500 pages are measured against a twin of 500
// plain pages (heap and bytes).
func TestM4PageSizeAlternatingOrientationAtMaxPages(t *testing.T) {
	paper := PageSize{h4PaperW, h4PaperH}
	doc := func(n int, named bool) (string, []string) {
		var sb strings.Builder
		var words []string
		sb.WriteString(`<style>@page l{size:landscape} @page p{size:portrait} div{height:5px;break-after:page}</style>`)
		for i := range n {
			w := fmt.Sprintf("ALT%05d", i)
			words = append(words, w)
			cls := "p"
			if i%2 == 0 {
				cls = "l"
			}
			if named {
				sb.WriteString(`<div style="page:` + cls + `">` + w + `</div>`)
			} else {
				sb.WriteString(`<div>` + w + `</div>`)
			}
		}
		return sb.String(), words
	}
	html, words := doc(500, true)
	r := h4RenderTwin(t, h4PageSizeOpts(t, paper, ""), html)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.res.Pages != 500 {
		t.Fatalf("%d pages", r.res.Pages)
	}
	for i, p := range r.doc.Pages {
		land := p.MediaBox[2] > p.MediaBox[3]
		if land != (i%2 == 0) {
			t.Fatalf("page %d: media box %v", i+1, p.MediaBox)
		}
	}
	h4TextOnce(t, r, words)
	plain, _ := doc(500, false)
	twin := h4RenderTwin(t, h4PageSizeOpts(t, paper, ""), plain)
	if twin.err != nil {
		t.Fatal(twin.err)
	}
	t.Logf("500 alternating pages: work %d boxes %d glyphs %d; twin of plain pages: work %d boxes %d glyphs %d", r.work, r.boxes, r.glyphs, twin.work, twin.boxes, twin.glyphs)
	if r.res.Pages != twin.res.Pages || r.boxes != twin.boxes || r.glyphs != twin.glyphs || r.work > 2*twin.work+10000 || r.peak > 2*twin.peak+32<<20 || r.res.Bytes > 2*twin.res.Bytes+1<<20 {
		t.Errorf("500 alternating pages: %d pages, work %d, boxes %d, glyphs %d, heap %d MiB, %d bytes; twin of plain pages: %d pages, work %d, boxes %d, glyphs %d, heap %d MiB, %d bytes",
			r.res.Pages, r.work, r.boxes, r.glyphs, r.peak>>20, r.res.Bytes, twin.res.Pages, twin.work, twin.boxes, twin.glyphs, twin.peak>>20, twin.res.Bytes)
	}
	over, _ := doc(501, true)
	r2 := h4Render(t, h4PageSizeOpts(t, paper, ""), over, true)
	if r2.err == nil {
		t.Fatal("501 alternating pages rendered")
	}
	var le *LimitError
	if !errors.As(r2.err, &le) || le.Limit != LimitPages {
		t.Errorf("501 pages: %v, want a pages LimitError", r2.err)
	}
}

// Extreme papers with hostile margins: the margin clamp still applies and nothing is hidden. Each row
// has the exact paper it must print on and is held against a twin on the same paper with default margins:
// at most one page per paragraph, bytes and heap in proportion. A row ends in a pages LimitError only when
// it says so (wantLimit).
func TestM4PageSizeExtremesAndMargins(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ps        PageSize
		css       string
		wantW     float64 // the paper the pages must have, in CSS px
		wantH     float64
		wantLimit bool
	}{
		{"96x96 default margins", PageSize{96, 96}, "", 96, 96, false},
		{"96x96 margin 1e9px", PageSize{96, 96}, "@page{margin:1e9px}", 96, 96, false},
		{"96x96 margin -1e9px", PageSize{96, 96}, "@page{margin:-1e9px}", 96, 96, false},
		{"96x96 margin 50px", PageSize{96, 96}, "@page{margin:50px}", 96, 96, false},
		{"96x96 margin boxes", PageSize{96, 96}, "@page{margin:10px;@top-center{content:'T';border-bottom:3px solid red}@bottom-left{content:counter(page)}}", 96, 96, false},
		{"19200x19200", PageSize{19200, 19200}, "", 19200, 19200, false},
		{"19200x19200 margin 1e9px", PageSize{19200, 19200}, "@page{margin:1e9px}", 19200, 19200, false},
		{"19200x96 landscape swap", PageSize{19200, 96}, "@page{size:landscape}", 96, 19200, false},
		{"96x19200 landscape swap", PageSize{96, 19200}, "@page{size:landscape}", 19200, 96, false},
		{"1e9px size ignored", PageSize{h4PaperW, h4PaperH}, "@page{size:1e9px 1e9px}", h4PaperW, h4PaperH, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			words, html := h4Markers("EXT", 40)
			// Hostile page rules come from the document: ExtraCSS is trusted configuration and New rejects a clamped one.
			opts := h4PageSizeOpts(t, tc.ps, "")
			r := h4RenderTwin(t, opts, "<style>"+tc.css+"</style>"+html)
			if tc.wantLimit {
				var le *LimitError
				if !errors.As(r.err, &le) || le.Limit != LimitPages {
					t.Fatalf("%v, want a pages LimitError", r.err)
				}
				return
			}
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4MediaBoxes(t, r, tc.wantW, tc.wantH)
			h4TextOnce(t, r, words)
			for _, p := range r.doc.Pages {
				if p.MediaBox[2] > pdf.MaxPageSize+0.01 || p.MediaBox[3] > pdf.MaxPageSize+0.01 {
					t.Fatalf("media box %v above the PDF limit", p.MediaBox)
				}
			}
			twin := h4RenderTwin(t, opts, "<style>@page{size:"+fmt.Sprintf("%gpx %gpx", tc.wantW, tc.wantH)+"}</style>"+html)
			if twin.err != nil {
				t.Fatal(twin.err)
			}
			t.Logf("work %d boxes %d glyphs %d, %d pages; twin: work %d boxes %d glyphs %d, %d pages", r.work, r.boxes, r.glyphs, r.res.Pages, twin.work, twin.boxes, twin.glyphs, twin.res.Pages)
			if r.res.Pages > len(words)+1 || r.res.Bytes > 4*twin.res.Bytes+1<<20 || r.peak > 2*twin.peak+32<<20 {
				t.Errorf("%d pages for %d paragraphs, %d bytes, heap %d MiB; twin with default margins: %d pages, %d bytes, heap %d MiB",
					r.res.Pages, len(words), r.res.Bytes, r.peak>>20, twin.res.Pages, twin.res.Bytes, twin.peak>>20)
			}
			// The margins change no box and no glyph (the margin-box row adds its own text): the boxes are
			// those of the twin and the work stays within twice the twin's.
			if r.boxes != twin.boxes || r.work > 2*twin.work+10000 || r.glyphs < twin.glyphs {
				t.Errorf("work %d, boxes %d, glyphs %d; twin: work %d, boxes %d, glyphs %d", r.work, r.boxes, r.glyphs, twin.work, twin.boxes, twin.glyphs)
			}
		})
	}
}
