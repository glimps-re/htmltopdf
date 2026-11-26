package htmltopdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/pdf"
	"github.com/glimps-re/htmltopdf/internal/pdf/pdftest"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// The signatures of design §4, frozen.
var (
	_ func(Options) (*Converter, error)                                        = New
	_ func(*Converter, context.Context, io.Reader, io.Writer) (*Result, error) = (*Converter).Render
	_ func() Limits                                                            = DefaultLimits
	_ func(*LimitError) string                                                 = (*LimitError).Error
	_ func(*LimitError, error) bool                                            = (*LimitError).Is
	_ error                                                                    = (*LimitError)(nil)
	_                                                                          = [...]error{ErrInvalidOptions, ErrLimit, ErrInternal}
	_ [11]Limit                                                                = [...]Limit{LimitInputBytes, LimitNodes, LimitCSSBytes, LimitSelectors, LimitStyles, LimitBoxes, LimitGlyphs, LimitPages, LimitOutputBytes, LimitWork, LimitHTMLDepth}
	_ [11]WarningCode                                                          = [...]WarningCode{WarnUnsupportedProperty, WarnInvalidValue, WarnUnsupportedAtRule, WarnUnsupportedSelector, WarnUnsupportedElement, WarnBlockedLink, WarnMissingGlyph, WarnClamped, WarnVarBudget, WarnShapingFallback, WarnImageRejected}
)

func TestClassify(t *testing.T) {
	c := mustNew(t, Options{Limits: Limits{MaxOutputBytes: 1234}})
	tests := []struct {
		name string
		err  error
		want func(error) bool
	}{
		{"nil", nil, func(e error) bool { return e == nil }},
		{"limit", &budget.LimitError{Kind: budget.Boxes, Max: 7}, func(e error) bool {
			var le *LimitError
			return errors.As(e, &le) && le.Limit == LimitBoxes && le.Max == 7
		}},
		{"wrapped limit", fmt.Errorf("x: %w", &budget.LimitError{Kind: budget.HTMLDepth, Max: 512}), func(e error) bool {
			var le *LimitError
			return errors.As(e, &le) && le.Limit == LimitHTMLDepth
		}},
		{"content size", fmt.Errorf("page: %w", pdf.ErrContentSize), func(e error) bool {
			var le *LimitError
			return errors.As(e, &le) && le.Limit == LimitOutputBytes && le.Max == pdf.MaxContentBytes
		}},
		{"canceled", fmt.Errorf("x: %w", context.Canceled), func(e error) bool { return errors.Is(e, context.Canceled) }},
		{"deadline", context.DeadlineExceeded, func(e error) bool { return errors.Is(e, context.DeadlineExceeded) }},
		{"pdf invariant", pdf.ErrNumber, func(e error) bool { return errors.Is(e, ErrInternal) && errors.Is(e, pdf.ErrNumber) }},
		{"unknown", errors.New("boom"), func(e error) bool { return errors.Is(e, ErrInternal) }},
		{"var bytes is not an error", &budget.LimitError{Kind: budget.VarBytes, Max: 1}, func(e error) bool { return errors.Is(e, ErrInternal) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.classify(tt.err, &errReader{}, &errWriter{}); !tt.want(got) {
				t.Errorf("classify(%v) = %v", tt.err, got)
			}
		})
	}
}

func TestCorpusReportsRenderAndValidate(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	c := mustNew(t, referenceOptions(t))
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			t.Run(name+"/"+string(size), func(t *testing.T) {
				data, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
				if err != nil {
					t.Fatal(err)
				}
				res, pdfBytes := renderBytes(t, c, string(data))
				doc := pdftest.Validate(t, pdfBytes)
				if res.Pages != len(doc.Pages) || res.Pages < 1 {
					t.Errorf("%d pages in the result, %d in the document", res.Pages, len(doc.Pages))
				}
				if !strings.Contains(pagesText(doc), "a1b2c3d4") {
					t.Errorf("the report text is missing from the PDF")
				}
				t.Logf("%d pages, %d bytes, %d warnings", res.Pages, res.Bytes, len(res.Warnings))
			})
		}
	}
}

// The public warning codes are the internal ones, string for string.
func TestWarningCodesMatchTheBudgetCodes(t *testing.T) {
	public := []WarningCode{
		WarnUnsupportedProperty, WarnInvalidValue, WarnUnsupportedAtRule, WarnUnsupportedSelector, WarnUnsupportedElement,
		WarnBlockedLink, WarnMissingGlyph, WarnClamped, WarnVarBudget, WarnShapingFallback, WarnImageRejected,
	}
	internal := map[string]bool{}
	for _, c := range budget.WarningCodes() {
		internal[string(c)] = true
	}
	if len(public) != len(internal) {
		t.Fatalf("%d public codes, %d internal ones", len(public), len(internal))
	}
	for _, c := range public {
		if !internal[string(c)] {
			t.Errorf("public code %q is not an internal one", c)
		}
	}
}
