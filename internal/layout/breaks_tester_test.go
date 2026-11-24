package layout

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/box"
	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// xorshift is a fixed generator: the documents are the same on every run.
type xorshift struct{ s uint64 }

func (x *xorshift) n(n int) int {
	x.s ^= x.s << 13
	x.s ^= x.s >> 7
	x.s ^= x.s << 17
	return int(x.s>>33&0x7fffffff) % n
}

const pageH = 600.0

// bnode is a generated block: a leaf of text lines or a div of children.
type bnode struct {
	id            string
	lines, lh     int
	kids          []*bnode
	before, after string
	inside        string
	parent        *bnode
	idx           int
	// wrap is "td" or "ib" for a div laid out as a table cell or as an
	// inline-block instead of a block container.
	wrap string
}

func (n *bnode) height() float64 {
	if n.kids == nil {
		return float64(n.lines * n.lh)
	}
	h := 0.0
	for _, k := range n.kids {
		h += k.height()
	}
	return h
}

func (n *bnode) leaves() []*bnode {
	if n.kids == nil {
		return []*bnode{n}
	}
	var out []*bnode
	for _, k := range n.kids {
		out = append(out, k.leaves()...)
	}
	return out
}

func (n *bnode) hasForcedInside() bool {
	for i, k := range n.kids {
		if (i > 0 && k.before == "page") || (i < len(n.kids)-1 && k.after == "page") || k.hasForcedInside() {
			return true
		}
	}
	return false
}

func (n *bnode) style() string { return n.styleWith("") }

func (n *bnode) styleWith(extra string) string {
	var parts []string
	if extra != "" {
		parts = append(parts, extra)
	}
	if n.before != "" {
		parts = append(parts, "break-before:"+n.before)
	}
	if n.after != "" {
		parts = append(parts, "break-after:"+n.after)
	}
	if n.inside != "" {
		parts = append(parts, "break-inside:"+n.inside)
	}
	if len(parts) == 0 {
		return ""
	}
	return ` style="` + strings.Join(parts, ";") + `"`
}

func (n *bnode) html(sb *strings.Builder) {
	if n.kids == nil {
		class := "b"
		if n.lh == 200 {
			class = "r"
		}
		fmt.Fprintf(sb, `<div class=%s id=%s%s>%s</div>`, class, n.id, n.style(), strings.Repeat("x<br>", n.lines-1)+"x")
		return
	}
	open, closing := `<div`+n.style()+`>`, `</div>`
	switch n.wrap {
	case "td":
		open = `<table` + n.styleWith("border-spacing:0;border-collapse:separate") + `><tr><td style="padding:0">`
		closing = `</td></tr></table>`
	case "ib":
		open = `<div` + n.style() + `><span style="display:inline-block">`
		closing = `</span></div>`
	}
	sb.WriteString(open)
	for _, k := range n.kids {
		k.html(sb)
	}
	sb.WriteString(closing)
}

type bgen struct {
	rng  *xorshift
	next int
	// wraps lets a div be generated as a table cell or an inline-block.
	wraps bool
}

func (g *bgen) pick(opts ...string) string { return opts[g.rng.n(len(opts))] }

func (g *bgen) node(depth int) *bnode {
	if depth == 0 || g.rng.n(3) == 0 {
		n := &bnode{id: fmt.Sprintf("L%d", g.next), lines: 1 + g.rng.n(3), lh: 100}
		if g.rng.n(12) == 0 {
			n.lh, n.lines = 200, 3+g.rng.n(2)
		}
		g.next++
		return n
	}
	n := &bnode{}
	if g.wraps && g.rng.n(2) == 0 {
		n.wrap = g.pick("td", "ib")
	}
	for range 1 + g.rng.n(4) {
		n.kids = append(n.kids, g.node(depth-1))
	}
	return n
}

