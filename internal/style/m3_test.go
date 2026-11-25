package style

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

func TestM3Properties(t *testing.T) {
	px := func(v float64) Dim { return Dim{Kind: DimPx, Value: v} }
	auto := Dim{Kind: DimAuto}
	tests := []struct {
		decl  string
		check func(*Computed) bool
	}{
		{"position: relative", func(c *Computed) bool { return c.Position == PositionRelative }},
		{"position: ABSOLUTE; display: inline", func(c *Computed) bool { return c.Position == PositionAbsolute && c.Display == DisplayBlock }},
		{"position: absolute; display: inline-block", func(c *Computed) bool { return c.Display == DisplayBlock }},
		{"position: absolute; display: table-cell", func(c *Computed) bool { return c.Display == DisplayBlock }},
		{"position: absolute; display: flex", func(c *Computed) bool { return c.Display == DisplayFlex }},
		{"position: static", func(c *Computed) bool { return c.Position == PositionStatic }},
		{"top: 10px; left: -2em; right: 50%; bottom: auto", func(c *Computed) bool {
			return c.Inset == [4]Dim{px(10), {Kind: DimPercent, Value: 50}, auto, px(-32)}
		}},
		{"top: 0", func(c *Computed) bool { return c.Inset[Top] == px(0) }},
		{"z-index: 3", func(c *Computed) bool { return c.ZIndex == 3 && !c.ZAuto }},
		{"z-index: -7", func(c *Computed) bool { return c.ZIndex == -7 && !c.ZAuto }},
		{"z-index: auto", func(c *Computed) bool { return c.ZAuto }},
		{"z-index: 99999999999", func(c *Computed) bool { return c.ZIndex == math.MaxInt32 }},
		{"z-index: -99999999999", func(c *Computed) bool { return c.ZIndex == math.MinInt32 }},
		{"height: 10mm", func(c *Computed) bool { return nearPx(c.Height.Value, 10*mmPx) && c.Height.Kind == DimPx }},
		{"height: 50%", func(c *Computed) bool { return c.Height == Dim{Kind: DimPercent, Value: 50} }},
		{"height: auto", func(c *Computed) bool { return c.Height == auto }},
		{"height: 0", func(c *Computed) bool { return c.Height == px(0) }},
		{"min-width: 1.5mm", func(c *Computed) bool { return nearPx(c.MinWidth.Value, 1.5*mmPx) }},
		{"min-width: 30%", func(c *Computed) bool { return c.MinWidth == Dim{Kind: DimPercent, Value: 30} }},
		{"min-width: auto", func(c *Computed) bool { return c.MinWidth == auto }},
		{"min-width: 0", func(c *Computed) bool { return c.MinWidth == px(0) }},
		{"vertical-align: -0.3mm", func(c *Computed) bool {
			return c.VerticalAlign == VAlignLength && nearPx(c.VAlignOffset.Value, -0.3*mmPx)
		}},
		{"vertical-align: 2em", func(c *Computed) bool { return c.VerticalAlign == VAlignLength && c.VAlignOffset == px(32) }},
		{"vertical-align: top", func(c *Computed) bool { return c.VerticalAlign == VAlignTop && c.VAlignOffset == px(0) }},
		{"vertical-align: 3px; vertical-align: middle", func(c *Computed) bool { return c.VerticalAlign == VAlignMiddle && c.VAlignOffset == px(0) }},
		{"align-items: baseline", func(c *Computed) bool { return c.AlignItems == ItemBaseline }},
		{"transform: none", func(c *Computed) bool { return c.Rotate == 0 }},
		{"transform: rotate(-4deg)", func(c *Computed) bool { return nearPx(c.Rotate, 356*math.Pi/180) }},
		{"transform: rotate(90deg)", func(c *Computed) bool { return nearPx(c.Rotate, math.Pi/2) }},
		{"transform: rotate(0.25turn)", func(c *Computed) bool { return nearPx(c.Rotate, math.Pi/2) }},
		{"transform: ROTATE(100grad)", func(c *Computed) bool { return nearPx(c.Rotate, math.Pi/2) }},
		{"transform: rotate(3.141592653589793rad)", func(c *Computed) bool { return nearPx(c.Rotate, math.Pi) }},
		{"transform: rotate(450deg)", func(c *Computed) bool { return nearPx(c.Rotate, math.Pi/2) }},
		{"transform: rotate(0)", func(c *Computed) bool { return c.Rotate == 0 }},
		{"page: cover", func(c *Computed) bool { return c.Page == "cover" }},
		{"page: Cover", func(c *Computed) bool { return c.Page == "Cover" }},
		{"page: auto", func(c *Computed) bool { return c.Page == "" }},
		{"counter-reset: chapter", func(c *Computed) bool { return reflect.DeepEqual(c.CounterReset, []CounterOp{{"chapter", 0}}) }},
		{"counter-increment: a 1 b 2 a 3", func(c *Computed) bool {
			return reflect.DeepEqual(c.CounterIncrement, []CounterOp{{"a", 4}, {"b", 2}})
		}},
		{"counter-increment: a 2147483647 a 2147483647", func(c *Computed) bool { return c.CounterIncrement[0].Value == math.MaxInt32 }},
		{"counter-reset: c 1 c 2", func(c *Computed) bool { return reflect.DeepEqual(c.CounterReset, []CounterOp{{"c", 2}}) }},
		{"counter-reset: a 1 b 2 a", func(c *Computed) bool { return reflect.DeepEqual(c.CounterReset, []CounterOp{{"a", 0}, {"b", 2}}) }},
		{"transform: rotate( 90deg )", func(c *Computed) bool { return nearPx(c.Rotate, math.Pi/2) }},
		{"counter-reset: a 5 b -2 c", func(c *Computed) bool {
			return reflect.DeepEqual(c.CounterReset, []CounterOp{{"a", 5}, {"b", -2}, {"c", 0}})
		}},
		{"counter-increment: finding", func(c *Computed) bool { return reflect.DeepEqual(c.CounterIncrement, []CounterOp{{"finding", 1}}) }},
		{"counter-increment: a 0 B 3", func(c *Computed) bool {
			return reflect.DeepEqual(c.CounterIncrement, []CounterOp{{"a", 0}, {"B", 3}})
		}},
		{"counter-increment: a 2147483647", func(c *Computed) bool { return c.CounterIncrement[0].Value == math.MaxInt32 }},
		{"counter-increment: a -99999999999", func(c *Computed) bool { return c.CounterIncrement[0].Value == math.MinInt32 }},
		{"counter-increment: none", func(c *Computed) bool { return c.CounterIncrement == nil }},
		{"counter-reset: none", func(c *Computed) bool { return c.CounterReset == nil }},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, tt.decl)
			if !tt.check(c) {
				t.Errorf("got %+v", c)
			}
			if w, _ := x.b.Warnings(); len(w) != 0 {
				t.Errorf("warnings %+v", w)
			}
		})
	}
}

