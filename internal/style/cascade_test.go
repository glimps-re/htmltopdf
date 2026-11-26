package style

import (
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
)

func rgb(r, g, b float64) css.Color { return css.Color{R: r / 255, G: g / 255, B: b / 255, A: 1} }

func TestUASheetHasNoWarning(t *testing.T) {
	b := newBudget()
	if _, err := CompileUA(b); err != nil {
		t.Fatal(err)
	}
	if w, dropped := b.Warnings(); len(w) != 0 || dropped != 0 {
		t.Fatalf("UA sheet warns: %+v", w)
	}
}

func TestUADefaults(t *testing.T) {
	x := resolve(t, `<h1>t</h1><p>p</p><a href=x>l</a><a>n</a><ul><li>i</ul><table><tr><th>h<td>c</table><b>b</b><i>i</i><img src=x><video></video><script>1</script>`, "", "")
	if c := x.by(t, "h1"); c.Display != DisplayBlock || c.FontSize != 32 || c.FontWeight != 700 || !near(c.Margin[Top].Value, 21.44) {
		t.Errorf("h1: %+v", c)
	}
	if c := x.by(t, "p"); c.Margin[Top].Value != 16 || c.Margin[Bottom].Value != 16 || c.Margin[Left].Value != 0 {
		t.Errorf("p margins: %+v", c.Margin)
	}
	if c := x.by(t, "body"); c.Margin[Left].Value != 8 {
		t.Errorf("body margin: %+v", c.Margin)
	}
	if c := x.by(t, "a"); c.TextDecoration != DecorationUnderline || c.Color != rgb(0, 0, 0xee) {
		t.Errorf("a[href]: %+v", c)
	}
	if c := x.by(t, "li"); c.Display != DisplayListItem {
		t.Errorf("li: %v", c.Display)
	}
	if c := x.by(t, "th"); c.Display != DisplayTableCell || c.FontWeight != 700 || c.TextAlign != AlignCenter || c.Padding[Top].Value != 1 {
		t.Errorf("th: %+v", c)
	}
	if c := x.by(t, "table"); c.Display != DisplayTable || c.BorderSpacing != [2]float64{2, 2} || c.BoxSizing != BorderBox {
		t.Errorf("table: %+v", c)
	}
	if c := x.by(t, "b"); c.FontWeight != 700 {
		t.Errorf("b: %d", c.FontWeight)
	}
	if c := x.by(t, "img"); c.Display != DisplayInline {
		t.Errorf("img: %v", c.Display)
	}
	if c := x.by(t, "video"); c.Display != DisplayNone {
		t.Errorf("video: %v", c.Display)
	}
	if c := x.by(t, "script"); c.Display != DisplayNone {
		t.Errorf("script: %v", c.Display)
	}
	if !x.warnings()["unsupported-element: video"] {
		t.Errorf("no warning for video: %v", x.warnings())
	}
	if x.warnings()["unsupported-element: img"] {
		t.Error("img must not warn: the box builder warns a rejected image")
	}
	if x.warnings()["unsupported-element: script"] {
		t.Error("script must not warn")
	}
}

func TestSpecificityAndOrder(t *testing.T) {
	x := resolve(t, `<p id=i class=c>x</p>`, `
		p { color: #010101 }
		.c { color: #020202 }
		p.c { color: #030303 }
		#i { color: #040404 }
		p.c { color: #050505 }
	`, "")
	if got := x.by(t, "p").Color; got != rgb(4, 4, 4) {
		t.Errorf("color = %+v", got)
	}
	x = resolve(t, `<p class=c>x</p>`, `.c { color: #010101 } p.c { color: #020202 } .c { color: #030303 }`, "")
	if got := x.by(t, "p").Color; got != rgb(2, 2, 2) {
		t.Errorf("p.c must beat a later .c: %+v", got)
	}
	x = resolve(t, `<p class=c>x</p>`, `.c { color: #010101 } .c { color: #030303 }`, "")
	if got := x.by(t, "p").Color; got != rgb(3, 3, 3) {
		t.Errorf("later rule must win: %+v", got)
	}
}

