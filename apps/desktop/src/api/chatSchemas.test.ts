import { describe, expect, it } from "vitest";
import { create, decode, encode } from "@app/server-api-contract";
import * as T from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import {
  PatchChangedLineKind,
  ToolPresentationKind,
} from "@app/server-api-contract/gen/kent/api/transcript/tool_presentation_pb";
import { committedRow } from "./chatTranscriptRows";

const stepId = "123e4567-e89b-42d3-a456-426614174000";

describe("committed Thinking notices", () => {
  it("preserves the selected effort on a detail-visible configuration row", () => {
    const row = committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, thinkingRow("high"))));
    expect(row.Visibility).toBe("detail");
    expect(row.Notice?.ThinkingEffort).toBe("high");
  });

  it("rejects Thinking notices without a nonblank selected effort", () => {
    for (const effort of ["", " \t\n", undefined]) {
      expect(() => encode(T.CommittedRowSchema, thinkingRow(effort))).toThrow();
    }
  });

  it("rejects Thinking effort on other notice reasons", () => {
    const notice = create(T.NoticeRowSchema, {
      reason: T.NoticeReason.LEGACY_UNTYPED_NOTICE,
      severity: T.NoticeSeverity.INFO,
      legacyText: "notice",
    });
    expect(() => encode(T.NoticeRowSchema, notice)).not.toThrow();
    notice.thinkingEffort = "high";
    expect(() => encode(T.NoticeRowSchema, notice)).toThrow();
  });

  it.each([
    { severity: T.NoticeSeverity.WARNING },
    { severity: T.NoticeSeverity.ERROR },
    { messageType: T.NoticeMessageType.HEADLESS_MODE },
    { legacyText: "notice" },
    { cacheWarning: { scope: "session", reason: "cache", visibility: T.EntryVisibility.DETAIL } },
    { compaction: { count: 1 } },
    { toolOutputRepair: { kind: "fresh_resource", count: 1 } },
    { providerModelMismatch: { requestedModel: "requested", servedModel: "served" } },
    { background: { activityId: "activity", processId: "process" } },
  ])("rejects competing Thinking notice facts: %o", (payload) => {
    const notice = create(T.NoticeRowSchema, {
      reason: T.NoticeReason.THINKING_UPDATE,
      severity: T.NoticeSeverity.INFO,
      thinkingEffort: "high",
      ...payload,
    });
    expect(() => encode(T.NoticeRowSchema, notice)).toThrow();
  });
});

function thinkingRow(effort: string | undefined) {
  return create(T.CommittedRowSchema, {
    visibility: T.EntryVisibility.DETAIL,
    integrity: T.RowIntegrity.VALID,
    locator: { eventSequence: 1n, rowOrdinal: 1 },
    row: {
      case: "notice",
      value: {
        reason: T.NoticeReason.THINKING_UPDATE,
        severity: T.NoticeSeverity.INFO,
        ...(effort === undefined ? {} : { thinkingEffort: effort }),
      },
    },
  });
}

it("carries hosted search facts through the generated boundary and Chat adaptation", () => {
  for (const webSearch of [
    undefined,
    {
      action: { case: "search" as const, value: { queries: ["first", "second"] } },
      results: [{ kind: T.WebSearchResultKind.IMAGE, destination: "https://example.com/image" }],
      sources: ["https://example.com", "https://example.com"],
    },
  ]) {
    const row = create(T.CommittedRowSchema, {
      visibility: T.EntryVisibility.ONGOING_COLLAPSED,
      integrity: T.RowIntegrity.VALID,
      locator: { eventSequence: 1n, rowOrdinal: 1 },
      row: { case: "tool", value: { toolCallId: "search-1", toolName: "web_search", webSearch } },
    });
    const adapted = committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, row)));
    expect(adapted.Tool?.WebSearch).toEqual(
      webSearch === undefined
        ? null
        : {
            action: { kind: "search", queries: ["first", "second"] },
            results: [{ kind: "image", title: null, destination: "https://example.com/image" }],
            sources: ["https://example.com", "https://example.com"],
          },
    );
  }
});