// A function left open at the end of the input is closed there (CSS Syntax).
func TestRotateAtEOF(t *testing.T) {
	x := resolve(t, `<p style="transform: rotate(45deg">x</p>`, "", "")
	if c := x.by(t, "p"); !nearPx(c.Rotate, math.Pi/4) {
		t.Errorf("rotate %v, warnings %v", c.Rotate, x.warnings())
	}
}

func TestM3InvalidValues(t *testing.T) {
	for _, tt := range []struct{ decl, prop string }{
		{"position: sticky", "position"},
		{"position: foo", "position"},
		{"top: 1", "top"},
		{"left: none", "left"},
		{"bottom: 1px 2px", "bottom"},
		{"z-index: 1.5", "z-index"},
		{"z-index: 1e2", "z-index"},
		{"z-index: 1px", "z-index"},
		{"z-index: foo", "z-index"},
		{"height: -1px", "height"},
		{"height: -5%", "height"},
		{"height: foo", "height"},
		{"height: 1", "height"},
		{"height: 10px 20px", "height"},
		{"min-width: -1px", "min-width"},
		{"min-width: none", "min-width"},
		{"min-width: 5", "min-width"},
		{"vertical-align: 10%", "vertical-align"},
		{"vertical-align: 5", "vertical-align"},
		{"vertical-align: sub", "vertical-align"},
		{"transform: rotate(10px)", "transform"},
		{"transform: rotate(1)", "transform"},
		{"transform: rotate()", "transform"},
		{"transform: translate(1px)", "transform"},
		{"transform: rotate(1deg) rotate(2deg)", "transform"},
		{"transform: scale(2)", "transform"},
		{"transform: rotate(1deg, 2deg)", "transform"},
		{"page: 1", "page"},
		{"page: a b", "page"},
		{"page: none", "page"},
		{"page: default", "page"},
		{"counter-reset: 5", "counter-reset"},
		{"counter-reset: a 1.5", "counter-reset"},
		{"counter-reset: a b 1 2", "counter-reset"},
		{"counter-reset: none a", "counter-reset"},
		{"counter-reset: a, b", "counter-reset"},
		{"counter-reset: inherit a", "counter-reset"},
		{"counter-increment: a 1e3", "counter-increment"},
		{"counter-increment: -", "counter-increment"},
		{"counter-increment: auto", "counter-increment"},
	} {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, tt.decl+"; font-size: 14px")
			if !x.has("invalid-value: " + tt.prop) {
				t.Errorf("no invalid-value warning: %v", x.warnings())
			}
			if c.FontSize != 14 {
				t.Error("the next declaration was dropped")
			}
			init := initialStyle
			if c.Position != init.Position || c.ZAuto != init.ZAuto || c.Rotate != 0 || c.Height != init.Height ||
				c.MinWidth != init.MinWidth || c.Page != "" || c.CounterReset != nil || c.CounterIncrement != nil || c.VerticalAlign != VAlignBaseline {
				t.Errorf("an invalid declaration applied: %+v", c)
			}
		})
	}
	// An invalid declaration leaves the earlier valid one in place.
	c, _ := computed(t, "z-index: 4; z-index: 1.5; position: relative; position: sticky")
	if c.ZIndex != 4 || c.Position != PositionRelative {
		t.Errorf("%+v", c)
	}
	// transform-origin and the other transforms stay unsupported.
	_, x := computed(t, "transform-origin: top left")
	if !x.has("unsupported-property: transform-origin") {
		t.Errorf("%v", x.warnings())
	}
}

