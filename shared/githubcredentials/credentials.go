package githubcredentials

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func EnvironmentToken(lookup func(string) (string, bool)) *string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if value, present := lookup(name); present && strings.TrimSpace(value) != "" {
			return &value
		}
	}
	return nil
}

// LoginToken discovers an existing login only. stdout is secret, including on failure.
func LoginToken(ctx context.Context) (*string, error) {
	command := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com")
	output, err := command.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("GitHub login discovery: %w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("GitHub login discovery: %w", err)
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return nil, errors.New("GitHub login discovery returned an empty token")
	}
	return &token, nil
}
