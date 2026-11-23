package dom

// A frozen copy of the estimator before the compact stack representation. The
// compact one must give identical estimates (TestEstimatorMatchesReference).

import (
	"bytes"
	"hash/maphash"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type refAfeEntry struct {
	key, sig uint64
	cost     int64
}

type refEstimator struct {
	limit int64
	est   int64
	seed  maphash.Seed

	// Model of the stack of open elements: keys, prefix counts of special and
	// barrier elements, and the positions of each key.
	stack   []uint64
	special []int
	barrier []int
	tscope  []int
	pos     map[uint64][]int

	// Model of the list of active formatting elements.
	afe     []refAfeEntry
	afeCost int64
	maxCost int64

	// svgSeen is set once an svg or math element was seen: the parser then
	// tokenizes some raw-text elements as markup, depending on a namespace
	// the model does not track, so the tokenizer is always run in markup mode.
	svgSeen    bool
	rawPending bool
	rawKey     uint64
	// cdataPending is set after a <![CDATA[ token in a document with foreign
	// content: the parser may treat everything up to ]]> as text.
	cdataPending bool
}

// refEstimateNodes returns the estimated node cost of buf. It stops as soon as
// the estimate exceeds limit and returns that partial estimate (> limit).
func refEstimateNodes(buf []byte, limit int64, poll func() error) (int64, error) {
	e := &refEstimator{
		limit:   limit,
		seed:    maphash.MakeSeed(),
		pos:     make(map[uint64][]int),
		maxCost: 1,
		est:     3,
	}
	e.push(uint64(atom.Html), tagFlags[atom.Html])
	z := html.NewTokenizer(bytes.NewReader(buf))
	consumed := 0
	for n := 1; e.est <= e.limit; n++ {
		if n%tokenPollInterval == 0 {
			if err := poll(); err != nil {
				return e.est, err
			}
		}
		if e.svgSeen {
			z.NextIsNotRawText()
		}
		tt := z.Next()
		if tt == html.ErrorToken {
			e.checkBig(len(buf) - consumed)
			return e.est, nil
		}
		consumed += len(z.Raw())
		switch tt {
		case html.TextToken:
			e.est += textNodes(z.Text()) + e.afeCost
		case html.CommentToken, html.DoctypeToken:
			e.est++
			e.checkBig(len(z.Raw()))
			e.watchCDATA(z.Raw())
		case html.StartTagToken:
			e.checkBig(len(z.Raw()))
			e.watchCDATA(z.Raw())
			e.start(z, false)
		case html.SelfClosingTagToken:
			e.checkBig(len(z.Raw()))
			e.watchCDATA(z.Raw())
			e.start(z, true)
		case html.EndTagToken:
			e.checkBig(len(z.Raw()))
			e.watchCDATA(z.Raw())
			e.end(z)
		}
	}
	return e.est, nil
}

// checkBig covers a token that swallows input: after a raw-text start tag in a
// document with foreign content, the parser may tokenize the following bytes
// as raw text where the model tokenizes markup. A markup token (an
// unterminated comment, a quoted value) could then hide nodes the parser
// creates, so it is charged as if every other byte started a node that clones
// every other one.
func (e *refEstimator) checkBig(n int) {
	if e.rawPending || e.cdataPending {
		e.est += int64(n) * int64(n) / 4
	}
}

// watchCDATA tracks the span of a <![CDATA[ section, which only the parser
// can tell from a bogus comment; checkBig then covers the tokens inside it.
func (e *refEstimator) watchCDATA(raw []byte) {
	if e.cdataPending {
		e.cdataPending = !bytes.Contains(raw, []byte("]]>"))
		return
	}
	if e.svgSeen && bytes.HasPrefix(raw, []byte("<![CDATA[")) {
		e.cdataPending = !bytes.Contains(raw[len("<![CDATA["):], []byte("]]>"))
	}
}

func (e *refEstimator) key(at atom.Atom, name []byte) uint64 {
	if at != 0 {
		return uint64(at)
	}
	return maphash.Bytes(e.seed, name) | 1<<63
}

func (e *refEstimator) attrHash(k, v []byte) uint64 {
	var h maphash.Hash
	h.SetSeed(e.seed)
	_, _ = h.Write(k)
	_ = h.WriteByte(0)
	_, _ = h.Write(v)
	return h.Sum64()
}

func (e *refEstimator) start(z *html.Tokenizer, selfClosing bool) {
	name, hasAttr := z.TagName()
	at := atom.Lookup(name)
	fl := tagFlags[at]
	key := e.key(at, name)

	var attrs int64
	var sig uint64
	for hasAttr {
		var k, v []byte
		k, v, hasAttr = z.TagAttr()
		attrs++
		sig += e.attrHash(k, v)
	}
	cost := 1 + attrs

	if fl&flagFormatting != 0 && (at == atom.A || at == atom.Nobr) {
		e.endFormatting(key)
	}
	e.est += cost + e.afeCost
	if fl&flagTableEnd != 0 {
		// Flat charge for the elements the table insertion modes insert (tbody,
		// tr, colgroup) wherever the model believes the parser is: the model
		// does not follow foreign content, templates or foster parenting here.
		e.est += 2
	}
	if (at == atom.Svg || at == atom.Math) && !selfClosing {
		e.svgSeen = true
	}
	if fl&flagRaw != 0 && e.svgSeen {
		e.rawPending, e.rawKey = true, key
	}
	if fl&flagFormatting != 0 {
		e.pushFormatting(key, sig, cost)
	}
	if fl&flagVoid == 0 && !e.mergedIntoOpen(at) {
		e.push(key, fl)
	}
}

// mergedIntoOpen reports an html start tag, a second head or a second body: the
// parser merges its attributes into the open element (html is always open,
// implied by the first token) instead of inserting one.
func (e *refEstimator) mergedIntoOpen(at atom.Atom) bool {
	return (at == atom.Html || at == atom.Head || at == atom.Body) && e.topmost(uint64(at)) >= 0
}

func (e *refEstimator) end(z *html.Tokenizer) {
	name, _ := z.TagName()
	at := atom.Lookup(name)
	fl := tagFlags[at]
	key := e.key(at, name)

	e.est++
	if at == atom.Br {
		e.est += e.afeCost
	}
	if e.rawPending && key == e.rawKey {
		e.rawPending = false
	}
	if fl&flagFormatting != 0 && e.endFormatting(key) {
		return
	}
	e.genericEnd(key, fl)
}

// endFormatting models the adoption agency algorithm for an end tag (or an
// a/nobr start tag) of the formatting element key. It reports whether the
// element is in the list. The entry is removed only when the parser certainly
// removes it: the element is open and no special element sits above it.
func (e *refEstimator) endFormatting(key uint64) bool {
	i := e.lastFormatting(key)
	if i < 0 {
		return false
	}
	if p := e.topmost(key); p >= 0 {
		if sp, _, _ := e.above(p); sp == 0 {
			e.popTo(p)
			e.removeFormatting(i)
			return true
		}
	}
	later := int64(len(e.afe) - 1 - i)
	e.est += maxAdoptions * (min(later, 3) + 1) * e.maxCost
	return true
}

func (e *refEstimator) genericEnd(key uint64, fl uint8) {
	if key == uint64(atom.Html) || key == uint64(atom.Body) {
		return // the parser keeps both open until the end of the input
	}
	p := e.topmost(key)
	if p < 0 {
		return
	}
	sp, br, ts := e.above(p)
	switch {
	case fl&flagTableEnd != 0:
		if ts > 0 {
			return
		}
	case fl&flagSpecial != 0:
		if br > 0 {
			return
		}
	case sp > 0:
		return
	}
	e.popTo(p)
}

func (e *refEstimator) push(key uint64, fl uint8) {
	var s, b, ts int
	if n := len(e.stack); n > 0 {
		s, b, ts = e.special[n-1], e.barrier[n-1], e.tscope[n-1]
	}
	if fl&flagTableScope != 0 {
		ts++
	}
	if fl&flagSpecial != 0 {
		s++
	}
	if fl&flagBarrier != 0 {
		b++
	}
	e.pos[key] = append(e.pos[key], len(e.stack))
	e.stack = append(e.stack, key)
	e.special = append(e.special, s)
	e.barrier = append(e.barrier, b)
	e.tscope = append(e.tscope, ts)
}

func (e *refEstimator) popTo(p int) {
	for i := len(e.stack) - 1; i >= p; i-- {
		k := e.stack[i]
		l := e.pos[k]
		if len(l) <= 1 {
			delete(e.pos, k)
		} else {
			e.pos[k] = l[:len(l)-1]
		}
	}
	e.stack = e.stack[:p]
	e.special = e.special[:p]
	e.barrier = e.barrier[:p]
	e.tscope = e.tscope[:p]
}

func (e *refEstimator) topmost(key uint64) int {
	l := e.pos[key]
	if len(l) == 0 {
		return -1
	}
	return l[len(l)-1]
}

// above returns the numbers of special, scope-barrier and table-scope-barrier
// elements above position p.
func (e *refEstimator) above(p int) (special, barrier, tableScope int) {
	top := len(e.stack) - 1
	return e.special[top] - e.special[p], e.barrier[top] - e.barrier[p], e.tscope[top] - e.tscope[p]
}

func (e *refEstimator) lastFormatting(key uint64) int {
	for i := len(e.afe) - 1; i >= 0; i-- {
		if e.afe[i].key == key {
			return i
		}
	}
	return -1
}

func (e *refEstimator) removeFormatting(i int) {
	e.afeCost -= e.afe[i].cost
	e.afe = append(e.afe[:i], e.afe[i+1:]...)
}

// pushFormatting mirrors the Noah's Ark clause: a fourth identical entry
// evicts the oldest one.
func (e *refEstimator) pushFormatting(key, sig uint64, cost int64) {
	same, oldest := 0, -1
	for i := len(e.afe) - 1; i >= 0; i-- {
		if e.afe[i].key == key && e.afe[i].sig == sig {
			same++
			oldest = i
		}
	}
	if same >= 3 {
		e.removeFormatting(oldest)
	}
	e.afe = append(e.afe, refAfeEntry{key: key, sig: sig, cost: cost})
	e.afeCost += cost
	e.maxCost = max(e.maxCost, cost)
}

func TestEstimatorMatchesReference(t *testing.T) {
	check := func(src string) {
		t.Helper()
		got, err1 := estimateNodes([]byte(src), noLimit, noPoll)
		want, err2 := refEstimateNodes([]byte(src), noLimit, noPoll)
		if err1 != nil || err2 != nil || got != want {
			t.Fatalf("estimate %d (%v), reference %d (%v) for %q", got, err1, want, err2, src)
		}
	}
	iterations := 4000
	if testing.Short() || raceEnabled {
		iterations = 400
	}
	for name, tokens := range vocabularies {
		t.Run(name, func(*testing.T) {
			randomDocs(tokens, 5, iterations, 150, check)
		})
	}
	for _, src := range []string{
		"", "x", "<table><svg><tfoot><tr><div><td>", strings.Repeat("<b><i><u><p>x", 50),
		strings.Repeat("<div>", 500), strings.Repeat("<x1><x2></x1>", 50), strings.Repeat("<!---->", 100),
	} {
		check(src)
	}
	check(string(report(t, largeReport, testcorpus.Large)))
}
