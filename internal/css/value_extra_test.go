package css

import (
	"context"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// Angle is documented to lie in [0, 360): a tiny negative angle must not round
// up to 360.
func TestGradientAngleRange(t *testing.T) {
	for _, a := range []string{
		"-1e-20deg", "-1e-30turn", "-1e-40rad", "-1e-10grad", "-0deg", "-360deg", "-1e38turn", "1e38turn",
		"-1e38grad", "360deg", "720deg", "359.9999999deg", "-0.0000001deg",
	} {
		t.Run(a, func(t *testing.T) {
			g, ok := ParseGradient(valueTokens(t, "linear-gradient("+a+", red, blue)"))
			if !ok {
				t.Fatal("rejected")
			}
			if !(g.Angle >= 0 && g.Angle < 360) {
				t.Fatalf("angle %v outside [0, 360)", g.Angle)
			}
		})
	}
}

func TestValueExtremes(t *testing.T) {
	for _, in := range []string{"1e39px", "3.4e38in", "-3.4e38cm", "1e-50em", "+.5em", "-0px", "1Q", "1q", "1E3PX"} {
		l, ok := ParseLength(valueTokens(t, in))
		if !ok || math.IsNaN(l.Value) || math.IsInf(l.Value, 0) || math.Abs(l.Value) > MaxLengthPx {
			t.Errorf("%s: %+v %v", in, l, ok)
		}
	}
	for _, in := range []string{
		"rgb(1e30 1e30 1e30 / 1e30)", "rgb(-1e30%, -1e30%, -1e30%)", "rgba(0,0,0,1e-50)", "rgb(3.4e38 0 0)",
	} {
		c, ok := ParseColor(valueTokens(t, in))
		if !ok {
			t.Errorf("%s rejected", in)
		}
		for _, x := range []float64{c.R, c.G, c.B, c.A} {
			if !(x >= 0 && x <= 1) {
				t.Errorf("%s: component %v", in, x)
			}
		}
	}
	for _, in := range []string{
		"calc(1px)", "calc(1px + 2px)", "CALC(1px)", "calc(0)", "-webkit-calc(1px)", "min(1px, 2px)", "max(1px,2px)",
		"clamp(1px, 2px, 3px)", "calc(1px", "calc()", "calc(calc(1px))",
	} {
		if l, ok := ParseLength(valueTokens(t, in)); ok {
			t.Errorf("length %s accepted: %+v", in, l)
		}
		if c, ok := ParseColor(valueTokens(t, in)); ok {
			t.Errorf("color %s accepted: %+v", in, c)
		}
	}
	for _, in := range []string{
		"rgb(calc(1) 2 3)", "rgb(1 2 calc(3))", "rgb(1, 2, 3, calc(1))", "rgb(1 2 3 / calc(1))",
		"linear-gradient(calc(1deg), red, blue)", "linear-gradient(red calc(10%), blue)",
		"linear-gradient(red, blue calc(1% + 1%))",
	} {
		toks := valueTokens(t, in)
		if c, ok := ParseColor(toks); ok {
			t.Errorf("%s accepted as color: %+v", in, c)
		}
		if g, ok := ParseGradient(toks); ok {
			t.Errorf("%s accepted as gradient: %+v", in, g)
		}
	}
}

// Nesting just under and over MaxNesting inside a color or gradient.
func TestValueNestingDoesNotPanic(t *testing.T) {
	for _, n := range []int{MaxNesting - 1, MaxNesting, MaxNesting + 1, 5000} {
		src := "linear-gradient(" + strings.Repeat("rgb(", n) + "1,2,3" + strings.Repeat(")", n) + ", red)"
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ds, err := ParseDeclarations([]byte("x:"+src+";y:"+strings.Repeat("(", n)+"red"+strings.Repeat(")", n)), b)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			if _, ok := ParseGradient(d.Value); ok {
				t.Errorf("depth %d: accepted", n)
			}
			ParseColor(d.Value)
			ParseLength(d.Value)
		}
	}
}

