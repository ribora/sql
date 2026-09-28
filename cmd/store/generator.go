package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"
)

// Parsed once rather than per struct: the sweep writes a file for every stored
// struct in the tree.
var packageTemplate = template.Must(template.New("").Funcs(template.FuncMap{
	"title": func(value string) string {
		return strings.ToUpper(value[:1]) + value[1:]
	},
	"contains": func(slice []string, s string) bool {
		return slices.Contains(slice, s)
	},
	"lowerCamel": strcase.ToLowerCamel,
}).Parse(`//go:build !` + buildTag + `

` + generatedHeader + `
package {{ .PackageName }}

{{ if and (gt (len .Lexem.Imports) 0) (or (not .Lexem.NoNew) (not .Lexem.NoUnwrap)) -}}
import (
	{{- range .Lexem.Imports }}
	{{ . }}
	{{- end }}
)
{{- end }}

var {{ .Lexem.Name }}Mappings = map[string]string{
	{{- range .Lexem.Fields }}
		"{{ lowerCamel .Source }}": "{{ .Field }}",
	{{- end }}
}

{{ if not .Lexem.NoNew -}}
func new{{ title .Lexem.Name}}(m {{ .Lexem.Pointer }}{{ if ne .Lexem.Module "" }}{{ .Lexem.Module }}.{{ end }}{{ .Lexem.Model }}) *{{ .Lexem.Name }} {
	return &{{ .Lexem.Name }}{
		{{- range .Lexem.Fields }}
		{{ .Name }}: 
			{{- if .Wrapped -}}
			{{ $.SqlWrapper }}.NewJson(m.{{ .Source }}),
			{{- else if eq .Convert "csv" -}}
			{{ $.SqlWrapper }}.JoinCsv(m.{{ .Source }}),
			{{- else -}}
			m.{{ .Source }},
			{{- end }}
		{{- end }}
	}
}
{{- end }}

{{ if not .Lexem.NoUnwrap -}}
func (m *{{ .Lexem.Name }}) Unwrap() {{ .Lexem.Pointer }}{{ if ne .Lexem.Module "" }}{{ .Lexem.Module }}.{{ end }}{{ .Lexem.Model }} {
	return {{ if eq .Lexem.Pointer "*" }}&{{ end }}{{ if ne .Lexem.Module "" }}{{ .Lexem.Module }}.{{ end }}{{ .Lexem.Model }}{
		{{- range .Lexem.Fields }}
		{{ .Source }}: 
			{{- if .Wrapped -}}
			m.{{ .Name }}.Unwrap(),
			{{- else if eq .Convert "csv" -}}
			{{ $.SqlWrapper }}.SplitCsv(m.{{ .Name }}),
			{{- else -}}
			m.{{ .Name }},
			{{- end -}}
		{{- end }}
	}
}
{{- end }}

func (m *{{ .Lexem.Name }}) Mappings() map[string]string {
	return {{ .Lexem.Name }}Mappings
}

{{ if .Lexem.Sort -}}
func (m *{{ .Lexem.Name }}) DefaultSort() []string {
	return []string{
		{{- range .Lexem.Sort }}
		"{{ . }}",
		{{- end }}
	}
}
{{- end }}

func (m *{{ .Lexem.Name }}) Updates() map[string]any {
	return map[string]any{
		{{- range .Lexem.Fields }}
		{{- if not .Readonly }}
		"{{ .Field }}": m.{{ .Name }},
		{{- end }}
		{{- end }}
	}
}

{{ range $group := .Lexem.UpdateGroups }}
func (m *{{ $.Lexem.Name }}) Updates{{ title $group }}() map[string]any {
	return map[string]any{
		{{- range $.Lexem.Fields }}
		{{- if contains .UpdateGroups $group }}
		"{{ .Field }}": m.{{ .Name }},
		{{- end }}
		{{- end }}
	}
}
{{ end }}

func (m *{{ .Lexem.Name }}) Inserts() ([]string, []any) {
	return []string{
		{{- range .Lexem.Fields }}
		{{- if not .Readonly }}
		"{{ .Field }}",
		{{- end }}
		{{- end }}
	}, []any{
		{{- range .Lexem.Fields }}
		{{- if not .Readonly }}
		m.{{ .Name }},
		{{- end }}
		{{- end }}
	}
}

{{ range $group := .Lexem.InsertGroups }}
func (m *{{ $.Lexem.Name }}) Inserts{{ title $group }}() ([]string, []any) {
	return []string{
		{{- range $.Lexem.Fields }}
		{{- if contains .InsertGroups $group }}
		"{{ .Field }}",
		{{- end }}
		{{- end }}
	}, []any{
		{{- range $.Lexem.Fields }}
		{{- if contains .InsertGroups $group }}
		m.{{ .Name }},
		{{- end }}
		{{- end }}
	}
}
{{ end }}

func (m *{{ .Lexem.Name }}) Returns() []string {
	return []string{
		{{- range .Lexem.Fields }}
		"{{ .Field }}",
		{{- end }}
	}
}

func (m *{{ .Lexem.Name }}) Scan() []any {
	return []any{
		{{- range .Lexem.Fields }}
		&m.{{ .Name }},
		{{- end }}
	}
}
`))

func write(path string, packageName string, lexem *Lexem) error {
	var buf bytes.Buffer
	err := packageTemplate.Execute(&buf, map[string]any{
		"PackageName": packageName,
		"Lexem":       lexem,
		"SqlWrapper":  sqlWrapper,
	})
	if err != nil {
		return err
	}

	// A template that renders unparseable Go is a defect in this generator, not in
	// the input, so the source goes into the message.
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("formatting generated code for %s: %w\nOriginal source:\n%s", lexem.Name, err, buf.String())
	}

	out := filepath.Join(path, toSnake(lexem.Name)+"_gen.go")
	return os.WriteFile(out, formatted, 0o644)
}

func toSnake(source string) string {
	var builder strings.Builder
	for i, c := range source {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				builder.WriteByte(byte('_'))
			}
			c += 32
		}
		builder.WriteByte(byte(c))
	}
	return builder.String()
}
