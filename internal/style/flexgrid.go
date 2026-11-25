package style

import (
	"math"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
)

// MaxFlexFactor bounds flex-grow and flex-shrink.
const MaxFlexFactor = 1e6

// gridCut is the n of a grid-template-columns spec cut at MaxGridTracks.
const gridCut = 1

var (
	justifyKeywords = map[string]int32{
		"normal": int32(JustifyStart), "flex-start": int32(JustifyStart), "start": int32(JustifyStart), "left": int32(JustifyStart),
		"flex-end": int32(JustifyEnd), "end": int32(JustifyEnd), "right": int32(JustifyEnd),
		"center": int32(JustifyCenter), "space-between": int32(JustifySpaceBetween),
		"space-around": int32(JustifySpaceAround), "space-evenly": int32(JustifySpaceEvenly),
	}
	alignItemsKeywords = map[string]int32{
		"normal": int32(ItemStretch), "stretch": int32(ItemStretch),
		"flex-start": int32(ItemStart), "start": int32(ItemStart), "self-start": int32(ItemStart),
		"flex-end": int32(ItemEnd), "end": int32(ItemEnd), "self-end": int32(ItemEnd),
		"center": int32(ItemCenter), "baseline": int32(ItemBaseline),
	}
	flexWrapKeywords      = map[string]int32{"nowrap": 0, "wrap": 1}
	flexDirectionKeywords = map[string]int32{"row": 0}
)

func parseFlexFactor(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if len(toks) != 1 || toks[0].Type != css.TNumber || toks[0].Num < 0 {
		return spec{}, false
	}
	return spec{f: float64(toks[0].Num)}, true
}

func parseFlexBasis(toks []css.Token) (spec, bool) {
	return parseLengthPercentage(toks, true, false)
}

func parseGap(toks []css.Token) (spec, bool) {
	if kw, ok := ident(toks); ok {
		return spec{}, kw == "normal"
	}
	return parseLengthPercentage(toks, false, false)
}

func gapShorthand(toks []css.Token) ([]propSpec, bool) {
	fs := fields(trim(toks))
	if len(fs) < 1 || len(fs) > 2 {
		return nil, false
	}
	row, ok := parseGap(fs[0])
	if !ok {
		return nil, false
	}
	col := row
	if len(fs) == 2 {
		if col, ok = parseGap(fs[1]); !ok {
			return nil, false
		}
	}
	row.kind, col.kind = specValue, specValue
	return []propSpec{{pRowGap, row}, {pColumnGap, col}}, true
}

// flexShorthand parses flex (Flexbox §7.1.1): none, auto, or one to two
// factors and a basis in any order around them; flex: 1 is 1 1 0%.
func flexShorthand(toks []css.Token) ([]propSpec, bool) {
	fs := fields(trim(toks))
	if len(fs) < 1 || len(fs) > 3 {
		return nil, false
	}
	grow, shrink := spec{f: 1}, spec{f: 1}
	basis := spec{len: css.Length{Unit: css.UnitPercent}}
	if kw, ok := ident(fs[0]); ok && len(fs) == 1 {
		switch kw {
		case "none":
			return flexSpecs(spec{}, spec{}, spec{auto: true}), true
		case "auto":
			return flexSpecs(grow, shrink, spec{auto: true}), true
		}
		return nil, false
	}
	factors, haveBasis := 0, false
	lastFactor := -2
	for i, f := range fs {
		if len(f) == 1 && f[0].Type == css.TNumber && factors < 2 && (factors == 0 || lastFactor == i-1) {
			s, ok := parseFlexFactor(f)
			if !ok {
				return nil, false
			}
			if factors == 0 {
				grow = s
			} else {
				shrink = s
			}
			factors++
			lastFactor = i
			continue
		}
		s, ok := parseFlexBasis(f)
		if !ok || haveBasis {
			return nil, false
		}
		basis, haveBasis = s, true
	}
	if factors == 0 && !haveBasis {
		return nil, false
	}
	return flexSpecs(grow, shrink, basis), true
}

func flexSpecs(grow, shrink, basis spec) []propSpec {
	grow.kind, shrink.kind, basis.kind = specValue, specValue, specValue
	return []propSpec{{pFlexGrow, grow}, {pFlexShrink, shrink}, {pFlexBasis, basis}}
}

func parseOpacity(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if len(toks) != 1 {
		return spec{}, false
	}
	v := float64(toks[0].Num)
	switch toks[0].Type {
	case css.TNumber:
	case css.TPercentage:
		v /= 100
	default:
		return spec{}, false
	}
	return spec{f: math.Max(0, math.Min(1, v))}, true
}

type trackSpec struct {
	kind TrackKind
	len  css.Length
	fr   float64
}

// gridGroup is a run of tracks repeated count times.
type gridGroup struct {
	count  int32
	tracks []trackSpec
}

func parseTrack(toks []css.Token) (trackSpec, bool) {
	toks = trim(toks)
	if len(toks) != 1 {
		return trackSpec{}, false
	}
	t := toks[0]
	switch {
	case t.Type == css.TIdent:
		switch lowerASCII(t.Text) {
		case "auto":
			return trackSpec{kind: TrackAuto}, true
		case "min-content":
			return trackSpec{kind: TrackMinContent}, true
		case "max-content":
			return trackSpec{kind: TrackMaxContent}, true
		}
	case t.Type == css.TDimension && t.Unit == css.UnitFr:
		return trackSpec{kind: TrackFr, fr: float64(t.Num)}, t.Num >= 0
	}
	l, ok := css.ParseLength(toks)
	if !ok || l.Value < 0 {
		return trackSpec{}, false
	}
	return trackSpec{kind: TrackPx, len: l}, true
}

