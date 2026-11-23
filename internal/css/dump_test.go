package css

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
)

func dumpTokens(toks []Token) string {
	var b strings.Builder
	for _, t := range toks {
		switch t.Type {
		case TIdent, TDelim, TMatch, TUnicodeRange:
			b.WriteString(t.Text)
		case TFunction:
			b.WriteString(t.Text + "(")
		case TAtKeyword:
			b.WriteString("@" + t.Text)
		case THash:
			b.WriteString("#" + t.Text)
		case TString:
			fmt.Fprintf(&b, "%q", t.Text)
		case TURL:
			b.WriteString("url<" + t.Text + ">")
		case TBadString:
			b.WriteString("<badstring>")
		case TBadURL:
			b.WriteString("<badurl>")
		case TNumber:
			fmt.Fprintf(&b, "%g", t.Num)
		case TPercentage:
			fmt.Fprintf(&b, "%g%%", t.Num)
		case TDimension:
			u := t.Text
			if t.Unit != UnitOther {
				u = fmt.Sprintf("u%d", t.Unit)
			}
			fmt.Fprintf(&b, "%g%s", t.Num, u)
		case TWhitespace:
			b.WriteByte('_')
		case TCDO:
			b.WriteString("<!--")
		case TCDC:
			b.WriteString("-->")
		case TColon:
			b.WriteByte(':')
		case TSemicolon:
			b.WriteByte(';')
		case TComma:
			b.WriteByte(',')
		case TLBracket:
			b.WriteByte('[')
		case TRBracket:
			b.WriteByte(']')
		case TLParen:
			b.WriteByte('(')
		case TRParen:
			b.WriteByte(')')
		case TLBrace:
			b.WriteByte('{')
		case TRBrace:
			b.WriteByte('}')
		default:
			fmt.Fprintf(&b, "<?%d>", t.Type)
		}
	}
	return b.String()
}

func dumpDecls(ds []Declaration) string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = d.Name + ":" + dumpTokens(d.Value)
		if d.Important {
			parts[i] += "!"
		}
	}
	return strings.Join(parts, ";")
}

func dumpRules(rs []Rule) string {
	var parts []string
	for _, r := range rs {
		var s string
		switch r.Kind {
		case RuleStyle:
			s = fmt.Sprintf("%s{%s}", dumpTokens(r.Prelude), dumpDecls(r.Decls))
		case RuleMedia:
			s = fmt.Sprintf("@media %s{%s}", dumpTokens(r.Prelude), dumpRules(r.Rules))
		case RulePage:
			s = fmt.Sprintf("@page %s{%s", dumpTokens(r.Prelude), dumpDecls(r.Decls))
			if len(r.Rules) > 0 {
				s += "|" + dumpRules(r.Rules)
			}
			s += "}"
		case RuleMarginBox:
			s = fmt.Sprintf("@%s{%s}", r.Name, dumpDecls(r.Decls))
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func newBudget(cssBytes int64) *budget.Budget {
	l := budget.Defaults()
	l[budget.CSSBytes] = cssBytes
	return budget.New(context.Background(), l, nil)
}

// parseSheet parses src and returns the dump with the warnings as "code:detail".
func parseSheet(tb testing.TB, src string) (string, []string) {
	tb.Helper()
	b := newBudget(1 << 30)
	ss, err := ParseStylesheet([]byte(src), OriginDocument, b)
	if err != nil {
		tb.Fatal(err)
	}
	return dumpRules(ss.Rules), warnings(b)
}

func warnings(b *budget.Budget) []string {
	ws, _ := b.Warnings()
	if len(ws) == 0 {
		return nil
	}
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Code + ":" + w.Detail
	}
	return out
}
