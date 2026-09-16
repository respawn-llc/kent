import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactElement } from "react";

import { TranscriptWindow, type TranscriptCommittedItem, type TranscriptRenderSlots } from "@/app-facade";
import { hydration } from "@/test-support/transcript-window";
import { installResizeObserverGeometry } from "@/test-support/resize-observer";

import { TranscriptWindowView } from "./index";

let geometry: ReturnType<typeof installResizeObserverGeometry>;

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
      Tool: {
        StepID: "step",
        ToolCallID: "tool-call",
        ToolName: "shell",
        Text: "Committed tool",
        IsError: false,
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
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(600);
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
  });

  afterEach(() => {
    geometry.restore();
    vi.restoreAllMocks();
  });

  it("keeps typed family presentations mounted through committed promotion", () => {
    const window = new TranscriptWindow();
    const initial = hydration([]);
    window.dispatch({
      kind: "initial-hydration",
      hydration: {
        ...initial,
        ActiveAssistant: {
          StepID: "step",
          StreamID: "assistant-stream",
          Phase: "commentary",
          Text: "Live assistant",
        },
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
    const view = render(
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
    const before = new Map(
      ["assistant", "tool", "reasoning_trace"].map((family) => [
        family,
        screen.getByTestId(`family-${family}`),
      ]),
    );
    for (const row of committedPromotions()) {
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
      expect(presentation).toBe(before.get(family));
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
    expect(onInput).toHaveBeenCalledOnce();
    expect(onInput).toHaveBeenCalledWith({ kind: "retry", direction: "older" });
  });
});
