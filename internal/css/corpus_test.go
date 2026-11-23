package css_test

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	tcss "github.com/tdewolff/parse/v2/css"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/css"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// counts: style rules with declarations, all declarations, @media, @page and
// the declarations of margin boxes (included in decls).
type counts struct{ style, decls, media, page, margin int }

// styleSheets returns the content of the <style> elements of the report name.
func styleSheets(tb testing.TB, name string) string {
	tb.Helper()
	html, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, testcorpus.Small)
	if err != nil {
		tb.Fatal(err)
	}
	var out strings.Builder
	rest := string(html)
	for {
		i := strings.Index(rest, "<style")
		if i < 0 {
			break
		}
		rest = rest[i:]
		start := strings.IndexByte(rest, '>') + 1
		end := strings.Index(rest, "</style>")
		out.WriteString(rest[start:end])
		out.WriteByte('\n')
		rest = rest[end:]
	}
	return out.String()
}

func count(rules []css.Rule) counts {
	var c counts
	for _, r := range rules {
		switch r.Kind {
		case css.RuleStyle:
			c.style++
		case css.RuleMedia:
			c.media++
		case css.RulePage:
			c.page++
		case css.RuleMarginBox:
			c.margin += len(r.Decls)
		}
		c.decls += len(r.Decls)
		sub := count(r.Rules)
		c.style += sub.style
		c.decls += sub.decls
		c.media += sub.media
		c.page += sub.page
		c.margin += sub.margin
	}
	return c
}

// oracle counts with the grammar-level parser of the lexer's own module.
func oracle(src string) counts {
	var c counts
	p := tcss.NewParser(parse.NewInputBytes([]byte(src)), false)
	hasDecl := []bool{}
	for {
		gt, _, data := p.Next()
		switch gt {
		case tcss.ErrorGrammar:
			return c
		case tcss.BeginRulesetGrammar:
			hasDecl = append(hasDecl, false)
		case tcss.DeclarationGrammar, tcss.CustomPropertyGrammar:
			c.decls++
			if len(hasDecl) > 0 {
				hasDecl[len(hasDecl)-1] = true
			}
		case tcss.EndRulesetGrammar:
			if hasDecl[len(hasDecl)-1] {
				c.style++
			}
			hasDecl = hasDecl[:len(hasDecl)-1]
		case tcss.BeginAtRuleGrammar:
			switch strings.ToLower(string(data)) {
			case "@media":
				c.media++
			case "@page":
				c.page++
			}
		}
	}
}

// Pinned counts of the four corpus stylesheets (rules with declarations,
// declarations, @media, @page); the grammar-level oracle must agree.
var corpusCounts = map[string]counts{
	"00-host-connector": {style: 19, decls: 83, media: 1, page: 1, margin: 6},
	"01-modern":         {style: 54, decls: 167, media: 0, page: 2, margin: 15},
	"02-executive":      {style: 61, decls: 230, media: 0, page: 2, margin: 18},
	"03-analyst":        {style: 57, decls: 164, media: 0, page: 1, margin: 10},
}

func TestCorpusStylesheets(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		src := styleSheets(t, name)
		b := budget.New(context.Background(), budget.Defaults(), nil)
		ss, err := css.ParseStylesheet([]byte(src), css.OriginDocument, b)
		if err != nil {
			t.Fatal(err)
		}
		got := count(ss.Rules)
		if ws, _ := b.Warnings(); len(ws) != 0 {
			t.Errorf("%s: unexpected warnings %v", name, ws)
		}
		// The oracle does not descend into margin boxes.
		want, plain := oracle(src), got
		plain.decls -= plain.margin
		plain.margin = 0
		if plain != want {
			t.Errorf("%s: parser %+v, oracle %+v", name, plain, want)
		}
		if want := corpusCounts[name]; got != want {
			t.Errorf("%s: counts %+v, pinned %+v", name, got, want)
		}
		t.Logf("%s: %d bytes, %+v", name, len(src), got)
	}
}

func BenchmarkParseCorpus(b *testing.B) {
	for _, name := range []string{"00-host-connector", "01-modern", "02-executive", "03-analyst"} {
		src := []byte(styleSheets(b, name))
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			l := budget.Defaults()
			l[budget.CSSBytes] = math.MaxInt64
			bud := budget.New(context.Background(), l, nil)
			for i := 0; i < b.N; i++ {
				if _, err := css.ParseStylesheet(src, css.OriginDocument, bud); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkLexerBaseline is the floor of ParseCorpus/00: the bare lexer over
// the same bytes.
func BenchmarkLexerBaseline(b *testing.B) {
	src := []byte(styleSheets(b, "00-host-connector"))
	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	for i := 0; i < b.N; i++ {
		buf := make([]byte, len(src), len(src)+1)
		copy(buf, src)
		l := tcss.NewLexer(parse.NewInputBytes(buf))
		for {
			if tt, _ := l.Next(); tt == tcss.ErrorToken {
				break
			}
		}
	}
}
