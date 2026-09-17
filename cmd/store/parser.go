package main

import (
	"fmt"
	"go/types"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

const (
	// A hash, so the alias cannot collide with anything the author imported.
	sqlWrapper = "c87d7b66130cd9245a1d3f5dde49683a"
	// A csv field needs it even when the source file imports no sql package.
	defaultSqlImport = `"gitlab.com/gorib/sql"`
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

type MissingField struct {
	Name string
	Type types.Type
}

// The package is loaded once per directory, by the caller.
func parse(pkg *packages.Package, fileName string) (string, []*Lexem, error) {
	var lexems []*Lexem
	for _, st := range storedTypesIn(pkg, fileName) {
		lexem, err := describe(pkg, st)
		if err != nil {
			return "", nil, fmt.Errorf("%s: %s: %w", fileName, st.name, err)
		}
		if lexem != nil {
			lexems = append(lexems, lexem)
		}
	}
	return pkg.Name, lexems, nil
}

const modelHint = "the _ field must name the model type, as in `_ *model.User`"

func describe(pkg *packages.Package, st storedType) (*Lexem, error) {
	lexem, model, err := modelLexem(pkg, st)
	if err != nil || lexem == nil {
		return nil, err
	}
	if lexem.NoNew && lexem.NoUnwrap {
		// No model field is ever read, so nothing lines up against one.
		model = nil
	}
	if err := collectFields(pkg, st, lexem, model); err != nil {
		return nil, err
	}
	if lexem.sqlPath != "" {
		lexem.addImport(sqlWrapper + " " + lexem.sqlPath)
	}
	return lexem, nil
}

// A nil lexem and a nil error: no `_` field, so not a stored struct.
func modelLexem(pkg *packages.Package, st storedType) (*Lexem, *types.Struct, error) {
	var marker *types.Var
	var tag string
	for i := range st.st.NumFields() {
		if f := st.st.Field(i); f.Name() == "_" {
			marker, tag = f, st.st.Tag(i)
			break
		}
	}
	if marker == nil {
		return nil, nil, nil
	}

	lexem := &Lexem{Name: st.name}
	for _, part := range strings.Split(reflect.StructTag(tag).Get("store"), ",") {
		switch part {
		case "nonew":
			lexem.NoNew = true
		case "nounwrap":
			lexem.NoUnwrap = true
		}
	}

	typ := marker.Type()
	if ptr, ok := typ.(*types.Pointer); ok {
		lexem.Pointer = "*"
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return lexem, nil, untypedModel(lexem)
	}
	model, ok := named.Underlying().(*types.Struct)
	if !ok {
		return lexem, nil, untypedModel(lexem)
	}

	lexem.Model = named.Obj().Name()
	if modelPkg := named.Obj().Pkg(); modelPkg != nil && modelPkg.Path() != pkg.PkgPath {
		lexem.Module = modelPkg.Name()
		lexem.addImport(modelPkg.Name() + " " + `"` + modelPkg.Path() + `"`)
	}
	return lexem, model, nil
}

// `_ any` is a read model. Without both flags the output would map fields off a
// type that has none.
func untypedModel(lexem *Lexem) error {
	if lexem.NoNew && lexem.NoUnwrap {
		return nil
	}
	return fmt.Errorf("%s (or mark the struct `store:\"nonew,nounwrap\"`)", modelHint)
}

func collectFields(pkg *packages.Package, st storedType, lexem *Lexem, model *types.Struct) error {
	columns := map[string]string{}
	mapped := map[string]bool{}

	for i := range st.st.NumFields() {
		f := st.st.Field(i)
		tag := reflect.StructTag(st.st.Tag(i))
		if f.Name() == "_" {
			continue
		}

		dbTag, hasDb := tag.Lookup("db")
		storeParts := strings.Split(tag.Get("store"), ",")

		if f.Embedded() {
			if hasDb {
				return fmt.Errorf("an embedded field cannot carry a db tag: name the field, or drop the tag")
			}
			continue
		}
		if storeParts[0] == "-" {
			continue
		}
		if !hasDb {
			return fmt.Errorf("%s: a field needs a db tag to become a column, or `store:\"-\"` to stay out of one", f.Name())
		}

		column := strings.Split(dbTag, ",")[0]
		if other, seen := columns[column]; seen {
			return fmt.Errorf("%s and %s: fields sharing one db tag would map to one column: give each its own tag",
				other, f.Name())
		}
		columns[column] = f.Name()

		field, err := describeField(pkg, f, storeParts, column, lexem, model)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name(), err)
		}
		mapped[field.Source] = true
		lexem.Fields = append(lexem.Fields, field)
	}
	return nil
}

