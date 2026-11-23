package htmltopdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type valueReader struct{ v any }

func (p valueReader) Read([]byte) (int, error) { panic(p.v) }

func TestPanicDetailIsBounded(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Render(context.Background(), valueReader{strings.Repeat("a\n", 1<<19)}, &bytes.Buffer{})
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) > 200 || strings.ContainsRune(err.Error(), '\n') {
		t.Errorf("unbounded or multi-line panic text: %d bytes", len(err.Error()))
	}
}

func TestCanceledContextStopsReaderAndWriter(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Render(ctx, strings.NewReader("<p>x"), &bytes.Buffer{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestStyleMediaAttribute(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		media    string
		applies  bool
		warnings bool
	}{
		{"", true, false},
		{"print", true, false},
		{"all", true, false},
		{"screen", false, false},
		{"(min-width: 99999px)", false, false},
		{"((", false, true},
		{"print{}", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.media, func(t *testing.T) {
			var buf bytes.Buffer
			html := `<style media="` + tt.media + `">p{display:none}</style><p>x`
			res, err := c.Render(context.Background(), strings.NewReader(html), &buf)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(res.Warnings) > 0; got != tt.warnings {
				t.Errorf("warnings = %v: %v", res.Warnings, tt.warnings)
			}
			var plain bytes.Buffer
			if _, err := c.Render(context.Background(), strings.NewReader("<p>x"), &plain); err != nil {
				t.Fatal(err)
			}
			if same := buf.Len() == plain.Len(); same == tt.applies && tt.applies {
				t.Errorf("sheet did not apply: output as large as the plain document")
			}
		})
	}
}

func TestFontCapsAreCheckedBeforeParsing(t *testing.T) {
	_, err := New(Options{Fonts: make([]Font, 65)})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("65 fonts: %v", err)
	}
	_, err = New(Options{Fonts: []Font{{Data: make([]byte, 33<<20), Family: "x"}}})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("33 MiB font: %v", err)
	}
}
