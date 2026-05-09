package onboardingimportgenerated

import "testing"

func TestParseNameUsesFrontmatterNameAndSanitizesWhitespace(t *testing.T) {
	name, ok := ParseName("fallback", "---\nname: ' Review  code '\ndescription: ' helps review '\n---\nbody")
	if !ok || name != "Review code" {
		t.Fatalf("name=%q ok=%t", name, ok)
	}
}

func TestParseNameFallsBackToDirectoryName(t *testing.T) {
	name, ok := ParseName(" builder-dogfooding ", "---\ndescription: ' use builder itself '\n---\nbody")
	if !ok || name != "builder-dogfooding" {
		t.Fatalf("name=%q ok=%t", name, ok)
	}
}

func TestParseNameRejectsMissingDescriptionOrFrontmatter(t *testing.T) {
	for _, contents := range []string{
		"body only",
		"---\nname: demo\n---\nbody",
		"---\ndescription: ''\n---\nbody",
	} {
		if name, ok := ParseName("fallback", contents); ok {
			t.Fatalf("expected %q to be rejected, got %q", contents, name)
		}
	}
}

func TestDiscoverReturnsGeneratedSkillCatalog(t *testing.T) {
	items, err := Discover()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.ProviderLabel != "Preinstalled" {
			t.Fatalf("provider label = %q", item.ProviderLabel)
		}
		seen[item.SkillName] = true
	}
	for _, name := range []string{"builder-dogfooding", "creating-skills"} {
		if !seen[name] {
			t.Fatalf("missing generated skill %q in %+v", name, items)
		}
	}
}
