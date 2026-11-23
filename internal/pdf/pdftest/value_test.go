package pdftest

import (
	"reflect"
	"testing"
)

func TestParserValues(t *testing.T) {
	tests := []struct {
		in   string
		want any
	}{
		{`(a\(b\)\\c(nested)\n)`, "a(b)\\c(nested)\n"},
		{`(\q)`, "q"},
		{`<48656C6C6F>`, "Hello"},
		{`/Name`, Name("Name")},
		{`12`, 12.0},
		{`-1.5`, -1.5},
		{`3 0 R`, Ref(3)},
		{`3 1`, 3.0},
		{`true`, true},
		{`false`, false},
		{`null`, nil},
		{`[1 2 (x) /N 4 0 R]`, []any{1.0, 2.0, "x", Name("N"), Ref(4)}},
		{`<< /A 1 /B [ ] >>`, Dict{"A": 1.0, "B": []any(nil)}},
	}
	for _, tt := range tests {
		p := &parser{data: []byte(tt.in)}
		got, err := p.value(0)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s = %#v, %v; want %#v", tt.in, got, err, tt.want)
		}
	}
}

func TestParserErrors(t *testing.T) {
	for _, in := range []string{
		``, `(abc`, `(abc\`, `<4>`, `<41`, `[1 2`, `<< /A >>`, `<< /A 1 /A 2 >>`, `<< 1 2 >>`,
		`abc`, `)`, `/N#41`, `<<`, `<< /A 1`,
		"[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]",
	} {
		p := &parser{data: []byte(in)}
		if v, err := p.value(0); err == nil {
			t.Errorf("%q accepted as %#v", in, v)
		}
	}
}

func TestDecodeText(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"abc", "abc", true},
		{"\xfe\xff\x00\xe9", "é", true},
		{"\xfe\xff\xd8\x3d\xde\x00", "😀", true},
		{"\xfe\xff\x00", "", false},
	}
	for _, tt := range tests {
		got, err := decodeText(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("decodeText(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestInflateLimits(t *testing.T) {
	if _, err := inflate([]byte("junk")); err == nil {
		t.Error("accepted junk")
	}
	if _, err := inflate([]byte{0x78, 0x9c, 0xff}); err == nil {
		t.Error("accepted truncated stream")
	}
}
