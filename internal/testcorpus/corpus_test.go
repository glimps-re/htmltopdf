package testcorpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func templatesFS() fs.FS { return os.DirFS("../../testdata/templates") }

func fontsFS() fs.FS { return os.DirFS("../../testdata/fonts") }

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type fixture struct {
	name string
	data []byte
}

var allFixtures = sync.OnceValues(buildFixtures)

func fixtures(t testing.TB) []fixture {
	t.Helper()
	fx, err := allFixtures()
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func buildFixtures() ([]fixture, error) {
	var out []fixture
	names, err := ReportNames(templatesFS())
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		for _, size := range []Size{Small, Large} {
			b, err := Report(templatesFS(), n, size)
			if err != nil {
				return nil, err
			}
			out = append(out, fixture{FixtureName(n, size), b})
		}
	}
	for _, c := range SpikeHostile() {
		out = append(out, fixture{c.Name, c.HTML()})
	}
	for _, c := range Hostile(budget.Defaults()) {
		out = append(out, fixture{c.Name, c.HTML()})
	}
	return out, nil
}

// TestPrintSums prints the expected-sums table after a deliberate change of a
// generator: CORPUS_PRINT_SUMS=1 go test -run TestPrintSums -v ./internal/testcorpus
func TestPrintSums(t *testing.T) {
	if os.Getenv("CORPUS_PRINT_SUMS") == "" {
		t.Skip("set CORPUS_PRINT_SUMS=1 to print the table")
	}
	fx := fixtures(t)
	sort.Slice(fx, func(i, j int) bool { return fx[i].name < fx[j].name })
	for _, f := range fx {
		fmt.Printf("\t%q: %q,\n", f.name, sum(f.data))
	}
}

func TestFixtureSums(t *testing.T) {
	fx := fixtures(t)
	seen := map[string]bool{}
	for _, f := range fx {
		t.Run(f.name, func(t *testing.T) {
			want, ok := expectedSums[f.name]
			if !ok {
				t.Fatalf("no committed SHA-256 for %s", f.name)
			}
			if got := sum(f.data); got != want {
				t.Fatalf("SHA-256 = %s, want %s", got, want)
			}
		})
		if seen[f.name] {
			t.Errorf("duplicate fixture name %s", f.name)
		}
		seen[f.name] = true
	}
	for name := range expectedSums {
		if !seen[name] {
			t.Errorf("committed SHA-256 for unknown fixture %s", name)
		}
	}
}

func TestFixturesAreDeterministic(t *testing.T) {
	first := fixtures(t)
	second, err := buildFixtures()
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if !bytes.Equal(first[i].data, second[i].data) {
			t.Errorf("%s differs between two runs", first[i].name)
		}
	}
}

// The 13 spike files that are still byte for byte the spike's. The two
// report_00 files are no longer: the spike injected a @page rule that
// production HTML does not have (design §4.1).
func TestSpikeSumsMatchSpike(t *testing.T) {
	spike := map[string]string{
		"hostile_50k_rows":          "024af226d4f4aaf92e5a9e7cb97086727005804e8814775a37ae1596f13fa854",
		"hostile_colspan":           "6ccd4acd0f876059dd11fc68fbea4573a66c18307ae4c6854a81c08aa97d6f9d",
		"hostile_deep_nesting":      "6196ebe9f665973b27fc3fba70651aebf852ff23ed00d85d3ba4e2e8c60c9dd4",
		"hostile_huge_page":         "857fb373964bc57292ce8afdd20ebfefd148c007ad389033de1c84fad8682b44",
		"hostile_long_word":         "2a997ae2ae56c3cdff077b3e607a138ad94d481d7980d62ff31eae18ea7a7ab9",
		"hostile_remote":            "1a2eac3f3b12a862bd747b336252afb1b2e7e03e4efc743e9433286cab84f67a",
		"hostile_var_bomb":          "35bfee3feb197abdd15c96bdbc8577fba6fbac4a76fc2964501c53de0bcafcd6",
		"report_01-modern_large":    "e5c395c577c2be845b83965a742526dd33e3b488f31b4041e82f3d677612e508",
		"report_01-modern_small":    "4cdeee88b39a0a33edcd36624e1710251c432f024eaddc6baf415ff478b7e141",
		"report_02-executive_large": "0470870531f4a0ee585bd65e02def5c2ffa4d5b7bbfe43bba789d49f41816b76",
		"report_02-executive_small": "0681883be41c79af05d3f4515fd1d2741bff0dd82b0e1369f56e4f0215ba764a",
		"report_03-analyst_large":   "1475ab00a0cd2ad9d9566449bfe019521520c94e65486048b98281f3c3019d94",
		"report_03-analyst_small":   "1d8a616e6c76cdbccb9f038a918e5560b4b26562447c5b18992523af3686e7ec",
	}
	for name, want := range spike {
		if got := expectedSums[name]; got != want {
			t.Errorf("%s: committed %s, spike %s", name, got, want)
		}
	}
}