func (g *bgen) decorate(n *bnode, forced bool) {
	for i, k := range n.kids {
		k.parent, k.idx = n, i
		if g.rng.n(4) == 0 {
			k.after = g.pick("avoid", "avoid", "page")
			if k.after == "page" && (!forced || i == len(n.kids)-1) {
				k.after = "avoid"
			}
		}
		if g.rng.n(4) == 0 {
			k.before = g.pick("avoid", "avoid", "page")
			if k.before == "page" && (!forced || i == 0) {
				k.before = "avoid"
			}
		}
		if k.kids != nil && g.rng.n(3) == 0 {
			k.inside = "avoid"
		}
		g.decorate(k, forced)
	}
}

// pos is a place in the paginated output: page and y of a line.
type pos struct {
	page int
	y    float64
}

func (p pos) before(q pos) bool { return p.page < q.page || (p.page == q.page && p.y < q.y-0.01) }

// leafLines are the line positions of every leaf id on the pages.
func leafLines(pages []*Page) map[string][]pos {
	out := map[string][]pos{}
	for i, pg := range pages {
		walk(pg.root, func(f *frag) {
			for _, ln := range f.lines {
				out[f.box.ID] = append(out[f.box.ID], pos{i, ln.baseline})
			}
		})
	}
	return out
}

func firstPos(lines map[string][]pos, n *bnode) pos {
	l := n.leaves()
	return lines[l[0].id][0]
}

func lastPos(lines map[string][]pos, n *bnode) pos {
	l := n.leaves()
	ps := lines[l[len(l)-1].id]
	return ps[len(ps)-1]
}

// checkBreaks verifies the invariants of forced and avoided breaks on a
// generated document and returns a description of the first violation.
func checkBreaks(root *bnode, pages []*Page) string {
	lines := leafLines(pages)
	leaves := root.leaves()
	var prev pos
	for k, lf := range leaves {
		ps := lines[lf.id]
		if len(ps) != lf.lines {
			return fmt.Sprintf("#%s has %d lines on the pages, want %d", lf.id, len(ps), lf.lines)
		}
		for _, p := range ps {
			if p.y < -0.01 || p.y > pageH+0.01 {
				return fmt.Sprintf("#%s line with its baseline at y=%v is outside the page area", lf.id, p.y)
			}
			if k > 0 && p.before(prev) {
				return fmt.Sprintf("#%s line at page %d y=%v comes before the previous line (page %d y=%v)", lf.id, p.page, p.y, prev.page, prev.y)
			}
			prev = p
		}
	}
	var walkNodes func(n *bnode) string
	walkNodes = func(n *bnode) string {
		if n.wrap != "" {
			return ""
		}
		for i, k := range n.kids {
			if i+1 < len(n.kids) {
				next := n.kids[i+1]
				if k.after == "page" || next.before == "page" {
					if lastPos(lines, k).page >= firstPos(lines, next).page {
						return fmt.Sprintf("forced break between siblings %d and %d of a div not honoured (pages %d and %d)", i, i+1, lastPos(lines, k).page, firstPos(lines, next).page)
					}
				} else if (k.after == "avoid" || next.before == "avoid") && lastPos(lines, k).page < firstPos(lines, next).page {
					start := i
					for start > 0 {
						p, q := n.kids[start-1], n.kids[start]
						if p.after == "page" || q.before == "page" || (p.after != "avoid" && q.before != "avoid") {
							break
						}
						start--
					}
					fp, page := firstPos(lines, n.kids[start]), lastPos(lines, k).page
					for _, lf := range leaves {
						if lf == n.kids[start].leaves()[0] {
							break
						}
						if ps := lines[lf.id]; ps[len(ps)-1].page == page && !fp.before(ps[len(ps)-1]) {
							return fmt.Sprintf("break avoided between siblings %d and %d is taken, though the chain starting at %d is not at the top of page %d", i, i+1, start, page)
						}
					}
				}
			}
			if k.kids != nil && k.wrap == "" && k.inside == "avoid" && k.height() <= pageH && !k.hasForcedInside() {
				if firstPos(lines, k).page != lastPos(lines, k).page {
					return fmt.Sprintf("break-inside:avoid div of %vpx split over pages %d-%d", k.height(), firstPos(lines, k).page, lastPos(lines, k).page)
				}
			}
			if msg := walkNodes(k); msg != "" {
				return msg
			}
		}
		return ""
	}
	return walkNodes(root)
}

