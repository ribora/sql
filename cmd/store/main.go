package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

func main() {
	strict := flag.Bool("strict", false, "stop on a stored struct that names no default sort")
	flag.Parse()
	path := flag.Arg(0)
	if path == "" {
		path = "."
	}
	var needTree bool
	if strings.HasSuffix(path, "/...") {
		needTree = true
		path = path[:len(path)-4]
	}
	if err := generate(path, needTree, *strict); err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
}

func generate(path string, needTree bool, strict bool) error {
	byDir, err := targets(path, needTree)
	if err != nil {
		return err
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		if err := generateDir(dir, byDir[dir], strict); err != nil {
			return err
		}
	}
	return nil
}

// Once per directory: it is the package that is type checked, not the file.
func generateDir(dir string, files []string, strict bool) error {
	pkg, err := loadPackage(dir)
	if err != nil {
		return err
	}

	// Before the checks, so one pass names every missing field. See plan().
	added, err := synchronise(pkg, files)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		return fmt.Errorf("added to the stored struct, now give each a db tag or `store:\"-\"`:\n\t%s",
			strings.Join(added, "\n\t"))
	}

	for _, f := range files {
		packageName, lexems, err := parse(pkg, f, strict)
		if err != nil {
			return err
		}
		for _, lexem := range lexems {
			if err := write(dir, packageName, lexem); err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
		}
	}
	return nil
}

func synchronise(pkg *packages.Package, files []string) ([]string, error) {
	var added []string
	for _, f := range files {
		var work []structSync
		for _, st := range storedTypesIn(pkg, f) {
			missing, err := plan(pkg, st)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", f, st.name, err)
			}
			if len(missing) == 0 {
				continue
			}
			work = append(work, structSync{name: st.name, missing: missing})
			for _, m := range missing {
				added = append(added, fmt.Sprintf("%s: %s.%s", filepath.Base(f), st.name, m.Name))
			}
		}
		if _, err := syncFile(f, work, pkg.PkgPath); err != nil {
			return nil, err
		}
	}
	return added, nil
}

func targets(path string, needTree bool) (map[string][]string, error) {
	byDir := map[string][]string{}
	for f, err := range walk(path, needTree) {
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_gen.go") {
			continue
		}
		stored, err := declaresStored(f)
		if err != nil {
			return nil, err
		}
		if !stored {
			continue
		}
		dir := filepath.Dir(f)
		byDir[dir] = append(byDir[dir], f)
	}
	for dir := range byDir {
		sort.Strings(byDir[dir])
	}
	return byDir, nil
}

// Syntax only: a package with nothing to generate is never type checked, and so
// never reports someone else's broken build as this tool's error.
func declaresStored(fileName string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fileName, nil, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			if len(field.Names) == 1 && field.Names[0].Name == "_" {
				found = true
				return false
			}
		}
		return true
	})
	return found, nil
}
