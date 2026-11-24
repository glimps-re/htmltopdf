package style

import (
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
)

func shadowTokens(t testing.TB, v string) []css.Token {
	t.Helper()
	ds, err := css.ParseDeclarations([]byte("x:"+v), newBudget())
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %v", ds, err)
	}
	return ds[0].Value
}

func TestParseBoxShadow(t *testing.T) {
	tests := []struct {
		value   string
		n       int
		inset   bool
		clamped bool
		ok      bool
	}{
		{"none", 0, false, false, true},
		{"0 1px 2px red", 1, false, false, true},
		{"2px 2px", 1, false, false, true},
		{"2px 2px 4px 1px #00000040", 1, false, false, true},
		{"red 2px 2px", 1, false, false, true},
		{"2px 2px 4px 1px red", 1, false, false, true},
		{"0 1px 2px rgba(0,0,0,.2), 0 4px 8px rgba(0,0,0,.1)", 2, false, false, true},
		{"-2px -3px 0 -1px blue", 1, false, false, true},
		{"1em 1rem 1pt", 1, false, false, true},
		{"inset 0 1px 2px red", 0, true, false, true},
		{"0 1px 2px red inset", 0, true, false, true},
		{"0 1px red, inset 0 2px blue", 1, true, false, true},
		{"1px", 0, false, false, false},
		{"1px 2px 3px 4px 5px", 0, false, false, false},
		{"1px 2px -3px", 0, false, false, false},
		{"1px 2px 50%", 0, false, false, false},
		{"1px red 2px", 0, false, false, false},
		{"1px 2px red blue", 0, false, false, false},
		{"1px 2px red inset inset", 0, false, false, false},
		{"1 2px", 0, false, false, false},
		{"none, 1px 2px", 0, false, false, false},
		{"1px 2px red,", 0, false, false, false},
		{"1px 2px foo", 0, false, false, false},
		{"1px 2px calc(1px + 2px)", 0, false, false, false},
		{strings.Repeat("1px 1px red,", 8) + "1px 1px red", 8, false, true, true},
		{strings.Repeat("1px 1px red,", 20) + "1px 1px", 8, false, true, true},
		{strings.Repeat("1px 1px red,", 500) + "1px 1px", 0, false, false, false},
	}
	for i, tt := range tests {
		t.Run(strings.Fields(tt.value + " x")[0]+string(rune('a'+i)), func(t *testing.T) {
			l, ok := parseBoxShadow(shadowTokens(t, tt.value))
			if ok != tt.ok {
				t.Fatalf("ok = %v", ok)
			}
			if ok && (len(l.shadows) != tt.n || l.inset != tt.inset || l.clamped != tt.clamped) {
				t.Errorf("%d shadows, inset %v, clamped %v", len(l.shadows), l.inset, l.clamped)
			}
		})
	}
}

func TestParseBoxShadowValues(t *testing.T) {
	l, ok := parseBoxShadow(shadowTokens(t, "1px 2px 3px 4px red"))
	if !ok || len(l.shadows) != 1 {
		t.Fatal(ok)
	}
	s := l.shadows[0]
	if s.x.Value != 1 || s.y.Value != 2 || s.blur.Value != 3 || s.spread.Value != 4 || s.cur || s.color.R != 1 {
		t.Errorf("%+v", s)
	}
	l, _ = parseBoxShadow(shadowTokens(t, "1px 2px"))
	if !l.shadows[0].cur {
		t.Error("a missing colour is currentcolor")
	}
}

func TestResolveShadows(t *testing.T) {
	c := &Computed{FontSize: 10, rootFont: 20, Color: css.Color{R: 1, A: 1}}
	x := &computeCtx{c: c}
	l, _ := parseBoxShadow(shadowTokens(t, "1em 1rem 2px, 0 0 -0px green"))
	if l.shadows == nil {
		t.Fatal("not parsed")
	}
	got := x.resolveShadows(l.shadows)
	if len(got) != 2 || got[0].X != 10 || got[0].Y != 20 || got[0].Blur != 2 || got[0].Color != c.Color {
		t.Errorf("%+v", got)
	}
	c.fade = 0.5
	c.raw = &unfaded{color: css.Color{R: 1, A: 1}}
	c.Color.A = 0.5
	got = x.resolveShadows(l.shadows)
	if got[0].Color.A != 0.5 || got[1].Color.A != 0.5 {
		t.Errorf("faded alphas %v %v", got[0].Color.A, got[1].Color.A)
	}
	if x.resolveShadows(nil) != nil {
		t.Error("none must resolve to nil")
	}
}

func FuzzParseBoxShadow(f *testing.F) {
	for _, s := range []string{"none", "0 1px 2px red", "inset 1px 2px 3px 4px #fff, 1px 1px", "1px 2px calc(1px+2px)", ","} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if len(v) > 4096 {
			return
		}
		ds, err := css.ParseDeclarations([]byte("x:"+v), newBudget())
		if err != nil || len(ds) != 1 {
			return
		}
		if l, ok := parseBoxShadow(ds[0].Value); ok && len(l.shadows) > MaxShadows {
			t.Fatalf("%d shadows", len(l.shadows))
		}
	})
}

