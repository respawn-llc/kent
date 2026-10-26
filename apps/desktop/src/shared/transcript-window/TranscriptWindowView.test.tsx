import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ComponentProps, ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "bun:test";

import { TranscriptWindow, type TranscriptCommittedItem, type TranscriptRenderSlots } from "@/app-facade";
import { hydration, row } from "@/test-support/transcript-window";
import {
  installResizeObserverGeometry,
  installVirtualizedScrollGeometry,
} from "@/test-support/resize-observer";

import { TranscriptWindowView } from "./index";

let geometry: ReturnType<typeof installResizeObserverGeometry>;
const originalOffsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight");
const originalOffsetWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth");

const reasoningIdentity = {
  Provider: { ItemID: "reasoning-item", SummaryIndex: 0 },
  Kent: null,
} as const;

function committedPromotions(): readonly TranscriptCommittedItem["row"][] {
  return [
    {
      Kind: "assistant",
      Locator: { event_sequence: 1, row_ordinal: 1 },
      Visibility: "ongoing",
      Integrity: 0,
      User: null,
      Assistant: {
        StepID: "step",
        StreamID: "assistant-stream",
        Text: "Committed assistant",
        CondensedText: "",
        Phase: "commentary",
        committed_at_unix_ms: null,
      },
      ToolCall: null,
      Tool: null,
      ReasoningTrace: null,
      Notice: null,
      ReviewerFeedback: null,
      ReviewerError: null,
    },
    {
      Kind: "tool",
      Locator: { event_sequence: 2, row_ordinal: 1 },
      Visibility: "ongoing",
      Integrity: 0,
      User: null,
      Assistant: null,
      ToolCall: null,
      Tool: {
        ViewedImageKind: null,
        StepID: "step",
        ToolCallID: "tool-call",
        ToolName: "shell",
        Text: "Committed tool",
        IsError: false,
        AnsweredBySessionID: null,
        AnsweredBySessionShortID: null,
        ResultSummary: "",
        CondensedText: "",
        Presentation: null,
      },
      ReasoningTrace: null,
      Notice: null,
      ReviewerFeedback: null,
      ReviewerError: null,
    },
    {
      Kind: "reasoning_trace",
      Locator: { event_sequence: 3, row_ordinal: 1 },
      Visibility: "ongoing",
      Integrity: 0,
      User: null,
      Assistant: null,
      ToolCall: null,
      Tool: null,
      ReasoningTrace: {
        StepID: "step",
        CompactText: "Committed reasoning",
        Text: "Committed reasoning detail",
        duration_ms: null,
        ProvisionalIdentity: reasoningIdentity,
      },
      Notice: null,
      ReviewerFeedback: null,
      ReviewerError: null,
    },
  ];
}

const slots: TranscriptRenderSlots<ReactElement | null> = {
  user: (item) => <div data-testid={`family-${item.kind}`}>{item.value.Text}</div>,
  assistant: (item) => <div data-testid={`family-${item.kind}`}>{item.state}</div>,
  tool: (item) => <div data-testid={`family-${item.kind}`}>{item.state}</div>,
  reasoning: (item) => <div data-testid={`family-${item.kind}`}>{item.state}</div>,
  notice: (item) => <div data-testid={`family-${item.kind}`} />,
  thinkingStatus: (item) =>
    item === null ? null : (
      <div data-testid="family-thinking-status">{item.kind === "text" ? item.text : item.kind}</div>
    ),
};