// gridBuilder collects the groups of a track list under the MaxGridTracks bound,
// which it applies before anything is expanded.
type gridBuilder struct {
	groups []gridGroup
	plain  []trackSpec
	total  int
	cut    bool
}

func (g *gridBuilder) track(t trackSpec) {
	if g.total >= MaxGridTracks {
		g.cut = true
		return
	}
	g.plain = append(g.plain, t)
	g.total++
}

func (g *gridBuilder) repeat(r gridGroup) {
	g.flush()
	room := (MaxGridTracks - g.total) / len(r.tracks)
	if int(r.count) > room {
		r.count, g.cut = clamp32(room), true
	}
	g.total += int(r.count) * len(r.tracks)
	if r.count > 0 {
		g.groups = append(g.groups, r)
	}
}

func (g *gridBuilder) flush() {
	if len(g.plain) > 0 {
		g.groups = append(g.groups, gridGroup{count: 1, tracks: g.plain})
		g.plain = nil
	}
}

// parseGridColumns parses none or a track list, repeat() included.
func parseGridColumns(toks []css.Token) (spec, bool) {
	toks = trim(toks)
	if kw, ok := ident(toks); ok {
		return spec{none: true}, kw == "none"
	}
	var g gridBuilder
	for i := 0; i < len(toks); {
		t := toks[i]
		switch {
		case t.Type == css.TWhitespace:
			i++
		case t.Type == css.TFunction && lowerASCII(t.Text) == "repeat":
			end := min(i+int(t.End), len(toks))
			if end-i < 2 || toks[end-1].Type != css.TRParen {
				return spec{}, false
			}
			r, ok := parseRepeat(toks[i+1 : end-1])
			if !ok {
				return spec{}, false
			}
			g.repeat(r)
			i = end
		default:
			// Other functions and blocks (minmax(), fit-content(), line names) are not supported.
			tr, ok := parseTrack(toks[i : i+1])
			if !ok {
				return spec{}, false
			}
			g.track(tr)
			i++
		}
	}
	g.flush()
	if len(g.groups) == 0 {
		return spec{}, false
	}
	s := spec{grid: g.groups}
	if g.cut {
		s.n = gridCut
	}
	return s, true
}

func parseRepeat(args []css.Token) (gridGroup, bool) {
	parts := splitOn(args, isComma)
	if len(parts) != 2 || len(parts[0]) != 1 {
		return gridGroup{}, false
	}
	n := parts[0][0]
	if n.Type != css.TNumber || n.Flags&css.FlagInt == 0 || n.Num < 1 {
		return gridGroup{}, false
	}
	var tracks []trackSpec
	for _, f := range fields(parts[1]) {
		tr, ok := parseTrack(f)
		if !ok {
			return gridGroup{}, false
		}
		if len(tracks) >= MaxGridTracks { // bounds the allocation of a hostile repeat() body
			return gridGroup{}, false
		}
		tracks = append(tracks, tr)
	}
	if len(tracks) == 0 {
		return gridGroup{}, false
	}
	return gridGroup{count: clamp32(int(min(n.Num, MaxGridTracks+1))), tracks: tracks}, true
}

// gridCache shares the expanded track lists of a render: a list is expanded
// once per declaration (and per font size when it uses em or rem), whatever the
// number of styles that use it.
type gridCache struct{ m map[gridKey][]Track }

type gridKey struct {
	g       *gridGroup
	em, rem float64
}

// gridTracks resolves the groups of a grid-template-columns spec. A list that
// is not cached is charged to the Styles limit, one unit per 64 tracks, so a
// document of distinct font sizes cannot make the render allocate without bound.
func (x *computeCtx) gridTracks(groups []gridGroup) []Track {
	if x.grid == nil || len(groups) == 0 {
		return nil
	}
	key := gridKey{g: &groups[0]}
	if usesFontUnits(groups) {
		key.em, key.rem = x.c.FontSize, x.c.rootFont
	}
	if t, ok := x.grid.m[key]; ok {
		return t
	}
	out := x.expandTracks(groups)
	if x.b != nil && x.err == nil {
		x.err = x.b.Charge(budget.Styles, int64(len(out)/64+1))
	}
	if x.grid.m == nil {
		x.grid.m = make(map[gridKey][]Track)
	}
	x.grid.m[key] = out
	return out
}

func usesFontUnits(groups []gridGroup) bool {
	for i := range groups {
		for _, t := range groups[i].tracks {
			if t.kind == TrackPx && (t.len.Unit == css.UnitEm || t.len.Unit == css.UnitRem) {
				return true
			}
		}
	}
	return false
}

func (x *computeCtx) expandTracks(groups []gridGroup) []Track {
	n := 0
	for i := range groups {
		n += int(groups[i].count) * len(groups[i].tracks)
	}
	out := make([]Track, 0, n)
	for i := range groups {
		for range groups[i].count {
			for _, t := range groups[i].tracks {
				out = append(out, x.track(t))
			}
		}
	}
	return out
}

func (x *computeCtx) track(t trackSpec) Track {
	switch t.kind {
	case TrackFr:
		return Track{Kind: TrackFr, Value: clampPx(t.fr)}
	case TrackPx:
		d := x.dim(t.len, x.c.FontSize)
		if d.Kind == DimPercent {
			return Track{Kind: TrackPercent, Value: d.Value}
		}
		return Track{Kind: TrackPx, Value: d.Value}
	}
	return Track{Kind: t.kind}
}