func TestImportantAndInline(t *testing.T) {
	x := resolve(t, `<p id=i style="color:#020202">x</p>`, `#i { color: #010101 }`, "")
	if got := x.by(t, "p").Color; got != rgb(2, 2, 2) {
		t.Errorf("style attribute must beat #i: %+v", got)
	}
	x = resolve(t, `<p id=i style="color:#020202">x</p>`, `p { color: #010101 !important }`, "")
	if got := x.by(t, "p").Color; got != rgb(1, 1, 1) {
		t.Errorf("!important must beat the style attribute: %+v", got)
	}
	x = resolve(t, `<p id=i style="color:#020202 !important">x</p>`, `p { color: #010101 !important }`, "")
	if got := x.by(t, "p").Color; got != rgb(2, 2, 2) {
		t.Errorf("inline !important must beat a rule !important: %+v", got)
	}
}

// ExtraCSS beats every document declaration, whatever the specificity and
// !important (design §9.1).
func TestExtraCSSIsThePriorityOrigin(t *testing.T) {
	html := `<p id=i class=c style="color:#020202 !important; font-size: 30px !important">x</p>`
	doc := `#i.c { color: #010101 !important; font-size: 20px !important } p { margin-top: 99px !important }`
	extra := `p { color: #0a0a0a; font-size: 10px }`
	x := resolve(t, html, doc, extra)
	c := x.by(t, "p")
	if c.Color != rgb(10, 10, 10) || c.FontSize != 10 {
		t.Errorf("ExtraCSS lost: color %+v size %v", c.Color, c.FontSize)
	}
	if c.Margin[Top].Value != 99 {
		t.Errorf("a property ExtraCSS does not set keeps the document's: %v", c.Margin[Top])
	}
	// Inside ExtraCSS the order alone decides, !important or not.
	x = resolve(t, `<p>x</p>`, "", `p { color: #010101 !important; color: #020202 }`)
	if got := x.by(t, "p").Color; got != rgb(2, 2, 2) {
		t.Errorf("source order inside ExtraCSS: %+v", got)
	}
	x = resolve(t, `<p>x</p>`, "", `p { color: #010101 } p { color: #020202 !important } p { color: #030303 }`)
	if got := x.by(t, "p").Color; got != rgb(3, 3, 3) {
		t.Errorf("!important has no weight inside ExtraCSS: %+v", got)
	}
}

