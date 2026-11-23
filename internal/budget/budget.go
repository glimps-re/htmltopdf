// Package budget enforces the per-render resource limits of the converter.
//
// Every input-derived quantity is charged to a Budget before the matching
// allocation or loop. The first overflow returns a *LimitError, which callers
// propagate unchanged. A Budget also polls the render context and collects a
// bounded set of warnings.
package budget

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/glimps-re/htmltopdf/internal/textsafe"
)

// Kind identifies a limited quantity.
type Kind uint8

// Limited quantities. Each is named after the public limit that bounds it.
const (
	InputBytes Kind = iota
	Nodes
	CSSBytes
	Selectors
	VarBytes
	Styles
	Boxes
	Glyphs
	Pages
	OutputBytes
	// Work counts abstract CPU units charged by loops whose cost is not linear
	// in a counted quantity. Charging it also polls the context.
	Work
	// HTMLDepth is the fixed nesting bound of the HTML parser. It is never
	// charged: it only names the limit in a LimitError. It must stay the last
	// Kind before NumKinds: Charge relies on it to reject unchargeable kinds.
	HTMLDepth
	NumKinds
)

var kindNames = [NumKinds]string{
	InputBytes:  "MaxInputBytes",
	Nodes:       "MaxNodes",
	CSSBytes:    "MaxCSSBytes",
	Selectors:   "MaxSelectors",
	VarBytes:    "MaxVarBytes",
	Styles:      "MaxStyles",
	Boxes:       "MaxBoxes",
	Glyphs:      "MaxGlyphs",
	Pages:       "MaxPages",
	OutputBytes: "MaxOutputBytes",
	Work:        "MaxWork",
	HTMLDepth:   "HTMLDepth",
}

// String returns the public name of the limit that bounds k.
func (k Kind) String() string {
	if k >= NumKinds {
		return fmt.Sprintf("Kind(%d)", uint8(k))
	}
	return kindNames[k]
}

// WorkPollInterval is the number of Work units between two context polls.
const WorkPollInterval = 4096

// MaxWarnings is the number of distinct warnings kept; further distinct
// warnings only increment the dropped counter.
const MaxWarnings = 256

// MaxDetailBytes is the maximum length of a warning detail.
const MaxDetailBytes = 80

// ErrInvalidCharge is returned by Charge for a Kind that cannot be charged
// (unknown or HTMLDepth) or a negative amount.
var ErrInvalidCharge = errors.New("budget: invalid charge")

// Limits holds the maximum total of each Kind, indexed by Kind. The caller
// resolves it fully, with entries >= 0; Defaults returns the default values. A
// zero entry rejects any positive charge of that Kind.
type Limits [NumKinds]int64

// Defaults returns the default limits. The MaxWork default is provisional
// until it is calibrated on the corpus.
func Defaults() Limits {
	return Limits{
		InputBytes:  2 << 20,
		Nodes:       100_000,
		CSSBytes:    512 << 10,
		Selectors:   5_000,
		VarBytes:    256 << 10,
		Styles:      20_000,
		Boxes:       200_000,
		Glyphs:      2_000_000,
		Pages:       500,
		OutputBytes: 64 << 20,
		Work:        100_000_000,
		HTMLDepth:   512,
	}
}

// LimitError reports that a charge made the total of its Kind exceed Max.
type LimitError struct {
	Kind Kind
	Max  int64
}

// Error implements error.
func (e *LimitError) Error() string {
	return fmt.Sprintf("budget: %s exceeded (max %d)", e.Kind, e.Max)
}

// Warning is a deduplicated warning with its number of occurrences.
type Warning struct {
	Code   string
	Detail string
	Count  int
}

type warnKey struct{ code, detail string }

// slot holds the running total of a Kind and the total up to which Charge
// needs no slow path: the limit, or for Work the end of the polling interval.
type slot struct{ used, ceiling int64 }

// Budget tracks the usage of one render. It is not safe for concurrent use.
type Budget struct {
	ctx        context.Context
	max        Limits
	slots      [NumKinds]slot
	checkpoint func(stage string)
	warnIndex  map[warnKey]int
	warnings   []Warning
	dropped    int
}

// New returns a Budget bound to ctx, which must be non-nil (Charge of Work
// and Poll call ctx.Err) and already carry the render timeout. hook is called by Checkpoint; it is nil in production.
func New(ctx context.Context, l Limits, hook func(stage string)) *Budget {
	b := &Budget{ctx: ctx, max: l, checkpoint: hook}
	for k := range l {
		b.slots[k].ceiling = l[k]
	}
	b.slots[Work].ceiling = min(l[Work], WorkPollInterval-1)
	return b
}

