package style

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func TestCyclesAreInvalidWhateverTheFallback(t *testing.T) {
	tests := []struct {
		name, css string
		want      float64
	}{
		{"self cycle with fallback", `p { --a: var(--a, 5px); margin-left: var(--a) }`, 0},
		{"use fallback after a cycle", `p { --a: var(--a, 5px); margin-left: var(--a, 9px) }`, 9},
		{"two members", `p { --a: var(--b, 1px); --b: var(--a, 2px); margin-left: var(--a, 7px) }`, 7},
		{"outsider falls back", `p { --a: var(--a, 5px); --c: var(--a, 4px); margin-left: var(--c) }`, 4},
		{"outsider of a two cycle", `p { --a: var(--b); --b: var(--a); --c: var(--b, 6px); margin-left: var(--c) }`, 6},
		{"no cycle", `p { --a: 3px; --b: var(--a, 1px); margin-left: var(--b) }`, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := resolve(t, `<p>x</p>`, tt.css, "")
			if got := x.by(t, "p").Margin[Left].Value; got != tt.want {
				t.Errorf("margin-left = %v, want %v", got, tt.want)
			}
		})
	}
	// A long cycle, with fallbacks everywhere.
	var sb strings.Builder
	const n = 2000
	sb.WriteString("p {")
	for i := range n {
		fmt.Fprintf(&sb, "--v%d: var(--v%d, 1px);", i, (i+1)%n)
	}
	sb.WriteString("margin-left: var(--v0, 8px) }")
	x := resolve(t, `<p>x</p>`, sb.String(), "")
	if got := x.by(t, "p").Margin[Left].Value; got != 8 {
		t.Errorf("long cycle: margin-left = %v, want 8", got)
	}
}

// Many custom properties on :root and a distinct style attribute per element:
// each style stores only its own declarations.

func TestLongChainValuesResolve(t *testing.T) {
	var html strings.Builder
	const depth = 200
	for i := range depth {
		fmt.Fprintf(&html, `<div style="--d%d:%dpx">`, i, i)
	}
	html.WriteString(`<p style="margin-left: var(--d0); padding-left: var(--d123); margin-right: var(--d199)">x</p>`)
	x := resolve(t, html.String(), "", "")
	p := x.by(t, "p")
	if p.Margin[Left].Value != 0 || p.Padding[Left].Value != 123 || p.Margin[Right].Value != 199 {
		t.Errorf("margin-left %v padding-left %v margin-right %v", p.Margin[Left], p.Padding[Left], p.Margin[Right])
	}
}

func TestDeclarationLoopsAreCharged(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("* {")
	for i := range 5000 {
		fmt.Fprintf(&sb, "--x%d: 1;", i)
	}
	sb.WriteString("}")
	b := budget.New(context.Background(), func() budget.Limits {
		l := budget.Defaults()
		l[budget.Work] = 3000
		return l
	}(), nil)
	d, err := dom.Parse(strings.NewReader(`<p>x</p>`), b)
	if err != nil {
		t.Fatal(err)
	}
	b2 := newBudget()
	ss, err := css.ParseStylesheet([]byte(sb.String()), css.OriginDocument, b2)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := Compile(ss, b2)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(ua(), nil, []*Sheet{sh}, printEnv, b)
	var le *budget.LimitError
	var parent *Computed
	for i := range d.Elements {
		c, err := r.Element(d, int32(i), parent)
		if err != nil {
			if !errors.As(err, &le) || le.Kind != budget.Work {
				t.Fatalf("got %v", err)
			}
			return
		}
		parent = c
	}
	t.Error("5000 declarations per element did not reach the Work limit")
}

