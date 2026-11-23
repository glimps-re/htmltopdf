package htmltopdf

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/budget"
	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

// maxWorkCeiling is the largest default MaxWork: at 20 ns per unit it bounds a
// render to about two seconds (design §6.2).
const maxWorkCeiling = 100_000_000

// The default MaxWork is at least 20 times the Work of every large report of
// the corpus, and at most maxWorkCeiling.
func TestMaxWorkCalibration(t *testing.T) {
	names, err := testcorpus.ReportNames(os.DirFS("../../testdata/templates"))
	if err != nil {
		t.Fatal(err)
	}
	opts := referenceOptions(t)
	opts.Limits.MaxWork = 1 << 40
	c := mustNew(t, opts)
	var worst int64
	for _, name := range names {
		for _, size := range []testcorpus.Size{testcorpus.Small, testcorpus.Large} {
			data, err := testcorpus.Report(os.DirFS("../../testdata/templates"), name, size)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			_, b, err := c.render(context.Background(), strings.NewReader(string(data)), &buf)
			if err != nil {
				t.Fatalf("%s/%s: %v", name, size, err)
			}
			used := b.Used(budget.Work)
			t.Logf("%s/%s: %d Work units", name, size, used)
			worst = max(worst, used)
		}
	}
	def := DefaultLimits().MaxWork
	t.Logf("corpus maximum %d, default MaxWork %d (%.0f times)", worst, def, float64(def)/float64(worst))
	if def < 20*worst {
		t.Errorf("default MaxWork %d is below 20 times the corpus maximum %d", def, worst)
	}
	if def > maxWorkCeiling {
		t.Errorf("default MaxWork %d exceeds %d", def, maxWorkCeiling)
	}
}
