package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/proto"
)

func TestToolOwnerContractRejectsNormalizedPatchFamilyWithoutPatch(t *testing.T) {
	for _, name := range []string{" patch ", " edit ", " replace ", " write ", "shell"} {
		valid := name == "shell"
		row := &transcriptpb.ToolRow{ToolName: proto.String(name)}
		start := &transcriptpb.ToolStart{
			StepId: "22222222-2222-4222-8222-222222222222", ToolCallId: "call-1", ToolName: name,
		}
		for _, message := range []proto.Message{row, start} {
			if err := protoapi.Validate(message); (err == nil) != valid {
				t.Errorf("%T owner %q acceptance = %v, want %t", message, name, err, valid)
			}
		}
	}
}

func TestToolRenderHintSourceRequiresSubstantivePath(t *testing.T) {
	for _, test := range []struct {
		kind  transcriptpb.ToolRenderKind
		path  *string
		valid bool
	}{
		{kind: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SOURCE, path: nil},
		{kind: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SOURCE, path: proto.String(" \t ")},
		{kind: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SOURCE, path: proto.String("file.go"), valid: true},
		{kind: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SHELL, valid: true},
		{kind: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_DIFF, valid: true},
		{kind: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_PLAIN, valid: true},
	} {
		message := &transcriptpb.ToolRenderHint{Kind: test.kind, Path: test.path}
		if err := protoapi.Validate(message); (err == nil) != test.valid {
			t.Errorf("hint %+v acceptance = %v, want %t", message, err, test.valid)
		}
	}
}

func TestToolRowPresentationRequiresToolName(t *testing.T) {
	presentation := &transcriptpb.ToolPresentation{
		Presentation:   transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
		RenderBehavior: transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
	}
	for _, name := range []*string{nil, proto.String("shell")} {
		message := &transcriptpb.ToolRow{ToolName: name, Presentation: presentation}
		if err := protoapi.Validate(message); (err == nil) != (name != nil) {
			t.Errorf("owner name %v acceptance = %v", name, err)
		}
	}
}
