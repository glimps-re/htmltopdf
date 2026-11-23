package dom

import (
	"bytes"
	"hash/maphash"
	"math"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The estimate is an upper bound of the node cost (nodes plus attributes) of
// the tree x/net/html builds for a given input, computed from the token
// stream in one linear pass without building the tree.
//
// Besides one node per token, the parser creates nodes from three sources:
//   - implied elements (html, head, body, tbody, tr, colgroup, and the p or br
//     created by a stray </p> or </br>; every end tag is charged one node);
//   - clones made when it reconstructs the active formatting elements: every
//     start tag and text token can clone each entry of the list;
//   - clones made by the adoption agency algorithm on a misnested end tag of
//     a formatting element: at most 8 rounds of at most 4 clones.
//
// A simplified model of the stack of open elements and of the list of active
// formatting elements tracks the second and third sources. The model never
// pops what the parser might keep, so that it overestimates.

const (
	flagSpecial uint8 = 1 << iota
	flagBarrier
	flagFormatting
	flagVoid
	flagTableEnd
	flagTableScope
	flagRaw
)

var tagFlags = buildTagFlags()

func buildTagFlags() map[atom.Atom]uint8 {
	flags := make(map[atom.Atom]uint8)
	add := func(f uint8, names ...string) {
		for _, n := range names {
			if a := atom.Lookup([]byte(n)); a != 0 {
				flags[a] |= f
			}
		}
	}
	add(flagSpecial, "address", "applet", "area", "article", "aside", "base", "basefont",
		"bgsound", "blockquote", "body", "br", "button", "caption", "center", "col",
		"colgroup", "dd", "details", "dir", "div", "dl", "dt", "embed", "fieldset",
		"figcaption", "figure", "footer", "form", "frame", "frameset", "h1", "h2", "h3",
		"h4", "h5", "h6", "head", "header", "hgroup", "hr", "html", "iframe", "img",
		"input", "keygen", "li", "link", "listing", "main", "marquee", "menu", "meta",
		"nav", "noembed", "noframes", "noscript", "object", "ol", "p", "param",
		"plaintext", "pre", "script", "section", "select", "source", "style", "summary",
		"table", "tbody", "td", "template", "textarea", "tfoot", "th", "thead", "title",
		"tr", "track", "ul", "wbr", "xmp",
		"mi", "mo", "mn", "ms", "mtext", "annotation-xml", "foreignobject", "desc")
	add(flagBarrier, "applet", "button", "caption", "html", "marquee", "object", "ol",
		"table", "td", "template", "th", "ul",
		"mi", "mo", "mn", "ms", "mtext", "annotation-xml", "foreignobject", "desc", "title")
	add(flagFormatting, "a", "b", "big", "code", "em", "font", "i", "nobr", "s", "small",
		"strike", "strong", "tt", "u")
	add(flagVoid, "area", "base", "basefont", "bgsound", "br", "col", "embed", "frame",
		"hr", "img", "input", "keygen", "link", "meta", "param", "source", "track", "wbr")
	add(flagTableEnd, "caption", "col", "colgroup", "table", "tbody", "td", "tfoot", "th", "thead", "tr")
	add(flagTableScope, "html", "table", "template")
	add(flagRaw, "iframe", "noembed", "noframes", "noscript", "plaintext", "script",
		"style", "textarea", "title", "xmp")
	return flags
}

const (
	tokenPollInterval = 1024
	maxAdoptions      = 8
)

type afeEntry struct {
	key, sig uint64
	cost     int64
}

// stackEntry is an element of the model of the stack of open elements with
// the inclusive prefix counts of special, scope-barrier and table-scope-barrier
// elements, and the index of the previous entry with the same key.
type stackEntry struct {
	key                      uint64
	prevSame                 int32
	special, barrier, tscope int32
}

type estimator struct {
	limit int64
	est   int64
	seed  maphash.Seed

	// Model of the stack of open elements, and the index of the topmost entry
	// of each key.
	stack []stackEntry
	top   map[uint64]int32
	// n is len(stack) as an int32: positions never exceed MaxInt32 because the
	// estimate passes any limit first.
	n int32

	// Model of the list of active formatting elements.
	afe     []afeEntry
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

// estimateNodes returns the estimated node cost of buf. It stops as soon as
// the estimate exceeds limit and returns that partial estimate (> limit).
func estimateNodes(buf []byte, limit int64, poll func() error) (int64, error) {
	e, err := runEstimator(buf, limit, poll)
	return e.est, err
}

func runEstimator(buf []byte, limit int64, poll func() error) (*estimator, error) {
	e := &estimator{
		limit:   limit,
		seed:    maphash.MakeSeed(),
		top:     make(map[uint64]int32),
		maxCost: 1,
		est:     3,
	}
	e.push(uint64(atom.Html), tagFlags[atom.Html])
	z := html.NewTokenizer(bytes.NewReader(buf))
	consumed := 0
	for n := 1; e.est <= e.limit; n++ {
		if n%tokenPollInterval == 0 {
			if err := poll(); err != nil {
				return e, err
			}
		}
		if e.svgSeen {
			z.NextIsNotRawText()
		}
		tt := z.Next()
		if tt == html.ErrorToken {
			e.checkBig(len(buf) - consumed)
			return e, nil
		}
		raw := z.Raw()
		consumed += len(raw)
		if tt != html.TextToken {
			e.checkBig(len(raw))
			e.watchCDATA(raw)
		}
		switch tt {
		case html.TextToken:
			e.est += textNodes(z.Text()) + e.afeCost
		case html.CommentToken, html.DoctypeToken:
			e.est++
		case html.StartTagToken:
			e.start(z, false)
		case html.SelfClosingTagToken:
			e.start(z, true)
		case html.EndTagToken:
			e.end(z)
		}
	}
	return e, nil
}

// textNodes returns the number of nodes a text token can become: some
// insertion modes (in column group, after head, in frameset...) insert the
// leading whitespace where they are and reprocess the rest elsewhere.
func textNodes(text []byte) int64 {
	if len(text) < 2 || !isHTMLSpace(rune(text[0])) {
		return 1
	}
	for _, c := range text[1:] {
		if !isHTMLSpace(rune(c)) {
			return 2
		}
	}
	return 1
}

// checkBig covers a token that swallows input: after a raw-text start tag in a
// document with foreign content, the parser may tokenize the following bytes
// as raw text where the model tokenizes markup. A markup token (an
// unterminated comment, a quoted value) could then hide nodes the parser
// creates, so it is charged as if every other byte started a node that clones
// every other one.
func (e *estimator) checkBig(n int) {
	if e.rawPending || e.cdataPending {
		e.est += int64(n) * int64(n) / 4
	}
}

// watchCDATA tracks the span of a <![CDATA[ section, which only the parser
// can tell from a bogus comment; checkBig then covers the tokens inside it.
func (e *estimator) watchCDATA(raw []byte) {
	if e.cdataPending {
		e.cdataPending = !bytes.Contains(raw, []byte("]]>"))
		return
	}
	if e.svgSeen && bytes.HasPrefix(raw, []byte("<![CDATA[")) {
		e.cdataPending = !bytes.Contains(raw[len("<![CDATA["):], []byte("]]>"))
	}
}

func (e *estimator) key(at atom.Atom, name []byte) uint64 {
	if at != 0 {
		return uint64(at)
	}
	return maphash.Bytes(e.seed, name) | 1<<63
}

func (e *estimator) attrHash(k, v []byte) uint64 {
	var h maphash.Hash // its Write methods never fail
	h.SetSeed(e.seed)
	_, _ = h.Write(k)
	_ = h.WriteByte(0)
	_, _ = h.Write(v)
	return h.Sum64()
}

// tag reads the name of the current tag token: its atom, flags and model key.
func (e *estimator) tag(z *html.Tokenizer) (at atom.Atom, fl uint8, key uint64, hasAttr bool) {
	name, hasAttr := z.TagName()
	at = atom.Lookup(name)
	return at, tagFlags[at], e.key(at, name), hasAttr
}

func (e *estimator) start(z *html.Tokenizer, selfClosing bool) {
	at, fl, key, hasAttr := e.tag(z)

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
func (e *estimator) mergedIntoOpen(at atom.Atom) bool {
	return (at == atom.Html || at == atom.Head || at == atom.Body) && e.topmost(uint64(at)) >= 0
}

func (e *estimator) end(z *html.Tokenizer) {
	at, fl, key, _ := e.tag(z)

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
func (e *estimator) endFormatting(key uint64) bool {
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

func (e *estimator) genericEnd(key uint64, fl uint8) {
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

func (e *estimator) push(key uint64, fl uint8) {
	if e.n == math.MaxInt32 {
		e.est = max(e.est, e.limit+1) // stop: no real input has that many open elements
		return
	}
	var s, b, ts int32
	if e.n > 0 {
		last := &e.stack[e.n-1]
		s, b, ts = last.special, last.barrier, last.tscope
	}
	if fl&flagSpecial != 0 {
		s++
	}
	if fl&flagBarrier != 0 {
		b++
	}
	if fl&flagTableScope != 0 {
		ts++
	}
	prev, ok := e.top[key]
	if !ok {
		prev = -1
	}
	e.top[key] = e.n
	e.stack = append(e.stack, stackEntry{key: key, prevSame: prev, special: s, barrier: b, tscope: ts})
	e.n++
}

func (e *estimator) popTo(p int32) {
	for i := e.n - 1; i >= p; i-- {
		if prev := e.stack[i].prevSame; prev < 0 {
			delete(e.top, e.stack[i].key)
		} else {
			e.top[e.stack[i].key] = prev
		}
	}
	e.stack = e.stack[:p]
	e.n = p
}

func (e *estimator) topmost(key uint64) int32 {
	if i, ok := e.top[key]; ok {
		return i
	}
	return -1
}

// above returns the numbers of special, scope-barrier and table-scope-barrier
// elements above position p.
func (e *estimator) above(p int32) (special, barrier, tableScope int32) {
	top, at := &e.stack[e.n-1], &e.stack[p]
	return top.special - at.special, top.barrier - at.barrier, top.tscope - at.tscope
}

func (e *estimator) lastFormatting(key uint64) int {
	for i := len(e.afe) - 1; i >= 0; i-- {
		if e.afe[i].key == key {
			return i
		}
	}
	return -1
}

func (e *estimator) removeFormatting(i int) {
	e.afeCost -= e.afe[i].cost
	e.afe = append(e.afe[:i], e.afe[i+1:]...)
}

// pushFormatting mirrors the Noah's Ark clause: a fourth identical entry
// evicts the oldest one.
func (e *estimator) pushFormatting(key, sig uint64, cost int64) {
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
	e.afe = append(e.afe, afeEntry{key: key, sig: sig, cost: cost})
	e.afeCost += cost
	e.maxCost = max(e.maxCost, cost)
}
