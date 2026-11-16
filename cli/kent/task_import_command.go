package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"core/shared/client"
	"core/shared/config"
	"core/shared/githubcredentials"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
)

func parseImportReferences(inputs []string, github bool) ([]*taskimportpb.Reference, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("import requires an issue reference")
	}
	var references []*taskimportpb.Reference
	for _, input := range inputs {
		for _, value := range strings.Split(input, ",") {
			value = strings.TrimSpace(value)
			if parsed, err := url.ParseRequestURI(value); err == nil && parsed.Scheme == "https" && parsed.Host != "" {
				references = append(references, &taskimportpb.Reference{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: value}})
				continue
			}
			if !github {
				return nil, fmt.Errorf("generic import requires issue URLs: %q", value)
			}
			first, last, interval := strings.Cut(value, "-")
			number, err := strconv.ParseInt(first, 10, 64)
			if err != nil || number <= 0 {
				return nil, fmt.Errorf("invalid issue reference %q", value)
			}
			if interval {
				end, err := strconv.ParseInt(last, 10, 64)
				if err != nil || end < number {
					return nil, fmt.Errorf("invalid inclusive issue range %q", value)
				}
				references = append(references, &taskimportpb.Reference{Value: &taskimportpb.Reference_Range{Range: &taskimportpb.NumberRange{First: number, Last: end}}})
			} else {
				references = append(references, &taskimportpb.Reference{Value: &taskimportpb.Reference_Number{Number: number}})
			}
		}
	}
	return references, nil
}

func taskImportSubcommand(args []string, stdout, stderr io.Writer) int {
	github := len(args) > 0 && args[0] == "gh"
	if github {
		args = args[1:]
	}
	fs := newCommandFlagSet(config.Command+" task import", stderr, taskImportUsage)
	project := fs.String("project", "", "destination Project ID or attached Workspace path")
	workflow := fs.String("workflow", "", "destination Workflow UUID")
	workspace := fs.String("source-workspace", "", "Task source Workspace ID or path")
	repo := fs.String("repo", "", "GitHub repository owner/repo")
	labels := fs.Bool("labels", false, "create missing GitHub Labels")
	force := fs.Bool("force", false, "create a new Task even when its source URL already exists")
	jsonOutput := fs.Bool("json", false, "stream structured import results as one JSON document")
	positionals, flags := takeLeadingPositionals(args, len(args))
	if ok, code := parseCommandFlags(fs, flags); !ok {
		return code
	}
	positionals = append(positionals, fs.Args()...)
	references, err := parseImportReferences(positionals, github)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	request := &taskimportpb.Request{Cwd: cwd, References: references, Mode: taskimportpb.ImportMode_IMPORT_MODE_SKIP_EXISTING, CreateLabels: *labels}
	if *force {
		request.Mode = taskimportpb.ImportMode_IMPORT_MODE_FORCE_CREATE
	}
	for _, selected := range []struct {
		name   string
		value  *string
		target **string
	}{
		{"project", project, &request.Project}, {"workflow", workflow, &request.WorkflowId}, {"source-workspace", workspace, &request.SourceWorkspace}, {"repo", repo, &request.Repository},
	} {
		if flagExplicit(fs, selected.name) {
			if strings.TrimSpace(*selected.value) == "" {
				fmt.Fprintf(stderr, "--%s must not be blank\n", selected.name)
				return 2
			}
			*selected.target = selected.value
		}
	}
	return runWorkflowCommandSession(stderr, func(_ config.Connection, remote *client.Remote) int {
		if isTerminalWriter(stderr) {
			fmt.Fprintln(stderr, "Importing GitHub issues…")
		}
		candidates := &taskimportpb.CredentialCandidates{EnvironmentToken: githubcredentials.EnvironmentToken(os.LookupEnv)}
		if candidates.EnvironmentToken == nil {
			var err error
			candidates.LoginToken, err = githubcredentials.LoginToken(context.Background())
			if err != nil {
				diagnostic := err.Error()
				candidates.LoginDiagnostic = &diagnostic
			}
		}
		request.Credentials = candidates
		output := &importOutput{stdout: stdout, stderr: stderr, json: *jsonOutput}
		result, err := remote.ImportTasks(context.Background(), request, output)
		return output.finish(result, err)
	})
}
