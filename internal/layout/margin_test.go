package layout

import (
	"math"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/display"
	"github.com/glimps-re/htmltopdf/internal/style"
)

const footerCSS = `@page {
	size: A4; margin: 1in 0.3in 1.3in 0.3in;
	@bottom-left { content: "TLP:AMBER"; background: #000; color: #FFC000; font-size: 10px; }
	@bottom-right { content: "Page " counter(page) " of " counter(pages); font-size: 10px; }
}`

func marginItems(t *testing.T, extra string, number, total int) (display.List, *run) {
	t.Helper()
	r := newRun(t, "<p>x</p>", opts{extra: extra})
	pg, err := r.pag.Next()
	if err != nil {
		t.Fatal(err)
	}
	pg.Number = number
	items, err := r.pag.MarginBoxes(pg, total)
	if err != nil {
		t.Fatal(err)
	}
	return items, r
}

func texts(l display.List) []*display.Text {
	var out []*display.Text
	for _, it := range l {
		if tx, ok := it.(*display.Text); ok {
			out = append(out, tx)
		}
	}
	return out
}

// boxTexts returns the text of each margin box: the pieces between a PushClip
// and its PopClip, joined.
func boxTexts(l display.List) []string {
	var out []string
	var sb strings.Builder
	for _, it := range l {
		switch v := it.(type) {
		case display.PushClip:
			sb.Reset()
		case *display.Text:
			sb.WriteString(v.Text)
		case display.PopClip:
			out = append(out, sb.String())
		}
	}
	return out
}

func TestFooterOfTheHostConnector(t *testing.T) {
	items, r := marginItems(t, footerCSS, 9, 10)
	ps := r.pag.cfg.Page
	got := boxTexts(items)
	if len(got) != 2 || got[0] != "TLP:AMBER" || got[1] != "Page 9 of 10" {
		t.Fatalf("texts = %q", got)
	}
	var fill display.FillRect
	for _, it := range items {
		if f, ok := it.(display.FillRect); ok {
			fill = f
		}
	}
	areaW := ps.Width - ps.Margin[style.Left] - ps.Margin[style.Right]
	if fill.Rect.X != ps.Margin[style.Left] || fill.Rect.Y != ps.Height-ps.Margin[style.Bottom] || fill.Rect.H != ps.Margin[style.Bottom] {
		t.Errorf("the black box does not fill its margin area: %+v", fill.Rect)
	}
	if fill.Rect.W <= areaW*0.3 || fill.Rect.W >= areaW*0.7 {
		t.Errorf("black width %.1f of %.1f: not shared in proportion to the contents", fill.Rect.W, areaW)
	}
	all := texts(items)
	tx := all[len(all)-1]
	if right := tx.X + tx.Width(); math.Abs(right-(ps.Width-ps.Margin[style.Right])) > 0.01 {
		t.Errorf("the page counter ends at %.2f, want the right edge %.2f", right, ps.Width-ps.Margin[style.Right])
	}
	if tx := all[0]; tx.Baseline < fill.Rect.Y || tx.Baseline > fill.Rect.Bottom() {
		t.Errorf("the text of the left box is outside its box: baseline %.1f", tx.Baseline)
	}
}

func TestCountersFollowThePage(t *testing.T) {
	for _, n := range []int{1, 12} {
		items, _ := marginItems(t, footerCSS, n, 12)
		want := "Page " + map[int]string{1: "1", 12: "12"}[n] + " of 12"
		if got := boxTexts(items); got[len(got)-1] != want {
			t.Errorf("page %d: %q, want %q", n, got[len(got)-1], want)
		}
	}
}

func TestNoMarginBoxNoItems(t *testing.T) {
	if items, _ := marginItems(t, "", 1, 1); items != nil {
		t.Errorf("%d items without margin boxes", len(items))
	}
}

func TestTopBoxesSitInTheTopMargin(t *testing.T) {
	items, r := marginItems(t, `@page { margin: 1in; @top-center { content: "Title" } }`, 1, 1)
	tx := texts(items)
	if len(tx) != 1 {
		t.Fatalf("%d texts", len(tx))
	}
	ps := r.pag.cfg.Page
	if tx[0].Baseline <= 0 || tx[0].Baseline > ps.Margin[style.Top] {
		t.Errorf("baseline %.1f outside the top margin %.1f", tx[0].Baseline, ps.Margin[style.Top])
	}
	mid := tx[0].X + tx[0].Width()/2
	if math.Abs(mid-ps.Width/2) > 0.01 {
		t.Errorf("center box text centered at %.2f, page center %.2f", mid, ps.Width/2)
	}
}

