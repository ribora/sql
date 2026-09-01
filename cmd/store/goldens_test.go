// The golden-file suite for store. Each case under testdata/cases/<name>/ holds
// setup.go (input) plus the *_gen.go files store is expected to write next to it.
//
// A run generates into a copy of the case directory, so a plain `go test` never
// rewrites what it is comparing against; `-update` copies the result back.
package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the *_gen.go goldens instead of comparing")

var successCases = []string{
	"basic",
	"model_pkg",
	"major_version",
	"renamed_pkg",
	"grouped",
	"other_types",
	"import_order",
	"json_wrapper",
	"csv_with_json",
	"wrapping_pkg",
}

var errorCases = []struct {
	dir         string
	errContains string
}{
	{"err_embedded_tag", "an embedded field cannot carry a db tag"},
	{"err_shared_tag", "would map to one column"},
	{"err_bad_model", "must name the model type"},
}

func TestGoldens(t *testing.T) {
	for _, name := range successCases {
		t.Run(name, func(t *testing.T) {
			src := caseDir(t, name)
			tmp := copyTree(t, src)

			if err := generate(tmp, true); err != nil {
				t.Fatalf("generate: %v", err)
			}

			got := generatedFiles(t, tmp)
			if len(got) == 0 {
				t.Fatal("no *_gen.go was written")
			}
			if *update {
				for rel, content := range got {
					writeFile(t, filepath.Join(src, rel), content)
				}
				return
			}
			for rel, content := range got {
				want, err := os.ReadFile(filepath.Join(src, rel))
				if err != nil {
					t.Fatalf("read golden (run with -update to create): %v", err)
				}
				if !bytes.Equal(content, want) {
					t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", rel, content, want)
				}
			}
			for rel := range generatedFiles(t, src) {
				if _, ok := got[rel]; !ok {
					t.Errorf("%s is a stale golden: this run does not produce it", rel)
				}
			}
		})
	}
}

func TestErrors(t *testing.T) {
	for _, c := range errorCases {
		t.Run(c.dir, func(t *testing.T) {
			tmp := copyTree(t, caseDir(t, c.dir))
			err := generate(tmp, true)
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", c.errContains)
			}
			if !strings.Contains(err.Error(), c.errContains) {
				t.Errorf("error mismatch: got %v, want substring %q", err, c.errContains)
			}
			if files := generatedFiles(t, tmp); len(files) > 0 {
				t.Errorf("a file was written despite the error: %v", keys(files))
			}
		})
	}
}

// TestGoldens compares bytes and never builds what it generated, so a wrong
// import line passes it and still writes a file that does not compile. The go
// tool ignores testdata in wildcard patterns, hence the explicit import paths.
func TestGoldensCompile(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"build"}
	for _, name := range successCases {
		args = append(args, "gitlab.com/gorib/sql/cmd/store/testdata/cases/"+name)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compiling goldens: %v\n%s", err, out)
	}
}

func caseDir(t *testing.T, name string) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "cmd", "store", "testdata", "cases", name)
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// The whole subtree, so a case can keep its model in a package of its own — but
// not the goldens: copied along, a file the generator has stopped writing would
// still be sitting there, and the comparison would pass on it.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	// Under the module rather than in the system temp dir: resolving an import to
	// its package name asks the go tool, and outside a module it has no answer —
	// which is never the case where the generator actually runs.
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.MkdirTemp(filepath.Join(root, "cmd", "store", "testdata"), "run-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	// Left behind, it would sit under testdata for every later run.
	t.Cleanup(func() {
		if err := os.RemoveAll(dst); err != nil {
			t.Errorf("remove %s: %v", dst, err)
		}
	})

	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, "_gen.go") {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
	if err != nil {
		t.Fatalf("copy case: %v", err)
	}
	return dst
}

func generatedFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_gen.go") {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = content
		return nil
	})
	if err != nil {
		t.Fatalf("collect generated files: %v", err)
	}
	return out
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
