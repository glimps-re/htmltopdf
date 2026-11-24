// Package text selects fonts, shapes text and finds line-break opportunities.
//
// A FontSet is built once and shared by concurrent renders; a Shaper belongs to
// one render. Text is shaped in left-to-right order only: bidi is a non-goal
// (design §8.4).
package text

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
)

// Limits on caller fonts, checked by NewFontSet.
const (
	MaxFontBytes      = 32 << 20
	MaxTotalFontBytes = 128 << 20
	MaxFonts          = 64
	maxFamilyBytes    = 256
)

// ErrInvalidFont is wrapped by every NewFontSet error.
var ErrInvalidFont = errors.New("text: invalid font")

// Source is a font file supplied by the caller. Family overrides the name
// read from the font.
type Source struct {
	Data   []byte
	Family string
}

// Generic names the families used for the CSS generic families; an empty field
// selects the embedded Go font.
type Generic struct {
	SansSerif string
	Serif     string
	Monospace string
}

// Face is one font file with its family, weight and style. Its metadata is read
// when the FontSet is built; the full parse happens once, on first use.
type Face struct {
	// Family is the family name as given to CSS.
	Family string
	// Weight is the OS/2 weight class, 1 to 1000.
	Weight int
	// Italic is true for italic and oblique faces.
	Italic bool

	norm string
	data []byte
	once sync.Once
	font *font.Font
	err  error
}

// Data returns the font file. It must not be modified.
func (f *Face) Data() []byte { return f.data }

// UnitsPerEm returns the em size in font units, or 0 if the font cannot be
// parsed.
func (f *Face) UnitsPerEm() uint16 {
	ft, err := f.load()
	if err != nil {
		return 0
	}
	return ft.Upem()
}

// load parses the font once. A panic in the parser is reported as an error.
func (f *Face) load() (*font.Font, error) {
	f.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				f.err = fmt.Errorf("text: parsing %q panicked: %v", f.Family, r)
			}
		}()
		ld, err := ot.NewLoader(bytes.NewReader(f.data))
		if err != nil {
			f.err = err
			return
		}
		if f.font, f.err = font.NewFont(ld); f.err == nil && f.font.Upem() == 0 {
			f.font, f.err = nil, errors.New("text: font with 0 units per em")
		}
	})
	return f.font, f.err
}

// FontSet is the immutable set of faces of a Converter.
type FontSet struct {
	byFamily map[string][]*Face
	faces    []*Face
	generic  map[string]string
	fallback []string
}

var builtin = sync.OnceValue(func() map[string][]*Face {
	out := map[string][]*Face{}
	for _, d := range [][]byte{
		goregular.TTF, goitalic.TTF, gobold.TTF, gobolditalic.TTF,
		gomono.TTF, gomonoitalic.TTF, gomonobold.TTF, gomonobolditalic.TTF,
	} {
		f, err := describe(Source{Data: d})
		if err != nil {
			panic(err) // unreachable: the embedded fonts are valid
		}
		out[f.norm] = append(out[f.norm], f)
	}
	return out
})

var keyGo, keyGoMono = normFamily("Go"), normFamily("Go Mono")

func normFamily(s string) string { return font.NormalizeFamily(strings.TrimSpace(s)) }

// describe validates src and reads its metadata without a full parse. It
// rejects collections, anything without glyf outlines (CFF) and a missing
// family.
func describe(src Source) (f *Face, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("parser panic: %v", r)
		}
	}()
	d := src.Data
	switch {
	case len(d) == 0:
		return nil, errors.New("empty")
	case len(d) > MaxFontBytes:
		return nil, fmt.Errorf("%d bytes exceeds %d", len(d), MaxFontBytes)
	case len(d) < 4:
		return nil, errors.New("truncated")
	case string(d[:4]) == "ttcf":
		return nil, errors.New("font collections are not supported")
	}
	if err := checkSfnt(d); err != nil {
		return nil, err
	}
	ld, err := ot.NewLoader(bytes.NewReader(d))
	if err != nil {
		return nil, err
	}
	if !ld.HasTable(ot.MustNewTag("glyf")) || !ld.HasTable(ot.MustNewTag("cmap")) {
		return nil, errors.New("not a TrueType font with glyf outlines")
	}
	// The second result is a scratch buffer, not an error: a font without
	// readable names has an empty family and is rejected below.
	desc, _ := font.Describe(ld, nil)
	family := src.Family
	if family == "" {
		family = desc.Family
	}
	if family == "" || len(family) > maxFamilyBytes {
		return nil, errors.New("missing or oversized family name")
	}
	w := int(desc.Aspect.Weight)
	if w < 1 || w > 1000 {
		w = 400
	}
	return &Face{
		Family: family, Weight: w, Italic: desc.Aspect.Style == font.StyleItalic,
		norm: normFamily(family), data: d,
	}, nil
}

