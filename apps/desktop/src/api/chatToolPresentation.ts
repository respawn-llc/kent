import * as T from "@app/server-api-contract/gen/kent/api/transcript/tool_presentation_pb";
import type {
  ChatPatchGroup,
  ChatPatchOperation,
  ChatPatchPath,
  ChatPatchPresentation,
  ChatToolPresentation,
} from "./chatTranscriptTypes";
import { enumValue, required } from "./chatWire";
import { ContractError } from "./errors";

function presentationKind(value: T.ToolPresentationKind): ChatToolPresentation["Presentation"] {
  return enumValue(value, {
    [T.ToolPresentationKind.DEFAULT]: "default",
    [T.ToolPresentationKind.SHELL]: "shell",
    [T.ToolPresentationKind.ASK_QUESTION]: "ask_question",
  });
}

export function toolPresentation(
  value: T.ToolPresentation | undefined,
  toolName: string | undefined,
): ChatToolPresentation | null {
  if (value === undefined) return null;
  return {
    ToolName: required(toolName),
    Presentation: presentationKind(value.presentation),
    RenderBehavior: presentationKind(value.renderBehavior),
    IsShell: value.isShell,
    UserInitiated: value.userInitiated,
    Command: value.command ?? "",
    CompactText: value.compactText ?? "",
    InlineMeta: value.inlineMeta ?? "",
    TimeoutLabel: value.timeoutLabel ?? "",
    Question: value.question ?? "",
    Suggestions: [...value.suggestions],
    RecommendedOptionIndex: value.recommendedOptionIndex ?? 0,
    OmitSuccessfulResult: value.omitSuccessfulResult,
    RawOutputRequested: value.rawOutputRequested,
    OutputTruncated: value.outputTruncated,
    MovedToBackground: value.movedToBackground,
    ShellExitCode: value.shellExitCode ?? null,
    RenderHint: renderHint(value.renderHint),
    PatchPresentation:
      value.patchPresentation === undefined ? null : patchPresentation(value.patchPresentation),
  };
}

function renderHint(
  value: T.ToolRenderHint | undefined,
): Exclude<ChatToolPresentation["RenderHint"], undefined> {
  if (value === undefined) return null;
  return {
    Kind: enumValue(value.kind, {
      [T.ToolRenderKind.SHELL]: "shell",
      [T.ToolRenderKind.DIFF]: "diff",
      [T.ToolRenderKind.SOURCE]: "source",
      [T.ToolRenderKind.PLAIN]: "plain",
    }),
    Path: value.path ?? "",
    ResultOnly: value.resultOnly,
    ShellDialect:
      value.shellDialect === undefined
        ? ""
        : enumValue(value.shellDialect, {
            [T.ToolShellDialect.POSIX]: "posix",
            [T.ToolShellDialect.POWER_SHELL]: "powershell",
            [T.ToolShellDialect.WINDOWS_COMMAND]: "windows_command",
          }),
  };
}

function path(value: T.PatchPath): ChatPatchPath {
  return { Absolute: value.absolute, Relative: value.relative };
}

function group(value: T.PatchChangeGroup): ChatPatchGroup {
  return {
    Lines: value.lines.map((line) => ({
      Kind: enumValue(line.kind, {
        [T.PatchChangedLineKind.ADDED]: "added",
        [T.PatchChangedLineKind.REMOVED]: "removed",
      }),
      Content: line.content,
    })),
  };
}

function operation(value: T.PatchFileOperation): ChatPatchOperation {
  const branch = value.operation;
  switch (branch.case) {
    case "add":
    case "update":
      return { Kind: branch.case, Source: null, Groups: branch.value.groups.map(group), Deletion: null };
    case "move":
      return {
        Kind: "move",
        Source: path(required(branch.value.source)),
        Groups: branch.value.groups.map(group),
        Deletion: null,
      };
    case "delete": {
      const disposition = branch.value.disposition;
      return {
        Kind: "delete",
        Source: null,
        Groups: [],
        Deletion: {
          id: { hunk_ordinal: required(branch.value.id).hunkOrdinal },
          disposition:
            disposition === undefined
              ? null
              : {
                  physical_group: {
                    first_operation: {
                      hunk_ordinal: required(required(disposition.physicalGroup).firstOperation).hunkOrdinal,
                    },
                  },
                  removed: disposition.removed,
                },
        },
      };
    }
    case undefined:
      throw new ContractError("Patch operation is missing.");
  }
}

function patchPresentation(value: T.PatchPresentation): ChatPatchPresentation {
  const branch = value.presentation;
  switch (branch.case) {
    case "invalidInput":
      return { Variant: "invalid_input", InvalidInput: { InputDetail: branch.value.inputDetail } };
    case "changes":
      return {
        Variant: "changes",
        InvalidInput: null,
        Files: branch.value.files.map((file) => ({
          Path: path(required(file.path)),
          Added: file.added,
          Removed: file.removed ?? null,
          Operations: file.operations.map(operation),
        })),
      };
    case undefined:
      throw new ContractError("Patch presentation is missing.");
  }
}
