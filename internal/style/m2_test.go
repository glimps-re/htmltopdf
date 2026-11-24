package style

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func computed(t *testing.T, decl string) (*Computed, *doc) {
	t.Helper()
	x := resolve(t, `<p>x</p>`, `p { `+decl+` }`, "")
	return x.by(t, "p"), x
}

func TestKeywordProperties(t *testing.T) {
	tests := []struct {
		decl  string
		check func(*Computed) bool
	}{
		{"display: inline-block", func(c *Computed) bool { return c.Display == DisplayInlineBlock }},
		{"display: flex", func(c *Computed) bool { return c.Display == DisplayFlex }},
		{"display: grid", func(c *Computed) bool { return c.Display == DisplayGrid }},
		{"white-space: nowrap", func(c *Computed) bool { return c.WhiteSpace == WhiteSpaceNowrap }},
		{"text-transform: uppercase", func(c *Computed) bool { return c.TextTransform == TransformUppercase }},
		{"text-transform: lowercase", func(c *Computed) bool { return c.TextTransform == TransformLowercase }},
		{"text-transform: none", func(c *Computed) bool { return c.TextTransform == TransformNone }},
		{"font-variant-numeric: tabular-nums", func(c *Computed) bool { return c.TabularNums }},
		{"font-variant-numeric: normal", func(c *Computed) bool { return !c.TabularNums }},
		{"break-before: page", func(c *Computed) bool { return c.BreakBefore == BreakPage }},
		{"break-before: left", func(c *Computed) bool { return c.BreakBefore == BreakPage }},
		{"break-before: recto", func(c *Computed) bool { return c.BreakBefore == BreakPage }},
		{"break-before: verso", func(c *Computed) bool { return c.BreakBefore == BreakPage }},
		{"break-before: avoid", func(c *Computed) bool { return c.BreakBefore == BreakAvoid }},
		{"break-before: avoid-page", func(c *Computed) bool { return c.BreakBefore == BreakAvoid }},
		{"break-before: auto", func(c *Computed) bool { return c.BreakBefore == BreakAuto }},
		{"break-after: page", func(c *Computed) bool { return c.BreakAfter == BreakPage }},
		{"break-after: avoid", func(c *Computed) bool { return c.BreakAfter == BreakAvoid }},
		{"break-inside: avoid", func(c *Computed) bool { return c.BreakInside == BreakAvoid }},
		{"break-inside: avoid-page", func(c *Computed) bool { return c.BreakInside == BreakAvoid }},
		{"break-inside: auto", func(c *Computed) bool { return c.BreakInside == BreakAuto }},
		{"flex-direction: row", func(*Computed) bool { return true }},
		{"flex-wrap: wrap", func(c *Computed) bool { return c.FlexWrap }},
		{"flex-wrap: nowrap", func(c *Computed) bool { return !c.FlexWrap }},
		{"justify-content: normal", func(c *Computed) bool { return c.JustifyContent == JustifyStart }},
		{"justify-content: flex-start", func(c *Computed) bool { return c.JustifyContent == JustifyStart }},
		{"justify-content: left", func(c *Computed) bool { return c.JustifyContent == JustifyStart }},
		{"justify-content: end", func(c *Computed) bool { return c.JustifyContent == JustifyEnd }},
		{"justify-content: right", func(c *Computed) bool { return c.JustifyContent == JustifyEnd }},
		{"justify-content: center", func(c *Computed) bool { return c.JustifyContent == JustifyCenter }},
		{"justify-content: space-between", func(c *Computed) bool { return c.JustifyContent == JustifySpaceBetween }},
		{"justify-content: space-around", func(c *Computed) bool { return c.JustifyContent == JustifySpaceAround }},
		{"justify-content: space-evenly", func(c *Computed) bool { return c.JustifyContent == JustifySpaceEvenly }},
		{"align-items: normal", func(c *Computed) bool { return c.AlignItems == ItemStretch }},
		{"align-items: stretch", func(c *Computed) bool { return c.AlignItems == ItemStretch }},
		{"align-items: self-start", func(c *Computed) bool { return c.AlignItems == ItemStart }},
		{"align-items: flex-end", func(c *Computed) bool { return c.AlignItems == ItemEnd }},
		{"align-items: center", func(c *Computed) bool { return c.AlignItems == ItemCenter }},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, tt.decl)
			if !tt.check(c) {
				t.Errorf("not applied: %+v", c)
			}
			if w := x.warnings(); len(w) != 0 {
				t.Errorf("warnings: %v", w)
			}
		})
	}
}

