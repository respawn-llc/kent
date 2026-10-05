package runtimeview

import (
	"fmt"
	"slices"

	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
	"core/shared/transcript/patchformat"
)

// ToolPresentationToProto projects the native, merged display facts shared by
// tool starts and committed rows. Tool identity belongs to the enclosing record.
func ToolPresentationToProto(meta *transcript.ToolCallMeta) (*transcriptpb.ToolPresentation, error) {
	if meta == nil {
		return nil, nil
	}
	normalized := transcript.NormalizeToolCallMeta(*meta)
	if !normalized.Valid() {
		return nil, fmt.Errorf("tool presentation is structurally invalid")
	}
	presentations := map[transcript.ToolPresentationKind]transcriptpb.ToolPresentationKind{
		transcript.ToolPresentationDefault:     transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
		transcript.ToolPresentationShell:       transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL,
		transcript.ToolPresentationAskQuestion: transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_ASK_QUESTION,
	}
	behaviors := map[transcript.ToolCallRenderBehavior]transcriptpb.ToolPresentationKind{
		transcript.ToolCallRenderBehaviorDefault:     transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
		transcript.ToolCallRenderBehaviorShell:       transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL,
		transcript.ToolCallRenderBehaviorAskQuestion: transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_ASK_QUESTION,
	}
	out := &transcriptpb.ToolPresentation{
		Presentation:         presentations[normalized.Presentation],
		RenderBehavior:       behaviors[normalized.RenderBehavior],
		IsShell:              normalized.IsShell,
		UserInitiated:        normalized.UserInitiated,
		Suggestions:          slices.Clone(normalized.Suggestions),
		OmitSuccessfulResult: normalized.OmitSuccessfulResult,
		RawOutputRequested:   normalized.RawOutputRequested,
		OutputTruncated:      normalized.OutputTruncated,
		MovedToBackground:    normalized.MovedToBackground,
	}
	// Empty native metadata denotes inapplicability. Preserve nonempty display
	// content verbatim, including whitespace, after the native normalization.
	for _, field := range []struct {
		value  string
		target **string
	}{
		{normalized.Command, &out.Command},
		{normalized.CompactText, &out.CompactText},
		{normalized.InlineMeta, &out.InlineMeta},
		{normalized.TimeoutLabel, &out.TimeoutLabel},
		{normalized.Question, &out.Question},
	} {
		if field.value != "" {
			value := field.value
			*field.target = &value
		}
	}
	if normalized.RecommendedOptionIndex != 0 {
		index, err := protoapi.Int32(normalized.RecommendedOptionIndex, "tool recommended option index")
		if err != nil {
			return nil, err
		}
		out.RecommendedOptionIndex = &index
	}
	if normalized.ShellExitCode != nil {
		code, err := protoapi.Int32(*normalized.ShellExitCode, "tool shell exit code")
		if err != nil {
			return nil, err
		}
		out.ShellExitCode = &code
	}
	if hint := normalized.RenderHint; hint != nil {
		kinds := map[transcript.ToolRenderKind]transcriptpb.ToolRenderKind{
			transcript.ToolRenderKindShell:  transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SHELL,
			transcript.ToolRenderKindDiff:   transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_DIFF,
			transcript.ToolRenderKindSource: transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_SOURCE,
			transcript.ToolRenderKindPlain:  transcriptpb.ToolRenderKind_TOOL_RENDER_KIND_PLAIN,
		}
		out.RenderHint = &transcriptpb.ToolRenderHint{
			Kind:       kinds[hint.Kind],
			ResultOnly: hint.ResultOnly,
		}
		if hint.Path != "" {
			path := hint.Path
			out.RenderHint.Path = &path
		}
		if hint.ShellDialect != "" {
			dialects := map[transcript.ToolShellDialect]transcriptpb.ToolShellDialect{
				transcript.ToolShellDialectPosix:          transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_POSIX,
				transcript.ToolShellDialectPowerShell:     transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_POWER_SHELL,
				transcript.ToolShellDialectWindowsCommand: transcriptpb.ToolShellDialect_TOOL_SHELL_DIALECT_WINDOWS_COMMAND,
			}
			dialect := dialects[hint.ShellDialect]
			out.RenderHint.ShellDialect = &dialect
		}
	}
	if normalized.PatchPresentation != nil {
		patch, err := patchPresentationToProto(*normalized.PatchPresentation)
		if err != nil {
			return nil, fmt.Errorf("tool patch presentation: %w", err)
		}
		out.PatchPresentation = patch
	}
	if err := protoapi.Validate(out); err != nil {
		return nil, fmt.Errorf("tool presentation: %w", err)
	}
	return out, nil
}