func TestInheritance(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div>`, `div { color: #ff0000; margin-left: 5px; font-size: 20px; line-height: 1.5; font-family: Foo, "Bar Baz", sans-serif }`, "")
	p := x.by(t, "p")
	if p.Color != rgb(255, 0, 0) || p.FontSize != 20 || p.LineHeight != (LineHeight{Kind: LineHeightNumber, Value: 1.5}) {
		t.Errorf("inherited properties: %+v", p)
	}
	if p.Margin[Left].Value != 0 {
		t.Errorf("margin must not inherit: %v", p.Margin[Left])
	}
	if got := strings.Join(p.FontFamily, "|"); got != "Foo|Bar Baz|sans-serif" {
		t.Errorf("font-family = %q", got)
	}
}

func TestRelativeUnits(t *testing.T) {
	x := resolve(t, `<div><p>x</p><span>y</span></div>`, `
		html { font-size: 10px }
		div { font-size: 2em; margin-left: 1.5em; padding-top: 1rem; line-height: 150% }
		p { font-size: 50%; padding-left: 2em; margin-right: 3rem; width: 25%; letter-spacing: 0.1em }
		span { font-size: larger }
	`, "")
	div, p, span := x.by(t, "div"), x.by(t, "p"), x.by(t, "span")
	if div.FontSize != 20 || div.Margin[Left].Value != 30 || div.Padding[Top].Value != 10 {
		t.Errorf("div: size %v margin %v padding %v", div.FontSize, div.Margin[Left], div.Padding[Top])
	}
	if p.FontSize != 10 || p.Padding[Left].Value != 20 || p.Margin[Right].Value != 30 {
		t.Errorf("p: size %v padding %v margin %v", p.FontSize, p.Padding[Left], p.Margin[Right])
	}
	if p.Width != (Dim{Kind: DimPercent, Value: 25}) || !near(p.LetterSpacing, 1) {
		t.Errorf("p: width %+v spacing %v", p.Width, p.LetterSpacing)
	}
	if p.LineHeight != (LineHeight{Kind: LineHeightPx, Value: 30}) {
		t.Errorf("a computed line-height in px inherits as px: %+v", p.LineHeight)
	}
	if span.FontSize != 24 {
		t.Errorf("larger: %v", span.FontSize)
	}
}

func TestInitialInheritUnset(t *testing.T) {
	x := resolve(t, `<div><p>x</p><span>y</span><i>z</i></div>`, `
		div { color: #ff0000; margin-top: 7px; border: 1px solid }
		p { color: initial; margin-top: inherit; border-top-style: solid; border-top-width: inherit }
		span { color: unset; margin-top: unset }
		i { all: initial }
	`, "")
	p, span := x.by(t, "p"), x.by(t, "span")
	if p.Color != (css.Color{A: 1}) || p.Margin[Top].Value != 7 || p.Border[Top].Width != 1 {
		t.Errorf("p: %+v", p)
	}
	if span.Color != rgb(255, 0, 0) || span.Margin[Top].Value != 0 {
		t.Errorf("span: color %+v margin %v", span.Color, span.Margin[Top])
	}
	if !x.warnings()["unsupported-property: all"] {
		t.Errorf("warnings: %v", x.warnings())
	}
}

func TestShorthands(t *testing.T) {
	x := resolve(t, `<div id=a>x</div><div id=b>y</div>`, `
		#a { margin: 1px 2px 3px 4px; padding: 5px 6px; border: 2px solid #112233; border-radius: 8px 4px / 2px;
		     background: #445566 linear-gradient(90deg, #000, #fff) }
		#b { margin: 0 auto; padding: 1px 2px 3px; border-width: 1px 2px; border-style: solid; border-color: red;
		     border-bottom: none; border-left-color: #00ff00; background: none }
	`, "")
	a, b := x.by(t, "#a"), x.by(t, "#b")
	if a.Margin != [4]Dim{{Value: 1}, {Value: 2}, {Value: 3}, {Value: 4}} || a.Padding != [4]Dim{{Value: 5}, {Value: 6}, {Value: 5}, {Value: 6}} {
		t.Errorf("a box: %+v %+v", a.Margin, a.Padding)
	}
	for i := range a.Border {
		if a.Border[i] != (Border{Width: 2, Style: BorderSolid, Color: rgb(0x11, 0x22, 0x33)}) {
			t.Errorf("a border %d: %+v", i, a.Border[i])
		}
	}
	if a.Radius[TopLeft] != (Corner{H: Dim{Value: 8}, V: Dim{Value: 2}}) || a.Radius[TopRight] != (Corner{H: Dim{Value: 4}, V: Dim{Value: 2}}) ||
		a.Radius[BottomRight] != (Corner{H: Dim{Value: 8}, V: Dim{Value: 2}}) {
		t.Errorf("a radius: %+v", a.Radius)
	}
	if a.BackgroundColor != rgb(0x44, 0x55, 0x66) || a.BackgroundImage == nil || len(a.BackgroundImage.Stops) != 2 {
		t.Errorf("a background: %+v", a)
	}
	if b.Margin[Left].Kind != DimAuto || b.Margin[Right].Kind != DimAuto || b.Margin[Top].Value != 0 {
		t.Errorf("b margin: %+v", b.Margin)
	}
	if b.Padding != [4]Dim{{Value: 1}, {Value: 2}, {Value: 3}, {Value: 2}} {
		t.Errorf("b padding: %+v", b.Padding)
	}
	if b.Border[Top].Width != 1 || b.Border[Right].Width != 2 || b.Border[Right].Style != BorderSolid || b.Border[Bottom].Width != 0 {
		t.Errorf("b border: %+v", b.Border)
	}
	if b.Border[Left].Color != rgb(0, 255, 0) || b.Border[Top].Color != rgb(255, 0, 0) {
		t.Errorf("b border colors: %+v", b.Border)
	}
	if b.BackgroundImage != nil || b.BackgroundColor != (css.Color{}) {
		t.Errorf("b background: %+v", b)
	}
}

func TestBorderCurrentColorAndWidthKeywords(t *testing.T) {
	x := resolve(t, `<p>x</p><div>y</div>`, `p { color: #123456; border: thick solid } div { border-top: medium solid currentcolor; color: #654321; background-color: currentcolor }`, "")
	p, div := x.by(t, "p"), x.by(t, "div")
	if p.Border[Top].Color != rgb(0x12, 0x34, 0x56) || p.Border[Top].Width != 5 {
		t.Errorf("p border: %+v", p.Border[Top])
	}
	if div.Border[Top].Color != rgb(0x65, 0x43, 0x21) || div.Border[Top].Width != 3 || div.BackgroundColor != rgb(0x65, 0x43, 0x21) {
		t.Errorf("div: %+v bg %+v", div.Border[Top], div.BackgroundColor)
	}
	if div.Border[Left].Width != 0 {
		t.Errorf("a side without style has no width: %+v", div.Border[Left])
	}
}

func TestTextAndTableProperties(t *testing.T) {
	x := resolve(t, `<table id=t><tr><td id=c>x</table>`, `
		#t { border-collapse: collapse; table-layout: fixed; border-spacing: 1px 3px; width: 100%; max-width: 900px; word-wrap: break-word; overflow: hidden }
		#c { vertical-align: top; text-align: right; white-space: pre; text-decoration: underline; font: x }
		#t { font-weight: 600; font-style: italic }
	`, "")
	tb, c := x.by(t, "#t"), x.by(t, "#c")
	if !tb.BorderCollapse || !tb.TableLayoutFixed || tb.BorderSpacing != [2]float64{1, 3} || tb.Width != (Dim{Kind: DimPercent, Value: 100}) ||
		tb.MaxWidth != (Dim{Value: 900}) || tb.OverflowWrap != WrapBreakWord || !tb.OverflowHidden || tb.FontWeight != 600 || tb.FontStyle != FontItalic {
		t.Errorf("table: %+v", tb)
	}
	if c.VerticalAlign != VAlignTop || c.TextAlign != AlignRight || c.WhiteSpace != WhiteSpacePre || c.TextDecoration != DecorationUnderline {
		t.Errorf("cell: %+v", c)
	}
	// Inherited from the table through tr.
	if !c.BorderCollapse || c.OverflowWrap != WrapBreakWord || c.FontWeight != 600 {
		t.Errorf("cell inheritance: %+v", c)
	}
	if c.OverflowHidden {
		t.Error("overflow must not inherit")
	}
}

func TestInvalidAndUnsupportedDeclarations(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { color: #010101; color: nonsense; margin: 1px 2px 3px 4px 5px; padding: -1px; width: -5px;
		display: inline-flex; box-shadow: 0; foo: bar; font-weight: 1500; float: left }`, "")
	p := x.by(t, "p")
	if p.Color != rgb(1, 1, 1) {
		t.Errorf("an invalid declaration is dropped: %+v", p.Color)
	}
	if p.Margin[Top].Value != 16 || p.Padding[Top].Value != 0 || p.Width.Kind != DimAuto || p.Display != DisplayBlock {
		t.Errorf("invalid values must not apply: %+v", p)
	}
	for _, want := range []string{
		"invalid-value: color: nonsense", "invalid-value: margin: 1 2 3 4 5", "invalid-value: padding: -1", "invalid-value: width: -5",
		"invalid-value: display: inline-flex", "invalid-value: font-weight: 1500", "invalid-value: box-shadow: 0",
		"unsupported-property: foo", "unsupported-property: float",
	} {
		if !x.has(want) {
			t.Errorf("missing warning %q in %v", want, x.warnings())
		}
	}
}

func TestPseudoElementRulesAreIgnored(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p::before { color: red } p:after { color: blue } p, p::before { margin-left: 9px }`, "")
	p := x.by(t, "p")
	if p.Color != (css.Color{A: 1}) {
		t.Errorf("p::before must not change p: %+v", p.Color)
	}
	if p.Margin[Left].Value != 9 {
		t.Errorf("the plain member of a list still applies: %v", p.Margin[Left])
	}
	if w := x.warnings(); len(w) != 0 {
		t.Errorf("warnings: %v", w)
	}
}