func TestInvalidKeywords(t *testing.T) {
	for _, tt := range []struct{ decl, prop string }{
		{"display: inline-flex", "display"},
		{"display: inline-grid", "display"},
		{"white-space: pre-wrap", "white-space"},
		{"white-space: pre-line", "white-space"},
		{"text-transform: capitalize", "text-transform"},
		{"text-transform: full-width", "text-transform"},
		{"font-variant-numeric: oldstyle-nums", "font-variant-numeric"},
		{"font-variant-numeric: tabular-nums slashed-zero", "font-variant-numeric"},
		{"break-before: column", "break-before"},
		{"break-after: avoid-column", "break-after"},
		{"break-before: region", "break-before"},
		{"break-inside: page", "break-inside"},
		{"break-inside: avoid-column", "break-inside"},
		{"flex-direction: column", "flex-direction"},
		{"flex-direction: row-reverse", "flex-direction"},
		{"flex-wrap: wrap-reverse", "flex-wrap"},
		{"justify-content: stretch", "justify-content"},
		{"align-items: baseline", "align-items"},
		{"align-items: last baseline", "align-items"},
		{"flex-grow: -1", "flex-grow"},
		{"flex-shrink: x", "flex-shrink"},
		{"flex-basis: -1px", "flex-basis"},
		{"flex-basis: content", "flex-basis"},
		{"row-gap: -1px", "row-gap"},
		{"column-gap: auto", "column-gap"},
		{"gap: 1px 2px 3px", "gap"},
		{"opacity: x", "opacity"},
		{"opacity: 1px", "opacity"},
		{"flex: -1", "flex"},
		{"flex: 1 2 3", "flex"},
		{"flex: 1 1px 2", "flex"},
		{"flex: 1px 2px", "flex"},
		{"flex: content", "flex"},
	} {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, tt.decl)
			ref, _ := computed(t, "")
			if c.Display != ref.Display || c.WhiteSpace != ref.WhiteSpace || c.FlexGrow != 0 || c.FlexShrink != 1 ||
				c.FlexBasis != (Dim{Kind: DimAuto}) || c.FlexWrap || c.AlignItems != ItemStretch || c.RowGap != (Dim{}) ||
				c.TextTransform != TransformNone || c.TabularNums || c.BreakBefore != BreakAuto || c.BreakInside != BreakAuto || c.fade != 0 {
				t.Errorf("an invalid declaration applied: %+v", c)
			}
			if !x.has("invalid-value: " + tt.prop + ": ") {
				t.Errorf("no invalid-value warning: %v", x.warnings())
			}
		})
	}
}

