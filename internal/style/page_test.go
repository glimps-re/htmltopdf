package style

import (
	"math"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func pageOf(t testing.TB, docCSS, extraCSS string) (*PageStyle, *budget.Budget) {
	t.Helper()
	b := newBudget()
	var sheets []*Sheet
	if docCSS != "" {
		sheets = append(sheets, compileSheet(t, docCSS, css.OriginDocument, b))
	}
	var extra *Sheet
	if extraCSS != "" {
		extra = compileSheet(t, extraCSS, css.OriginExtra, b)
	}
	return NewResolver(ua(), extra, sheets, printEnv, b).Page(PageContext{Number: 1}), b
}

func nearPx(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func TestPageDefaults(t *testing.T) {
	ps, _ := pageOf(t, "", "")
	if !nearPx(ps.Width, 793.7008) || !nearPx(ps.Height, 1122.5197) {
		t.Errorf("size %v x %v", ps.Width, ps.Height)
	}
	for i, m := range ps.Margin {
		if !nearPx(m, 37.7953) {
			t.Errorf("margin %d = %v", i, m)
		}
	}
	for i, b := range ps.Boxes {
		if b != nil {
			t.Errorf("box %d without content must be nil", i)
		}
	}
}

func TestPageSizes(t *testing.T) {
	tests := []struct {
		css  string
		w, h float64
	}{
		{"@page { size: A4 }", 210 * mmPx, 297 * mmPx},
		{"@page { size: a4 landscape }", 297 * mmPx, 210 * mmPx},
		{"@page { size: landscape }", 297 * mmPx, 210 * mmPx},
		{"@page { size: landscape A3 }", 420 * mmPx, 297 * mmPx},
		{"@page { size: letter portrait }", 8.5 * 96, 11 * 96},
		{"@page { size: legal }", 8.5 * 96, 14 * 96},
		{"@page { size: A5 landscape }", 210 * mmPx, 148 * mmPx},
		{"@page { size: 200mm 100mm }", 200 * mmPx, 100 * mmPx},
		{"@page { size: 10cm }", 100 * mmPx, 100 * mmPx},
		{"@page { size: 5in 6in }", 480, 576},
		{"@page { size: auto }", 210 * mmPx, 297 * mmPx},
	}
	for _, tt := range tests {
		ps, b := pageOf(t, "", tt.css)
		if !nearPx(ps.Width, tt.w) || !nearPx(ps.Height, tt.h) {
			t.Errorf("%s: %v x %v, want %v x %v", tt.css, ps.Width, ps.Height, tt.w, tt.h)
		}
		if w, _ := b.Warnings(); len(w) != 0 {
			t.Errorf("%s: warnings %+v", tt.css, w)
		}
	}
}

func TestPageSizeInvalid(t *testing.T) {
	for _, src := range []string{
		"@page { size: A4 A3 }", "@page { size: 10px A4 }", "@page { size: 1px 2px 3px }", "@page { size: 50% }",
		"@page { size: -1px }", "@page { size: portrait landscape }", "@page { size: nonsense }", "@page { size: 0 }",
	} {
		b := newBudget()
		ss, err := css.ParseStylesheet([]byte(src), css.OriginExtra, b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(ss, b); err != nil {
			t.Fatal(err)
		}
		if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != budget.WarnInvalidValue {
			t.Errorf("%s: warnings %+v", src, w)
		}
	}
}

func TestPageSizeClamp(t *testing.T) {
	ps, b := pageOf(t, "", "@page { size: 99999999in 0.1in; margin: 0 }")
	if ps.Width != MaxPageSidePx || ps.Height != MinPageSidePx {
		t.Errorf("size %v x %v", ps.Width, ps.Height)
	}
	if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != budget.WarnClamped {
		t.Errorf("warnings %+v", w)
	}
	// Exactly at the bounds: no warning.
	ps, b = pageOf(t, "", "@page { size: 200in 1in; margin: 0 }")
	if ps.Width != MaxPageSidePx || ps.Height != MinPageSidePx {
		t.Errorf("size %v x %v", ps.Width, ps.Height)
	}
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Errorf("at the bounds: warnings %+v", w)
	}
}

func TestPageMargins(t *testing.T) {
	ps, _ := pageOf(t, "", "@page { size: 10in 10in; margin: 1in 0.3in 1.3in }")
	want := [4]float64{96, 28.8, 124.8, 28.8}
	for i := range want {
		if !nearPx(ps.Margin[i], want[i]) {
			t.Errorf("margin %d = %v, want %v", i, ps.Margin[i], want[i])
		}
	}
	ps, _ = pageOf(t, "", "@page { size: 10in 10in; margin-top: 5%; margin-left: 2em; margin-right: 1rem }")
	if !nearPx(ps.Margin[Top], 48) || !nearPx(ps.Margin[Left], 32) || !nearPx(ps.Margin[Right], 16) {
		t.Errorf("margins %v", ps.Margin)
	}
	// Margins that leave less than 1 in are scaled down together.
	ps, b := pageOf(t, "", "@page { size: 10in 10in; margin: 50in }")
	if !nearPx(ps.Margin[Left]+ps.Margin[Right], 9*96) || !nearPx(ps.Margin[Top]+ps.Margin[Bottom], 9*96) || !nearPx(ps.Margin[Left], ps.Margin[Right]) {
		t.Errorf("margins %v", ps.Margin)
	}
	if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != budget.WarnClamped {
		t.Errorf("warnings %+v", w)
	}
	// Exactly 1 in left: not clamped.
	ps, b = pageOf(t, "", "@page { size: 10in 10in; margin: 4.5in }")
	if !nearPx(ps.Margin[Left], 4.5*96) {
		t.Errorf("margins %v", ps.Margin)
	}
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Errorf("at the limit: warnings %+v", w)
	}
	ps, _ = pageOf(t, "", "@page { size: 10in 10in; margin: 4.6in }")
	if !nearPx(ps.Margin[Left]+ps.Margin[Right], 9*96) {
		t.Errorf("just over: margins %v", ps.Margin)
	}
}

func TestPageOrigins(t *testing.T) {
	ps, _ := pageOf(t, "@page { size: A3; margin: 0 !important }", "@page { size: A5 }")
	if !nearPx(ps.Width, 148*mmPx) {
		t.Errorf("ExtraCSS must win: %v", ps.Width)
	}
	if ps.Margin[Top] != 0 {
		t.Errorf("document margin: %v", ps.Margin)
	}
	ps, _ = pageOf(t, "@page { margin: 1in } @page { margin: 2in }", "")
	if !nearPx(ps.Margin[Top], 192) {
		t.Errorf("later @page wins: %v", ps.Margin)
	}
}

func TestPageMediaScopedRules(t *testing.T) {
	ps, _ := pageOf(t, "@media screen { @page { size: A5 } } @media print { @page { size: A3 } }", "")
	if !nearPx(ps.Width, 297*mmPx) {
		t.Errorf("width %v", ps.Width)
	}
}

func TestMarginBoxes(t *testing.T) {
	ps, b := pageOf(t, "", `@page {
		@bottom-left { content: "TLP:AMBER"; background: #000; color: #FFC000; font-size: 10px }
		@bottom-right { content: "Page " counter(page) " of " counter(pages, decimal); font-size: 10px }
		@top-center { content: none }
		@top-left { content: "" }
		@bottom-center { content: normal }
	}`)
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Fatalf("warnings %+v", w)
	}
	bl := ps.Boxes[MarginBottomLeft]
	if bl == nil || len(bl.Content) != 1 || bl.Content[0] != (ContentItem{Kind: ContentText, Text: "TLP:AMBER"}) {
		t.Fatalf("bottom-left: %+v", bl)
	}
	if bl.Style.BackgroundColor != rgb(0, 0, 0) || bl.Style.Color != rgb(0xff, 0xc0, 0) || bl.Style.FontSize != 10 ||
		bl.Style.TextAlign != AlignLeft || bl.Style.VerticalAlign != VAlignMiddle {
		t.Errorf("bottom-left style: %+v", bl.Style)
	}
	br := ps.Boxes[MarginBottomRight]
	want := []ContentItem{{ContentText, "Page "}, {ContentPage, ""}, {ContentText, " of "}, {ContentPages, ""}}
	if br == nil || len(br.Content) != len(want) {
		t.Fatalf("bottom-right: %+v", br)
	}
	for i := range want {
		if br.Content[i] != want[i] {
			t.Errorf("item %d = %+v", i, br.Content[i])
		}
	}
	if br.Style.TextAlign != AlignRight {
		t.Errorf("bottom-right text-align: %v", br.Style.TextAlign)
	}
	if ps.Boxes[MarginTopCenter] != nil || ps.Boxes[MarginBottomCenter] != nil {
		t.Error("content none and normal generate no box")
	}
	if tl := ps.Boxes[MarginTopLeft]; tl == nil || len(tl.Content) != 1 || tl.Content[0].Text != "" {
		t.Errorf("empty string content generates a box: %+v", tl)
	}
}

