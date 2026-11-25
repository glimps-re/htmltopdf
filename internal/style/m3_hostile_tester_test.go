package style

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func allocBytes(fn func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	fn()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// The pseudo-element style of 40 000 elements sharing one rule is one shared
// Computed, and the work is bounded against the twin without pseudo rules.
func TestManyPseudoElementsShareStyles(t *testing.T) {
	const n = 40000
	html := strings.Repeat(`<span>x</span>`, n)
	run := func(docCSS string) (distinct, boxes int, alloc uint64) {
		b := newBudget()
		d, err := dom.Parse(strings.NewReader(html), b)
		if err != nil {
			t.Fatal(err)
		}
		r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, docCSS, css.OriginDocument, b)}, printEnv, b)
		styles := make([]*Computed, len(d.Elements))
		seen := map[*Computed]bool{}
		alloc = allocBytes(func() {
			for i := range d.Elements {
				var parent *Computed
				if p := d.Elements[i].Parent; p >= 0 {
					parent = styles[p]
				}
				c, err := r.Element(d, int32(i), parent)
				if err != nil {
					t.Fatal(err)
				}
				styles[i] = c
				for _, pe := range []css.PseudoElement{css.PseudoBefore, css.PseudoAfter} {
					p, err := r.Pseudo(d, int32(i), pe, c)
					if err != nil {
						t.Fatal(err)
					}
					if p != nil {
						boxes++
						seen[p] = true
					}
				}
			}
		})
		return len(seen), boxes, alloc
	}
	distinct, boxes, with := run(`span::before{content:"a";color:red} span::after{content:counter(c);counter-increment:c 3;position:absolute;top:1px}`)
	_, _, twin := run(`span{color:red}`)
	if boxes != 2*n || distinct != 2 {
		t.Errorf("%d pseudo boxes, %d distinct styles, want %d and 2", boxes, distinct, 2*n)
	}
	t.Logf("pseudo run %d MiB, twin %d MiB", with>>20, twin>>20)
	if with > twin+uint64(n)*160 {
		t.Errorf("pseudo elements cost %d bytes each beyond the twin", (with-twin)/n)
	}
}

// 5 000 distinct named pages: a cache entry per name used, none for unknown
// names, at most one PageStyle allocation per name.
func TestNamedPagesCostPerNameUsed(t *testing.T) {
	const n = 5000
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "@page n%d { margin: %dpx; @top-center { content: \"h%d\" } }\n", i, i%50+1, i)
	}
	b := newBudget()
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, sb.String(), css.OriginDocument, b)}, printEnv, b)
	def := r.Page(PageContext{})
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("n%d", i)
	}
	got := allocBytes(func() {
		for i := range n {
			r.Page(PageContext{Number: i + 1, Name: names[i]})
		}
	})
	t.Logf("%d bytes for %d names (%d per name)", got, n, got/n)
	if per := got / n; per > 8192 {
		t.Errorf("%d bytes per named page", per)
	}
	unknownNames := make([]string, 100000)
	for i := range unknownNames {
		unknownNames[i] = "zz" + fmt.Sprint(i)
	}
	unknown := allocBytes(func() {
		for i := range 100000 {
			if r.Page(PageContext{Number: i, Name: unknownNames[i]}) != def {
				t.Fatal("unknown name does not share the default page")
			}
		}
	})
	if unknown/100000 > 8 {
		t.Errorf("%d bytes per lookup of an unknown name (cache growth?)", unknown/100000)
	}
	if len(r.pageNames) != n {
		t.Errorf("%d cached names", len(r.pageNames))
	}
}