func TestInheritanceOfM2Properties(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div>`, `div { text-transform: uppercase; white-space: nowrap; font-variant-numeric: tabular-nums;
		break-before: page; break-after: avoid; break-inside: avoid; flex-wrap: wrap; justify-content: center; align-items: center;
		flex: 2 3 10px; gap: 4px; grid-template-columns: 1fr 2fr; opacity: .5 }
		p { }`, "")
	p := x.by(t, "p")
	if p.TextTransform != TransformUppercase || p.WhiteSpace != WhiteSpaceNowrap || !p.TabularNums {
		t.Errorf("inherited properties: %+v", p)
	}
	if p.BreakBefore != BreakAuto || p.BreakAfter != BreakAuto || p.BreakInside != BreakAuto || p.FlexWrap || p.JustifyContent != JustifyStart ||
		p.AlignItems != ItemStretch || p.FlexGrow != 0 || p.FlexShrink != 1 || p.FlexBasis.Kind != DimAuto || p.RowGap != (Dim{}) || p.GridColumns != nil {
		t.Errorf("non-inherited properties leaked: %+v", p)
	}
	for _, tt := range []struct {
		decl string
		ok   func(*Computed) bool
	}{
		{"text-transform: inherit", func(c *Computed) bool { return c.TextTransform == TransformUppercase }},
		{"break-before: inherit", func(c *Computed) bool { return c.BreakBefore == BreakPage }},
		{"flex-wrap: inherit", func(c *Computed) bool { return c.FlexWrap }},
		{"gap: inherit", func(c *Computed) bool { return c.RowGap.Value == 4 && c.ColumnGap.Value == 4 }},
		{"flex: inherit", func(c *Computed) bool { return c.FlexGrow == 2 && c.FlexShrink == 3 && c.FlexBasis.Value == 10 }},
		{"grid-template-columns: inherit", func(c *Computed) bool { return len(c.GridColumns) == 2 }},
		{"text-transform: initial", func(c *Computed) bool { return c.TextTransform == TransformNone }},
		{"white-space: unset", func(c *Computed) bool { return c.WhiteSpace == WhiteSpaceNowrap }},
		{"break-before: unset", func(c *Computed) bool { return c.BreakBefore == BreakAuto }},
		{"flex: initial", func(c *Computed) bool { return c.FlexGrow == 0 && c.FlexShrink == 1 && c.FlexBasis.Kind == DimAuto }},
		{"opacity: inherit", func(c *Computed) bool { return nearPx(1-c.fade, .25) }},
	} {
		y := resolve(t, `<div><p>x</p></div>`, `div { text-transform: uppercase; white-space: nowrap; break-before: page; flex-wrap: wrap; gap: 4px;
			flex: 2 3 10px; grid-template-columns: 1fr 2fr; opacity: .5 } p { `+tt.decl+` }`, "")
		if c := y.by(t, "p"); !tt.ok(c) {
			t.Errorf("%s: %+v", tt.decl, c)
		}
	}
}

func TestFlexProperties(t *testing.T) {
	px := func(v float64) Dim { return Dim{Kind: DimPx, Value: v} }
	tests := []struct {
		decl                string
		grow, shrink, basis any
	}{
		{"flex: none", 0.0, 0.0, Dim{Kind: DimAuto}},
		{"flex: auto", 1.0, 1.0, Dim{Kind: DimAuto}},
		{"flex: 1", 1.0, 1.0, Dim{Kind: DimPercent}},
		{"flex: 0", 0.0, 1.0, Dim{Kind: DimPercent}},
		{"flex: 2 3", 2.0, 3.0, Dim{Kind: DimPercent}},
		{"flex: 2 3 10px", 2.0, 3.0, px(10)},
		{"flex: 0 0 25%", 0.0, 0.0, Dim{Kind: DimPercent, Value: 25}},
		{"flex: 10px", 1.0, 1.0, px(10)},
		{"flex: 1 1 0", 1.0, 1.0, px(0)},
		{"flex: 10px 2", 2.0, 1.0, px(10)},
		{"flex: 1 auto", 1.0, 1.0, Dim{Kind: DimAuto}},
		{"flex: auto 3", 3.0, 1.0, Dim{Kind: DimAuto}},
		{"flex: 1.5 0.5 2em", 1.5, 0.5, px(32)},
		{"flex-grow: 3", 3.0, 1.0, Dim{Kind: DimAuto}},
		{"flex-shrink: 0", 0.0, 0.0, Dim{Kind: DimAuto}},
		{"flex-grow: 1e9", MaxFlexFactor, 1.0, Dim{Kind: DimAuto}},
		{"flex-basis: 30%", 0.0, 1.0, Dim{Kind: DimPercent, Value: 30}},
		{"flex-basis: auto", 0.0, 1.0, Dim{Kind: DimAuto}},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, tt.decl)
			if c.FlexGrow != tt.grow || (tt.decl != "flex-shrink: 0" && c.FlexShrink != tt.shrink) || c.FlexBasis != tt.basis {
				t.Errorf("grow %v shrink %v basis %+v", c.FlexGrow, c.FlexShrink, c.FlexBasis)
			}
			if tt.decl == "flex-grow: 1e9" != x.has("clamped: flex-grow") {
				t.Errorf("warnings: %v", x.warnings())
			}
		})
	}
	c, _ := computed(t, "flex-shrink: 0")
	if c.FlexShrink != 0 {
		t.Errorf("flex-shrink: 0 = %v", c.FlexShrink)
	}
}

func TestGap(t *testing.T) {
	for _, tt := range []struct {
		decl     string
		row, col Dim
	}{
		{"gap: 4px", Dim{Value: 4}, Dim{Value: 4}},
		{"gap: 4px 8px", Dim{Value: 4}, Dim{Value: 8}},
		{"gap: normal", Dim{}, Dim{}},
		{"gap: 1em 10%", Dim{Value: 16}, Dim{Kind: DimPercent, Value: 10}},
		{"row-gap: 3px", Dim{Value: 3}, Dim{}},
		{"column-gap: 5px", Dim{}, Dim{Value: 5}},
		{"column-gap: normal", Dim{}, Dim{}},
	} {
		t.Run(tt.decl, func(t *testing.T) {
			c, _ := computed(t, tt.decl)
			if c.RowGap != tt.row || c.ColumnGap != tt.col {
				t.Errorf("row %+v col %+v", c.RowGap, c.ColumnGap)
			}
		})
	}
}

func TestOpacity(t *testing.T) {
	t.Run("clamped and parsed", func(t *testing.T) {
		for decl, want := range map[string]float64{"opacity: .5": .5, "opacity: 50%": .5, "opacity: 2": 1, "opacity: -1": 0, "opacity: 0": 0, "opacity: 150%": 1} {
			c, x := computed(t, "color: #fff; "+decl)
			if !nearPx(c.Color.A, want) || len(x.warnings()) != 0 {
				t.Errorf("%s: alpha %v, warnings %v", decl, c.Color.A, x.warnings())
			}
		}
	})
	t.Run("product of ancestors", func(t *testing.T) {
		x := resolve(t, `<div><p>x<span>y</span></p></div>`, `div { opacity: .5; background: #fff } p { opacity: .5; border: 1px solid; background: #00f; color: #f00 }`, "")
		div, p, span := x.by(t, "div"), x.by(t, "p"), x.by(t, "span")
		if !nearPx(div.BackgroundColor.A, .5) || !nearPx(p.Color.A, .25) || !nearPx(p.BackgroundColor.A, .25) || !nearPx(span.Color.A, .25) {
			t.Errorf("div %+v p %+v span %+v", div.BackgroundColor, p.Color, span.Color)
		}
		if p.Color.R != 1 || span.Color.R != 1 {
			t.Errorf("the colour channels must not change: %+v %+v", p.Color, span.Color)
		}
		if !nearPx(p.Border[Top].Color.A, .25) {
			t.Errorf("currentColor border multiplied once: %+v", p.Border[Top].Color)
		}
	})
	t.Run("not inherited by value", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { opacity: .5; color: #000 } p { opacity: 1 }`, "")
		if p := x.by(t, "p"); !nearPx(p.Color.A, .5) {
			t.Errorf("p opacity 1 under .5: %+v", p.Color)
		}
	})
	t.Run("zero", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { opacity: 0; background: red }`, "")
		if x.by(t, "div").BackgroundColor.A != 0 || x.by(t, "p").Color.A != 0 {
			t.Error("opacity 0 must give alpha 0")
		}
	})
	t.Run("inherited colour is not multiplied twice", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { opacity: .5; color: #000 } p { border: 1px solid; background-color: inherit }`, "")
		p := x.by(t, "p")
		if !nearPx(p.Color.A, .5) || !nearPx(p.Border[Top].Color.A, .5) {
			t.Errorf("p: %+v %+v", p.Color, p.Border[Top].Color)
		}
	})
	t.Run("translucent colours multiply", func(t *testing.T) {
		c, _ := computed(t, "opacity: .5; background: rgba(255, 0, 0, .5)")
		if !nearPx(c.BackgroundColor.A, .25) {
			t.Errorf("%+v", c.BackgroundColor)
		}
	})
	t.Run("gradient warns once", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { opacity: .5; background: linear-gradient(red, blue) }`, "")
		w, _ := x.b.Warnings()
		n := 0
		for _, v := range w {
			if v.Detail == "opacity on gradient" {
				n++
				if v.Code != "unsupported-property" {
					t.Errorf("code %v", v.Code)
				}
			}
		}
		if n != 1 {
			t.Errorf("%d warnings: %v", n, x.warnings())
		}
		if x.by(t, "div").BackgroundImage == nil {
			t.Error("the gradient is kept")
		}
	})
}

func TestBlockification(t *testing.T) {
	for _, disp := range []string{"flex", "grid"} {
		x := resolve(t, `<div id=c><span id=s>a<b id=b>b</b></span><i id=ib>i</i><li id=li>l</li><table id=t><tr><td id=td>x</table></div>`,
			`#c { display: `+disp+` } #ib { display: inline-block } #ib b, #s b { font-size: 1em }`, "")
		if c := x.by(t, "#s"); c.Display != DisplayBlock {
			t.Errorf("%s: span = %v", disp, c.Display)
		}
		if c := x.by(t, "#ib"); c.Display != DisplayBlock {
			t.Errorf("%s: inline-block = %v", disp, c.Display)
		}
		if c := x.by(t, "#b"); c.Display != DisplayInline {
			t.Errorf("%s: a grandchild stays inline: %v", disp, c.Display)
		}
		if c := x.by(t, "#li"); c.Display != DisplayListItem {
			t.Errorf("%s: list-item kept: %v", disp, c.Display)
		}
		if c := x.by(t, "#t"); c.Display != DisplayTable {
			t.Errorf("%s: table kept: %v", disp, c.Display)
		}
		if c := x.by(t, "#td"); c.Display != DisplayTableCell {
			t.Errorf("%s: table parts kept: %v", disp, c.Display)
		}
		if c := x.by(t, "#c"); c.Display != map[string]Display{"flex": DisplayFlex, "grid": DisplayGrid}[disp] {
			t.Errorf("container: %v", c.Display)
		}
	}
	x := resolve(t, `<div><span>a</span></div>`, `div { display: inline-block } span { display: inline }`, "")
	if x.by(t, "span").Display != DisplayInline {
		t.Error("an inline-block parent does not blockify")
	}
}

