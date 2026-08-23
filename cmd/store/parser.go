package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	sqlWrapper = "c87d7b66130cd9245a1d3f5dde49683a"
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

func parse(fileName string) (string, []*Lexem) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fileName, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	var packageName string
	var structs []*Lexem
	imports := map[string]string{}

	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.File:
			packageName = t.Name.Name
		case *ast.ImportSpec:
			imp := strings.Trim(t.Path.Value, "\"")
			alias := filepath.Base(imp)
			if t.Name != nil {
				alias = t.Name.Name
			}
			imports[alias] = t.Path.Value
		case *ast.GenDecl:
			if t.Tok == token.TYPE {
				var model string
				var module string
				var pointer string

				typeSpec := t.Specs[0].(*ast.TypeSpec)
				structSpec := typeSpec.Type.(*ast.StructType)

				var lexem *Lexem
				var fields []*LexemField
				for _, field := range structSpec.Fields.List {
					if len(field.Names) > 0 {
						if field.Names[0].Name == "_" {
							if star, ok := field.Type.(*ast.StarExpr); ok {
								pointer = "*"
								if selector, ok := star.X.(*ast.SelectorExpr); ok {
									module = selector.X.(*ast.Ident).Name
									model = selector.Sel.Name
								} else if ident, ok := star.X.(*ast.Ident); ok {
									model = ident.Name
								}
							} else if selector, ok := field.Type.(*ast.SelectorExpr); ok {
								module = selector.X.(*ast.Ident).Name
								model = selector.Sel.Name
							} else if ident, ok := field.Type.(*ast.Ident); ok {
								model = ident.Name
							}
							lexem = &Lexem{
								Name:    typeSpec.Name.Name,
								Model:   model,
								Pointer: pointer,
							}

							if field.Tag != nil && field.Tag.Value != "" {
								store := regexp.MustCompile("store:\"([^\"]*)\"").FindStringSubmatch(field.Tag.Value)
								if len(store) == 2 {
									tags := strings.Split(store[1], ",")
									for _, tag := range tags {
										if tag == "nonew" {
											lexem.NoNew = true
										}
										if tag == "nounwrap" {
											lexem.NoUnwrap = true
										}
									}
								}
							}

							if module != "" {
								lexem.Module = module
								lexem.addImport(fmt.Sprintf("%s %s", module, imports[module]))
							}
						}
					}
					if field.Tag == nil || field.Tag.Value == "" {
						continue
					}
					db := regexp.MustCompile("db:\"([^\"]*)\"").FindStringSubmatch(field.Tag.Value)
					store := regexp.MustCompile("store:\"([^\"]*)\"").FindStringSubmatch(field.Tag.Value)

					if len(db) != 2 || len(store) == 2 && store[1] == "-" {
						continue
					}
					dbTag := strings.Split(db[1], ",")[0]
					value := field.Names[0].Name
					var convert string
					var readonly bool
					var updateGroups []string
					var insertGroups []string

					if len(store) == 2 {
						storeTag := strings.Split(store[1], ",")
						if storeTag[0] != "" {
							value = storeTag[0]
						}
						for _, part := range storeTag {
							if part == "readonly" {
								readonly = true
							}
							if converts := strings.Split(part, ":"); len(converts) == 2 {
								switch converts[0] {
								case "convert":
									convert = converts[1]
									if convert == "csv" {
										lexem.addImport("\"strings\"")
									}
								case "updates":
									groups := strings.Split(converts[1], "|")
									updateGroups = groups
									for _, g := range groups {
										lexem.addUpdateGroup(g)
									}
								case "inserts":
									groups := strings.Split(converts[1], "|")
									insertGroups = groups
									for _, g := range groups {
										lexem.addInsertGroup(g)
									}
								}
							}
						}
					}
					var wrapped bool
					if star, ok := field.Type.(*ast.StarExpr); ok {
						if idx, ok := star.X.(*ast.IndexExpr); ok {
							if sel, ok := idx.X.(*ast.SelectorExpr); ok {
								if sel.Sel.String() == "Json" {
									wrapped = true
									lexem.addImport(fmt.Sprintf("%s %s", sqlWrapper, imports[sel.X.(*ast.Ident).Name]))
								}
							}
						}
					}

					fields = append(fields, &LexemField{
						Name:         field.Names[0].Name,
						Source:       value,
						Field:        dbTag,
						Readonly:     readonly,
						Wrapped:      wrapped,
						Convert:      convert,
						UpdateGroups: updateGroups,
						InsertGroups: insertGroups,
					})
				}

				if lexem != nil {
					lexem.Fields = fields
					structs = append(structs, lexem)
				}
			}
		}
		return true
	})

	return packageName, structs
}