func baselines(items display.List) (clips int, lines map[float64]bool) {
	lines = map[float64]bool{}
	for _, it := range items {
		switch v := it.(type) {
		case display.PushClip:
			clips++
		case *display.Text:
			lines[v.Baseline] = true
		}
	}
	return clips, lines
}

func TestStaticMarginTextWrapsInItsBox(t *testing.T) {
	long := strings.Repeat("word ", 400)
	items, _ := marginItems(t, `@page { margin: 1in; @top-left { content: "`+long+`"; font-size: 8px } }`, 1, 1)
	if clips, lines := baselines(items); clips != 1 || len(lines) < 2 {
		t.Errorf("%d clips, %d lines: a static text wraps and is clipped to its area", clips, len(lines))
	}
}

func TestWrappedMarginTextFollowsTextAlign(t *testing.T) {
	first := func(align string) float64 {
		items, _ := marginItems(t, `@page { margin: 1in; @top-center { content: "`+strings.Repeat("ab cd ", 60)+`"; text-align: `+align+`; font-size: 8px } }`, 1, 1)
		return texts(items)[0].X
	}
	if l, c, r := first("left"), first("center"), first("right"); !(l < c && c < r) {
		t.Errorf("first line x: left %.1f, center %.1f, right %.1f", l, c, r)
	}
}

func TestMarginTextWithACounterIsOneClippedLineFromTheBoxStart(t *testing.T) {
	long := strings.Repeat("word ", 400)
	for _, align := range []string{"left", "center", "right"} {
		items, r := marginItems(t, `@page { margin: 1in; @top-left { content: "`+long+`" counter(page); text-align: `+align+`; font-size: 8px } }`, 1, 1)
		clips, lines := baselines(items)
		if clips != 1 || len(lines) != 1 {
			t.Errorf("%s: %d clips, %d lines: one clipped line expected", align, clips, len(lines))
		}
		if x := texts(items)[0].X; math.Abs(x-r.pag.cfg.Page.Margin[style.Left]) > 0.01 {
			t.Errorf("%s: overflowing text starts at x=%.2f, want the box start %.2f", align, x, r.pag.cfg.Page.Margin[style.Left])
		}
	}
}

func TestMarginTextFollowsTextAlignWhenItFits(t *testing.T) {
	x := func(align string) float64 {
		items, _ := marginItems(t, `@page { margin: 1in; @top-center { content: "ab cd"; text-align: `+align+`; font-size: 8px } }`, 1, 1)
		return texts(items)[0].X
	}
	left, center, right := x("left"), x("center"), x("right")
	if !(left < center && center < right) {
		t.Errorf("x left %.1f, center %.1f, right %.1f", left, center, right)
	}
}

func TestMarginBoxesAreShapedOncePerString(t *testing.T) {
	// Six 1 KiB boxes over 500 pages: the strings are shaped once, only the
	// counters again on every page, and MaxGlyphs is never reached.
	str := strings.Repeat("abcdefghi ", 100)
	var sb strings.Builder
	sb.WriteString("@page { margin: 1in;")
	for _, box := range []string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"} {
		sb.WriteString("@" + box + ` { content: "` + str + `" counter(page) " of " counter(pages); font-size: 8px }`)
	}
	sb.WriteString("}")
	r := newRun(t, "<p>x</p>", opts{extra: sb.String(), limits: func(l *budget.Limits) { l[budget.Glyphs] = 70_000 }})
	pg, err := r.pag.Next()
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 500; n++ {
		pg.Number = n
		if _, err := r.pag.MarginBoxes(pg, 500); err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
	}
}

func TestDistribute(t *testing.T) {
	mk := func(a, b, c float64) *[3]marginSlot {
		var s [3]marginSlot
		for i, w := range [3]float64{a, b, c} {
			if w > 0 {
				s[i] = marginSlot{box: &style.MarginBox{}, max: w}
			}
		}
		return &s
	}
	tests := []struct {
		name string
		s    *[3]marginSlot
		want [3]float64
		both bool
	}{
		{"left and right in proportion", mk(50, 0, 150), [3]float64{100, 0, 300}, false},
		{"center stays centered", mk(100, 50, 0), [3]float64{160, 80, 160}, false},
		{"one box takes the row", mk(0, 0, 10), [3]float64{0, 0, 400}, false},
		{"center alone", mk(0, 20, 0), [3]float64{0, 400, 0}, false},
		{"empty texts share equally", mk(0, 0, 0), [3]float64{200, 0, 200}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.both {
				tt.s[0].box, tt.s[2].box = &style.MarginBox{}, &style.MarginBox{}
			}
			got := distribute(tt.s, 400)
			for i := range got {
				if math.Abs(got[i]-tt.want[i]) > 1e-9 {
					t.Fatalf("widths = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