describe("committed Ask Question rows", () => {
  it("preserves typed answers and absent or one-based recommendations", () => {
    for (const recommendedOptionIndex of [undefined, 1, 2]) {
      const row = askQuestionRow({
        recommendedOptionIndex,
        questionAnswer: { selectedOptionNumber: 2, freeform: "keep the split" },
      });
      const product = committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, row)));
      expect(product.Tool?.QuestionAnswer).toEqual({ SelectedOptionNumber: 2, Freeform: "keep the split" });
      expect(product.Tool?.Presentation?.RecommendedOptionIndex).toBe(recommendedOptionIndex ?? 0);
    }
  });

  it("preserves a stored successful Question without typed answer facts", () => {
    const row = decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, askQuestionRow({})));
    expect(committedRow(row).Tool?.QuestionAnswer).toBeNull();
  });

  it("preserves unknown fields and rejects undeclared enum values", () => {
    const row = askQuestionRow({});
    const unknown = new Uint8Array([0xb8, 0x3e, 17]);
    const known = encode(T.CommittedRowSchema, row);
    const wire = new Uint8Array([...known, ...unknown]);
    expect(encode(T.CommittedRowSchema, decode(T.CommittedRowSchema, wire))).toEqual(wire);
    const invalidEnum = new Uint8Array([...known, 0x08, 0x7f]);
    expect(() => decode(T.CommittedRowSchema, invalidEnum)).toThrow();
  });

  it("preserves absent tool identities on malformed history while requiring them on valid rows", () => {
    const row = create(T.CommittedRowSchema, {
      visibility: T.EntryVisibility.DETAIL,
      integrity: T.RowIntegrity.RECOVERABLE_MALFORMED,
      locator: { eventSequence: 1n, rowOrdinal: 1 },
      row: { case: "tool", value: { text: "retained output" } },
    });
    expect(committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, row))).Tool).toMatchObject({
      ToolCallID: null,
      ToolName: null,
      Text: "retained output",
      Presentation: null,
    });
    row.integrity = T.RowIntegrity.VALID;
    expect(() => encode(T.CommittedRowSchema, row)).toThrow();
  });

  it("rejects recommendations outside the offered option range", () => {
    for (const recommendedOptionIndex of [-1, 0, 3]) {
      expect(() =>
        encode(
          T.CommittedRowSchema,
          askQuestionRow({
            recommendedOptionIndex,
            questionAnswer: { freeform: "answer" },
          }),
        ),
      ).toThrow();
    }
  });

  it("requires nonblank failure detail for failed Questions", () => {
    for (const text of ["canceled", " canceled "])
      expect(() => encode(T.CommittedRowSchema, askQuestionRow({ isError: true, text }))).not.toThrow();
    for (const text of ["", " \t\n"])
      expect(() => encode(T.CommittedRowSchema, askQuestionRow({ isError: true, text }))).toThrow();
  });

  it("preserves zero elapsed time and historical commit instants without conflating absence", () => {
    const base = {
      visibility: T.EntryVisibility.ONGOING,
      integrity: T.RowIntegrity.VALID,
      locator: { eventSequence: 1n, rowOrdinal: 1 },
    };
    for (const seconds of [-1n, 0n, 1n]) {
      const row = create(T.CommittedRowSchema, {
        ...base,
        row: { case: "user", value: { text: "hello", committedAt: { seconds, nanos: 0 } } },
      });
      expect(
        committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, row))).User
          ?.committed_at_unix_ms,
      ).toBe(Number(seconds) * 1000);
    }
    const trace = create(T.CommittedRowSchema, {
      ...base,
      row: {
        case: "reasoningTrace",
        value: { stepId, compactText: "thought", text: "full thought", duration: { seconds: 0n, nanos: 0 } },
      },
    });
    expect(
      committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, trace))).ReasoningTrace,
    ).toMatchObject({
      duration_ms: 0,
      Text: "full thought",
      CompactText: "thought",
    });
  });

  it("preserves structured patch lines, pending deletion identity, and displayed content", () => {
    const row = create(T.CommittedRowSchema, {
      visibility: T.EntryVisibility.ONGOING_COLLAPSED,
      integrity: T.RowIntegrity.VALID,
      locator: { eventSequence: 2n, rowOrdinal: 1 },
      row: {
        case: "tool",
        value: {
          toolCallId: "patch-call",
          toolName: "patch",
          text: "",
          resultSummary: "Two files",
          condensedText: "patch summary",
          presentation: {
            presentation: ToolPresentationKind.DEFAULT,
            renderBehavior: ToolPresentationKind.DEFAULT,
            compactText: "Update files",
            patchPresentation: {
              presentation: {
                case: "changes",
                value: {
                  files: [
                    {
                      path: { absolute: "/workspace/a", relative: "a" },
                      added: 1,
                      removed: 0,
                      operations: [
                        {
                          operation: {
                            case: "add",
                            value: {
                              groups: [{ lines: [{ kind: PatchChangedLineKind.ADDED, content: "" }] }],
                            },
                          },
                        },
                      ],
                    },
                    {
                      path: { absolute: "/workspace/b", relative: "b" },
                      added: 0,
                      operations: [{ operation: { case: "delete", value: { id: { hunkOrdinal: 0 } } } }],
                    },
                  ],
                },
              },
            },
          },
        },
      },
    });
    const product = committedRow(decode(T.CommittedRowSchema, encode(T.CommittedRowSchema, row)));
    expect(product.Tool).toMatchObject({
      Text: "",
      ResultSummary: "Two files",
      CondensedText: "patch summary",
      Presentation: {
        CompactText: "Update files",
        PatchPresentation: {
          Variant: "changes",
          Files: [
            {
              Added: 1,
              Removed: 0,
              Operations: [{ Kind: "add", Groups: [{ Lines: [{ Kind: "added", Content: "" }] }] }],
            },
            {
              Added: 0,
              Removed: null,
              Operations: [{ Kind: "delete", Deletion: { id: { hunk_ordinal: 0 }, disposition: null } }],
            },
          ],
        },
      },
    });
  });
});

function askQuestionRow(input: {
  isError?: boolean;
  text?: string;
  recommendedOptionIndex?: number | undefined;
  questionAnswer?: { selectedOptionNumber?: number; freeform?: string };
}) {
  return create(T.CommittedRowSchema, {
    visibility: T.EntryVisibility.ONGOING_COLLAPSED,
    integrity: T.RowIntegrity.VALID,
    locator: { eventSequence: 1n, rowOrdinal: 1 },
    row: {
      case: "tool",
      value: {
        stepId,
        toolCallId: "call-question",
        toolName: "ask_question",
        text: input.text ?? "answered",
        isError: input.isError ?? false,
        ...(input.questionAnswer === undefined ? {} : { questionAnswer: input.questionAnswer }),
        presentation: {
          presentation: ToolPresentationKind.ASK_QUESTION,
          renderBehavior: ToolPresentationKind.ASK_QUESTION,
          question: "Which option?",
          suggestions: ["first", "second"],
          ...(input.recommendedOptionIndex === undefined
            ? {}
            : { recommendedOptionIndex: input.recommendedOptionIndex }),
        },
      },
    },
  });
}
