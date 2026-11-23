package css

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/dom"
)

// A selector built as a structure, rendered to text for the parser, and
// evaluated by a naive recursive definition that shares no code with the
// matcher: the oracle of the differential tests.

type oSimple struct {
	kind string // "class", "attr", "first", "last", "nth", "root", "hover"
	name string
	op   string
	val  string
	a, b int
}

type oCompound struct {
	comb    string // combinator to the previous compound: "", " ", ">", "+", "~"
	tag     string
	id      string
	simples []oSimple
	pseudo  string // "::before", "::after" or ""
}

type oSelector []oCompound

func (s oSelector) String() string {
	var sb strings.Builder
	for i, c := range s {
		if i > 0 {
			if c.comb == " " {
				sb.WriteByte(' ')
			} else {
				sb.WriteString(" " + c.comb + " ")
			}
		}
		if c.tag != "" {
			sb.WriteString(c.tag)
		}
		if c.id != "" {
			sb.WriteString("#" + c.id)
		}
		for _, p := range c.simples {
			switch p.kind {
			case "class":
				sb.WriteString("." + p.name)
			case "attr":
				if p.op == "" {
					sb.WriteString("[" + p.name + "]")
				} else {
					op := p.op
					if op == "=" {
						op = ""
					}
					fmt.Fprintf(&sb, "[%s%s=%q]", p.name, op, p.val)
				}
			case "first":
				sb.WriteString(":first-child")
			case "last":
				sb.WriteString(":last-child")
			case "nth":
				fmt.Fprintf(&sb, ":nth-child(%dn%+d)", p.a, p.b)
			case "root":
				sb.WriteString(":root")
			case "hover":
				sb.WriteString(":hover")
			}
		}
		if c.tag == "" && c.id == "" && len(c.simples) == 0 {
			sb.WriteString("*")
		}
		sb.WriteString(c.pseudo)
	}
	return sb.String()
}

// rng is a deterministic generator: failures must be reproducible.
type rng uint64

func newRng(a, b uint64) *rng {
	r := rng((a+1)*0x9E3779B97F4A7C15 ^ b*0xBF58476D1CE4E5B9 | 1)
	return &r
}

func (r *rng) IntN(n int) int {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return int(min((uint64(*r)>>11)%clampU(n), math.MaxInt32))
}

// tree is the oracle's own view of the document.
type tree struct {
	d        *dom.Document
	children map[int32][]int32
	index    []int // 0-based position among element siblings
}

func newTree(d *dom.Document) *tree {
	t := &tree{d: d, children: map[int32][]int32{}, index: make([]int, len(d.Elements))}
	for i := range d.Elements {
		p := d.Elements[i].Parent
		t.index[i] = len(t.children[p])
		t.children[p] = append(t.children[p], int32(i))
	}
	return t
}

func (t *tree) prevSiblings(e int32) []int32 {
	sibs := t.children[t.d.Elements[e].Parent]
	return sibs[:t.index[e]]
}

func (t *tree) compoundMatches(c oCompound, e int32) bool {
	el := &t.d.Elements[e]
	name := el.Name
	if el.Tag != 0 {
		name = el.Tag.String()
	}
	if c.tag != "" && !strings.EqualFold(c.tag, name) {
		return false
	}
	if c.id != "" && el.ID != c.id {
		return false
	}
	pos := t.index[e] + 1
	n := len(t.children[el.Parent])
	for _, p := range c.simples {
		switch p.kind {
		case "class":
			if !slices.Contains(el.Classes, p.name) {
				return false
			}
		case "attr":
			v, ok := el.Attr(p.name)
			if !ok || !attrOK(p, v) {
				return false
			}
		case "first":
			if pos != 1 {
				return false
			}
		case "last":
			if pos != n {
				return false
			}
		case "nth":
			found := false
			for k := 0; k <= pos+1; k++ {
				if p.a*k+p.b == pos {
					found = true
				}
			}
			if !found {
				return false
			}
		case "root":
			if el.Parent >= 0 {
				return false
			}
		case "hover":
			return false
		}
	}
	return true
}

