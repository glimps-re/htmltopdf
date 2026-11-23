package dom

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const noLimit = int64(1) << 50

func noPoll() error { return nil }

// assertCovers fails unless the estimate of src is at least the real node cost
// of its parse. Inputs the parser rejects (depth) are skipped.
func assertCovers(t testing.TB, src string) {
	t.Helper()
	real, ok := actual(t, src)
	if !ok {
		return
	}
	if est := estimate(t, src); est < real {
		t.Fatalf("estimate %d < actual %d for %.200q (%d bytes)", est, real, src, len(src))
	}
}

func estimate(t testing.TB, src string) int64 {
	t.Helper()
	est, err := estimateNodes([]byte(src), noLimit, noPoll)
	if err != nil {
		t.Fatal(err)
	}
	return est
}

func actual(t testing.TB, src string) (int64, bool) {
	t.Helper()
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return 0, false
	}
	return countCost(root), true
}

func TestEstimateCoversActual(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"text":          "hello",
		"nested":        "<div><p>a</p><p>b</p></div>",
		"strayP":        "</p></p></p>",
		"strayBr":       "</br></br>",
		"table":         "<td>a<td>b<tr><td>c",
		"col":           "<col><col>",
		"formatting":    "<b><i><u><p>x<p>y<p>z",
		"misnest":       "<b><p>x</b>y<p>z</b>",
		"adoption":      "<a><div><a><div></a></a>",
		"deepAdoption":  "<b>" + strings.Repeat("<div>", 20) + "</b>x",
		"attrsClone":    `<b a=1 b=2 c=3><p>x<p>y`,
		"noah":          strings.Repeat("<b>", 10) + "x",
		"comment":       "<!-- a --><!doctype html><?php x ?>",
		"svgTitle":      "<svg><title><p><br><br></title></svg>",
		"svgBreakout":   "<svg><p><script><!-- </script><br><br><br>",
		"select":        "<select><option>a<option>b</select>",
		"templ":         "<template><td>x</template>",
		"formClose":     "<form><form><div></form>",
		"frameset":      "<frameset><frame><frame>",
		"textarea":      "<textarea><b></textarea><b>",
		"cdata":         "<svg><![CDATA[<br><br>]]></svg><br>",
		"svgInTable":    "<table><svg><tfoot><tr><div><td>",
		"svgInTable100": strings.Repeat("<table><svg><tfoot><tr><div><td>", 100),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) { assertCovers(t, src) })
	}
}

func TestEstimateStopsAtLimit(t *testing.T) {
	src := strings.Repeat("<br>", 1000)
	est, err := estimateNodes([]byte(src), 100, noPoll)
	if err != nil {
		t.Fatal(err)
	}
	if est <= 100 || est > 120 {
		t.Fatalf("estimate = %d, want just above the limit of 100", est)
	}
}

