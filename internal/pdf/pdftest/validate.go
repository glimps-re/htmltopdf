package pdftest

import (
	"bytes"
	"context"
	"fmt"
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
