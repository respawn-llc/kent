package onboardingimportskills

import "testing"

func TestCandidatesHidesGeneratedSkillsShadowedByExistingOrImportedNames(t *testing.T) {
	items := Candidates(
		[]Item{{ID: "imported", SkillName: "Review", TargetDirName: "review", ProviderLabel: "Claude"}},
		[]Item{
			{ID: "generated-review", SkillName: " review ", TargetDirName: "review"},
			{ID: "generated-plan", SkillName: "Plan", TargetDirName: "plan"},
			{ID: "generated-code", SkillName: "Code", TargetDirName: "code"},
		},
		map[string]bool{"code": true},
	)
	got := ids(items)
	want := []string{"imported", "generated-plan"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestAnnotateDuplicateSourcesUsesProviderOrSourceDir(t *testing.T) {
	items := AnnotateDuplicateSources([]Item{
		{ID: "a", TargetDirName: "review", ProviderLabel: "Claude", SourceDir: "/tmp/one"},
		{ID: "b", TargetDirName: " REVIEW ", ProviderLabel: "Codex", SourceDir: "/tmp/two"},
		{ID: "c", TargetDirName: "review", ProviderLabel: "Claude", SourceDir: "/tmp/three"},
	})
	notes := map[string]string{}
	for _, item := range items {
		notes[item.ID] = item.DuplicateSourceNote
	}
	if notes["a"] != "Codex, three" {
		t.Fatalf("note a = %q", notes["a"])
	}
	if notes["b"] != "Claude" {
		t.Fatalf("note b = %q", notes["b"])
	}
	if notes["c"] != "Codex, one" {
		t.Fatalf("note c = %q", notes["c"])
	}
}

func TestToggleAllTitle(t *testing.T) {
	items := []Item{{ID: "a"}, {ID: "b"}}
	if got := ToggleAllTitle(items, map[string]bool{"a": true, "b": true}); got != "Disable all" {
		t.Fatalf("title = %q", got)
	}
	if got := ToggleAllTitle(items, map[string]bool{"a": true}); got != "Enable all" {
		t.Fatalf("title = %q", got)
	}
	if got := ToggleAllTitle(nil, nil); got != "Enable all" {
		t.Fatalf("empty title = %q", got)
	}
}

func ids(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}
