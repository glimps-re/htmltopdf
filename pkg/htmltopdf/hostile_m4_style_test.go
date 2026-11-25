package htmltopdf

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Every M4 declaration through Render: a valid value draws no warning, an invalid one is dropped with an
// invalid-value warning, an out-of-range one is clamped and says so, and the text is in the PDF once
// whatever happens to the property.
func TestM4StyleDeclarations(t *testing.T) {
	const (
		ok      = ""
		invalid = "invalid-value: "
		clamped = "clamped: "
	)
	for _, tc := range []struct {
		decl string
		warn string // prefix of the only warning, ok for none
	}{
		{"position:fixed", ok},
		{"position:FIXED", ok},
		{"position:fixed !important", ok},
		{"position:sticky", invalid + "position"},
		{"columns:2", ok},
		{"columns:auto", ok},
		{"columns:100px", invalid + "columns"},
		{"columns:2 100px", invalid + "columns"},
		{"columns:2 2", invalid + "columns"},
		{"columns:0", invalid + "columns"},
		{"columns:-1", invalid + "columns"},
		{"columns:1.5", invalid + "columns"},
		{"columns:1e9", invalid + "columns"},
		{"column-count:3", ok},
		{"column-count:auto", ok},
		{"column-count:0", invalid + "column-count"},
		{"column-count:-1", invalid + "column-count"},
		{"column-count:1.5", invalid + "column-count"},
		{"column-count:1000000000", clamped + "columns"},
		{"column-width:100px", invalid + "column-width"},
		{"column-gap:normal", ok},
		{"column-gap:10px", ok},
		{"column-gap:-1px", invalid + "column-gap"},
		{"column-gap:1e30px", clamped + "length beyond"},
		{"column-rule:1px solid red", ok},
		{"column-rule:2px dashed #00f", ok},
		{"column-rule:3px dotted", ok},
		{"column-rule:1px groove red", invalid + "column-rule"},
		{"column-rule:1e9px solid red", clamped + "length beyond"},
		{"column-rule-style:dashed", ok},
		{"column-rule-style:double", invalid + "column-rule-style"},
		{"column-rule-width:0.5pt", ok},
		{"column-rule-width:-1px", invalid + "column-rule-width"},
		{"column-rule-color:red", ok},
		{"column-rule-color:nonsense", invalid + "column-rule-color"},
		{"text-decoration-thickness:auto", ok},
		{"text-decoration-thickness:from-font", ok},
		{"text-decoration-thickness:2px", ok},
		{"text-decoration-thickness:0", ok},
		{"text-decoration-thickness:50%", invalid + "text-decoration-thickness"},
		{"text-decoration-thickness:-1px", invalid + "text-decoration-thickness"},
		{"text-decoration-thickness:1e30px", clamped + "length beyond"},
		{"border:1px dashed red", ok},
		{"border:1px dotted red", ok},
		{"border-style:dashed dotted solid none", ok},
		{"border-top-style:dotted", ok},
		{"border-style:double", invalid + "border-style"},
		{"border-style:groove", invalid + "border-style"},
		{"break-before:column", invalid + "break-before"},
		{"break-after:column", invalid + "break-after"},
	} {
		t.Run(tc.decl, func(t *testing.T) {
			words, body := h4Markers("STY", 3)
			r := h4Render(t, referenceOptions(t), "<style>p{"+tc.decl+"}</style>"+body, false)
			if r.err != nil {
				t.Fatal(r.err)
			}
			ws := h4Warnings(r.res)
			switch {
			case tc.warn == ok && len(ws) != 0:
				t.Errorf("warnings %v, want none", ws)
			case tc.warn != ok && (len(ws) != 1 || !strings.HasPrefix(ws[0], tc.warn)):
				t.Errorf("warnings %v, want one starting %q", ws, tc.warn)
			}
			h4TextOnce(t, r, words)
		})
	}
}

// ExtraCSS is strict: an M4 value that would draw a warning makes New fail, a valid one is accepted.
func TestM4StyleExtraCSSIsStrict(t *testing.T) {
	for decl, valid := range map[string]bool{
		"position:fixed": true, "columns:2": true, "column-rule:1px dashed red": true, "text-decoration-thickness:from-font": true,
		"columns:100px": false, "column-rule:1px groove red": false, "column-count:0": false, "border-style:double": false, "column-gap:1e30px": false,
	} {
		o := referenceOptions(t)
		o.ExtraCSS = "p{" + decl + "}"
		_, err := New(o)
		if valid && err != nil {
			t.Errorf("%s: %v", decl, err)
		}
		if !valid && !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: %v, want ErrInvalidOptions", decl, err)
		}
	}
}

