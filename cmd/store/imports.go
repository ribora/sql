package main

import (
	"fmt"
	"go/ast"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// A wrong guess from the path used to emit an empty import, hence the fallback.
type importSet struct {
	dir      string
	explicit map[string]string
	guessed  map[string]string
	asked    bool
}

func newImportSet(fileName string, f *ast.File) *importSet {
	s := &importSet{
		dir:      filepath.Dir(fileName),
		explicit: map[string]string{},
		guessed:  map[string]string{},
	}
	for _, spec := range f.Imports {
		if spec.Name != nil {
			s.explicit[spec.Name.Name] = spec.Path.Value
			continue
		}
		for _, name := range pathCandidates(strings.Trim(spec.Path.Value, `"`)) {
			s.guessed[name] = spec.Path.Value
		}
	}
	return s
}

// A trailing vN may be the major version or the package name (k8s.io/api/core/v1).
func pathCandidates(p string) []string {
	base := path.Base(p)
	out := []string{base}
	if looksLikeMajorVersion(base) {
		if parent := path.Base(path.Dir(p)); parent != "." && parent != "/" {
			out = append(out, parent)
		}
	}
	return out
}

func looksLikeMajorVersion(s string) bool {
	digits, ok := strings.CutPrefix(s, "v")
	if !ok || digits == "" {
		return false
	}
	return strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// path returns the quoted import path for a qualifier as written in the source.
func (s *importSet) path(qualifier string) (string, error) {
	if p, ok := s.explicit[qualifier]; ok {
		return p, nil
	}
	if p, ok := s.guessed[qualifier]; ok {
		return p, nil
	}
	if !s.asked {
		s.asked = true
		if err := s.resolveNames(); err != nil {
			return "", fmt.Errorf("resolving %q: %w", qualifier, err)
		}
		return s.path(qualifier)
	}
	return "", fmt.Errorf("no import in this file provides %q", qualifier)
}

// NeedName and no more: loading types would pull in this generator's own output.
func (s *importSet) resolveNames() error {
	paths := make([]string, 0, len(s.guessed))
	for _, quoted := range s.guessed {
		paths = append(paths, strings.Trim(quoted, `"`))
	}
	if len(paths) == 0 {
		return nil
	}
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName, Dir: s.dir}, paths...)
	if err != nil {
		return fmt.Errorf("go list in %s: %w", s.dir, err)
	}
	var failed []string
	for _, pkg := range pkgs {
		if pkg.Name != "" && pkg.PkgPath != "" {
			s.guessed[pkg.Name] = `"` + pkg.PkgPath + `"`
			continue
		}
		for _, e := range pkg.Errors {
			failed = append(failed, e.Msg)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("go list in %s: %s", s.dir, strings.Join(failed, "; "))
	}
	return nil
}
