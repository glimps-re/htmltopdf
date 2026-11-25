package htmltopdf

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func TestNewZeroOptions(t *testing.T) {
	if _, err := New(Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	ref := referenceOptions(t)
	bad := func(mod func(*Options)) Options {
		o := Options{}
		mod(&o)
		return o
	}
	tests := []struct {
		name string
		opts Options
	}{
		{"scale too small", Options{Scale: 0.05}},
		{"scale too large", Options{Scale: 2.5}},
		{"scale negative", Options{Scale: -1}},
		{"scale NaN", Options{Scale: math.NaN()}},
		{"scale infinite", Options{Scale: math.Inf(1)}},
		{"negative MaxInputBytes", Options{Limits: Limits{MaxInputBytes: -1}}},
		{"negative MaxNodes", Options{Limits: Limits{MaxNodes: -1}}},
		{"negative MaxCSSBytes", Options{Limits: Limits{MaxCSSBytes: -1}}},
		{"negative MaxSelectors", Options{Limits: Limits{MaxSelectors: -1}}},
		{"negative MaxVarBytes", Options{Limits: Limits{MaxVarBytes: -1}}},
		{"negative MaxStyles", Options{Limits: Limits{MaxStyles: -1}}},
		{"negative MaxBoxes", Options{Limits: Limits{MaxBoxes: -1}}},
		{"negative MaxGlyphs", Options{Limits: Limits{MaxGlyphs: -1}}},
		{"negative MaxPages", Options{Limits: Limits{MaxPages: -1}}},
		{"negative MaxOutputBytes", Options{Limits: Limits{MaxOutputBytes: -1}}},
		{"negative MaxWork", Options{Limits: Limits{MaxWork: -1}}},
		{"negative Timeout", Options{Limits: Limits{Timeout: -time.Second}}},
		{"empty font", Options{Fonts: []Font{{}}}},
		{"garbage font", Options{Fonts: []Font{{Data: []byte("not a font at all")}}}},
		{"truncated font", Options{Fonts: []Font{{Data: ref.Fonts[0].Data[:200]}}}},
		{"collection", Options{Fonts: []Font{{Data: append([]byte("ttcf"), make([]byte, 64)...)}}}},
		{"CFF font", Options{Fonts: []Font{{Data: append([]byte("OTTO"), make([]byte, 64)...)}}}},
		{"font without family", bad(func(o *Options) { o.Fonts = []Font{{Data: ref.Fonts[0].Data, Family: strings.Repeat("x", 1000)}} })},
		{"too many fonts", bad(func(o *Options) {
			for range 70 {
				o.Fonts = append(o.Fonts, ref.Fonts[0])
			}
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if c, err := New(tt.opts); !errors.Is(err, ErrInvalidOptions) || c != nil {
				t.Fatalf("got %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestNewAcceptsCallerFonts(t *testing.T) {
	ref := referenceOptions(t)
	c, err := New(Options{Fonts: ref.Fonts[:1], Fallback: []string{"Nope"}, Generic: GenericFamilies{Serif: "Liberation Sans"}})
	if err != nil {
		t.Fatal(err)
	}
	renderBytes(t, c, "<p style='font-family: serif'>x</p>")
	// Family overrides the name of the font.
	c, err = New(Options{Fonts: []Font{{Data: ref.Fonts[0].Data, Family: "My Face"}}})
	if err != nil {
		t.Fatal(err)
	}
	renderBytes(t, c, "<p style=\"font-family: 'My Face'\">x</p>")
}

func TestExtraCSSIsStrict(t *testing.T) {
	rejected := map[string]string{
		"syntax error":              "p { color: red",
		"unknown property":          "p { frobnicate: 1 }",
		"invalid value":             "p { color: notacolor }",
		"unsupported value":         "p { display: inline-flex }",
		"unsupported at-rule":       "@font-face { font-family: x }",
		"import":                    "@import 'x.css';",
		"invalid selector":          "p:nope { color: red }",
		"stray brace":               "} p { color: red }",
		"corner margin box":         "@page { @top-left-corner { content: 'x' } }",
		"side margin box":           "@page { @left-top { content: 'x' } }",
		"invalid margin box value":  "@page { @bottom-left { content: attr(title) } }",
		"page size":                 "@page { size: nonsense }",
		"page size clamp":           "@page { size: 99999999in }",
		"page margin clamp":         "@page { size: 10in 10in; margin: 50in }",
		"var in page":               "@page { margin: var(--m) }",
		"bad media query":           "@media (frobnicate: 1) { p { color: red } }",
		"pseudo-class in selector":  "a:visited:nope { color: red }",
		"unterminated string":       "p { content: 'x }",
		"too large":                 strings.Repeat("p{color:red}", 100000),
		"unsupported float":         "p { float: left }",
		"unsupported page selector": "@page :first { size: A3 }",
	}
	for name, src := range rejected {
		t.Run(name, func(t *testing.T) {
			if _, err := New(Options{ExtraCSS: src}); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("got %v, want ErrInvalidOptions", err)
			}
		})
	}
	valid := []string{
		"", "  \n ", "@page { size: A4; margin: 1in 0.3in 1.3in }", "p { color: red !important }",
		"@media print { p { margin: 0 } }", "/* only a comment */",
		"@page { @bottom-right { content: 'Page ' counter(page) ' of ' counter(pages) } }",
	}
	for _, src := range valid {
		if _, err := New(Options{ExtraCSS: src}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestExtraCSSBeatsTheDocument(t *testing.T) {
	c := mustNew(t, Options{ExtraCSS: "p { color: #00ff00 }"})
	_, data := renderBytes(t, c, `<style>p { color: #ff0000 !important } #i { color: #0000ff !important }</style><p id=i style="color: #aabbcc !important">x</p>`)
	if content := pagesContent(t, data); !strings.Contains(content, "0 1 0 rg") || strings.Contains(content, "1 0 0 rg") || strings.Contains(content, "0 0 1 rg") {
		t.Errorf("ExtraCSS must win:\n%s", content)
	}
}

func TestDefaultLimits(t *testing.T) {
	d := DefaultLimits()
	want := Limits{
		MaxInputBytes: 2 << 20, MaxNodes: 100_000, MaxCSSBytes: 512 << 10, MaxSelectors: 5000, MaxVarBytes: 256 << 10,
		MaxStyles: 20_000, MaxBoxes: 200_000, MaxGlyphs: 2_000_000, MaxPages: 500, MaxOutputBytes: 64 << 20,
		MaxWork: budget.Defaults()[budget.Work], Timeout: 60 * time.Second,
	}
	if d != want {
		t.Errorf("DefaultLimits = %+v, want %+v", d, want)
	}
	if r, err := (Limits{}).resolve(); err != nil || r.limits != budget.Defaults() || r.timeout != 60*time.Second {
		t.Errorf("zero Limits resolve to %+v, %v", r, err)
	}
	r, err := Limits{MaxPages: 7, Timeout: time.Second}.resolve()
	if err != nil || r.limits[budget.Pages] != 7 || r.limits[budget.Nodes] != 100_000 || r.timeout != time.Second {
		t.Errorf("partial Limits resolve to %+v, %v", r, err)
	}
}

func TestLimitError(t *testing.T) {
	err := error(&LimitError{Limit: LimitNodes, Max: 5})
	if !errors.Is(err, ErrLimit) || errors.Is(err, ErrInternal) {
		t.Error("Is(ErrLimit)")
	}
	if got := err.Error(); got != "htmltopdf: limit exceeded: MaxNodes (max 5)" {
		t.Errorf("message %q", got)
	}
}