func TestM3CSSWideKeywordsAndInheritance(t *testing.T) {
	decls := "position: absolute; top: 5px; z-index: 2; height: 9px; min-width: 7px; vertical-align: 3px; transform: rotate(10deg); " +
		"page: cover; counter-reset: a 3; counter-increment: b 4; align-items: baseline"
	check := func(c *Computed, set bool) error {
		var bad []string
		if set != (c.Position == PositionAbsolute) {
			bad = append(bad, "position")
		}
		if set != (c.Inset[Top] == Dim{Value: 5}) {
			bad = append(bad, "top")
		}
		if set != (c.ZIndex == 2 && !c.ZAuto) {
			bad = append(bad, "z-index")
		}
		if set != (c.Height.Kind == DimPx) {
			bad = append(bad, "height")
		}
		if set != (c.MinWidth.Value == 7) {
			bad = append(bad, "min-width")
		}
		if set != (c.VerticalAlign == VAlignLength && c.VAlignOffset.Value == 3) {
			bad = append(bad, "vertical-align")
		}
		if set != (c.Rotate != 0) {
			bad = append(bad, "transform")
		}
		if set != (c.Page == "cover") {
			bad = append(bad, "page")
		}
		if set != (len(c.CounterReset) == 1 && c.CounterReset[0].Value == 3) {
			bad = append(bad, "counter-reset")
		}
		if set != (len(c.CounterIncrement) == 1 && c.CounterIncrement[0].Value == 4) {
			bad = append(bad, "counter-increment")
		}
		if set != (c.AlignItems == ItemBaseline) {
			bad = append(bad, "align-items")
		}
		if len(bad) != 0 {
			return fmt.Errorf("%v", bad)
		}
		return nil
	}
	props := []string{
		"position", "top", "z-index", "height", "min-width", "vertical-align", "transform", "page",
		"counter-reset", "counter-increment", "align-items",
	}
	for _, kw := range []string{"initial", "unset"} {
		t.Run(kw, func(t *testing.T) {
			var sb strings.Builder
			for _, p := range props {
				sb.WriteString(p + ": " + kw + ";")
			}
			x := resolve(t, `<div id=a><p id=b>x</p></div>`, `#a { `+decls+` } #b { `+sb.String()+` }`, "")
			if err := check(x.by(t, "#b"), false); err != nil {
				t.Errorf("%s did not reset: %v", kw, err)
			}
		})
	}
	t.Run("inherit", func(t *testing.T) {
		var sb strings.Builder
		for _, p := range props {
			sb.WriteString(p + ": inherit;")
		}
		x := resolve(t, `<div id=a><p id=b>x</p></div>`, `#a { `+decls+` } #b { `+sb.String()+` }`, "")
		if err := check(x.by(t, "#b"), true); err != nil {
			t.Errorf("inherit: %v", err)
		}
	})
	t.Run("not inherited", func(t *testing.T) {
		x := resolve(t, `<div id=a><p id=b>x</p></div>`, `#a { `+decls+` }`, "")
		if err := check(x.by(t, "#b"), false); err != nil {
			t.Errorf("inherited: %v", err)
		}
	})
}

// The insets are auto unless given, whatever the way a value comes (initial,
// inherit, unset), and 0 stays distinguishable from auto.
func TestInsetsAreAutoByDefault(t *testing.T) {
	autoAll := [4]Dim{{Kind: DimAuto}, {Kind: DimAuto}, {Kind: DimAuto}, {Kind: DimAuto}}
	if c, _ := computed(t, "color: red"); c.Inset != autoAll {
		t.Errorf("default %+v", c.Inset)
	}
	if c, _ := computed(t, "top: 0"); c.Inset[Top] != (Dim{}) || c.Inset[Left].Kind != DimAuto {
		t.Errorf("top: 0 %+v", c.Inset)
	}
	for _, kw := range []string{"initial", "unset", "inherit"} {
		x := resolve(t, `<div id=a><p id=b>x</p></div>`, `#a { top: 5px; left: 3px } #b { top: `+kw+`; right: `+kw+`; bottom: `+kw+`; left: `+kw+` }`, "")
		got := x.by(t, "#b").Inset
		want := autoAll
		if kw == "inherit" {
			want = [4]Dim{{Value: 5}, {Kind: DimAuto}, {Kind: DimAuto}, {Value: 3}}
		}
		if got != want {
			t.Errorf("%s: %+v, want %+v", kw, got, want)
		}
	}
}

func TestUsedOpacity(t *testing.T) {
	x := resolve(t, `<div id=a><p id=b>x</p></div><i>y</i>`, `#a, #b { opacity: .5 }`, "")
	if got := x.by(t, "#b").UsedOpacity(); !nearPx(got, .25) {
		t.Errorf("0.5 x 0.5 = %v", got)
	}
	if got := x.by(t, "i").UsedOpacity(); got != 1 {
		t.Errorf("no opacity: %v", got)
	}
}

