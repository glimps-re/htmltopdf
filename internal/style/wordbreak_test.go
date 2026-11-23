package style

import "testing"

func TestOverflowWrapAndWordBreakValues(t *testing.T) {
	for _, tc := range []struct {
		decl string
		wrap OverflowWrap
		brk  WordBreak
	}{
		{"overflow-wrap: anywhere", WrapAnywhere, WordBreakNormal},
		{"word-wrap: anywhere", WrapAnywhere, WordBreakNormal},
		{"overflow-wrap: break-word", WrapBreakWord, WordBreakNormal},
		{"overflow-wrap: normal", WrapNormal, WordBreakNormal},
		{"word-break: break-all", WrapNormal, WordBreakAll},
		{"word-break: normal", WrapNormal, WordBreakNormal},
	} {
		x := resolve(t, `<div><p>x</p></div>`, `div { `+tc.decl+` }`, "")
		for _, tag := range []string{"div", "p"} {
			c := x.by(t, tag)
			if c.OverflowWrap != tc.wrap || c.WordBreak != tc.brk {
				t.Errorf("%s on %s: wrap %v, break %v", tc.decl, tag, c.OverflowWrap, c.WordBreak)
			}
		}
		if len(x.warnings()) != 0 {
			t.Errorf("%s: warnings %v", tc.decl, x.warnings())
		}
	}
}

func TestWordBreakExplicitOverrideAndInitial(t *testing.T) {
	x := resolve(t, `<div><p>x</p><i>y</i></div>`, `div { word-break: break-all; overflow-wrap: anywhere } p { word-break: normal; overflow-wrap: initial } i { word-break: inherit }`, "")
	if p := x.by(t, "p"); p.WordBreak != WordBreakNormal || p.OverflowWrap != WrapNormal {
		t.Errorf("p: %+v %+v", p.WordBreak, p.OverflowWrap)
	}
	if i := x.by(t, "i"); i.WordBreak != WordBreakAll || i.OverflowWrap != WrapAnywhere {
		t.Errorf("i: %+v %+v", i.WordBreak, i.OverflowWrap)
	}
}
