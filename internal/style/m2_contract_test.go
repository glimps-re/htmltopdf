package style

import (
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func ctNear(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func ctOne(t *testing.T, decl string) (*Computed, *doc) {
	t.Helper()
	x := resolve(t, `<p>x</p>`, `p { `+decl+` }`, "")
	return x.by(t, "p"), x
}

func TestContractFontShorthandValid(t *testing.T) {
	tests := []struct {
		decl   string
		style  FontStyle
		weight int
		size   float64
		lh     LineHeight
		family string
	}{
		{"font: 12px serif", FontNormal, 400, 12, LineHeight{}, "serif"},
		{"font: italic 12px serif", FontItalic, 400, 12, LineHeight{}, "serif"},
		{"font: oblique 12px serif", FontOblique, 400, 12, LineHeight{}, "serif"},
		{"font: bold 12px serif", FontNormal, 700, 12, LineHeight{}, "serif"},
		{"font: 600 12px serif", FontNormal, 600, 12, LineHeight{}, "serif"},
		{"font: 1000 12px serif", FontNormal, 1000, 12, LineHeight{}, "serif"},
		{"font: 1 12px serif", FontNormal, 1, 12, LineHeight{}, "serif"},
		{"font: italic bold 12px serif", FontItalic, 700, 12, LineHeight{}, "serif"},
		{"font: bold italic 12px serif", FontItalic, 700, 12, LineHeight{}, "serif"},
		{"font: normal normal 12px serif", FontNormal, 400, 12, LineHeight{}, "serif"},
		{"font: normal normal normal 12px serif", FontNormal, 400, 12, LineHeight{}, "serif"},
		{"font: italic normal 700 12px serif", FontItalic, 700, 12, LineHeight{}, "serif"},
		{"font: 12px/1.5 serif", FontNormal, 400, 12, LineHeight{Kind: LineHeightNumber, Value: 1.5}, "serif"},
		{"font: 12px / 1.5 serif", FontNormal, 400, 12, LineHeight{Kind: LineHeightNumber, Value: 1.5}, "serif"},
		{"font: 12px/20px serif", FontNormal, 400, 12, LineHeight{Kind: LineHeightPx, Value: 20}, "serif"},
		{"font: 12px/normal serif", FontNormal, 400, 12, LineHeight{}, "serif"},
		{"font: 9pt/1.2 monospace", FontNormal, 400, 12, LineHeight{Kind: LineHeightNumber, Value: 1.2}, "monospace"},
		{"font: 12PX SERIF", FontNormal, 400, 12, LineHeight{}, "SERIF"},
		{"font: 8pt \"DejaVu Sans\", Arial, sans-serif", FontNormal, 400, 8 * 4 / 3.0, LineHeight{}, "DejaVu Sans"},
		{"font: 1.5em serif", FontNormal, 400, 24, LineHeight{}, "serif"},
		{"font: 200% serif", FontNormal, 400, 32, LineHeight{}, "serif"},
		{"font: large serif", FontNormal, 400, 18, LineHeight{}, "serif"},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := ctOne(t, tt.decl)
			if c.FontStyle != tt.style || c.FontWeight != tt.weight || !ctNear(c.FontSize, tt.size) || c.LineHeight.Kind != tt.lh.Kind || !ctNear(c.LineHeight.Value, tt.lh.Value) {
				t.Errorf("style %v weight %d size %v lh %+v; want %v %d %v %+v",
					c.FontStyle, c.FontWeight, c.FontSize, c.LineHeight, tt.style, tt.weight, tt.size, tt.lh)
			}
			if len(c.FontFamily) == 0 || !strings.EqualFold(c.FontFamily[0], tt.family) {
				t.Errorf("family %v, want first %q", c.FontFamily, tt.family)
			}
			if w := x.warnings(); len(w) != 0 {
				t.Errorf("warnings: %v", w)
			}
		})
	}
}

func TestContractFontShorthandInvalid(t *testing.T) {
	for _, decl := range []string{
		"font: caption", "font: icon", "font: menu", "font: message-box", "font: small-caption", "font: status-bar",
		"font: serif",
		"font: 12px",
		"font:",
		"font: small-caps 12px serif",
		"font: condensed 12px serif",
		"font: expanded 12px serif",
		"font: ultra-condensed 12px serif",
		"font: italic italic 12px serif",
		"font: bold bold 12px serif",
		"font: bold 700 12px serif",
		"font: 0 12px serif",
		"font: 1001 12px serif",
		"font: -1 12px serif",
		"font: normal normal normal normal 12px serif",
		"font: 12px/ serif",
		"font: 12px/1.5/2 serif",
		"font: 12px/-1 serif",
		"font: -12px serif",
		"font: 12 serif",
		"font: 12px 13px serif",
		"font: 12deg serif",
		"font: 12px serif ; color: red",
		"font: 12px ,",
		"font: bold",
		"font: 12px/1.5",
	} {
		t.Run(decl, func(t *testing.T) {
			ref, _ := ctOne(t, "")
			c, x := ctOne(t, "font-weight: 300; "+decl)
			if strings.Contains(decl, "; color") {
				return
			}
			if c.FontSize != ref.FontSize || c.FontStyle != FontNormal {
				t.Errorf("an invalid font was applied: size %v style %v family %v", c.FontSize, c.FontStyle, c.FontFamily)
			}
			if c.FontWeight != 300 {
				t.Errorf("invalid font touched the weight longhand: %d", c.FontWeight)
			}
			if len(x.warnings()) == 0 {
				t.Errorf("no invalid-value warning: %v", x.warnings())
			}
		})
	}
}

func TestContractFontShorthandResets(t *testing.T) {
	c, _ := ctOne(t, "line-height: 3; font-weight: 900; font-style: italic; font-variant-numeric: tabular-nums; font: 12px serif")
	if c.LineHeight != (LineHeight{}) {
		t.Errorf("line-height not reset to normal: %+v", c.LineHeight)
	}
	if c.FontWeight != 400 || c.FontStyle != FontNormal {
		t.Errorf("weight %d style %v not reset", c.FontWeight, c.FontStyle)
	}
	if c.TabularNums {
		t.Error("font-variant-numeric not reset")
	}

	c, _ = ctOne(t, "font: 12px serif; line-height: 2; font-weight: bold")
	if c.LineHeight != (LineHeight{Kind: LineHeightNumber, Value: 2}) || c.FontWeight != 700 {
		t.Errorf("later longhands must win: %+v %d", c.LineHeight, c.FontWeight)
	}

	x := resolve(t, `<div><p>x</p></div>`, `div { line-height: 3; font-weight: bold; font-style: italic } p { font: 10px serif }`, "")
	if p := x.by(t, "p"); p.LineHeight != (LineHeight{}) || p.FontWeight != 400 || p.FontStyle != FontNormal {
		t.Errorf("font must reset the inherited values to initial, not inherit: %+v", p)
	}
}