describe("TranscriptWindowView", () => {
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

  it("keeps local user content mounted when the same input is committed", () => {
    const window = new TranscriptWindow();
    window.dispatch({ kind: "initial-hydration", hydration: hydration([]) });
    const inputID = crypto.randomUUID();
    const text = "A submitted message";
    const committed = { ...row(1), User: { Text: text, QueueItemIDs: [inputID] } };
    const content = (local: boolean) => (
      <TranscriptWindowView
        boundaryErrorMessage={(error) => error.message}
        estimateSize={() => 60}
        loadingLabel="Loading"
        localMessages={local ? [{ inputID, content: <div data-testid="family-user">{text}</div> }] : []}
        onInput={vi.fn()}
        retryLabel="Retry"
        slots={slots}
        snapshot={window.snapshot}
      />
    );
    const view = render(content(true));
    const preview = screen.getByTestId("family-user");
    window.dispatch({ kind: "committed-row", row: committed });
    view.rerender(content(false));

    expect(screen.getByTestId("family-user")).toBe(preview);
    expect(preview).toBeInTheDocument();
  });

  it.each([
    "confirmation",
    "addition",
    "removal",
    "reordering",
    "replacement",
    "inset",
    "jump",
    "update",
  ] as const)("preserves tail request semantics during submitted input %s", (change) => {
    const scrollGeometry = installVirtualizedScrollGeometry(600);
    const transcript = new TranscriptWindow();
    transcript.dispatch({
      kind: "initial-hydration",
      hydration: hydration(Array.from({ length: 20 }, (_, index) => row(index + 1))),
    });
    const inputIDs = [crypto.randomUUID(), crypto.randomUUID()] as const;
    const nextIDs = {
      confirmation: inputIDs,
      addition: [...inputIDs, crypto.randomUUID()],
      removal: inputIDs.slice(0, 1),
      reordering: [...inputIDs].reverse(),
      replacement: [inputIDs[0], crypto.randomUUID()],
      inset: inputIDs,
      jump: inputIDs,
      update: inputIDs,
    }[change];
    const content = (
      ids: readonly string[],
      overrides: Partial<ComponentProps<typeof TranscriptWindowView>> = {},
    ) => (
      <TranscriptWindowView
        boundaryErrorMessage={(error) => error.message}
        estimateSize={() => 600}
        loadingLabel="Loading"
        localMessages={ids.map((inputID) => ({ inputID, content: <div /> }))}
        onInput={vi.fn()}
        overlay={(following) => <input type="checkbox" checked={following} readOnly />}
        retryLabel="Retry"
        slots={slots}
        snapshot={transcript.snapshot}
        {...overrides}
      />
    );
    const view = render(content(inputIDs));
    try {
      const list = screen.getByRole("list");
      list.scrollTop = list.scrollHeight - list.clientHeight;
      fireEvent.scroll(list);
      list.scrollTop = list.scrollHeight - list.clientHeight - 20;
      fireEvent.scroll(list);
      expect(screen.getByRole("checkbox")).toBeChecked();
      const before = list.scrollTop;
      expect(before).toBeGreaterThan(0);
      expect(before).toBeLessThan(list.scrollHeight - list.clientHeight);
      const scrolling = vi.spyOn(list, "scrollTo");
      view.rerender(
        content(nextIDs, {
          ...(change === "inset" ? { bottomInset: 40 } : {}),
          ...(change === "jump" ? { scrollRequest: Symbol() } : {}),
          ...(change === "update" ? { presentationUpdate: { kind: "replacement" } } : {}),
        }),
      );
      if (change === "confirmation" || change === "jump") {
        expect(list.scrollTop).toBe(before);
        if (change === "jump") {
          expect(scrolling).toHaveBeenCalledWith(expect.objectContaining({ behavior: "smooth" }));
        }
      } else {
        expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
      }
    } finally {
      view.unmount();
      scrollGeometry.restore();
    }
  });

  it("keeps typed family presentations mounted through committed promotion", () => {
    const window = new TranscriptWindow();
    const initial = hydration([]);
    window.dispatch({
      kind: "initial-hydration",
      hydration: {
        ...initial,
        ActiveAssistants: [
          {
            StepID: "step",
            StreamID: "assistant-stream",
            Phase: "commentary",
            Text: "Live assistant",
            SourceState: "streaming",
          },
        ],
        InFlightTools: [
          {
            StepID: "step",
            ToolCallID: "tool-call",
            ToolName: "shell",
            Presentation: null,
          },
        ],
        ActiveReasoningTraces: [
          {
            StepID: "step",
            Identity: reasoningIdentity,
            CompactText: "Live reasoning",
            Text: "Live reasoning detail",
          },
        ],
      },
    });
    const onInput = vi.fn();
    const content = () => (
      <TranscriptWindowView
        boundaryErrorMessage={(error) => error.message}
        estimateSize={() => 60}
        loadingLabel="Loading"
        onInput={onInput}
        retryLabel="Retry"
        slots={slots}
        snapshot={window.snapshot}
      />
    );
    const view = render(content());
    const before = new Map(
      ["assistant", "tool", "reasoning_trace"].map((family) => [
        family,
        screen.getByTestId(`family-${family}`),
      ]),
    );
    for (const row of committedPromotions()) {
      if (row.Tool !== null) {
        expect(
          window.dispatch({
            kind: "committed-row",
            row: {
              ...row,
              Kind: "tool_call",
              Tool: null,
              ToolCall: { StepID: "step", ToolCallID: "tool-call", ToolName: "shell" },
            },
          }).kind,
        ).toBe("accepted");
        view.rerender(content());
        const previous = before.get("tool");
        if (previous === undefined) throw new Error("Missing initial tool presentation.");
        expect(screen.getByTestId("family-tool_call")).toBe(previous);
      }
      expect(window.dispatch({ kind: "committed-row", row }).kind).toBe("accepted");
    }
    view.rerender(
      <TranscriptWindowView
        boundaryErrorMessage={(error) => error.message}
        estimateSize={() => 60}
        loadingLabel="Loading"
        onInput={onInput}
        retryLabel="Retry"
        slots={slots}
        snapshot={window.snapshot}
      />,
    );

    for (const family of ["assistant", "tool", "reasoning_trace"]) {
      const presentation = screen.getByTestId(`family-${family}`);
      const previous = before.get(family);
      if (previous === undefined) throw new Error(`Missing initial presentation for ${family}.`);
      expect(presentation).toBe(previous);
      expect(presentation).toHaveTextContent("committed");
    }
  });

  it("derives exact edge failure presentation from the reducer snapshot and emits its Retry transition", () => {
    const window = new TranscriptWindow();
    window.dispatch({
      kind: "initial-hydration",
      hydration: {
        ...hydration([]),
        TailSegment: { Entries: [], HasMoreAbove: true, OlderCursor: 987 },
      },
    });
    const request = window.dispatch({
      kind: "edge-visit",
      direction: "older",
      older: true,
      newer: false,
    }).effects[0];
    if (request?.kind !== "page-request") throw new Error("Expected older page request.");
    const onInput = vi.fn();
    const boundaryErrorMessage = vi.fn(() => "Localized history failure");

    const view = render(
      <TranscriptWindowView
        boundaryErrorMessage={boundaryErrorMessage}
        estimateSize={() => 60}
        loadingLabel="Loading"
        onInput={onInput}
        retryLabel="Retry"
        slots={slots}
        snapshot={window.snapshot}
      />,
    );
    expect(screen.getByRole("status")).toBeInTheDocument();

    const error = new Error("History read failed");
    window.dispatch({ kind: "page-failure", request: request.request, error });
    view.rerender(
      <TranscriptWindowView
        boundaryErrorMessage={boundaryErrorMessage}
        estimateSize={() => 60}
        loadingLabel="Loading"
        onInput={onInput}
        retryLabel="Retry"
        slots={slots}
        snapshot={window.snapshot}
      />,
    );

    expect(boundaryErrorMessage).toHaveBeenCalledWith(error);
    fireEvent.click(within(screen.getByRole("alert")).getByRole("button"));
    expect(onInput).toHaveBeenCalledTimes(1);
    expect(onInput).toHaveBeenCalledWith({ kind: "retry", direction: "older" });
  });
});
