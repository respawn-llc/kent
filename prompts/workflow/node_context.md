Workflow task
Task ID: {{.TaskId}}
Task short ID: {{.TaskShortId}}
Task title: {{.TaskTitle}}
Task body:
{{.TaskBody}}

Workflow node
Node ID: {{.NodeId}}
Node key: {{.NodeKey}}
Node display name: {{.NodeDisplayName}}
{{- if .ContextMode }}
Context mode: {{.ContextMode}}
{{- end }}
{{- if .SourceSessionID }}
Source session: {{.SourceSessionID}}
{{- end }}
Completion mode: {{.CompletionMode}}
{{- if .OutputFields }}

Required node output fields:
{{- range .OutputFields }}
- {{.Name}}: {{.Description}}
{{- end }}
{{- end }}
{{- if .Transitions }}

Available transitions:
{{- range .Transitions }}
- {{.ID}}{{if .DisplayName}} ({{.DisplayName}}){{end}}{{if .Description}}: {{.Description}}{{end}}
{{- end }}
{{- end }}
{{- if .InputValues }}

Bound input values:
{{- range .InputValues }}
- {{.Name}}: {{.Value}}
{{- end }}
{{- end }}
{{- if .NodePrompt }}

Node prompt:
{{.NodePrompt}}
{{- end }}
