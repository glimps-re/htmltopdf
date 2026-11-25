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
	for _, d := range append([]Dim{c.FlexBasis, c.RowGap, c.ColumnGap, c.Height, c.MinWidth, c.VAlignOffset}, c.Inset[:]...) {
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
	if c.ColumnCount < 0 || c.ColumnCount > MaxColumns || c.ColumnRule.Width < 0 || !finite(c.ColumnRule.Width) ||
		!finite(c.TextDecorationThickness.Value) || c.TextDecorationThickness.Value < 0 ||
		((c.ColumnRule.Style == BorderNone || c.ColumnRule.Style == BorderHidden) && c.ColumnRule.Width != 0) ||
		c.Border[Top].Style > BorderDotted || c.Position > PositionFixed {
		t.Fatalf("bad M4 values %+v", c)
	}
	if c.Rotate < 0 || c.Rotate >= 2*math.Pi || len(c.CounterReset) > MaxCounterOps || len(c.CounterIncrement) > MaxCounterOps ||
		len(c.Content) > maxContentItems || len(c.Page) > MaxIdentBytes ||
		(c.Position == PositionAbsolute && c.Display != DisplayBlock && c.Display != DisplayFlex && c.Display != DisplayGrid &&
			c.Display != DisplayListItem && c.Display != DisplayTable && c.Display != DisplayNone &&
			c.Display != DisplayTableColumn && c.Display != DisplayTableColumnGroup) {
		t.Fatalf("bad M3 values %+v", c)
	}
	for _, op := range append(append([]CounterOp(nil), c.CounterReset...), c.CounterIncrement...) {
		if len(op.Name) > MaxIdentBytes || op.Name == "" {
			t.Fatalf("bad counter %+v", op)
		}
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
		"div{box-shadow:0 1px 2px 3px rgba(0,0,0,.2),inset 1px 1px red,1em 1rem;opacity:.5;color:blue}",
		"div{box-shadow:" + strings.Repeat("1px 1px 1px red,", 200) + "1px 1px;--s:1px 2px;box-shadow:var(--s) green}",
		"div{box-shadow:inherit;box-shadow:none}p{box-shadow:-1px 1e30px 0 -1e30px currentcolor}",
		"p{position:fixed;display:inline;top:-1e30px;columns:1e30;column-count:1001;column-width:10px;column-gap:normal;gap:normal 1e9px}",
		"div{columns:2 auto;column-rule:2px dashed red;column-rule-color:inherit;opacity:.3} p{column-rule:1e30px dotted;column-rule-style:none;column-rule-width:-1px}",
		"p{text-decoration:underline;text-decoration-thickness:from-font;text-decoration-thickness:1e999em;text-decoration-thickness:-1px;border:1e30px dashed;border-style:dotted double}",
		"p::before{content:counter(c,decimal-leading-zero) \"  \";position:absolute;top:-1e30px;z-index:99999999999;counter-increment:c 2147483647 d}",
		"p::after{content:leader(\".\") target-counter(attr(href),page);content:\"\"} div{counter-reset:a 1e9 b -5 " + strings.Repeat("n ", 200) + "}",
		"p{height:50%;min-width:1e30px;vertical-align:-1e30px;transform:rotate(1e30turn);page:cover;align-items:baseline;position:fixed}",
		"div{position:absolute;display:table-cell;height:1e30mm;transform:rotate(-1e-30deg)} @page cover{margin:50in;size:1e9in;@top-left{content:\"x\"}}",
		"p{background:radial-gradient(circle farthest-corner at center,rgba(1,2,3,0),red 70%);opacity:.3} @page a b{} @page n:first{} @page n{margin:0}",
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
		for i := range d.Elements {
			for _, pe := range []css.PseudoElement{css.PseudoBefore, css.PseudoAfter} {
				c, err := r.Pseudo(d, int32(i), pe, styles[i])
				if err != nil {
					return
				}
				if c != nil {
					checkComputed(t, c)
					if len(c.Content) == 0 {
						t.Fatal("pseudo style without content")
					}
				}
			}
		}
		for _, name := range []string{"cover", "n", "x"} {
			if p := r.Page(PageContext{Number: 1, Name: name}); p.Width < MinPageSidePx || p.Height > MaxPageSidePx {
				t.Fatalf("named page size %v x %v", p.Width, p.Height)
			}
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
