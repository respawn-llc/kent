import { beforeAll, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { initializeI18n, appI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { TranscriptAskQuestionRow } from "./TranscriptAskQuestionRow";

import type { ChatTranscriptCommittedRow } from "@/api";

import {
  askQuestionCopyText,
  isAskQuestionToolRow,
  type TranscriptAskQuestionToolRow,
} from "./transcriptAskQuestionPolicy";

beforeAll(initializeI18n);

describe("Chat Ask Question policy", () => {
  it("shows a completed question once with immutable radio selection and no collapse control", async () => {
    const row = questionRow("ongoing_collapsed");
    render(
      <TestAppProviders services={createTestServices([])}>
        <TranscriptAskQuestionRow row={row} />
      </TestAppProviders>,
    );
    expect(screen.getAllByText(row.Tool.Presentation.Question)).toHaveLength(1);
    expect(screen.queryByRole("button", { name: appI18n.t("app.collapse") })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: appI18n.t("app.expand") })).not.toBeInTheDocument();
    expect(screen.getByRole("radiogroup")).toBeVisible();
    const options = screen.getAllByRole("radio");
    expect(options).toHaveLength(2);
    expect(options[0]).not.toBeChecked();
    expect(options[1]).toBeChecked();
    for (const option of options) expect(option).toBeDisabled();
    await userEvent.click(screen.getByRole("radio", { name: "first" }));
    expect(options[0]).not.toBeChecked();
    expect(options[1]).toBeChecked();
    expect(screen.getByText("commentary")).toBeVisible();
  });

  it("shows failed questions and their suggestions without inventing an answer", async () => {
    const row = questionRow("ongoing_collapsed", true, null, "diagnostic");
    render(
      <TestAppProviders services={createTestServices([])}>
        <TranscriptAskQuestionRow row={row} />
      </TestAppProviders>,
    );
    expect(screen.getAllByText(row.Tool.Presentation.Question)).toHaveLength(1);
    expect(await screen.findByText(row.Tool.Text)).toBeVisible();
    for (const option of screen.getAllByRole("radio")) {
      expect(option).not.toBeChecked();
      expect(option).toBeDisabled();
    }
  });

  it("claims typed Questions even when the server marks them hidden", () => {
    expect(isAskQuestionToolRow(questionRow("hidden"))).toBe(true);
  });

  it("copies freeform, selected-option, and failed Question content in display order", () => {
    expect(askQuestionCopyText(questionRow("ongoing_collapsed", false, 2, "commentary"))).toBe(
      "question\n\nsecond\n\ncommentary",
    );
    expect(askQuestionCopyText(questionRow("ongoing_collapsed", false, null, "freeform"))).toBe(
      "question\n\nfreeform",
    );
    expect(askQuestionCopyText(questionRow("ongoing_collapsed", true, null, "failed Question"))).toBe(
      "question\n\nfailed Question",
    );
    const historical = questionRow("ongoing_collapsed");
    expect(
      askQuestionCopyText({
        ...historical,
        Tool: {
          ...historical.Tool,
          Text: "**stored answer**\nwithout structured facts",
          QuestionAnswer: null,
        },
      }),
    ).toBe("question\n\n**stored answer**\nwithout structured facts");
  });
});

function questionRow(
  visibility: ChatTranscriptCommittedRow["Visibility"],
  isError = false,
  selectedOptionNumber: number | null = 2,
  freeform: string | null = "commentary",
): TranscriptAskQuestionToolRow {
  return {
    Visibility: visibility,
    Integrity: 0,
    Kind: "tool",
    Locator: { event_sequence: 1, row_ordinal: 1 },
    User: null,
    Assistant: null,
    Tool: {
      StepID: null,
      ToolCallID: "question-call",
      ToolName: "ask_question",
      Text: isError ? (freeform ?? "failed Question") : "answered",
      IsError: isError,
      ResultSummary: null,
      CondensedText: null,
      Presentation: {
        ToolName: "ask_question",
        Presentation: "ask_question",
        RenderBehavior: "ask_question",
        IsShell: false,
        UserInitiated: false,
        Command: "",
        CompactText: "question",
        InlineMeta: "",
        TimeoutLabel: "",
        PatchPresentation: null,
        RenderHint: null,
        Question: "question",
        Suggestions: ["first", "second"],
        RecommendedOptionIndex: 2,
        OmitSuccessfulResult: false,
        RawOutputRequested: false,
        OutputTruncated: false,
        MovedToBackground: false,
        ShellExitCode: null,
      },
      QuestionAnswer: isError ? null : { SelectedOptionNumber: selectedOptionNumber, Freeform: freeform },
    },
    ReasoningTrace: null,
    Notice: null,
    ReviewerFeedback: null,
    ReviewerError: null,
  } as const;
}
