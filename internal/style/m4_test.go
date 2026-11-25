package style

import (
	"fmt"
	"math"
	"testing"
)

func TestM4Properties(t *testing.T) {
	px := func(v float64) Dim { return Dim{Kind: DimPx, Value: v} }
	tests := []struct {
		decl  string
		check func(*Computed) bool
	}{
		{"position: fixed", func(c *Computed) bool { return c.Position == PositionFixed }},
		{"position: FIXED; display: inline", func(c *Computed) bool { return c.Position == PositionFixed && c.Display == DisplayBlock }},
		{"position: fixed; display: inline-block", func(c *Computed) bool { return c.Display == DisplayBlock }},
		{"position: fixed; top: 0; z-index: -1", func(c *Computed) bool { return c.Inset[Top] == px(0) && c.ZIndex == -1 && !c.ZAuto }},
		{"columns: 2", func(c *Computed) bool { return c.ColumnCount == 2 }},
		{"columns: auto", func(c *Computed) bool { return c.ColumnCount == 0 }},
		{"columns: 3 auto", func(c *Computed) bool { return c.ColumnCount == 3 }},
		{"columns: auto 4", func(c *Computed) bool { return c.ColumnCount == 4 }},
		{"columns: auto auto", func(c *Computed) bool { return c.ColumnCount == 0 }},
		{"column-count: 5", func(c *Computed) bool { return c.ColumnCount == 5 }},
		{"column-count: auto", func(c *Computed) bool { return c.ColumnCount == 0 }},
		{"column-count: 1000", func(c *Computed) bool { return c.ColumnCount == 1000 }},
		{"column-width: auto", func(c *Computed) bool { return c.ColumnCount == 0 }},
		{"column-gap: normal", func(c *Computed) bool { return c.ColumnGapNormal && c.ColumnGap == px(0) }},
		{"column-gap: 12px", func(c *Computed) bool { return !c.ColumnGapNormal && c.ColumnGap == px(12) }},
		{"column-gap: 0", func(c *Computed) bool { return !c.ColumnGapNormal && c.ColumnGap == px(0) }},
		{"gap: 5px normal", func(c *Computed) bool { return c.ColumnGapNormal && c.RowGap == px(5) }},
		{"gap: normal 5px", func(c *Computed) bool { return !c.ColumnGapNormal && c.ColumnGap == px(5) }},
		{"gap: normal", func(c *Computed) bool { return c.ColumnGapNormal }},
		{"gap: 4px", func(c *Computed) bool { return !c.ColumnGapNormal && c.ColumnGap == px(4) }},
		{"column-gap: normal; column-gap: 3px", func(c *Computed) bool { return !c.ColumnGapNormal }},
		{"column-rule: 2px dashed red", func(c *Computed) bool {
			return c.ColumnRule.Width == 2 && c.ColumnRule.Style == BorderDashed && c.ColumnRule.Color.R == 1
		}},
		{"column-rule: dotted", func(c *Computed) bool { return c.ColumnRule.Style == BorderDotted && c.ColumnRule.Width == 3 }},
		{"column-rule: solid 0.5pt", func(c *Computed) bool { return c.ColumnRule.Style == BorderSolid && c.ColumnRule.Width == 1 }},
		{"column-rule: blue thick solid", func(c *Computed) bool { return c.ColumnRule.Width == 5 && c.ColumnRule.Color.B == 1 }},
		{"column-rule: 4px", func(c *Computed) bool { return c.ColumnRule.Width == 0 && c.ColumnRule.Style == BorderNone }},
		{"column-rule: 2.5pt solid", func(c *Computed) bool { return c.ColumnRule.Width == 3 }},
		{"column-rule-width: 7px; column-rule-style: solid", func(c *Computed) bool { return c.ColumnRule.Width == 7 }},
		{"column-rule-style: hidden", func(c *Computed) bool { return c.ColumnRule.Style == BorderHidden && c.ColumnRule.Width == 0 }},
		{"column-rule-style: solid; color: #00f", func(c *Computed) bool { return c.ColumnRule.Color.B == 1 }},
		{"column-rule-style: solid; column-rule-color: red; color: #00f", func(c *Computed) bool { return c.ColumnRule.Color.R == 1 && c.ColumnRule.Color.B == 0 }},
		{"column-rule: solid red; column-rule-color: currentcolor; color: #00f", func(c *Computed) bool { return c.ColumnRule.Color.B == 1 }},
		{"text-decoration-thickness: auto", func(c *Computed) bool { return c.TextDecorationThickness.Kind == DimAuto }},
		{"text-decoration-thickness: from-font", func(c *Computed) bool { return c.TextDecorationThickness.Kind == DimAuto }},
		{"text-decoration-thickness: 3px", func(c *Computed) bool { return c.TextDecorationThickness == px(3) }},
		{"text-decoration-thickness: 0.1em", func(c *Computed) bool { return nearPx(c.TextDecorationThickness.Value, 1.6) }},
		{"text-decoration-thickness: 0", func(c *Computed) bool { return c.TextDecorationThickness == px(0) }},
		{"text-decoration-thickness: 10%", func(c *Computed) bool { return c.TextDecorationThickness == Dim{Kind: DimPercent, Value: 10} }},
		{"border-style: dashed", func(c *Computed) bool {
			return c.Border[Top].Style == BorderDashed && c.Border[Left].Style == BorderDashed
		}},
		{"border-style: dotted solid", func(c *Computed) bool {
			return c.Border[Top].Style == BorderDotted && c.Border[Right].Style == BorderSolid
		}},
		{"border: 2px dotted red", func(c *Computed) bool { return c.Border[Bottom].Style == BorderDotted && c.Border[Bottom].Width == 2 }},
		{"border-left: 1px dashed", func(c *Computed) bool {
			return c.Border[Left].Style == BorderDashed && c.Border[Right].Style == BorderNone
		}},
		{"border-top-style: dashed", func(c *Computed) bool { return c.Border[Top].Style == BorderDashed }},
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

func TestM4InvalidValues(t *testing.T) {
	for _, tt := range []struct{ decl, prop string }{
		{"columns: 200px", "columns"},
		{"columns: 2 200px", "columns"},
		{"columns: 200px 2", "columns"},
		{"columns: 2 3", "columns"},
		{"columns: 0", "columns"},
		{"columns: -1", "columns"},
		{"columns: 1.5", "columns"},
		{"columns: foo", "columns"},
		{"columns: 1 2 3", "columns"},
		{"column-count: 0", "column-count"},
		{"column-count: -2", "column-count"},
		{"column-count: 2.5", "column-count"},
		{"column-count: 2px", "column-count"},
		{"column-count: none", "column-count"},
		{"column-width: 200px", "column-width"},
		{"column-width: 10em", "column-width"},
		{"column-width: 0", "column-width"},
		{"column-gap: -1px", "column-gap"},
		{"column-gap: 10%x", "column-gap"},
		{"column-gap: auto", "column-gap"},
		{"column-rule: 2px 3px", "column-rule"},
		{"column-rule: solid solid", "column-rule"},
		{"column-rule: -1px solid", "column-rule"},
		{"column-rule: double", "column-rule"},
		{"column-rule: red blue", "column-rule"},
		{"column-rule-style: groove", "column-rule-style"},
		{"column-rule-width: -2px", "column-rule-width"},
		{"column-rule-width: 50%", "column-rule-width"},
		{"column-rule-color: 5", "column-rule-color"},
		{"text-decoration-thickness: -1px", "text-decoration-thickness"},
		{"text-decoration-thickness: thin", "text-decoration-thickness"},
		{"text-decoration-thickness: 1", "text-decoration-thickness"},
		{"text-decoration-thickness: auto 2px", "text-decoration-thickness"},
		{"border-style: double", "border-style"},
		{"border-style: groove", "border-style"},
		{"border-style: ridge", "border-style"},
		{"border-style: inset", "border-style"},
		{"border-style: outset", "border-style"},
		{"border: 1px double red", "border"},
		{"position: sticky", "position"},
		{"break-before: column", "break-before"},
		{"break-after: column", "break-after"},
		{"break-inside: column", "break-inside"},
	} {
		t.Run(tt.decl, func(t *testing.T) {
			c, x := computed(t, tt.decl+"; font-size: 14px")
			if !x.has("invalid-value: " + tt.prop) {
				t.Errorf("no invalid-value warning: %v", x.warnings())
			}
			if c.FontSize != 14 || c.ColumnCount != 0 || c.Position == PositionFixed || c.ColumnRule.Style != BorderNone ||
				c.TextDecorationThickness.Kind != DimAuto || c.Border[Top].Style == BorderDashed || c.BreakBefore != BreakAuto {
				t.Errorf("an invalid declaration applied: %+v", c)
			}
		})
	}
	// column-span stays an unsupported property.
	if _, x := computed(t, "column-span: all"); !x.has("unsupported-property: column-span") {
		t.Errorf("%v", x.warnings())
	}
}

// column-count above MaxColumns is clamped with a warning; the bound itself is not.
func TestM4ColumnCountBound(t *testing.T) {
	for _, n := range []int{MaxColumns, MaxColumns + 1, 1 << 30, 99999999999} {
		for _, prop := range []string{"column-count", "columns"} {
			c, x := computed(t, fmt.Sprintf("%s: %d", prop, n))
			want := min(n, MaxColumns)
			clamped := x.has("clamped: columns")
			if c.ColumnCount != want || clamped != (n > MaxColumns) {
				t.Errorf("%s: %d: count %d, warned %v", prop, n, c.ColumnCount, clamped)
			}
		}
	}
}

// None of the properties is inherited: unset and initial give the initial value,
// inherit copies the parent's.
func TestM4CSSWideKeywordsAndInheritance(t *testing.T) {
	decls := "position: fixed; columns: 3; column-gap: 9px; column-rule: 2px dashed red; text-decoration-thickness: 4px"
	props := []string{"position", "column-count", "column-gap", "column-rule-width", "column-rule-style", "column-rule-color", "text-decoration-thickness"}
	check := func(c *Computed, set bool) error {
		var bad []string
		if set != (c.Position == PositionFixed) {
			bad = append(bad, "position")
		}
		if set != (c.ColumnCount == 3) {
			bad = append(bad, "column-count")
		}
		if set != (!c.ColumnGapNormal && c.ColumnGap == Dim{Value: 9}) {
			bad = append(bad, "column-gap")
		}
		if set != (c.ColumnRule.Width == 2 && c.ColumnRule.Style == BorderDashed && c.ColumnRule.Color.R == 1) {
			bad = append(bad, "column-rule")
		}
		if set != (c.TextDecorationThickness == Dim{Value: 4}) {
			bad = append(bad, "text-decoration-thickness")
		}
		if len(bad) != 0 {
			return fmt.Errorf("%v", bad)
		}
		return nil
	}
	for _, kw := range []string{"initial", "unset", "inherit"} {
		t.Run(kw, func(t *testing.T) {
			rules := ""
			for _, p := range props {
				rules += p + ": " + kw + ";"
			}
			x := resolve(t, `<div id=a><p id=b>x</p></div>`, `#a { `+decls+` } #b { `+rules+` }`, "")
			if err := check(x.by(t, "#b"), kw == "inherit"); err != nil {
				t.Errorf("%s: %v", kw, err)
			}
		})
	}
	t.Run("not inherited", func(t *testing.T) {
		x := resolve(t, `<div id=a><p id=b>x</p></div>`, `#a { `+decls+` }`, "")
		if err := check(x.by(t, "#b"), false); err != nil {
			t.Errorf("inherited: %v", err)
		}
	})
}

// The initial values are the ones of the design.
func TestM4InitialValues(t *testing.T) {
	c, _ := computed(t, "color: red")
	if c.ColumnCount != 0 || !c.ColumnGapNormal || c.ColumnRule.Style != BorderNone || c.ColumnRule.Width != 0 ||
		c.TextDecorationThickness.Kind != DimAuto || c.Position != PositionStatic {
		t.Errorf("%+v", c)
	}
	if PositionFixed != PositionAbsolute+1 || BorderDashed != BorderSolid+1 || BorderDotted != BorderSolid+2 {
		t.Error("the constants are not appended after the M3 ones")
	}
}

// The rule colour follows currentcolor and the opacity, like the border colours;
// an inherited colour is faded once.
func TestM4ColumnRuleColorAndOpacity(t *testing.T) {
	x := resolve(t, `<div id=a><div id=b>x</div></div>`, `#a { opacity: .5; column-rule: 1px solid; color: rgb(0 0 255) } #b { opacity: .5; column-rule-style: solid; column-rule-color: inherit }`, "")
	a, b := x.by(t, "#a"), x.by(t, "#b")
	if a.ColumnRule.Color.B != 1 || !nearPx(a.ColumnRule.Color.A, .5) {
		t.Errorf("a %+v", a.ColumnRule.Color)
	}
	if !nearPx(b.ColumnRule.Color.A, .25) || b.ColumnRule.Color.B != 1 {
		t.Errorf("an inherited colour is faded once by the product: %+v", b.ColumnRule.Color)
	}
}

// A hostile value never leaves the bounds.
func TestM4HostileValues(t *testing.T) {
	for _, d := range []string{
		"column-rule: 1e30px solid", "column-rule-width: 1e999px", "text-decoration-thickness: 1e30px", "text-decoration-thickness: 1e999em",
		"column-gap: 1e30px", "columns: 1e30", "column-count: -1e30",
	} {
		c, _ := computed(t, d+"; column-rule-style: solid")
		for _, v := range []float64{c.ColumnRule.Width, c.TextDecorationThickness.Value, c.ColumnGap.Value} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e7 {
				t.Errorf("%s: %v", d, v)
			}
		}
		if c.ColumnCount < 0 || c.ColumnCount > MaxColumns {
			t.Errorf("%s: %d columns", d, c.ColumnCount)
		}
	}
}
