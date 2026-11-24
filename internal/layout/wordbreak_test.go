package layout

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glimps-re/htmltopdf/internal/budget"

	"github.com/glimps-re/htmltopdf/internal/text"
)

func TestAppendBreakAll(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		base     []text.Break
		want     []int32
	}{
		{"letters", "abcd", nil, []int32{1, 2, 3}},
		{"digits and letters", "a1b", nil, []int32{1, 2}},
		{"underscore breaks like a letter", "a_b", nil, []int32{1, 2}},
		{"separators", "a.b", nil, []int32{2}},
		{"nothing before a separator", "a:b/c", nil, []int32{2, 4}},
		{"hyphen", "a-b", nil, []int32{1, 2}},
		{"backslash only breaks before", "a\\b", nil, []int32{1}},
		{"quotes", "a\"b", nil, nil},
		{"parentheses", "a(b)c", nil, []int32{1, 4}},
		{"space from the base", "ab cd", []text.Break{{Offset: 3}}, []int32{1, 3, 4}},
		{"decomposed accent stays whole", "e\u0301e\u0301", nil, []int32{3}},
		{"mark after a space or punctuation", " \u0301a.\u0301b", nil, []int32{7}},
		{"lone mark first", "\u0301a", nil, nil},
		{"zwj", "a\u200db", nil, nil},
		{"hangul jamo", "\u1100\u1161\u11a8\u1100\u1161", nil, []int32{9}},
		{"virama conjunct", "\u0915\u094d\u0937\u0915", nil, []int32{9}},
		{"multibyte", "éé", nil, []int32{2}},
		{"invalid utf-8", "a\xffb", nil, nil},
		{"base breaks are kept once", "ab", []text.Break{{Offset: 1}}, []int32{1}},
		{"empty", "", nil, nil},
	} {
		got := appendBreakAll(nil, tc.in, tc.base)
		if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Lines of a monospace box three characters wide under word-break: break-all,
// as Chrome gives them.
func TestBreakAllMatchesChrome(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"a_b_c_d_e", []string{"a_b", "_c_", "d_e"}},
		{"ab:cd:ef:gh", []string{"ab:", "cd:", "ef:", "gh"}},
		{"a/b/c/d/e/f", []string{"a/", "b/", "c/", "d/", "e/f"}},
		{"a.b.c.d.e.f", []string{"a.", "b.", "c.", "d.", "e.f"}},
		{"a-b-c-d-e-f", []string{"a-b", "-c-", "d-e", "-f"}},
		{"C:\\Users\\x\\y", []string{"C:\\U", "ser", "s\\x", "\\y"}},
		{"1,234,567,890", []string{"1,2", "34,", "56", "7,8", "90"}},
		{"abc(def)ghi", []string{"abc", "(de", "f)g", "hi"}},
		{"ab\"cd\"ef\"gh", []string{"a", "b\"c", "d\"e", "f\"g", "h"}},
	} {
		html := `<div style="width:30px;font-family:monospace;font-size:16px;word-break:break-all">` + strings.ReplaceAll(tc.in, `"`, "&quot;") + `</div>`
		got := lineTexts(newRun(t, html, opts{css: smallPage + " body{margin:0}"}).all(t))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.in, got, tc.want)
		}
	}
}

// lineTexts returns the text of every line of the pages.
func lineTexts(pages []*Page) []string {
	var out []string
	var walk func(f *frag)
	walk = func(f *frag) {
		for _, c := range f.children {
			walk(c)
		}
		for _, ln := range f.lines {
			var sb strings.Builder
			for _, it := range ln.items {
				if it.text != nil {
					sb.WriteString(it.text.Text)
				}
			}
			out = append(out, sb.String())
			for _, it := range ln.items {
				if it.atom != nil {
					walk(it.atom.f)
				}
			}
		}
	}
	for _, pg := range pages {
		walk(pg.root)
	}
	return out
}

func wrapPages(t *testing.T, decl string) []*Page {
	t.Helper()
	html := `<div style="width:100px;font-size:16px;` + decl + `">Hello wonderfulworld_of/paths:x</div>`
	return newRun(t, html, opts{css: smallPage + " body{margin:0}"}).all(t)
}

