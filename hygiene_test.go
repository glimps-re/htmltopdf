package htmltopdf_test

import (
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glimps-re/htmltopdf/internal/testcorpus"
)

const modulePath = "github.com/glimps-re/htmltopdf"

var licenseTexts = []string{"LICENSE-Liberation.txt", "LICENSE-DejaVu.txt"}

// testOnlyImports lists the packages that only test files may import, and the
// directories (slash-separated) allowed to import them from non-test files.
var testOnlyImports = map[string][]string{
	modulePath + "/internal/testcorpus":  {"internal/testcorpus"},
	modulePath + "/internal/pdf/pdftest": {"internal/pdf/pdftest"},
	"github.com/pdfcpu/pdfcpu":           {"internal/pdf/pdftest"},
}

func isTestOnlyImport(path string) (allowed []string, ok bool) {
	for prefix, dirs := range testOnlyImports {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return dirs, true
		}
	}
	return nil, false
}

func TestLibraryCodeIsTestFree(t *testing.T) {
	root, err := os.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	fsys := root.FS()
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == d.Name() {
				switch path {
				case "testdata", "tmp", "bin", "docs", ".git":
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "testdata") {
			t.Errorf("%s references testdata: test fonts and fixtures are test-only", path)
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if allowed, ok := isTestOnlyImport(p); ok && !slices.Contains(allowed, dir) {
				t.Errorf("%s imports %s, which only test files may import", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTestFontsMatchLicenses(t *testing.T) {
	doc, err := os.ReadFile("LICENSES.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(string(doc), "\n")
	dir := filepath.Join("testdata", "fonts")

	want := slices.Clone(licenseTexts)
	for _, f := range testcorpus.HostConnectorFontFiles() {
		want = append(want, f.Name)
		sum := fileSHA256(t, filepath.Join(dir, f.Name))
		if !slices.ContainsFunc(rows, func(r string) bool {
			return strings.Contains(r, "`"+f.Name+"`") && strings.Contains(r, sum)
		}) {
			t.Errorf("%s: no LICENSES.md row names it with its SHA-256 %s", f.Name, sum)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("testdata/fonts holds %v, want %v", got, want)
	}
	for _, name := range licenseTexts {
		if st, err := os.Stat(filepath.Join(dir, name)); err != nil || st.Size() == 0 {
			t.Errorf("%s missing or empty: %v", name, err)
		}
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
