package text

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/go-text/typesetting/harfbuzz"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

var referenceFiles = []string{
	"LiberationSans-Regular.ttf", "LiberationSans-Bold.ttf", "LiberationSans-Italic.ttf",
	"LiberationSans-BoldItalic.ttf", "DejaVuSans.ttf", "DejaVuSans-Bold.ttf", "DejaVuSansMono.ttf",
}

func referenceSources(tb testing.TB) []Source {
	tb.Helper()
	root, err := os.OpenRoot(filepath.Join("..", "..", "testdata", "fonts"))
	if err != nil {
		tb.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			tb.Error(err)
		}
	}()
	var out []Source
	for _, name := range referenceFiles {
		data, err := fs.ReadFile(root.FS(), name)
		if err != nil {
			tb.Fatal(err)
		}
		out = append(out, Source{Data: data})
	}
	return out
}

func referenceSet(tb testing.TB) *FontSet {
	tb.Helper()
	fs, err := NewFontSet(referenceSources(tb),
		Generic{SansSerif: "Liberation Sans", Monospace: "DejaVu Sans Mono"}, []string{"DejaVu Sans"})
	if err != nil {
		tb.Fatal(err)
	}
	return fs
}

func newBudget(glyphs int64, hook func(string)) *budget.Budget {
	l := budget.Defaults()
	l[budget.Glyphs] = glyphs
	return budget.New(context.Background(), l, hook)
}

func newShaper(tb testing.TB) (*Shaper, *budget.Budget) {
	tb.Helper()
	b := newBudget(budget.Defaults()[budget.Glyphs], nil)
	return NewShaper(referenceSet(tb), b), b
}

var sans = FontSpec{Families: []string{"'Segoe UI'", "Arial", "sans-serif"}, Weight: 400, Size: 16}

func TestFontSetFamilies(t *testing.T) {
	fs := referenceSet(t)
	got := map[string]int{}
	for _, f := range fs.Faces() {
		got[f.Family]++
		if f.UnitsPerEm() == 0 {
			t.Errorf("%s: no upem", f.Family)
		}
	}
	want := map[string]int{"Liberation Sans": 4, "DejaVu Sans": 2, "DejaVu Sans Mono": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("family %q: %d faces, want %d (all: %v)", k, got[k], v, got)
		}
	}
}