func TestContractFontShorthandKeywordsAndVar(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div>`, `div { font: italic 700 20px/2 monospace } p { font: inherit }`, "")
	p := x.by(t, "p")
	if p.FontStyle != FontItalic || p.FontWeight != 700 || p.FontSize != 20 || p.LineHeight.Value != 2 || p.FontFamily[0] != "monospace" {
		t.Errorf("font: inherit: %+v", p)
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { font: bold 20px serif } p { font: initial }`, "")
	if p := x.by(t, "p"); p.FontWeight != 400 || p.FontSize != 16 {
		t.Errorf("font: initial: %+v", p)
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { font: bold 20px serif } p { font: unset }`, "")
	if p := x.by(t, "p"); p.FontWeight != 700 || p.FontSize != 20 {
		t.Errorf("font: unset inherits: %+v", p)
	}

	for name, css := range map[string]string{
		"whole":  `:root { --f: italic bold 14px/1.5 serif } p { font: var(--f) }`,
		"size":   `:root { --s: 14px } p { font: bold var(--s) serif }`,
		"family": `:root { --fam: monospace } p { font: 14px var(--fam) }`,
		"fallbk": `p { font: var(--missing, 14px serif) }`,
	} {
		x := resolve(t, `<p>x</p>`, css, "")
		p := x.by(t, "p")
		if p.FontSize != 14 || len(x.warnings()) != 0 {
			t.Errorf("%s: size %v warnings %v", name, p.FontSize, x.warnings())
		}
	}
	x = resolve(t, `<p>x</p>`, `:root { --f: notafont } p { font: var(--f); font-weight: 300 }`, "")
	if p := x.by(t, "p"); p.FontSize != 16 || p.FontWeight != 300 {
		t.Errorf("an invalid var() font must be dropped: %+v", p)
	}
	x = resolve(t, `<p>x</p>`, `p { font: var(--a); --a: var(--a) }`, "")
	if p := x.by(t, "p"); p.FontSize != 16 {
		t.Errorf("cycle: %+v", p)
	}
}

func TestContractFontInMarginBox(t *testing.T) {
	ps, b := pageOf(t, "", `@page { @top-left { content: "x"; font: bold 9pt/1 "DejaVu Sans", sans-serif }
		@top-right { content: "y"; font: small-caps 9pt serif }
		@bottom-center { content: "z"; font: 10px serif; text-transform: lowercase; opacity: .5; color: #000 } }`)
	tl := ps.Boxes[MarginTopLeft]
	if tl == nil || tl.Style.FontWeight != 700 || !ctNear(tl.Style.FontSize, 12) || tl.Style.LineHeight != (LineHeight{Kind: LineHeightNumber, Value: 1}) {
		t.Errorf("top-left %+v", tl)
	}
	if tr := ps.Boxes[MarginTopRight]; tr != nil && tr.Style.FontSize != 16 && tr.Style.FontSize != 12 {
		t.Errorf("top-right %+v", tr.Style)
	}
	if bc := ps.Boxes[MarginBottomCenter]; bc == nil || bc.Style.FontSize != 10 || bc.Style.TextTransform != TransformLowercase {
		t.Errorf("bottom-center %+v", bc)
	}
	w, _ := b.Warnings()
	if len(w) != 1 || w[0].Code != "invalid-value" {
		t.Errorf("want exactly the small-caps invalid-value warning: %+v", w)
	}
}

func TestContractOpacity(t *testing.T) {
	t.Run("values", func(t *testing.T) {
		for decl, want := range map[string]float64{
			"opacity: 1": 1, "opacity: 0.25": .25, "opacity: 25%": .25, "opacity: 0%": 0, "opacity: 100%": 1,
			"opacity: 1e0": 1, "opacity: .0": 0, "opacity: 1.0000001": 1, "opacity: -0.0001": 0,
			"opacity: 99999999999999999999": 1, "opacity: -99999999999999999999": 0, "opacity: 1e400": 1,
		} {
			c, x := ctOne(t, "color: #000; "+decl)
			if !ctNear(c.Color.A, want) {
				t.Errorf("%s: alpha %v want %v (warnings %v)", decl, c.Color.A, want, x.warnings())
			}
		}
	})
	t.Run("invalid", func(t *testing.T) {
		for _, decl := range []string{
			"opacity:", "opacity: auto", "opacity: 1px", "opacity: 1 2", "opacity: 50% 50%", "opacity: calc(", "opacity: #fff",
			"opacity: 1em", "opacity: nan", "opacity: infinity", "opacity: --1", "opacity: 0.5.5", "opacity: \"1\"",
		} {
			c, x := ctOne(t, "color: #000; "+decl)
			if c.Color.A != 1 {
				t.Errorf("%s applied: alpha %v", decl, c.Color.A)
			}
			if len(x.warnings()) == 0 {
				t.Errorf("%s: no warning: %v", decl, x.warnings())
			}
		}
	})
	t.Run("never NaN or out of range", func(t *testing.T) {
		for _, v := range []string{"1e999999", "-1e999999", "0.0000000000000000001", "1e-999999"} {
			c, _ := ctOne(t, "color: #123; background: #456; border: 1px solid; opacity: "+v)
			for _, a := range []float64{c.Color.A, c.BackgroundColor.A, c.Border[Top].Color.A} {
				if math.IsNaN(a) || a < 0 || a > 1 {
					t.Errorf("opacity %s gives alpha %v", v, a)
				}
			}
		}
	})
	t.Run("product, all coloured faces, channels intact", func(t *testing.T) {
		x := resolve(t, `<div><p><span>y</span></p></div>`,
			`div { opacity: .5 } p { opacity: .5; color: rgb(10, 20, 30); background: rgb(40, 50, 60); border: 2px solid rgb(70, 80, 90) } span { opacity: .8; border-left: 1px solid; color: #fff }`, "")
		p, span := x.by(t, "p"), x.by(t, "span")
		for name, a := range map[string]float64{"color": p.Color.A, "bg": p.BackgroundColor.A, "top": p.Border[Top].Color.A, "left": p.Border[Left].Color.A} {
			if !ctNear(a, .25) {
				t.Errorf("p %s alpha %v want .25", name, a)
			}
		}
		if !ctNear(span.Color.A, .2) || !ctNear(span.Border[Left].Color.A, .2) {
			t.Errorf("span alpha %v / %v want .2 (currentColor border multiplied once)", span.Color.A, span.Border[Left].Color.A)
		}
		if p.Color.R == 0 || p.BackgroundColor.G == 0 || p.Border[Top].Color.B == 0 {
			t.Errorf("channels altered: %+v %+v %+v", p.Color, p.BackgroundColor, p.Border[Top].Color)
		}
	})
	t.Run("opacity 1 child restores nothing", func(t *testing.T) {
		x := resolve(t, `<div><p>y</p></div>`, `div { opacity: .5; color: #000 } p { opacity: 1 }`, "")
		if a := x.by(t, "p").Color.A; !ctNear(a, .5) {
			t.Errorf("child of a translucent parent cannot be more opaque: %v", a)
		}
	})
	t.Run("opacity itself is not inherited as a value", func(t *testing.T) {
		x := resolve(t, `<div><p>y</p></div>`, `div { opacity: .5 } p { color: #000 }`, "")
		if a := x.by(t, "p").Color.A; !ctNear(a, .5) {
			t.Errorf("p alpha %v want .5 (once, not .25)", a)
		}
	})
	t.Run("inherit, initial, unset", func(t *testing.T) {
		x := resolve(t, `<div><p>y</p></div>`, `div { opacity: .5 } p { color: #000; opacity: inherit }`, "")
		if a := x.by(t, "p").Color.A; !ctNear(a, .25) {
			t.Errorf("opacity: inherit under .5 gives %v want .25", a)
		}
		x = resolve(t, `<div><p>y</p></div>`, `div { opacity: .5 } p { color: #000; opacity: initial }`, "")
		if a := x.by(t, "p").Color.A; !ctNear(a, .5) {
			t.Errorf("opacity: initial gives %v want .5 (parent's only)", a)
		}
		x = resolve(t, `<div><p>y</p></div>`, `div { opacity: .5 } p { color: #000; opacity: unset }`, "")
		if a := x.by(t, "p").Color.A; !ctNear(a, .5) {
			t.Errorf("opacity: unset gives %v want .5 (not inherited)", a)
		}
	})
	t.Run("currentColor and inheritance use the raw colour", func(t *testing.T) {
		x := resolve(t, `<div><p><b>y</b></p></div>`,
			`div { opacity: .5; color: rgb(255, 0, 0) } p { border: 1px solid currentColor; background: currentColor } b { border: 1px solid; background-color: inherit }`, "")
		p, b := x.by(t, "p"), x.by(t, "b")
		if !ctNear(p.Border[Top].Color.A, .5) || !ctNear(p.BackgroundColor.A, .5) {
			t.Errorf("p border %v bg %v want .5", p.Border[Top].Color.A, p.BackgroundColor.A)
		}
		if !ctNear(b.Color.A, .5) || !ctNear(b.Border[Top].Color.A, .5) {
			t.Errorf("b colour %v border %v want .5", b.Color.A, b.Border[Top].Color.A)
		}
	})
	t.Run("zero and deep nesting", func(t *testing.T) {
		x := resolve(t, `<div><p><span>y</span></p></div>`, `div { opacity: 0; color: #000 } p { opacity: 1 }`, "")
		if x.by(t, "span").Color.A != 0 {
			t.Error("opacity 0 must propagate to descendants")
		}
		var sb strings.Builder
		for range 200 {
			sb.WriteString("<div>")
		}
		sb.WriteString("x")
		x = resolve(t, sb.String(), `div { opacity: .99; color: #000 }`, "")
		last := x.styles[len(x.styles)-1]
		if want := math.Pow(.99, 200); !ctNear(last.Color.A, want) {
			t.Errorf("deep product %v want %v", last.Color.A, want)
		}
	})
	t.Run("one warning for gradient under opacity", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { opacity: .5; background: linear-gradient(red, blue) }`, "")
		n := 0
		w, _ := x.b.Warnings()
		for _, v := range w {
			if v.Detail == "opacity on gradient" {
				n++
			}
		}
		if n < 1 {
			t.Errorf("no gradient warning: %v", x.warnings())
		}
		x = resolve(t, `<div><p>x</p></div>`, `div { background: linear-gradient(red, blue) }`, "")
		if x.has("unsupported-property: opacity") {
			t.Error("gradient without opacity must not warn")
		}
		x = resolve(t, `<div>x</div>`, `div { opacity: 1; background: linear-gradient(red, blue) }`, "")
		if x.has("unsupported-property: opacity") {
			t.Error("opacity 1 must not warn")
		}
	})
}

func TestContractBlockification(t *testing.T) {
	for _, parent := range []string{"flex", "grid"} {
		x := resolve(t, `<div><span><i>a</i></span><b class="ib"><i>b</i></b><li>c</li><table><tr><td>d</td></tr></table><em class="n">e</em></div>`,
			`div { display: `+parent+` } .ib { display: inline-block } .n { display: none }`, "")
		if d := x.by(t, "div").Display; d != map[string]Display{"flex": DisplayFlex, "grid": DisplayGrid}[parent] {
			t.Errorf("%s container display %v", parent, d)
		}
		if d := x.by(t, "span").Display; d != DisplayBlock {
			t.Errorf("%s: span %v want block", parent, d)
		}
		if d := x.by(t, "b").Display; d != DisplayBlock {
			t.Errorf("%s: inline-block %v want block", parent, d)
		}
		if d := x.by(t, "i").Display; d != DisplayInline {
			t.Errorf("%s: grandchild %v must stay inline", parent, d)
		}
		if d := x.by(t, "li").Display; d != DisplayListItem {
			t.Errorf("%s: list-item %v must be kept", parent, d)
		}
		if d := x.by(t, "table").Display; d != DisplayTable {
			t.Errorf("%s: table %v must be kept", parent, d)
		}
		if d := x.by(t, "td").Display; d != DisplayTableCell {
			t.Errorf("%s: td %v", parent, d)
		}
		if d := x.by(t, "em").Display; d != DisplayNone {
			t.Errorf("%s: display none %v must be kept", parent, d)
		}
	}
	t.Run("nested containers", func(t *testing.T) {
		x := resolve(t, `<div><section><span>a</span></section></div>`, `div { display: flex } section { display: grid }`, "")
		if x.by(t, "section").Display != DisplayGrid || x.by(t, "span").Display != DisplayBlock {
			t.Errorf("section %v span %v", x.by(t, "section").Display, x.by(t, "span").Display)
		}
	})
	t.Run("not blockified outside a container", func(t *testing.T) {
		x := resolve(t, `<div><span>a</span></div>`, `div { display: block } span { display: inline-block }`, "")
		if d := x.by(t, "span").Display; d != DisplayInlineBlock {
			t.Errorf("%v", d)
		}
		x = resolve(t, `<div><span>a</span></div>`, `div { display: inline-block }`, "")
		if d := x.by(t, "span").Display; d != DisplayInline {
			t.Errorf("child of inline-block %v must stay inline", d)
		}
	})
	t.Run("display decided after a later rule", func(t *testing.T) {
		x := resolve(t, `<div><span id="s">a</span></div>`, `#s { display: inline } div { display: flex } span { display: inline-block !important }`, "")
		if d := x.by(t, "span").Display; d != DisplayBlock {
			t.Errorf("%v", d)
		}
	})
	t.Run("flex with inherit", func(t *testing.T) {
		x := resolve(t, `<div><span>a</span></div>`, `div { display: flex } span { display: inherit }`, "")
		if d := x.by(t, "span").Display; d != DisplayFlex {
			t.Errorf("display: inherit from a flex parent: %v", d)
		}
	})
}

func TestContractGridColumns(t *testing.T) {
	px := func(v float64) Track { return Track{Kind: TrackPx, Value: v} }
	fr := func(v float64) Track { return Track{Kind: TrackFr, Value: v} }
	tests := []struct {
		decl string
		want []Track
	}{
		{"grid-template-columns: none", nil},
		{"grid-template-columns: 1fr", []Track{fr(1)}},
		{"grid-template-columns: 1fr 2fr", []Track{fr(1), fr(2)}},
		{"grid-template-columns: 0.5fr", []Track{fr(.5)}},
		{"grid-template-columns: 100px 50% auto min-content max-content", []Track{px(100), {Kind: TrackPercent, Value: 50}, {Kind: TrackAuto}, {Kind: TrackMinContent}, {Kind: TrackMaxContent}}},
		{"grid-template-columns: max-content 1fr", []Track{{Kind: TrackMaxContent}, fr(1)}},
		{"grid-template-columns: repeat(4, 1fr)", []Track{fr(1), fr(1), fr(1), fr(1)}},
		{"grid-template-columns: repeat(2, 10px 1fr)", []Track{px(10), fr(1), px(10), fr(1)}},
		{"grid-template-columns: 5px repeat(2, 1fr) 7px", []Track{px(5), fr(1), fr(1), px(7)}},
		{"grid-template-columns: repeat(1, auto)", []Track{{Kind: TrackAuto}}},
		{"grid-template-columns: repeat(2, 1fr) repeat(2, 2px)", []Track{fr(1), fr(1), px(2), px(2)}},
		{"grid-template-columns: 2em", []Track{px(32)}},
		{"grid-template-columns: 0", []Track{px(0)}},
		{"grid-template-columns: 0fr", []Track{fr(0)}},
		{"grid-template-columns: REPEAT(2, 1FR)", []Track{fr(1), fr(1)}},
		{"grid-template-columns: repeat( 2 , 1fr )", []Track{fr(1), fr(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := ctOne(t, "font-size: 16px; display: grid; "+tt.decl)
			if len(c.GridColumns) != len(tt.want) {
				t.Fatalf("tracks %+v want %+v (warnings %v)", c.GridColumns, tt.want, x.warnings())
			}
			for i := range tt.want {
				if c.GridColumns[i].Kind != tt.want[i].Kind || !ctNear(c.GridColumns[i].Value, tt.want[i].Value) {
					t.Errorf("track %d: %+v want %+v", i, c.GridColumns[i], tt.want[i])
				}
			}
			if len(x.warnings()) != 0 {
				t.Errorf("warnings: %v", x.warnings())
			}
		})
	}

	invalid := []string{
		"grid-template-columns: repeat(0, 1fr)", "grid-template-columns: repeat(-1, 1fr)", "grid-template-columns: repeat(-100000, 1fr)",
		"grid-template-columns: repeat(1.5, 1fr)", "grid-template-columns: repeat(auto-fill, 1fr)", "grid-template-columns: repeat(auto-fit, 100px)",
		"grid-template-columns: repeat(2)", "grid-template-columns: repeat(2,)", "grid-template-columns: repeat(, 1fr)", "grid-template-columns: repeat(2 1fr)",
		"grid-template-columns: repeat(2, repeat(2, 1fr))", "grid-template-columns: repeat(2, 1fr", "grid-template-columns: repeat(1fr, 2)",
		"grid-template-columns: repeat(2px, 1fr)", "grid-template-columns: repeat(2, minmax(1px, 1fr))",
		"grid-template-columns: minmax(10px, 1fr)", "grid-template-columns: fit-content(10px)", "grid-template-columns: [a] 1fr", "grid-template-columns: 1fr [b]",
		"grid-template-columns: -1fr", "grid-template-columns: -5px", "grid-template-columns: 1fr / 1fr", "grid-template-columns: foo",
		"grid-template-columns: none 1fr", "grid-template-columns: auto-fill", "grid-template-columns: 10", "grid-template-columns: 1deg",
		"grid-template-columns:", "grid-template-columns: 1fr,2fr", "grid-template-columns: repeat(2, none)",
	}
	for _, decl := range invalid {
		t.Run(decl, func(t *testing.T) {
			c, x := ctOne(t, decl)
			if c.GridColumns != nil {
				t.Errorf("applied: %+v", c.GridColumns)
			}
			if len(x.warnings()) == 0 {
				t.Errorf("no invalid-value warning: %v", x.warnings())
			}
		})
	}

	t.Run("invalid keeps the previous value", func(t *testing.T) {
		c, _ := ctOne(t, "grid-template-columns: 1fr 1fr; grid-template-columns: repeat(0, 1fr)")
		if len(c.GridColumns) != 2 {
			t.Errorf("%+v", c.GridColumns)
		}
	})
	t.Run("inherit, initial", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { grid-template-columns: 1fr 2fr } p { grid-template-columns: inherit }`, "")
		if len(x.by(t, "p").GridColumns) != 2 {
			t.Error("inherit")
		}
		x = resolve(t, `<div><p>x</p></div>`, `div { grid-template-columns: 1fr 2fr } p { grid-template-columns: initial }`, "")
		if x.by(t, "p").GridColumns != nil {
			t.Error("initial")
		}
		x = resolve(t, `<div><p>x</p></div>`, `div { grid-template-columns: 1fr 2fr }`, "")
		if x.by(t, "p").GridColumns != nil {
			t.Error("grid-template-columns must not be inherited")
		}
	})
	t.Run("var", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `:root { --n: 3 } p { grid-template-columns: repeat(var(--n), 1fr) }`, "")
		if n := len(x.by(t, "p").GridColumns); n != 3 {
			t.Errorf("%d tracks", n)
		}
	})
	t.Run("em resolves against the element", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { font-size: 10px } p { font-size: 20px; grid-template-columns: 2em }`, "")
		if g := x.by(t, "p").GridColumns; len(g) != 1 || !ctNear(g[0].Value, 40) {
			t.Errorf("%+v", g)
		}
	})
}

