package style

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"

	"github.com/glimps-re/htmltopdf/internal/css"
)

func warningCount(t *testing.T, x *doc, code budget.Code, detail string) int {
	t.Helper()
	w, _ := x.b.Warnings()
	for _, v := range w {
		if v.Code == code && v.Detail == detail {
			return v.Count
		}
	}
	return 0
}

func TestUnsupportedElementWarningCountsOccurrences(t *testing.T) {
	x := resolve(t, `<iframe src=a></iframe><p>x</p><iframe src=b></iframe><div><iframe src=c></iframe><img src=d></div><input>`, "", "")
	if got := warningCount(t, x, budget.WarnUnsupportedElement, "iframe"); got != 3 {
		t.Errorf("iframe count = %d, want 3", got)
	}
	if got := warningCount(t, x, budget.WarnUnsupportedElement, "img"); got != 0 {
		t.Errorf("img count = %d, want 0", got)
	}
	if got := warningCount(t, x, budget.WarnUnsupportedElement, "input"); got != 1 {
		t.Errorf("input count = %d, want 1", got)
	}
}

func TestVarInPageRulesIsInvalidNeverSilent(t *testing.T) {
	for _, src := range []string{
		"@page { margin: var(--m) }",
		"@page { size: var(--s) }",
		"@page { @bottom-left { content: var(--c) } }",
		"@page { --m: 1in }",
	} {
		b := newBudget()
		ss, err := css.ParseStylesheet([]byte(src), css.OriginExtra, b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(ss, b); err != nil {
			t.Fatal(err)
		}
		if w, _ := b.Warnings(); len(w) != 1 || w[0].Count != 1 {
			t.Errorf("%s: warnings %+v", src, w)
		}
	}
}

func TestFontSizeAndFamilyClampsAreWarned(t *testing.T) {
	x := resolve(t, `<p>x</p>`, `p { font-size: 1e30px }`, "")
	if got := x.by(t, "p").FontSize; got != MaxFontSizePx {
		t.Errorf("font-size = %v", got)
	}
	if warningCount(t, x, budget.WarnClamped, "font-size") != 1 {
		t.Errorf("warnings: %v", x.warnings())
	}
	var fams []string
	for i := range MaxFontFamilies + 5 {
		fams = append(fams, fmt.Sprintf("f%d", i))
	}
	x = resolve(t, `<p>x</p>`, `p { font-family: `+strings.Join(fams, ",")+` }`, "")
	if got := len(x.by(t, "p").FontFamily); got != MaxFontFamilies {
		t.Errorf("families = %d", got)
	}
	if warningCount(t, x, budget.WarnClamped, "font-family") != 1 {
		t.Errorf("warnings: %v", x.warnings())
	}
	// At the limit nothing is cut or warned.
	x = resolve(t, `<p>x</p>`, `p { font-family: `+strings.Join(fams[:MaxFontFamilies], ",")+`; font-size: 10000px }`, "")
	if len(x.by(t, "p").FontFamily) != MaxFontFamilies || len(x.warnings()) != 0 {
		t.Errorf("at the limit: %v", x.warnings())
	}
}

func TestLengthClampIsWarned(t *testing.T) {
	tests := []struct {
		css  string
		want int
	}{
		{"p { margin-left: 10000000px }", 0},
		{"p { margin-left: 10000001px }", 1},
		{"p { margin-left: -10000001px }", 1},
		{"p { width: 200000in }", 1},
		{"p { padding: 10000001px 1px }", 1},
		{"p { margin-left: 9999999px }", 0},
	}
	for _, tt := range tests {
		x := resolve(t, `<p>x</p>`, tt.css, "")
		if got := warningCount(t, x, budget.WarnClamped, "length beyond 1e7 px"); got != tt.want {
			t.Errorf("%s: %d warnings, want %d", tt.css, got, tt.want)
		}
	}
}
