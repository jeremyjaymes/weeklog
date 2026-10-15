---
week: {{.Week}}
generated_at: {{.GeneratedAtUTC}}
weeklog_version: {{.Version}}
---

# {{.Heading}}

{{.Summary.Line}}
{{- if .Summary.ActorLine}}
{{.Summary.ActorLine}}
{{- end}}
{{range .Sections}}
## {{.Title}}
{{range .Items}}
### {{.Heading}}
{{range .Bullets}}- {{.}}
{{end}}{{end}}{{end}}{{if .OtherLine}}
## Other activity

- {{.OtherLine}}
{{end}}
<!-- weeklog:notes -->{{.Notes}}<!-- weeklog:notes -->
