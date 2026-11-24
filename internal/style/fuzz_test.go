package style

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func finite(v float64) bool { return !math.IsNaN(v) && math.Abs(v) <= css.MaxLengthPx }

func checkComputed(t *testing.T, c *Computed) {
	t.Helper()
	dims := append(append([]Dim{c.Width, c.MaxWidth}, c.Margin[:]...), c.Padding[:]...)
	for _, r := range c.Radius {
		dims = append(dims, r.H, r.V)
	}
	for _, d := range dims {
		if !finite(d.Value) {
			t.Fatalf("bad dimension %+v", d)
		}
	}
	for _, d := range []Dim{c.FlexBasis, c.RowGap, c.ColumnGap} {
		if !finite(d.Value) {
			t.Fatalf("bad dimension %+v", d)
		}
	}
	for _, tr := range c.GridColumns {
		if !finite(tr.Value) || tr.Kind > TrackMaxContent {
			t.Fatalf("bad track %+v", tr)
		}
	}
	if len(c.GridColumns) > MaxGridTracks || c.FlexGrow < 0 || c.FlexGrow > MaxFlexFactor || c.FlexShrink < 0 || c.FlexShrink > MaxFlexFactor ||
		c.fade < 0 || c.fade > 1 || c.Color.A < 0 || c.Color.A > 1 {
		t.Fatalf("bad M2 values %+v", c)
	}
	vals := []float64{c.FontSize, c.LetterSpacing, c.BorderSpacing[0], c.BorderSpacing[1], c.LineHeight.Value}
	for _, b := range c.Border {
		vals = append(vals, b.Width)
	}
	for _, v := range vals {
		if !finite(v) {
			t.Fatalf("bad value %v in %+v", v, c)
		}
	}
	if c.FontSize < 0 || c.FontSize > MaxFontSizePx || c.FontWeight < 1 || c.FontWeight > 1000 || len(c.FontFamily) == 0 || len(c.FontFamily) > MaxFontFamilies {
		t.Fatalf("bad font %+v", c)
	}
}

func FuzzStyle(f *testing.F) {
	for _, s := range []string{
		"p { color: red; margin: 1px 2px }", ":root{--a:var(--b)} p{color:var(--a, blue)}", "p{font-size:1e30em;margin:1e30px}",
		"p{--x:var(--x)} @media print{p{width:50%}} @page{size:A4 landscape;margin:1in;@top-left{content:\"x\"}}",
		"p{background:linear-gradient(red,blue);border:1px solid;border-radius:1px/2px;font-family:a,b,\"c d\"}",
		"p{font:italic 700 7pt/4mm Georgia,serif;font:var(--f);opacity:50%;text-transform:uppercase;white-space:nowrap}",
		"div{display:grid;grid-template-columns:repeat(100000,1fr 2fr) auto;gap:1px 2px;flex:1 1 0;flex:auto 3}",
		"div{display:flex;flex-wrap:wrap;justify-content:space-between;align-items:center;flex-grow:1e30;break-before:page}",
	} {
		f.Add(s, "color:red;--q:1")
	}
	f.Fuzz(func(t *testing.T, sheet, inline string) {
		if len(sheet) > 4096 || len(inline) > 512 {
			return
		}
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ss, err := css.ParseStylesheet([]byte(sheet), css.OriginDocument, b)
		if err != nil {
			return
		}
		s, err := Compile(ss, b)
		if err != nil {
			return
		}
		html := `<div id=a class="x y"><p style="` + strings.ReplaceAll(inline, `"`, "") + `">t</p><span>s</span></div>`
		d, err := dom.Parse(strings.NewReader(html), b)
		if err != nil {
			return
		}
		r := NewResolver(ua(), nil, []*Sheet{s}, printEnv, b)
		styles := make([]*Computed, len(d.Elements))
		for i := range d.Elements {
			var parent *Computed
			if p := d.Elements[i].Parent; p >= 0 {
				parent = styles[p]
			}
			c, err := r.Element(d, int32(i), parent)
			if err != nil {
				return
			}
			checkComputed(t, c)
			styles[i] = c
		}
		ps := r.Page(PageContext{Number: 1})
		if ps.Width < MinPageSidePx || ps.Width > MaxPageSidePx || ps.Height < MinPageSidePx || ps.Height > MaxPageSidePx {
			t.Fatalf("page size %v x %v", ps.Width, ps.Height)
		}
		for i := 0; i < 2; i++ {
			if ps.Width-ps.Margin[Left+i*0]-ps.Margin[Right] < MinPageAreaPx-1e-6 && i == 0 {
				t.Fatalf("margins %v leave less than 1 in of %v", ps.Margin, ps.Width)
			}
		}
		if ps.Height-ps.Margin[Top]-ps.Margin[Bottom] < MinPageAreaPx-1e-6 {
			t.Fatalf("margins %v leave less than 1 in of %v", ps.Margin, ps.Height)
		}
	})
}