func TestGridColumns(t *testing.T) {
	px := func(v float64) Track { return Track{Kind: TrackPx, Value: v} }
	fr := func(v float64) Track { return Track{Kind: TrackFr, Value: v} }
	tests := []struct {
		decl string
		want []Track
	}{
		{"none", nil},
		{"1fr", []Track{fr(1)}},
		{"repeat(4, 1fr)", []Track{fr(1), fr(1), fr(1), fr(1)}},
		{"max-content 1fr", []Track{{Kind: TrackMaxContent}, fr(1)}},
		{"min-content auto 20px 25% 2.5fr 1em", []Track{{Kind: TrackMinContent}, {Kind: TrackAuto}, px(20), {Kind: TrackPercent, Value: 25}, fr(2.5), px(16)}},
		{"100px repeat(2, 1fr 50px) auto", []Track{px(100), fr(1), px(50), fr(1), px(50), {Kind: TrackAuto}}},
		{"REPEAT(2, AUTO)", []Track{{Kind: TrackAuto}, {Kind: TrackAuto}}},
		{"0fr 0", []Track{fr(0), px(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, "grid-template-columns: "+tt.decl)
			if len(c.GridColumns) != len(tt.want) {
				t.Fatalf("%+v", c.GridColumns)
			}
			for i := range tt.want {
				if c.GridColumns[i] != tt.want[i] {
					t.Errorf("track %d = %+v, want %+v", i, c.GridColumns[i], tt.want[i])
				}
			}
			if len(x.warnings()) != 0 {
				t.Errorf("warnings: %v", x.warnings())
			}
		})
	}
	for _, decl := range []string{
		"minmax(10px, 1fr)", "repeat(auto-fill, 100px)", "repeat(auto-fit, 1fr)", "[a] 1fr", "1fr [b]", "repeat(0, 1fr)", "repeat(-1, 1fr)",
		"repeat(1.5, 1fr)", "repeat(2.0, 1fr)", "repeat(1.0, 1fr)", "repeat(2e0, 1fr)", "repeat(1e3, 1fr)", "repeat(+2, 1fr)x", "repeat(2)", "repeat(2, )", "repeat(2, repeat(2, 1fr))", "-1fr", "-5px", "fit-content(10px)", "1fr, 1fr", "x",
		"repeat(2, minmax(1px, 2px))", "1fr foo",
	} {
		t.Run("invalid "+decl, func(t *testing.T) {
			c, x := computed(t, "grid-template-columns: "+decl)
			if c.GridColumns != nil {
				t.Errorf("applied: %+v", c.GridColumns)
			}
			if !x.has("invalid-value: grid-template-columns") {
				t.Errorf("warnings: %v", x.warnings())
			}
		})
	}
}

