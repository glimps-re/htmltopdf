package style

import (
	"context"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

func newBudget() *budget.Budget {
	return budget.New(context.Background(), budget.Defaults(), nil)
}

func compileSheet(t testing.TB, src string, origin css.Origin, b *budget.Budget) *Sheet {
	t.Helper()
	ss, err := css.ParseStylesheet([]byte(src), origin, b)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(ss, b)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var (
	uaSheet     *Sheet
	uaSheetOnce = func() *Sheet {
		b := newBudget()
		s, err := CompileUA(b)
		if err != nil {
			panic(err)
		}
		if w, _ := b.Warnings(); len(w) != 0 {
			panic("UA sheet warns")
		}
		return s
	}
)

func ua() *Sheet {
	if uaSheet == nil {
		uaSheet = uaSheetOnce()
	}
	return uaSheet
}

var printEnv = css.MediaEnv{Print: true, Width: 794, Height: 1123}

// doc is a document with the computed style of every element.
type doc struct {
	d      *dom.Document
	styles []*Computed
	b      *budget.Budget
	r      *Resolver
}

// resolve parses html and computes every style with the UA sheet, the document
// sheet docCSS and the ExtraCSS extraCSS (either may be empty).
func resolve(t testing.TB, html, docCSS, extraCSS string) *doc {
	t.Helper()
	b := newBudget()
	d, err := dom.Parse(strings.NewReader(html), b)
	if err != nil {
		t.Fatal(err)
	}
	var sheets []*Sheet
	if docCSS != "" {
		sheets = append(sheets, compileSheet(t, docCSS, css.OriginDocument, b))
	}
	var extra *Sheet
	if extraCSS != "" {
		extra = compileSheet(t, extraCSS, css.OriginExtra, b)
	}
	r := NewResolver(ua(), extra, sheets, printEnv, b)
	out := &doc{d: d, b: b, r: r, styles: make([]*Computed, len(d.Elements))}
	for i := range d.Elements {
		var parent *Computed
		if p := d.Elements[i].Parent; p >= 0 {
			parent = out.styles[p]
		}
		c, err := r.Element(d, int32(i), parent)
		if err != nil {
			t.Fatalf("element %d: %v", i, err)
		}
		out.styles[i] = c
	}
	return out
}

// by returns the computed style of the first element with the tag name or id.
func (x *doc) by(t testing.TB, sel string) *Computed {
	t.Helper()
	for i := range x.d.Elements {
		e := &x.d.Elements[i]
		if e.Tag.String() == sel || e.ID == strings.TrimPrefix(sel, "#") && strings.HasPrefix(sel, "#") {
			return x.styles[i]
		}
	}
	t.Fatalf("no element %q", sel)
	return nil
}

func (x *doc) warnings() map[string]bool {
	w, _ := x.b.Warnings()
	out := make(map[string]bool, len(w))
	for _, v := range w {
		out[v.Code+": "+v.Detail] = true
	}
	return out
}

// has reports whether a warning starts with prefix ("code: detail").
func (x *doc) has(prefix string) bool {
	for k := range x.warnings() {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}