// Every color and gradient written in the four corpus stylesheets, found by
// parsing them rather than by scraping the text, must be accepted.
func TestCorpusStylesheetValues(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	var colors, gradients int
	var walk func(name string, rules []Rule)
	check := func(name string, d Declaration) {
		for _, f := range fields(d.Value) {
			if len(f) == 0 {
				continue
			}
			f = substVars(f)
			isVar := false
			for _, tk := range f {
				if tk.Type == TFunction && strings.EqualFold(tk.Text, "var") {
					isVar = true
				}
			}
			switch {
			case isVar:
			case f[0].Type == THash || f[0].Type == TFunction && (strings.EqualFold(f[0].Text, "rgb") || strings.EqualFold(f[0].Text, "rgba")):
				colors++
				if _, ok := ParseColor(f); !ok {
					t.Errorf("%s: %s: color %q rejected", name, d.Name, dumpTokens(f))
				}
			case f[0].Type == TFunction && strings.EqualFold(f[0].Text, "linear-gradient"):
				gradients++
				if _, ok := ParseGradient(f); !ok {
					t.Errorf("%s: %s: gradient %q rejected", name, d.Name, dumpTokens(f))
				}
			case (d.Name == "color" || d.Name == "background-color") && !cssKeyword(f):
				colors++
				if _, ok := ParseColor(f); !ok {
					t.Errorf("%s: %s: %q rejected", name, d.Name, dumpTokens(f))
				}
			}
		}
	}
	walk = func(name string, rules []Rule) {
		for _, r := range rules {
			for _, d := range r.Decls {
				check(name, d)
			}
			walk(name, r.Rules)
		}
	}
	for _, name := range names {
		src := corpusStyles(t, name)
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ss, err := ParseStylesheet([]byte(src), OriginDocument, b)
		if err != nil {
			t.Fatal(err)
		}
		walk(name, ss.Rules)
		html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, testcorpus.Small)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range styleAttrRE.FindAllSubmatch(html, -1) {
			ds, err := ParseDeclarations(m[1], b)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range ds {
				check(name+" style=", d)
			}
		}
	}
	t.Logf("%d colors and %d gradients checked in %d stylesheets", colors, gradients, len(names))
	if colors < 50 || gradients == 0 {
		t.Fatalf("only %d colors and %d gradients found", colors, gradients)
	}
}

func corpusStyles(tb testing.TB, name string) string {
	tb.Helper()
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, testcorpus.Small)
	if err != nil {
		tb.Fatal(err)
	}
	var out strings.Builder
	rest := string(html)
	for {
		i := strings.Index(rest, "<style")
		if i < 0 {
			return out.String()
		}
		rest = rest[i:]
		start := strings.IndexByte(rest, '>') + 1
		end := strings.Index(rest, "</style>")
		out.WriteString(rest[start:end])
		out.WriteByte('\n')
		rest = rest[end:]
	}
}

func cssKeyword(f []Token) bool {
	if len(f) != 1 || f[0].Type != TIdent {
		return false
	}
	switch strings.ToLower(f[0].Text) {
	case "inherit", "initial", "unset", "currentcolor":
		return true
	}
	return false
}

var styleAttrRE = regexp.MustCompile(`style="([^"]*)"`)

// substVars replaces every var() call by a hex color token, standing for the
// resolved custom property.
func substVars(f []Token) []Token {
	var out []Token
	for i := 0; i < len(f); i++ {
		if f[i].Type == TFunction && strings.EqualFold(f[i].Text, "var") {
			out = append(out, Token{Type: THash, Text: "123456"})
			i += int(f[i].End) - 1
			continue
		}
		out = append(out, f[i])
	}
	var stack []int
	for i := range out {
		switch {
		case isOpener(out[i].Type):
			stack = append(stack, i)
		case isCloser(out[i].Type) && len(stack) > 0:
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			out[j].End = clampIdx(i - j + 1)
		}
	}
	for _, j := range stack {
		out[j].End = clampIdx(len(out) - j)
	}
	return out
}

// An unclosed function at the end of a style attribute, with trailing
// whitespace, is still a complete value (CSS Syntax §5).
func TestUnclosedFunctionTrailingSpace(t *testing.T) {
	for _, src := range []string{"color: rgb(1 2 3 ", "color: rgb(1, 2, 3 \n", "color:rgb(1 2 3 / 0.5 ", "color:rgb(1,2,3"} {
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ds, err := ParseDeclarations([]byte(src), b)
		if err != nil || len(ds) != 1 {
			t.Fatalf("%q: %d declarations, err %v", src, len(ds), err)
		}
		if c, ok := ParseColor(ds[0].Value); !ok || c.B != 3.0/255 {
			t.Errorf("%q: %+v %v", src, c, ok)
		}
	}
	b := budget.New(context.Background(), budget.Defaults(), nil)
	ds, err := ParseDeclarations([]byte("background: linear-gradient(red, blue "), b)
	if err != nil || len(ds) != 1 {
		t.Fatal(err, ds)
	}
	if _, ok := ParseGradient(ds[0].Value); !ok {
		t.Error("unclosed gradient rejected")
	}
}
