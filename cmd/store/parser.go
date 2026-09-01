package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

const (
	// A hash, so the alias cannot collide with anything the author imported.
	sqlWrapper = "c87d7b66130cd9245a1d3f5dde49683a"
	// A csv field needs it even when the source file imports no sql package.
	defaultSqlImport = `"gitlab.com/gorib/sql"`
)

var (
	dbTagRE    = regexp.MustCompile(`db:"([^"]*)"`)
	storeTagRE = regexp.MustCompile(`store:"([^"]*)"`)
)

type Lexem struct {
	Name         string
	Fields       []*LexemField
	Model        string
	Module       string
	Pointer      string
	Imports      []string
	UpdateGroups []string
	InsertGroups []string
	NoNew        bool
	NoUnwrap     bool

	// One path per struct: two aliases for the same package would not build.
	sqlPath string
}

func (m *Lexem) addImport(module string) {
	for _, mod := range m.Imports {
		if module == mod {
			return
		}
	}
	m.Imports = append(m.Imports, module)
}

func (m *Lexem) addUpdateGroup(group string) {
	for _, g := range m.UpdateGroups {
		if g == group {
			return
		}
	}
	m.UpdateGroups = append(m.UpdateGroups, group)
}

func (m *Lexem) addInsertGroup(group string) {
	for _, g := range m.InsertGroups {
		if g == group {
			return
		}
	}
	m.InsertGroups = append(m.InsertGroups, group)
}

type LexemField struct {
	Name         string
	Source       string
	Field        string
	Readonly     bool
	Wrapped      bool
	Convert      string
	UpdateGroups []string
	InsertGroups []string
}

func parse(fileName string) (string, []*Lexem, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fileName, nil, parser.ParseComments)
	if err != nil {
		return "", nil, err
	}

	imports := newImportSet(fileName, f)

	var structs []*Lexem
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		// A grouped `type (...)` block used to generate the first and drop the rest.
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			// `type Status int` in the same file is legitimate; asserting here crashed.
			structSpec, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			lexem, err := parseStruct(typeSpec.Name.Name, structSpec, imports)
			if err != nil {
				return "", nil, fmt.Errorf("%s: %w", fileName, err)
			}
			if lexem != nil {
				structs = append(structs, lexem)
			}
		}
	}

	return f.Name.Name, structs, nil
}

// `_` gets a pass of its own: in source order, a convert:csv field declared above
// it dereferenced a nil lexem.
func parseStruct(name string, st *ast.StructType, imports *importSet) (*Lexem, error) {
	var lexem *Lexem
	for _, field := range st.Fields.List {
		if len(field.Names) != 1 || field.Names[0].Name != "_" {
			continue
		}
		var err error
		if lexem, err = modelLexem(name, field, imports); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		break
	}
	// No `_` field: not a stored struct, nothing to generate.
	if lexem == nil {
		return nil, nil
	}

	for _, field := range st.Fields.List {
		f, err := parseField(field, lexem, imports)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if f != nil {
			lexem.Fields = append(lexem.Fields, f)
		}
	}
	if lexem.sqlPath != "" {
		lexem.addImport(sqlWrapper + " " + lexem.sqlPath)
	}
	return lexem, nil
}

const modelHint = "the _ field must name the model type, as in `_ *model.User`"

func modelLexem(name string, field *ast.Field, imports *importSet) (*Lexem, error) {
	typ := field.Type
	pointer := ""
	if star, ok := typ.(*ast.StarExpr); ok {
		pointer = "*"
		typ = star.X
	}

	var module, model string
	switch x := typ.(type) {
	case *ast.SelectorExpr:
		ident, ok := x.X.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("%s", modelHint)
		}
		module, model = ident.Name, x.Sel.Name
	case *ast.Ident:
		model = x.Name
	default:
		return nil, fmt.Errorf("%s", modelHint)
	}

	lexem := &Lexem{Name: name, Model: model, Pointer: pointer}
	if field.Tag != nil {
		for _, part := range storeTagParts(field.Tag.Value) {
			switch part {
			case "nonew":
				lexem.NoNew = true
			case "nounwrap":
				lexem.NoUnwrap = true
			}
		}
	}
	if module != "" {
		path, err := imports.path(module)
		if err != nil {
			return nil, err
		}
		lexem.Module = module
		lexem.addImport(module + " " + path)
	}
	return lexem, nil
}

