package box

import (
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/style"
)

// counterInst is a counter in scope: its value and the scope that created it.
type counterInst struct {
	value int32
	scope int
}

// counterScope is the set of counters created by the children of one element:
// they live until the end of that element (CSS Lists 3 §4.4).
type counterScope struct {
	id      int
	created []string
}

// counterSet holds the counters in scope, innermost last per name.
type counterSet struct {
	byName map[string][]counterInst
	scopes []counterScope
	nextID int
}

// enterScope opens the scope of the children of an element.
func (bd *builder) enterScope() {
	bd.counters.nextID++
	bd.counters.scopes = append(bd.counters.scopes, counterScope{id: bd.counters.nextID})
}

// leaveScope ends the scope entered last and drops the counters it created.
func (bd *builder) leaveScope() {
	cs := &bd.counters
	top := cs.scopes[len(cs.scopes)-1]
	cs.scopes = cs.scopes[:len(cs.scopes)-1]
	for i := len(top.created) - 1; i >= 0; i-- {
		name := top.created[i]
		stack := cs.byName[name]
		if len(stack) <= 1 {
			delete(cs.byName, name)
			continue
		}
		cs.byName[name] = stack[:len(stack)-1]
	}
}

func (cs *counterSet) current() *counterScope { return &cs.scopes[len(cs.scopes)-1] }

// instantiate creates a counter named name in the current scope. A counter of
// that name created by a sibling (the same scope) is replaced instead of
// nested (CSS Lists 3 §4.4.1).
func (cs *counterSet) instantiate(name string, value int32) {
	sc := cs.current()
	if cs.byName == nil {
		cs.byName = make(map[string][]counterInst)
	}
	stack := cs.byName[name]
	if n := len(stack); n > 0 && stack[n-1].scope == sc.id {
		stack[n-1].value = value
		return
	}
	cs.byName[name] = append(stack, counterInst{value: value, scope: sc.id})
	sc.created = append(sc.created, name)
}

func saturate(v int64) int32 {
	return int32(max(math.MinInt32, min(math.MaxInt32, v)))
}

// applyCounters runs counter-reset then counter-increment of cs, on an element
// or a pseudo-element: an increment of a counter that does not exist creates it
// with 0 first. Each operation costs one unit of Work.
func (bd *builder) applyCounters(cs *style.Computed) error {
	if len(cs.CounterReset) == 0 && len(cs.CounterIncrement) == 0 {
		return nil
	}
	if err := bd.b.Charge(budget.Work, int64(len(cs.CounterReset)+len(cs.CounterIncrement))); err != nil {
		return err
	}
	set := &bd.counters
	for _, op := range cs.CounterReset {
		set.instantiate(op.Name, op.Value)
	}
	for _, op := range cs.CounterIncrement {
		stack := set.byName[op.Name]
		if len(stack) == 0 {
			set.instantiate(op.Name, 0)
			stack = set.byName[op.Name]
		}
		top := &stack[len(stack)-1]
		top.value = saturate(int64(top.value) + int64(op.Value))
	}
	return nil
}

// counterValue returns the value of the innermost counter named name; reading a
// counter that does not exist creates it with 0 (CSS Lists 3 §4.5).
func (bd *builder) counterValue(name string) int32 {
	set := &bd.counters
	stack := set.byName[name]
	if len(stack) == 0 {
		set.instantiate(name, 0)
		return 0
	}
	return stack[len(stack)-1].value
}

// formatCounter formats v as the counter style f says: decimal-leading-zero
// pads 0 to 9 to two digits and gives negative numbers their sign.
func formatCounter(v int32, f style.CounterFormat) string {
	s := strconv.FormatInt(int64(v), 10)
	if f == style.CounterDecimalLeadingZero && v >= 0 && v < 10 {
		return "0" + s
	}
	return s
}

// MaxGeneratedBytes bounds the text that the pseudo-elements of a render
// generate, counted on the final text of each box. The text that would cross
// it is cut at a rune boundary, later pseudo-elements generate no text, and the
// render degrades with one WarnClamped "content".
const MaxGeneratedBytes = 1 << 20

// cutRunes returns the longest prefix of s of at most n bytes that ends on a
// rune boundary.
func cutRunes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// takeGenerated counts s, the final text of a generated box, against
// MaxGeneratedBytes and returns the part that fits.
func (bd *builder) takeGenerated(s string) string {
	room := MaxGeneratedBytes - bd.generated
	if len(s) > room {
		bd.b.Warn(budget.WarnClamped, "content")
		s = cutRunes(s, room)
	}
	bd.generated += len(s)
	return s
}

// generatedText returns the text of the content of a pseudo-element, at most
// as long as the room left before MaxGeneratedBytes (the processed text is never
// longer than a few times the input, and is counted again when it is boxed).
// A single text item is shared with the style, not copied.
func (bd *builder) generatedText(cs *style.Computed) (string, error) {
	if err := bd.b.Charge(budget.Work, int64(len(cs.Content))); err != nil {
		return "", err
	}
	room := MaxGeneratedBytes - bd.generated
	if room <= 0 {
		bd.b.Warn(budget.WarnClamped, "content")
		return "", nil
	}
	if len(cs.Content) == 1 && cs.Content[0].Kind == style.ContentText {
		return cutRunes(cs.Content[0].Text, room), nil
	}
	var sb []byte
	for _, it := range cs.Content {
		if len(sb) >= room {
			break
		}
		switch it.Kind {
		case style.ContentText:
			sb = append(sb, cutRunes(it.Text, room-len(sb))...)
		case style.ContentCounter:
			sb = append(sb, formatCounter(bd.counterValue(it.Text), it.Format)...)
		}
	}
	return string(sb), nil
}