func TestContractGridTrackBounds(t *testing.T) {
	for _, tt := range []struct {
		n     string
		want  int
		clamp bool
	}{
		{"1", 1, false},
		{"999", 999, false},
		{"1000", 1000, false},
		{"1001", 1000, true},
		{"100000", 1000, true},
		{"2147483647", 1000, true},
		{"4294967296", 1000, true},
		{"99999999999999999999", 1000, true},
	} {
		t.Run(tt.n, func(t *testing.T) {
			c, x := ctOne(t, "grid-template-columns: repeat("+tt.n+", 1fr)")
			if len(c.GridColumns) != tt.want {
				t.Fatalf("%d tracks want %d (warnings %v)", len(c.GridColumns), tt.want, x.warnings())
			}
			if got := x.has("clamped: grid tracks"); got != tt.clamp {
				t.Errorf("clamp warning %v want %v: %v", got, tt.clamp, x.warnings())
			}
			if tt.want == 0 {
				return
			}
			if c.GridColumns[tt.want-1] != (Track{Kind: TrackFr, Value: 1}) {
				t.Errorf("last track %+v", c.GridColumns[tt.want-1])
			}
		})
	}
	t.Run("group of 3 tracks clamps to the limit", func(t *testing.T) {
		c, x := ctOne(t, "grid-template-columns: repeat(100000, 1px 2px 3px)")
		if len(c.GridColumns) > MaxGridTracks || len(c.GridColumns) < MaxGridTracks-2 || !x.has("clamped: grid tracks") {
			t.Errorf("%d tracks, warnings %v", len(c.GridColumns), x.warnings())
		}
		if c.GridColumns[0].Value != 1 || c.GridColumns[1].Value != 2 || c.GridColumns[2].Value != 3 || c.GridColumns[3].Value != 1 {
			t.Errorf("pattern broken: %+v", c.GridColumns[:4])
		}
	})
	t.Run("nested total is bounded", func(t *testing.T) {
		c, x := ctOne(t, "grid-template-columns: repeat(100000, 1fr) repeat(100000, 2fr) 5px")
		if len(c.GridColumns) > MaxGridTracks {
			t.Errorf("%d tracks", len(c.GridColumns))
		}
		if !x.has("clamped: grid tracks") {
			t.Errorf("no warning: %v", x.warnings())
		}
	})
	t.Run("many plain tracks are bounded", func(t *testing.T) {
		c, _ := ctOne(t, "grid-template-columns:"+strings.Repeat(" 1fr", 5000))
		if len(c.GridColumns) > MaxGridTracks {
			t.Errorf("%d tracks", len(c.GridColumns))
		}
	})
	t.Run("many repeat groups are bounded", func(t *testing.T) {
		c, _ := ctOne(t, "grid-template-columns:"+strings.Repeat(" repeat(100000, 1fr)", 2000))
		if len(c.GridColumns) > MaxGridTracks {
			t.Errorf("%d tracks", len(c.GridColumns))
		}
	})
	t.Run("shared slice across styles", func(t *testing.T) {
		x := resolve(t, `<p>a</p><p>b</p>`, `p { grid-template-columns: repeat(1000, 1fr) }`, "")
		var ps []*Computed
		for i, e := range x.d.Elements {
			if e.Tag.String() == "p" {
				ps = append(ps, x.styles[i])
			}
		}
		if len(ps) != 2 || len(ps[0].GridColumns) != 1000 || len(ps[1].GridColumns) != 1000 {
			t.Fatalf("%d p", len(ps))
		}
	})
}