func attrOK(p oSimple, v string) bool {
	switch p.op {
	case "":
		return true
	case "=":
		return v == p.val
	case "~":
		return p.val != "" && slices.Contains(strings.Fields(v), p.val)
	case "|":
		return v == p.val || strings.HasPrefix(v, p.val+"-")
	case "^":
		return p.val != "" && strings.HasPrefix(v, p.val)
	case "$":
		return p.val != "" && strings.HasSuffix(v, p.val)
	case "*":
		return p.val != "" && strings.Contains(v, p.val)
	}
	return false
}

func (t *tree) match(s oSelector, i int, e int32) bool {
	if !t.compoundMatches(s[i], e) {
		return false
	}
	if i == 0 {
		return true
	}
	el := &t.d.Elements[e]
	switch s[i].comb {
	case ">":
		return el.Parent >= 0 && t.match(s, i-1, el.Parent)
	case " ":
		for x := el.Parent; x >= 0; x = t.d.Elements[x].Parent {
			if t.match(s, i-1, x) {
				return true
			}
		}
	case "+":
		if sibs := t.prevSiblings(e); len(sibs) > 0 {
			return t.match(s, i-1, sibs[len(sibs)-1])
		}
	case "~":
		for _, x := range t.prevSiblings(e) {
			if t.match(s, i-1, x) {
				return true
			}
		}
	}
	return false
}

func (s oSelector) specificity() (ids, classes, types int) {
	for _, c := range s {
		if c.id != "" {
			ids++
		}
		if c.tag != "" {
			types++
		}
		classes += len(c.simples)
		if c.pseudo != "" {
			types++
		}
	}
	return
}

var (
	oTags    = []string{"div", "p", "span", "b", "i", "section", "ul", "li"}
	oClasses = []string{"a", "b", "c", "d"}
	oIDs     = []string{"x1", "x2", "x3"}
	oVals    = []string{"v", "vw", "w-v", "en", "en-us", "hello world", "k"}
	oAttrs   = []string{"data-k", "lang", "title"}
)

func randHTML(r *rng, maxEl int) string {
	var sb strings.Builder
	n := 0
	var gen func(depth int)
	gen = func(depth int) {
		for k := r.IntN(4); k > 0 && n < maxEl; k-- {
			n++
			tag := oTags[r.IntN(len(oTags))]
			sb.WriteString("<" + tag)
			if r.IntN(4) == 0 {
				sb.WriteString(" id=" + oIDs[r.IntN(len(oIDs))])
			}
			if r.IntN(2) == 0 {
				var cl []string
				for range 1 + r.IntN(3) {
					cl = append(cl, oClasses[r.IntN(len(oClasses))])
				}
				sb.WriteString(` class="` + strings.Join(cl, " ") + `"`)
			}
			for _, a := range oAttrs {
				if r.IntN(4) == 0 {
					fmt.Fprintf(&sb, ` %s="%s"`, a, oVals[r.IntN(len(oVals))])
				}
			}
			sb.WriteString(">")
			if depth < 6 {
				gen(depth + 1)
			}
			sb.WriteString("</" + tag + ">")
		}
	}
	sb.WriteString("<!doctype html><html><body>")
	gen(0)
	sb.WriteString("</body></html>")
	return sb.String()
}