func TestKeywordsBeyondM1AreDroppedAndWarned(t *testing.T) {
	tests := []struct {
		decl, prop string
		check      func(*Computed) bool
	}{
		{"border-style: dashed", "border-style", func(c *Computed) bool { return c.Border[Top].Style == BorderNone }},
		{"border-style: dotted", "border-style", func(c *Computed) bool { return c.Border[Top].Style == BorderNone }},
		{"border: 1px dashed red", "border", func(c *Computed) bool { return c.Border[Top].Width == 0 }},
		{"border-top: 2px dotted", "border-top", func(c *Computed) bool { return c.Border[Top].Width == 0 }},
		{"overflow-wrap: anywhere", "overflow-wrap", func(c *Computed) bool { return c.OverflowWrap == WrapNormal }},
		{"word-wrap: anywhere", "word-wrap", func(c *Computed) bool { return c.OverflowWrap == WrapNormal }},
		{"white-space: nowrap", "white-space", func(c *Computed) bool { return c.WhiteSpace == WhiteSpaceNormal }},
		{"white-space: pre-wrap", "white-space", func(c *Computed) bool { return c.WhiteSpace == WhiteSpaceNormal }},
		{"white-space: pre-line", "white-space", func(c *Computed) bool { return c.WhiteSpace == WhiteSpaceNormal }},
		{"text-decoration: overline", "text-decoration", func(c *Computed) bool { return c.TextDecoration == 0 }},
		{"text-decoration: underline overline", "text-decoration", func(c *Computed) bool { return c.TextDecoration == 0 }},
		{"text-align: justify", "text-align", func(c *Computed) bool { return c.TextAlign == AlignLeft }},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			x := resolve(t, `<p>x</p>`, `p { `+tt.decl+` }`, "")
			if !tt.check(x.by(t, "p")) {
				t.Errorf("the declaration applied: %+v", x.by(t, "p"))
			}
			if !x.has("invalid-value: " + tt.prop + ": ") {
				t.Errorf("no invalid-value warning for %s: %v", tt.prop, x.warnings())
			}
		})
	}
	// The cascade falls back to the previous declaration.
	x := resolve(t, `<p>x</p>`, `p { white-space: pre; white-space: nowrap; text-align: center; text-align: justify }`, "")
	if p := x.by(t, "p"); p.WhiteSpace != WhiteSpacePre || p.TextAlign != AlignCenter {
		t.Errorf("fallback: %+v", p)
	}
	// font-style: oblique stays.
	x = resolve(t, `<p>x</p>`, `p { font-style: oblique }`, "")
	if x.by(t, "p").FontStyle != FontOblique || len(x.warnings()) != 0 {
		t.Errorf("oblique: %v", x.warnings())
	}
}