func TestReportErrors(t *testing.T) {
	if _, err := Report(templatesFS(), "00-host-connector", "huge"); err == nil {
		t.Error("unknown size must fail")
	}
	if _, err := Report(templatesFS(), "missing", Small); err == nil {
		t.Error("missing template must fail")
	}
	if _, err := ReportNames(os.DirFS(t.TempDir())); err == nil {
		t.Error("no template must fail")
	}
}

// The reports are the template output as is: production HTML carries no @page
// (the host-connector page rules arrive as ExtraCSS), whereas the other
// templates bring their own.
func TestReportsAreTheTemplateOutput(t *testing.T) {
	names, err := ReportNames(templatesFS())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 4 {
		t.Fatalf("got templates %v, want the 4 of the corpus", names)
	}
	for _, n := range names {
		for _, size := range []Size{Small, Large} {
			b, err := Report(templatesFS(), n, size)
			if err != nil {
				t.Fatal(err)
			}
			hasPage := strings.Contains(string(b), "@page")
			if hasPage == (n == "00-host-connector") {
				t.Errorf("%s %s: @page present = %v", n, size, hasPage)
			}
			if strings.Contains(string(b), "TLP:AMBER") && n == "00-host-connector" {
				t.Errorf("%s %s: the host-connector footer must come from ExtraCSS only", n, size)
			}
			if len(b) > int(budget.Defaults()[budget.InputBytes]) {
				t.Errorf("%s %s: %d bytes exceed MaxInputBytes", n, size, len(b))
			}
		}
	}
}

func TestHostConnectorReference(t *testing.T) {
	fonts, err := ReadHostConnectorFonts(fontsFS())
	if err != nil {
		t.Fatal(err)
	}
	if len(fonts) != 7 {
		t.Fatalf("got %d fonts, want 7", len(fonts))
	}
	for i, f := range fonts {
		if len(f.Data) < 4 || string(f.Data[:4]) != "\x00\x01\x00\x00" {
			t.Errorf("%s: not a TrueType/glyf font", HostConnectorFontFiles()[i].Name)
		}
	}
	if _, err := ReadHostConnectorFonts(os.DirFS(t.TempDir())); err == nil {
		t.Error("missing fonts must fail")
	}
	hc := HostConnector()
	if hc.Scale != 0.9 || hc.SansSerif != "Liberation Sans" || hc.Monospace != "DejaVu Sans Mono" || len(hc.Fallback) != 1 {
		t.Errorf("unexpected reference options %+v", hc)
	}
	hc.Fallback[0] = "changed"
	if HostConnector().Fallback[0] != "DejaVu Sans" {
		t.Error("HostConnector must return a fresh copy")
	}
	for _, want := range []string{"size: 794.56px 1122.56px", "margin: 96px 28px 124px 28px", "TLP:AMBER", "counter(page)", "counter(pages)"} {
		if !strings.Contains(HostConnector().ExtraCSS, want) {
			t.Errorf("ExtraCSS lacks %q", want)
		}
	}
}

func TestChromePaper(t *testing.T) {
	cp := ChromePaper()
	hc := HostConnector()
	if cp.ExtraCSS != ChromePaperCSS || !strings.Contains(cp.ExtraCSS, "size: 794.56px 1122.56px") {
		t.Errorf("ExtraCSS = %q", cp.ExtraCSS)
	}
	for _, bad := range []string{"margin", "@bottom", "TLP:AMBER"} {
		if strings.Contains(cp.ExtraCSS, bad) {
			t.Errorf("paper-only CSS contains %q", bad)
		}
	}
	if cp.Scale != hc.Scale || cp.SansSerif != hc.SansSerif || cp.Monospace != hc.Monospace || len(cp.Fallback) != 1 || cp.Fallback[0] != hc.Fallback[0] {
		t.Errorf("ChromePaper %+v differs from HostConnector %+v beyond ExtraCSS", cp, hc)
	}
	cp.Fallback[0] = "changed"
	if ChromePaper().Fallback[0] != "DejaVu Sans" {
		t.Error("ChromePaper must return a fresh copy")
	}
	if HostConnector().ExtraCSS != HostConnectorPageCSS {
		t.Error("ChromePaper must not alter HostConnector")
	}
}

