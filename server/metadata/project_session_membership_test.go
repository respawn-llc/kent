package metadata

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestFilterProjectSessionIDsReturnsOnlyCandidateMembers(t *testing.T) {
	store, cfg, binding := newMetadataTestStore(t)
	other, err := store.RegisterWorkspaceBinding(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"member-a", "member-b", "unrequested"} {
		importSessionPageFixture(t, store, cfg, binding, id, nil, time.Now())
	}
	importSessionPageFixture(t, store, cfg, other, "foreign", nil, time.Now())

	for _, test := range []struct {
		name       string
		projectID  string
		candidates []string
		want       []string
	}{
		{"mixed", binding.ProjectID, []string{"member-b", "foreign", "missing", "member-a", "member-b"}, []string{"member-a", "member-b"}},
		{"other project", other.ProjectID, []string{"member-a", "foreign"}, []string{"foreign"}},
		{"missing project", "missing-project", []string{"member-a"}, nil},
		{"empty", binding.ProjectID, []string{}, nil},
		{"nil", binding.ProjectID, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := store.FilterProjectSessionIDs(t.Context(), test.projectID, test.candidates)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			if !slices.Equal(got, test.want) {
				t.Fatalf("members = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFilterProjectSessionIDsAcceptsLargeCandidateSets(t *testing.T) {
	store, cfg, binding := newMetadataTestStore(t)
	importSessionPageFixture(t, store, cfg, binding, "member", nil, time.Now())
	candidates := make([]string, 40_000)
	for i := range candidates {
		candidates[i] = fmt.Sprintf("missing-%d", i)
	}
	candidates = append(candidates, "member")
	got, err := store.FilterProjectSessionIDs(t.Context(), binding.ProjectID, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"member"}) {
		t.Fatalf("members = %v, want [member]", got)
	}
}