func TestGeneratedBreaks(t *testing.T) {
	for _, forced := range []bool{false, true} {
		g := &bgen{rng: &xorshift{s: 12345}}
		for i := range 400 {
			root := &bnode{}
			for range 2 + g.rng.n(5) {
				root.kids = append(root.kids, g.node(3))
			}
			g.decorate(root, forced)
			var sb strings.Builder
			root.html(&sb)
			html := sb.String()
			pages := breakRun(t, html)
			if len(pages) > 100 {
				t.Fatalf("doc %d: %d pages\n%s", i, len(pages), html)
			}
			if msg := checkBreaks(root, pages); msg != "" {
				t.Errorf("forced=%v doc %d: %s\n%s", forced, i, msg, html)
				return
			}
		}
	}
}

// 10 000 headings chained to one tall block render in time and memory.
func TestAvoidChainResources(t *testing.T) {
	html := strings.Repeat(`<h2 class=s style="break-after:avoid">h</h2>`, 10000) + `<div class=t>t</div>`
	start := time.Now()
	r := newRun(t, html, opts{extra: smallPage, css: breaksCSS})
	pages := r.all(t)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
	if len(pages) < 1 {
		t.Fatal("no page")
	}
	if got := len(leafLinesByTag(pages, "h2")); got != 10000 {
		t.Errorf("%d headings laid out, want 10000", got)
	}
}

func leafLinesByTag(pages []*Page, tag string) []pos {
	var out []pos
	for i, pg := range pages {
		walk(pg.root, func(f *frag) {
			if f.box.Tag.String() == tag {
				for _, ln := range f.lines {
					out = append(out, pos{i, ln.rect.Y})
				}
			}
		})
	}
	return out
}

