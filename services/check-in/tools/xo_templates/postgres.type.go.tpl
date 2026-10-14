// {{ .Name }} is the database row for {{ schema .Schema .Table.TableName }}.
type {{ .Name }} struct {
{{- range .Fields }}
    {{ .Name }} {{ if eq .Col.DataType "uuid" }}{{ if .Col.NotNull }}uuid.UUID{{ else }}uuid.NullUUID{{ end }}{{ else if or (eq .Col.DataType "date") (eq .Col.DataType "timestamp with time zone") }}{{ if .Col.NotNull }}time.Time{{ else }}sql.NullTime{{ end }}{{ else }}{{ retype .Type }}{{ end }} `db:"{{ .Col.ColumnName }}"`
{{- end }}
}