func TestInvalidSelectorDropsTheRule(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p, p:unknown { color: red } p { margin-left: 3px }`, "")
	p := x.by(t, "p")
	if p.Color != (css.Color{A: 1}) || p.Margin[Left].Value != 3 {
		t.Errorf("p: %+v", p)
	}
	found := false
	for k := range x.warnings() {
		if strings.HasPrefix(k, "unsupported-selector") {
			found = true
		}
	}
	if !found {
		t.Errorf("no unsupported-selector warning: %v", x.warnings())
	}
}

func TestMedia(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `
		p { margin-left: 1px }
		@media print { p { margin-right: 2px } }
		@media screen { p { margin-top: 3px } }
		@media (max-width: 600px) { p { margin-bottom: 4px } }
		@media (min-width: 600px) and (max-width: 900px) { p { padding-left: 5px } }
	`, "")
	p := x.by(t, "p")
	if p.Margin[Left].Value != 1 || p.Margin[Right].Value != 2 || p.Margin[Top].Value != 16 ||
		p.Margin[Bottom].Value != 16 || p.Padding[Left].Value != 5 {
		t.Errorf("p: margin %+v padding %+v", p.Margin, p.Padding)
	}
}

func TestStyleSharing(t *testing.T) {
	x := resolve(t, `<ul><li>a<li>b<li class=k>c</ul><ul><li>d</ul>`, `.k { color: red }`, "")
	var lis []*Computed
	for i := range x.d.Elements {
		if x.d.Elements[i].Tag.String() == "li" {
			lis = append(lis, x.styles[i])
		}
	}
	if len(lis) != 4 || lis[0] != lis[1] || lis[0] != lis[3] || lis[2] == lis[0] {
		t.Errorf("sharing: %p %p %p %p", lis[0], lis[1], lis[2], lis[3])
	}
}

func TestCustomPropertyDeclarationsApplyToTheirElement(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div>`, `div { --c: #0000ff } p { color: var(--c) }`, "")
	if got := x.by(t, "p").Color; got != rgb(0, 0, 255) {
		t.Errorf("color = %+v", got)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestAnonymousStyle(t *testing.T) {
	x := resolve(t, `<div>x</div>`, `div { color: #ff0000; margin-left: 9px; font-size: 20px; border: 1px solid; text-decoration: underline; --v: 1 }`, "")
	div := x.by(t, "div")
	a, err := x.r.Anonymous(div, DisplayBlock)
	if err != nil {
		t.Fatal(err)
	}
	if a.Display != DisplayBlock || a.Color != div.Color || a.FontSize != 20 {
		t.Errorf("inherited properties: %+v", a)
	}
	if a.Margin[Left].Value != 0 || a.Border[Top].Width != 0 || a.TextDecoration != 0 || a.Width.Kind != DimAuto {
		t.Errorf("non-inherited properties must be initial: %+v", a)
	}
	if b, _ := x.r.Anonymous(div, DisplayBlock); b != a {
		t.Error("same parent and display share the style")
	}
	if c, _ := x.r.Anonymous(div, DisplayInline); c == a || c.Display != DisplayInline {
		t.Error("another display is another style")
	}
	if root, err := x.r.Anonymous(nil, DisplayBlock); err != nil || root.FontSize != 16 {
		t.Errorf("anonymous root: %+v %v", root, err)
	}
}
