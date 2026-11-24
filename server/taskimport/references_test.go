package taskimport

import (
	"core/shared/protoapi"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"google.golang.org/protobuf/proto"
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

func TestImportNumericBounds(t *testing.T) {
	const maximum = 9007199254740991
	for _, test := range []struct {
		name    string
		message proto.Message
		valid   bool
	}{
		{"number maximum", &taskimportpb.Reference{Value: &taskimportpb.Reference_Number{Number: maximum}}, true},
		{"number overflow", &taskimportpb.Reference{Value: &taskimportpb.Reference_Number{Number: maximum + 1}}, false},
		{"range maximum", &taskimportpb.NumberRange{First: maximum, Last: maximum}, true},
		{"range first overflow", &taskimportpb.NumberRange{First: maximum + 1, Last: maximum + 1}, false},
		{"range last overflow", &taskimportpb.NumberRange{First: 1, Last: maximum + 1}, false},
		{"zero counts", &taskimportpb.Success{}, true},
		{"maximum counts", &taskimportpb.Success{Created: maximum, Skipped: maximum, Failed: maximum}, true},
		{"created overflow", &taskimportpb.Success{Created: maximum + 1}, false},
		{"skipped overflow", &taskimportpb.Success{Skipped: maximum + 1}, false},
		{"failed overflow", &taskimportpb.Success{Failed: maximum + 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := protoapi.Validate(test.message); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
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
	for ref := range individualReferences([]*taskimportpb.Reference{{Value: &taskimportpb.Reference_Range{Range: &taskimportpb.NumberRange{First: 1, Last: 9007199254740991}}}}) {
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