func TestInheritedPropertiesAreConsistent(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div>`, `div {
		border-collapse: collapse; border-spacing: 3px; font-family: Foo; font-size: 22px; font-weight: 700; font-style: italic;
		line-height: 2; color: #123456; text-align: right; letter-spacing: 2px; overflow-wrap: break-word; white-space: pre;
		list-style-type: none; display: block; margin: 5px; padding: 6px; width: 50px; max-width: 70px; box-sizing: border-box;
		border: 1px solid #fedcba; border-radius: 4px; background: #010203; overflow: hidden; table-layout: fixed;
		text-decoration: underline; vertical-align: top
	}`, "")
	parent := x.by(t, "div")
	a := initialStyle
	a.inheritFrom(parent)
	b := initialStyle
	xc := &computeCtx{c: &b}
	for p := prop(0); p < numProps; p++ {
		if inherited[p] {
			xc.copyProp(p, parent)
		}
	}
	// Compare through the exported state; the private fields travel with
	// inheritFrom only.
	a.rootFont, a.vars, b.rootFont, b.vars = 0, nil, 0, nil
	if fmt.Sprintf("%+v", a) != fmt.Sprintf("%+v", b) {
		t.Errorf("inheritFrom and copyProp disagree:\n%+v\n%+v", a, b)
	}
}

func TestUnsupportedElementsAreDisplayNone(t *testing.T) {
	for tag := range unsupportedElements {
		x := resolve(t, `<`+tag+`>x</`+tag+`>`, "", "")
		found := false
		for i := range x.d.Elements {
			if x.d.Elements[i].Tag.String() == tag {
				found = true
				if x.styles[i].Display != DisplayNone {
					t.Errorf("%s is %v, want display none", tag, x.styles[i].Display)
				}
			}
		}
		if !found {
			t.Errorf("%s was not parsed as an element", tag)
		}
	}
}

func TestMarginBoxContentIsCapped(t *testing.T) {
	long := strings.Repeat("é", MaxContentBytes) // 2 KiB, multi-byte
	ps, b := pageOf(t, "", `@page { @bottom-left { content: "`+long+`" "tail" } @bottom-right { content: "`+strings.Repeat("x", MaxContentBytes)+`" } }`)
	got := ps.Boxes[MarginBottomLeft].Content
	total := 0
	for _, it := range got {
		total += len(it.Text)
		if strings.ContainsRune(it.Text, '�') {
			t.Errorf("cut inside a rune: %q", it.Text[len(it.Text)-4:])
		}
	}
	if total > MaxContentBytes {
		t.Errorf("%d bytes kept", total)
	}
	if w, _ := b.Warnings(); len(w) != 1 || w[0].Code != WarnClamped || w[0].Count != 1 {
		t.Errorf("warnings %+v", w)
	}
	// At the limit: kept whole, no warning.
	ps, b = pageOf(t, "", `@page { @bottom-right { content: "`+strings.Repeat("x", MaxContentBytes)+`" } }`)
	if len(ps.Boxes[MarginBottomRight].Content[0].Text) != MaxContentBytes {
		t.Error("content at the limit was cut")
	}
	if w, _ := b.Warnings(); len(w) != 0 {
		t.Errorf("warnings %+v", w)
	}
}

func TestLineHeightProductIsBounded(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { font-size: 1e4px; line-height: 1e30 }`, "")
	p := x.by(t, "p")
	if p.LineHeight.Kind != LineHeightNumber || p.LineHeight.Value*p.FontSize > css.MaxLengthPx*1.0001 {
		t.Errorf("line-height %+v at %v px", p.LineHeight, p.FontSize)
	}
	x = resolve(t, `<p>x</p>`, `p { line-height: -2 }`, "")
	if !x.has("invalid-value: line-height: -2") {
		t.Errorf("warnings: %v", x.warnings())
	}
}

func TestAnonymousStylesLimit(t *testing.T) {
	l := budget.Defaults()
	l[budget.Styles] = 3
	b := budget.New(context.Background(), l, nil)
	r := NewResolver(ua(), nil, nil, printEnv, b)
	root, err := r.Anonymous(nil, DisplayBlock)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range []Display{DisplayInline, DisplayTable} {
		if _, err := r.Anonymous(root, d); err != nil {
			t.Fatalf("style %d: %v", i+2, err)
		}
	}
	_, err = r.Anonymous(root, DisplayListItem)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Styles || le.Max != 3 {
		t.Errorf("over the limit: %v", err)
	}
	if _, err := r.Anonymous(root, DisplayInline); err != nil {
		t.Errorf("a shared style costs nothing: %v", err)
	}
}

func TestAnonymousDropsBorderAndMargin(t *testing.T) {
	x := resolve(t, `<div>x</div>`, `div { border: 2px solid red; margin: 7px; padding: 3px; background: #123; color: #0f0; font-size: 21px }`, "")
	div := x.by(t, "div")
	a, err := x.r.Anonymous(div, DisplayBlock)
	if err != nil {
		t.Fatal(err)
	}
	if a.Border[Top].Width != 0 || a.Margin[Top].Value != 0 || a.Padding[Top].Value != 0 || a.BackgroundColor.A != 0 {
		t.Errorf("anonymous box keeps box decorations: %+v", a)
	}
	if a.Color != rgb(0, 255, 0) || a.FontSize != 21 {
		t.Errorf("inherited: %+v", a)
	}
}