func randCompound(r *rng) oCompound {
	var c oCompound
	if r.IntN(2) == 0 {
		c.tag = oTags[r.IntN(len(oTags))]
	}
	if r.IntN(8) == 0 {
		c.id = oIDs[r.IntN(len(oIDs))]
	}
	for range r.IntN(3) {
		switch r.IntN(9) {
		case 0, 1, 2:
			c.simples = append(c.simples, oSimple{kind: "class", name: oClasses[r.IntN(len(oClasses))]})
		case 3, 4:
			ops := []string{"", "", "=", "~", "|", "^", "$", "*"}
			c.simples = append(c.simples, oSimple{kind: "attr", name: oAttrs[r.IntN(len(oAttrs))], op: ops[r.IntN(len(ops))], val: oVals[r.IntN(len(oVals))]})
		case 5:
			c.simples = append(c.simples, oSimple{kind: "first"})
		case 6:
			c.simples = append(c.simples, oSimple{kind: "last"})
		case 7:
			c.simples = append(c.simples, oSimple{kind: "nth", a: r.IntN(4) - 1, b: r.IntN(5) - 1})
		case 8:
			if r.IntN(3) == 0 {
				c.simples = append(c.simples, oSimple{kind: "root"})
			} else if r.IntN(6) == 0 {
				c.simples = append(c.simples, oSimple{kind: "hover"})
			}
		}
	}
	return c
}

func randSelector(r *rng) oSelector {
	s := oSelector{randCompound(r)}
	for range r.IntN(4) {
		c := randCompound(r)
		c.comb = []string{" ", ">", "+", "~"}[r.IntN(4)]
		s = append(s, c)
	}
	if r.IntN(10) == 0 {
		s[len(s)-1].pseudo = []string{"::before", "::after"}[r.IntN(2)]
	}
	return s
}

func oracleBudget() *budget.Budget {
	l := budget.Defaults()
	l[budget.Work] = 1 << 60
	l[budget.Selectors] = 1 << 30
	return budget.New(context.Background(), l, nil)
}

// TestSelectorOracle: parse, specificity and Match agree with the naive
// definition on random documents and random selectors; backtracking shapes
// (a > b c, a ~ b c, a b ~ c, descendant chains) come up all the time in
// small trees with few tags.
func TestSelectorOracle(t *testing.T) {
	rounds := 400
	if raceOn || testing.Short() {
		rounds = 60
	}
	checked, matched := 0, 0
	for round := range rounds {
		r := newRng(clampU(round), 77)
		d := parseHTML(t, randHTML(r, 40))
		tr := newTree(d)
		b := oracleBudget()
		for range 60 {
			os := randSelector(r)
			text := os.String()
			sels, _ := parseSel(t, text)
			if len(sels) != 1 {
				t.Fatalf("%q not parsed", text)
			}
			s := sels[0]
			ids, cls, tys := os.specificity()
			sp := s.Specificity()
			if sp.IDs() != ids || sp.Classes() != cls || sp.Types() != tys {
				t.Fatalf("%q: specificity %d,%d,%d want %d,%d,%d", text, sp.IDs(), sp.Classes(), sp.Types(), ids, cls, tys)
			}
			for e := range d.Elements {
				want := tr.match(os, len(os)-1, int32(e))
				got, err := s.Match(d, int32(e), b)
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("round %d: %q on element %d (%s): got %v want %v\n%s", round, text, e, d.Elements[e].Name, got, want, dumpTree(d))
				}
				checked++
				if want {
					matched++
				}
			}
		}
	}
	t.Logf("%d (selector, element) pairs, %d matches", checked, matched)
	if matched < checked/50 {
		t.Fatalf("generator too sparse: %d matches of %d", matched, checked)
	}
}

func dumpTree(d *dom.Document) string {
	var sb strings.Builder
	for i := range d.Elements {
		el := &d.Elements[i]
		fmt.Fprintf(&sb, "%d:%s parent=%d id=%q class=%v attrs=%v\n", i, el.Tag.String(), el.Parent, el.ID, el.Classes, el.Attrs)
	}
	return sb.String()
}

