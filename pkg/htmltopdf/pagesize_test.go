package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func pageSizeDoc(t *testing.T, ps PageSize, html string) (*Result, *pdftest.Doc) {
	t.Helper()
	res, data := renderBytes(t, mustNew(t, Options{PageSize: ps}), html)
	return res, pdftest.Validate(t, data)
}

func mediaBox(doc *pdftest.Doc, p int) (w, h float64) {
	mb := doc.Pages[p].MediaBox
	return mb[2] - mb[0], mb[3] - mb[1]
}

const px = 0.75 // points per CSS px

// The paper replaces the size of the @page rules and only their orientation is
// kept, per page (design 4.1).
func TestPageSizeOrientation(t *testing.T) {
	paper := PageSize{Width: 400, Height: 600}
	for _, tt := range []struct {
		name, css string
		w, h      float64
	}{
		{"no rule", "", 400, 600},
		{"A4 is the paper", "@page { size: A4 }", 400, 600},
		{"landscape", "@page { size: landscape }", 600, 400},
		{"A4 landscape", "@page { size: A4 landscape }", 600, 400},
		{"2in 1in is landscape", "@page { size: 2in 1in }", 600, 400},
		{"1in 2in is portrait", "@page { size: 1in 2in }", 400, 600},
		{"square is portrait", "@page { size: 3in 3in }", 400, 600},
		{"A5 portrait", "@page { size: A5 portrait }", 400, 600},
		{"auto", "@page { size: auto }", 400, 600},
		{"huge landscape size is not clamped", "@page { size: 1e9px 1px }", 600, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, doc := pageSizeDoc(t, paper, "<style>"+tt.css+"</style><p>x</p>")
			if w, h := mediaBox(doc, 0); math.Abs(w-tt.w*px) > 0.01 || math.Abs(h-tt.h*px) > 0.01 {
				t.Errorf("%.2f x %.2f pt, want %v x %v px", w, h, tt.w, tt.h)
			}
		})
	}
}

// The orientation is that of each page's rules: a named page can be landscape
// while the others keep the paper; ExtraCSS wins over the document as usual.
func TestPageSizeOrientationPerNamedPage(t *testing.T) {
	html := `<style>@page { size: A4 } @page wide { size: landscape }</style><p>a</p><div style="page:wide">b</div><p>c</p>`
	_, doc := pageSizeDoc(t, PageSize{Width: 400, Height: 600}, html)
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages", len(doc.Pages))
	}
	for i, want := range [][2]float64{{400, 600}, {600, 400}, {400, 600}} {
		if w, h := mediaBox(doc, i); math.Abs(w-want[0]*px) > 0.01 || math.Abs(h-want[1]*px) > 0.01 {
			t.Errorf("page %d: %.2f x %.2f pt", i+1, w, h)
		}
	}
}

// ExtraCSS wins over the document for the orientation too.
func TestPageSizeOrientationFromExtraCSS(t *testing.T) {
	c, err := New(Options{PageSize: PageSize{Width: 400, Height: 600}, ExtraCSS: "@page { size: landscape }"})
	if err != nil {
		t.Fatal(err)
	}
	_, data := renderBytes(t, c, "<style>@page { size: A4 }</style><p>x</p>")
	if w, h := mediaBox(pdftest.Validate(t, data), 0); w < h {
		t.Errorf("ExtraCSS landscape: %.2f x %.2f", w, h)
	}
}

// The margins still come from the cascade and the margin clamp still applies, on
// the paper (not on the size of the rules).
func TestPageSizeKeepsMarginsAndClamp(t *testing.T) {
	html := `<style>@page { size: 10in 10in; margin: 50px }</style><p>x</p>`
	res, doc := pageSizeDoc(t, PageSize{Width: 200, Height: 300}, html)
	if w, h := mediaBox(doc, 0); res.Pages != 1 || math.Abs(w-200*px) > 0.01 || math.Abs(h-300*px) > 0.01 {
		t.Errorf("%d pages, %.2f x %.2f pt: the 10in rule must not size the paper", res.Pages, w, h)
	}
	clamped := false
	html = `<style>@page { margin: 150px }</style><p>x</p>`
	res, _ = pageSizeDoc(t, PageSize{Width: 200, Height: 300}, html)
	for _, w := range res.Warnings {
		clamped = clamped || (w.Code == "clamped" && w.Detail == "page margins")
	}
	if !clamped {
		t.Errorf("no margin clamp on a 200 px paper: %+v", res.Warnings)
	}
}

