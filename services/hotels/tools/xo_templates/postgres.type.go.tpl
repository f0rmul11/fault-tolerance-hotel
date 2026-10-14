// {{ .Name }} is the database row for {{ schema .Schema .Table.TableName }}.
type {{ .Name }} struct {
{{- range .Fields }}
    {{ .Name }} {{ if eq .Col.DataType "uuid" }}uuid.UUID{{ else if or (eq .Col.DataType "date") (eq .Col.DataType "timestamp with time zone") }}time.Time{{ else }}{{ retype .Type }}{{ end }} `db:"{{ .Col.ColumnName }}"`
{{- end }}
}