func TestContractGridRepeatMemory(t *testing.T) {
	const runs = 20
	for _, n := range []string{"1000", "1001", "100000", "2147483647"} {
		src := `<p>x</p>`
		css := `p { grid-template-columns: repeat(` + n + `, 1fr) }`
		x := resolve(t, src, css, "")
		_ = x
		var m0, m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m0)
		for range runs {
			resolve(t, `<p>x</p>`, css, "")
		}
		runtime.ReadMemStats(&m1)
		perRun := (m1.TotalAlloc - m0.TotalAlloc) / runs
		// A clamped repeat() expands to at most 1000 tracks of 16 bytes (16 KB) once per
		// distinct style; the whole run must stay far from what the count claims.
		if perRun > 8<<20 {
			t.Errorf("repeat(%s): %d bytes per full resolve", n, perRun)
		}
	}
}

func TestContractFlexShorthand(t *testing.T) {
	auto := Dim{Kind: DimAuto}
	pct0 := Dim{Kind: DimPercent, Value: 0}
	tests := []struct {
		decl   string
		grow   float64
		shrink float64
		basis  Dim
	}{
		{"flex: none", 0, 0, auto},
		{"flex: auto", 1, 1, auto},
		{"flex: initial", 0, 1, auto},
		{"flex: 1", 1, 1, pct0},
		{"flex: 2", 2, 1, pct0},
		{"flex: 0", 0, 1, pct0},
		{"flex: 0.5", .5, 1, pct0},
		{"flex: 1 2", 1, 2, pct0},
		{"flex: 1 0", 1, 0, pct0},
		{"flex: 0 0 auto", 0, 0, auto},
		{"flex: 1 1 auto", 1, 1, auto},
		{"flex: 1 1 0", 1, 1, Dim{Kind: DimPx}},
		{"flex: 1 1 0px", 1, 1, Dim{Kind: DimPx}},
		{"flex: 2 3 40px", 2, 3, Dim{Kind: DimPx, Value: 40}},
		{"flex: 10px", 1, 1, Dim{Kind: DimPx, Value: 10}},
		{"flex: 25%", 1, 1, Dim{Kind: DimPercent, Value: 25}},
		{"flex: 1 30px", 1, 1, Dim{Kind: DimPx, Value: 30}},
		{"flex: 30px 1", 1, 1, Dim{Kind: DimPx, Value: 30}},
		{"flex: 30px 2 3", 2, 3, Dim{Kind: DimPx, Value: 30}},
		{"flex: 1 2 30px", 1, 2, Dim{Kind: DimPx, Value: 30}},
		{"flex: auto 1", 1, 1, auto},
		{"flex: 1 auto", 1, 1, auto},
		{"flex: 3 auto", 3, 1, auto},
		{"flex: 0 auto", 0, 1, auto},
		{"flex: 1e3", 1000, 1, pct0},
		{"flex: 99999999 1", 1e6, 1, pct0},
		{"flex: AUTO", 1, 1, auto},
		{"flex: NONE", 0, 0, auto},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := ctOne(t, tt.decl)
			if !ctNear(c.FlexGrow, tt.grow) || !ctNear(c.FlexShrink, tt.shrink) || c.FlexBasis.Kind != tt.basis.Kind || !ctNear(c.FlexBasis.Value, tt.basis.Value) {
				t.Errorf("grow %v shrink %v basis %+v; want %v %v %+v", c.FlexGrow, c.FlexShrink, c.FlexBasis, tt.grow, tt.shrink, tt.basis)
			}
			if w := x.warnings(); len(w) != 0 && tt.grow != 1e6 {
				t.Errorf("warnings: %v", w)
			}
		})
	}
	t.Run("flex-grow over 1e6 clamps with a warning", func(t *testing.T) {
		c, x := ctOne(t, "flex-grow: 1e9; flex-shrink: 5e9")
		if c.FlexGrow != 1e6 || c.FlexShrink != 1e6 {
			t.Errorf("%v %v", c.FlexGrow, c.FlexShrink)
		}
		if !x.has("clamped: flex-grow") || !x.has("clamped: flex-shrink") {
			t.Errorf("warnings: %v", x.warnings())
		}
	})
	for _, decl := range []string{
		"flex:", "flex: 1 2 3", "flex: 1 1 1 1", "flex: -1", "flex: 1 -1", "flex: -1px", "flex: 1px 2px", "flex: 1 auto auto",
		"flex: none 1", "flex: auto none", "flex: initial 1", "flex: foo", "flex: 1 foo", "flex: content", "flex: 1 1 content", "flex: 1, 2",
		"flex: 1 / 2", "flex: 1fr", "flex: 1 1 1fr", "flex: calc(", "flex: 1 1px 2", "flex: 1 2px 3", "flex: 1deg", "flex: 1 1 -1px", "flex: #fff",
		"flex: nan", "flex: \"1\"",
	} {
		t.Run(decl, func(t *testing.T) {
			c, x := ctOne(t, "flex-grow: 3; "+decl)
			if c.FlexGrow != 3 || c.FlexShrink != 1 || c.FlexBasis != (Dim{Kind: DimAuto}) {
				t.Errorf("an invalid flex touched longhands: %v %v %+v", c.FlexGrow, c.FlexShrink, c.FlexBasis)
			}
			if len(x.warnings()) == 0 {
				t.Errorf("no warning: %v", x.warnings())
			}
		})
	}
	t.Run("longhand after shorthand wins", func(t *testing.T) {
		c, _ := ctOne(t, "flex: 1; flex-basis: auto; flex-shrink: 0")
		if c.FlexGrow != 1 || c.FlexShrink != 0 || c.FlexBasis.Kind != DimAuto {
			t.Errorf("%v %v %+v", c.FlexGrow, c.FlexShrink, c.FlexBasis)
		}
	})
	t.Run("shorthand resets earlier longhands", func(t *testing.T) {
		c, _ := ctOne(t, "flex-grow: 4; flex-shrink: 4; flex-basis: 10px; flex: none")
		if c.FlexGrow != 0 || c.FlexShrink != 0 || c.FlexBasis.Kind != DimAuto {
			t.Errorf("%v %v %+v", c.FlexGrow, c.FlexShrink, c.FlexBasis)
		}
	})
	t.Run("not inherited, inherit and initial", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { flex: 3 4 5px }`, "")
		if p := x.by(t, "p"); p.FlexGrow != 0 || p.FlexShrink != 1 || p.FlexBasis.Kind != DimAuto {
			t.Errorf("inherited: %+v", p)
		}
		x = resolve(t, `<div><p>x</p></div>`, `div { flex: 3 4 5px } p { flex: inherit }`, "")
		if p := x.by(t, "p"); p.FlexGrow != 3 || p.FlexShrink != 4 || p.FlexBasis.Value != 5 {
			t.Errorf("inherit: %v %v %+v", p.FlexGrow, p.FlexShrink, p.FlexBasis)
		}
		x = resolve(t, `<div><p>x</p></div>`, `p { flex: 3 4 5px; flex: initial }`, "")
		if p := x.by(t, "p"); p.FlexGrow != 0 || p.FlexShrink != 1 || p.FlexBasis.Kind != DimAuto {
			t.Errorf("initial: %+v", p)
		}
	})
	t.Run("var", func(t *testing.T) {
		x := resolve(t, `<p>x</p>`, `:root { --g: 2 } p { flex: var(--g) }`, "")
		if p := x.by(t, "p"); p.FlexGrow != 2 {
			t.Errorf("%v", p.FlexGrow)
		}
	})
}

func TestContractGap(t *testing.T) {
	for decl, want := range map[string][2]float64{
		"gap: 4px": {4, 4}, "gap: 4px 8px": {4, 8}, "gap: normal": {0, 0}, "gap: normal 3px": {0, 3},
		"row-gap: 5px": {5, 0}, "column-gap: 5px": {0, 5}, "column-gap: normal": {0, 0}, "gap: 1em": {16, 16}, "gap: 0": {0, 0},
	} {
		c, x := ctOne(t, "font-size: 16px; "+decl)
		if c.RowGap.Kind != DimPx || c.ColumnGap.Kind != DimPx || !ctNear(c.RowGap.Value, want[0]) || !ctNear(c.ColumnGap.Value, want[1]) {
			t.Errorf("%s: %+v %+v want %v", decl, c.RowGap, c.ColumnGap, want)
		}
		if len(x.warnings()) != 0 {
			t.Errorf("%s: %v", decl, x.warnings())
		}
	}
	for _, decl := range []string{"gap:", "gap: 1 2", "gap: -1px", "gap: 1px 2px 3px", "gap: auto", "gap: 1fr", "row-gap: -0.1px", "gap: foo"} {
		c, x := ctOne(t, decl)
		if c.RowGap != (Dim{}) || c.ColumnGap != (Dim{}) {
			t.Errorf("%s applied", decl)
		}
		if !x.has("invalid-value: ") {
			t.Errorf("%s: no warning", decl)
		}
	}
}

func TestContractTextTransformAndNowrap(t *testing.T) {
	x := resolve(t, `<div><p><b>x</b></p></div>`, `div { text-transform: uppercase; white-space: nowrap } p { }`, "")
	for _, n := range []string{"div", "p", "b"} {
		c := x.by(t, n)
		if c.TextTransform != TransformUppercase || c.WhiteSpace != WhiteSpaceNowrap {
			t.Errorf("%s: transform %v white-space %v not inherited", n, c.TextTransform, c.WhiteSpace)
		}
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { text-transform: uppercase } p { text-transform: none }`, "")
	if x.by(t, "p").TextTransform != TransformNone {
		t.Error("none overrides")
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { text-transform: uppercase } p { text-transform: initial }`, "")
	if x.by(t, "p").TextTransform != TransformNone {
		t.Error("initial")
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { text-transform: lowercase } p { text-transform: unset }`, "")
	if x.by(t, "p").TextTransform != TransformLowercase {
		t.Error("unset inherits")
	}
	x = resolve(t, `<div><pre>x</pre></div>`, `div { white-space: nowrap }`, "")
	if x.by(t, "pre").WhiteSpace != WhiteSpacePre {
		t.Error("UA pre rule must beat inheritance")
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { white-space: nowrap } p { white-space: normal }`, "")
	if x.by(t, "p").WhiteSpace != WhiteSpaceNormal {
		t.Error("normal overrides")
	}
	c, _ := ctOne(t, "text-transform: UPPERCASE; white-space: NOWRAP")
	if c.TextTransform != TransformUppercase || c.WhiteSpace != WhiteSpaceNowrap {
		t.Error("keywords are case-insensitive")
	}
	for _, decl := range []string{
		"text-transform: capitalize", "text-transform: uppercase lowercase", "text-transform: 1", "text-transform:", "text-transform: upper",
		"white-space: pre-wrap", "white-space: pre-line", "white-space: break-spaces", "white-space: nowrap pre", "white-space: wrap", "white-space: 0",
		"font-variant-numeric: lining-nums", "font-variant-numeric: normal tabular-nums", "font-variant-numeric: 1",
	} {
		c, x := ctOne(t, decl)
		if c.TextTransform != TransformNone || c.WhiteSpace != WhiteSpaceNormal || c.TabularNums {
			t.Errorf("%s applied", decl)
		}
		if !x.has("invalid-value: ") {
			t.Errorf("%s: no warning: %v", decl, x.warnings())
		}
	}
	c, x2 := ctOne(t, "font-variant-numeric: tabular-nums")
	if !c.TabularNums || len(x2.warnings()) != 0 {
		t.Errorf("tabular-nums: %v %v", c.TabularNums, x2.warnings())
	}
}

func TestContractBreaks(t *testing.T) {
	before := map[string]Break{
		"auto": BreakAuto, "avoid": BreakAvoid, "avoid-page": BreakAvoid, "page": BreakPage, "left": BreakPage,
		"right": BreakPage, "recto": BreakPage, "verso": BreakPage, "PAGE": BreakPage, "Avoid": BreakAvoid,
	}
	for v, want := range before {
		for _, prop := range []string{"break-before", "break-after"} {
			c, x := ctOne(t, prop+": "+v)
			got := c.BreakBefore
			if prop == "break-after" {
				got = c.BreakAfter
			}
			if got != want || len(x.warnings()) != 0 {
				t.Errorf("%s: %s -> %v want %v (%v)", prop, v, got, want, x.warnings())
			}
		}
	}
	for v, want := range map[string]Break{"auto": BreakAuto, "avoid": BreakAvoid, "avoid-page": BreakAvoid} {
		if c, _ := ctOne(t, "break-inside: "+v); c.BreakInside != want {
			t.Errorf("break-inside %s -> %v", v, c.BreakInside)
		}
	}
	c, _ := ctOne(t, "break-before: page")
	if c.BreakAfter != BreakAuto || c.BreakInside != BreakAuto {
		t.Error("break-before must not touch the others")
	}
	for _, decl := range []string{
		"break-before: column", "break-before: avoid-column", "break-before: region", "break-before: avoid-region", "break-before: always",
		"break-before: page page", "break-before: 1", "break-before:", "break-after: column", "break-after: avoid-region",
		"break-inside: page", "break-inside: left", "break-inside: column", "break-inside: avoid-column", "break-inside: always",
	} {
		c, x := ctOne(t, decl)
		if c.BreakBefore != BreakAuto || c.BreakAfter != BreakAuto || c.BreakInside != BreakAuto {
			t.Errorf("%s applied", decl)
		}
		if !x.has("invalid-value: ") {
			t.Errorf("%s: no warning: %v", decl, x.warnings())
		}
	}
	t.Run("not inherited", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { break-before: page; break-after: avoid; break-inside: avoid }`, "")
		if p := x.by(t, "p"); p.BreakBefore != BreakAuto || p.BreakAfter != BreakAuto || p.BreakInside != BreakAuto {
			t.Errorf("%+v", p)
		}
	})
	t.Run("inherit, initial, unset", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { break-before: page } p { break-before: inherit }`, "")
		if x.by(t, "p").BreakBefore != BreakPage {
			t.Error("inherit")
		}
		x = resolve(t, `<div><p>x</p></div>`, `p { break-before: page; break-before: initial }`, "")
		if x.by(t, "p").BreakBefore != BreakAuto {
			t.Error("initial")
		}
		x = resolve(t, `<div><p>x</p></div>`, `div { break-before: page } p { break-before: unset }`, "")
		if x.by(t, "p").BreakBefore != BreakAuto {
			t.Error("unset on a non-inherited property is initial")
		}
	})
	t.Run("page-break aliases are not added", func(t *testing.T) {
		c, x := ctOne(t, "page-break-before: always")
		if c.BreakBefore != BreakAuto || !x.has("unsupported-property: ") {
			t.Errorf("%v %v", c.BreakBefore, x.warnings())
		}
	})
}

func TestContractDisplayAndFlexKeywords(t *testing.T) {
	for decl, want := range map[string]Display{
		"display: inline-block": DisplayInlineBlock, "display: flex": DisplayFlex, "display: grid": DisplayGrid, "display: INLINE-BLOCK": DisplayInlineBlock,
	} {
		if c, x := ctOne(t, decl); c.Display != want || len(x.warnings()) != 0 {
			t.Errorf("%s: %v %v", decl, c.Display, x.warnings())
		}
	}
	for _, decl := range []string{"display: inline-flex", "display: inline-grid", "display: flow-root", "display: contents", "display: flex inline", "display: block flex", "display: -webkit-box"} {
		c, x := ctOne(t, "display: block; "+decl)
		if c.Display != DisplayBlock {
			t.Errorf("%s applied: %v", decl, c.Display)
		}
		if !x.has("invalid-value: display: ") && !x.has("unsupported-property: display") {
			t.Errorf("%s: no warning: %v", decl, x.warnings())
		}
	}
	for _, decl := range []string{"flex-direction: column", "flex-direction: row-reverse", "flex-direction: column-reverse", "flex-wrap: wrap-reverse", "flex-flow: row wrap"} {
		_, x := ctOne(t, decl)
		if len(x.warnings()) == 0 {
			t.Errorf("%s: silently accepted", decl)
		}
	}
	if _, x := ctOne(t, "flex-direction: row"); len(x.warnings()) != 0 {
		t.Errorf("flex-direction: row warns: %v", x.warnings())
	}
	if c, _ := ctOne(t, "align-items: start"); c.AlignItems != ItemStart {
		t.Error("align-items: start")
	}
	if c, _ := ctOne(t, "align-items: end"); c.AlignItems != ItemEnd {
		t.Error("align-items: end")
	}
	if c, _ := ctOne(t, "align-items: self-end"); c.AlignItems != ItemEnd {
		t.Error("align-items: self-end")
	}
	if c, _ := ctOne(t, "align-items: flex-start"); c.AlignItems != ItemStart {
		t.Error("align-items: flex-start")
	}
}

func TestContractHostileValues(t *testing.T) {
	huge := strings.Repeat("9", 400)
	long := strings.Repeat("a", 1<<16)
	decls := []string{
		"font: " + huge + "px serif", "font: 12px/" + huge + " serif", "font: " + huge + " 12px serif", "font: 1e999px serif", "font: nanpx serif",
		"font: 12px " + long, "font: 12px \"" + long + "\"", "font: " + strings.Repeat("bold ", 5000) + "12px serif",
		"font: 12px " + strings.Repeat("a,", 100000) + "b", "font: " + strings.Repeat("(", 10000) + "12px serif",
		"font: 12px/" + strings.Repeat("(", 10000), "font: 12px serif" + strings.Repeat(")", 10000),
		"opacity: " + huge, "opacity: " + huge + "%", "opacity: 0." + huge, "opacity: " + long,
		"flex: " + huge, "flex: " + huge + " " + huge + " " + huge + "px", "flex: 1 1 1e999px", "flex: " + strings.Repeat("1 ", 100000),
		"flex-grow: " + huge, "flex-basis: " + huge + "px", "flex-basis: 1e999%", "gap: " + huge + "px", "gap: 1e999px 1e999px", "row-gap: " + huge + "em",
		"grid-template-columns: " + huge + "px", "grid-template-columns: " + huge + "fr", "grid-template-columns: repeat(" + huge + ", 1fr)",
		"grid-template-columns: repeat(2, " + strings.Repeat("1fr ", 100000) + ")",
		"grid-template-columns: " + strings.Repeat("repeat(", 5000) + "1fr" + strings.Repeat(")", 5000),
		"grid-template-columns: " + long, "grid-template-columns: repeat(" + long + ", 1fr)", "grid-template-columns: repeat(2, " + long + ")",
		"grid-template-columns: 1e999px 1e999fr", "grid-template-columns: 1e-999fr", "grid-template-columns: " + strings.Repeat("1fr ", 1<<14),
		"break-before: " + long, "text-transform: " + long, "white-space: " + long, "display: " + long, "justify-content: " + long, "align-items: " + long,
		"flex: \x00", "font: \x00 12px serif", "opacity: \x00", "grid-template-columns: repeat(\x00, 1fr)", "font: 12px \"\x00\"",
		"font: 12px serif !important !important", "opacity: .5 !important", "flex: 1 !important",
	}
	for _, d := range decls {
		name := d
		if len(name) > 40 {
			name = name[:40]
		}
		t.Run(name, func(t *testing.T) {
			c, _ := ctOne(t, "color: #000; background: #fff; border: 1px solid; "+d)
			for _, v := range []float64{c.FontSize, c.FlexGrow, c.FlexShrink, c.FlexBasis.Value, c.RowGap.Value, c.ColumnGap.Value, c.LineHeight.Value, c.Color.A, c.BackgroundColor.A, c.Border[Top].Color.A} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("non-finite value in %+v", c)
				}
			}
			if c.FlexGrow < 0 || c.FlexGrow > 1e6 || c.FlexShrink < 0 || c.FlexShrink > 1e6 {
				t.Errorf("flex factors out of range: %v %v", c.FlexGrow, c.FlexShrink)
			}
			if c.Color.A < 0 || c.Color.A > 1 {
				t.Errorf("alpha %v", c.Color.A)
			}
			if len(c.GridColumns) > MaxGridTracks {
				t.Errorf("%d tracks", len(c.GridColumns))
			}
			if c.FontSize < 0 {
				t.Errorf("negative font size %v", c.FontSize)
			}
		})
	}
}

func TestContractShorthandLongestDeclarationsStayBounded(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("p {")
	for range 2000 {
		sb.WriteString("font: bold 12px serif; flex: 1 1 0; opacity: .9; grid-template-columns: repeat(100000, 1fr); gap: 1px 2px;")
	}
	sb.WriteString("}")
	x := resolve(t, `<p>x</p>`, sb.String(), "")
	c := x.by(t, "p")
	if c.FontSize != 16 && c.FontSize != 12 {
		t.Errorf("font size %v", c.FontSize)
	}
	if len(c.GridColumns) != MaxGridTracks {
		t.Errorf("%d tracks", len(c.GridColumns))
	}
}

func TestContractM2PropertiesInExtraCSSAndInline(t *testing.T) {
	x := resolve(t, `<p style="opacity: .5; font: bold 20px serif; flex: 2">x</p>`, `p { opacity: 1; font: 10px serif; flex: 1 }`, `p { font-size: 30px }`)
	p := x.by(t, "p")
	if p.FontSize != 30 {
		t.Errorf("ExtraCSS must win over the inline style: font-size %v", p.FontSize)
	}
	if p.FontWeight != 700 || p.FlexGrow != 2 || !ctNear(p.Color.A, .5) {
		t.Errorf("inline style must win over the sheet: %+v", p)
	}
}

// CSS Grid 2 §7.2.3.1: the repeat() count is an <integer>; "2.0" and "1e3" are
// <number> tokens in CSS Syntax 3 and Chrome rejects them.
func TestContractRepeatCountIsAnInteger(t *testing.T) {
	for _, n := range []string{"2.0", "1e3", "2e0", "1.0"} {
		c, x := ctOne(t, "grid-template-columns: repeat("+n+", 1fr)")
		if c.GridColumns != nil || !x.has("invalid-value: grid-template-columns: ") {
			t.Errorf("repeat(%s, 1fr) applied: %d tracks, warnings %v", n, len(c.GridColumns), x.warnings())
		}
	}
}

func FuzzContractM2(f *testing.F) {
	for _, s := range []string{
		"opacity: .5; color: #123; border: 1px solid; background: #456",
		"font: italic 700 7pt/4mm Georgia, serif",
		"font: var(--f, 12px serif); --f: bold 9px monospace",
		"grid-template-columns: repeat(100000, 1fr 2px) auto",
		"grid-template-columns: repeat(1001, 1fr)",
		"flex: 1e30 -1 5px; flex: auto 3; gap: 1px 1e30px",
		"display: flex; break-before: page; text-transform: uppercase; white-space: nowrap",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, decls string) {
		if len(decls) > 4096 || strings.ContainsAny(decls, "{}") {
			return
		}
		b := newBudget()
		ss, err := css.ParseStylesheet([]byte("div, p { "+decls+" } span { opacity: .5 }"), css.OriginDocument, b)
		if err != nil {
			return
		}
		sh, err := Compile(ss, b)
		if err != nil {
			return
		}
		d, err := dom.Parse(strings.NewReader(`<div><p><span>x</span></p></div>`), b)
		if err != nil {
			return
		}
		r := NewResolver(ua(), nil, []*Sheet{sh}, printEnv, b)
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
			styles[i] = c
			for _, a := range []float64{c.Color.A, c.BackgroundColor.A, c.Border[Top].Color.A, c.Border[Left].Color.A} {
				if math.IsNaN(a) || a < 0 || a > 1 {
					t.Fatalf("alpha %v", a)
				}
			}
			if len(c.GridColumns) > MaxGridTracks {
				t.Fatalf("%d tracks", len(c.GridColumns))
			}
			if !(c.FlexGrow >= 0 && c.FlexGrow <= 1e6 && c.FlexShrink >= 0 && c.FlexShrink <= 1e6) {
				t.Fatalf("flex %v %v", c.FlexGrow, c.FlexShrink)
			}
			for _, v := range []float64{c.FontSize, c.RowGap.Value, c.ColumnGap.Value, c.FlexBasis.Value} {
				if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
					t.Fatalf("value %v", v)
				}
			}
			for _, tr := range c.GridColumns {
				if math.IsNaN(tr.Value) || math.IsInf(tr.Value, 0) || tr.Value < 0 {
					t.Fatalf("track %+v", tr)
				}
			}
		}
	})
}

func TestContractGridBombStaysBounded(t *testing.T) {
	const n = 19000
	var sb strings.Builder
	for i := range n {
		sb.WriteString(`<p style="margin-left:`)
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(`px;grid-template-columns:repeat(1000,1px)">x</p>`)
	}
	b := newBudget()
	d, err := dom.Parse(strings.NewReader(sb.String()), b)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(ua(), nil, nil, printEnv, b)
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	styles := make([]*Computed, len(d.Elements))
	var stopped error
	done := 0
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		c, err := r.Element(d, int32(i), parent)
		if err != nil {
			stopped = err
			break
		}
		styles[i] = c
		done++
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	runtime.KeepAlive(styles)
	var grown uint64
	if m1.HeapAlloc > m0.HeapAlloc {
		grown = m1.HeapAlloc - m0.HeapAlloc
	}
	t.Logf("%d styles computed, stopped: %v, live heap +%d MiB", done, stopped, grown>>20)
	if stopped == nil {
		t.Error("19000 distinct 1000-track styles must hit a budget limit")
	}
	if grown > 64<<20 {
		t.Errorf("live heap grew by %d MiB (limit 64)", grown>>20)
	}
}