// A nil field is one this struct does not store: no db tag, or store:"-".
func parseField(field *ast.Field, lexem *Lexem, imports *importSet) (*LexemField, error) {
	if field.Tag == nil || field.Tag.Value == "" {
		return nil, nil
	}
	db := dbTagRE.FindStringSubmatch(field.Tag.Value)
	store := storeTagRE.FindStringSubmatch(field.Tag.Value)
	if len(db) != 2 || len(store) == 2 && store[1] == "-" {
		return nil, nil
	}
	switch {
	case len(field.Names) == 0:
		return nil, fmt.Errorf("an embedded field cannot carry a db tag: name the field, or drop the tag")
	case len(field.Names) > 1:
		return nil, fmt.Errorf("%s: fields sharing one db tag would map to one column: give each its own tag",
			strings.Join(names(field), ", "))
	case field.Names[0].Name == "_":
		return nil, nil
	}

	name := field.Names[0].Name
	source := name
	var convert string
	var readonly bool
	var updateGroups, insertGroups []string

	if len(store) == 2 {
		parts := strings.Split(store[1], ",")
		if parts[0] != "" {
			source = parts[0]
		}
		for _, part := range parts {
			if part == "readonly" {
				readonly = true
			}
			converts := strings.Split(part, ":")
			if len(converts) != 2 {
				continue
			}
			switch converts[0] {
			case "convert":
				convert = converts[1]
				if convert == "csv" && lexem.sqlPath == "" {
					lexem.sqlPath = defaultSqlImport
				}
			case "updates":
				updateGroups = strings.Split(converts[1], "|")
				for _, g := range updateGroups {
					lexem.addUpdateGroup(g)
				}
			case "inserts":
				insertGroups = strings.Split(converts[1], "|")
				for _, g := range insertGroups {
					lexem.addInsertGroup(g)
				}
			}
		}
	}

	wrapped, err := jsonWrapped(field, lexem, imports)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return &LexemField{
		Name:         name,
		Source:       source,
		Field:        strings.Split(db[1], ",")[0],
		Readonly:     readonly,
		Wrapped:      wrapped,
		Convert:      convert,
		UpdateGroups: updateGroups,
		InsertGroups: insertGroups,
	}, nil
}

func jsonWrapped(field *ast.Field, lexem *Lexem, imports *importSet) (bool, error) {
	star, ok := field.Type.(*ast.StarExpr)
	if !ok {
		return false, nil
	}
	idx, ok := star.X.(*ast.IndexExpr)
	if !ok {
		return false, nil
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Json" {
		return false, nil
	}
	qualifier, ok := sel.X.(*ast.Ident)
	if !ok {
		return false, fmt.Errorf("the Json wrapper has to be qualified by a package name")
	}
	path, err := imports.path(qualifier.Name)
	if err != nil {
		return false, err
	}
	// Wins over a csv field's default, and the struct gets one sql import — so a
	// package that provides Json has to provide JoinCsv and SplitCsv too.
	lexem.sqlPath = path
	return true, nil
}

func names(field *ast.Field) []string {
	out := make([]string, len(field.Names))
	for i, n := range field.Names {
		out[i] = n.Name
	}
	return out
}

func storeTagParts(tag string) []string {
	m := storeTagRE.FindStringSubmatch(tag)
	if len(m) != 2 {
		return nil
	}
	return strings.Split(m[1], ",")
}
