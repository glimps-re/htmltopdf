package style

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
)

func TestVarSubstitution(t *testing.T) {
	x := resolve(t, `<div id=a><p id=b>x</p></div><div id=c>y</div>`, `
		:root { --red: #ff0000; --m: 7px; --pair: 3px 4px; --grad: linear-gradient(90deg, var(--red), #00f) }
		#a { color: var(--red); margin-left: var(--m); padding: var(--pair); background: var(--grad) }
		#b { color: var(--missing, #00ff00); margin-top: var(--nope, var(--m)) }
		#c { color: var(--missing); font-size: 30px; margin-right: var(--m) var(--m) }
	`, "")
	a, b, c := x.by(t, "#a"), x.by(t, "#b"), x.by(t, "#c")
	if a.Color != rgb(255, 0, 0) || a.Margin[Left].Value != 7 || a.Padding != [4]Dim{{Value: 3}, {Value: 4}, {Value: 3}, {Value: 4}} {
		t.Errorf("a: color %+v margin %v padding %v", a.Color, a.Margin[Left], a.Padding)
	}
	if a.BackgroundImage == nil || a.BackgroundImage.Stops[0].Color != rgb(255, 0, 0) {
		t.Errorf("a background: %+v", a.BackgroundImage)
	}
	if b.Color != rgb(0, 255, 0) || b.Margin[Top].Value != 7 {
		t.Errorf("b (fallbacks): color %+v margin %v", b.Color, b.Margin[Top])
	}
	// An undefined variable without fallback makes the property unset: color
	// inherits (black here), the invalid margin is initial.
	if c.Color != (css.Color{A: 1}) || c.Margin[Right].Value != 0 {
		t.Errorf("c (invalid at computed-value time): color %+v margin %v", c.Color, c.Margin[Right])
	}
}

func TestCustomPropertiesInherit(t *testing.T) {
	x := resolve(t, `<div id=a><div id=b><p id=c>x</p></div></div>`, `
		#a { --c: #ff0000; --d: var(--c) }
		#b { --c: #00ff00 }
		#c { color: var(--d) }
	`, "")
	// --d was resolved on #a, with #a's --c, and is inherited as resolved.
	if got := x.by(t, "#c").Color; got != rgb(255, 0, 0) {
		t.Errorf("color = %+v", got)
	}
}

func TestCustomPropertyCycles(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { --a: var(--b); --b: var(--a); --self: var(--self); color: var(--a, #00ff00); margin-left: var(--self, 5px) }`, "")
	p := x.by(t, "p")
	if p.Color != rgb(0, 255, 0) || p.Margin[Left].Value != 5 {
		t.Errorf("cycles must use the fallback: color %+v margin %v", p.Color, p.Margin[Left])
	}
}

func TestCustomPropertyKeywords(t *testing.T) {
	x := resolve(t, `<div><p id=a>x</p><p id=b>y</p><p id=c>z</p></div>`, `
		div { --c: #ff0000 }
		#a { --c: initial; color: var(--c, #00ff00) }
		#b { --c: inherit; color: var(--c) }
		#c { --c: unset; color: var(--c) }
	`, "")
	if got := x.by(t, "#a").Color; got != rgb(0, 255, 0) {
		t.Errorf("--c: initial: %+v", got)
	}
	if got := x.by(t, "#b").Color; got != rgb(255, 0, 0) {
		t.Errorf("--c: inherit: %+v", got)
	}
	if got := x.by(t, "#c").Color; got != rgb(255, 0, 0) {
		t.Errorf("--c: unset inherits: %+v", got)
	}
}

func TestVarInsideFunctionsAndFallbackCommas(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { --r: 12; color: rgb(var(--r), var(--g, 34), 56); font-family: var(--f, "A, B", C) }`, "")
	p := x.by(t, "p")
	if p.Color != rgb(12, 34, 56) {
		t.Errorf("color = %+v", p.Color)
	}
	if got := strings.Join(p.FontFamily, "|"); got != "A, B|C" {
		t.Errorf("font-family = %q", got)
	}
}

// 2^40 expansion of a custom property: bounded by VarBytes, quick, warned.
func TestVarBomb(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(":root { --v0: x x }\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "--v%d: var(--v%d) var(--v%d);\n", i, i-1, i-1)
	}
	css := ":root {" + strings.Replace(sb.String(), ":root { --v0: x x }\n", "--v0: x x;\n", 1) + "} p { font-family: var(--v40); margin-left: 3px }"
	x := resolve(t, `<p>x</p>`, css, "")
	p := x.by(t, "p")
	if got := strings.Join(p.FontFamily, "|"); got == "" || len(p.FontFamily) > MaxFontFamilies {
		t.Errorf("font-family = %v", p.FontFamily)
	}
	if p.Margin[Left].Value != 3 {
		t.Errorf("the rest of the style must survive: %v", p.Margin[Left])
	}
	found := false
	for k := range x.warnings() {
		if strings.HasPrefix(k, WarnVarBudget) {
			found = true
		}
	}
	if !found {
		t.Errorf("no var-budget warning: %v", x.warnings())
	}
}

func BenchmarkVarBomb(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(":root { --v0: x x;\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "--v%d: var(--v%d) var(--v%d);\n", i, i-1, i-1)
	}
	sb.WriteString("} p { font-family: var(--v40) }")
	for b.Loop() {
		x := resolve(b, `<p>x</p>`, sb.String(), "")
		_ = x
	}
}