func TestEstimatePollError(t *testing.T) {
	src := strings.Repeat("<br>", 5000)
	want := bytes.ErrTooLarge
	_, err := estimateNodes([]byte(src), noLimit, func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func FuzzDOM(f *testing.F) {
	seeds := []string{
		"<table><svg><tfoot><tr><div><td>", strings.Repeat("<table><svg><tfoot><tr><div><td>", 100),
		"<table><caption><table><tr><table></table><td>", strings.Repeat("<table><caption><table><tr><table></table><td>", 100),
		"", "x<p>", "<p>", "<br>", "<p a b c d e f g h>", "<!---->",
		"<b><i><u><p>x<p>y</b></i></u>", "<table><b><tr><td></b></table>",
		"<a href=1><a href=2><div></a></div>", "<svg><title><b></title></svg><title><b>",
		"<svg><foreignObject><script><!--</script><b>x</b>", "<math><mi><style><b></style>",
		"<td><b>x</td><b>y</b>", "<select><b>x</select>", "<template><tr><b>x</template>",
		"<nobr><nobr><div></nobr>", "<b a=1><b a=2><b a=3><b a=4><p>x", "<frameset><b>x",
		"<table><tr><td><table><b><td></b>", "<p><button></p><li><ul></li>",
		"<![CDATA[<b>]]><svg><![CDATA[<b>]]></svg>", "<plaintext><b>", "<noscript><b></noscript>",
		"<font color=red><b><i>x<div></b></font>y",
		"<table><tr><td>x</tbody><td>y", "<table><tr><td>x</tr><td>y</table><td>z", "<table><col><colgroup><col>",
		"<table><caption><b>x</table><b>y", "<td><td><tr><td>", "<tr><td><table><tr><td></td></table>",
		"<table><tbody><tr><td><b>x<td></b>y</table>", "<table><b><tr><td><i></b></i>",
		strings.Repeat("<b><i><div>", 20) + strings.Repeat("</b>", 20) + "z",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		root, err := html.Parse(bytes.NewReader(data))
		if err != nil {
			return
		}
		real := countCost(root)
		est, err := estimateNodes(data, noLimit, noPoll)
		if err != nil {
			t.Fatal(err)
		}
		if est < real {
			t.Fatalf("estimate %d < actual %d for %.200q", est, real, data)
		}
	})
}

var randomTokens = []string{
	"<b>", "</b>", "<i>", "</i>", "<u>", "</u>", "<a href=1>", "<a href=2>", "</a>", "<font color=r>", "</font>",
	"<nobr>", "</nobr>", "<div>", "</div>", "<p>", "</p>", "<table>", "</table>", "<tr>", "</tr>", "<td>", "</td>",
	"<th>", "<tbody>", "</tbody>", "<caption>", "<col>", "<colgroup>", "<li>", "<ul>", "</ul>", "<button>", "</button>",
	"<svg>", "</svg>", "<math>", "<mi>", "</mi>", "<foreignObject>", "</foreignObject>", "<title>", "</title>",
	"<script>", "</script>", "<style>", "</style>", "<textarea>", "</textarea>", "<select>", "</select>", "<option>",
	"<br>", "</br>", "x", "yy", " ", "<!-- c -->", "<!--", "<![CDATA[", "]]>", "<template>", "</template>",
	"<h1>", "</h2>", "<span a=1 b=2>", "</span>", "<pre>", "</pre>", "<form>", "</form>", "<frameset>", "<noscript>",
	"<b a=1>", "<b a=2>", "<i a=1>", "<html>", "<body>", "<head>", "</body>", "</html>", "\"", "'", "<img src=x>",
}

func TestEstimateRandomDifferential(t *testing.T) {
	randomDifferential(t, randomTokens, 1, 3000, 120)
}

// Formatting elements and block elements only: the sequences that make the
// parser clone nodes.
var formattingTokens = []string{
	"<b>", "</b>", "<i>", "</i>", "<u>", "</u>", "<a href=1>", "<a href=2>", "</a>", "<b a=1>", "<b a=2>", "<i a=1>",
	"<div>", "</div>", "<p>", "</p>", "<table>", "</table>", "<td>", "</td>", "<tr>", "x", "<li>", "<span>", "</span>",
	"<font a=1>", "</font>", "<button>", "</button>", "<nobr>", "</nobr>", "<br>",
}

func TestEstimateRandomFormatting(t *testing.T) {
	randomDifferential(t, formattingTokens, 9, 3000, 400)
}

// xorshift is a deterministic generator: the sequences must be reproducible.
type xorshift uint64

func (x *xorshift) intn(n int) int {
	*x ^= *x << 13
	*x ^= *x >> 7
	*x ^= *x << 17
	return int((*x>>11)&math.MaxInt32) % n
}

// Table, head and frameset structure with whitespace-led text.
var structureTokens = []string{
	"<table>", "</table>", "<tr>", "</tr>", "<td>", "</td>", "<th>", "</th>", "<tbody>", "</tbody>", "<thead>", "<tfoot>",
	"<caption>", "</caption>", "<col>", "<colgroup>", "</colgroup>", "<frameset>", "</frameset>", "<frame>", "<head>",
	"</head>", "<body>", "</body>", "<html>", "</html>", "<title>", "</title>", "<meta>", "<template>", "</template>",
	" ", " x", "x ", "\n", "\t y", "x", "<b>", "</b>", "<div>", "</div>", "<select>", "<option>", "</select>", "<!-- c -->",
	"<svg>", "</svg>", "<math>", "<mi>", "<foreignObject>", "<desc>", "<g>", "<path/>", "<p>",
}

func TestEstimateRandomStructure(t *testing.T) {
	randomDifferential(t, structureTokens, 21, 3000, 60)
}

// vocabularies are the token sets the random tests draw documents from.
var vocabularies = map[string][]string{
	"general":    randomTokens,
	"formatting": formattingTokens,
	"structure":  structureTokens,
}

func randomDocs(tokens []string, seed uint64, iterations, maxTokens int, f func(src string)) {
	rng := xorshift(seed*0x9E3779B97F4A7C15 | 1)
	for range iterations {
		var sb strings.Builder
		for range rng.intn(maxTokens) + 1 {
			sb.WriteString(tokens[rng.intn(len(tokens))])
		}
		f(sb.String())
	}
}

func randomDifferential(t *testing.T, tokens []string, seed uint64, iterations, maxTokens int) {
	t.Helper()
	if testing.Short() {
		iterations /= 10
	}
	if raceEnabled {
		iterations /= 20 // the non-race run and FuzzDOM carry the coverage
	}
	randomDocs(tokens, seed, iterations, maxTokens, func(src string) {
		assertCovers(t, src)
	})
}

// TestEstimateHillClimb searches for an undercount: from random documents it
// mutates one token at a time, keeping the mutations that bring the real node
// cost closest to the estimate, over every vocabulary. The estimate must never
// fall below the real cost.
func TestEstimateHillClimb(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("about 20 s: skipped by -short and -race")
	}
	names := make([]string, 0, len(vocabularies))
	for name := range vocabularies {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		tokens := vocabularies[name]
		t.Run(name, func(t *testing.T) {
			rng := xorshift(0xC0FFEE)
			gap := func(doc []string) (int64, bool) {
				src := strings.Join(doc, "")
				real, ok := actual(t, src)
				if !ok {
					return 0, false
				}
				est := estimate(t, src)
				if est < real {
					t.Fatalf("estimate %d < actual %d for %.200q", est, real, src)
				}
				return real - est, true
			}
			for range hillRestarts {
				doc := make([]string, 8+rng.intn(40))
				for i := range doc {
					doc[i] = tokens[rng.intn(len(tokens))]
				}
				best, ok := gap(doc)
				if !ok {
					continue
				}
				for range hillSteps {
					cand := slices.Clone(doc)
					switch i := rng.intn(len(cand)); rng.intn(3) {
					case 0:
						cand[i] = tokens[rng.intn(len(tokens))]
					case 1:
						if len(cand) < 120 {
							cand = slices.Insert(cand, i, tokens[rng.intn(len(tokens))])
						}
					default:
						if len(cand) > 2 {
							cand = slices.Delete(cand, i, i+1)
						}
					}
					if g, ok := gap(cand); ok && g >= best {
						doc, best = cand, g
					}
				}
			}
		})
	}
}

const (
	hillRestarts = 150
	hillSteps    = 1500
)