func TestBoxShadowComputed(t *testing.T) {
	c, x := computed(t, "color: #102030; box-shadow: 1px 2px 3px 4px red, 0 0 1px")
	if len(c.BoxShadow) != 2 {
		t.Fatalf("%+v %v", c.BoxShadow, x.warnings())
	}
	if s := c.BoxShadow[0]; s.X != 1 || s.Y != 2 || s.Blur != 3 || s.Spread != 4 || s.Color.R != 1 {
		t.Errorf("%+v", s)
	}
	if got, want := c.BoxShadow[1].Color, c.Color; got != want {
		t.Errorf("default colour %+v, want currentcolor %+v", got, want)
	}
	if len(x.warnings()) != 0 {
		t.Errorf("warnings %v", x.warnings())
	}
	if c, _ := computed(t, "box-shadow: none"); c.BoxShadow != nil {
		t.Error("none")
	}
}

func TestBoxShadowIsNotInheritedButInheritKeywordWorks(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div><div class=i><p>y</p></div>`, `div { box-shadow: 1px 1px red } .i { box-shadow: none } .i p { box-shadow: inherit } p { box-shadow: 2px 2px blue }`, "")
	if c := x.by(t, "div"); len(c.BoxShadow) != 1 {
		t.Errorf("div: %+v", c.BoxShadow)
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { box-shadow: 1px 1px red }`, "")
	if c := x.by(t, "p"); c.BoxShadow != nil {
		t.Errorf("inherited: %+v", c.BoxShadow)
	}
	x = resolve(t, `<div><p>x</p></div>`, `div { box-shadow: 1px 1px red } p { box-shadow: inherit }`, "")
	if c := x.by(t, "p"); len(c.BoxShadow) != 1 || c.BoxShadow[0].Color.R != 1 {
		t.Errorf("inherit: %+v", c.BoxShadow)
	}
}

func TestBoxShadowOpacityAndWarnings(t *testing.T) {
	c, _ := computed(t, "opacity: .5; box-shadow: 1px 1px rgba(0,0,0,.4)")
	if got := c.BoxShadow[0].Color.A; got < 0.199 || got > 0.201 {
		t.Errorf("alpha %v, want 0.2", got)
	}
	c, _ = computed(t, "opacity: .5; color: #f00; box-shadow: 1px 1px")
	if got := c.BoxShadow[0].Color; got.R != 1 || got.A < 0.499 || got.A > 0.501 {
		t.Errorf("currentcolor shadow %+v", got)
	}
	_, x := computed(t, "box-shadow: inset 0 1px 2px red, 1px 1px blue")
	if !x.has("unsupported-property: box-shadow inset") {
		t.Errorf("%v", x.warnings())
	}
	_, x = computed(t, "box-shadow: "+strings.Repeat("1px 1px red,", 20)+"1px 1px")
	if !x.has("clamped: box-shadow") {
		t.Errorf("%v", x.warnings())
	}
	c, x = computed(t, "--s: 1px 2px 3px green; box-shadow: var(--s)")
	if len(c.BoxShadow) != 1 || c.BoxShadow[0].Blur != 3 {
		t.Errorf("var(): %+v %v", c.BoxShadow, x.warnings())
	}
	c, _ = computed(t, "font-size: 10px; box-shadow: 1em 2em 1em")
	if c.BoxShadow[0].X != 10 || c.BoxShadow[0].Y != 20 || c.BoxShadow[0].Blur != 10 {
		t.Errorf("em: %+v", c.BoxShadow[0])
	}
}

func TestManyShadowsStayBounded(t *testing.T) {
	html := strings.Repeat(`<section>x</section>`, 1000)
	x := resolve(t, html, `section { box-shadow: `+strings.Repeat("1px 1px 1px red,", 7)+`1px 1px red }`, "")
	if c := x.by(t, "section"); len(c.BoxShadow) != MaxShadows {
		t.Errorf("%d shadows", len(c.BoxShadow))
	}
}

func TestInheritedShadowIsResolvedAgainstTheInheritingElement(t *testing.T) {
	x := resolve(t, `<div><p>x</p></div>`, `div { box-shadow: 0 0 2px; color: red } p { box-shadow: inherit; color: blue; opacity: .5 }`, "")
	c := x.by(t, "p")
	if len(c.BoxShadow) != 1 {
		t.Fatalf("%+v", c.BoxShadow)
	}
	if got := c.BoxShadow[0].Color; got.B != 1 || got.R != 0 || got.A < 0.499 || got.A > 0.501 {
		t.Errorf("inherited shadow colour %+v, want the element's blue at half opacity", got)
	}
	if d := x.by(t, "div"); len(d.BoxShadow) != 1 || d.BoxShadow[0].Color.R != 1 || d.BoxShadow[0].Color.A != 1 {
		t.Errorf("source shadow %+v", d.BoxShadow)
	}
}