func TestM3VarSubstitution(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { --h: 12px; --z: 3; --c: a 2; height: var(--h); z-index: var(--z); counter-reset: var(--c); position: var(--bad, absolute) }`, "")
	c := x.by(t, "p")
	if c.Height.Value != 12 || c.ZIndex != 3 || c.Position != PositionAbsolute || !reflect.DeepEqual(c.CounterReset, []CounterOp{{"a", 2}}) {
		t.Errorf("%+v", c)
	}
}

func pseudoOf(t testing.TB, x *doc, sel string, pe css.PseudoElement) *Computed {
	t.Helper()
	for i := range x.d.Elements {
		e := &x.d.Elements[i]
		if e.Tag.String() == sel || (e.ID != "" && sel == "#"+e.ID) {
			c, err := x.r.Pseudo(x.d, int32(i), pe, x.styles[i])
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
	}
	t.Fatalf("no element %q", sel)
	return nil
}

func TestPseudoContent(t *testing.T) {
	text := func(s string) ContentItem { return ContentItem{Kind: ContentText, Text: s} }
	ctr := func(n string, f CounterFormat) ContentItem {
		return ContentItem{Kind: ContentCounter, Text: n, Format: f}
	}
	tests := []struct {
		decl string
		want []ContentItem
	}{
		{`content: "x"`, []ContentItem{text("x")}},
		{`content: ""`, []ContentItem{text("")}},
		{`content: "a" "b"`, []ContentItem{text("a"), text("b")}},
		{`content: "a"counter(c)"b"`, []ContentItem{text("a"), ctr("c", CounterDecimal), text("b")}},
		{`content: counter(c)counter(d, decimal)`, []ContentItem{ctr("c", CounterDecimal), ctr("d", CounterDecimal)}},
		{`content: counter(c)`, []ContentItem{ctr("c", CounterDecimal)}},
		{`content: counter(c, decimal)`, []ContentItem{ctr("c", CounterDecimal)}},
		{`content: COUNTER(c, decimal-leading-zero) "  "`, []ContentItem{ctr("c", CounterDecimalLeadingZero), text("  ")}},
		{`content: counter(page)`, []ContentItem{ctr("page", CounterDecimal)}},
		{"content: \"a\\0 b\u202e\"", nil},
		{`content: none`, nil},
		{`content: normal`, nil},
		{`content: initial`, nil},
		{`content: unset`, nil},
		{`content: inherit`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			x := resolve(t, `<p>x</p>`, `p::before { `+tt.decl+` }`, "")
			c := pseudoOf(t, x, "p", css.PseudoBefore)
			if tt.want == nil {
				if strings.Contains(tt.decl, `\0`) {
					if c == nil || strings.ContainsAny(c.Content[0].Text, "\x00\u202e") {
						t.Errorf("content not sanitized: %+v", c)
					}
					return
				}
				if c != nil {
					t.Errorf("box generated: %+v", c.Content)
				}
				return
			}
			if c == nil || !reflect.DeepEqual(c.Content, tt.want) {
				t.Fatalf("got %+v, want %v", c, tt.want)
			}
			if w, _ := x.b.Warnings(); len(w) != 0 {
				t.Errorf("warnings %+v", w)
			}
		})
	}
}

func TestPseudoInvalidContent(t *testing.T) {
	for _, decl := range []string{
		`content: attr(href)`, `content: leader(".")`, `content: target-counter(attr(href), page)`, `content: target-text(attr(href))`,
		`content: counters(c, ".")`, `content: counter(c, lower-roman)`, `content: counter(c, upper-latin)`, `content: counter()`,
		`content: counter(c, decimal, x)`, `content: counter(none)`, `content: url(x.png)`, `content: open-quote`,
		`content: "a" foo`, `content: "a", "b"`, `content: counter(5)`, `content: string(scan)`,
		`content: leader(".") target-counter(attr(href), page)`,
	} {
		t.Run(decl, func(t *testing.T) {
			x := resolve(t, `<p>x</p>`, `p::after { `+decl+` }`, "")
			if c := pseudoOf(t, x, "p", css.PseudoAfter); c != nil {
				t.Errorf("box generated: %+v", c.Content)
			}
			if !x.has("invalid-value: content") {
				t.Errorf("warnings %v", x.warnings())
			}
		})
	}
	// An invalid declaration leaves the previous content.
	x := resolve(t, `<p>x</p>`, `p::after { content: "ok"; content: attr(href) }`, "")
	if c := pseudoOf(t, x, "p", css.PseudoAfter); c == nil || c.Content[0].Text != "ok" {
		t.Errorf("%+v", c)
	}
	// Too many items.
	x = resolve(t, `<p>x</p>`, `p::after { content: `+strings.Repeat(`"a" `, maxContentItems+1)+` }`, "")
	if c := pseudoOf(t, x, "p", css.PseudoAfter); c == nil || len(c.Content) != maxContentItems || !x.has("clamped: content") {
		t.Errorf("65 items: %+v, warnings %v", c, x.warnings())
	}
	x = resolve(t, `<p>x</p>`, `p::after { content: `+strings.Repeat(`"a" `, maxContentItems)+` }`, "")
	if c := pseudoOf(t, x, "p", css.PseudoAfter); c == nil || len(c.Content) != maxContentItems {
		t.Error("64 items rejected")
	}
}

func TestPseudoCascade(t *testing.T) {
	t.Run("before and after are separate", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p::before { content: "a"; color: red } p::after { content: "b" }`, "")
		b, a := pseudoOf(t, x, "p", css.PseudoBefore), pseudoOf(t, x, "p", css.PseudoAfter)
		if b == nil || a == nil || b.Content[0].Text != "a" || a.Content[0].Text != "b" {
			t.Fatalf("%+v %+v", b, a)
		}
		if b.Color.R != 1 || a.Color.R != 0 {
			t.Errorf("colors %+v %+v", b.Color, a.Color)
		}
	})
	t.Run("inherits from the element", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p { color: #123456; font-size: 20px; font-weight: bold; margin-left: 9px; position: relative } p::before { content: "a" }`, "")
		c := pseudoOf(t, x, "p", css.PseudoBefore)
		p := x.by(t, "p")
		if c.Color != p.Color || c.FontSize != 20 || c.FontWeight != 700 {
			t.Errorf("not inherited: %+v", c)
		}
		if c.Margin[Left].Value != 0 || c.Position != PositionStatic || c.Display != DisplayInline {
			t.Errorf("a non-inherited property leaked: %+v", c)
		}
	})
	t.Run("an inline pseudo has no content on the element style", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p { content: "no" } p::before { content: "a" }`, "")
		if c := x.by(t, "p"); c.Content != nil {
			t.Errorf("element content %+v", c.Content)
		}
	})
	t.Run("specificity then order", func(t *testing.T) {
		x := resolve(t, `<p class=k id=i>x</p>`, `.k::before { content: "class" } p::before { content: "type" } #i::before { content: "id" } p::before { content: "type2"; color: blue }`, "")
		c := pseudoOf(t, x, "#i", css.PseudoBefore)
		if c.Content[0].Text != "id" || c.Color.B != 1 {
			t.Errorf("%+v", c)
		}
	})
	t.Run("important and extra", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p::before { content: "doc" !important; color: red }`, `p::before { content: "extra" }`)
		if c := pseudoOf(t, x, "p", css.PseudoBefore); c.Content[0].Text != "extra" {
			t.Errorf("%+v", c.Content)
		}
	})
	t.Run("selector list", func(t *testing.T) {
		x := resolve(t, `<p>x</p><i>y</i>`, `p, i::after { content: "v"; color: red } i::before { content: "w" }`, "")
		if pseudoOf(t, x, "p", css.PseudoAfter) != nil || pseudoOf(t, x, "i", css.PseudoAfter) == nil {
			t.Error("a list mixing an element and a pseudo-element selector")
		}
		if x.by(t, "p").Color.R != 1 {
			t.Error("the plain selector of the list applies to the element")
		}
	})
	t.Run("pseudo rules never match the element", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p::before { color: red; margin-left: 5px }`, "")
		p := x.by(t, "p")
		if p.Color.R != 0 || p.Margin[Left].Value != 0 {
			t.Errorf("%+v", p)
		}
	})
	t.Run("sharing and cache", func(t *testing.T) {
		x := resolve(t, `<p>a</p><p>b</p><p>c</p>`, `p::before { content: "a" }`, "")
		var first *Computed
		for i := range x.d.Elements {
			if x.d.Elements[i].Tag.String() != "p" {
				continue
			}
			c, err := x.r.Pseudo(x.d, int32(i), css.PseudoBefore, x.styles[i])
			if err != nil || c == nil {
				t.Fatal(c, err)
			}
			if first == nil {
				first = c
			} else if c != first {
				t.Error("same parent style and rules: the pseudo style is shared")
			}
		}
	})
	t.Run("custom properties", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p { --c: green } p::before { content: "a"; color: var(--c) }`, "")
		if c := pseudoOf(t, x, "p", css.PseudoBefore); c.Color.G < 0.4 {
			t.Errorf("%+v", c.Color)
		}
	})
	t.Run("display none pseudo", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p::before { content: "a"; display: none }`, "")
		if pseudoOf(t, x, "p", css.PseudoBefore) != nil {
			t.Error("box for a display: none pseudo-element")
		}
	})
	t.Run("display none parent", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p { display: none } p::before { content: "a" }`, "")
		if pseudoOf(t, x, "p", css.PseudoBefore) != nil {
			t.Error("box for a display: none element")
		}
	})
	t.Run("no pseudo rule", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p { color: red }`, "")
		if pseudoOf(t, x, "p", css.PseudoBefore) != nil {
			t.Error("box without rule")
		}
	})
	t.Run("positioned and counters", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `p::after { content: ""; position: absolute; top: 2px; z-index: 5; counter-increment: n 2; counter-reset: m; display: inline }`, "")
		c := pseudoOf(t, x, "p", css.PseudoAfter)
		if c.Position != PositionAbsolute || c.Display != DisplayBlock || c.Inset[Top].Value != 2 || c.ZIndex != 5 ||
			!reflect.DeepEqual(c.CounterIncrement, []CounterOp{{"n", 2}}) || !reflect.DeepEqual(c.CounterReset, []CounterOp{{"m", 0}}) {
			t.Errorf("%+v", c)
		}
	})
}

