package box

import (
	"strings"
	"testing"
)

type collect struct {
	parts []string
	brs   int
}

func (c *collect) text(s string)    { c.parts = append(c.parts, s) }
func (c *collect) lineBreak() error { c.brs++; return nil }

func TestProcessText(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		pre   bool
		start wsState
		want  []string
		brs   int
		end   wsState
	}{
		{"plain", "hello world", false, wsState{}, []string{"hello world"}, 0, wsState{}},
		{"trailing space is kept", "hello ", false, wsState{}, []string{"hello "}, 0, wsState{space: true}},
		{"collapses", "a  b\n\tc", false, wsState{}, []string{"a b c"}, 0, wsState{}},
		{"leading space dropped at the start of a line", " a", false, wsState{space: true}, []string{"a"}, 0, wsState{}},
		{"leading space kept after text", " a", false, wsState{}, []string{" a"}, 0, wsState{}},
		{"only white space at the start", " \n ", false, wsState{space: true}, nil, 0, wsState{space: true}},
		{"white space after a space", "  a", false, wsState{}, []string{" a"}, 0, wsState{}},
		{"nbsp is text", "a   b", false, wsState{}, []string{"a   b"}, 0, wsState{}},
		{"pre keeps spaces", "  a  b ", true, wsState{}, []string{"  a  b "}, 0, wsState{col: 7}},
		{"pre breaks lines", "a\nb\r\nc\rd", true, wsState{}, []string{"a", "b", "c", "d"}, 3, wsState{col: 1}},
		{"pre tab stops", "ab\tc", true, wsState{}, []string{"ab      c"}, 0, wsState{col: 9}},
		{"pre tab after a break", "a\n\tb", true, wsState{}, []string{"a", "        b"}, 1, wsState{col: 9}},
		{"pre multibyte columns", "é\tx", true, wsState{}, []string{"é       x"}, 0, wsState{col: 9}},
		{"empty", "", false, wsState{space: true}, nil, 0, wsState{space: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c collect
			st := tt.start
			if err := processText(tt.data, tt.pre, &st, &c); err != nil {
				t.Fatal(err)
			}
			if strings.Join(c.parts, "|") != strings.Join(tt.want, "|") || c.brs != tt.brs || st != tt.end {
				t.Fatalf("parts %q breaks %d state %+v; want %q %d %+v", c.parts, c.brs, st, tt.want, tt.brs, tt.end)
			}
		})
	}
}

func TestUnchangedTextIsNotCopied(t *testing.T) {
	data := strings.Repeat("word ", 100)
	for _, pre := range []bool{false, true} {
		var c collect
		allocs := testing.AllocsPerRun(10, func() {
			c.parts = c.parts[:0]
			st := wsState{}
			_ = processText(data, pre, &st, &c)
		})
		if allocs != 0 {
			t.Errorf("pre=%v: %v allocations for text that needs no change", pre, allocs)
		}
		if len(c.parts) != 1 || c.parts[0] != data {
			t.Errorf("pre=%v: text changed", pre)
		}
	}
}

func TestTabExpansionIsBounded(t *testing.T) {
	var c collect
	st := wsState{}
	data := strings.Repeat("\t", 1<<16)
	if err := processText(data, true, &st, &c); err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Join(c.parts, "")); got != tabSize*len(data) {
		t.Fatalf("%d bytes for %d tabs, want %d (8 per tab)", got, len(data), tabSize*len(data))
	}
}
