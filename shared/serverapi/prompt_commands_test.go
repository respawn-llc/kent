package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	"core/shared/serverapi"
)

func TestPromptCommandCatalogResponseValidatesNamesAndUnicodePreviewLimit(t *testing.T) {
	response := &promptcommandpb.Catalog{Commands: []*promptcommandpb.CatalogEntry{
		{Name: "prompt:review", Preview: "review"},
	}}
	if err := protoapi.Validate(response); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	response.Commands = append(response.Commands, &promptcommandpb.CatalogEntry{Name: "prompt:review", Preview: "review"})
	if err := protoapi.Validate(response); err == nil {
		t.Fatal("duplicate catalog entry validated")
	}
	for _, name := range []string{" prompt:preview", "prompt:preview "} {
		t.Run("noncanonical name "+name, func(t *testing.T) {
			invalid := &promptcommandpb.Catalog{Commands: []*promptcommandpb.CatalogEntry{{Name: name, Preview: "preview"}}}
			if err := protoapi.Validate(invalid); err == nil {
				t.Fatalf("name %q validated", name)
			}
		})
	}
	for _, preview := range []string{"", " leading", "trailing ", "two  spaces", "line\nbreak", "tab\tbreak"} {
		t.Run("invalid preview "+preview, func(t *testing.T) {
			invalid := &promptcommandpb.Catalog{Commands: []*promptcommandpb.CatalogEntry{{Name: "prompt:preview", Preview: preview}}}
			if err := protoapi.Validate(invalid); err == nil {
				t.Fatalf("preview %q validated", preview)
			}
		})
	}
}

func TestPromptCommandErrorValidation(t *testing.T) {
	command := "prompt:missing"
	if err := (&serverapi.PromptCommandError{Kind: serverapi.PromptCommandErrorKindCommandNotFound, Command: &command}).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
