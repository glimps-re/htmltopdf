package box

import (
	"strconv"
	"strings"
	"testing"
)

func TestTableFixUp(t *testing.T) {
	tests := []struct {
		name, html, want string
	}{
		{
			"proper table",
			`<table><thead><tr><th>h</thead><tbody><tr><td>a<td>b</table>`,
			`block table
  block thead
    block tr
      block th
        text "h"
  block tbody
    block tr
      block td
        text "a"
      block td
        text "b"`,
		},
		{
			"text and block directly in a table become an anonymous row and cell",
			`<div style="display:table">x<div>y</div>z</div>`,
			`block div
  anon-block
    anon-block
      anon-block
        text "x"
      block div
        text "y"
      anon-block
        text "z"`,
		},
		{
			"cells directly in a table get an anonymous row",
			`<div style="display:table"><div style="display:table-cell">a</div><div style="display:table-cell">b</div></div>`,
			`block div
  anon-block
    block div
      text "a"
    block div
      text "b"`,
		},
		{
			"rows directly in a group and loose text in a row",
			`<div style="display:table"><div style="display:table-row-group"><div style="display:table-row">t<div style="display:table-cell">c</div></div></div></div>`,
			`block div
  block div
    block div
      anon-block
        anon-block
          text "t"
      block div
        text "c"`,
		},
		{
			"columns are kept for layout",
			`<table><colgroup><col><col span=3></colgroup><tr><td>a</table>`,
			`block table
  block colgroup
    block col
    block col colspan=3
  block tbody
    block tr
      block td
        text "a"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := buildHTML(t, tc.html)
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestCellSpans(t *testing.T) {
	for _, tc := range []struct {
		attr string
		span int
		warn bool
	}{
		{"", 1, false},
		{"colspan=3", 3, false},
		{"colspan=0", 1, false},
		{"colspan=-4", 1, false},
		{"colspan=abc", 1, false},
		{"colspan=1000", 1000, false},
		{"colspan=1001", 1000, true},
		{"colspan=99999999999999999999", 1000, true},
		{"colspan=0000000000000000000000002", 2, false},
		{`colspan=" 7x"`, 7, false},
	} {
		html := "<table><tr><td " + tc.attr + ">a</table>"
		got, b := buildHTML(t, html)
		want := ""
		if tc.span > 1 {
			want = " colspan=" + itoa(tc.span)
		}
		if !strings.Contains(got, "block td"+want+"\n") {
			t.Errorf("%q: got\n%s", tc.attr, got)
		}
		if has := strings.Contains(strings.Join(warnings(b), ","), "clamped:colspan"); has != tc.warn {
			t.Errorf("%q: warning %v, want %v", tc.attr, has, tc.warn)
		}
	}
}

func TestRowspanRenderedAsOne(t *testing.T) {
	got, b := buildHTML(t, `<table><tr><td rowspan=3>a<tr><td>b</table>`)
	if strings.Contains(got, "rowspan") {
		t.Fatal(got)
	}
	if !strings.Contains(strings.Join(warnings(b), ","), "clamped:rowspan") {
		t.Fatalf("warnings %v", warnings(b))
	}
	if _, b := buildHTML(t, `<table><tr><td rowspan=1>a</table>`); len(warnings(b)) != 0 {
		t.Fatalf("rowspan=1 warned: %v", warnings(b))
	}
}

func itoa(n int) string {
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

func TestOrphanCellGetsAnonymousParents(t *testing.T) {
	got, _ := buildHTML(t, `<div><span style="display:table-cell">a</span><span style="display:table-cell">b</span>text</div>`)
	want := `block div
  anon-block
    anon-block
      block span
        text "a"
      block span
        text "b"
  anon-block
    text "text"`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestBlankBetweenTablePartsIsDroppedUnderPre(t *testing.T) {
	got, _ := buildHTML(t, "<table style=\"white-space:pre\">\n<tr>\n<td>a</td>\n<td>b</td>\n</tr>\n</table>")
	want := `block table
  block tbody
    block tr
      block td
        text "a"
      block td
        text "b"`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	got, _ = buildHTML(t, `<div style="display:table;white-space:pre"><div style="display:table-row"><div style="display:table-cell">a</div> x </div></div>`)
	if !strings.Contains(got, `text " x "`) {
		t.Fatalf("text between cells must stay:\n%s", got)
	}
}

func TestOrphanPartsSeparatedByBlankShareOneTable(t *testing.T) {
	got, _ := buildHTML(t, "<div style=\"white-space:pre\"><span style=\"display:table-cell\">a</span>\n<span style=\"display:table-cell\">b</span></div>")
	if n := strings.Count(got, "anon-block\n"); n != 2 { // one table, one row
		t.Fatalf("want one anonymous table and one row:\n%s", got)
	}
	if strings.Count(got, "block span") != 2 {
		t.Fatalf("cells lost:\n%s", got)
	}
	// Blank after the last part stays after the table.
	got, _ = buildHTML(t, "<div style=\"white-space:pre\"><span style=\"display:table-cell\">a</span>\n</div>")
	if !strings.Contains(got, "br") {
		t.Fatalf("trailing blank dropped:\n%s", got)
	}
}

func TestOrphanColumnsAreLeftAlone(t *testing.T) {
	got, _ := buildHTML(t, `<div><span style="display:table-column"></span><span style="display:table-column"></span>x</div>`)
	if strings.Count(got, "anon-block") != 1 || !strings.Contains(got, `text "x"`) {
		t.Fatalf("columns alone must not make an anonymous table:\n%s", got)
	}
}

func TestColumnSpanAttribute(t *testing.T) {
	for _, tc := range []struct {
		attr string
		want string
		warn bool
	}{
		{"", "", false},
		{"span=3", " colspan=3", false},
		{"span=0", "", false},
		{"span=1001", " colspan=1000", true},
		{"span=1000", " colspan=1000", false},
		{"span=junk", "", false},
	} {
		got, b := buildHTML(t, "<table><colgroup><col "+tc.attr+"></colgroup><tr><td>a</table>")
		if !strings.Contains(got, "block col"+tc.want+"\n") {
			t.Errorf("%q:\n%s", tc.attr, got)
		}
		if has := strings.Contains(strings.Join(warnings(b), ","), "clamped:span"); has != tc.warn {
			t.Errorf("%q: warning %v, want %v", tc.attr, has, tc.warn)
		}
	}
}

func TestOverflowCellsAreFoldedIntoTheLastKeptCell(t *testing.T) {
	got, b := buildHTML(t, `<table><tr><td colspan=1000>decoy</td><td>one <b>x</b></td><td></td><td><p>two</p></td></tr></table>`)
	want := `block table
  block tbody
    block tr
      block td colspan=1000
        anon-block
          text "decoy"
        anon-block
          text "one "
          inline b
            text "x"
        anon-block
          block p
            text "two"`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	ws, _ := b.Warnings()
	if len(ws) != 1 || ws[0].Detail != ClampedColumns {
		t.Errorf("warnings %v, want one %q", ws, ClampedColumns)
	}
}

func TestRowWithinTheLimitIsNotFolded(t *testing.T) {
	got, b := buildHTML(t, `<table><tr><td colspan=999>a</td><td>b</td></tr></table>`)
	if strings.Contains(got, "anon-block") {
		t.Errorf("a row of 1000 columns was folded:\n%s", got)
	}
	if ws, _ := b.Warnings(); len(ws) != 0 {
		t.Errorf("warnings %v", ws)
	}
}

func countTextAndAbs(b *Box) (texts, outOfFlow int) {
	if b.Kind == KindText {
		texts++
	}
	outOfFlow += len(b.OutOfFlow)
	for _, c := range append(append([]*Box(nil), b.Children...), b.OutOfFlow...) {
		ct, co := countTextAndAbs(c)
		texts, outOfFlow = texts+ct, outOfFlow+co
	}
	return texts, outOfFlow
}

// Folding the cells past column 1000 loses no text box and no absolute box, and
// keeps the id that links point to.
func TestFoldKeepsEveryBoxAndTheID(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<table><tr>")
	for i := range 1003 {
		sb.WriteString(`<td id="c` + strconv.Itoa(i) + `">t` + strconv.Itoa(i) + `<span style="position:absolute">a` + strconv.Itoa(i) + `</span></td>`)
	}
	sb.WriteString("</tr></table>")
	got, _ := buildHTML(t, sb.String())
	for _, id := range []string{"#c0", "#c999", "#c1000", "#c1002"} {
		if strings.Count(got, id+"\n") != 1 {
			t.Errorf("id %s not kept once", id)
		}
	}
	b := testBudget(nil)
	d := parseDoc(t, sb.String(), b)
	root, err := Build(d, newStyler(t, d, b), b)
	if err != nil {
		t.Fatal(err)
	}
	texts, abs := countTextAndAbs(root)
	if texts != 2*1003 || abs != 1003 {
		t.Errorf("%d text boxes and %d absolute boxes, want %d and 1003", texts, abs, 2*1003)
	}
}
