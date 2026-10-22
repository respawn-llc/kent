package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/proto"
)

func TestToolPresentationPreservesServerPatchCounts(t *testing.T) {
	message := &transcriptpb.ToolPresentation{
		Presentation:   transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
		RenderBehavior: transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
		PatchPresentation: &transcriptpb.PatchPresentation{
			Presentation: &transcriptpb.PatchPresentation_Changes{
				Changes: &transcriptpb.PatchChanges{Files: []*transcriptpb.PatchFileChange{{
					Path:    &transcriptpb.PatchPath{Absolute: "/workspace/file.go", Relative: "file.go"},
					Added:   7,
					Removed: proto.Int32(3),
					Operations: []*transcriptpb.PatchFileOperation{{
						Operation: &transcriptpb.PatchFileOperation_Add{
							Add: &transcriptpb.PatchAddOperation{Groups: []*transcriptpb.PatchChangeGroup{{
								Lines: []*transcriptpb.PatchChangedLine{{
									Kind:    transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_ADDED,
									Content: "one visible added line",
								}},
							}}},
						},
					}},
				}}},
			},
		},
	}
	encoded, err := protoapi.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	received := &transcriptpb.ToolPresentation{}
	if err := protoapi.Decode(encoded, received); err != nil {
		t.Fatal(err)
	}
	adapted := protoapi.ToolPresentationFromProto("patch", received)
	file := adapted.PatchPresentation.Files[0]
	if file.Added != 7 || file.Removed == nil || *file.Removed != 3 {
		t.Fatalf("server counts changed: added=%d removed=%v", file.Added, file.Removed)
	}
}
