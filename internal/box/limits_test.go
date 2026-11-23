package box

import (
	"errors"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func countBoxes(b *Box) int {
	n := 1
	for _, c := range b.Children {
		n += countBoxes(c)
	}
	return n
}

func TestMaxBoxes(t *testing.T) {
	html := "<div><p>a <b>b</b></p>text<p>c</p></div>"
	b0 := testBudget(nil)
	d0 := parseDoc(t, html, b0)
	root, err := Build(d0, newStyler(t, d0, b0), b0)
	if err != nil {
		t.Fatal(err)
	}
	n := countBoxes(root)
	if used := int(b0.Used(budget.Boxes)); used != n {
		t.Fatalf("charged %d boxes for a tree of %d", used, n)
	}
	for _, tt := range []struct {
		name  string
		limit int
		ok    bool
	}{{"just under", n + 1, true}, {"at the limit", n, true}, {"over", n - 1, false}} {
		t.Run(tt.name, func(t *testing.T) {
			b := testBudget(func(l *budget.Limits) { l[budget.Boxes] = int64(tt.limit) })
			d := parseDoc(t, html, b)
			_, err := Build(d, newStyler(t, d, b), b)
			var le *budget.LimitError
			if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.Boxes)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestMaxBoxesHostile(t *testing.T) {
	const limit = 1000
	for name, html := range map[string]string{
		"many paragraphs": strings.Repeat("<p>x</p>", 5000),
		"many inlines":    "<p>" + strings.Repeat("<b>x</b>", 5000) + "</p>",
		"block in inline": strings.Repeat("<span>a<div>b</div>", 400),
		"nested divs":     strings.Repeat("<div>", 400) + "x" + strings.Repeat("</div>", 400),
	} {
		t.Run(name, func(t *testing.T) {
			b := testBudget(func(l *budget.Limits) { l[budget.Boxes] = limit })
			d := parseDoc(t, html, b)
			root, err := Build(d, newStyler(t, d, b), b)
			var le *budget.LimitError
			if err != nil && (!errors.As(err, &le) || le.Kind != budget.Boxes || root != nil) {
				t.Fatalf("err = %v", err)
			}
			if err == nil && countBoxes(root) > limit {
				t.Fatalf("%d boxes over a limit of %d", countBoxes(root), limit)
			}
		})
	}
}

func TestBuildCancellation(t *testing.T) {
	b := testBudget(nil)
	d := parseDoc(t, strings.Repeat("<p>x</p>", 20000), b)
	st := newStyler(t, d, b)
	ctxB := testBudgetCancelled()
	if _, err := Build(d, st, ctxB); err == nil {
		t.Fatal("a cancelled context was not seen")
	}
}

func TestBuildWithoutElements(t *testing.T) {
	if _, err := Build(&emptyDoc, nil, testBudget(nil)); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("err = %v", err)
	}
}
