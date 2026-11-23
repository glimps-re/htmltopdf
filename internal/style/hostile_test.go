package style

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// resolveUntilLimit computes every style and returns the first error.
func resolveUntilLimit(tb testing.TB, d *dom.Document, sheets []*Sheet, b *budget.Budget) error {
	tb.Helper()
	r := NewResolver(ua(), nil, sheets, printEnv, b)
	styles := make([]*Computed, len(d.Elements))
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		c, err := r.Element(d, clampIdx32(i), parent)
		if err != nil {
			return err
		}
		styles[i] = c
	}
	runtime.KeepAlive(styles)
	return nil
}

func clampIdx32(i int) int32 { return int32(min(max(i, 0), math.MaxInt32)) }

// A :root with thousands of custom properties and many elements whose
// style attribute adds one more: each distinct style must not copy the whole
// set. The probe stays under 200 MiB; the retained bytes per unique element,
// projected to MaxStyles, must stay within 512 MiB, unless a limit error ends
// the run first.
func TestCustomPropertyBombIsBounded(t *testing.T) {
	const customs, elements = 5000, 300
	var sb strings.Builder
	sb.WriteString(":root{")
	for i := range customs {
		fmt.Fprintf(&sb, "--c%d:%d;", i, i)
	}
	sb.WriteString("}")
	var h strings.Builder
	for i := range elements {
		fmt.Fprintf(&h, `<p style="--z:%d">`, i)
	}
	b := newBudget()
	d, err := dom.Parse(strings.NewReader(h.String()), b)
	if err != nil {
		t.Fatal(err)
	}
	sh := compileSheet(t, sb.String(), css.OriginDocument, b)
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	start := time.Now()
	r := NewResolver(ua(), nil, []*Sheet{sh}, printEnv, b)
	styles := make([]*Computed, len(d.Elements))
	var limit error
	done := 0
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = styles[p]
		}
		c, err := r.Element(d, clampIdx32(i), parent)
		if err != nil {
			limit = err
			break
		}
		styles[i] = c
		done++
	}
	runtime.GC()
	runtime.ReadMemStats(&m1)
	retained := float64(m1.HeapAlloc) - float64(m0.HeapAlloc)
	per := retained / float64(max(done, 1))
	projected := per * float64(budget.Defaults()[budget.Styles])
	t.Logf("%d of %d elements, limit error %v, retained %.1f MiB (%.0f KiB per element), projected at MaxStyles %.2f GiB, %v",
		done, len(d.Elements), limit, retained/(1<<20), per/1024, projected/(1<<30), time.Since(start).Round(time.Millisecond))
	runtime.KeepAlive(styles)
	var le *budget.LimitError
	if limit != nil && !errors.As(limit, &le) {
		t.Fatalf("error %v", limit)
	}
	if limit == nil && projected > 512<<20 {
		t.Errorf("%.0f KiB retained per element with a distinct custom property: %.1f GiB at MaxStyles (20 000)", per/1024, projected/(1<<30))
	}
}

