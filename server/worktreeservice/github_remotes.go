package worktree

import (
	"context"
	"fmt"
	"slices"
	"strings"

	giturls "github.com/whilp/git-urls"
)

type GitHubRepository struct {
	Owner string
	Name  string
}

func (i *GitInspector) GitHubRepositories(ctx context.Context, root string) ([]GitHubRepository, error) {
	output, exit, err := i.runner.Run(ctx, root, "remote")
	if err != nil {
		return nil, formatGitRunError(exit, err, output, "remote")
	}
	var repositories []GitHubRepository
	for _, remote := range strings.Fields(string(output)) {
		output, exit, err := i.runner.Run(ctx, root, "remote", "get-url", "--all", remote)
		if err != nil {
			return nil, formatGitRunError(exit, err, output, "remote", "get-url", "--all", remote)
		}
		for _, raw := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			value, err := giturls.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("parse remote %q: %w", remote, err)
			}
			if !strings.EqualFold(value.Hostname(), "github.com") {
				continue
			}
			parts := strings.Split(strings.Trim(strings.TrimSuffix(value.Path, ".git"), "/"), "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("GitHub remote %q has no owner/repository path", remote)
			}
			repository := GitHubRepository{Owner: strings.ToLower(parts[0]), Name: strings.ToLower(parts[1])}
			if !slices.Contains(repositories, repository) {
				repositories = append(repositories, repository)
			}
		}
	}
	return repositories, nil
}
