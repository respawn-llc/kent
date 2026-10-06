import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { StrictMode } from "react";

import { TranscriptWindow } from "@/app-facade";
import { hydration, page, row, visit } from "@/test-support/transcript-window";
import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import { StaticMarkdown, StreamingMarkdown } from "@/ui";
import { TranscriptWindowView } from "./TranscriptWindowView";

function streamingView(history: readonly ReturnType<typeof row>[]) {
  const window = new TranscriptWindow();
  window.dispatch({
    kind: "initial-hydration",
    hydration: {
      ...hydration(history),
      ActiveAssistant: { StepID: "step", StreamID: "stream", Phase: "commentary", Text: "Live response" },
    },
  });
  const onInput = vi.fn();
  const content = () => (
    <TranscriptWindowView
      snapshot={window.snapshot}
      estimateSize={() => 40}
      loadingLabel="Loading"
      retryLabel="Retry"
      boundaryErrorMessage={(error) => error.message}
      onInput={onInput}
      slots={{
        user: (item) => <div>{item.value.Text}</div>,
        assistant: (item) =>
          item.state === "live" ? (
            <div data-testid="live-response">
              <StreamingMarkdown value={item.value.Text} />
            </div>
          ) : (
            <StaticMarkdown value={item.value.Text} />
          ),
        tool: () => null,
        notice: () => null,
        reasoning: () => null,
        thinkingStatus: () => null,
      }}
    />
  );
  const view = render(content());
  return {
    window,
    onInput,
    view,
    renderSnapshot: () => {
      view.rerender(content());
    },
  };
}

it("keeps a streaming response and its measured height intact offscreen until it commits", () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const rowHeight = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (
    this: HTMLElement,
  ) {
    return this.getAttribute("role") === "list" ? 600 : 40;
  });
  try {
    const { window, view, renderSnapshot } = streamingView(
      Array.from({ length: 100 }, (_, index) => row(index + 1)),
    );
    const list = screen.getByRole("list");
    list.scrollTop = list.scrollHeight - list.clientHeight;
    fireEvent.scroll(list);
    const bubble = screen.getByTestId("live-response");
    const originalCharacter = within(bubble).getByText("L", { exact: true });
    const responseRow = screen
      .getAllByRole("listitem")
      .find((item) => within(item).queryByTestId("live-response") !== null);
    if (responseRow === undefined) throw new Error("Expected a mounted streaming response.");
    act(() => {
      geometry.resize(responseRow, 300);
    });
    const height = list.scrollHeight;
    list.scrollTop = 0;
    fireEvent.scroll(list);
    expect(screen.getByTestId("live-response")).toBe(bubble);
    expect(list.scrollHeight).toBe(height);
    window.dispatch({
      kind: "live-fact",
      fact: {
        kind: "assistant_delta",
        payload: { StepID: "step", StreamID: "stream", Phase: "commentary", Delta: "!" },
      },
    });
    renderSnapshot();
    expect(bubble).toHaveTextContent("Live response!");
    expect(within(bubble).getByText("L", { exact: true })).toBe(originalCharacter);
    list.scrollTop = list.scrollHeight - list.clientHeight;
    fireEvent.scroll(list);
    expect(screen.getByTestId("live-response")).toBe(bubble);
    expect(list.scrollHeight).toBe(height);
    list.scrollTop = 0;
    fireEvent.scroll(list);
    window.dispatch({
      kind: "committed-row",
      row: {
        ...row(101),
        Kind: "assistant",
        User: null,
        Assistant: {
          StepID: "step",
          StreamID: "stream",
          Phase: "commentary",
          Text: "Live response!",
          CondensedText: null,
          committed_at_unix_ms: null,
        },
      },
    });
    renderSnapshot();
    expect(screen.queryByTestId("live-response")).not.toBeInTheDocument();
    expect(bubble.isConnected).toBe(false);
    view.unmount();
  } finally {
    rowHeight.mockRestore();
    geometry.restore();
  }
});

