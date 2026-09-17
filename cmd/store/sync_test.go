package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The run fails on the missing tag, and the field is on disk regardless.
func TestSyncWritesTheMissingField(t *testing.T) {
	tmp := copyTree(t, caseDir(t, "unmapped_field"))

	if err := generate(tmp, true); err == nil {
		t.Fatal("expected the missing tag to fail the run")
	}

	source := readFile(t, filepath.Join(tmp, "setup.go"))
	if !strings.Contains(source, "Lines []string") {
		t.Errorf("the model field was not written in:\n%s", source)
	}
	if files := generatedFiles(t, tmp); len(files) > 0 {
		t.Errorf("a _gen.go was written despite the error: %v", keys(files))
	}
}

func TestSyncAddsTheImportTheTypeNeeds(t *testing.T) {
	tmp := copyTree(t, caseDir(t, "unmapped_import"))

	if err := generate(tmp, true); err == nil {
		t.Fatal("expected the missing tags to fail the run")
	}

	source := readFile(t, filepath.Join(tmp, "setup.go"))
	// Column alignment is gofmt's business, so compare on collapsed whitespace.
	flat := strings.Join(strings.Fields(source), " ")
	for _, want := range []string{`"time"`, "At time.Time", "Kind model.Status"} {
		if !strings.Contains(flat, want) {
			t.Errorf("%q missing from the synchronised source:\n%s", want, source)
		}
	}
}

// Tagging the field is the whole fix: a stopped run leaves nothing to undo.
func TestSyncSettlesOnTheSecondRun(t *testing.T) {
	tmp := copyTree(t, caseDir(t, "unmapped_field"))

	if err := generate(tmp, true); err == nil {
		t.Fatal("expected the missing tag to fail the run")
	}
	setup := filepath.Join(tmp, "setup.go")
	// The last one: the model declares a field of the same name and type.
	source := readFile(t, setup)
	at := strings.LastIndex(source, "Lines []string")
	source = source[:at] + "Lines []string `store:\"-\"`" + source[at+len("Lines []string"):]
	writeFile(t, setup, []byte(source))

	if err := generate(tmp, true); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if files := generatedFiles(t, tmp); len(files) != 1 {
		t.Errorf("expected one generated file, got %v", keys(files))
	}
}

// The cycle the build tag exists to break: output predating the tag is still
// compiled in, and broken output would block the run that replaces it.
func TestRegeneratesOverBrokenUntaggedOutput(t *testing.T) {
	tmp := copyTree(t, caseDir(t, "model_pkg"))

	stale := filepath.Join(tmp, "stored_order_gen.go")
	writeFile(t, stale, []byte(generatedHeader+`
package model_pkg

func newStoredOrder(m *model.Order) *StoredOrder {
	return &StoredOrder{Gone: m.Gone,
`))

	if err := generate(tmp, true); err != nil {
		t.Fatalf("generate over broken output: %v", err)
	}
	got := readFile(t, stale)
	if !strings.HasPrefix(got, "//go:build !store") {
		t.Errorf("the replacement carries no build guard:\n%s", got)
	}
	if strings.Contains(got, "Gone") {
		t.Errorf("the broken output survived:\n%s", got)
	}
}

// Output of a renamed struct names a type that is gone. Under the tag it is not
// in the package at all; deleting it stays the author's job.
func TestStaleOutputOfARenamedStructIsIgnored(t *testing.T) {
	tmp := copyTree(t, caseDir(t, "model_pkg"))

	writeFile(t, filepath.Join(tmp, "stored_gone_gen.go"), []byte("//go:build !store\n\n"+generatedHeader+`
package model_pkg

func (m *StoredGone) Returns() []string { return nil }
`))

	if err := generate(tmp, true); err != nil {
		t.Fatalf("generate beside stale output: %v", err)
	}
	if _, ok := generatedFiles(t, tmp)["stored_order_gen.go"]; !ok {
		t.Error("the struct that is still there was not regenerated")
	}
}

// A file this generator does not own is not one whose syntax errors it may skip.
func TestBrokenSourceStillFailsTheRun(t *testing.T) {
	tmp := copyTree(t, caseDir(t, "model_pkg"))
	writeFile(t, filepath.Join(tmp, "broken.go"), []byte("package model_pkg\n\nfunc Broken() {\n"))

	if err := generate(tmp, true); err == nil {
		t.Fatal("expected the syntax error to fail the run")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