func TestFamilyOverride(t *testing.T) {
	src := referenceSources(t)[0]
	src.Family = "My Font"
	fs, err := NewFontSet([]Source{src}, Generic{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := fs.chain(nil, []string{"my font"}, 400, false); got[0].Family != "My Font" {
		t.Fatalf("chain = %v", got[0].Family)
	}
}

func TestMatching(t *testing.T) {
	fs := referenceSet(t)
	tests := []struct {
		families []string
		weight   int
		italic   bool
		family   string
		face     int // wanted weight
		italicF  bool
	}{
		{[]string{"Liberation Sans"}, 400, false, "Liberation Sans", 400, false},
		{[]string{"Liberation Sans"}, 600, false, "Liberation Sans", 700, false},
		{[]string{"Liberation Sans"}, 500, false, "Liberation Sans", 400, false},
		{[]string{"Liberation Sans"}, 100, false, "Liberation Sans", 400, false},
		{[]string{"Liberation Sans"}, 900, false, "Liberation Sans", 700, false},
		{[]string{"Liberation Sans"}, 700, true, "Liberation Sans", 700, true},
		{[]string{"liberation  sans"}, 400, true, "Liberation Sans", 400, true},
		{[]string{"DejaVu Sans Mono"}, 700, true, "DejaVu Sans Mono", 400, false},
		{[]string{"nope", "monospace"}, 400, false, "DejaVu Sans Mono", 400, false},
		{[]string{"Segoe UI", "Arial", "sans-serif"}, 400, false, "Liberation Sans", 400, false},
		{[]string{"serif"}, 400, false, "Go", 400, false},
		{[]string{"nope"}, 400, false, "DejaVu Sans", 400, false},
	}
	for _, tt := range tests {
		c := fs.chain(nil, tt.families, tt.weight, tt.italic)
		if f := c[0]; f.Family != tt.family || f.Weight != tt.face || f.Italic != tt.italicF {
			t.Errorf("%v w%d i%v → %s %d italic=%v", tt.families, tt.weight, tt.italic, f.Family, f.Weight, f.Italic)
		}
	}
	last := fs.chain(nil, nil, 400, false)
	if n := len(last); last[n-1].Family != "Go Mono" || last[n-2].Family != "Go" {
		t.Errorf("Go fonts not last: %v", last)
	}
}

func TestPickWeight(t *testing.T) {
	mk := func(ws ...int) []*Face {
		var out []*Face
		for _, w := range ws {
			out = append(out, &Face{Weight: w})
		}
		return out
	}
	tests := []struct {
		have []int
		want int
		got  int
	}{
		{[]int{300, 700}, 400, 300},
		{[]int{300, 600, 700}, 400, 300},
		{[]int{300, 450, 700}, 400, 450},
		{[]int{300, 450, 700}, 500, 450},
		{[]int{300, 800}, 500, 300},
		{[]int{200, 300, 800}, 350, 300},
		{[]int{200, 300, 800}, 100, 200},
		{[]int{200, 300, 800}, 900, 800},
		{[]int{200, 600, 800}, 700, 800},
		{[]int{200, 600}, 700, 600},
		{[]int{400}, 700, 400},
	}
	for _, tt := range tests {
		if got := pick(mk(tt.have...), tt.want, false).Weight; got != tt.got {
			t.Errorf("want %d among %v: got %d, expected %d", tt.want, tt.have, got, tt.got)
		}
	}
	if pick(nil, 400, false) != nil {
		t.Error("pick on nothing")
	}
}

func TestShapeFallbackKeepsPrimary(t *testing.T) {
	s, b := newShaper(t)
	text := "Go ↳ end"
	runs, err := s.Shape(text, sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fam []string
	for _, r := range runs {
		fam = append(fam, r.Face.Family)
	}
	if strings.Join(fam, "|") != "Liberation Sans|DejaVu Sans|Liberation Sans" {
		t.Fatalf("runs from %v", fam)
	}
	if dv := runs[1]; len(dv.Glyphs) != 1 || dv.Glyphs[0].ID == 0 || dv.Glyphs[0].Cluster != 3 {
		t.Fatalf("arrow run = %+v", dv)
	}
	if ws, _ := b.Warnings(); len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	var sum float32
	for _, r := range runs {
		sum += r.Advance
	}
	if sum < 40 || sum > 120 {
		t.Fatalf("total advance %v px for %q", sum, text)
	}
}

func TestShapeWeight600(t *testing.T) {
	s, _ := newShaper(t)
	spec := sans
	spec.Weight = 600
	runs, err := s.Shape("Title", spec, nil)
	if err != nil || len(runs) != 1 || runs[0].Face.Family != "Liberation Sans" || runs[0].Face.Weight != 700 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}

func TestMissingGlyph(t *testing.T) {
	b := newBudget(1000, nil)
	s := NewShaper(func() *FontSet { fs, _ := NewFontSet(nil, Generic{}, nil); return fs }(), b)
	runs, err := s.Shape("a↳\n 日", FontSpec{Families: []string{"sans-serif"}, Size: 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := b.Warnings()
	if len(ws) != 2 || ws[0].Code != budget.WarnMissingGlyph || ws[0].Detail != "U+21B3" || ws[1].Detail != "U+65E5" {
		t.Fatalf("warnings = %+v", ws)
	}
	if runs[0].Face.Family != "Go" || runs[0].Glyphs[1].ID != 0 {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestShapeClusters(t *testing.T) {
	s, _ := newShaper(t)
	tests := []struct {
		text string
		want []int32
	}{
		{"abc", []int32{0, 1, 2}},
		{"aé b", []int32{0, 1, 3, 4}},
		{"éx", []int32{0, 3}},
		{"a😀b", []int32{0, 1, 5}},
	}
	for _, tt := range tests {
		runs, err := s.Shape(tt.text, sans, nil)
		if err != nil {
			t.Fatal(err)
		}
		var got []int32
		for _, r := range runs {
			for _, g := range r.Glyphs {
				if len(got) == 0 || got[len(got)-1] != g.Cluster {
					got = append(got, g.Cluster)
				}
			}
		}
		if len(got) < 2 || got[0] != 0 {
			t.Errorf("%q: clusters %v", tt.text, got)
			continue
		}
		for _, w := range tt.want {
			found := false
			for _, g := range got {
				found = found || g == w
			}
			if !found && tt.text != "a😀b" && tt.text != "éx" {
				t.Errorf("%q: cluster %d missing in %v", tt.text, w, got)
			}
		}
	}
}

func TestShapeInvalidUTF8(t *testing.T) {
	s, _ := newShaper(t)
	runs, err := s.Shape("a\xffb", sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	var cl []int32
	for _, r := range runs {
		for _, g := range r.Glyphs {
			cl = append(cl, g.Cluster)
		}
	}
	if len(cl) != 3 || cl[0] != 0 || cl[1] != 1 || cl[2] != 2 {
		t.Fatalf("clusters = %v", cl)
	}
}

func TestShapeInvalidSpec(t *testing.T) {
	s, _ := newShaper(t)
	for _, size := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1)), MaxFontSize * 2} {
		spec := sans
		spec.Size = size
		if _, err := s.Shape("a", spec, nil); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("size %v: %v", size, err)
		}
	}
	spec := sans
	spec.Size = MaxFontSize
	if _, err := s.Shape("a", spec, nil); err != nil {
		t.Errorf("max size: %v", err)
	}
	if runs, err := s.Shape("", sans, nil); err != nil || runs != nil {
		t.Errorf("empty text: %v %v", runs, err)
	}
}

func TestGlyphLimit(t *testing.T) {
	const limit = 1000
	text := strings.Repeat("a", limit)
	for _, tt := range []struct {
		name string
		text string
		ok   bool
	}{
		{"just under", text[:limit-1], true},
		{"at the limit", text, true},
		{"over", text + "a", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := NewShaper(referenceSet(t), newBudget(limit, nil))
			runs, err := s.Shape(tt.text, sans, nil)
			var le *budget.LimitError
			if tt.ok != (err == nil) || (!tt.ok && (!errors.As(err, &le) || le.Kind != budget.Glyphs || len(runs) != 0)) {
				t.Fatalf("err = %v, runs = %d", err, len(runs))
			}
		})
	}
}

func heapMax(t *testing.T, glyphs int64, word string) (max uint64, runs []Run, err error) {
	t.Helper()
	fs := referenceSet(t)
	if _, err := NewShaper(fs, newBudget(10, nil)).Shape("a", sans, nil); err != nil {
		t.Fatal(err)
	}
	defer debug.SetGCPercent(debug.SetGCPercent(100))
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	hook := func(string) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if d := m.HeapAlloc - min(m.HeapAlloc, base.HeapAlloc); d > max {
			max = d
		}
	}
	s := NewShaper(fs, newBudget(glyphs, hook))
	runs, err = s.Shape(word, sans, nil)
	return max, runs, err
}

func TestHugeWordHeap(t *testing.T) {
	n := int(budget.Defaults()[budget.Glyphs])
	word := strings.Repeat("a", n+1)
	max, runs, err := heapMax(t, int64(n), word)
	var le *budget.LimitError
	if !errors.As(err, &le) || le.Kind != budget.Glyphs || len(runs) != 0 || max > 48<<20 {
		t.Fatalf("over: err=%v runs=%d heap-max=%d MiB", err, len(runs), max>>20)
	}
	max, runs, err = heapMax(t, int64(n), word[:n])
	if err != nil || len(runs) != 1 || len(runs[0].Glyphs) != n {
		t.Fatalf("at the limit: err=%v runs=%d", err, len(runs))
	}
	if max > 48<<20 {
		t.Fatalf("heap-max = %d MiB, want <= 48", max>>20)
	}
	t.Logf("2M-glyph word: heap-max %d MiB", max>>20)
}

func TestChunking(t *testing.T) {
	s, _ := newShaper(t)
	text := strings.Repeat("hello world, ", 30)
	whole, err := s.Shape(text, sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.chunk = 8
	split, err := s.Shape(text, sans, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) != 1 || len(split) != 1 || len(whole[0].Glyphs) != len(split[0].Glyphs) {
		t.Fatalf("runs %d/%d glyphs %d/%d", len(whole), len(split), len(whole[0].Glyphs), len(split[0].Glyphs))
	}
	for i := range whole[0].Glyphs {
		if whole[0].Glyphs[i].ID != split[0].Glyphs[i].ID || whole[0].Glyphs[i].Cluster != split[0].Glyphs[i].Cluster {
			t.Fatalf("glyph %d differs", i)
		}
	}
}

func TestChunkEnd(t *testing.T) {
	s, _ := newShaper(t)
	s.chunk = 8
	tests := []struct {
		name string
		text string
		want int
	}{
		{"fits", "abcdefgh", 8},
		{"at the break", "abcd efghi", 5},
		{"no break: cluster boundary", "abcdefghijkl", 8},
		{"mark not split", "abcdefgh\u0301ijkl", 7},
		{"zwj not split", "abcdefg\u200d\U0001F600hi", 6},
	}
	for _, tt := range tests {
		r := []rune(tt.text)
		if got := s.chunkEnd(r, 0, len(r)); got != tt.want {
			t.Errorf("%s: cut at %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestShaperPanicFallsBackToNominal(t *testing.T) {
	s, b := newShaper(t)
	s.afterShape = func(*harfbuzz.Buffer) { panic("boom") }
	runs, err := s.Shape("Hé!", sans, nil)
	if err != nil || len(runs) != 1 || len(runs[0].Glyphs) != 3 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	for i, g := range runs[0].Glyphs {
		if g.ID == 0 || g.Advance <= 0 {
			t.Errorf("glyph %d = %+v", i, g)
		}
	}
	if runs[0].Glyphs[2].Cluster != 3 {
		t.Errorf("clusters: %+v", runs[0].Glyphs)
	}
	ws, _ := b.Warnings()
	if len(ws) != 1 || ws[0].Code != budget.WarnShapingFallback {
		t.Fatalf("warnings = %+v", ws)
	}
}

func TestBrokenFaceIsSkipped(t *testing.T) {
	bad := referenceSources(t)[0]
	bad = Source{Data: append([]byte(nil), bad.Data...)}
	fs, err := NewFontSet([]Source{bad}, Generic{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := fs.Faces()[0]
	f.once.Do(func() { f.err = errors.New("corrupt") })
	b := newBudget(100, nil)
	runs, err := NewShaper(fs, b).Shape("abc", FontSpec{Families: []string{f.Family}, Size: 10}, nil)
	if err != nil || runs[0].Face.Family != "Go" {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	if ws, _ := b.Warnings(); len(ws) != 1 || ws[0].Code != budget.WarnShapingFallback {
		t.Fatalf("warnings = %+v", ws)
	}
	if f.UnitsPerEm() != 0 {
		t.Error("broken face reports an em size")
	}
}

func TestContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := budget.New(ctx, budget.Defaults(), nil)
	_, err := NewShaper(referenceSet(t), b).Shape("abc", sans, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestExtents(t *testing.T) {
	s, _ := newShaper(t)
	f := s.fs.chain(nil, []string{"Liberation Sans"}, 400, false)[0]
	a, d, g := s.Extents(f, 100)
	if a < 80 || a > 100 || d < 15 || d > 30 || g < 0 || g > 20 {
		t.Fatalf("extents = %v %v %v", a, d, g)
	}
	broken := &Face{data: []byte("x")}
	if a, d, g := s.Extents(broken, 10); a != 0 || d != 0 || g != 0 {
		t.Fatal("broken face has extents")
	}
}

func TestConcurrentShapers(t *testing.T) {
	fs := referenceSet(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewShaper(fs, newBudget(100000, nil))
			for j := 0; j < 20; j++ {
				if _, err := s.Shape("Hello ↳ world", sans, nil); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkShapeCell(b *testing.B) {
	cells := map[string]string{
		"ascii-100B": "Detected as Win32.Trojan.Agent by 12 engines; see the report for details, hashes and the timeline.",
		"arrow-100B": "Detected as Win32.Trojan.Agent by 12 engines; \u21b3 see report for details and hashes of samples.",
	}
	for name, text := range cells {
		b.Run(name, func(b *testing.B) {
			s := NewShaper(referenceSet(b), newBudget(math.MaxInt64, nil))
			var runs []Run
			var brk []Break
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var err error
				if runs, err = s.Shape(text, sans, runs[:0]); err != nil {
					b.Fatal(err)
				}
				brk = Breaks(text, brk[:0])
			}
		})
	}
}

func BenchmarkNewFontSet(b *testing.B) {
	src := referenceSources(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewFontSet(src, Generic{}, nil); err != nil {
			b.Fatal(err)
		}
	}
	var fileBytes int
	for _, s := range src {
		fileBytes += len(s.Data)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fs, err := NewFontSet(src, Generic{}, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, f := range fs.Faces() {
		f.UnitsPerEm()
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(fileBytes)/(1<<20), "font-file-MiB")
	b.ReportMetric(float64(after.HeapAlloc-min(after.HeapAlloc, before.HeapAlloc))/(1<<20), "parsed-faces-MiB")
	runtime.KeepAlive(fs)
}

// The direct engine drive gives the glyphs of shaping.HarfbuzzShaper: ids,
// advances and clusters, whatever the text (kerning, ligatures, spaces the font
// lacks, marks, scripts it does not cover, emoji, controls).
func TestDirectShapingMatchesTheEngineWrapper(t *testing.T) {
	texts := []string{
		"Hello, World! AV To Ty fi ffi fl", "a b c d e　f", "é ạ̈ กิ้",
		"مرحبا بالعالم", "नमस्ते दुनिया", "日本語のテキスト 한국어", "😀👨‍👩‍👧 🇫🇷 \u200d\u200c", "tab\there\nnewline\x00nul\x7f", strings.Repeat("The quick brown fox. ", 200),
		"1234567890 ½ ⅓ ℃ ﬁ ﬂ", "ab\xc3\x28cd\xe2\x82", "\xf0\x9f\x98 x\xed\xa0\x80 y", "\U0010ffff\ufffd\uFEFF",
	}
	for _, chunk := range []int{0, 8, 3} {
		for _, spec := range []FontSpec{sans, {Families: []string{"serif"}, Weight: 700, Size: 13}, {Families: []string{"monospace"}, Weight: 400, Italic: true, Size: 7.5}} {
			for _, text := range texts {
				fs := referenceSet(t)
				direct, lib := NewShaper(fs, newBudget(1<<30, nil)), NewShaper(fs, newBudget(1<<30, nil))
				if chunk > 0 {
					direct.chunk, lib.chunk = chunk, chunk
				}
				lib.chunkFn = wrapperChunk()
				got, err := direct.Shape(text, spec, nil)
				if err != nil {
					t.Fatal(err)
				}
				want, err := lib.Shape(text, spec, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want) {
					t.Fatalf("%q: %d runs, want %d", text, len(got), len(want))
				}
				for i := range got {
					if got[i].Face != want[i].Face || got[i].Size != want[i].Size || len(got[i].Glyphs) != len(want[i].Glyphs) {
						t.Fatalf("%q run %d: %d glyphs, want %d", text, i, len(got[i].Glyphs), len(want[i].Glyphs))
					}
					for k := range got[i].Glyphs {
						if got[i].Glyphs[k] != want[i].Glyphs[k] {
							t.Fatalf("%q run %d glyph %d: %+v, want %+v", text, i, k, got[i].Glyphs[k], want[i].Glyphs[k])
						}
					}
				}
			}
		}
	}
}

// FuzzShape shapes arbitrary text with the direct engine drive and with the
// engine wrapper: same glyphs, and the glyph count and clusters stay in range.
func FuzzShape(f *testing.F) {
	for _, s := range []string{"Hello AV", "a b́", "مرحبا", "नमस्ते", "😀‍👨", "\x00\xff\xfe", strings.Repeat("ab ", 400)} {
		f.Add(s)
	}
	fs := referenceSet(f)
	f.Fuzz(func(t *testing.T, text string) {
		direct, lib := NewShaper(fs, newBudget(1<<24, nil)), NewShaper(fs, newBudget(1<<24, nil))
		lib.chunkFn = wrapperChunk()
		got, err1 := direct.Shape(text, sans, nil)
		want, err2 := lib.Shape(text, sans, nil)
		if (err1 == nil) != (err2 == nil) || len(got) != len(want) {
			t.Fatalf("%q: %v/%v, %d/%d runs", text, err1, err2, len(got), len(want))
		}
		for i := range got {
			if len(got[i].Glyphs) != len(want[i].Glyphs) {
				t.Fatalf("%q run %d: %d glyphs, want %d", text, i, len(got[i].Glyphs), len(want[i].Glyphs))
			}
			for k, g := range got[i].Glyphs {
				if g != want[i].Glyphs[k] || int(g.Cluster) > len(text) {
					t.Fatalf("%q run %d glyph %d: %+v, want %+v", text, i, k, g, want[i].Glyphs[k])
				}
			}
		}
	})
}

// The production fallbacks of the engine drive: a panic of the engine, a cluster
// outside the text, a negative one. Each gives the nominal glyphs, one warning,
// leaves the glyphs of the earlier chunks alone and does not spoil the next call.
func TestEngineFailuresFallBackToNominal(t *testing.T) {
	tests := map[string]func(*harfbuzz.Buffer){
		"panic":           func(*harfbuzz.Buffer) { panic("boom") },
		"cluster too big": func(b *harfbuzz.Buffer) { b.Info[len(b.Info)-1].Cluster = 1 << 20 },
		"negative":        func(b *harfbuzz.Buffer) { b.Info[0].Cluster = -1 },
	}
	for name, fail := range tests {
		t.Run(name, func(t *testing.T) {
			s, b := newShaper(t)
			s.chunk = 8
			text := "abc def ghi jkl mno pqr"
			calls := 0
			s.afterShape = func(buf *harfbuzz.Buffer) {
				if calls++; calls == 2 {
					fail(buf)
				}
			}
			runs, err := s.Shape(text, sans, nil)
			if err != nil || len(runs) != 1 || len(runs[0].Glyphs) != len(text) {
				t.Fatalf("runs %+v, %v", runs, err)
			}
			for i, g := range runs[0].Glyphs {
				if g.ID == 0 || g.Advance <= 0 || int(g.Cluster) != i {
					t.Fatalf("glyph %d = %+v", i, g)
				}
			}
			if ws, _ := b.Warnings(); len(ws) != 1 || ws[0].Code != budget.WarnShapingFallback || ws[0].Count != 1 {
				t.Errorf("warnings %+v", ws)
			}
			s.afterShape = nil
			again, err := s.Shape(text, sans, nil)
			if err != nil || len(again) != 1 || len(again[0].Glyphs) != len(text) {
				t.Fatalf("next call: %+v, %v", again, err)
			}
			plain, _ := newShaper(t)
			plain.chunk = 8
			want, _ := plain.Shape(text, sans, nil)
			if len(want) == 1 && len(again[0].Glyphs) == len(want[0].Glyphs) {
				for i := range want[0].Glyphs {
					if again[0].Glyphs[i].Advance != want[0].Glyphs[i].Advance {
						t.Fatalf("the next call is spoiled at glyph %d", i)
					}
				}
			}
		})
	}
}

func BenchmarkShapeLongWord(b *testing.B) {
	fs := referenceSet(b)
	word := strings.Repeat("a", 1<<20)
	b.ReportAllocs()
	b.SetBytes(int64(len(word)))
	for b.Loop() {
		s := NewShaper(fs, newBudget(1<<30, nil))
		if _, err := s.Shape(word, sans, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGlyphAdvanceMatchesShaping(t *testing.T) {
	s, _ := newShaper(t)
	runs, err := s.Shape("H", sans, nil)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs %v, err %v", runs, err)
	}
	g := runs[0].Glyphs[0]
	got := s.GlyphAdvance(runs[0].Face, g.ID, runs[0].Size)
	if d := got - g.Advance; d > 0.01 || d < -0.01 || got <= 0 {
		t.Fatalf("GlyphAdvance %v, shaped advance %v", got, g.Advance)
	}
	if got := s.GlyphAdvance(&Face{}, g.ID, 16); got != 0 {
		t.Fatalf("unusable face: %v", got)
	}
}