// Names at, over and weirdly beyond the identifier bound, in @page and page:.
func TestPageNamesBounds(t *testing.T) {
	ok := strings.Repeat("a", MaxIdentBytes)
	over := strings.Repeat("a", MaxIdentBytes+1)
	b := newBudget()
	sh := compileSheet(t, fmt.Sprintf("@page %s { margin: 1px } @page %s { margin: 2px }", ok, over), css.OriginDocument, b)
	r := NewResolver(ua(), nil, []*Sheet{sh}, printEnv, b)
	if r.Page(PageContext{Name: ok}).Margin[Top] != 1 {
		t.Error("a name of MaxIdentBytes is not honoured")
	}
	if ps := r.Page(PageContext{Name: over}); ps.Margin[Top] == 2 {
		t.Error("a name over MaxIdentBytes is honoured")
	}
	c, _ := computedOf(t, "page: "+over)
	if len(c.Page) > MaxIdentBytes {
		t.Errorf("page name of %d bytes kept", len(c.Page))
	}
	if c, _ := computedOf(t, "page: "+ok); c.Page != ok {
		t.Error("page: a name of MaxIdentBytes dropped")
	}
	for _, v := range []string{"page: 9abc", "page: \"cover\"", "page: cover other", "page: -", "page: auto cover", "page: initial"} {
		c, _ := computedOf(t, v)
		if c.Page != "" {
			t.Errorf("%s: Page = %q", v, c.Page)
		}
	}
	// page is not inherited and a name is case-sensitive (CSS Page 3: custom-ident).
	x := resolve(t, `<div id=a style="page:cover"><p id=b>x</p></div>`, "", "")
	if x.by(t, "#b").Page != "" {
		t.Error("page is inherited")
	}
	b = newBudget()
	r = NewResolver(ua(), nil, []*Sheet{compileSheet(t, "@page Cover{margin:3px}", css.OriginDocument, b)}, printEnv, b)
	if r.Page(PageContext{Name: "cover"}).Margin[Top] == 3 || r.Page(PageContext{Name: "Cover"}).Margin[Top] != 3 {
		t.Error("page names must be case-sensitive")
	}
}

func computedOf(t testing.TB, decl string) (*Computed, *doc) {
	t.Helper()
	x := resolve(t, `<div id=a style="`+decl+`">x</div>`, "", "")
	return x.by(t, "#a"), x
}

// Counter names and values at the edges: names beyond MaxIdentBytes drop the
// declaration, values saturate, entries stay in order, nothing panics.
func TestCounterEdgeValues(t *testing.T) {
	for decl, want := range map[string]int32{
		"counter-increment: c 2147483647":           math.MaxInt32,
		"counter-increment: c 2147483648":           math.MaxInt32,
		"counter-increment: c -2147483648":          math.MinInt32,
		"counter-increment: c -2147483649":          math.MinInt32,
		"counter-increment: c 99999999999999999999": math.MaxInt32,
		"counter-increment: c +3":                   3,
		"counter-increment: c -0":                   0,
		"counter-reset: c 0":                        0,
		"counter-increment: c 2147483647 c 1":       math.MaxInt32,
		"counter-increment: c -2147483648 c -1":     math.MinInt32,
	} {
		c, x := computedOf(t, decl)
		ops := c.CounterIncrement
		if strings.HasPrefix(decl, "counter-reset") {
			ops = c.CounterReset
		}
		if len(ops) != 1 || ops[0].Value != want {
			t.Errorf("%s: %+v (want %d), warnings %v", decl, ops, want, x.warnings())
		}
	}
	for _, decl := range []string{
		"counter-increment: c 1e3", "counter-increment: c 1.5", "counter-increment: c inherit", "counter-increment: none c", "counter-increment: c none",
		"counter-increment: 5", "counter-reset: c c2 x:", "counter-reset: , c", "counter-increment: c 1 2", "counter-reset: initial c", "counter-reset: c unset",
		"counter-increment: -", "counter-increment: --x 2", "counter-increment: c !important",
	} {
		c, _ := computedOf(t, decl)
		if len(c.CounterIncrement) != 0 && strings.HasPrefix(decl, "counter-increment") && !strings.Contains(decl, "--x") && !strings.Contains(decl, "important") {
			t.Errorf("%s: kept %+v", decl, c.CounterIncrement)
		}
		if len(c.CounterReset) != 0 && strings.HasPrefix(decl, "counter-reset") {
			t.Errorf("%s: kept %+v", decl, c.CounterReset)
		}
	}
	// A name listed twice in counter-reset: the last one wins (Chrome); in counter-increment they sum.
	c, _ := computedOf(t, "counter-reset: c 1 c 2; counter-increment: c 3 c 4")
	if len(c.CounterReset) != 1 || c.CounterReset[0].Value != 2 {
		t.Errorf("counter-reset: c 1 c 2 = %+v, Chrome: the last wins (2)", c.CounterReset)
	}
	if len(c.CounterIncrement) != 1 || c.CounterIncrement[0].Value != 7 {
		t.Errorf("counter-increment: c 3 c 4 = %+v, want 7", c.CounterIncrement)
	}
}