func TestPseudoOnAnyElement(t *testing.T) {
	x := resolve(t, `<table><tr><td id=c>x</td></tr></table><p>a<br>b</p>`, `:root::before { content: "r" } td::after { content: "t" } br::before { content: "b" }`, "")
	for _, sel := range []string{"html", "td", "br"} {
		pe := css.PseudoBefore
		if sel == "td" {
			pe = css.PseudoAfter
		}
		if c := pseudoOf(t, x, sel, pe); c == nil {
			t.Errorf("no pseudo style on %s", sel)
		}
	}
	if c := pseudoOf(t, x, "td", css.PseudoAfter); c != nil && c.Display != DisplayInline {
		t.Errorf("display %v", c.Display)
	}
}

func TestPseudoStylesLimit(t *testing.T) {
	l := budget.Defaults()
	l[budget.Styles] = 5
	b := budget.New(t.Context(), l, nil)
	var sb strings.Builder
	for i := range 20 {
		fmt.Fprintf(&sb, `<p class=c%d>x</p>`, i)
	}
	d, perr := dom.Parse(strings.NewReader(sb.String()), b)
	if perr != nil {
		t.Fatal(perr)
	}
	var rules strings.Builder
	for i := range 20 {
		fmt.Fprintf(&rules, `.c%d::before { content: "%d" }`, i, i)
	}
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, rules.String(), css.OriginDocument, b)}, printEnv, b)
	styles := make([]*Computed, len(d.Elements))
	var err error
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		if styles[i], err = r.Element(d, int32(i), parent); err != nil {
			break
		}
		if _, err = r.Pseudo(d, int32(i), css.PseudoBefore, styles[i]); err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("no Styles limit error")
	}
}