func TestWordWrapKeywords(t *testing.T) {
	normal := lineTexts(wrapPages(t, ""))
	breakWord := lineTexts(wrapPages(t, "overflow-wrap:break-word"))
	anywhere := lineTexts(wrapPages(t, "overflow-wrap:anywhere"))
	breakAll := lineTexts(wrapPages(t, "word-break:break-all"))
	if !reflect.DeepEqual(breakWord, anywhere) {
		t.Errorf("anywhere differs from break-word:\n%q\n%q", anywhere, breakWord)
	}
	if reflect.DeepEqual(normal, breakWord) {
		t.Errorf("break-word changed nothing: %q", normal)
	}
	wantAll := []string{"Hello wonderf", "ulworld_of/pat", "hs:x"}
	if !reflect.DeepEqual(breakAll, wantAll) {
		t.Errorf("break-all lines %q, want %q", breakAll, wantAll)
	}
	wantWord := []string{"Hello", "wonderfulworl", "d_of/paths:x"}
	if !reflect.DeepEqual(breakWord, wantWord) {
		t.Errorf("break-word lines %q, want %q", breakWord, wantWord)
	}
}

func TestWordWrapGoldens(t *testing.T) {
	golden(t, "wrap-break-word", wrapPages(t, "overflow-wrap:break-word"))
	golden(t, "wrap-anywhere", wrapPages(t, "overflow-wrap:anywhere"))
	golden(t, "wrap-break-all", wrapPages(t, "word-break:break-all"))
}

func TestBreakAllIsInheritedAndDoesNotApplyToPre(t *testing.T) {
	word := "abcdefghijklmnopqrstuvwxyz"
	r := newRun(t, `<div style="width:100px;word-break:break-all"><p>`+word+`</p></div><pre style="width:100px;word-break:break-all">`+word+`</pre>`, opts{css: smallPage + " body{margin:0}"})
	lines := lineTexts(r.all(t))
	whole := 0
	for _, ln := range lines {
		if ln == word {
			whole++
		}
	}
	if whole != 1 || len(lines) < 3 {
		t.Fatalf("the paragraph must break (inherited) and the pre must not: %q", lines)
	}
}

func TestHugeWordsAreBrokenInLinearTime(t *testing.T) {
	if testing.Short() {
		t.Skip("2 MB words")
	}
	for name, decl := range map[string]string{
		"anywhere":           "overflow-wrap:anywhere",
		"break-word":         "overflow-wrap:break-word",
		"break-all":          "word-break:break-all",
		"break-all+anywhere": "word-break:break-all;overflow-wrap:anywhere",
	} {
		for _, ch := range []string{"a", "_"} {
			t.Run(name+"/"+ch, func(t *testing.T) {
				html := `<div style="width:200px;` + decl + `">` + strings.Repeat(ch, 1_900_000) + `</div>`
				start := time.Now()
				r := newRun(t, html, opts{css: smallPage + " body{margin:0}", limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
				var err error
				for err == nil {
					_, err = r.pag.Next()
				}
				var le *budget.LimitError
				if !errors.Is(err, io.EOF) && !errors.As(err, &le) {
					t.Fatal(err)
				}
				if d := time.Since(start); d > 10*time.Second && !raceEnabled {
					t.Errorf("took %v", d)
				}
			})
		}
	}
}

// One 1 MiB word under overflow-wrap: anywhere ends in 2 s or on MaxWork: the
// limits stop it, not the context. Quadratic line breaking took minutes.
func TestMiBWordUnderAnywhereIsStoppedByTheLimitsNotTheClock(t *testing.T) {
	html := `<div style="width:200px;overflow-wrap:anywhere">` + strings.Repeat("a", 1<<20) + `</div>`
	start := time.Now()
	r := newRun(t, html, opts{css: smallPage + " body{margin:0}", limits: func(l *budget.Limits) { l[budget.Pages] = 100000 }})
	var err error
	for err == nil {
		_, err = r.pag.Next()
	}
	var le *budget.LimitError
	if !errors.Is(err, io.EOF) && (!errors.As(err, &le) || le.Kind != budget.Work) {
		t.Fatalf("stopped by %v, want completion or LimitWork", err)
	}
	if d := time.Since(start); d > 2*time.Second && !raceEnabled {
		t.Errorf("took %v", d)
	}
}