// Field faults are deliberately not raised here: synchronising runs before the
// checks, so one pass names every missing field instead of the first fault.
func plan(pkg *packages.Package, st storedType) ([]MissingField, error) {
	lexem, model, err := modelLexem(pkg, st)
	if err != nil || lexem == nil || model == nil {
		return nil, err
	}
	if lexem.NoNew && lexem.NoUnwrap {
		return nil, nil
	}

	mapped := map[string]bool{}
	for i := range st.st.NumFields() {
		f := st.st.Field(i)
		if f.Name() == "_" || f.Embedded() {
			continue
		}
		parts := strings.Split(reflect.StructTag(st.st.Tag(i)).Get("store"), ",")
		if parts[0] == "-" {
			// Answered for: counting it as mapped is what stops it being written in again.
			mapped[f.Name()] = true
			continue
		}
		if parts[0] != "" {
			mapped[parts[0]] = true
			continue
		}
		mapped[f.Name()] = true
	}
	return missingFields(pkg, model, mapped), nil
}

func describeField(pkg *packages.Package, f *types.Var, storeParts []string, column string,
	lexem *Lexem, model *types.Struct) (*LexemField, error) {
	field := &LexemField{Name: f.Name(), Source: f.Name(), Field: column}
	if storeParts[0] != "" {
		field.Source = storeParts[0]
	}
	for _, part := range storeParts {
		if part == "readonly" {
			field.Readonly = true
		}
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		switch key {
		case "convert":
			field.Convert = value
			if value == "csv" && lexem.sqlPath == "" {
				lexem.sqlPath = defaultSqlImport
			}
		case "updates":
			field.UpdateGroups = strings.Split(value, "|")
			for _, g := range field.UpdateGroups {
				lexem.addUpdateGroup(g)
			}
		case "inserts":
			field.InsertGroups = strings.Split(value, "|")
			for _, g := range field.InsertGroups {
				lexem.addInsertGroup(g)
			}
		}
	}

	if wrapped, path := jsonWrapper(f.Type()); wrapped {
		field.Wrapped = true
		// Wins over a csv field's default, and the struct gets one sql import — so
		// a package that provides Json has to provide JoinCsv and SplitCsv too.
		lexem.sqlPath = `"` + path + `"`
	}

	if model == nil {
		return field, nil
	}
	source := modelField(model, field.Source)
	if source == nil {
		return nil, fmt.Errorf("the model has no field %s", field.Source)
	}
	// Type errors are ignored wholesale at load; one reaching a mapped field is not.
	if broken(f.Type()) || broken(source.Type()) {
		return nil, fmt.Errorf("the type does not resolve: fix what the package reports first")
	}
	if err := checkType(f.Type(), source.Type(), field); err != nil {
		return nil, err
	}
	return field, nil
}

func broken(t types.Type) bool {
	basic, ok := types.Unalias(t).(*types.Basic)
	return ok && basic.Kind() == types.Invalid
}

func checkType(stored, source types.Type, field *LexemField) error {
	switch {
	case field.Wrapped:
		inner, ok := jsonPayload(stored)
		if !ok {
			return fmt.Errorf("the Json wrapper needs a type argument")
		}
		if !types.Identical(inner, source) {
			return fmt.Errorf("the Json payload is %s and model field %s is %s",
				inner, field.Source, source)
		}
	case field.Convert == "csv":
		ptr, ok := stored.(*types.Pointer)
		if !ok || ptr.Elem().String() != "string" {
			return fmt.Errorf("convert:csv needs a *string field, and this one is %s", stored)
		}
		slice, ok := source.Underlying().(*types.Slice)
		if !ok || slice.Elem().String() != "string" {
			return fmt.Errorf("convert:csv needs a []string on the model, and %s is %s",
				field.Source, source)
		}
	case !types.Identical(stored, source):
		return fmt.Errorf("the field is %s and model field %s is %s", stored, field.Source, source)
	}
	return nil
}

// The alias is not resolved away: a project fronting sql with a package of its own
// declares Json as one, and the import has to be the package the author named.
func jsonWrapper(t types.Type) (bool, string) {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false, ""
	}
	obj := wrapperObj(ptr.Elem())
	if obj == nil || obj.Name() != "Json" || obj.Pkg() == nil {
		return false, ""
	}
	return true, obj.Pkg().Path()
}

func wrapperObj(t types.Type) *types.TypeName {
	switch x := t.(type) {
	case *types.Alias:
		return x.Obj()
	case *types.Named:
		return x.Obj()
	}
	return nil
}

func jsonPayload(t types.Type) (types.Type, bool) {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return nil, false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.TypeArgs() == nil || named.TypeArgs().Len() != 1 {
		return nil, false
	}
	return named.TypeArgs().At(0), true
}

func modelField(model *types.Struct, name string) *types.Var {
	for i := range model.NumFields() {
		if f := model.Field(i); f.Name() == name {
			return f
		}
	}
	return nil
}

// An unreachable model field is no candidate: writing it in would not compile.
func missingFields(pkg *packages.Package, model *types.Struct, mapped map[string]bool) []MissingField {
	var missing []MissingField
	for i := range model.NumFields() {
		f := model.Field(i)
		if mapped[f.Name()] || f.Embedded() {
			continue
		}
		if !f.Exported() && f.Pkg() != nil && f.Pkg().Path() != pkg.PkgPath {
			continue
		}
		missing = append(missing, MissingField{Name: f.Name(), Type: f.Type()})
	}
	return missing
}

func samePath(a, b string) bool {
	return canonPath(a) == canonPath(b)
}

func canonPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
