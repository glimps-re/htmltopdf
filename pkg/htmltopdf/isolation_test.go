package htmltopdf

import (
	"go/build"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The library never reaches the network, runs a program or opens a file
// (design §8.1): no package it links imports net, net/http or os/exec.
func TestNoNetworkOrExecDependency(t *testing.T) {
	forbidden := map[string]bool{"net": true, "net/http": true, "os/exec": true, "plugin": true}
	seen := map[string]bool{}
	var walk func(path, dir string)
	walk = func(path, dir string) {
		if seen[path] || path == "C" || path == "unsafe" {
			return
		}
		seen[path] = true
		if forbidden[path] {
			t.Errorf("the library links %s", path)
			return
		}
		p, err := build.Import(path, dir, 0)
		if err != nil {
			t.Fatalf("import %s: %v", path, err)
		}
		for _, imp := range p.Imports {
			walk(imp, p.Dir)
		}
	}
	walk("github.com/glimps-re/htmltopdf/pkg/htmltopdf", ".")
	if len(seen) < 20 {
		t.Fatalf("walked only %d packages", len(seen))
	}
}

var fileAccess = regexp.MustCompile(`\bos\.(Open|OpenFile|ReadFile|Create|CreateTemp|ReadDir|Stat|Lstat|Mkdir|MkdirAll|Remove|RemoveAll|Rename|Getenv|OpenRoot)\b|\bioutil\.`)

// No non-test code of the library touches the file system: fonts and
// stylesheets arrive as bytes.
func TestNoFileAccessInLibraryCode(t *testing.T) {
	root, err := os.OpenRoot("../..")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	fsys := root.FS()
	for _, dir := range []string{"pkg", "internal"} {
		err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Test-only packages read test data.
				if d.Name() == "testcorpus" || d.Name() == "pdftest" || d.Name() == "testdata" {
					return fs.SkipDir
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
			if m := fileAccess.Find(src); m != nil {
				t.Errorf("%s uses %s", path, m)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