func TestCounterOpsLimit(t *testing.T) {
	names := func(n int) string {
		var sb strings.Builder
		for i := range n {
			fmt.Fprintf(&sb, "c%d ", i)
		}
		return sb.String()
	}
	for _, prop := range []string{"counter-reset", "counter-increment"} {
		t.Run(prop, func(t *testing.T) {
			c, x := computed(t, prop+": "+names(MaxCounterOps))
			ops := c.CounterReset
			if prop == "counter-increment" {
				ops = c.CounterIncrement
			}
			if len(ops) != MaxCounterOps || x.has("clamped") {
				t.Errorf("%d ops, warnings %v", len(ops), x.warnings())
			}
			c, x = computed(t, prop+": "+names(MaxCounterOps+1))
			ops = c.CounterReset
			if prop == "counter-increment" {
				ops = c.CounterIncrement
			}
			if len(ops) != MaxCounterOps || ops[MaxCounterOps-1].Name != fmt.Sprintf("c%d", MaxCounterOps-1) || !x.has("clamped: counters") {
				t.Errorf("%d ops, warnings %v", len(ops), x.warnings())
			}
			c, _ = computed(t, prop+": "+names(1000))
			ops = c.CounterReset
			if prop == "counter-increment" {
				ops = c.CounterIncrement
			}
			if len(ops) != MaxCounterOps {
				t.Errorf("%d ops for 1000 entries", len(ops))
			}
		})
	}
	if _, x := computed(t, "counter-reset: "+strings.Repeat("a", MaxIdentBytes+1)); !x.has("invalid-value: counter-reset") {
		t.Errorf("a name beyond MaxIdentBytes: %v", x.warnings())
	}
	if c, _ := computed(t, "counter-reset: "+strings.Repeat("a", MaxIdentBytes)); len(c.CounterReset) != 1 {
		t.Error("a name of MaxIdentBytes rejected")
	}
}

func TestStyleOpacityOnGradient(t *testing.T) {
	x := resolve(t, `<div id=a><div id=b>x</div></div>`, `#a { opacity: .5; background: radial-gradient(circle, rgba(234, 91, 12, 1) 0%, rgba(234, 91, 12, 0) 70%) } #b { opacity: .5; background-image: inherit }`, "")
	a, b := x.by(t, "#a"), x.by(t, "#b")
	if a.BackgroundImage.Shape != css.GradientCircle || !nearPx(a.BackgroundImage.Stops[0].Color.A, .5) || a.BackgroundImage.Stops[1].Color.A != 0 {
		t.Errorf("a %+v", a.BackgroundImage)
	}
	if !nearPx(b.BackgroundImage.Stops[0].Color.A, .25) {
		t.Errorf("an inherited gradient is faded once, by the product of the opacities: %+v", b.BackgroundImage.Stops)
	}
}

func TestNamedPages(t *testing.T) {
	const src = `
@page { size: A4; margin: 25mm 20mm 22mm;
  @top-center { content: "head" } @bottom-left { content: "TLP" } @bottom-right { content: counter(page) " / " counter(pages) } }
@page cover { margin: 0; @top-center { content: none; border: none } @bottom-left { content: none } @bottom-right { content: none } }`
	ps, b := pageOfNamed(t, src, "cover")
	if ps.Margin != [4]float64{} {
		t.Errorf("cover margin %v", ps.Margin)
	}
	if ps.HasBoxes() {
		t.Errorf("cover boxes %+v", ps.Boxes)
	}
	if ps.Width != 210*mmPx || ps.Height != 297*mmPx {
		t.Errorf("cover size %v x %v: the default rule applies too", ps.Width, ps.Height)
	}
	def, _ := pageOfNamed(t, src, "")
	if !nearPx(def.Margin[Top], 25*mmPx) || def.Boxes[MarginTopCenter] == nil || def.Boxes[MarginBottomRight] == nil {
		t.Errorf("default page %+v", def)
	}
	b2 := newBudget()
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, src, css.OriginDocument, b2)}, printEnv, b2)
	if other := r.Page(PageContext{Name: "nobody"}); other != r.Page(PageContext{}) {
		t.Error("an unknown name shares the default page style")
	}
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Errorf("warnings %+v", w)
	}
}

func pageOfNamed(t testing.TB, docCSS, name string) (*PageStyle, *budget.Budget) {
	t.Helper()
	b := newBudget()
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, docCSS, css.OriginDocument, b)}, printEnv, b)
	return r.Page(PageContext{Number: 1, Name: name}), b
}