func TestCasesAreWellFormed(t *testing.T) {
	cases := append(SpikeHostile(), Hostile(budget.Defaults())...)
	byName := map[string]Case{}
	for _, c := range cases {
		byName[c.Name] = c
	}
	for _, c := range cases {
		if c.HTML == nil || c.Name == "" {
			t.Fatalf("incomplete case %+v", c)
		}
		if c.Twin == "" {
			continue
		}
		twin, ok := byName[c.Twin]
		if !ok || twin.Twin != c.Name || twin.Limit != c.Limit {
			t.Errorf("%s: twin %q missing or not reciprocal", c.Name, c.Twin)
		}
		if strings.HasSuffix(c.Name, "_under") && c.Want != Success {
			t.Errorf("%s: an under twin must expect Success", c.Name)
		}
		if strings.HasSuffix(c.Name, "_over") && c.Want == Success {
			t.Errorf("%s: an over case cannot expect Success", c.Name)
		}
	}
}

func TestEveryLimitHasOverAndUnder(t *testing.T) {
	have := map[string]bool{}
	for _, c := range Hostile(budget.Defaults()) {
		if strings.HasSuffix(c.Name, "_under") {
			have[c.Limit] = true
		}
	}
	for k := budget.Kind(0); k < budget.NumKinds; k++ {
		if k == budget.OutputBytes {
			continue
		}
		if !have[k.String()] {
			t.Errorf("no just-under twin exercising %s", k)
		}
	}
	for _, l := range []string{
		"colspan", "table-columns", "length", "page-size", "page-margins",
		"gradient-stops", "compounds", "simple-selectors", "css-nesting", "link-length",
	} {
		if !have[l] {
			t.Errorf("no just-under twin exercising %s", l)
		}
	}
}

var hostileCache = map[budget.Limits]map[string][]byte{}

func hostileByName(t *testing.T, l budget.Limits) map[string][]byte {
	t.Helper()
	if out, ok := hostileCache[l]; ok {
		return out
	}
	out := map[string][]byte{}
	for _, c := range Hostile(l) {
		out[c.Name] = c.HTML()
	}
	hostileCache[l] = out
	return out
}

func styleBody(s string) string {
	i := strings.Index(s, "<style>") + len("<style>")
	return s[i:strings.Index(s, "</style>")]
}

func TestInputBytesBoundary(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	if got := int64(len(m["input_bytes_under"])); got != l[budget.InputBytes] {
		t.Errorf("under = %d bytes, want exactly %d", got, l[budget.InputBytes])
	}
	if got := int64(len(m["input_bytes_over"])); got != l[budget.InputBytes]+1 {
		t.Errorf("over = %d bytes, want %d", got, l[budget.InputBytes]+1)
	}
}

func TestOnlyInputCasesExceedInputLimit(t *testing.T) {
	l := budget.Defaults()
	for name, b := range hostileByName(t, l) {
		if n := int64(len(b)); n > l[budget.InputBytes] && name != "input_bytes_over" {
			t.Errorf("%s is %d bytes: it would hit MaxInputBytes first", name, n)
		}
	}
	for _, c := range SpikeHostile() {
		over := int64(len(c.HTML())) > l[budget.InputBytes]
		if over != (c.Limit == "MaxInputBytes") {
			t.Errorf("%s: size over limit = %v, limit label %q", c.Name, over, c.Limit)
		}
	}
}

func TestHTMLDepthBoundaryMatchesParser(t *testing.T) {
	m := hostileByName(t, budget.Defaults())
	if _, err := html.Parse(bytes.NewReader(m["html_depth_under"])); err != nil {
		t.Errorf("under must parse: %v", err)
	}
	if _, err := html.Parse(bytes.NewReader(m["html_depth_over"])); err == nil {
		t.Error("over must fail in x/net/html: its depth bound moved, update the generator and design §5.2.1")
	}
	for _, c := range SpikeHostile() {
		if c.Name != "hostile_deep_nesting" {
			continue
		}
		if _, err := html.Parse(bytes.NewReader(c.HTML())); err == nil {
			t.Error("spike deep nesting must fail to parse")
		}
	}
}

