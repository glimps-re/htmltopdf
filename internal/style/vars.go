package style

import (
	"errors"
	"slices"
	"strconv"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

// WarnVarBudget is the warning code recorded when var() substitution reaches
// the VarBytes limit.
const WarnVarBudget = "var-budget"

// maxVarChain is the number of varSets walked by a lookup before a new set
// copies the chain into one map.
const maxVarChain = 16

type varEntry struct {
	val []css.Token
	// ok is false for a property that is guaranteed-invalid here: it hides the
	// value of the parent.
	ok bool
}

// varSet holds the custom properties in effect on an element. It is immutable
// and shared by inheritance: an element that defines no custom property uses
// its parent's set, and one that defines some stores only those, in front of
// its parent's set.
type varSet struct {
	parent *varSet
	m      map[string]varEntry
	depth  int
}

func (v *varSet) get(name string) ([]css.Token, bool) {
	for ; v != nil; v = v.parent {
		if e, ok := v.m[name]; ok {
			return e.val, e.ok
		}
	}
	return nil, false
}

// child returns the set of an element that defines own on top of v. A long
// chain is flattened; the copy is charged to Work by the caller through the
// returned size.
func (v *varSet) child(own map[string]varEntry) (*varSet, int) {
	if v == nil {
		return &varSet{m: own}, 0
	}
	if v.depth+1 < maxVarChain {
		return &varSet{parent: v, m: own, depth: v.depth + 1}, 0
	}
	flat := make(map[string]varEntry, len(own))
	var chain []*varSet
	for s := v; s != nil; s = s.parent {
		chain = append(chain, s)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for k, e := range chain[i].m {
			if e.ok {
				flat[k] = e
			} else {
				delete(flat, k)
			}
		}
	}
	for k, e := range own {
		if e.ok {
			flat[k] = e
		} else {
			delete(flat, k)
		}
	}
	return &varSet{m: flat}, len(flat)
}

// tokenBytes returns the length of the CSS text a token serializes to, at least
// 1: this is what VarBytes counts.
func tokenBytes(t *css.Token) int64 {
	var n int
	switch t.Type {
	case css.TNumber, css.TPercentage, css.TDimension:
		var buf [32]byte
		n = len(strconv.AppendFloat(buf[:0], float64(t.Num), 'g', -1, 32)) + len(t.Text)
		if t.Type == css.TPercentage || t.Type == css.TDimension && t.Unit != css.UnitOther {
			n++
		}
	case css.TString:
		n = len(t.Text) + 2
	case css.THash, css.TFunction, css.TAtKeyword:
		n = len(t.Text) + 1
	case css.TURL:
		n = len(t.Text) + 5
	default:
		n = len(t.Text)
	}
	return int64(max(1, n))
}

// charge accounts the tokens a substitution produces: their text in VarBytes
// and one Work unit each. budgetHit is true when VarBytes is exhausted: the
// substitution is then invalid, not fatal.
func (r *Resolver) charge(toks []css.Token) (budgetHit bool, err error) {
	var bytes int64
	for i := range toks {
		bytes += tokenBytes(&toks[i])
	}
	if err := r.b.Charge(budget.Work, int64(len(toks))); err != nil {
		return false, err
	}
	if err := r.b.Charge(budget.VarBytes, bytes); err != nil {
		var le *budget.LimitError
		if errors.As(err, &le) && le.Kind == budget.VarBytes {
			r.b.Warn(WarnVarBudget, "var() substitution limit reached")
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// lookupResult tells what a custom property reference found.
type lookupResult uint8

const (
	// varMissing: undefined or guaranteed-invalid: the fallback applies.
	varMissing lookupResult = iota
	varFound
	// varCycle: the reference closes a cycle: it is invalid, fallback or not
	// (CSS Variables 1 §2.3).
	varCycle
)

// substitute replaces every var() of toks by the value of its custom property
// (or its fallback). It reports false when the result is invalid at
// computed-value time: an undefined property without fallback, a cycle, or the
// VarBytes limit. The error is a fatal budget error (Work or the context).
func (r *Resolver) substitute(toks []css.Token, lookup func(string) ([]css.Token, lookupResult, error)) ([]css.Token, bool, error) {
	out := make([]css.Token, 0, len(toks))
	for i := 0; i < len(toks); {
		t := toks[i]
		if t.Type != css.TFunction || !eqFold(t.Text, "var") {
			out = append(out, t)
			i++
			continue
		}
		end := skip(toks, i)
		args := toks[i+1 : end]
		if n := len(args); n > 0 && args[n-1].Type == css.TRParen && closedBlock(args) {
			args = args[:n-1]
		}
		name, fallback, hasFallback, ok := varArgs(args)
		if !ok {
			return nil, false, nil
		}
		val, res, err := lookup(name)
		if err != nil || res == varCycle {
			return nil, false, err
		}
		if res == varMissing {
			if !hasFallback {
				return nil, false, nil
			}
			val, ok, err = r.substitute(fallback, lookup)
			if err != nil || !ok {
				return nil, false, err
			}
			val = trim(val)
		}
		hit, err := r.charge(val)
		if err != nil || hit {
			return nil, false, err
		}
		out = append(out, val...)
		i = end
	}
	return withEnds(out), true, nil
}

// closedBlock reports whether the last token of args closes the function they
// belong to rather than a nested block.
func closedBlock(args []css.Token) bool {
	for i := 0; i < len(args); {
		if i == len(args)-1 {
			return args[i].Type == css.TRParen
		}
		i = skip(args, i)
	}
	return false
}

// varArgs parses the arguments of var(): a custom property name, then
// optionally a comma and a fallback.
func varArgs(args []css.Token) (name string, fallback []css.Token, hasFallback, ok bool) {
	args = trim(args)
	if len(args) == 0 || args[0].Type != css.TIdent || len(args[0].Text) < 3 || args[0].Text[:2] != "--" {
		return "", nil, false, false
	}
	name = args[0].Text
	rest := trim(args[1:])
	if len(rest) == 0 {
		return name, nil, false, true
	}
	if rest[0].Type != css.TComma {
		return "", nil, false, false
	}
	return name, rest[1:], true, true
}

// customResolver resolves the custom properties declared on one element.
type customResolver struct {
	r       *Resolver
	parent  *varSet
	pending map[string]*decl
	state   map[string]uint8
	values  map[string]varEntry
	// path is the stack of properties being resolved; cyclic marks the members
	// of a cycle found on it.
	path   []string
	cyclic map[string]bool
}

const (
	stateVisiting uint8 = iota + 1
	stateDone
)

// resolveCustoms computes the custom properties of an element from its pending
// declarations and the parent's set. A cycle (all its members), an undefined
// reference without fallback or the VarBytes limit makes a property
// guaranteed-invalid. Work is charged for the declarations and the sort.
func (r *Resolver) resolveCustoms(parent *varSet, pending map[string]*decl) (*varSet, error) {
	if len(pending) == 0 {
		if parent == nil {
			return emptyVars, nil
		}
		return parent, nil
	}
	if err := r.b.Charge(budget.Work, int64(2*len(pending))); err != nil {
		return nil, err
	}
	cr := &customResolver{
		r: r, parent: parent, pending: pending,
		state:  make(map[string]uint8, len(pending)),
		values: make(map[string]varEntry, len(pending)),
		cyclic: map[string]bool{},
	}
	// Sorted, so that which property meets the VarBytes limit first does not
	// depend on map order.
	names := make([]string, 0, len(pending))
	for name := range pending {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if cr.state[name] == 0 {
			if err := cr.resolve(name); err != nil {
				return nil, err
			}
		}
	}
	set, copied := parent.child(cr.values)
	if err := r.b.Charge(budget.Work, int64(copied)); err != nil {
		return nil, err
	}
	return set, nil
}

func (cr *customResolver) resolve(name string) error {
	d := cr.pending[name]
	cr.state[name] = stateVisiting
	cr.path = append(cr.path, name)
	var val []css.Token
	valid := false
	switch d.kw {
	case specInherit, specUnset:
		val, valid = cr.parent.get(name)
	case specInitial:
	default:
		if !hasVar(d.toks) {
			val, valid = d.toks, true
			break
		}
		var err error
		if val, valid, err = cr.r.substitute(d.toks, cr.lookup); err != nil {
			return err
		}
	}
	cr.path = cr.path[:len(cr.path)-1]
	cr.state[name] = stateDone
	cr.values[name] = varEntry{val: val, ok: valid && !cr.cyclic[name]}
	return nil
}

func (cr *customResolver) lookup(name string) ([]css.Token, lookupResult, error) {
	if _, ok := cr.pending[name]; !ok {
		v, ok := cr.parent.get(name)
		if ok {
			return v, varFound, nil
		}
		return nil, varMissing, nil
	}
	switch cr.state[name] {
	case stateVisiting:
		// Every property from name to the top of the path is in the cycle.
		if i := slices.Index(cr.path, name); i >= 0 {
			for _, member := range cr.path[i:] {
				cr.cyclic[member] = true
			}
		}
		return nil, varCycle, nil
	case 0:
		if err := cr.resolve(name); err != nil {
			return nil, varMissing, err
		}
	}
	if e := cr.values[name]; e.ok {
		return e.val, varFound, nil
	}
	return nil, varMissing, nil
}

var emptyVars = &varSet{}
