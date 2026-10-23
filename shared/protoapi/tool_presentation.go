package protoapi

import (
	"fmt"
	"slices"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcriptmodel"
	"core/shared/transcriptmodel/patchformat"
)

// ToolPresentationFromProto restores native rendering facts using the tool
// identity on the enclosing row or start. Display normalization is server-owned.
func ToolPresentationFromProto(toolName string, presentation *transcriptpb.ToolPresentation) (*transcript.ToolCallMeta, error) {
	if presentation == nil {
		return nil, nil
	}
	out := &transcript.ToolCallMeta{
		ToolName:               toolName,
		IsShell:                presentation.IsShell,
		UserInitiated:          presentation.UserInitiated,
		Command:                presentation.GetCommand(),
		CompactText:            presentation.GetCompactText(),
		InlineMeta:             presentation.GetInlineMeta(),
		TimeoutLabel:           presentation.GetTimeoutLabel(),
		Question:               presentation.GetQuestion(),
		Suggestions:            slices.Clone(presentation.Suggestions),
		RecommendedOptionIndex: int(presentation.GetRecommendedOptionIndex()),
		OmitSuccessfulResult:   presentation.OmitSuccessfulResult,
		RawOutputRequested:     presentation.RawOutputRequested,
		OutputTruncated:        presentation.OutputTruncated,
		MovedToBackground:      presentation.MovedToBackground,
	}
	switch presentation.Presentation {
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT:
		out.Presentation = transcript.ToolPresentationDefault
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL:
		out.Presentation = transcript.ToolPresentationShell
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_ASK_QUESTION:
		out.Presentation = transcript.ToolPresentationAskQuestion
	}
	switch presentation.RenderBehavior {
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT:
		out.RenderBehavior = transcript.ToolCallRenderBehaviorDefault
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL:
		out.RenderBehavior = transcript.ToolCallRenderBehaviorShell
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_ASK_QUESTION:
		out.RenderBehavior = transcript.ToolCallRenderBehaviorAskQuestion
	}
	if presentation.ShellExitCode != nil {
		code := int(*presentation.ShellExitCode)
		out.ShellExitCode = &code
	}
	if presentation.RenderHint != nil {
		hint, err := toolRenderHintFromProto(presentation.RenderHint)
		if err != nil {
			return nil, err
		}
		out.RenderHint = hint
	}
	if presentation.PatchPresentation != nil {
		patch, err := patchPresentationFromProto(presentation.PatchPresentation)
		if err != nil {
			return nil, fmt.Errorf("tool patch presentation: %w", err)
		}
		out.PatchPresentation = patch
	}
	return out, nil
}

func toolRenderHintFromProto(hint *transcriptpb.ToolRenderHint) (*transcript.ToolRenderHint, error) {
	out := &transcript.ToolRenderHint{
		Path:       hint.GetPath(),
		ResultOnly: hint.ResultOnly,
	}
	switch hint.Kind {
	case transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SHELL:
		out.Kind = transcript.ToolRenderKindShell
	case transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_DIFF:
		out.Kind = transcript.ToolRenderKindDiff
	case transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SOURCE:
		out.Kind = transcript.ToolRenderKindSource
	case transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_PLAIN:
		out.Kind = transcript.ToolRenderKindPlain
	}
	if hint.ShellDialect != nil {
		switch *hint.ShellDialect {
		case transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_POSIX:
			out.ShellDialect = transcript.ToolShellDialectPosix
		case transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_POWER_SHELL:
			out.ShellDialect = transcript.ToolShellDialectPowerShell
		case transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_WINDOWS_COMMAND:
			out.ShellDialect = transcript.ToolShellDialectWindowsCommand
		}
	}
	return out, nil
}

