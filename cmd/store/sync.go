package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/imports"
)

type structSync struct {
	name    string
	missing []MissingField
}

// Without tags on purpose: which column it is, or that it is none, is the
// author's answer to give.
func syncFile(fileName string, work []structSync, pkgPath string) (bool, error) {
	var wanted []structSync
	for _, w := range work {
		if len(w.missing) > 0 {
			wanted = append(wanted, w)
		}
	}
	if len(wanted) == 0 {
		return false, nil
	}

	source, err := os.ReadFile(fileName)
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, source, parser.ParseComments)
	if err != nil {
		return false, err
	}

	printer := newTypePrinter(pkgPath, file)
	var edits []edit
	for _, w := range wanted {
		st := structType(file, w.name)
		if st == nil {
			return false, fmt.Errorf("%s: declaration vanished between load and edit", w.name)
		}
		// One blank line above the block, none inside it.
		block := strings.Builder{}
		block.WriteString("\n")
		for _, m := range w.missing {
			text, err := printer.print(m.Type)
			if err != nil {
				return false, fmt.Errorf("%s.%s: %w", w.name, m.Name, err)
			}
			fmt.Fprintf(&block, "%s %s\n", m.Name, text)
		}
		edits = append(edits, edit{at: fset.Position(st.Fields.Closing).Offset, text: block.String()})
	}

	importEdit, err := printer.importEdit(fset, file)
	if err != nil {
		return false, err
	}
	if importEdit != nil {
		edits = append(edits, *importEdit)
	}

	// Not gofmt: it only sorts within the group an import is already in.
	out, err := imports.Process(fileName, apply(source, edits), nil)
	if err != nil {
		return false, fmt.Errorf("%s: formatting the synchronised source: %w", fileName, err)
	}
	return true, os.WriteFile(fileName, out, 0o644)
}

// replaceTo past at makes the edit a replacement rather than an insertion.
type edit struct {
	at        int
	replaceTo int
	text      string
}

// Applied back to front, so an earlier offset is still the offset it was.
func apply(source []byte, edits []edit) []byte {
	sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	for _, e := range edits {
		tail := e.at
		if e.replaceTo > tail {
			tail = e.replaceTo
		}
		out := make([]byte, 0, len(source)+len(e.text))
		out = append(out, source[:e.at]...)
		out = append(out, e.text...)
		source = append(out, source[tail:]...)
	}
	return source
}

func structType(file *ast.File, name string) *ast.StructType {
	var found *ast.StructType
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != name {
			return true
		}
		if st, ok := spec.Type.(*ast.StructType); ok {
			found = st
		}
		return false
	})
	return found
}

// Spells a type for the stored struct's package, collecting the imports it needs.
type typePrinter struct {
	pkgPath string
	byPath  map[string]string
	byName  map[string]string
	added   []string
}

func newTypePrinter(pkgPath string, file *ast.File) *typePrinter {
	p := &typePrinter{
		pkgPath: pkgPath,
		byPath:  map[string]string{},
		byName:  map[string]string{},
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "" {
			// The clause is unknown without loading it; a wrong guess only ever
			// costs a redundant import line.
			name = path[strings.LastIndexByte(path, '/')+1:]
		}
		p.byPath[path] = name
		p.byName[name] = path
	}
	return p
}

func (p *typePrinter) print(t types.Type) (string, error) {
	if err := p.reachable(t, 0); err != nil {
		return "", err
	}
	var failure error
	text := types.TypeString(t, func(pkg *types.Package) string {
		if pkg.Path() == p.pkgPath {
			return ""
		}
		if name, ok := p.byPath[pkg.Path()]; ok {
			return name
		}
		if other, taken := p.byName[pkg.Name()]; taken && other != pkg.Path() {
			failure = fmt.Errorf("%q and %q would both be imported as %s", other, pkg.Path(), pkg.Name())
			return pkg.Name()
		}
		p.byPath[pkg.Path()] = pkg.Name()
		p.byName[pkg.Name()] = pkg.Path()
		p.added = append(p.added, pkg.Path())
		return pkg.Name()
	})
	return text, failure
}

// A type the package cannot name is not one that can be written into it.
func (p *typePrinter) reachable(t types.Type, depth int) error {
	if depth > 8 {
		return nil
	}
	switch x := t.(type) {
	case *types.Named:
		obj := x.Obj()
		if obj.Pkg() != nil && !obj.Exported() && obj.Pkg().Path() != p.pkgPath {
			return fmt.Errorf("type %s is not exported by %s", obj.Name(), obj.Pkg().Path())
		}
		for i := range x.TypeArgs().Len() {
			if err := p.reachable(x.TypeArgs().At(i), depth+1); err != nil {
				return err
			}
		}
	case *types.Pointer:
		return p.reachable(x.Elem(), depth+1)
	case *types.Slice:
		return p.reachable(x.Elem(), depth+1)
	case *types.Array:
		return p.reachable(x.Elem(), depth+1)
	case *types.Map:
		if err := p.reachable(x.Key(), depth+1); err != nil {
			return err
		}
		return p.reachable(x.Elem(), depth+1)
	case *types.Chan:
		return p.reachable(x.Elem(), depth+1)
	}
	return nil
}

func (p *typePrinter) importEdit(fset *token.FileSet, file *ast.File) (*edit, error) {
	if len(p.added) == 0 {
		return nil, nil
	}
	var lines strings.Builder
	for _, path := range p.added {
		fmt.Fprintf(&lines, "%q\n", path)
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		if gen.Lparen.IsValid() {
			return &edit{at: fset.Position(gen.Rparen).Offset, text: lines.String()}, nil
		}
		// A single unparenthesised import: reopen it as a block.
		var block strings.Builder
		block.WriteString("import (\n")
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			if imp.Name != nil {
				fmt.Fprintf(&block, "%s ", imp.Name.Name)
			}
			fmt.Fprintf(&block, "%s\n", imp.Path.Value)
		}
		block.WriteString(lines.String())
		block.WriteString(")")
		start := fset.Position(gen.Pos()).Offset
		end := fset.Position(gen.End()).Offset
		return &edit{at: start, text: block.String(), replaceTo: end}, nil
	}

	at := fset.Position(file.Name.End()).Offset
	return &edit{at: at, text: "\n\nimport (\n" + lines.String() + ")"}, nil
}