func TestNamedPageCascade(t *testing.T) {
	t.Run("named wins whatever the order", func(t *testing.T) {
		ps, _ := pageOfNamed(t, `@page n { margin: 0 } @page { margin: 30mm }`, "n")
		if ps.Margin != [4]float64{} {
			t.Errorf("%v", ps.Margin)
		}
	})
	t.Run("important of the default beats the named", func(t *testing.T) {
		ps, _ := pageOfNamed(t, `@page n { margin-top: 0 } @page { margin-top: 30mm !important }`, "n")
		if !nearPx(ps.Margin[Top], 30*mmPx) {
			t.Errorf("%v", ps.Margin)
		}
	})
	t.Run("same name twice merges in order", func(t *testing.T) {
		ps, _ := pageOfNamed(t, `@page n { margin-top: 5mm; margin-left: 6mm } @page n { margin-top: 7mm }`, "n")
		if !nearPx(ps.Margin[Top], 7*mmPx) || !nearPx(ps.Margin[Left], 6*mmPx) {
			t.Errorf("%v", ps.Margin)
		}
	})
	t.Run("named size and a margin box", func(t *testing.T) {
		ps, _ := pageOfNamed(t, `@page wide { size: A4 landscape; @top-left { content: "w" } }`, "wide")
		if ps.Width < ps.Height || ps.Boxes[MarginTopLeft] == nil {
			t.Errorf("%+v", ps)
		}
		if def, _ := pageOfNamed(t, `@page wide { size: A4 landscape; @top-left { content: "w" } }`, ""); def.HasBoxes() || def.Width > def.Height {
			t.Errorf("a named rule leaked into the default page %+v", def)
		}
	})
	t.Run("other selectors stay unsupported", func(t *testing.T) {
		for _, sel := range []string{":first", ":left", "n:first", "a b", "5", "n, m", "none", "auto"} {
			_, b := pageOfNamed(t, `@page `+sel+` { margin: 0 }`, "")
			if w, _ := b.Warnings(); len(w) != 1 || w[0].Detail != "@page selector" {
				t.Errorf("@page %s: %+v", sel, w)
			}
		}
	})
	t.Run("media", func(t *testing.T) {
		ps, _ := pageOfNamed(t, `@media screen { @page n { margin: 0 } }`, "n")
		if ps.Margin == [4]float64{} {
			t.Error("a screen-only named page applied to print")
		}
	})
}

// The unnamed rules are reduced once: a page name costs its own rules, not the
// thousands of the others.
func TestNamedPagesCostOnlyTheirRules(t *testing.T) {
	build := func(unnamed int) float64 {
		var sb strings.Builder
		for i := range unnamed {
			fmt.Fprintf(&sb, "@page { margin-left: %dmm; @top-left { content: \"%d\" } }\n", i%30, i)
		}
		for i := range 500 {
			fmt.Fprintf(&sb, "@page p%d { margin-top: %dmm }\n", i, i%20)
		}
		b := newBudget()
		sheet := compileSheet(t, sb.String(), css.OriginDocument, b)
		return testing.AllocsPerRun(1, func() {
			r := NewResolver(ua(), nil, []*Sheet{sheet}, printEnv, b)
			for i := range 500 {
				r.Page(PageContext{Name: fmt.Sprintf("p%d", i)})
			}
		})
	}
	twin, big := build(0), build(2500)
	if big > twin*1.3+100 {
		t.Errorf("%v allocations with 2500 unnamed rules, %v without", big, twin)
	}
	// And the cascade stays right: the named rule wins over the unnamed ones.
	var sb strings.Builder
	for i := range 2500 {
		fmt.Fprintf(&sb, "@page { margin-left: %dmm }\n", i)
	}
	sb.WriteString("@page n { margin-left: 3mm } @page { margin-top: 7mm !important } @page n { margin-top: 1mm }")
	ps, _ := pageOfNamed(t, sb.String(), "n")
	if !nearPx(ps.Margin[Left], 3*mmPx) || !nearPx(ps.Margin[Top], 7*mmPx) {
		t.Errorf("margins %v", ps.Margin)
	}
}

func TestManyNamedPages(t *testing.T) {
	var sb strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&sb, "@page p%d { margin: %dmm }\n", i, i%20)
	}
	b := newBudget()
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, sb.String(), css.OriginDocument, b)}, printEnv, b)
	seen := map[*PageStyle]bool{}
	for i := range 5000 {
		seen[r.Page(PageContext{Number: i + 1, Name: fmt.Sprintf("p%d", i)})] = true
		r.Page(PageContext{Number: i + 1, Name: fmt.Sprintf("p%d", i)})
		r.Page(PageContext{Number: i + 1, Name: "unused"})
	}
	if len(seen) != 5000 {
		t.Errorf("%d distinct styles, want one per name", len(seen))
	}
	if n := len(r.pageNames); n != 5000 {
		t.Errorf("%d cached styles", n)
	}
	if !nearPx(r.Page(PageContext{Name: "p3"}).Margin[Left], 3*mmPx) {
		t.Error("wrong margin")
	}
	allocs := testing.AllocsPerRun(10, func() {
		for i := range 5000 {
			r.Page(PageContext{Name: "p1"})
			_ = i
		}
	})
	if allocs > 0 {
		t.Errorf("cached lookups allocate: %v", allocs)
	}
}

func TestCorpus03Pseudo(t *testing.T) {
	x := corpusResolver(t, "03-analyst")
	got := map[string]int{}
	for i := range x.d.Elements {
		for _, pe := range []css.PseudoElement{css.PseudoBefore, css.PseudoAfter} {
			if c, err := x.r.Pseudo(x.d, int32(i), pe, x.styles[i]); err != nil {
				t.Fatal(err)
			} else if c != nil {
				got[fmt.Sprintf("%s.%s/%d/%v", x.d.Elements[i].Tag, strings.Join(x.d.Elements[i].Classes, "."), pe, c.Content)]++
			}
		}
	}
	for _, want := range []string{"td.idx/1/[{3 row 1}]", "div.note/1/[{0 i 0}]", "h2./2/[{0  0}]"} {
		if got[want] == 0 {
			t.Errorf("missing %q in %v", want, got)
		}
	}
}

