package taskimport

import (
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

func invalidReference(format string, args ...any) error {
	return &serverapi.TaskImportError{Detail: &taskimportpb.Failure{
		Code:       taskimportpb.FailureCode_FAILURE_CODE_INVALID_INPUT,
		Phase:      taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION,
		Diagnostic: fmt.Sprintf(format, args...),
	}}
}

func individualReferences(references []*taskimportpb.Reference) iter.Seq[*taskimportpb.Reference] {
	return func(yield func(*taskimportpb.Reference) bool) {
		for _, reference := range references {
			if interval, ok := reference.Value.(*taskimportpb.Reference_Range); ok {
				for number := interval.Range.First; ; number++ {
					if !yield(&taskimportpb.Reference{Value: &taskimportpb.Reference_Number{Number: number}}) {
						return
					}
					if number == interval.Range.Last {
						break
					}
				}
			} else if !yield(reference) {
				return
			}
		}
	}
}

type repository struct {
	Owner string
	Name  string
}

func parseRepository(value string) (repository, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return repository{}, invalidReference("invalid GitHub repository %q: expected owner/repo", value)
	}
	for _, part := range parts {
		if part == "." || part == ".." {
			return repository{}, invalidReference("invalid GitHub repository %q", value)
		}
		for _, character := range part {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
				continue
			}
			return repository{}, invalidReference("invalid GitHub repository %q", value)
		}
	}
	return repository{Owner: parts[0], Name: parts[1]}, nil
}

func (r repository) equal(other repository) bool {
	return strings.EqualFold(r.Owner, other.Owner) && strings.EqualFold(r.Name, other.Name)
}

type issueReference struct {
	Repository repository
	Number     int64
}

func (r issueReference) URL() string {
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", r.Repository.Owner, r.Repository.Name, r.Number)
}

func parseIssueURL(raw string) (issueReference, error) {
	value, err := url.Parse(raw)
	if err != nil {
		return issueReference{}, invalidReference("invalid GitHub issue URL: %v", err)
	}
	if value.Scheme != "https" || !strings.EqualFold(value.Host, "github.com") || value.User != nil {
		return issueReference{}, invalidReference("expected a github.com HTTPS issue URL: %q", raw)
	}
	parts := strings.Split(strings.TrimSuffix(value.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "" || parts[3] != "issues" {
		return issueReference{}, invalidReference("expected a GitHub issue path: %q", raw)
	}
	repo, err := parseRepository(parts[1] + "/" + parts[2])
	if err != nil {
		return issueReference{}, err
	}
	number, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || number <= 0 {
		return issueReference{}, invalidReference("invalid GitHub issue number %q", parts[4])
	}
	return issueReference{Repository: repo, Number: number}, nil
}

func resolveURL(raw string, explicit *repository) (issueReference, error) {
	issue, err := parseIssueURL(raw)
	if err != nil {
		return issueReference{}, err
	}
	return issue, validateRepositorySelection(issue, explicit)
}

func validateRepositorySelection(issue issueReference, explicit *repository) error {
	if explicit != nil && !explicit.equal(issue.Repository) {
		return invalidReference("issue URL conflicts with explicit repository %s/%s", explicit.Owner, explicit.Name)
	}
	return nil
}