// Bombs aimed at the M4 properties, each held against a twin without them (same boxes, same text):
// equal Boxes and Glyphs, Work within twice the twin's. They cover a var() chain into columns, the rule
// and the thickness (which must reach the substitution budget and say so), 12 000 declarations in one
// rule, 5 000 matched rules with M4 values, and huge dashed and dotted borders, with the text present
// once whatever the outcome.
func TestM4StyleBombs(t *testing.T) {
	chain := func(levels int, step string) string {
		var sb strings.Builder
		sb.WriteString(":root{--a:" + step + ";")
		prev := "--a"
		for i := range levels {
			ref := "var(" + prev + ")"
			if step == "2" {
				ref += " " + ref
			}
			fmt.Fprintf(&sb, "--v%d:%s;", i, ref)
			prev = fmt.Sprintf("--v%d", i)
		}
		sb.WriteString("}p{" + usesOf("var("+prev+")") + "}")
		return sb.String()
	}
	flat := func(value string) string {
		return ":root{--v:" + value + "}p{" + usesOf("var(--v)") + "}"
	}
	many := func(n int) string {
		return "p{" + strings.Repeat("columns:2;column-rule:1px dashed red;position:fixed;", n) + "}"
	}
	var rules, plain, elems strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&rules, ".c%d{columns:%d;column-rule:%dpx dotted #%06x}", i, i%9+1, i%5+1, i*37)
		fmt.Fprintf(&plain, ".c%d{color:#%06x}", i, i*37)
		fmt.Fprintf(&elems, `<div class="c%d"></div>`, i)
	}
	for _, tc := range []struct {
		name, css, twinCSS, extraBody string
		wantLimit                     Limit
		wantWarn                      WarningCode
		noWarning                     bool
		ratio                         int // work bound in eighths of the twin's, plus an eighth of slack
	}{
		{name: "var chain under the budget, fully substituted", css: chain(14, "5px"), twinCSS: flat("5px"), noWarning: true, ratio: 10},
		{name: "doubling var chain over the budget", css: chain(30, "2"), twinCSS: flat(strings.Repeat("2 ", int(DefaultLimits().MaxVarBytes/4))), wantWarn: WarnVarBudget, ratio: 8},
		{name: "12000 declarations in one rule (twin: half as many)", css: many(4000), twinCSS: many(2000), ratio: 16},
		{name: "5000 matched rules with M4 values", css: rules.String(), twinCSS: plain.String(), extraBody: elems.String(), ratio: 12},
		{name: "1e9px dashed border", css: "p{border:1e9px dashed red}", wantLimit: LimitPages},
		{name: "300px dotted border (text stays on the page)", css: "p{border:300px dotted red}", twinCSS: "p{border:300px solid red}", ratio: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			words, body := h4Markers("BMB", 5)
			body += tc.extraBody
			r := h4RenderTwin(t, referenceOptions(t), "<style>"+tc.css+"</style>"+body)
			if tc.wantLimit != "" {
				var le *LimitError
				if !errors.As(r.err, &le) || le.Limit != tc.wantLimit {
					t.Fatalf("%v, want a LimitError on %s", r.err, tc.wantLimit)
				}
				return
			}
			if r.err != nil {
				t.Fatal(r.err)
			}
			h4TextOnce(t, r, words)
			has := slices.ContainsFunc(r.res.Warnings, func(w Warning) bool { return w.Code == WarnVarBudget })
			if tc.noWarning && len(r.res.Warnings) != 0 {
				t.Errorf("warnings %v, want none: the chain is substituted whole", h4Warnings(r.res))
			}
			if tc.wantWarn != "" && !has {
				t.Errorf("no %s warning in %v", tc.wantWarn, h4Warnings(r.res))
			}
			twin := h4Render(t, referenceOptions(t), "<style>"+tc.twinCSS+"</style>"+body, false)
			if twin.err != nil {
				t.Fatal(twin.err)
			}
			if r.boxes != twin.boxes || r.glyphs != twin.glyphs || r.work > int64(tc.ratio)*twin.work/8+twin.work/8 {
				t.Errorf("work %d boxes %d glyphs %d; twin work %d boxes %d glyphs %d (bound %d/8 + 1/8)", r.work, r.boxes, r.glyphs, twin.work, twin.boxes, twin.glyphs, tc.ratio)
			}
		})
	}
}

func usesOf(v string) string {
	return "columns:" + v + ";column-rule:" + v + " solid red;column-gap:" + v + ";text-decoration-thickness:" + v
}
