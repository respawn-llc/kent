This project {{.ProjectName}} has {{.WorkspaceCount}} workspaces:
{{range .Workspaces}}- {{.Path}}{{if .IsDefault}} (default){{end}}
{{end}}{{if .RemainingWorkspaceCount}}- {{.RemainingWorkspaceCount}} more. `kent project workspaces` to list all{{end}}