// Table rows with break-inside: avoid stay whole unless taller than a page,
// inside giant tables and inside avoid boxes measured on a scratch page, and
// no cell line is lost or doubled.
func TestRowAvoidInTables(t *testing.T) {
	tables := 0
	table := func(rows int, rowAttr string) string {
		tables++
		var sb strings.Builder
		sb.WriteString(`<table style="border-spacing:0;border-collapse:separate">`)
		for i := range rows {
			fmt.Fprintf(&sb, `<tr %s><td class=w id=c%d_%d>a<br>b<br>c</td></tr>`, rowAttr, tables, i)
		}
		sb.WriteString(`</table>`)
		return sb.String()
	}
	for _, tc := range []struct{ name, html string }{
		{"giant", table(300, `style="break-inside:avoid"`)},
		{"after content", blocks(3, "p") + table(40, `style="break-inside:avoid"`)},
		{"first row does not fit on a started page", blocks(4, "p") + table(3, `style="break-inside:avoid"`)},
		{"in an avoid box that fits", blocks(4, "p") + `<div style="break-inside:avoid">` + table(2, `style="break-inside:avoid"`) + `</div>`},
		{"in an avoid box that does not", blocks(3, "p") + `<div style="break-inside:avoid">` + table(20, `style="break-inside:avoid"`) + `</div>` + blocks(2, "q")},
		{"heading chained to a table", blocks(5, "p") + `<h2 class=b style="break-after:avoid">h</h2>` + table(10, `style="break-inside:avoid"`)},
		{"forced inside a table box", table(4, `style="break-inside:avoid"`) + `<div class=b style="break-before:page">z</div>` + table(4, `style="break-inside:avoid"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := breakRun(t, tc.html)
			rows := strings.Count(tc.html, "<tr ")
			seen := map[string]int{}
			for _, f := range frags(pages, "td") {
				seen[f.box.ID] += len(f.lines)
			}
			if got := len(seen); got != rows {
				t.Errorf("%d cells on the pages, want %d", got, rows)
			}
			for id, n := range seen {
				if n != 3 {
					t.Errorf("#%s has %d lines, want 3", id, n)
				}
			}
			for _, f := range frags(pages, "tr") {
				if !f.first || !f.last {
					t.Errorf("a row is split (first=%v last=%v)", f.first, f.last)
				}
			}
		})
	}
}

// A measurement made while a table is cut must not leave state behind: the
// same document laid out twice gives the same pages, and a document with the
// avoid property on every block still paginates all of its lines.
func TestMeasureLeavesNoState(t *testing.T) {
	html := blocks(3, "p") + `<div style="break-inside:avoid">` + blocks(2, "m") +
		`<table><tr><td class=w id=t1>a<br>b<br>c</td><td class=w>a<br>b<br>c<br>d</td></tr></table>` + blocks(1, "n") + `</div>` + blocks(4, "z")
	a := breakRun(t, html)
	b := breakRun(t, html)
	if len(a) != len(b) {
		t.Fatalf("%d and %d pages", len(a), len(b))
	}
	for i := range a {
		if len(linesOf(a[i])) != len(linesOf(b[i])) {
			t.Errorf("page %d: %d and %d lines", i, len(linesOf(a[i])), len(linesOf(b[i])))
		}
	}
	// Every line of the document is on some page exactly once.
	want := 3 + 2 + (3 + 4) + 1 + 4 // blocks, the two cells of the table, tail
	got := 0
	for _, pg := range a {
		got += len(linesOf(pg))
	}
	if got != want {
		t.Errorf("%d lines on the pages, want %d", got, want)
	}
}

// Forced and avoided breaks next to each other never hang and always
// terminate within the page limit.
func TestForcedAvoidCombos(t *testing.T) {
	props := []string{
		"", "break-before:page", "break-after:page", "break-before:avoid", "break-after:avoid",
		"break-before:page;break-after:avoid", "break-before:avoid;break-after:page", "break-inside:avoid", "break-inside:avoid;break-after:page",
	}
	limit := func(l *budget.Limits) { l[budget.Pages] = 300 }
	for i := range props {
		for j := range props {
			for k := range props {
				html := `<div class=b id=a style="` + props[i] + `">a</div><div style="` + props[j] + `"><div class=b id=b>b</div><div class=b id=c style="` + props[k] + `">c</div></div><div class=b id=d>d</div>`
				r := newRun(t, html, opts{extra: smallPage, css: breaksCSS, limits: limit})
				pages := r.all(t)
				got := 0
				for _, pg := range pages {
					got += len(linesOf(pg))
				}
				if got != 4 {
					t.Fatalf("%q %q %q: %d lines, want 4", props[i], props[j], props[k], got)
				}
			}
		}
	}
}

// In the large reports, a break-inside: avoid box is split only when it is
// taller than a page (the callouts, kpis and rows of 01; the cards of 02).
func TestTemplatesAvoidBoxesStayWhole(t *testing.T) {
	for _, tpl := range []string{"01-modern", "02-executive"} {
		t.Run(tpl, func(t *testing.T) {
			html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), tpl, testcorpus.Large)
			if err != nil {
				t.Fatal(err)
			}
			pages := newRun(t, string(html), opts{extra: testcorpus.ChromePaperCSS, scale: 0.9}).all(t)
			type total struct {
				h     float64
				split bool
				tag   string
			}
			boxes := map[*box.Box]*total{}
			areaH := pages[0].Area.H
			for _, pg := range pages {
				walk(pg.root, func(f *frag) {
					if f.box.Style.BreakInside != style.BreakAvoid {
						return
					}
					tt := boxes[f.box]
					if tt == nil {
						tt = &total{tag: f.box.Tag.String()}
						boxes[f.box] = tt
					}
					tt.h += f.rect.H
					tt.split = tt.split || !f.first || !f.last
				})
			}
			avoided, split := 0, 0
			for _, tt := range boxes {
				avoided++
				if tt.split {
					split++
					if tt.h <= areaH {
						t.Errorf("a %s of %.1fpx (page area %.1fpx) is split", tt.tag, tt.h, areaH)
					}
				}
			}
			t.Logf("%d break-inside: avoid boxes, %d split (taller than a page)", avoided, split)
			if avoided == 0 {
				t.Error("no break-inside: avoid box found")
			}
		})
	}
}

func BenchmarkAvoidChain(b *testing.B) {
	html := strings.Repeat(`<h2 class=s style="break-after:avoid">h</h2>`, 10000) + `<div class=t>t</div>`
	b.ReportAllocs()
	for b.Loop() {
		newRun(b, html, opts{extra: smallPage, css: breaksCSS}).all(b)
	}
}

// A break-inside: avoid row taller than a page, in a table that starts in the
// middle of a page, is cut where it is: it is not moved to the next page first.
func TestTallAvoidRowIsCutInPlace(t *testing.T) {
	cell := strings.Repeat("x<br>", 8) + "x" // 9 lines of 100px: 1.5 pages
	doc := func(attr string) string {
		return blocks(2, "p") + `<table style="border-spacing:0;border-collapse:separate"><tr ` + attr + `><td class=b id=tall>` + cell + `</td></tr></table>`
	}
	shape := func(pages []*Page) (out []string, lines int) {
		for i, pg := range pages {
			walk(pg.root, func(f *frag) {
				if f.box.Tag.String() == "tr" {
					out = append(out, fmt.Sprintf("page %d y=%.1f h=%.1f first=%v last=%v", i, f.rect.Y, f.rect.H, f.first, f.last))
				}
				if f.box.Tag.String() == "td" {
					lines += len(f.lines)
				}
			})
		}
		return out, lines
	}
	pages := breakRun(t, doc(`style="break-inside:avoid"`))
	got, lines := shape(pages)
	want, _ := shape(breakRun(t, doc("")))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("with avoid:\n%s\nwithout:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if pg, y := pageOf(t, pages, "tall"); pg != 0 || y != 200 {
		t.Errorf("row starts on page %d at y=%v, want page 0 at 200", pg, y)
	}
	if lines != 9 {
		t.Errorf("%d cell lines, want 9", lines)
	}
}

// Known defect of the row cut, present before the break properties: a row
// taller than a page whose last full page leaves no room for the cell padding
// ends with a fragment of the padding alone (1px) on one more page. It does not
// depend on break-inside. This pins the behaviour; when the cut is fixed, the
// 1px fragment goes away and this test must be updated to expect two fragments.
func TestRowCutPaddingSliverIsKnown(t *testing.T) {
	cell := strings.Repeat("x<br>", 8) + "x"
	row := func(td string) []*frag {
		html := `<div class=b>a</div><div class=b>b</div><table style="border-spacing:0;border-collapse:separate"><tr><td ` + td + ` class=b>` + cell + `</td></tr></table>`
		return frags(breakRun(t, html), "tr")
	}
	if got := row(""); len(got) != 3 || got[2].rect.H != 1 {
		t.Errorf("%d row fragments, want 3 with the last 1px tall: the defect is gone, update this test", len(got))
	}
	if got := row(`style="padding:0"`); len(got) != 2 {
		t.Errorf("without padding: %d row fragments, want 2", len(got))
	}
}

// Break properties inside a table cell or an inline-block never drop text:
// those boxes are laid out without the sibling breaks of block containers, so
// every line must still be on some page.
func TestBreaksInsideCellsAndAtomsKeepText(t *testing.T) {
	leaf := func(id, attr string) string {
		return `<div class=b id=` + id + ` ` + attr + `>` + id + `</div>`
	}
	forcedBefore := `style="break-before:page"`
	forcedAfter := `style="break-after:page"`
	avoidAfter := `style="break-after:avoid"`
	three := func(a, b, c string) string { return leaf("AAA", a) + leaf("BBB", b) + leaf("CCC", c) }
	for _, tc := range []struct {
		name  string
		html  string
		lines int
	}{
		{"td break-before", `<table><tr><td>` + three("", forcedBefore, "") + `</td></tr></table>`, 3},
		{"td break-after", `<table><tr><td>` + three("", forcedAfter, "") + `</td></tr></table>`, 3},
		{"td both", `<table><tr><td>` + three(forcedAfter, forcedBefore, forcedBefore) + `</td></tr></table>`, 3},
		{"td after content", blocks(4, "p") + `<table><tr><td>` + three("", forcedBefore, "") + `</td></tr></table>`, 7},
		{"td avoid chain", blocks(4, "p") + `<table><tr><td>` + three(avoidAfter, avoidAfter, "") + `</td></tr></table>`, 7},
		{"td break-inside avoid", blocks(4, "p") + `<table><tr><td><div style="break-inside:avoid">` + three("", "", "") + `</div></td></tr></table>`, 7},
		{"td tr avoid with forced inside", `<table><tr style="break-inside:avoid"><td>` + three("", forcedBefore, "") + `</td><td>` + leaf("DDD", "") + `</td></tr></table>`, 4},
		{"two cells, forced in one", `<table><tr><td>` + three("", forcedBefore, "") + `</td><td>` + three("", "", "") + `</td></tr></table>`, 6},
		{"nested table in cell", `<table><tr><td><table><tr><td>` + three("", forcedBefore, "") + `</td></tr></table></td></tr></table>`, 3},
		{"inline-block break-before", `<div><span style="display:inline-block">` + three("", forcedBefore, "") + `</span></div>`, 3},
		{"inline-block break-after", `<div>x<span style="display:inline-block">` + three("", forcedAfter, "") + `</span>y</div>`, 3},
		{"inline-block avoid", blocks(4, "p") + `<div><span style="display:inline-block;break-inside:avoid">` + three(avoidAfter, "", "") + `</span></div>`, 7},
		{"inline-block in a cell", `<table><tr><td><span style="display:inline-block">` + three("", forcedBefore, "") + `</span></td></tr></table>`, 3},
		{"div in an inline-block in a cell, forced", `<table><tr><td><span style="display:inline-block"><div>` + three("", forcedBefore, "") + `</div></span></td></tr></table>`, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := breakRun(t, tc.html)
			got := 0
			for _, pg := range pages {
				got += len(linesOf(pg))
			}
			// Texts of x and y around an inline-block count as lines of their own paragraph.
			extra := strings.Count(tc.html, "<div>x<span")
			if got < tc.lines+extra {
				t.Errorf("%d lines on %d pages, want at least %d", got, len(pages), tc.lines+extra)
			}
			for _, id := range []string{"AAA", "BBB", "CCC", "DDD"} {
				if strings.Contains(tc.html, "id="+id) {
					found := false
					for _, pg := range pages {
						walk(pg.root, func(f *frag) {
							if f.box.ID == id && len(f.lines) > 0 {
								found = true
							}
						})
					}
					if !found {
						t.Errorf("#%s is missing from the pages", id)
					}
				}
			}
		})
	}
}

// The text of documents that mix block containers, table cells and
// inline-blocks with forced and avoided breaks is on the pages exactly once
// and in order (the break rules themselves are only checked between block
// siblings).
func TestGeneratedBreaksInCellsAndAtoms(t *testing.T) {
	g := &bgen{rng: &xorshift{s: 987654321}, wraps: true}
	for i := range 400 {
		root := &bnode{}
		for range 2 + g.rng.n(4) {
			root.kids = append(root.kids, g.node(3))
		}
		g.decorate(root, true)
		var sb strings.Builder
		root.html(&sb)
		pages := breakRun(t, sb.String())
		if msg := checkBreaks(root, pages); msg != "" {
			t.Errorf("doc %d: %s\n%s", i, msg, sb.String())
			return
		}
	}
}

// An inline-block that fits a page is not split over two, and one that comes
// after content moves whole.
func TestInlineBlockStaysWhole(t *testing.T) {
	ib := `<div><span style="display:inline-block">` + blocks(3, "m") + `</span></div>`
	pages := breakRun(t, blocks(4, "p")+ib)
	first, _ := pageOf(t, pages, "m0")
	last, _ := pageOf(t, pages, "m2")
	if first != last {
		t.Errorf("a 300px inline-block is split over pages %d and %d", first, last)
	}
}
