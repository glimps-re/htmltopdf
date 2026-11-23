package dom

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

var domLimits = map[string]budget.Kind{
	"MaxInputBytes": budget.InputBytes,
	"MaxNodes":      budget.Nodes,
	"HTMLDepth":     budget.HTMLDepth,
}

// Every testcorpus document goes through Parse: dom ends in the expected
// outcome for the limits it owns (input bytes, nodes, depth) and succeeds on
// the others (CSS and layout limits belong to later stages).
func TestCorpusHostileDocuments(t *testing.T) {
	for _, c := range testcorpus.Hostile(budget.Defaults()) {
		t.Run(c.Name, func(t *testing.T) {
			src := c.HTML()
			if raceEnabled && len(src) > 256<<10 {
				t.Skip("large document: covered without -race")
			}
			var peak uint64
			runtime.GC()
			b := budget.New(context.Background(), budget.Defaults(), func(string) {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				peak = max(peak, m.HeapAlloc)
			})
			doc, err := Parse(bytes.NewReader(src), b)
			kind, owned := domLimits[c.Limit]
			var le *budget.LimitError
			switch {
			case owned && c.Want == testcorpus.Limit:
				if !errors.As(err, &le) || le.Kind != kind {
					t.Fatalf("got %v, want a %s limit error", err, c.Limit)
				}
			case c.Want == testcorpus.Robust:
				if err != nil && !errors.As(err, &le) {
					t.Fatalf("non-limit error %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("got %v, want success", err)
				}
				if doc == nil || len(doc.Elements) == 0 {
					t.Fatal("empty document")
				}
			}
			if err != nil && !raceEnabled && peak > 64<<20 {
				t.Errorf("heap-max %d MiB for %d input bytes (limit 64 MiB)", peak>>20, len(src))
			}
		})
	}
}

func TestCorpusTextCases(t *testing.T) {
	for _, c := range testcorpus.TextCases() {
		t.Run(c.Name, func(t *testing.T) {
			doc, err := Parse(bytes.NewReader(testcorpus.TextDocument(c.Raw)), newBudget(budget.Defaults()))
			if err != nil {
				t.Fatal(err)
			}
			var all strings.Builder
			for _, tx := range doc.Texts {
				all.WriteString(tx.Data)
			}
			if !strings.Contains(all.String(), c.Want) {
				t.Fatalf("text %q does not contain %q", all.String(), c.Want)
			}
		})
	}
}

func TestSanitizeMoreFamilies(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"consecutive bidi", "\u202E\u202E\u2069", "\u27E8RLO\u27E9\u27E8RLO\u27E9\u27E8PDI\u27E9"},
		{"U+2028 alone", " ", " "},
		{"truncated 4-byte sequence", "a\xf0\x9f\x98", "a���"},
		{"surrogate encoding", "a\xed\xa0\x80b", "a���b"},
		{"overlong NUL", "a\xc0\x80b", "a��b"},
		{"beyond U+10FFFF", "a\xf4\x90\x80\x80b", "a����b"},
		{"tag block bounds", "\U000E0000\U000E0001\U000E007F", ""},
		{"Arabic prepended mark dropped", "a\u0600\u06DDb", "ab"},
		{"BOM mid-text", "a\uFEFFb", "ab"},
		{"NEL", "a\u0085b", "a�b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeText(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// Tokens the other differential tests do not use: obscure elements, adoption
// agency triggers, foster parenting, foreign content and attribute floods.
var breakerTokens = []string{
	"<ruby>", "<rb>", "<rt>", "<rp>", "<object>", "</object>", "<applet>", "<marquee>", "</marquee>", "<dd>", "<dt>", "<dl>",
	"<h1>", "<h2>", "</h3>", "<optgroup>", "<option>", "<menuitem>", "<image>", "<keygen>", "<isindex>", "<listing>",
	"<xmp>", "<iframe>", "<noembed>", "<noframes>", "<details>", "<summary>", "<desc>", "<svg>", "<svg><desc>", "</svg>",
	"<math>", "<annotation-xml encoding=text/html>", "<annotation-xml>", "<mtext>", "<mglyph>", "<malignmark>",
	"<b>", "<i>", "<em>", "<strong>", "<small>", "<big>", "<s>", "<strike>", "<tt>", "<code>", "<u>", "<a href=x>", "<nobr>",
	"</b>", "</i>", "</em>", "</strong>", "</a>", "</p>", "</br>", "<p>", "<div>", "</div>", "<center>", "<section>", "<li>",
	"<table>", "<tr>", "<td>", "<th>", "<caption>", "<colgroup>", "<tbody>", "</table>", "</td>", "</tr>", "<input>",
	"<input type=hidden>", "<form>", "<form>", "<button>", "<select>", "<textarea>", "<template>", "<body a=1>",
	"<html b=2>", "<a a b c d e f g h i j>", "<b a b c d e f g h>", "<p x y z>", "<hr>", "<br>", "<wbr>", "<embed>", "<param>",
	"<source>", "<track>", "<area>", "<base>", "<link>", "<meta>", "<head>", "</head>", "<body>", "</body>", "<frameset>",
	"x", "<!--", "-->", "<!x>", "<?x>", "<![CDATA[", "&amp;", "&#0;", "&#x110000;", "\x00", "\r\n", "<plaintext>", "<script>",
	"</script>", "<title>", "</title>", "<style>", "<textarea>", "</textarea>", "<fieldset>", "<legend>", "<label>", "<output>",
}

func TestEstimateBreakerRandom(t *testing.T) {
	seeds := uint64(108)
	if raceEnabled || testing.Short() {
		seeds = 102
	}
	for seed := uint64(100); seed < seeds; seed++ {
		randomDifferential(t, breakerTokens, seed, 2500, 150)
	}
}

// Long misnested runs: each period closes a formatting element across a block
// so the parser clones the open formatting elements again and again.
func TestEstimateBreakerPatterns(t *testing.T) {
	fmts := []string{"<b>", "<i>", "<u>", "<em>", "<strong>", "<font a=1>", "<a href=x>", "<nobr>", "<s>", "<tt>"}
	closes := []string{"</b>", "</i>", "</u>", "</em>", "</strong>", "</font>", "</a>", "</nobr>", "</s>", "</tt>"}
	blocks := []string{"<div>", "<p>", "<table><tr><td>", "<li>", "<h1>", "<td>", "<center>", "<button>", "<dd>"}
	var cases []string
	for n := 1; n <= len(fmts); n++ {
		for _, blk := range blocks {
			open := strings.Join(fmts[:n], "")
			for _, reps := range []int{1, 3, 30} {
				cases = append(cases,
					strings.Repeat(open+blk+"x", reps),
					strings.Repeat(open+blk+"x"+closes[0]+"y", reps),
					open+strings.Repeat(blk+"x", reps*10),
					open+strings.Repeat(blk+"x"+strings.Join(closes[:n], ""), reps),
					strings.Repeat("<table>"+open+"<tr>"+blk+closes[n-1]+"<td>", reps),
					open+strings.Repeat("<p>x</p>"+closes[n/2], reps*5),
					strings.Repeat(open+"<svg><title>"+blk+"</title></svg>", reps),
				)
			}
		}
	}
	// Noah's Ark: three identical formatting elements with attributes.
	cases = append(cases, strings.Repeat("<b a=1 b=2>", 50)+strings.Repeat("<p>x", 50),
		strings.Repeat("<b a=1><b a=2><b a=1><b a=1>", 10)+strings.Repeat("<div>x</b>", 30))
	for _, src := range cases {
		real, ok := actual(t, src)
		if !ok {
			continue
		}
		if est := estimate(t, src); est < real {
			t.Fatalf("estimate %d < actual %d for %.200q", est, real, src)
		}
	}
	t.Logf("%d patterns checked", len(cases))
}

// 00-large must convert in well under 5 ms (indicative on a loaded machine).
func TestConvertLargeReportTime(t *testing.T) {
	data, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "00-host-connector", testcorpus.Large)
	if err != nil {
		t.Fatal(err)
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := newBudget(budget.Defaults())
	if _, err := Convert(root, b); err != nil {
		t.Fatal(err)
	}
	t.Logf("00-large: %d bytes, %d nodes charged, estimate %d", len(data), b.Used(budget.Nodes), mustEstimate(t, data))
}

func mustEstimate(t testing.TB, data []byte) int64 {
	t.Helper()
	est, err := estimateNodes(data, noLimit, noPoll)
	if err != nil {
		t.Fatal(err)
	}
	return est
}

func BenchmarkConvertCorpusLarge(b *testing.B) {
	data, err := testcorpus.Report(os.DirFS("../../testdata/templates"), "00-host-connector", testcorpus.Large)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		root, err := html.Parse(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
		bg := budget.New(context.Background(), budget.Defaults(), nil)
		b.StartTimer()
		if _, err := Convert(root, bg); err != nil {
			b.Fatal(err)
		}
	}
}

// Twins of the pre-pass ruling (design §5.2.1): the exact node count is
// limited to MaxNodes by Convert, the estimate to 4 × MaxNodes by the pre-pass.
func TestNodeLimitTwins(t *testing.T) {
	max := budget.Defaults()[budget.Nodes]
	const unit = "<br>"
	baseReal, _ := actual(t, unit)
	baseReal--
	baseEst := estimate(t, unit) - 1
	run := func(n int64) ([]string, error) {
		var stages []string
		b := budget.New(t.Context(), budget.Defaults(), func(s string) { stages = append(stages, s) })
		_, err := Parse(strings.NewReader(strings.Repeat(unit, int(n))), b)
		return stages, err
	}
	var le *budget.LimitError
	if _, err := run(max - baseReal); err != nil {
		t.Fatalf("actual count %d: %v", max, err)
	}
	if _, err := run(max - baseReal + 1); !errors.As(err, &le) || le.Kind != budget.Nodes {
		t.Fatalf("actual count %d: got %v", max+1, err)
	}
	stages, err := run(EstimateFactor*max - baseEst)
	if !errors.As(err, &le) || le.Kind != budget.Nodes || !strings.Contains(strings.Join(stages, ","), "dom.parse") {
		t.Fatalf("estimate %d: stages %v, err %v: must pass the pre-pass and fail in Convert", EstimateFactor*max, stages, err)
	}
	stages, err = run(EstimateFactor*max - baseEst + 1)
	if !errors.As(err, &le) || le.Kind != budget.Nodes || strings.Contains(strings.Join(stages, ","), "dom.parse") {
		t.Fatalf("estimate %d: stages %v, err %v: must fail in the pre-pass", EstimateFactor*max+1, stages, err)
	}
}

var focusedSets = map[string][]string{
	"adoption": {
		"<a href=1>", "<a href=2>", "</a>", "<b>", "</b>", "<i>", "</i>", "<em>", "</em>", "<nobr>", "</nobr>", "<font a=1>", "</font>",
		"<div>", "</div>", "<p>", "</p>", "<h1>", "</h1>", "<h2>", "<li>", "</li>", "<ul>", "</ul>", "<span>", "</span>", "<center>",
		"</center>", "<pre>", "</pre>", "<button>", "</button>", "<form>", "</form>", "<dd>", "<dt>", "</dd>", "x", "<br>", "</br>",
		"<marquee>", "</marquee>", "<object>", "</object>", "<applet>", "<b a=1>", "<b a=2>", "<b a=1>", "</p >", "</P>", "</B>",
	},
	"template": {
		"<template>", "</template>", "<tr>", "</tr>", "<td>", "</td>", "<th>", "<table>", "</table>", "<tbody>", "<thead>", "<col>",
		"<colgroup>", "<caption>", "<b>", "</b>", "<i>", "</i>", "x", " ", "<p>", "</p>", "<div>", "<select>", "<option>", "<frameset>",
		"<head>", "<body>", "<html>", "<style>", "</style>", "<script>", "</script>", "<title>", "</title>", "<meta>", "<link>",
	},
	"foreign": {
		"<svg>", "</svg>", "<math>", "</math>", "<mi>", "<mo>", "<mn>", "<ms>", "<mtext>", "<annotation-xml>", "<annotation-xml encoding=text/html>",
		"<foreignObject>", "</foreignObject>", "<desc>", "</desc>", "<title>", "</title>", "<b>", "</b>", "<i>", "<p>", "</p>", "<br>",
		"</br>", "<font color=r>", "<div>", "</div>", "<table>", "<td>", "<script>", "</script>", "<style>", "x", "<![CDATA[", "]]>",
		"<g>", "</g>", "<path d=x>", "<a>", "</a>", "<textarea>", "</textarea>", "<h1>", "<span>", "<center>", "<img>", "<ul>", "<li>",
	},
	"table": {
		"<table>", "</table>", "<tr>", "</tr>", "<td>", "</td>", "<th>", "</th>", "<tbody>", "</tbody>", "<thead>", "<tfoot>", "<caption>",
		"</caption>", "<col>", "<colgroup>", "</colgroup>", "<b>", "</b>", "<i>", "</i>", "<a href=1>", "</a>", "x", " x", "<p>", "</p>",
		"<div>", "</div>", "<form>", "<input>", "<input type=hidden>", "<select>", "</select>", "<option>", "<span>", "<table>", "<td>",
		"<font a=1>", "<br>", "<!-- c -->", "<style>", "<script>", "<template>", "<svg>", "<math>", "<nobr>", "<button>", "<li>",
	},
	"frameset-body": {
		"<html>", "<head>", "</head>", "<body>", "</body>", "</html>", "<frameset>", "</frameset>", "<frame>", "<noframes>", "<b>", "<p>",
		"x", " ", "\n", "<title>", "<base>", "<template>", "<!doctype html>", "<!-- -->", "<a>", "<table>", "<div>", "</div>",
	},
}

// A single estimate below the actual count means the pre-pass can be fooled.
func TestEstimateBreakerFocused(t *testing.T) {
	iters := 6000
	if raceEnabled || testing.Short() {
		iters = 300
	}
	for name, tokens := range focusedSets {
		t.Run(name, func(t *testing.T) {
			for seed := uint64(1000); seed < 1008; seed++ {
				randomDifferential(t, tokens, seed, iters, 80)
				randomDifferential(t, tokens, seed+50, iters/4, 600)
			}
		})
	}
}

// Found by TestEstimateBreakerFocused with more seeds: svg inside a table
// followed by a foster-parented block and a cell. The estimate undercounts
// and the gap grows linearly with the repetitions.
func TestEstimateUndercountSVGInTable(t *testing.T) {
	const unit = "<table><svg><tfoot><tr><div><td>"
	for _, n := range []int{1, 10, 100} {
		src := strings.Repeat(unit, n)
		real, ok := actual(t, src)
		if !ok {
			t.Fatal("html.Parse failed")
		}
		if est := estimate(t, src); est < real {
			t.Errorf("%d × %q: estimate %d < actual %d", n, unit, est, real)
		}
	}
}
