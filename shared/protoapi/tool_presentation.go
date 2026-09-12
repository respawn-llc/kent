package protoapi

import (
	"fmt"
	"slices"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
	"core/shared/transcript/patchformat"
)

// ToolPresentationFromProto restores native rendering facts using the tool
// identity on the enclosing row or start. Display normalization is server-owned.
func ToolPresentationFromProto(toolName string, presentation *transcriptpb.ToolPresentation) (*transcript.ToolCallMeta, error) {
	if presentation == nil {
		return nil, nil
	}
	if err := Validate(presentation); err != nil {
		return nil, fmt.Errorf("tool presentation: %w", err)
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
	default:
		return nil, fmt.Errorf("unknown tool presentation kind %d", presentation.Presentation)
	}
	switch presentation.RenderBehavior {
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT:
		out.RenderBehavior = transcript.ToolCallRenderBehaviorDefault
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL:
		out.RenderBehavior = transcript.ToolCallRenderBehaviorShell
	case transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_ASK_QUESTION:
		out.RenderBehavior = transcript.ToolCallRenderBehaviorAskQuestion
	default:
		return nil, fmt.Errorf("unknown tool render behavior %d", presentation.RenderBehavior)
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
	if !out.Valid() {
		return nil, fmt.Errorf("tool presentation is structurally invalid for tool %q", toolName)
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
	default:
		return nil, fmt.Errorf("unknown tool render hint kind %d", hint.Kind)
	}
	if hint.ShellDialect != nil {
		switch *hint.ShellDialect {
		case transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_POSIX:
			out.ShellDialect = transcript.ToolShellDialectPosix
		case transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_POWER_SHELL:
			out.ShellDialect = transcript.ToolShellDialectPowerShell
		case transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_WINDOWS_COMMAND:
			out.ShellDialect = transcript.ToolShellDialectWindowsCommand
		default:
			return nil, fmt.Errorf("unknown tool shell dialect %d", *hint.ShellDialect)
		}
	}
	return out, nil
}

func patchPresentationFromProto(presentation *transcriptpb.PatchPresentation) (*patchformat.Presentation, error) {
	switch value := presentation.Presentation.(type) {
	case *transcriptpb.PatchPresentation_InvalidInput:
		if value == nil || value.InvalidInput == nil {
			return nil, fmt.Errorf("patch invalid input is required")
		}
		return &patchformat.Presentation{
			Variant:      patchformat.PresentationVariantInvalidInput,
			InvalidInput: &patchformat.InvalidInput{InputDetail: value.InvalidInput.InputDetail},
		}, nil
	case *transcriptpb.PatchPresentation_Changes:
		if value == nil || value.Changes == nil {
			return nil, fmt.Errorf("patch changes are required")
		}
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
	default:
		return nil, fmt.Errorf("unknown patch presentation variant %T", value)
	}
}

func patchPathFromProto(path *transcriptpb.PatchPath) patchformat.Path {
	return patchformat.Path{Absolute: path.Absolute, Relative: path.Relative}
}

func patchFileChangeFromProto(file *transcriptpb.PatchFileChange) (patchformat.FileChange, error) {
	if file == nil || file.Path == nil {
		return patchformat.FileChange{}, fmt.Errorf("patch file and path are required")
	}
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
		projected, err := patchFileOperationFromProto(operation)
		if err != nil {
			return patchformat.FileChange{}, err
		}
		out.Operations = append(out.Operations, projected)
	}
	return out, nil
}

func patchFileOperationFromProto(operation *transcriptpb.PatchFileOperation) (patchformat.FileOperation, error) {
	if operation == nil {
		return patchformat.FileOperation{}, fmt.Errorf("patch file operation is required")
	}
	out := patchformat.FileOperation{}
	var groups []*transcriptpb.PatchChangeGroup
	switch value := operation.Operation.(type) {
	case *transcriptpb.PatchFileOperation_Add:
		if value == nil || value.Add == nil {
			return patchformat.FileOperation{}, fmt.Errorf("patch add operation is required")
		}
		out.Kind = patchformat.FileOperationAdd
		groups = value.Add.Groups
	case *transcriptpb.PatchFileOperation_Update:
		if value == nil || value.Update == nil {
			return patchformat.FileOperation{}, fmt.Errorf("patch update operation is required")
		}
		out.Kind = patchformat.FileOperationUpdate
		groups = value.Update.Groups
	case *transcriptpb.PatchFileOperation_Move:
		if value == nil || value.Move == nil || value.Move.Source == nil {
			return patchformat.FileOperation{}, fmt.Errorf("patch move operation and source are required")
		}
		out.Kind = patchformat.FileOperationMove
		source := patchPathFromProto(value.Move.Source)
		out.Source = &source
		groups = value.Move.Groups
	case *transcriptpb.PatchFileOperation_Delete:
		if value == nil || value.Delete == nil || value.Delete.Id == nil {
			return patchformat.FileOperation{}, fmt.Errorf("patch deletion operation and id are required")
		}
		out.Kind = patchformat.FileOperationDelete
		out.Deletion = &patchformat.WholeFileDeletionOperation{
			ID: patchDeletionIDFromProto(value.Delete.Id),
		}
		if disposition := value.Delete.Disposition; disposition != nil {
			if disposition.PhysicalGroup == nil || disposition.PhysicalGroup.FirstOperation == nil {
				return patchformat.FileOperation{}, fmt.Errorf("patch deletion physical group and first operation are required")
			}
			out.Deletion.Disposition = &patchformat.WholeFileDeletionDisposition{
				PhysicalGroup: patchformat.WholeFileDeletionGroupID{
					FirstOperation: patchDeletionIDFromProto(disposition.PhysicalGroup.FirstOperation),
				},
				Removed: int(disposition.Removed),
			}
		}
	default:
		return patchformat.FileOperation{}, fmt.Errorf("unknown patch file operation %T", value)
	}
	for _, group := range groups {
		if group == nil {
			return patchformat.FileOperation{}, fmt.Errorf("patch change group is required")
		}
		projected := patchformat.ChangeGroup{Lines: make([]patchformat.ChangedLine, 0, len(group.Lines))}
		for _, line := range group.Lines {
			if line == nil {
				return patchformat.FileOperation{}, fmt.Errorf("patch changed line is required")
			}
			var kind patchformat.ChangedLineKind
			switch line.Kind {
			case transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_ADDED:
				kind = patchformat.ChangedLineAdded
			case transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_REMOVED:
				kind = patchformat.ChangedLineRemoved
			default:
				return patchformat.FileOperation{}, fmt.Errorf("unknown patch changed line kind %d", line.Kind)
			}
			projected.Lines = append(projected.Lines, patchformat.ChangedLine{Kind: kind, Content: line.Content})
		}
		out.Groups = append(out.Groups, projected)
	}
	return out, nil
}

func patchDeletionIDFromProto(id *transcriptpb.PatchDeletionOperationID) patchformat.WholeFileDeletionOperationID {
	return patchformat.WholeFileDeletionOperationID{HunkOrdinal: int(id.HunkOrdinal)}
}