// tokenEstimate mirrors the pre-pass bound of design §6.1 without formatting
// reconstruction: start tags, text, comments and attributes.
func tokenEstimate(b []byte) int64 {
	z := html.NewTokenizer(bytes.NewReader(b))
	var n int64
	for {
		switch z.Next() {
		case html.ErrorToken:
			return n
		case html.StartTagToken, html.SelfClosingTagToken:
			n++
			_, more := z.TagName()
			for more {
				_, _, more = z.TagAttr()
				n++
			}
		case html.TextToken, html.CommentToken:
			n++
		}
	}
}

func TestNodeCasesStraddleTheLimit(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	for _, u := range nodeUnits {
		if got := tokenEstimate([]byte(u.unit)); got != u.cost {
			t.Fatalf("%s: one unit costs %d tokens, table says %d", u.name, got, u.cost)
		}
		over := int64(strings.Count(string(m["nodes_"+u.name+"_over"]), u.unit)) * u.cost
		under := tokenEstimate(m["nodes_"+u.name+"_under"])
		if over <= l[budget.Nodes] {
			t.Errorf("%s over: estimate %d does not exceed %d", u.name, over, l[budget.Nodes])
		}
		if under > l[budget.Nodes] || under < l[budget.Nodes]*9/10 {
			t.Errorf("%s under: estimate %d not in [90%%, 100%%] of %d", u.name, under, l[budget.Nodes])
		}
		if tree := countNodes(t, m["nodes_"+u.name+"_under"]); tree > l[budget.Nodes] {
			t.Errorf("%s under: real tree has %d nodes", u.name, tree)
		}
	}
	const bomb = "<b><i><u><p>x"
	if got := tokenEstimate([]byte(bomb)); got != 5 {
		t.Fatalf("bomb unit costs %d tokens", got)
	}
	if int64(strings.Count(string(m["nodes_format_bomb_over"]), bomb))*5 <= l[budget.Nodes] {
		t.Error("bomb over must exceed the node limit by token count alone")
	}
}

func countNodes(t *testing.T, b []byte) int64 {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(n *html.Node) int64
	walk = func(n *html.Node) int64 {
		c := int64(1)
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			c += walk(ch)
		}
		return c
	}
	return walk(root)
}

func TestFormatBombUnderStaysUnderInTheRealTree(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	if got := countNodes(t, m["nodes_format_bomb_under"]); got > l[budget.Nodes] {
		t.Errorf("bomb under builds %d nodes, limit %d", got, l[budget.Nodes])
	}
}

func TestCSSBoundaries(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	css := func(name string) int64 { return int64(len(styleBody(string(m[name])))) }
	if css("css_style_element_under") != l[budget.CSSBytes] || css("css_style_element_over") != l[budget.CSSBytes]+1 {
		t.Error("style element boundary is not exactly MaxCSSBytes / +1")
	}
	attr := func(name string) int64 {
		s := string(m[name])
		i := strings.Index(s, `style="`) + 7
		return int64(strings.Index(s[i:], `"`))
	}
	if attr("css_style_attribute_under") != l[budget.CSSBytes] || attr("css_style_attribute_over") != l[budget.CSSBytes]+1 {
		t.Error("style attribute boundary is not exactly MaxCSSBytes / +1")
	}
	count := func(name string) int64 {
		list, _, _ := strings.Cut(styleBody(string(m[name])), "{")
		return int64(strings.Count(list, ",") + 1)
	}
	if got := count("selectors_list_under"); got != l[budget.Selectors] {
		t.Errorf("selectors under = %d", got)
	}
	if got := count("selectors_list_over"); got != l[budget.Selectors]+1 {
		t.Errorf("selectors over = %d", got)
	}
}

func TestSelectorsListMaxCSSFitsCSSBudget(t *testing.T) {
	l := budget.Defaults()
	body := styleBody(string(hostileByName(t, l)["selectors_list_max_css"]))
	if n := int64(len(body)); n > l[budget.CSSBytes] || n < l[budget.CSSBytes]-8 {
		t.Errorf("stylesheet is %d bytes, want just under %d", n, l[budget.CSSBytes])
	}
	if int64(strings.Count(body, ","))+1 <= l[budget.Selectors] {
		t.Error("must carry more selectors than MaxSelectors")
	}
}

