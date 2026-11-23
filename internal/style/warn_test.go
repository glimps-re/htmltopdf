package style

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/css"
)

func warningCount(t *testing.T, x *doc, code, detail string) int {
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
	x := resolve(t, `<img src=a><p>x</p><img src=b><div><img src=c></div><input>`, "", "")
	if got := warningCount(t, x, WarnUnsupportedElement, "img"); got != 3 {
		t.Errorf("img count = %d, want 3", got)
	}
	if got := warningCount(t, x, WarnUnsupportedElement, "input"); got != 1 {
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
	if warningCount(t, x, WarnClamped, "font-size") != 1 {
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
	if warningCount(t, x, WarnClamped, "font-family") != 1 {
		t.Errorf("warnings: %v", x.warnings())
	}
	// At the limit nothing is cut or warned.
	x = resolve(t, `<p>x</p>`, `p { font-family: `+strings.Join(fams[:MaxFontFamilies], ",")+`; font-size: 10000px }`, "")
	if len(x.by(t, "p").FontFamily) != MaxFontFamilies || len(x.warnings()) != 0 {
		t.Errorf("at the limit: %v", x.warnings())
	}
}
