package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/capability"
	"google.golang.org/protobuf/proto"
)

func TestCapabilityKnownModelIdentity(t *testing.T) {
	for _, tc := range []struct {
		id    *string
		known bool
		valid bool
	}{
		{nil, true, false},
		{proto.String(" \t"), true, false},
		{proto.String("model"), false, false},
		{proto.String(" model "), true, true},
		{proto.String("\uFEFF"), true, true},
	} {
		facts := &pb.ModelFacts{
			KnownModels:     []*pb.ModelFact{{ModelId: tc.id, Known: tc.known, Verbosity: &pb.ModelVerbosityFact{Source: "catalog"}}},
			UnknownFallback: &pb.ModelFact{Verbosity: &pb.ModelVerbosityFact{Source: "fallback"}},
		}
		if err := protoapi.Validate(facts); (err == nil) != tc.valid {
			t.Errorf("id=%v known=%v acceptance = %v", tc.id, tc.known, err)
		}
	}
}

func TestCapabilityThinkingLevelsAreSubstantive(t *testing.T) {
	for _, level := range []string{" \t", "\u0085", " high ", "\uFEFF"} {
		valid := level == " high " || level == "\uFEFF"
		fact := &pb.ModelFact{
			SupportedThinkingLevels: []string{level},
			Verbosity:               &pb.ModelVerbosityFact{Source: "catalog"},
		}
		if err := protoapi.Validate(fact); (err == nil) != valid {
			t.Errorf("level %q acceptance = %v", level, err)
		}
	}
}

func TestCapabilityVerbosityLevels(t *testing.T) {
	for _, level := range []string{"low", " medium ", "high", " \t", "custom"} {
		valid := level == "low" || level == " medium " || level == "high"
		if err := protoapi.Validate(&pb.ModelVerbosityFact{Source: "catalog", Levels: []string{level}}); (err == nil) != valid {
			t.Errorf("level %q acceptance = %v", level, err)
		}
	}
}

func TestCapabilitySymlinkChoiceIdentity(t *testing.T) {
	for _, tc := range []struct {
		provider *string
		root     *string
		valid    bool
	}{
		{nil, proto.String("/source"), false},
		{proto.String("provider"), nil, false},
		{proto.String(" \t"), proto.String("/source"), false},
		{proto.String("provider"), proto.String(" \t"), false},
		{proto.String(" provider "), proto.String(" /source "), true},
		{proto.String("\uFEFF"), proto.String("\uFEFF"), true},
	} {
		ref := &pb.ImportChoiceRef{
			Mode:             pb.ImportChoiceMode_IMPORT_CHOICE_MODE_SYMLINK_SOURCE,
			ImportProviderId: tc.provider, SourceRootPath: tc.root,
		}
		if err := protoapi.Validate(ref); (err == nil) != tc.valid {
			t.Errorf("provider=%v root=%v acceptance = %v", tc.provider, tc.root, err)
		}
	}
	if err := protoapi.Validate(&pb.ImportChoiceRef{Mode: pb.ImportChoiceMode_IMPORT_CHOICE_MODE_NONE}); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilitySkillEnablementCandidateName(t *testing.T) {
	for _, name := range []string{" \t", "\u0085", " skill ", "\uFEFF"} {
		valid := name == " skill " || name == "\uFEFF"
		ref := &pb.ImportItemRef{
			ItemKind:   pb.ImportItemKind_IMPORT_ITEM_KIND_SKILL,
			SourceKind: pb.ImportSourceKind_IMPORT_SOURCE_KIND_GLOBAL,
			TargetName: name,
		}
		if err := protoapi.Validate(ref); err != nil {
			t.Fatalf("shared import ref rejected %q: %v", name, err)
		}
		projection := &pb.SkillEnablementProjectionFact{
			ChoiceRef:  &pb.ImportChoiceRef{Mode: pb.ImportChoiceMode_IMPORT_CHOICE_MODE_NONE},
			Candidates: []*pb.ImportItemFact{{Ref: ref}},
		}
		if err := protoapi.Validate(projection); (err == nil) != valid {
			t.Errorf("candidate %q acceptance = %v", name, err)
		}
	}
}