func TestGridTrackClamp(t *testing.T) {
	for _, tt := range []struct {
		value string
		n     int
		warn  bool
	}{
		{"repeat(1000, 1fr)", 1000, false},
		{"repeat(1001, 1fr)", 1000, true},
		{"repeat(100000, 1fr)", 1000, true},
		{"repeat(999, 1fr) 2fr 3fr", 1000, true},
		{"repeat(600, 1fr 2fr)", 1000, true},
		{"repeat(2000000000, 1fr 1fr 1fr)", 999, true},
		{"1fr repeat(999, 1fr)", 1000, false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			c, x := computed(t, "grid-template-columns: "+tt.value)
			if len(c.GridColumns) != tt.n || x.has("clamped: grid tracks") != tt.warn {
				t.Errorf("%d tracks, warnings %v", len(c.GridColumns), x.warnings())
			}
		})
	}
	tracks := "1fr" + strings.Repeat(" 1fr", 1500)
	c, x := computed(t, "grid-template-columns: "+tracks)
	if len(c.GridColumns) != MaxGridTracks || !x.has("clamped: grid tracks") {
		t.Errorf("a long list: %d tracks, %v", len(c.GridColumns), x.warnings())
	}
}

func TestGridRepeatAllocatesLittle(t *testing.T) {
	toks := lexValue(t, "repeat(100000, 1fr)")
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	for range 100 {
		if _, ok := parseGridColumns(toks); !ok {
			t.Fatal("invalid")
		}
	}
	runtime.ReadMemStats(&m1)
	if per := (m1.TotalAlloc - m0.TotalAlloc) / 100; per > 16<<10 {
		t.Errorf("%d bytes allocated per declaration", per)
	}
}