func TestCustomCycleWithFallbackIsInvalid(t *testing.T) {
	for _, tc := range []struct {
		name, css string
		want      float64
	}{
		{"self reference with fallback, no fallback at use", `p { --a: var(--a, 5px); margin-left: var(--a) }`, 0},
		{"self reference with fallback, fallback at use", `p { --a: var(--a, 5px); margin-left: var(--a, 9px) }`, 9},
		{"two-cycle with fallbacks", `p { --a: var(--b, 1px); --b: var(--a, 2px); margin-left: var(--a) }`, 0},
		{"two-cycle with fallbacks, use fallback", `p { --a: var(--b, 1px); --b: var(--a, 2px); margin-left: var(--b, 3px) }`, 3},
		{"cycle member used by a non-member", `p { --a: var(--b); --b: var(--a, 7px); --c: var(--a, 4px); margin-left: var(--c) }`, 4},
		{"cycle through fallback", `p { --a: var(--zz, var(--a)); margin-left: var(--a, 6px) }`, 6},
		{"no cycle", `p { --a: var(--b, 1px); --b: 2px; margin-left: var(--a) }`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := resolve(t, `<p>x</p>`, tc.css, "")
			if got := x.by(t, "p").Margin[Left].Value; got != tc.want {
				t.Errorf("margin-left = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("child inherits an invalid cycle", func(t *testing.T) {
		x := resolve(t, `<div><p>x</p></div>`, `div { --a: var(--a, 5px) } p { margin-left: var(--a) }`, "")
		if got := x.by(t, "p").Margin[Left].Value; got != 0 {
			t.Errorf("margin-left = %v, want 0", got)
		}
	})
	t.Run("long chain and long cycle", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString("p {")
		for i := range 2000 {
			fmt.Fprintf(&sb, "--v%d: var(--v%d, 1px);", i, (i+1)%2000)
		}
		sb.WriteString("margin-left: var(--v0, 8px) }")
		start := time.Now()
		x := resolve(t, `<p>x</p>`, sb.String(), "")
		if got := x.by(t, "p").Margin[Left].Value; got != 8 {
			t.Errorf("margin-left = %v, want 8", got)
		}
		t.Logf("2000-long cycle: %v", time.Since(start).Round(time.Millisecond))
	})
}

func TestLineHeightTimesFontSizeIsClamped(t *testing.T) {
	for _, css := range []string{
		`p { font-size: 1e4px; line-height: 1e7 }`,
		`p { font-size: 1e30px; line-height: 1e30 }`,
		`p { font-size: 10000px; line-height: 1e7px }`,
		`p { font-size: 10000px; line-height: 1e7% }`,
		`p { font-size: 1e7em; line-height: 1e7em }`,
		`p { font-size: 10000px; line-height: 3.4e38 }`,
	} {
		x := resolve(t, `<p>x</p>`, css, "")
		c := x.by(t, "p")
		lh := c.LineHeight
		eff := lh.Value
		if lh.Kind == LineHeightNumber {
			eff = lh.Value * c.FontSize
		}
		if lh.Kind != LineHeightNormal && (math.IsNaN(eff) || math.IsInf(eff, 0) || eff > 2*css0Max) {
			t.Errorf("%s: effective line height %g px (font-size %g, kind %d)", css, eff, c.FontSize, lh.Kind)
		}
	}
}

// css0Max is the bound of every length: the product of a number line-height
// and the font size must respect it too, or layout overflows.
const css0Max = css.MaxLengthPx

func TestMarginBoxContentIsBounded(t *testing.T) {
	const maxBytes = 1024
	for _, n := range []int{maxBytes - 1, maxBytes, maxBytes + 1, 100_000} {
		ps, b := pageOf(t, "", `@page { @bottom-left { content: "`+strings.Repeat("a", n)+`" } }`)
		total := 0
		if box := ps.Boxes[MarginBottomLeft]; box != nil {
			for _, it := range box.Content {
				total += len(it.Text)
			}
		}
		ws, _ := b.Warnings()
		t.Logf("%d bytes of content: kept %d, %d warnings", n, total, len(ws))
		if total > maxBytes {
			t.Errorf("%d bytes in: %d bytes kept (limit %d)", n, total, maxBytes)
		}
		if n > maxBytes && len(ws) == 0 {
			t.Errorf("%d bytes in: cut or dropped silently", n)
		}
		if n <= maxBytes && (total != n || len(ws) != 0) {
			t.Errorf("%d bytes in (within the limit): kept %d, warnings %v", n, total, ws)
		}
	}
	// 64 items of a few KiB each: the total is bounded too.
	var items strings.Builder
	for range 64 {
		items.WriteString(`"` + strings.Repeat("b", 4096) + `" `)
	}
	ps, _ := pageOf(t, "", `@page { @bottom-left { content: `+items.String()+` } }`)
	total := 0
	if box := ps.Boxes[MarginBottomLeft]; box != nil {
		for _, it := range box.Content {
			total += len(it.Text)
		}
	}
	if total > maxBytes {
		t.Errorf("64 × 4 KiB items: %d bytes kept", total)
	}
}

var hostileStyleRE = regexp.MustCompile(`(?s)<style>(.*?)</style>`)

// The var and style hostile documents of testcorpus, at the default limits.
func TestCorpusVarAndStyleCases(t *testing.T) {
	for _, c := range testcorpus.Hostile(budget.Defaults()) {
		if c.Name != "var_doubling_over" && c.Name != "var_doubling_under" && c.Name != "styles_unique_over" && c.Name != "styles_unique_under" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			l := budget.Defaults()
			l[budget.InputBytes] = 64 << 20
			l[budget.Nodes] = 10 << 20
			b := budget.New(t.Context(), l, nil)
			src := c.HTML()
			d, err := dom.Parse(strings.NewReader(string(src)), b)
			if err != nil {
				t.Fatal(err)
			}
			var sheets []*Sheet
			for _, m := range hostileStyleRE.FindAllSubmatch(src, -1) {
				sheets = append(sheets, compileSheet(t, string(m[1]), css.OriginDocument, b))
			}
			var peak runtime.MemStats
			start := time.Now()
			err = resolveUntilLimit(t, d, sheets, b)
			took := time.Since(start)
			runtime.ReadMemStats(&peak)
			ws, _ := b.Warnings()
			t.Logf("%v, err %v, %d warnings, %v, heap %d MiB", c.Name, err, len(ws), took.Round(time.Millisecond), peak.HeapAlloc>>20)
			var le *budget.LimitError
			switch c.Name {
			case "var_doubling_over":
				if err != nil {
					t.Fatalf("a var bomb is degraded, not fatal: %v", err)
				}
				found := false
				for _, w := range ws {
					found = found || w.Code == WarnVarBudget
				}
				if !found {
					t.Errorf("no %s warning: %v", WarnVarBudget, ws)
				}
			case "var_doubling_under":
				if err != nil {
					t.Fatal(err)
				}
				for _, w := range ws {
					if w.Code == WarnVarBudget {
						t.Errorf("the under twin hits the var budget: %v", w)
					}
				}
			case "styles_unique_over":
				if !errors.As(err, &le) || le.Kind != budget.Styles {
					t.Fatalf("got %v, want LimitStyles", err)
				}
			case "styles_unique_under":
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestExtraCSSBeatsEveryImportant(t *testing.T) {
	html := `<div id=a class=c style="color:#020202 !important; margin-left: 7px !important; --k: 3px !important"><p id=i class=c style="width: 77px !important">x</p></div>`
	doc := `#a.c { color: #010101 !important; margin-left: 9px !important } div#a { margin-left: 8px !important } p#i.c { width: 66px !important }`
	extra := `div { color: #0a0a0a; margin-left: 1px } p { width: 2px }`
	x := resolve(t, html, doc, extra)
	div, p := x.by(t, "#a"), x.by(t, "#i")
	if div.Color != rgb(10, 10, 10) || div.Margin[Left].Value != 1 {
		t.Errorf("div: color %+v margin-left %v", div.Color, div.Margin[Left])
	}
	if p.Width.Value != 2 {
		t.Errorf("p width %v, want ExtraCSS's 2", p.Width)
	}
	// ExtraCSS !important beats ExtraCSS normal only by source order.
	x = resolve(t, html, doc, `div { margin-left: 4px !important } div { margin-left: 5px }`)
	if got := x.by(t, "#a").Margin[Left].Value; got != 5 {
		t.Errorf("inside ExtraCSS the order decides: %v", got)
	}
}

// A document rule beats a UA rule whatever the specificity (design §9.1): the
// UA sheet's `a[href]` must not outrank the document's `a`.
func TestDocumentBeatsUASheetWhateverTheSpecificity(t *testing.T) {
	for _, tc := range []struct {
		name, css string
		check     func(c *Computed) bool
	}{
		{"a overrides a[href] color", `a { color: #ff0000 }`, func(c *Computed) bool { return c.Color == rgb(255, 0, 0) }},
		{"a overrides a[href] decoration", `a { text-decoration: none }`, func(c *Computed) bool { return c.TextDecoration == 0 }},
		{"html body a overrides a[href]", `html body a { text-decoration: none }`, func(c *Computed) bool { return c.TextDecoration == 0 }},
		{"* overrides u underline", `* { text-decoration: none }`, func(c *Computed) bool { return c.TextDecoration == 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := resolve(t, `<a href=x>y</a>`, tc.css, "")
			if !tc.check(x.by(t, "a")) {
				c := x.by(t, "a")
				t.Errorf("the UA sheet won: color %+v decoration %d", c.Color, c.TextDecoration)
			}
		})
	}
	x := resolve(t, `<u>y</u>`, `* { text-decoration: none }`, "")
	if x.by(t, "u").TextDecoration != 0 {
		t.Errorf("`* {text-decoration:none}` loses to the UA rule `u`: %d", x.by(t, "u").TextDecoration)
	}
	x = resolve(t, `<a href=x>y</a>`, "", `a { color: #00ff00 }`)
	if x.by(t, "a").Color != rgb(0, 255, 0) {
		t.Errorf("ExtraCSS a loses to the UA a[href]: %+v", x.by(t, "a").Color)
	}
}
