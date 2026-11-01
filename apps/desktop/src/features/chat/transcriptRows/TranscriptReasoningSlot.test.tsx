import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, expect, it } from "bun:test";
import type { ReactElement } from "react";

import type { ChatTranscriptCommittedRow } from "@/api";
import {
  TranscriptWindow,
  type TranscriptReasoningDisplayGroup,
  type TranscriptRenderSlots,
} from "@/app-facade";
import { appI18n, initializeI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { hydration } from "@/test-support/transcript-window";
import { installResizeObserverGeometry } from "@/test-support/resize-observer";
import { TranscriptWindowView } from "@/shared/transcript-window";

import { TranscriptReasoningSlot } from "./TranscriptReasoningSlot";

beforeAll(initializeI18n);

let geometry: ReturnType<typeof installResizeObserverGeometry>;
const originalOffsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight");
const originalOffsetWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth");

beforeEach(() => {
  geometry = installResizeObserverGeometry();
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", { configurable: true, get: () => 600 });
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", { configurable: true, get: () => 800 });
});

afterEach(() => {
  geometry.restore();
  if (originalOffsetHeight === undefined) Reflect.deleteProperty(HTMLElement.prototype, "offsetHeight");
  else Object.defineProperty(HTMLElement.prototype, "offsetHeight", originalOffsetHeight);
  if (originalOffsetWidth === undefined) Reflect.deleteProperty(HTMLElement.prototype, "offsetWidth");
  else Object.defineProperty(HTMLElement.prototype, "offsetWidth", originalOffsetWidth);
});

const reasoningIdentity = {
  Provider: { ItemID: "provider-reasoning", SummaryIndex: 0 },
  Kent: null,
} as const;

const transcriptSlots: TranscriptRenderSlots<ReactElement | null> = {
  user: () => null,
  assistant: () => null,
  tool: () => null,
  reasoning: (group) => <TranscriptReasoningSlot item={group} />,
  notice: () => null,
  thinkingStatus: () => null,
};

function committedMember(
  sequence: number,
  text: string,
  durationMs: number | null = null,
  compactText = text,
): TranscriptReasoningDisplayGroup["members"][0] {
  const row = reasoningRow(sequence, text, durationMs, { compactText });
  const value = row.ReasoningTrace;
  if (value === null) throw new Error("Reasoning row fixture is missing its payload.");
  return {
    kind: "reasoning_trace",
    state: "committed",
    key: `${String(row.Locator.event_sequence)}:${String(row.Locator.row_ordinal)}`,
    row,
    value,
  };
}

function reasoningRow(
  sequence: number,
  text: string,
  durationMs: number | null,
  options: Readonly<{
    provisionalIdentity?: NonNullable<ChatTranscriptCommittedRow["ReasoningTrace"]>["ProvisionalIdentity"];
    compactText?: string;
  }> = {},
): ChatTranscriptCommittedRow {
  const { provisionalIdentity = null, compactText = text } = options;
  return {
    Visibility: "ongoing",
    Integrity: 0,
    Kind: "reasoning_trace",
    Locator: { event_sequence: sequence, row_ordinal: 1 },
    User: null,
    Assistant: null,
    ToolCall: null,
    Tool: null,
    ReasoningTrace: {
      StepID: "step",
      CompactText: compactText,
      Text: text,
      duration_ms: durationMs,
      ProvisionalIdentity: provisionalIdentity,
    },
    Notice: null,
    ReviewerFeedback: null,
    ReviewerError: null,
  };
}

it("shows grouped reasoning in order and copies its combined plain text once committed", async () => {
  const user = userEvent.setup();
  const firstText = "First trace detail";
  const secondText = "Second trace detail";
  const group: TranscriptReasoningDisplayGroup = {
    kind: "reasoning_group",
    key: "1:1",
    members: [committedMember(1, firstText), committedMember(2, secondText)],
  };

  render(
    <TestAppProviders skipAnimations services={createTestServices([])}>
      <TranscriptReasoningSlot item={group} />
    </TestAppProviders>,
  );

  await user.click(screen.getByRole("button", { name: appI18n.t("app.expand") }));
  await waitFor(() => {
    const body = expandedDisclosureBody();
    expect(within(body).getByText(firstText)).toBeVisible();
    expect(within(body).getByText(secondText)).toBeVisible();
  });

  const body = expandedDisclosureBody();
  const first = within(body).getByText(firstText);
  const second = within(body).getByText(secondText);
  expect(first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  await user.click(screen.getByRole("button", { name: appI18n.t("chatTranscript.copy") }));
  await waitFor(async () => {
    expect(await navigator.clipboard.readText()).toBe(`${firstText}\n\n${secondText}`);
  });
});

it("copies equal-text traces independently", async () => {
  const user = userEvent.setup();
  const text = "Repeated trace detail";
  const group: TranscriptReasoningDisplayGroup = {
    kind: "reasoning_group",
    key: "1:1",
    members: [committedMember(1, text), committedMember(2, text)],
  };

  render(
    <TestAppProviders skipAnimations services={createTestServices([])}>
      <TranscriptReasoningSlot item={group} />
    </TestAppProviders>,
  );

  await user.click(screen.getByRole("button", { name: appI18n.t("app.expand") }));
  const body = expandedDisclosureBody();
  expect(within(body).getAllByText(text)).toHaveLength(2);
  await user.click(screen.getByRole("button", { name: appI18n.t("chatTranscript.copy") }));
  await waitFor(async () => {
    expect(await navigator.clipboard.readText()).toBe(`${text}\n\n${text}`);
  });
});

it("keeps one expanded disclosure when a preceding trace arrives and the live trace commits", async () => {
  const user = userEvent.setup();
  const transcript = new TranscriptWindow();
  transcript.dispatch({
    kind: "initial-hydration",
    hydration: {
      ...hydration([]),
      ActiveReasoningTraces: [
        {
          StepID: "step",
          Identity: reasoningIdentity,
          CompactText: "Live A summary",
          Text: "Live A detail",
        },
      ],
    },
  });
  const content = () => (
    <TestAppProviders skipAnimations services={createTestServices([])}>
      <TranscriptWindowView
        boundaryErrorMessage={(error) => error.message}
        estimateSize={() => 60}
        onInput={() => undefined}
        retryLabel="Retry"
        slots={transcriptSlots}
        snapshot={transcript.snapshot}
      />
    </TestAppProviders>
  );
  const view = render(content());

  await user.click(screen.getByRole("button", { name: appI18n.t("app.expand") }));
  const disclosure = screen.getByRole("button", { name: appI18n.t("app.collapse") });
  expect(expandedDisclosureBody()).toHaveTextContent("Live A detail");
  expect(screen.queryByRole("button", { name: appI18n.t("chatTranscript.copy") })).not.toBeInTheDocument();

  transcript.dispatch({ kind: "committed-row", row: reasoningRow(1, "Completed B detail", 2500) });
  view.rerender(content());

  expect(screen.getByRole("button", { name: appI18n.t("app.collapse") })).toBe(disclosure);
  expect(screen.getByText(appI18n.t("chatTranscript.thinking"))).toBeVisible();
  expect(screen.queryByRole("button", { name: appI18n.t("chatTranscript.copy") })).not.toBeInTheDocument();

  transcript.dispatch({
    kind: "committed-row",
    row: reasoningRow(2, "Live A detail", 1000, { provisionalIdentity: reasoningIdentity }),
  });
  view.rerender(content());

  expect(screen.getByRole("button", { name: appI18n.t("app.collapse") })).toBe(disclosure);
  expect(screen.getByText(durationLabel(3500))).toBeVisible();
  await user.click(screen.getByRole("button", { name: appI18n.t("chatTranscript.copy") }));
  await waitFor(async () => {
    expect(await navigator.clipboard.readText()).toBe("Completed B detail\n\nLive A detail");
  });
});

it.each([
  { durations: [2500, 1000] as const, total: 3500 },
  { durations: [null, 1000] as const, total: 1000 },
])("shows the sum of supplied durations $durations", ({ durations, total }) => {
  const group: TranscriptReasoningDisplayGroup = {
    kind: "reasoning_group",
    key: "1:1",
    members: [
      committedMember(1, "First trace", durations[0]),
      committedMember(2, "Second trace", durations[1]),
    ],
  };

  render(
    <TestAppProviders skipAnimations services={createTestServices([])}>
      <TranscriptReasoningSlot item={group} />
    </TestAppProviders>,
  );

  expect(screen.getByText(durationLabel(total))).toBeVisible();
});

it("treats a supplied zero duration as a measurement", () => {
  const group: TranscriptReasoningDisplayGroup = {
    kind: "reasoning_group",
    key: "1:1",
    members: [committedMember(1, "First trace", 0), committedMember(2, "Second trace")],
  };

  render(
    <TestAppProviders skipAnimations services={createTestServices([])}>
      <TranscriptReasoningSlot item={group} />
    </TestAppProviders>,
  );

  expect(screen.getByText(durationLabel(0))).toBeVisible();
});

it("uses the first content-bearing compact preview when no trace has a duration", () => {
  const group: TranscriptReasoningDisplayGroup = {
    kind: "reasoning_group",
    key: "1:1",
    members: [
      committedMember(1, "", null, ""),
      committedMember(2, "Second trace detail", null, "Second trace preview"),
    ],
  };

  render(
    <TestAppProviders skipAnimations services={createTestServices([])}>
      <TranscriptReasoningSlot item={group} />
    </TestAppProviders>,
  );

  expect(screen.getByText("Second trace preview")).toBeVisible();
});

function expandedDisclosureBody(): HTMLElement {
  const disclosure = screen.getByRole("button", { name: appI18n.t("app.collapse") });
  const bodyID = disclosure.getAttribute("aria-controls");
  if (bodyID === null) throw new Error("Expanded reasoning disclosure has no body identity.");
  return screen.getByText((_, element) => element?.id === bodyID, {
    selector: "[id]",
  });
}

function durationLabel(durationMs: number): string {
  return appI18n.t("chatTranscript.thoughtDuration", {
    seconds: new Intl.NumberFormat(appI18n.language, { maximumFractionDigits: 1 }).format(durationMs / 1000),
  });
}