func lexValue(t testing.TB, v string) []css.Token {
	t.Helper()
	ds, err := css.ParseDeclarations([]byte("x:"+v), newBudget())
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %v", ds, err)
	}
	return ds[0].Value
}

func TestFontShorthand(t *testing.T) {
	serif := []string{"serif"}
	tests := []struct {
		value  string
		style  FontStyle
		weight int
		size   float64
		lh     LineHeight
		family []string
	}{
		{"12px serif", FontNormal, 400, 12, LineHeight{}, serif},
		{"italic 12px serif", FontItalic, 400, 12, LineHeight{}, serif},
		{"oblique 12px serif", FontOblique, 400, 12, LineHeight{}, serif},
		{"bold 12px serif", FontNormal, 700, 12, LineHeight{}, serif},
		{"700 12px serif", FontNormal, 700, 12, LineHeight{}, serif},
		{"600 10pt Foo", FontNormal, 600, 10 * 4 / 3.0, LineHeight{}, []string{"Foo"}},
		{"italic 700 7pt/4mm Georgia, serif", FontItalic, 700, 7 * 4 / 3.0, LineHeight{Kind: LineHeightPx, Value: 4 * 96 / 25.4}, []string{"Georgia", "serif"}},
		{"bold italic 12px/1.5 serif", FontItalic, 700, 12, LineHeight{Kind: LineHeightNumber, Value: 1.5}, serif},
		{"normal normal normal 12px serif", FontNormal, 400, 12, LineHeight{}, serif},
		{"normal bold 12px / normal serif", FontNormal, 700, 12, LineHeight{}, serif},
		{`10pt/1.55 "DejaVu Serif", Georgia, serif`, FontNormal, 400, 10 * 4 / 3.0, LineHeight{Kind: LineHeightNumber, Value: 1.55}, []string{"DejaVu Serif", "Georgia", "serif"}},
		{"12px/150% serif", FontNormal, 400, 12, LineHeight{Kind: LineHeightPx, Value: 18}, serif},
		{"medium serif", FontNormal, 400, 16, LineHeight{}, serif},
		{"1.5em serif", FontNormal, 400, 24, LineHeight{}, serif},
		{"lighter 1rem serif", FontNormal, 700, 16, LineHeight{}, serif},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			x := resolve(t, `<div><p>x</p></div>`, `div { font-weight: 900; line-height: 3; font-style: italic; font-variant-numeric: tabular-nums } p { font: `+tt.value+` }`, "")
			c := x.by(t, "p")
			if c.FontStyle != tt.style || c.FontWeight != tt.weight || !nearPx(c.FontSize, tt.size) || c.LineHeight.Kind != tt.lh.Kind ||
				!nearPx(c.LineHeight.Value, tt.lh.Value) || strings.Join(c.FontFamily, ",") != strings.Join(tt.family, ",") || c.TabularNums {
				t.Errorf("%+v", c)
			}
		})
	}
}

func TestFontShorthandInvalid(t *testing.T) {
	for _, v := range []string{
		"caption", "icon", "menu", "message-box", "small-caption", "status-bar", "small-caps 12px serif", "condensed 12px serif",
		"expanded 12px serif", "ultra-condensed 12px serif", "semi-expanded 12px serif", "12px", "bold", "bold 12px", "serif 12px",
		"large 12px/2 serif", "12px/ serif", "12px/x serif", "12px/-1 serif", "12px/1.5", "bold bold 12px serif", "italic italic 12px serif",
		"italic bold normal normal 12px serif", "1001 12px serif", "-12px serif", "12px 12px serif",
		"12px / 1 / 2 serif", "12 serif", "italic 12px", "12px/1.5,serif",
	} {
		t.Run(v, func(t *testing.T) {
			x := resolve(t, `<p>x</p>`, `p { font-size: 20px; font-weight: 600; font: `+v+` }`, "")
			c := x.by(t, "p")
			if c.FontSize != 20 || c.FontWeight != 600 {
				t.Errorf("applied: %+v", c)
			}
			if !x.has("invalid-value: font") {
				t.Errorf("no warning: %v", x.warnings())
			}
		})
	}
}

