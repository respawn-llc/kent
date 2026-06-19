- Complete this workflow task by sending a final answer whose entire content is exactly one raw JSON object.
- Do not include Markdown fences, prose, headings, comments, or any text outside the JSON object.
- `commentary` is optional; if provided, it must be a string.
- Use one of these JSON shapes for the current completion contract:

{{- range .Examples }}
```json
{{.JSON}}
```
{{- end }}