func TestMarginBoxContentIsSanitized(t *testing.T) {
	ps, _ := pageOf(t, "", "@page { @bottom-left { content: \"a\\202E  b\\200B  c\\1F468\\200D\\1F4BB\" } }")
	got := ps.Boxes[MarginBottomLeft].Content[0].Text
	if want := "a\u27E8RLO\u27E9 b c\U0001F468\u200D\U0001F4BB"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestMarginBoxWarnings(t *testing.T) {
	b := newBudget()
	ss, err := css.ParseStylesheet([]byte(`@page {
		@top-left-corner { content: "x" } @left-top { content: "y" }
		@top-left { content: attr(title); color: red; border-bottom: 1px solid; content: "ok" }
		@top-right { content: counter(chapter) }
		@bottom-center { content: counter(page, upper-roman) }
	}`), css.OriginExtra, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(ss, b); err != nil {
		t.Fatal(err)
	}
	w, _ := b.Warnings()
	got := map[string]bool{}
	for _, v := range w {
		got[string(v.Code)+": "+v.Detail] = true
	}
	for _, want := range []string{
		"unsupported-at-rule: @top-left-corner", "unsupported-at-rule: @left-top", "invalid-value: content: attr(title)",
		"invalid-value: content: counter(chapter)", "invalid-value: content: counter(page, upper-roman)",
	} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
}

func TestPageSelectorsAreWarned(t *testing.T) {
	b := newBudget()
	ss, err := css.ParseStylesheet([]byte(`@page :first { size: A3 } @page cover { size: A3 } @page { size: A5 }`), css.OriginExtra, b)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(ss, b)
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != budget.WarnUnsupportedAtRule || w[0].Count != 2 {
		t.Errorf("warnings %+v", w)
	}
	ps := NewResolver(ua(), s, nil, printEnv, b).Page(PageContext{})
	if !nearPx(ps.Width, 148*mmPx) {
		t.Errorf("width %v", ps.Width)
	}
}

func TestHostConnectorPageCSS(t *testing.T) {
	ps, b := pageOf(t, "", testcorpus.HostConnectorPageCSS)
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Fatalf("the reference ExtraCSS must compile strictly: %+v", w)
	}
	want := [4]float64{96, 0.3 * 96, 1.3 * 96, 0.3 * 96}
	for i := range want {
		if !nearPx(ps.Margin[i], want[i]) {
			t.Errorf("margin %d = %v, want %v", i, ps.Margin[i], want[i])
		}
	}
	if bl := ps.Boxes[MarginBottomLeft]; bl == nil || bl.Content[0].Text != "TLP:AMBER" || bl.Style.BackgroundColor != rgb(0, 0, 0) {
		t.Errorf("bottom-left: %+v", bl)
	}
	if br := ps.Boxes[MarginBottomRight]; br == nil || len(br.Content) != 4 {
		t.Errorf("bottom-right: %+v", br)
	}
}

func TestPageIsComputedOnce(t *testing.T) {
	b := newBudget()
	r := NewResolver(ua(), nil, nil, printEnv, b)
	if r.Page(PageContext{Number: 1}) != r.Page(PageContext{Number: 7}) {
		t.Error("M1 pages share one style")
	}
}