func TestFontShorthandBeatsEarlierLonghands(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { line-height: 3 } p { font: 12px serif }`, "")
	if c := x.by(t, "p"); c.LineHeight.Kind != LineHeightNormal {
		t.Errorf("line-height after font: %+v", c.LineHeight)
	}
	x = resolve(t, `<p>x</p>`, `p { font: 12px serif; line-height: 2; font-weight: bold }`, "")
	if c := x.by(t, "p"); c.LineHeight.Value != 2 || c.FontWeight != 700 || c.FontSize != 12 {
		t.Errorf("longhands after font: %+v", c)
	}
}

func TestFontShorthandWithVar(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `:root { --f: 10px/2 Foo } p { font: bold var(--f) } `, "")
	c := x.by(t, "p")
	if c.FontWeight != 700 || c.FontSize != 10 || c.LineHeight.Value != 2 || c.FontFamily[0] != "Foo" {
		t.Errorf("%+v", c)
	}
	x = resolve(t, `<p>x</p>`, `p { font-weight: 600; font: var(--missing) }`, "")
	if x.by(t, "p").FontWeight == 700 {
		t.Error("unexpected")
	}
}

func TestFontInMarginBox(t *testing.T) {
	ps, b := pageOf(t, "", `@page { @bottom-left { content: "x"; font: italic 700 8pt/1.2 "DejaVu Sans", Arial, sans-serif; text-transform: uppercase }
		@bottom-right { content: "y"; font: caption } }`)
	bl := ps.Boxes[MarginBottomLeft]
	if bl == nil || bl.Style.FontStyle != FontItalic || bl.Style.FontWeight != 700 || !nearPx(bl.Style.FontSize, 8*4/3.0) ||
		!nearPx(bl.Style.LineHeight.Value, 1.2) || bl.Style.FontFamily[0] != "DejaVu Sans" || bl.Style.TextTransform != TransformUppercase {
		t.Errorf("%+v", bl)
	}
	if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != "invalid-value" {
		t.Errorf("warnings %+v", w)
	}
}

var templateFont = regexp.MustCompile(`[;{ \t]font:([^;}]*)`)

func TestTemplateFontValues(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range []string{"01-modern", "02-executive", "03-analyst"} {
		data, err := os.ReadFile(filepath.Clean(templatesDir + "/" + name + ".html.tmpl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range templateFont.FindAllSubmatch(data, -1) {
			seen[strings.TrimSpace(string(m[1]))] = true
		}
	}
	if len(seen) != 27 {
		t.Fatalf("%d distinct font values, want 27", len(seen))
	}
	for v := range seen {
		x := resolve(t, `<p>x</p>`, `p { font: `+v+` }`, "")
		c := x.by(t, "p")
		if len(x.warnings()) != 0 {
			t.Errorf("%q: %v", v, x.warnings())
			continue
		}
		want := parseFontByHand(t, v)
		if !nearPx(c.FontSize, want.size) || c.FontWeight != want.weight || c.FontStyle != want.style || c.LineHeight.Kind != want.lh.Kind || !nearPx(c.LineHeight.Value, want.lh.Value) ||
			strings.Join(c.FontFamily, "|") != want.family {
			t.Errorf("%q: got %v/%d/%v/%+v/%v, want %+v", v, c.FontSize, c.FontWeight, c.FontStyle, c.LineHeight, c.FontFamily, want)
		}
	}
}

type fontWant struct {
	size   float64
	weight int
	style  FontStyle
	lh     LineHeight
	family string
}

// parseFontByHand is an independent reading of the template values: tokens
// before the size are style and weight, the size is in pt, an optional line
// height follows after a slash, the rest is the family.
func parseFontByHand(t *testing.T, v string) fontWant {
	t.Helper()
	w := fontWant{weight: 400}
	rest := v
	for prefix := true; prefix; {
		word, tail, _ := strings.Cut(rest, " ")
		switch word {
		case "italic":
			w.style = FontItalic
		case "bold":
			w.weight = 700
		case "400", "600", "700":
			w.weight = int(word[0]-'0')*100 + int(word[1]-'0')*10
		default:
			prefix = false
			continue
		}
		rest = tail
	}
	sizeTok, family, _ := strings.Cut(rest, " ")
	sz, lh, hasLH := strings.Cut(sizeTok, "/")
	w.size = atof(t, strings.TrimSuffix(sz, "pt")) * 4 / 3
	if hasLH {
		if strings.HasSuffix(lh, "mm") {
			w.lh = LineHeight{Kind: LineHeightPx, Value: atof(t, strings.TrimSuffix(lh, "mm")) * 96 / 25.4}
		} else {
			w.lh = LineHeight{Kind: LineHeightNumber, Value: atof(t, lh)}
		}
	}
	var fams []string
	for _, f := range strings.Split(family, ",") {
		fams = append(fams, strings.Trim(strings.TrimSpace(f), `"`))
	}
	w.family = strings.Join(fams, "|")
	return w
}