func patchPresentationToProto(presentation patchformat.Presentation) (*transcriptpb.PatchPresentation, error) {
	switch presentation.Variant {
	case patchformat.PresentationVariantInvalidInput:
		return &transcriptpb.PatchPresentation{
			Presentation: &transcriptpb.PatchPresentation_InvalidInput{
				InvalidInput: &transcriptpb.PatchInvalidInput{InputDetail: presentation.InvalidInput.InputDetail},
			},
		}, nil
	case patchformat.PresentationVariantChanges:
		changes := &transcriptpb.PatchChanges{
			Files: make([]*transcriptpb.PatchFileChange, 0, len(presentation.Changes.Files)),
		}
		for _, file := range presentation.Changes.Files {
			projected, err := patchFileChangeToProto(file)
			if err != nil {
				return nil, fmt.Errorf("file %q: %w", file.Path.Absolute, err)
			}
			changes.Files = append(changes.Files, projected)
		}
		return &transcriptpb.PatchPresentation{
			Presentation: &transcriptpb.PatchPresentation_Changes{Changes: changes},
		}, nil
	default:
		return nil, fmt.Errorf("unknown patch presentation variant %q", presentation.Variant)
	}
}

func patchPathToProto(path patchformat.Path) *transcriptpb.PatchPath {
	return &transcriptpb.PatchPath{Absolute: path.Absolute, Relative: path.Relative}
}

func patchFileChangeToProto(file patchformat.FileChange) (*transcriptpb.PatchFileChange, error) {
	added, err := protoapi.Int32(file.Added, "added line count")
	if err != nil {
		return nil, err
	}
	out := &transcriptpb.PatchFileChange{
		Path:       patchPathToProto(file.Path),
		Added:      added,
		Operations: make([]*transcriptpb.PatchFileOperation, 0, len(file.Operations)),
	}
	if file.Removed != nil {
		removed, err := protoapi.Int32(*file.Removed, "removed line count")
		if err != nil {
			return nil, err
		}
		out.Removed = &removed
	}
	for _, operation := range file.Operations {
		projected, err := patchFileOperationToProto(operation)
		if err != nil {
			return nil, err
		}
		out.Operations = append(out.Operations, projected)
	}
	return out, nil
}

func patchFileOperationToProto(operation patchformat.FileOperation) (*transcriptpb.PatchFileOperation, error) {
	groups := make([]*transcriptpb.PatchChangeGroup, 0, len(operation.Groups))
	for _, group := range operation.Groups {
		projected := &transcriptpb.PatchChangeGroup{
			Lines: make([]*transcriptpb.PatchChangedLine, 0, len(group.Lines)),
		}
		for _, line := range group.Lines {
			var kind transcriptpb.PatchChangedLineKind
			switch line.Kind {
			case patchformat.ChangedLineAdded:
				kind = transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_ADDED
			case patchformat.ChangedLineRemoved:
				kind = transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_REMOVED
			default:
				return nil, fmt.Errorf("unknown patch changed line kind %q", line.Kind)
			}
			projected.Lines = append(projected.Lines, &transcriptpb.PatchChangedLine{
				Kind: kind, Content: line.Content,
			})
		}
		groups = append(groups, projected)
	}
	out := &transcriptpb.PatchFileOperation{}
	switch operation.Kind {
	case patchformat.FileOperationAdd:
		out.Operation = &transcriptpb.PatchFileOperation_Add{Add: &transcriptpb.PatchAddOperation{Groups: groups}}
	case patchformat.FileOperationUpdate:
		out.Operation = &transcriptpb.PatchFileOperation_Update{Update: &transcriptpb.PatchUpdateOperation{Groups: groups}}
	case patchformat.FileOperationMove:
		out.Operation = &transcriptpb.PatchFileOperation_Move{Move: &transcriptpb.PatchMoveOperation{
			Source: patchPathToProto(*operation.Source), Groups: groups,
		}}
	case patchformat.FileOperationDelete:
		deletion, err := patchDeletionToProto(*operation.Deletion)
		if err != nil {
			return nil, err
		}
		out.Operation = &transcriptpb.PatchFileOperation_Delete{Delete: deletion}
	default:
		return nil, fmt.Errorf("unknown patch file operation kind %q", operation.Kind)
	}
	return out, nil
}

func patchDeletionIDToProto(id patchformat.WholeFileDeletionOperationID) (*transcriptpb.PatchDeletionOperationID, error) {
	ordinal, err := protoapi.Int32(id.HunkOrdinal, "deletion hunk ordinal")
	if err != nil {
		return nil, err
	}
	return &transcriptpb.PatchDeletionOperationID{HunkOrdinal: ordinal}, nil
}

func patchDeletionToProto(deletion patchformat.WholeFileDeletionOperation) (*transcriptpb.PatchDeletionOperation, error) {
	id, err := patchDeletionIDToProto(deletion.ID)
	if err != nil {
		return nil, err
	}
	out := &transcriptpb.PatchDeletionOperation{Id: id}
	if disposition := deletion.Disposition; disposition != nil {
		first, err := patchDeletionIDToProto(disposition.PhysicalGroup.FirstOperation)
		if err != nil {
			return nil, err
		}
		removed, err := protoapi.Int32(disposition.Removed, "deletion removed line count")
		if err != nil {
			return nil, err
		}
		out.Disposition = &transcriptpb.PatchDeletionDisposition{
			PhysicalGroup: &transcriptpb.PatchDeletionGroupID{FirstOperation: first},
			Removed:       removed,
		}
	}
	return out, nil
}
