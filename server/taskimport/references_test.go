package taskimport

import (
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"testing"
)

func TestIssueURL(t *testing.T) {
	issue, err := parseIssueURL("https://github.com/Respawn-LLC/kent/issues/1043?test=1#issuecomment-1")
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 1043 || issue.Repository.Owner != "Respawn-LLC" || issue.Repository.Name != "kent" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if issue.URL() != "https://github.com/Respawn-LLC/kent/issues/1043" {
		t.Fatalf("unexpected canonical source: %s", issue.URL())
	}
}

func TestReferencesExpandIncrementally(t *testing.T) {
	references := []*taskimportpb.Reference{
		{Value: &taskimportpb.Reference_Number{Number: 2}},
		{Value: &taskimportpb.Reference_Range{Range: &taskimportpb.NumberRange{First: 4, Last: 6}}},
	}
	var numbers []int64
	for ref := range individualReferences(references) {
		numbers = append(numbers, ref.GetNumber())
	}
	if len(numbers) != 4 || numbers[0] != 2 || numbers[1] != 4 || numbers[3] != 6 {
		t.Fatalf("unexpected references: %v", numbers)
	}
	for ref := range individualReferences([]*taskimportpb.Reference{{Value: &taskimportpb.Reference_Range{Range: &taskimportpb.NumberRange{First: 1, Last: 1 << 62}}}}) {
		if ref.GetNumber() != 1 {
			t.Fatal(ref)
		}
		break
	}
}

func TestExplicitRepositoryConflicts(t *testing.T) {
	repo, err := parseRepository("owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveURL("https://github.com/OWNER/REPO/issues/1", &repo); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveURL("https://github.com/other/repo/issues/1", &repo); err == nil {
		t.Fatal("conflicting repository accepted")
	}
}

func TestInvalidIssueURLs(t *testing.T) {
	for _, raw := range []string{
		"https://example.com/a/b/issues/1", "https://github.com/a/b/pull/1",
		"https://github.com/a/b/issues/0", "https://github.com/a/b/issues/no",
		"https://github.com/a/b/issues/1/other", "https://user@github.com/a/b/issues/1",
	} {
		if _, err := parseIssueURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
