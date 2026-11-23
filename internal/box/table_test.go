package box

import (
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
			"columns are dropped",
			`<table><colgroup><col><col></colgroup><tr><td>a</table>`,
			`block table
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
