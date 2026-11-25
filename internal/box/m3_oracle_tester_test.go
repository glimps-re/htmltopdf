package box

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// An independent implementation of the CSS Lists 3 counter scoping rules
// (reset creates a counter in the scope of the element, its following siblings
// and their descendants, replacing a counter made by a sibling; increment of a
// missing counter instantiates it on the element; reading a missing counter
// instantiates it on the pseudo-element) checked against random documents.

type oracleCounter struct {
	value   int
	creator int // id of the parent whose end ends the scope
}

type oracle struct {
	stacks  map[string][]oracleCounter
	created map[int][]string // parent id -> names of the counters made under it, in order
	out     []string
}

func (o *oracle) instantiate(name string, value, parent int) {
	st := o.stacks[name]
	if n := len(st); n > 0 && st[n-1].creator == parent {
		st[n-1].value = value
		return
	}
	o.stacks[name] = append(st, oracleCounter{value, parent})
	o.created[parent] = append(o.created[parent], name)
}

func (o *oracle) value(name string, parent int) int {
	st := o.stacks[name]
	if len(st) == 0 {
		o.instantiate(name, 0, parent)
		return 0
	}
	return st[len(st)-1].value
}

type genEl struct {
	id       int
	reset    [][2]string // name, value
	incr     [][2]string
	none     bool
	children []*genEl
}

func (o *oracle) walk(e *genEl, parent int) {
	if e.none {
		return
	}
	for _, r := range e.reset {
		v, err := strconv.Atoi(r[1])
		if err != nil {
			panic(err)
		}
		o.instantiate(r[0], v, parent)
	}
	for _, r := range e.incr {
		v, err := strconv.Atoi(r[1])
		if err != nil {
			panic(err)
		}
		if len(o.stacks[r[0]]) == 0 {
			o.instantiate(r[0], 0, parent)
		}
		st := o.stacks[r[0]]
		st[len(st)-1].value += v
	}
	// ::before is a child of e: a counter it creates ends with e.
	o.out = append(o.out, fmt.Sprintf("%d/%d", o.value("a", e.id), o.value("b", e.id)))
	for _, c := range e.children {
		o.walk(c, e.id)
	}
	o.out = append(o.out, fmt.Sprint(o.value("a", e.id)))
	for _, name := range o.created[e.id] {
		st := o.stacks[name]
		o.stacks[name] = st[:len(st)-1]
	}
	delete(o.created, e.id)
}

type genRNG struct{ s uint64 }

func (r *genRNG) n(k int) int {
	r.s ^= r.s << 13
	r.s ^= r.s >> 7
	r.s ^= r.s << 17
	return int(r.s%1000003) % k
}

func (r *genRNG) gen(next *int, depth int) *genEl {
	e := &genEl{id: *next}
	*next++
	e.none = r.n(12) == 0
	names := []string{"a", "b"}
	if r.n(3) == 0 {
		e.reset = append(e.reset, [2]string{names[r.n(2)], fmt.Sprint(r.n(20) - 5)})
	}
	if r.n(5) == 0 {
		// a second, different name in the same declaration
		n := names[r.n(2)]
		if len(e.reset) == 0 || e.reset[0][0] != n {
			e.reset = append(e.reset, [2]string{n, fmt.Sprint(r.n(9))})
		}
	}
	if r.n(2) == 0 {
		e.incr = append(e.incr, [2]string{names[r.n(2)], fmt.Sprint(r.n(7) - 2)})
	}
	if depth < 5 {
		for range r.n(4) {
			e.children = append(e.children, r.gen(next, depth+1))
		}
	}
	return e
}

func (e *genEl) html(sb *strings.Builder) {
	var st []string
	if e.none {
		st = append(st, "display:none")
	}
	join := func(ops [][2]string) string {
		var p []string
		for _, o := range ops {
			p = append(p, o[0]+" "+o[1])
		}
		return strings.Join(p, " ")
	}
	if len(e.reset) > 0 {
		st = append(st, "counter-reset:"+join(e.reset))
	}
	if len(e.incr) > 0 {
		st = append(st, "counter-increment:"+join(e.incr))
	}
	fmt.Fprintf(sb, `<div style="%s">`, strings.Join(st, ";"))
	for _, c := range e.children {
		c.html(sb)
	}
	sb.WriteString("</div>")
}

func TestCounterScopingMatchesTheSpecOracle(t *testing.T) {
	const css = `div::before{content:counter(a) "/" counter(b)} div::after{content:counter(a)}`
	for seed := uint64(1); seed <= 400; seed++ {
		r := &genRNG{s: seed * 0x9E3779B97F4A7C15}
		next := 1
		var roots []*genEl
		for range 1 + r.n(3) {
			roots = append(roots, r.gen(&next, 0))
		}
		var sb strings.Builder
		for _, e := range roots {
			e.html(&sb)
		}
		o := &oracle{stacks: map[string][]oracleCounter{}, created: map[int][]string{}}
		for _, e := range roots {
			o.walk(e, 0)
		}
		got := buildTexts(t, css, sb.String())
		if fmt.Sprint(got) != fmt.Sprint(o.out) {
			t.Fatalf("seed %d:\n%s\ngot  %q\nwant %q", seed, sb.String(), got, o.out)
		}
	}
}