it("retains the live renderer across historical page eviction without showing it in older history", () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const { window, onInput, view, renderSnapshot } = streamingView(
    Array.from({ length: 100 }, (_, index) => row(index + 300)),
  );
  try {
    const bubble = screen.getByTestId("live-response");
    window.dispatch({
      kind: "page-success",
      request: visit(window, "older"),
      page: page(
        Array.from({ length: 100 }, (_, index) => row(index + 100)),
        200,
        300,
      ),
    });
    renderSnapshot();
    window.dispatch({
      kind: "page-success",
      request: visit(window, "older"),
      page: page(
        Array.from({ length: 99 }, (_, index) => row(index + 1)),
        null,
        200,
      ),
    });
    onInput.mockClear();
    renderSnapshot();
    const list = screen.getByRole("list");
    list.scrollTop = 0;
    fireEvent.scroll(list);
    expect(window.snapshot.showsLive).toBe(false);
    expect(screen.getByTestId("live-response")).toBe(bubble);
    expect(bubble).not.toBeVisible();
    expect(onInput).not.toHaveBeenCalledWith(expect.objectContaining({ direction: "newer" }));
    window.dispatch({
      kind: "live-fact",
      fact: {
        kind: "assistant_delta",
        payload: { StepID: "step", StreamID: "stream", Phase: "commentary", Delta: "!" },
      },
    });
    renderSnapshot();
    expect(bubble).toHaveTextContent("Live response!");
    window.dispatch({ kind: "replace-window", page: page([row(399)], 300) });
    renderSnapshot();
    expect(screen.getByTestId("live-response")).toBe(bubble);
    expect(bubble).toBeVisible();
    window.dispatch({ kind: "observation-loss" });
    renderSnapshot();
    expect(screen.queryByTestId("live-response")).not.toBeInTheDocument();
  } finally {
    view.unmount();
    geometry.restore();
  }
});

it("keeps viewport compensation and measured content geometry in the same resize frame", () => {
  const geometry = installVirtualizedScrollGeometry(600);
  try {
    const window = new TranscriptWindow();
    window.dispatch({
      kind: "initial-hydration",
      hydration: hydration(Array.from({ length: 100 }, (_, index) => row(index + 1))),
    });
    const view = render(
      <TranscriptWindowView
        snapshot={window.snapshot}
        estimateSize={() => 40}
        loadingLabel="Loading"
        retryLabel="Retry"
        boundaryErrorMessage={(error) => error.message}
        onInput={vi.fn()}
        overlay={(following) => <output>{following ? "following" : "reading"}</output>}
        slots={{
          user: (item) => <div>{item.value.Text}</div>,
          assistant: () => null,
          tool: () => null,
          notice: () => null,
          reasoning: () => null,
          thinkingStatus: () => null,
        }}
      />,
    );
    const list = screen.getByRole("list");
    list.scrollTop = 500;
    fireEvent.scroll(list);
    const preceding = screen.getAllByRole("listitem")[0];
    if (preceding === undefined) throw new Error("Expected a measured preceding row.");
    const before = list.scrollHeight;
    act(() => {
      geometry.resize(preceding, 140);
      // ResizeObserver delivery precedes paint. Scroll compensation and the
      // rendered content extent must agree before delivery returns.
      expect(list.scrollHeight).toBe(before + 100);
    });
    list.scrollTop = list.scrollHeight - list.clientHeight;
    fireEvent.scroll(list);
    const tail = screen.getAllByRole("listitem").at(-1);
    if (tail === undefined) throw new Error("Expected a measured tail row.");
    expect(screen.getByText("following")).toBeInTheDocument();
    act(() => {
      geometry.resize(tail, 140);
    });
    expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
    view.unmount();
  } finally {
    geometry.restore();
  }
});

it("opens at the bottom and keeps the latest content clear as composer padding grows", async () => {
  const geometry = installVirtualizedScrollGeometry(600);
  try {
    const window = new TranscriptWindow();
    window.dispatch({
      kind: "initial-hydration",
      hydration: hydration(Array.from({ length: 100 }, (_, index) => row(index + 1))),
    });
    const content = (bottomInset: number) => (
      <StrictMode>
        <TranscriptWindowView
          bottomInset={bottomInset}
          snapshot={window.snapshot}
          estimateSize={() => 40}
          loadingLabel="Loading"
          retryLabel="Retry"
          boundaryErrorMessage={(error) => error.message}
          onInput={vi.fn()}
          slots={{
            user: (item) => <div>{item.value.Text}</div>,
            assistant: () => null,
            tool: () => null,
            notice: () => null,
            reasoning: () => null,
            thinkingStatus: () => null,
          }}
        />
      </StrictMode>
    );
    const view = render(content(0));
    const list = screen.getByRole("list");
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });
    expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
    const originalHeight = list.scrollHeight;
    view.rerender(content(180));
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });
    expect(list.scrollHeight).toBe(originalHeight + 180);
    expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
    view.rerender(content(80));
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });
    expect(list.scrollHeight).toBe(originalHeight + 80);
    expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
    list.scrollTop = 500;
    fireEvent.scroll(list);
    view.rerender(content(240));
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });
    expect(list.scrollTop).toBe(500);
    view.unmount();
  } finally {
    geometry.restore();
  }
});