func corpusResolver(t *testing.T, name string) *doc {
	t.Helper()
	_, d := loadReport(t, name, testcorpus.Small)
	b := newBudget()
	var sheets []*Sheet
	for _, src := range documentCSS(d) {
		sheets = append(sheets, compileSheet(t, src, css.OriginDocument, b))
	}
	x := &doc{d: d, b: b, styles: make([]*Computed, len(d.Elements))}
	x.r = NewResolver(ua(), nil, sheets, printEnv, b)
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = x.styles[p]
		}
		var err error
		if x.styles[i], err = x.r.Element(d, int32(i), parent); err != nil {
			t.Fatal(err)
		}
	}
	return x
}

func TestCorpus02Pseudo(t *testing.T) {
	x := corpusResolver(t, "02-executive")
	d := x.d
	got := map[string]int{}
	for i := range d.Elements {
		for _, pe := range []css.PseudoElement{css.PseudoBefore, css.PseudoAfter} {
			c, err := x.r.Pseudo(d, int32(i), pe, x.styles[i])
			if err != nil {
				t.Fatal(err)
			}
			if c == nil {
				continue
			}
			key := fmt.Sprintf("%s/%d/%v/%v", d.Elements[i].Tag, pe, c.Content, c.Position)
			got[key]++
		}
	}
	for _, want := range []string{
		"h2/1/[{3 chapter 1} {0    0}]/0",
		"section/2/[{0  0}]/2",
		"li/1/[{3 finding 0}]/2",
	} {
		if got[want] == 0 {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for key := range got {
		if strings.HasPrefix(key, "a/2/") {
			t.Errorf("nav.toc a::after generated a box: %s", key)
		}
	}
	if !x.has("invalid-value: content: leader") {
		t.Errorf("no invalid-value for the toc leader: %v", x.warnings())
	}
	for k := range x.warnings() {
		if !strings.HasPrefix(k, "invalid-value: content: leader") && !strings.HasPrefix(k, "unsupported-") &&
			!strings.HasPrefix(k, "clamped") {
			t.Logf("warning %s", k)
		}
	}
	// The cover page: margin 0 and no margin box.
	cover := x.r.Page(PageContext{Number: 1, Name: "cover"})
	if cover.Margin != [4]float64{} || cover.HasBoxes() {
		t.Errorf("cover %+v", cover)
	}
	if def := x.r.Page(PageContext{Number: 2}); !def.HasBoxes() || def.Margin == [4]float64{} {
		t.Errorf("default %+v", def)
	}
	// The glow: circle, accent at 0 %, transparent at 70 %, faded by opacity .55.
	found := false
	for i := range d.Elements {
		c, err := x.r.Pseudo(d, int32(i), css.PseudoAfter, x.styles[i])
		if err != nil || c == nil || c.BackgroundImage == nil {
			continue
		}
		g := c.BackgroundImage
		found = true
		if g.Shape != css.GradientCircle || len(g.Stops) != 2 || g.Stops[0].Pos != 0 || !nearPx(g.Stops[1].Pos, 0.7) ||
			!nearPx(g.Stops[0].Color.A, 0.55) || g.Stops[1].Color.A != 0 {
			t.Errorf("glow %+v", g)
		}
		if c.Position != PositionAbsolute || c.Display != DisplayBlock {
			t.Errorf("glow position %v display %v", c.Position, c.Display)
		}
	}
	if !found {
		t.Error("no glow")
	}
}

func TestSnapBorderWidth(t *testing.T) {
	for _, tt := range []struct{ in, want float64 }{
		{0, 0},
		{1e-300, 1},
		{0.1, 1},
		{0.5, 1},
		{0.9999, 1},
		{1, 1},
		{1.9, 1},
		{1.99, 1},
		{2, 2},
		{3.33, 3},
		{4.5, 4},
		{11.34, 11},
		{1e7, 1e7},
		{-1, 0},
		{-0.5, 0},
		{math.NaN(), 0},
	} {
		if got := snapBorderWidth(tt.in); got != tt.want {
			t.Errorf("snapBorderWidth(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// Border widths snap per side at computed-value time; keywords are 1, 3 and 5 px,
// a style of none makes the width 0, and a negative width drops the whole
// shorthand (invalid-value).
func TestBorderWidthsAreSnapped(t *testing.T) {
	c, x := computed(t, "border-style: solid; border-width: 0.5pt 2.5pt 1.9px 0.3px")
	if c.Border[Top].Width != 1 || c.Border[Right].Width != 3 || c.Border[Bottom].Width != 1 || c.Border[Left].Width != 1 {
		t.Errorf("per side %+v", c.Border)
	}
	if w, _ := x.b.Warnings(); len(w) != 0 {
		t.Errorf("warnings %+v", w)
	}
	for kw, want := range map[string]float64{"thin": 1, "medium": 3, "thick": 5} {
		if c, _ := computed(t, "border: "+kw+" solid"); c.Border[Top].Width != want {
			t.Errorf("%s: %v", kw, c.Border[Top].Width)
		}
	}
	if c, _ := computed(t, "border: 3px none"); c.Border[Top].Width != 0 {
		t.Errorf("style none: %v", c.Border[Top].Width)
	}
	if c, _ := computed(t, "border: 2.5pt solid; border-left-width: 0.2mm"); c.Border[Left].Width != 1 || c.Border[Top].Width != 3 {
		t.Errorf("longhand after shorthand: %+v", c.Border)
	}
	c, x = computed(t, "border: solid red; border: -1px solid blue")
	if !x.has("invalid-value: border") || c.Border[Top].Color.R != 1 || c.Border[Top].Width != 3 {
		t.Errorf("negative width: %+v warnings %v", c.Border[Top], x.warnings())
	}
}
