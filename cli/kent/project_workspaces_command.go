package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"core/cli/internal/pathdisplay"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
)

const (
	projectWorkspacesDefaultLimit   = 20
	projectWorkspacesCommandTimeout = 5 * time.Second
)

type projectWorkspacesListOutput struct {
	ProjectID  string                   `json:"project_id"`
	Offset     int32                    `json:"offset"`
	Workspaces []projectWorkspaceOutput `json:"workspaces"`
	NextOffset *int32                   `json:"next_offset,omitempty"`
}

type projectWorkspaceOutput struct {
	WorkspaceID string `json:"workspace_id"`
	DisplayName string `json:"display_name"`
	RootPath    string `json:"root_path"`
	IsDefault   bool   `json:"is_default"`
}

func projectWorkspacesSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" project workspaces", stderr, projectWorkspacesUsage)
	projectFlag := fs.String("project", "", "Project ID or attached Workspace path")
	offsetFlag := fs.Int("offset", 0, "zero-based Workspace offset (default 0)")
	limitFlag := fs.Int("limit", projectWorkspacesDefaultLimit, "maximum number of Workspaces to return (1-100)")
	jsonOut := fs.Bool("json", false, "write catalog facts as JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "project workspaces does not accept positional arguments")
		return 2
	}
	projectProvided := flagExplicit(fs, "project")
	projectID := strings.TrimSpace(*projectFlag)
	if projectProvided && projectID == "" {
		fmt.Fprintln(stderr, "--project must not be blank")
		return 2
	}
	requestOffset, err := protoapi.Int32(*offsetFlag, "offset")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	requestLimit, err := protoapi.Int32(*limitFlag, "limit")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	remote, selectedProjectID, err := openProjectWorkspacesRemote(
		context.Background(),
		projectProvided,
		projectID,
	)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() {
		if err := remote.Close(); err != nil {
			fmt.Fprintf(stderr, "close Project workspaces connection: %v\n", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), projectWorkspacesCommandTimeout)
	defer cancel()
	response, err := remote.ListProjectWorkspaces(ctx, &projectpb.ProjectWorkspaceListRequest{
		ProjectId: selectedProjectID,
		Offset:    requestOffset,
		Limit:     requestLimit,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeCommandJSON(stdout, stderr, projectWorkspacesOutput(response))
	}
	for _, workspace := range response.Workspaces {
		if _, err := fmt.Fprintf(stdout, "- %s\n", pathdisplay.CompactHome(workspace.RootPath)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if response.NextOffset != nil {
		if err := writeNextOffset(stderr, int(*response.NextOffset)); err != nil {
			return 1
		}
	}
	return 0
}

func openProjectWorkspacesRemote(
	ctx context.Context,
	projectProvided bool,
	projectID string,
) (*client.Remote, string, error) {
	if projectProvided {
		cfg, remote, err := openBindingCommandRemote(ctx, ".")
		if err != nil {
			return nil, "", err
		}
		resolvedProjectID, err := resolveWorkflowProjectID(ctx, cfg, remote, projectID)
		if err != nil {
			return nil, "", errors.Join(err, remote.Close())
		}
		return remote, resolvedProjectID, nil
	}

	sessionID, err := resolveOptionalWorktreeCommandSession("")
	if err != nil {
		return nil, "", err
	}
	if sessionID == nil {
		return nil, "", errors.New("--project is required outside a Kent Session")
	}
	remote, err := openWorktreeCommandRemote(ctx, sessionID)
	if err != nil {
		return nil, "", err
	}
	binding, ok := remote.ProjectBinding()
	if !ok || strings.TrimSpace(binding.ProjectID) == "" {
		return nil, "", errors.Join(
			errors.New("Session Project binding is unavailable"),
			remote.Close(),
		)
	}
	return remote, binding.ProjectID, nil
}

func projectWorkspacesOutput(response *projectpb.ListProjectWorkspacesSuccess) projectWorkspacesListOutput {
	workspaces := make([]projectWorkspaceOutput, 0, len(response.Workspaces))
	for _, workspace := range response.Workspaces {
		workspaces = append(workspaces, projectWorkspaceOutput{
			WorkspaceID: workspace.WorkspaceId,
			DisplayName: workspace.DisplayName,
			RootPath:    workspace.RootPath,
			IsDefault:   workspace.IsDefault,
		})
	}
	return projectWorkspacesListOutput{
		ProjectID:  response.ProjectId,
		Offset:     response.Offset,
		Workspaces: workspaces,
		NextOffset: response.NextOffset,
	}
}
