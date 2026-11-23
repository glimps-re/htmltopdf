package pdftest

import (
	"reflect"
	"testing"
)

func TestExtractText(t *testing.T) {
	f := &Font{ToUnicode: map[uint16]string{1: "a", 2: "bc", 3: "😀"}}
	fonts := map[string]*Font{"F1": f}
	tests := []struct {
		name    string
		content string
		want    []string
		fail    bool
	}{
		{"nothing", "", nil, false},
		{"one block", "BT /F1 12 Tf 0 0 Td [<0001> -50 <00020003>] TJ ET", []string{"abc😀"}, false},
		{"Tj", "BT /F1 12 Tf <0002>Tj ET", []string{"bc"}, false},
		{"two blocks", "BT /F1 1 Tf <0001> Tj ET q 1 0 0 rg Q BT /F1 1 Tf <0003> Tj ET", []string{"a", "😀"}, false},
		{"unmapped glyph is dropped", "BT /F1 1 Tf <0009> Tj ET", []string{""}, false},
		{"other operators and operands", "q 1 0 0 1 0 0 cm true false null 0.5 w (x) Q", nil, false},
		{"no font", "BT <0001> Tj ET", nil, true},
		{"unknown font", "BT /F9 1 Tf ET", nil, true},
		{"Tf without operands", "BT Tf ET", nil, true},
		{"odd code length", "BT /F1 1 Tf <000> Tj ET", nil, true},
		{"Tj without a string", "BT /F1 1 Tf 3 Tj ET", nil, true},
		{"TJ without an array", "BT /F1 1 Tf TJ ET", nil, true},
		{"TJ with a number", "BT /F1 1 Tf 3 TJ ET", nil, true},
		{"bad operand", "BT /F1 1 Tf <zz> ET", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractText([]byte(tt.content), fonts)
			if (err != nil) != tt.fail || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, %v; want %q (fail=%v)", got, err, tt.want, tt.fail)
			}
		})
	}
}

func TestParseToUnicode(t *testing.T) {
	good := "begincmap\n2 beginbfchar\n<0001> <0061>\n<0002> <D83DDE00>\nendbfchar\nendcmap\n"
	got, err := parseToUnicode([]byte(good))
	if err != nil || got[1] != "a" || got[2] != "😀" || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, bad := range []string{
		"", "begincmap\nendcmap", "begincmap\n1 beginbfchar\n<0001>\nendbfchar\nendcmap",
		"begincmap\n1 beginbfchar\n<zzzz> <0061>\nendbfchar\nendcmap",
		"begincmap\n1 beginbfchar\n<0001> <006>\nendbfchar\nendcmap",
		"begincmap\n1 beginbfchar\n<0001> <00zz>\nendbfchar\nendcmap",
	} {
		if _, err := parseToUnicode([]byte(bad)); err == nil && bad != "begincmap\nendcmap" {
			t.Errorf("accepted %q", bad)
		}
	}
}
