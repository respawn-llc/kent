import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, expect, it } from "vitest";

import type { ChatTranscriptCommittedRow } from "@/api";
import { appI18n, initializeI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";

import { TranscriptNoticeRow } from "./TranscriptNoticeRow";

beforeAll(initializeI18n);

it("keeps user shell output hidden until expanded and displays it as unparsed text", async () => {
  const services = createTestServices([]);
  const command = "echo '**literal**'";
  const fullText = `${command}\n**literal**\n<result>raw output</result>`;
  const row = {
    Visibility: "ongoing_collapsed",
    Integrity: 0,
    Kind: "notice",
    Locator: { event_sequence: 1, row_ordinal: 1 },
    User: null,
    Assistant: null,
    Tool: null,
    ReasoningTrace: null,
    Notice: {
      Reason: "runtime_diagnostic",
      Severity: "info",
      MessageType: "user_shell_command",
      CompactLabel: command,
      CondensedText: command,
      Diagnostic: { Code: "user_shell_command", Detail: fullText },
    },
    ReviewerFeedback: null,
    ReviewerError: null,
  } satisfies ChatTranscriptCommittedRow;
  render(
    <TestAppProviders services={services}>
      <TranscriptNoticeRow row={row} />
    </TestAppProviders>,
  );
  expect(screen.getByText(command)).toBeVisible();
  expect(screen.queryByText(fullText, { normalizer: (text) => text })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: appI18n.t("app.expand") }));
  await waitFor(() => {
    expect(screen.getByText(fullText, { normalizer: (text) => text })).toBeVisible();
  });
  await userEvent.click(screen.getByRole("button", { name: appI18n.t("app.collapse") }));
  await waitFor(() => {
    expect(screen.queryByText(fullText, { normalizer: (text) => text })).not.toBeInTheDocument();
  });
});

it("shows a detail-visible Thinking update once without disclosure or actions", async () => {
  const services = createTestServices([]);
  const effort = "high";
  const row = {
    Visibility: "detail",
    Integrity: 0,
    Kind: "notice",
    Locator: { event_sequence: 1, row_ordinal: 1 },
    User: null,
    Assistant: null,
    Tool: null,
    ReasoningTrace: null,
    Notice: { Reason: "thinking_update", Severity: "info", ThinkingEffort: effort },
    ReviewerFeedback: null,
    ReviewerError: null,
  } satisfies ChatTranscriptCommittedRow;
  const view = render(
    <TestAppProviders services={services}>
      <TranscriptNoticeRow row={row} />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(view.container.textContent).toBe(appI18n.t("chatTranscript.notice.thinkingSet", { effort }));
  });
  expect(screen.queryByRole("button")).not.toBeInTheDocument();

  view.rerender(
    <TestAppProviders services={services}>
      <TranscriptNoticeRow row={{ ...row, Visibility: "hidden" }} />
    </TestAppProviders>,
  );
  expect(view.container).toBeEmptyDOMElement();
});