func TestCorpusM2Styles(t *testing.T) {
	for _, name := range []string{"01-modern", "02-executive", "03-analyst"} {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(name+"/"+string(size), func(t *testing.T) {
				_, d := loadReport(t, name, size)
				sheets, extra := compileDoc(t, d)
				b := newBudget()
				styles := cascade(t, d, sheets, extra, b)
				if n := distinct(styles); name == "01-modern" && size == testcorpus.Large && n > 128 {
					t.Errorf("%d distinct styles, want at most 128", n)
				}
				ws, _ := b.Warnings()
				for _, w := range ws {
					if w.Code == "invalid-value" && strings.HasPrefix(w.Detail, "font") {
						t.Errorf("%+v", w)
					}
				}
			})
		}
	}
}

func BenchmarkCascade01Large(b *testing.B) {
	_, d := loadReport(b, "01-modern", testcorpus.Large)
	sheets, extra := compileDoc(b, d)
	b.ReportAllocs()
	for b.Loop() {
		cascade(b, d, sheets, extra, newBudget())
	}
}

func atof(t testing.TB, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestGridColumnsAreSharedBetweenStyles(t *testing.T) {
	const n = 3000
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<i style="width:%dpx">x</i>`, i+1)
	}
	x := resolve(t, "<p>"+sb.String()+"</p>", `i { grid-template-columns: repeat(1000, 1px) }`, "")
	var first *Track
	styles := map[*Computed]bool{}
	for i := range x.d.Elements {
		if x.d.Elements[i].Tag.String() != "i" {
			continue
		}
		c := x.styles[i]
		styles[c] = true
		if len(c.GridColumns) != 1000 {
			t.Fatalf("%d tracks", len(c.GridColumns))
		}
		if first == nil {
			first = &c.GridColumns[0]
		} else if first != &c.GridColumns[0] {
			t.Fatal("two styles expanded the same declaration twice")
		}
	}
	if len(styles) != n {
		t.Fatalf("%d distinct styles, want %d", len(styles), n)
	}
}

func TestGridColumnsOfDistinctFontSizesAreBounded(t *testing.T) {
	var sb strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&sb, `<i style="font-size:%dpx">x</i>`, 8+i%1000)
	}
	// 1000 distinct font sizes, each its own 1000-track expansion of 16 KB.
	html := "<p>" + sb.String() + "</p>"
	b := newBudget()
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, `i { grid-template-columns: repeat(1000, 1em) }`, css.OriginDocument, b)}, printEnv, b)
	styles := make([]*Computed, len(d.Elements))
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		c, err := r.Element(d, int32(i), parent)
		if err != nil {
			t.Fatal(err)
		}
		styles[i] = c
	}
	runtime.ReadMemStats(&m1)
	if used := b.Used(budget.Styles); used < 1000*16 {
		t.Errorf("only %d Styles units charged for 1000 expansions", used)
	}
	if grown := m1.TotalAlloc - m0.TotalAlloc; grown > 40<<20 {
		t.Errorf("%d bytes allocated", grown)
	}
	// A hostile document with 20000 distinct sizes hits the limit instead of
	// allocating 320 MB.
	var big strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&big, `<i style="font-size:%dpx">x</i>`, 8+i%9000)
	}
	b = newBudget()
	d, err = dom.Parse(strings.NewReader("<p>"+big.String()+"</p>"), b)
	if err != nil {
		t.Fatal(err)
	}
	r = NewResolver(ua(), nil, []*Sheet{compileSheet(t, `i { grid-template-columns: repeat(1000, 1em) }`, css.OriginDocument, b)}, printEnv, b)
	styles = make([]*Computed, len(d.Elements))
	var hit error
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		if styles[i], hit = r.Element(d, int32(i), parent); hit != nil {
			break
		}
	}
	var le *budget.LimitError
	if !errors.As(hit, &le) || le.Kind != budget.Styles {
		t.Errorf("got %v, want the Styles limit", hit)
	}
}
