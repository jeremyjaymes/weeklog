---
week: {{.Week}}
generated_at: {{.GeneratedAtUTC}}
weeklog_version: {{.Version}}
---

# {{.Heading}}

{{.Summary.Line}}
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
