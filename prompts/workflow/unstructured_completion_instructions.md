- Complete this workflow node by sending a final answer whose entire content is exactly one raw JSON object.
- Do not include Markdown fences, prose, headings, comments, or any text outside the JSON object.
- `commentary` is optional; if provided, it must be a string.
{{- if .Examples }}
- Use one of these JSON shapes for the current completion contract:
{{- range .Examples }}
{{- if $.MultipleTransitions }}
  - Transition `{{.TransitionID}}`{{if .DisplayName}} ({{.DisplayName}}){{end}}{{if .Description}}: {{.Description}}{{end}}
{{- else }}
  - The only available transition is inferred, so omit `transition`.
{{- end }}
```json
{{.JSON}}
```
{{- end }}
{{- else }}
- No outgoing transition is available. Ask for help instead of guessing a completion.
{{- end }}