// NewFontSet validates the fonts and indexes them by family. Errors wrap
// ErrInvalidFont.
func NewFontSet(fonts []Source, g Generic, fallback []string) (*FontSet, error) {
	if len(fonts) > MaxFonts {
		return nil, fmt.Errorf("%w: %d fonts exceed %d", ErrInvalidFont, len(fonts), MaxFonts)
	}
	fs := &FontSet{byFamily: map[string][]*Face{}, generic: map[string]string{}}
	total := 0
	for i, src := range fonts {
		if total += len(src.Data); total > MaxTotalFontBytes {
			return nil, fmt.Errorf("%w: fonts exceed %d bytes in total", ErrInvalidFont, MaxTotalFontBytes)
		}
		f, err := describe(src)
		if err != nil {
			return nil, fmt.Errorf("%w: font %d: %w", ErrInvalidFont, i, err)
		}
		fs.faces = append(fs.faces, f)
		fs.byFamily[f.norm] = append(fs.byFamily[f.norm], f)
	}
	for _, kv := range []struct{ kw, fam, def string }{
		{"sans-serif", g.SansSerif, "Go"}, {"serif", g.Serif, "Go"}, {"monospace", g.Monospace, "Go Mono"},
	} {
		if kv.fam == "" {
			kv.fam = kv.def
		}
		fs.generic[kv.kw] = normFamily(kv.fam)
	}
	for _, fam := range fallback {
		fs.fallback = append(fs.fallback, normFamily(fam))
	}
	return fs, nil
}

// Faces returns the caller faces in the order given to NewFontSet.
func (fs *FontSet) Faces() []*Face { return fs.faces }

// genericKeywords maps every CSS generic family to one of the three
// configured families; those without a configured font (cursive, fantasy...)
// use sans-serif, as a browser with no such font does.
var genericKeywords = map[string]string{
	"sans-serif": "sans-serif", "system-ui": "sans-serif", "ui-sans-serif": "sans-serif",
	"ui-rounded": "sans-serif", "cursive": "sans-serif", "fantasy": "sans-serif",
	"math": "sans-serif", "emoji": "sans-serif", "fangsong": "sans-serif",
	"serif": "serif", "ui-serif": "serif",
	"monospace": "monospace", "ui-monospace": "monospace",
}

// lookupKey returns the index key of a family of a CSS list: generic keywords
// resolve to their configured family.
func (fs *FontSet) lookupKey(family string) string {
	if kw, ok := genericKeywords[strings.ToLower(strings.TrimSpace(family))]; ok {
		return fs.generic[kw]
	}
	return normFamily(family)
}

func (fs *FontSet) lookup(family string) []*Face {
	key := fs.lookupKey(family)
	if faces := fs.byFamily[key]; len(faces) > 0 {
		return faces
	}
	return builtin()[key]
}

// chain appends to dst the faces to try for spec, in order: one face per
// family of the list, then the fallback families, then the Go fonts. The first
// face is the primary.
func (fs *FontSet) chain(dst []*Face, families []string, weight int, italic bool) []*Face {
	add := func(faces []*Face) {
		if f := pick(faces, weight, italic); f != nil && !containsFace(dst, f) {
			dst = append(dst, f)
		}
	}
	for _, fam := range families {
		add(fs.lookup(fam))
	}
	for _, fam := range fs.fallback {
		add(fs.byFamily[fam])
		add(builtin()[fam])
	}
	add(builtin()[keyGo])
	add(builtin()[keyGoMono])
	return dst
}

func containsFace(faces []*Face, f *Face) bool {
	for _, g := range faces {
		if g == f {
			return true
		}
	}
	return false
}

// weightRank orders weights by the CSS Fonts 4 §5.2 matching rule: lower is
// better.
func weightRank(want, have int) int {
	asc, desc := have, 1000-have
	switch {
	case have == want:
		return 0
	case want >= 400 && want <= 500:
		switch {
		case have > want && have <= 500:
			return 1000 + asc
		case have < want:
			return 2000 + desc
		}
		return 3000 + asc
	case want < 400:
		if have < want {
			return 1000 + desc
		}
		return 2000 + asc
	}
	if have > want {
		return 1000 + asc
	}
	return 2000 + desc
}

// pick returns the face of faces closest to the requested style, then weight.
func pick(faces []*Face, weight int, italic bool) *Face {
	sameStyle := false
	for _, f := range faces {
		if f.Italic == italic {
			sameStyle = true
			break
		}
	}
	var best *Face
	bestRank := 0
	for _, f := range faces {
		if sameStyle && f.Italic != italic {
			continue
		}
		if r := weightRank(weight, f.Weight); best == nil || r < bestRank {
			best, bestRank = f, r
		}
	}
	return best
}