func patchPresentationFromProto(presentation *transcriptpb.PatchPresentation) (*patchformat.Presentation, error) {
	if value, ok := presentation.Presentation.(*transcriptpb.PatchPresentation_InvalidInput); ok {
		return &patchformat.Presentation{
			Variant:      patchformat.PresentationVariantInvalidInput,
			InvalidInput: &patchformat.InvalidInput{InputDetail: value.InvalidInput.InputDetail},
		}, nil
	}
	value := presentation.Presentation.(*transcriptpb.PatchPresentation_Changes)
	changes := &patchformat.Changes{
		Files: make([]patchformat.FileChange, 0, len(value.Changes.Files)),
	}
	for _, file := range value.Changes.Files {
		projected, err := patchFileChangeFromProto(file)
		if err != nil {
			return nil, fmt.Errorf("patch file: %w", err)
		}
		changes.Files = append(changes.Files, projected)
	}
	return &patchformat.Presentation{
		Variant: patchformat.PresentationVariantChanges,
		Changes: changes,
	}, nil
}

func patchPathFromProto(path *transcriptpb.PatchPath) patchformat.Path {
	return patchformat.Path{Absolute: path.Absolute, Relative: path.Relative}
}

func patchFileChangeFromProto(file *transcriptpb.PatchFileChange) (patchformat.FileChange, error) {
	out := patchformat.FileChange{
		Path:       patchPathFromProto(file.Path),
		Added:      int(file.Added),
		Operations: make([]patchformat.FileOperation, 0, len(file.Operations)),
	}
	if file.Removed != nil {
		removed := int(*file.Removed)
		out.Removed = &removed
	}
	for _, operation := range file.Operations {
		projected := patchFileOperationFromProto(operation)
		out.Operations = append(out.Operations, projected)
	}
	if !out.CountsValid() {
		return patchformat.FileChange{}, fmt.Errorf("patch calculated counts disagree with operations")
	}
	return out, nil
}

func patchFileOperationFromProto(operation *transcriptpb.PatchFileOperation) patchformat.FileOperation {
	out := patchformat.FileOperation{}
	var groups []*transcriptpb.PatchChangeGroup
	switch value := operation.Operation.(type) {
	case *transcriptpb.PatchFileOperation_Add:
		out.Kind = patchformat.FileOperationAdd
		groups = value.Add.Groups
	case *transcriptpb.PatchFileOperation_Update:
		out.Kind = patchformat.FileOperationUpdate
		groups = value.Update.Groups
	case *transcriptpb.PatchFileOperation_Move:
		out.Kind = patchformat.FileOperationMove
		source := patchPathFromProto(value.Move.Source)
		out.Source = &source
		groups = value.Move.Groups
	default:
		deletion := operation.Operation.(*transcriptpb.PatchFileOperation_Delete).Delete
		out.Kind = patchformat.FileOperationDelete
		out.Deletion = &patchformat.WholeFileDeletionOperation{
			ID: patchDeletionIDFromProto(deletion.Id),
		}
		if disposition := deletion.Disposition; disposition != nil {
			out.Deletion.Disposition = &patchformat.WholeFileDeletionDisposition{
				PhysicalGroup: patchformat.WholeFileDeletionGroupID{
					FirstOperation: patchDeletionIDFromProto(disposition.PhysicalGroup.FirstOperation),
				},
				Removed: int(disposition.Removed),
			}
		}
	}
	for _, group := range groups {
		projected := patchformat.ChangeGroup{Lines: make([]patchformat.ChangedLine, 0, len(group.Lines))}
		for _, line := range group.Lines {
			var kind patchformat.ChangedLineKind
			switch line.Kind {
			case transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_ADDED:
				kind = patchformat.ChangedLineAdded
			case transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_REMOVED:
				kind = patchformat.ChangedLineRemoved
			}
			projected.Lines = append(projected.Lines, patchformat.ChangedLine{Kind: kind, Content: line.Content})
		}
		out.Groups = append(out.Groups, projected)
	}
	return out
}

func patchDeletionIDFromProto(id *transcriptpb.PatchDeletionOperationID) patchformat.WholeFileDeletionOperationID {
	return patchformat.WholeFileDeletionOperationID{HunkOrdinal: int(id.HunkOrdinal)}
}