func TestVarDoublingStraddlesTheLimit(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	levels := func(name string) int { return (strings.Count(string(m[name]), "--v") - 2) / 3 }
	over, under := levels("var_doubling_over"), levels("var_doubling_under")
	if over != 16 || under != 15 {
		t.Errorf("levels over %d, under %d, want 16 and 15", over, under)
	}
	if total := 3 * (int64(1)<<(over+1) - 1); total <= l[budget.VarBytes] {
		t.Errorf("over: total charge of %d bytes does not exceed %d", total, l[budget.VarBytes])
	}
	if total := 3 * (int64(1)<<(under+1) - 1); total > l[budget.VarBytes] || total <= l[budget.VarBytes]/2 {
		t.Errorf("under: total charge of %d bytes is not the last level that fits in %d", total, l[budget.VarBytes])
	}
}

func TestStylesUniqueAndCounted(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	for name, want := range map[string]int{
		"styles_unique_over":  int(l[budget.Styles]) + 1,
		"styles_unique_under": int(l[budget.Styles]) - 10,
	} {
		s := string(m[name])
		if got := strings.Count(s, "<i style="); got != want {
			t.Errorf("%s: %d elements, want %d", name, got, want)
		}
		if got := strings.Count(s, "width:"); got != want {
			t.Errorf("%s: %d declarations", name, got)
		}
		if !strings.Contains(s, "width:"+fmt.Sprint(want-1)+"px") || strings.Contains(s, "width:"+fmt.Sprint(want)+"px") {
			t.Errorf("%s: widths are not 0..%d", name, want-1)
		}
	}
}

