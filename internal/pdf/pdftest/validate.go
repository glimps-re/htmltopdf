package pdftest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// ValidatePDFCPU runs pdfcpu's validator in strict mode on data.
func ValidatePDFCPU(data []byte) error {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		return fmt.Errorf("pdfcpu configuration: %w", err)
	}
	conf.ValidationMode = model.ValidationStrict
	if err := api.Validate(context.Background(), bytes.NewReader(data), conf, nil); err != nil {
		return fmt.Errorf("pdfcpu strict validation: %w", err)
	}
	return nil
}

// PDFCPUContent returns the decoded content stream of each page (1-based
// keys) as pdfcpu reads it, to cross-check the reader of this package.
func PDFCPUContent(data []byte) (map[int][]byte, error) {
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		return nil, fmt.Errorf("pdfcpu configuration: %w", err)
	}
	out := map[int][]byte{}
	digest := func(r io.Reader, page int) error {
		var buf bytes.Buffer
		n, err := buf.ReadFrom(io.LimitReader(r, maxInflated+1))
		if err != nil || n > maxInflated {
			return fmt.Errorf("reading page %d: %w (%d bytes)", page, err, n)
		}
		out[page] = buf.Bytes()
		return nil
	}
	if err := api.ExtractContent(context.Background(), bytes.NewReader(data), nil, digest, conf); err != nil {
		return nil, fmt.Errorf("pdfcpu content extraction: %w", err)
	}
	return out, nil
}

// Validate fails t unless data passes both Parse and ValidatePDFCPU, and
// returns the parsed document.
func Validate(tb testing.TB, data []byte) *Doc {
	tb.Helper()
	doc, err := Parse(data)
	if err != nil {
		tb.Fatalf("pdftest: %v", err)
	}
	if err := ValidatePDFCPU(data); err != nil {
		tb.Fatal(err)
	}
	return doc
}
