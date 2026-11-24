package taskimport

import (
	"core/shared/labelcontract"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"testing"
)

func TestCategoricalLabelCandidates(t *testing.T) {
	for _, test := range []struct{ field, value, mapped string }{
		{"Priority", "Urgent", "P0"}, {"Priority", "High", "P1"}, {"Priority", "Medium", "P2"}, {"Priority", "Low", "P3"},
		{"Effort", "Low", "Small"}, {"Effort", "Medium", "Medium"}, {"Effort", "High", "Large"},
		{"Priority", "Unknown", "Unknown"}, {"Team", "Straße", "Straße"},
	} {
		candidate := categoricalLabel(test.field, test.value)
		if candidate.createName != test.mapped {
			t.Fatalf("%+v", candidate)
		}
		index := labelIndex{labelcontract.Fold(test.field + ": " + test.value): "prefixed"}
		if match := candidate.match(index); match == nil || *match != "prefixed" {
			t.Fatalf("prefixed match: %+v", candidate)
		}
		index[labelcontract.Fold(test.mapped)] = "mapped"
		want := "mapped"
		if test.value == "Unknown" || test.field == "Team" {
			want = "prefixed"
		}
		if match := candidate.match(index); match == nil || *match != want {
			t.Fatalf("mapped match: %+v", candidate)
		}
	}
}

func TestFieldsAndAuthorWarning(t *testing.T) {
	for _, kind := range []string{"text", "date", "number"} {
		if len(fieldLabels(githubField{Name: "Other", DataType: kind})) != 0 {
			t.Fatalf("projected %s", kind)
		}
	}
	if len(fieldLabels(githubField{Name: "Team", DataType: "multi_select", MultiSelect: []githubLabel{{Name: "One"}, {Name: "Two"}}})) != 2 {
		t.Fatal("multi-select lost values")
	}
	if match := literalLabel("STRASSE").match(labelIndex{labelcontract.Fold("Straße"): "unicode"}); match == nil || *match != "unicode" {
		t.Fatal("Unicode match failed")
	}
	for _, association := range []string{"OWNER", "MEMBER", "COLLABORATOR", "CONTRIBUTOR", "NONE", ""} {
		issue := githubIssue{User: githubUser{Login: "author"}, AuthorAssociation: association}
		want := association != "OWNER" && association != "MEMBER" && association != "COLLABORATOR"
		if needsAuthorWarning(issue, nil) != want {
			t.Fatalf("association %s", association)
		}
		if needsAuthorWarning(issue, &taskimportpb.Account{Login: "AUTHOR"}) {
			t.Fatal("self-author warned")
		}
	}
}