func TestPaginationCasesStraddleMaxPages(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	height := func(name string) int64 {
		var h int64
		s := string(m[name])
		i := strings.Index(s, "padding-top:") + 12
		if _, err := fmt.Sscanf(s[i:], "%dpx", &h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	const contentPx, pagePx = 930, 1123
	if p := height("pages_tall_block_over") / pagePx; p < l[budget.Pages] {
		t.Errorf("over: at most %d pages of the tallest possible content, want > %d", p, l[budget.Pages])
	}
	if p := (height("pages_tall_block_under") + 16 + contentPx - 1) / contentPx; p != l[budget.Pages]-1 {
		t.Errorf("under: %d pages, want exactly %d", p, l[budget.Pages]-1)
	}
	if h := height("pages_tall_block_over"); h > 1e7 {
		t.Errorf("over height %d exceeds the clamp of 1e7 px", h)
	}
}

func TestWorkCasesStraddleMaxWork(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	work := func(name string) int64 {
		s := string(m[name])
		sel := int64(strings.Count(s, "~i"))
		n := int64(strings.Count(s, "<i></i>"))
		return sel * n * n / 2
	}
	if w := work("work_sibling_scan_over"); w < 20*l[budget.Work] {
		t.Errorf("over: %d units, want ≥ 20× MaxWork", w)
	}
	if w := work("work_sibling_scan_under"); w > l[budget.Work]/2 || w < l[budget.Work]/10 {
		t.Errorf("under: %d units, want between 10%% and 50%% of MaxWork", w)
	}
	over := string(m["work_sibling_scan_over"])
	if int64(strings.Count(over, "~i")) != l[budget.Selectors] {
		t.Error("over must carry MaxSelectors selectors")
	}
	if int64(strings.Count(over, "<i></i>")) >= l[budget.Nodes] {
		t.Error("over must stay under MaxNodes so that Work is the first limit hit")
	}
}

func TestOtherCountedCasesStayUnderNodeLimit(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	for _, c := range Hostile(l) {
		if c.Limit == "MaxNodes" || c.Limit == "HTMLDepth" || c.Limit == "MaxInputBytes" {
			continue
		}
		if est := tokenEstimate(m[c.Name]); est > l[budget.Nodes] {
			t.Errorf("%s: %d tokens would hit MaxNodes first", c.Name, est)
		}
	}
}

func TestBoxesAndGlyphsBoundaries(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	if got := glyphRunes(m["glyphs_over"]); got != l[budget.Glyphs]+1 {
		t.Errorf("glyphs over = %d", got)
	}
	if got := glyphRunes(m["glyphs_under"]); got != l[budget.Glyphs]-glyphAllowance {
		t.Errorf("glyphs under = %d", got)
	}
	if !strings.Contains(string(m["glyphs_over"]), "body{font-size:4px}") {
		t.Error("glyphs cases are set at 4px")
	}
	if got := strings.Count(string(m["glyphs_longword_1M"]), "A"); got != 1_000_000 {
		t.Errorf("1M word = %d runes", got)
	}
	if got := strings.Count(string(m["glyphs_longword_2M"]), "A"); got != 2_000_000 {
		t.Errorf("2M word = %d runes", got)
	}
	if got := strings.Count(string(m["glyphs_longword_2M_plain"]), "A"); int64(got) != l[budget.Glyphs]-glyphAllowance || strings.Contains(string(m["glyphs_longword_2M_plain"]), "letter-spacing") {
		t.Errorf("plain 2M word: %d runes", got)
	}
	if got := strings.Count(string(m["boxes_pseudo_over"]), "<i></i>"); int64(got)*3 <= l[budget.Boxes] {
		t.Errorf("boxes over: %d elements make at most %d boxes", got, got*3)
	}
	if got := strings.Count(string(m["boxes_pseudo_under"]), "<i></i>"); int64(got)*6 > l[budget.Boxes] {
		t.Errorf("boxes under: %d elements", got)
	}
}

func TestTableAndSpanBoundaries(t *testing.T) {
	m := hostileByName(t, budget.Defaults())
	for name, want := range map[string]int{"table_columns_over": 1001, "table_columns_under": 1000} {
		first := strings.SplitN(string(m[name]), "</tr>", 2)[0]
		if got := strings.Count(first, "<td>"); got != want {
			t.Errorf("%s: %d cells, want %d", name, got, want)
		}
	}
	if !strings.Contains(string(m["colspan_over"]), `colspan="1001"`) ||
		!strings.Contains(string(m["colspan_under"]), `colspan="1000"`) {
		t.Error("colspan boundary")
	}
	if got := strings.Count(string(m["colspan_under"]), "<td>"); got != 1000 {
		t.Errorf("colspan columns = %d, want 1000", got)
	}
}

func TestValueBoundaries(t *testing.T) {
	m := hostileByName(t, budget.Defaults())
	for name, sub := range map[string]string{
		"length_over": "10000001px", "length_under": "10000000px",
		"page_size_max_over": "201in", "page_size_max_under": "200in",
		"page_size_min_over": "0.99in", "page_size_min_under": "size:1in 1in",
		"page_margins_over": "margin:50in", "page_margins_under": "margin:3.6in",
	} {
		if !strings.Contains(string(m[name]), sub) {
			t.Errorf("%s lacks %q", name, sub)
		}
	}
	for name, want := range map[string]int{"gradient_stops_over": 65, "gradient_stops_under": 64} {
		if got := strings.Count(string(m[name]), "rgb("); got != want {
			t.Errorf("%s: %d stops, want %d", name, got, want)
		}
	}
	if strings.Count(string(m["css_nesting_over"]), "@media") != 65 || strings.Count(string(m["css_nesting_under"]), "@media") != 64 ||
		strings.Count(string(m["css_nesting_1000"]), "@media") != 1000 {
		t.Error("nesting depths")
	}
	if strings.Count(string(m["selector_compounds_10000"]), "a ") < 9999 {
		t.Error("10 000 compounds")
	}
	if sel := selectorOf(m["selector_simples_over"]); strings.Count(sel, ".")+1 != 33 {
		t.Errorf("over simples: %q", sel)
	}
	if sel := selectorOf(m["selector_simples_under"]); strings.Count(sel, ".")+1 != 32 {
		t.Errorf("under simples: %q", sel)
	}
	if sel := selectorOf(m["selector_compounds_over"]); strings.Count(sel, " ")+1 != 33 {
		t.Error("over compounds")
	}
	if sel := selectorOf(m["selector_compounds_under"]); strings.Count(sel, " ")+1 != 32 {
		t.Error("under compounds")
	}
}

func selectorOf(b []byte) string {
	sel, _, _ := strings.Cut(styleBody(string(b)), "{")
	return sel
}

func TestLinkLengthBoundary(t *testing.T) {
	m := hostileByName(t, budget.Defaults())
	href := func(name string) string {
		s := string(m[name])
		i := strings.Index(s, `href="`) + 6
		return s[i : i+strings.Index(s[i:], `"`)]
	}
	if got := len(href("link_target_under")); got != 2048 {
		t.Errorf("under = %d bytes", got)
	}
	if got := len(href("link_target_over")); got != 2049 {
		t.Errorf("over = %d bytes", got)
	}
}

func TestLinksTable(t *testing.T) {
	var allowed, blocked int
	for _, l := range Links() {
		if l.Allowed {
			allowed++
		} else {
			blocked++
		}
	}
	if allowed < 5 || blocked < 10 {
		t.Errorf("link table too thin: %d allowed, %d blocked", allowed, blocked)
	}
	doc := string(hostileByName(t, budget.Defaults())["links_schemes"])
	if got := strings.Count(doc, "<a href="); got != len(Links()) {
		t.Errorf("document carries %d links, table has %d", got, len(Links()))
	}
	for _, want := range []string{"JaVaScRiPt:", "\x01javascript:", "java\tscript:"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %q", strings.ToValidUTF8(want, "?"))
		}
	}
}

func TestTextCasesCoverEveryFamily(t *testing.T) {
	cs := TextCases()
	names := map[string]TextCase{}
	for _, c := range cs {
		names[c.Name] = c
		if !utf8.ValidString(c.Want) {
			t.Errorf("%s: expectation is not valid UTF-8", c.Name)
		}
	}
	for _, n := range []string{
		"bidi_LRE", "bidi_RLE", "bidi_PDF", "bidi_LRO", "bidi_RLO", "bidi_LRI", "bidi_RLI", "bidi_FSI", "bidi_PDI",
		"rlo_filename", "dropped_U+200B", "kept_U+200C", "kept_U+200D", "dropped_U+200F", "dropped_U+2060", "dropped_U+2064", "dropped_U+FEFF",
		"dropped_U+00AD", "control_U+0001", "control_U+001F", "control_U+0080", "control_U+009F", "control_U+000B",
		"control_U+000C", "control_U+007F", "whitespace_controls_kept", "invalid_lone_continuation", "valid_untouched",
	} {
		if _, ok := names[n]; !ok {
			t.Errorf("missing text case %s", n)
		}
	}
	for _, r := range []rune{0x09, 0x0a, 0x0d} {
		if _, ok := names[fmt.Sprintf("control_U+%04X", r)]; ok {
			t.Errorf("U+%04X must not be replaced", r)
		}
	}
	if got := len(runeRange([2]rune{0x200b, 0x200f})); got != 5 {
		t.Errorf("runeRange = %d", got)
	}
	if !bytes.Contains(TextDocument("a\x01\xffb"), []byte("a\x01\xffb")) {
		t.Error("TextDocument must keep raw bytes")
	}
}

func freshCase(t *testing.T, cs []Case, name string) []byte {
	t.Helper()
	for _, c := range cs {
		if c.Name == name {
			return c.HTML()
		}
	}
	t.Fatalf("no case %s", name)
	return nil
}

func TestRobustCases(t *testing.T) {
	m := hostileByName(t, budget.Defaults())
	if len(m["empty_document"]) != 0 {
		t.Error("empty document must be empty")
	}
	g := m["binary_garbage"]
	if len(g) != 64<<10 || utf8.Valid(g) || !bytes.Equal(g, freshCase(t, robustCases(), "binary_garbage")) {
		t.Error("garbage must be 64 KiB, invalid UTF-8 and stable")
	}
	if utf8.Valid(m["invalid_utf8_document"]) {
		t.Error("invalid_utf8_document must be invalid UTF-8")
	}
	if !bytes.HasPrefix(m["pdf_header_garbage"], []byte("%PDF-")) {
		t.Error("pdf_header_garbage prefix")
	}
	if len(malformedCSS) < 10 {
		t.Error("malformed CSS list too short")
	}
}

func TestHostileScalesWithLimits(t *testing.T) {
	l := budget.Defaults()
	l[budget.InputBytes], l[budget.Nodes], l[budget.CSSBytes] = 64<<10, 1000, 4096
	l[budget.Selectors], l[budget.Styles], l[budget.Glyphs] = 50, 200, 5000
	l[budget.Boxes], l[budget.Pages], l[budget.Work], l[budget.VarBytes] = 3000, 10, 1_000_000, 1024
	if len(Hostile(l)) != len(Hostile(budget.Defaults())) {
		t.Fatal("case list must not depend on limits")
	}
	m := hostileByName(t, l)
	if int64(len(m["input_bytes_under"])) != l[budget.InputBytes] {
		t.Error("input boundary must follow the limit")
	}
	if got := glyphRunes(m["glyphs_over"]); got != l[budget.Glyphs]+1 {
		t.Errorf("glyphs over = %d", got)
	}
}

// glyphRunes counts the runes of the paragraph of a glyph case.
func glyphRunes(doc []byte) int64 {
	s := string(doc)
	i := strings.Index(s, "<p>") + 3
	j := strings.Index(s, "</p>")
	return int64(utf8.RuneCountInString(s[i:j]))
}

func TestHostileIsTotalOverTinyLimits(t *testing.T) {
	for _, v := range []int64{0, 1, 2, 11, 29} {
		var l budget.Limits
		for i := range l {
			l[i] = v
		}
		cs := Hostile(l)
		if len(cs) != len(Hostile(budget.Defaults())) {
			t.Fatalf("limits %d: case list changed", v)
		}
		for _, c := range cs {
			_ = c.HTML()
		}
	}
	l := budget.Defaults()
	l[budget.CSSBytes] = 2
	for _, c := range Hostile(l) {
		_ = c.HTML()
	}
}

func TestHostileIsTotalOverUnlimitedLimits(t *testing.T) {
	var l budget.Limits
	for i := range l {
		l[i] = math.MaxInt64
	}
	done := make(chan []Case)
	go func() { done <- Hostile(l) }()
	select {
	case cs := <-done:
		if len(cs) != len(Hostile(budget.Defaults())) {
			t.Error("case list must not depend on limits")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Hostile does not return for MaxInt64 limits")
	}
}

func BenchmarkGenerateReports(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Report(templatesFS(), "00-host-connector", Large); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTableRowCases(t *testing.T) {
	l := budget.Defaults()
	m := hostileByName(t, l)
	rows := func(name string) int64 { return int64(strings.Count(string(m[name]), "<tr>")) }
	maxPages := l[budget.Pages]
	if got := rows("pages_table_tall_rows_over"); got != 5*(maxPages+1) {
		t.Errorf("tall rows over: %d rows, want %d", got, 5*(maxPages+1))
	}
	if got := rows("pages_table_tall_rows_under"); got != 5*(maxPages-1) {
		t.Errorf("tall rows under: %d rows, want %d", got, 5*(maxPages-1))
	}
	for _, name := range []string{"pages_table_tall_rows_over", "pages_table_tall_rows_under"} {
		for _, want := range []string{"@page{size:A4;margin:0}", "td{padding:0;line-height:200px}", "tr,td{border:0;margin:0}"} {
			if !strings.Contains(string(m[name]), want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
	over, under := string(m["pages_table_rows_over"]), string(m["pages_table_rows_under"])
	if over != under {
		t.Error("the row-stress twin must be the same document: only MaxPages is raised")
	}
	if got := rows("pages_table_rows_over"); got != l[budget.Nodes]*9/10/3 {
		t.Errorf("stress rows: %d", got)
	}
	if nodes := 3*rows("pages_table_rows_over") + 10; nodes >= l[budget.Nodes] {
		t.Errorf("about %d nodes, over MaxNodes %d", nodes, l[budget.Nodes])
	}
	for _, c := range Hostile(l) {
		switch c.Name {
		case "pages_table_rows_under":
			lim := l
			if c.Limits == nil {
				t.Fatal("the stress twin raises MaxPages")
			}
			c.Limits(&lim)
			if lim[budget.Pages] != 1000 {
				t.Errorf("MaxPages = %d, want 1000", lim[budget.Pages])
			}
			for k := budget.Kind(0); k < budget.NumKinds; k++ {
				if k != budget.Pages && lim[k] != l[k] {
					t.Errorf("the twin changes %s", k)
				}
			}
		case "pages_table_rows_over":
			if c.Limits != nil || c.Want != Limit {
				t.Errorf("stress over: Limits %v, Want %v", c.Limits != nil, c.Want)
			}
		}
	}
	for _, c := range SpikeHostile() {
		if c.Name == "hostile_50k_rows" && (c.Want != Limit || c.Limit != "MaxInputBytes") {
			t.Errorf("hostile_50k_rows: %v %s", c.Want, c.Limit)
		}
	}
}