// 20 000 elements each with 32 counter entries and a long content list: heap
// and time stay bounded and styles are shared.
func TestCounterAndContentHeavyDocument(t *testing.T) {
	var ops strings.Builder
	for i := range MaxCounterOps {
		fmt.Fprintf(&ops, "c%d %d ", i, i)
	}
	var items strings.Builder
	for range maxContentItems {
		items.WriteString(`"x" counter(c1) `)
	}
	html := strings.Repeat(`<p>x</p>`, 20000)
	docCSS := fmt.Sprintf(`p{counter-reset:%s;counter-increment:%s} p::before{content:%s}`, ops.String(), ops.String(), items.String())
	b := newBudget()
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, docCSS, css.OriginDocument, b)}, printEnv, b)
	start := time.Now()
	seen := map[*Computed]bool{}
	got := allocBytes(func() {
		styles := make([]*Computed, len(d.Elements))
		for i := range d.Elements {
			var parent *Computed
			if p := d.Elements[i].Parent; p >= 0 {
				parent = styles[p]
			}
			c, err := r.Element(d, int32(i), parent)
			if err != nil {
				t.Fatal(err)
			}
			styles[i] = c
			p, err := r.Pseudo(d, int32(i), css.PseudoBefore, c)
			if err != nil {
				t.Fatal(err)
			}
			if p != nil {
				seen[p] = true
			}
		}
	})
	if len(seen) > 2 {
		t.Errorf("%d distinct pseudo styles", len(seen))
	}
	if got > 128<<20 || time.Since(start) > 30*time.Second {
		t.Errorf("%d MiB in %v", got>>20, time.Since(start))
	}
}

// content: at and over the item bound; the over case is clamped with a
// warning, never dropped silently and never unbounded.
func TestContentItemBound(t *testing.T) {
	mk := func(n int) string { return strings.Repeat(`"a" `, n) }
	x := resolve(t, `<p>x</p>`, `p::before{content:`+mk(maxContentItems)+`}`, "")
	c := pseudoOf(t, x, "p", css.PseudoBefore)
	if c == nil || len(c.Content) != maxContentItems || x.has("clamped") {
		t.Errorf("at the bound: %v items, warnings %v", func() int {
			if c == nil {
				return -1
			}
			return len(c.Content)
		}(), x.warnings())
	}
	x = resolve(t, `<p>x</p>`, `p::before{content:`+mk(maxContentItems+1)+`}`, "")
	c = pseudoOf(t, x, "p", css.PseudoBefore)
	if c == nil || len(c.Content) != maxContentItems || !x.has("clamped") {
		t.Errorf("over the bound: box %v, warnings %v", c != nil, x.warnings())
	}
	x = resolve(t, `<p>x</p>`, `p::before{content:`+mk(maxContentItems*1000)+`}`, "")
	if c = pseudoOf(t, x, "p", css.PseudoBefore); c != nil && len(c.Content) > maxContentItems {
		t.Errorf("%d items", len(c.Content))
	}
}

// A hostile var() chain into the M3 properties cannot exceed the var budget
// nor produce more than the bounds above.
func TestVarBombIntoM3Properties(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`:root{--a:"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx";`)
	prev := "--a"
	for i := range 30 {
		name := fmt.Sprintf("--v%d", i)
		fmt.Fprintf(&sb, "%s:var(%s) var(%s) var(%s) var(%s);", name, prev, prev, prev, prev)
		prev = name
	}
	sb.WriteString(`} p::before{content:var(` + prev + `)} p{counter-reset:var(` + prev + `);transform:rotate(var(` + prev + `));page:var(` + prev + `)}`)
	b := newBudget()
	d, err := dom.Parse(strings.NewReader(`<p>x</p>`), b)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(ua(), nil, []*Sheet{compileSheet(t, sb.String(), css.OriginDocument, b)}, printEnv, b)
	var res error
	got := allocBytes(func() {
		for i := range d.Elements {
			var parent *Computed
			if p := d.Elements[i].Parent; p >= 0 {
				_ = p
			}
			c, err := r.Element(d, int32(i), parent)
			if err != nil {
				res = err
				return
			}
			if _, err := r.Pseudo(d, int32(i), css.PseudoBefore, c); err != nil {
				res = err
				return
			}
		}
	})
	if res != nil {
		var le *budget.LimitError
		if !asLimit(res, &le) {
			t.Fatalf("unexpected error %v", res)
		}
	}
	if got > 128<<20 {
		t.Errorf("%d MiB for the var bomb", got>>20)
	}
}

func asLimit(err error, target **budget.LimitError) bool { return errors.As(err, target) }