// New validates the paper: each side in [96, 19200] px, a twin at each bound.
func TestPageSizeIsValidated(t *testing.T) {
	for _, tt := range []struct {
		ps PageSize
		ok bool
	}{
		{PageSize{}, true},
		{PageSize{96, 96}, true},
		{PageSize{95.99, 600}, false},
		{PageSize{600, 95.99}, false},
		{PageSize{19200, 19200}, true},
		{PageSize{19200.01, 600}, false},
		{PageSize{600, 19200.01}, false},
		{PageSize{19200.0005, 600}, false},
		{PageSize{600, math.NaN()}, false},
		{PageSize{600, math.Inf(-1)}, false},
		{PageSize{600, math.Inf(1)}, false},
		{PageSize{600, 0}, false},
		{PageSize{0, 600}, false},
		{PageSize{-1, 600}, false},
		{PageSize{math.NaN(), 600}, false},
		{PageSize{math.Inf(1), 600}, false},
	} {
		_, err := New(Options{PageSize: tt.ps})
		if tt.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidOptions)) {
			t.Errorf("%+v: %v", tt.ps, err)
		}
	}
}

// PageSize is the paper that the CSS emulation sets, so a landscape template
// gives the same bytes either way: 03 S and L, as the template is.
func TestPageSizeEqualsItsCSSEmulation(t *testing.T) {
	for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
		html := string(m2HTML(t, analyst, size))
		viaOption := referenceOptions(t)
		viaOption.ExtraCSS = ""
		viaCSS := viaOption
		viaCSS.PageSize = PageSize{}
		viaCSS.ExtraCSS = "@page { size: 1122.56px 794.56px }"
		_, a := renderBytes(t, mustNew(t, viaOption), html)
		_, b := renderBytes(t, mustNew(t, viaCSS), html)
		if !bytes.Equal(a, b) {
			t.Errorf("03 %s: PageSize and its CSS emulation differ (%d and %d bytes)", size, len(a), len(b))
		}
		doc := pdftest.Validate(t, a)
		if w, h := mediaBox(doc, 0); math.Abs(w-841.92) > 0.01 || math.Abs(h-595.92) > 0.01 {
			t.Errorf("03 %s: %.2f x %.2f pt, want 841.92 x 595.92", size, w, h)
		}
	}
}

// ExtraCSS is validated against the paper: margins that fit a large paper are
// accepted with it, and still rejected (clamped) on the default page.
func TestPageSizeExtraCSSIsValidatedAgainstThePaper(t *testing.T) {
	css := "@page { margin: 600px }"
	if _, err := New(Options{PageSize: PageSize{Width: 19200, Height: 19200}, ExtraCSS: css}); err != nil {
		t.Errorf("margins that fit a 19200 px paper: %v", err)
	}
	if _, err := New(Options{ExtraCSS: css}); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("the same margins on the default page: %v", err)
	}
	// On a small paper the margins are clamped: that is an invalid ExtraCSS again.
	if _, err := New(Options{PageSize: PageSize{Width: 96, Height: 96}, ExtraCSS: "@page { margin: 50px }"}); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("margins that do not fit a 96 px paper: %v", err)
	}
}

// The extremes of the paper render: 96 px with large margins (clamped, warned)
// and at Scale 2, 19200 px square.
func TestPageSizeExtremesRender(t *testing.T) {
	res, doc := pageSizeDoc(t, PageSize{Width: 96, Height: 96}, "<style>@page { margin: 1000px }</style><p>x</p>")
	if w, h := mediaBox(doc, 0); math.Abs(w-96*px) > 0.01 || math.Abs(h-96*px) > 0.01 || len(res.Warnings) == 0 {
		t.Errorf("96 px paper: %.2f x %.2f pt, warnings %+v", w, h, res.Warnings)
	}
	c, err := New(Options{PageSize: PageSize{Width: 96, Height: 96}, Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, data := renderBytes(t, c, "<p>x y z</p>"); len(data) == 0 {
		t.Error("no output at Scale 2 on a 96 px paper")
	}
	if _, doc := pageSizeDoc(t, PageSize{Width: 19200, Height: 19200}, "<p>x</p>"); len(doc.Pages) != 1 {
		t.Errorf("%d pages", len(doc.Pages))
	}
}

// A Converter with a paper is shared by concurrent renders (run under -race).
func TestPageSizeConverterIsSafeForConcurrentUse(t *testing.T) {
	c := mustNew(t, Options{PageSize: PageSize{Width: 400, Height: 600}})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			css := ""
			if i%2 == 1 {
				css = "@page { size: landscape }"
			}
			var buf bytes.Buffer
			if _, err := c.Render(context.Background(), strings.NewReader("<style>"+css+"</style><p>x</p>"), &buf); err != nil {
				t.Error(err)
				return
			}
			doc := pdftest.Validate(t, buf.Bytes())
			if w, h := mediaBox(doc, 0); (w > h) != (i%2 == 1) {
				t.Errorf("render %d: %.2f x %.2f", i, w, h)
			}
		}()
	}
	wg.Wait()
}