// TestIndexEqualsBruteForce: the elements a rule is selected for through the
// index are exactly those of a scan of all the rules, and a selector that
// holds an interaction pseudo-class is never filed nor matched.
func TestIndexEqualsBruteForce(t *testing.T) {
	rounds := 200
	if raceOn || testing.Short() {
		rounds = 30
	}
	for round := range rounds {
		r := newRng(clampU(round), 91)
		d := parseHTML(t, randHTML(r, 40))
		var ix Index[int]
		var all []*Selector
		var oracle []oSelector
		for i := 0; i < 80; i++ {
			os := randSelector(r)
			sels, _ := parseSel(t, os.String())
			all = append(all, sels[0])
			oracle = append(oracle, os)
			ix.Add(sels[0], i)
		}
		tr := newTree(d)
		b := oracleBudget()
		var buf []Entry[int]
		for e := range d.Elements {
			buf = mustCandidates(t, &ix, d, clampIdx(e), buf[:0], b)
			var viaIndex []int
			for _, c := range buf {
				ok, err := c.Selector.Match(d, int32(e), b)
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					viaIndex = append(viaIndex, c.Value)
				}
			}
			slices.Sort(viaIndex)
			var brute []int
			for i, s := range all {
				ok, err := s.Match(d, int32(e), b)
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					brute = append(brute, i)
				}
				if want := tr.match(oracle[i], len(oracle[i])-1, int32(e)); want != ok {
					t.Fatalf("%q on %d: matcher %v oracle %v", oracle[i], e, ok, want)
				}
			}
			if !slices.Equal(viaIndex, brute) {
				t.Fatalf("round %d element %d: index %v, brute force %v", round, e, viaIndex, brute)
			}
			seen := map[int]bool{}
			for _, c := range buf {
				if seen[c.Value] {
					t.Fatalf("rule %d is a candidate twice for element %d", c.Value, e)
				}
				seen[c.Value] = true
				if c.Selector.NeverMatches() {
					t.Fatalf("never-matching selector %q is in the index", c.Selector)
				}
			}
		}
		if ix.Len() > len(all) {
			t.Fatalf("index holds %d of %d selectors", ix.Len(), len(all))
		}
	}
}

// Hand-written backtracking shapes, each failing for the nearest candidate and
// succeeding for a farther one (or the opposite).
func TestBacktrackingShapes(t *testing.T) {
	doc := parseHTML(t, `<!doctype html><html><body>
<a id=i1><b id=i2><b id=i3><c id=i4></c></b></b></a>
<div id=i5><p id=i6></p><span id=i7></span><p id=i8></p><em id=i9><i id=i10></i></em></div>
<div class=q id=i11><div id=i12><div class=q id=i13><p id=i14></p></div></div></div>
<ul id=i15><li id=i16></li><li class=z id=i17></li><li id=i18></li><li class=z id=i19></li></ul></body></html>`)
	for _, tt := range []struct {
		sel  string
		want []int
	}{
		{"a > b c", []int{4}},
		{"a > b > c", []int{}},
		{"a b > c", []int{4}},
		{"a > b > b > c", []int{4}},
		{"b b c", []int{4}},
		{"p ~ span ~ p", []int{8}},
		{"p + span + p", []int{8}},
		{"p ~ span + p", []int{8}},
		{"span ~ p + i", []int{}},
		{"div > p ~ em i", []int{10}},
		{"div p ~ em > i", []int{10}},
		{".q > div > .q p", []int{14}},
		{".q .q p", []int{14}},
		{".q > .q p", []int{}},
		{".q div > p", []int{14}},
		{"div div > p", []int{14}},
		{"body > .q p", []int{14}},
		{".z ~ .z", []int{19}},
		{"li ~ .z ~ li", []int{18, 19}},
		{".z + li ~ .z", []int{19}},
		{"li:first-child ~ .z:last-child", []int{19}},
		{"ul > li:nth-child(2n) ~ li:nth-child(4)", []int{19}},
		{"ul li:nth-child(odd) + li.z", []int{17, 19}},
		{"* > * > c", []int{4}},
		{"* * * * * c", []int{4}},
	} {
		t.Run(tt.sel, func(t *testing.T) {
			got := matchingIDs(t, doc, tt.sel)
			if !slices.Equal(got, nonNil(tt.want)) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func nonNil(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}

func clampU(v int) uint64 { return uint64(max(v, 0)) }
