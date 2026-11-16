package taskimport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"core/server/httpcompression"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
)

type githubUser struct {
	Login string `json:"login"`
}
type githubLabel struct {
	Name string `json:"name"`
}
type githubIssue struct {
	Title             string           `json:"title"`
	Body              string           `json:"body"`
	User              githubUser       `json:"user"`
	AuthorAssociation string           `json:"author_association"`
	Labels            []githubLabel    `json:"labels"`
	Type              *githubLabel     `json:"type"`
	PullRequest       *json.RawMessage `json:"pull_request"`
}
type githubComment struct {
	Body string     `json:"body"`
	User githubUser `json:"user"`
}
type githubField struct {
	Name         string        `json:"issue_field_name"`
	DataType     string        `json:"data_type"`
	SingleSelect *githubLabel  `json:"single_select_option"`
	MultiSelect  []githubLabel `json:"multi_select_options"`
}

type githubReader struct {
	client *http.Client
	token  *string
}

func newGitHubReader(client *http.Client, token *string) *githubReader {
	configured := httpcompression.NewClient(client)
	configured.Timeout = 30 * time.Second
	configured.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &githubReader{client: configured, token: token}
}

func (g *githubReader) get(ctx context.Context, path string, phase taskimportpb.FailurePhase, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	if g.token != nil {
		request.Header.Set("Authorization", "Bearer "+*g.token)
	}
	response, err := g.client.Do(request)
	if err != nil {
		return fmt.Errorf("GitHub request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		code := taskimportpb.FailureCode_FAILURE_CODE_UNAVAILABLE
		switch {
		case response.StatusCode == 429 || response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != ""):
			code = taskimportpb.FailureCode_FAILURE_CODE_RATE_LIMIT
		case response.StatusCode == 401 || response.StatusCode == 403:
			code = taskimportpb.FailureCode_FAILURE_CODE_AUTHENTICATION
		}
		status := int32(response.StatusCode)
		return &serverapi.TaskImportError{Detail: &taskimportpb.Failure{
			Code: code, Phase: phase, HttpStatus: &status,
			Diagnostic: fmt.Sprintf("GitHub %s returned HTTP %d", path, status),
		}}
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func issueAPIPath(issue issueReference) string {
	return fmt.Sprintf("/repos/%s/%s/issues/%d", issue.Repository.Owner, issue.Repository.Name, issue.Number)
}

func (g *githubReader) account(ctx context.Context) (*taskimportpb.Account, error) {
	if g.token == nil {
		return nil, nil
	}
	var user githubUser
	if err := g.get(ctx, "/user", taskimportpb.FailurePhase_FAILURE_PHASE_CREDENTIALS, &user); err != nil {
		return nil, err
	}
	if user.Login == "" {
		return nil, fmt.Errorf("GitHub returned an account without a login")
	}
	return &taskimportpb.Account{Login: user.Login}, nil
}

func (g *githubReader) issue(ctx context.Context, ref issueReference) (githubIssue, error) {
	var issue githubIssue
	if err := g.get(ctx, issueAPIPath(ref), taskimportpb.FailurePhase_FAILURE_PHASE_ISSUE, &issue); err != nil {
		return issue, err
	}
	if issue.PullRequest != nil {
		return issue, &serverapi.TaskImportError{Detail: &taskimportpb.Failure{Code: taskimportpb.FailureCode_FAILURE_CODE_PULL_REQUEST, Phase: taskimportpb.FailurePhase_FAILURE_PHASE_ISSUE, Diagnostic: "reference is a pull request, not an issue"}}
	}
	if issue.Title == "" || issue.User.Login == "" {
		return issue, fmt.Errorf("GitHub issue is missing title or author")
	}
	return issue, nil
}

func (g *githubReader) fields(ctx context.Context, ref issueReference, consume func(githubField) error) error {
	for page := int64(1); ; page++ {
		var values []githubField
		if err := g.get(ctx, issueAPIPath(ref)+"/issue-field-values?per_page=100&page="+strconv.FormatInt(page, 10), taskimportpb.FailurePhase_FAILURE_PHASE_FIELDS, &values); err != nil {
			return err
		}
		for _, field := range values {
			if err := consume(field); err != nil {
				return err
			}
		}
		if len(values) < 100 {
			return nil
		}
	}
}

func (g *githubReader) comments(ctx context.Context, ref issueReference, add func(githubComment) error) error {
	for page := int64(1); ; page++ {
		var comments []githubComment
		if err := g.get(ctx, issueAPIPath(ref)+"/comments?per_page=100&page="+strconv.FormatInt(page, 10), taskimportpb.FailurePhase_FAILURE_PHASE_COMMENTS, &comments); err != nil {
			return err
		}
		for _, comment := range comments {
			if err := add(comment); err != nil {
				return err
			}
		}
		if len(comments) < 100 {
			return nil
		}
	}
}