// Charge adds n units to k before the caller allocates or loops. The total
// keeps growing after a failure. It returns a *LimitError when the new total
// exceeds the limit (a total equal to the limit succeeds), the context error
// when a charge of Work crosses a multiple of WorkPollInterval and the context
// is done, and ErrInvalidCharge for an unknown Kind, HTMLDepth or a negative n.
func (b *Budget) Charge(k Kind, n int64) error {
	if k < HTMLDepth && n >= 0 {
		if s := &b.slots[k]; s.used+n >= 0 && s.used+n <= s.ceiling {
			s.used += n
			return nil
		}
	}
	return b.chargeSlow(k, n)
}

// chargeSlow handles the cases the inlined fast path of Charge leaves: errors,
// int64 overflow and context polling. It is entered with nothing modified.
//
//go:noinline
func (b *Budget) chargeSlow(k Kind, n int64) error {
	if k >= HTMLDepth || n < 0 {
		return ErrInvalidCharge
	}
	before := b.slots[k].used
	if n > math.MaxInt64-before {
		b.slots[k].used = math.MaxInt64
		return &LimitError{Kind: k, Max: b.max[k]}
	}
	b.slots[k].used = before + n
	if b.slots[k].used > b.max[k] {
		return &LimitError{Kind: k, Max: b.max[k]}
	}
	if k == Work {
		b.slots[Work].ceiling = min(b.max[Work], (b.slots[Work].used/WorkPollInterval+1)*WorkPollInterval-1)
		if b.slots[k].used/WorkPollInterval != before/WorkPollInterval {
			return b.ctx.Err()
		}
	}
	return nil
}

// Used returns the total charged to k.
func (b *Budget) Used(k Kind) int64 {
	if k >= NumKinds {
		return 0
	}
	return b.slots[k].used
}

// Max returns the limit of k, or 0 for an unknown Kind.
func (b *Budget) Max(k Kind) int64 {
	if k >= NumKinds {
		return 0
	}
	return b.max[k]
}

// Poll returns the context error, for use between stages.
func (b *Budget) Poll() error {
	return b.ctx.Err()
}

// Checkpoint calls the hook, if any, with the name of the stage just completed.
func (b *Budget) Checkpoint(stage string) {
	if b.checkpoint != nil {
		b.checkpoint(stage)
	}
}

// Warn records a warning. The code must be a constant of the caller (a
// WarningCode): it is not sanitized. The detail is hostile text: it is sanitized and cut
// to MaxDetailBytes before deduplication. At most MaxWarnings distinct
// warnings are kept. A repeat of a kept warning always increments its Count; an
// occurrence that would create a further entry only increments the dropped count.
func (b *Budget) Warn(code, detail string) {
	key := warnKey{code: code, detail: sanitizeDetail(detail)}
	if i, ok := b.warnIndex[key]; ok {
		b.warnings[i].Count++
		return
	}
	if len(b.warnings) >= MaxWarnings {
		b.dropped++
		return
	}
	if b.warnIndex == nil {
		b.warnIndex = make(map[warnKey]int)
	}
	key.detail = strings.Clone(key.detail)
	b.warnIndex[key] = len(b.warnings)
	b.warnings = append(b.warnings, Warning{Code: key.code, Detail: key.detail, Count: 1})
}

// Warnings returns a copy of the warnings sorted by Code then Detail, and the
// number of warnings dropped because the cap was reached.
func (b *Budget) Warnings() (warnings []Warning, dropped int) {
	out := make([]Warning, len(b.warnings))
	copy(out, b.warnings)
	slices.SortFunc(out, func(a, b Warning) int {
		return cmp.Or(cmp.Compare(a.Code, b.Code), cmp.Compare(a.Detail, b.Detail))
	})
	return out, b.dropped
}

// sanitizeDetail reads only the runes needed to fill MaxDetailBytes, so its
// cost does not depend on the length of hostile input. It returns a substring
// of s, without allocating, when the kept prefix is clean.
func sanitizeDetail(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isUnsafeRune(r, size) {
			break
		}
		if i+size > MaxDetailBytes {
			return s[:i]
		}
		i += size
	}
	if i == len(s) {
		return s
	}
	var sb strings.Builder
	sb.Grow(min(len(s), MaxDetailBytes))
	sb.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if isUnsafeRune(r, size) {
			r = '?'
		}
		if sb.Len()+utf8.RuneLen(r) > MaxDetailBytes {
			break
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func isUnsafeRune(r rune, size int) bool {
	return r == utf8.RuneError && size == 1 || textsafe.IsUnsafe(r)
}
