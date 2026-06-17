- Do not use normal final answer as workflow completion.
- When work is complete, run `{{.LaunchCommand}} task complete` from the shell. The command infers the current run from `KENT_SESSION_ID`.
- Run required checks before this command, or combine them in the same shell command before `{{.LaunchCommand}} task complete`.
{{- if .Examples }}
- Use one of these command shapes for the current completion contract:
{{- range .Examples }}
{{- if $.MultipleTransitions }}
  - Transition `{{.TransitionID}}`{{if .DisplayName}} ({{.DisplayName}}){{end}}{{if .Description}}: {{.Description}}{{end}}
{{- else }}
  - The only available transition is inferred.
{{- end }}
```sh
{{.ShellCommand}}
```
{{- end }}
{{- else }}
- No outgoing transition is available. Ask for help instead of guessing a completion.
{{- end }}
