import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "bun:test";
import { StrictMode } from "react";

import { ChatTranscriptHost, TranscriptWindow } from "@/app-facade";
import { hydration, page, row, visit } from "@/test-support/transcript-window";
import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import { StaticMarkdown, StreamingMarkdown } from "@/ui";
import { TranscriptWindowView } from "./TranscriptWindowView";

it.each(["older", "newer"] as const)(
  "continues through equal-sized %s pages without another edge gesture",
  (direction) => {
    const geometry = installVirtualizedScrollGeometry(600);
    const window = new TranscriptWindow();
    const rows = (start: number) => [row(start), row(start + 1)];
    window.dispatch({ kind: "initial-hydration", hydration: hydration(rows(301)) });
    window.dispatch({
      kind: "page-success",
      request: visit(window, "older"),
      page: page(rows(251), 250, 300),
    });
    window.dispatch({
      kind: "page-success",
      request: visit(window, "older"),
      page: page(rows(201), 200, 250),
    });
    if (direction === "newer") {
      window.dispatch({
        kind: "page-success",
        request: visit(window, "older"),
        page: page(rows(101), null, 200),
      });
    }
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
          assistant: () => null,
          tool: () => null,
          notice: () => null,
          reasoning: () => null,
          thinkingStatus: () => null,
        }}
      />
    );
    const view = render(content());
    try {
      expect(onInput).toHaveBeenCalledWith(expect.objectContaining({ kind: "edge-visit", direction }));
      onInput.mockClear();
      const request = visit(window, direction);
      // A fast read can finish before React commits the loading snapshot.
      window.dispatch({
        kind: "page-success",
        request,
        page: direction === "older" ? page(rows(101), 100, 200) : page(rows(251), 250, 300),
      });
      view.rerender(content());
      expect(onInput).toHaveBeenCalledWith(expect.objectContaining({ kind: "edge-visit", direction }));
    } finally {
      view.unmount();
      geometry.restore();
    }
  },
);

it("opens and remounts a compacted live transcript at the bottom despite underestimated rows", async () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const originalOffsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight");
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return this.getAttribute("role") === "list" ? 600 : 400;
    },
  });
  const host = new ChatTranscriptHost(
    {
      getTranscriptPage: vi.fn().mockResolvedValue(
        page(
          Array.from({ length: 100 }, (_, index) => row(index + 301)),
          300,
        ),
      ),
    },
    { projectID: "project", sessionID: "123e4567-e89b-42d3-a456-426614174000" },
    { onContractFailure: vi.fn(), onOpeningFailure: vi.fn(), onScratchRehydration: vi.fn() },
  );
  await act(async () => {
    // The independent newest-page read can complete before subscription hydration.
    host.open();
  });
  const content = () => (
    <TranscriptWindowView
      snapshot={host.snapshot}
      presentationUpdate={host.presentationUpdate}
      estimateSize={() => 40}
      loadingLabel="Loading"
      retryLabel="Retry"
      boundaryErrorMessage={(error) => error.message}
      onInput={vi.fn()}
      slots={{
        user: (item) => <div>{item.value.Text}</div>,
        assistant: () => <div>Live response</div>,
        tool: () => null,
        notice: () => null,
        reasoning: () => null,
        thinkingStatus: () => null,
      }}
    />
  );
  let view = render(content());
  try {
    for (let mount = 0; mount < 2; mount++) {
      if (mount !== 0) view = render(content());
      host.hydration("initial", {
        ...hydration(
          Array.from({ length: 300 }, (_, index) => row(index + 101)),
          3,
        ),
        ActiveAssistants: [
          { StepID: "step", StreamID: "stream", Phase: "commentary", Text: "Live response", SourceState: "streaming" },
        ],
      });
      view.rerender(content());
      for (let frame = 0; frame < 5; frame++) {
        await act(async () => {
          await new Promise((resolve) => requestAnimationFrame(resolve));
        });
      }
      const list = screen.getByRole("list");
      expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
      list.scrollTop = 500;
      fireEvent.scroll(list);
      view.unmount();
    }
  } finally {
    view.unmount();
    host.dispose();
    if (originalOffsetHeight === undefined) Reflect.deleteProperty(HTMLElement.prototype, "offsetHeight");
    else Object.defineProperty(HTMLElement.prototype, "offsetHeight", originalOffsetHeight);
    geometry.restore();
  }
});

it.each(["following", "reading", "historical-end"])(
  "preserves %s through live compaction hydration",
  async (position) => {
    const geometry = installVirtualizedScrollGeometry(600);
    const host = new ChatTranscriptHost(
      {
        getTranscriptPage: vi
          .fn()
          .mockResolvedValueOnce(
            page(
              Array.from({ length: 100 }, (_, index) => row(index + 101)),
              100,
              300,
            ),
          )
          .mockResolvedValueOnce(
            page(
              Array.from({ length: 100 }, (_, index) => row(index + 1)),
              null,
              100,
            ),
          ),
      },
      { projectID: "project", sessionID: "123e4567-e89b-42d3-a456-426614174000" },
      { onContractFailure: vi.fn(), onOpeningFailure: vi.fn(), onScratchRehydration: vi.fn() },
    );
    host.hydration("initial", hydration(Array.from({ length: 100 }, (_, index) => row(index + 301))));
    const content = () => (
      <TranscriptWindowView
        snapshot={host.snapshot}
        presentationUpdate={host.presentationUpdate}
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
    );
    const view = render(content());
    try {
      await act(async () => {
        await new Promise((resolve) => requestAnimationFrame(resolve));
      });
      const list = screen.getByRole("list");
      if (position === "historical-end") {
        fireEvent.wheel(list, { deltaY: -100 });
        for (let index = 0; index < 2; index++) {
          await act(async () => {
            host.dispatch({ kind: "edge-visit", direction: "older", older: true, newer: false });
          });
          view.rerender(content());
        }
        expect(host.snapshot.newer.cursor).not.toBeNull();
        list.scrollTop = list.scrollHeight - list.clientHeight;
        fireEvent.scroll(list);
      } else if (position === "reading") {
        list.scrollTop = 500;
        fireEvent.scroll(list);
      }
      const before = list.scrollTop;
      const start = position === "historical-end" ? 101 : 301;
      host.hydration(
        "scratch",
        hydration(
          Array.from({ length: 120 }, (_, index) => row(index + start)),
          1,
        ),
      );
      view.rerender(content());
      await act(async () => {
        await new Promise((resolve) => requestAnimationFrame(resolve));
      });
      if (position === "historical-end") {
        expect(list.scrollTop).toBeLessThan(list.scrollHeight - list.clientHeight);
      } else {
        expect(list.scrollTop).toBe(
          position === "following" ? list.scrollHeight - list.clientHeight : before,
        );
      }
    } finally {
      view.unmount();
      host.dispose();
      geometry.restore();
    }
  },
);

function streamingView(history: readonly ReturnType<typeof row>[]) {
  const window = new TranscriptWindow();
  window.dispatch({
    kind: "initial-hydration",
    hydration: {
      ...hydration(history),
      ActiveAssistants: [{ StepID: "step", StreamID: "stream", Phase: "commentary", Text: "Live response", SourceState: "streaming" }],
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

it.each([
  { input: "wheel", emit: fireEvent.wheel },
  { input: "scrollbar pointer", emit: fireEvent.pointerDown },
  { input: "touch", emit: fireEvent.touchStart },
])("lets $input take over an opening scroll", async ({ emit }) => {
  const geometry = installVirtualizedScrollGeometry(600);
  const { view } = streamingView(Array.from({ length: 100 }, (_, index) => row(index + 1)));
  try {
    const list = screen.getByRole("list");
    emit(list);
    list.scrollTop = 500;
    fireEvent.scroll(list);
    for (let frame = 0; frame < 4; frame++) {
      await act(async () => {
        await new Promise((resolve) => requestAnimationFrame(resolve));
      });
    }
    expect(list.scrollTop).toBe(500);
  } finally {
    view.unmount();
    geometry.restore();
  }
});

it("keeps a streaming response and its measured height intact offscreen until it commits", () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const originalOffsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight");
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return this.getAttribute("role") === "list" ? 600 : 40;
    },
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
    if (originalOffsetHeight === undefined) Reflect.deleteProperty(HTMLElement.prototype, "offsetHeight");
    else Object.defineProperty(HTMLElement.prototype, "offsetHeight", originalOffsetHeight);
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
    const offsetBefore = list.scrollTop;
    act(() => {
      geometry.resize(preceding, 140);
      // ResizeObserver delivery precedes paint. Scroll compensation and the
      // rendered content extent must agree before delivery returns.
      expect(list.scrollHeight).toBe(before + 100);
      expect(list.scrollTop).toBe(offsetBefore + 100);
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

it("keeps the tail aligned during viewport resizing without moving a reader back to the tail", async () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const { view } = streamingView(Array.from({ length: 100 }, (_, index) => row(index + 1)));
  try {
    await act(async () => {
      await new Promise(requestAnimationFrame);
    });
    const list = screen.getByRole("list");
    list.scrollTop = list.scrollHeight - list.clientHeight;
    fireEvent.scroll(list);
    for (const height of [400, 500, 300]) {
      act(() => {
        geometry.resize(list, height);
      });
      expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
      await act(async () => {
        await new Promise(requestAnimationFrame);
      });
    }
    fireEvent.wheel(list);
    list.scrollTop = 500;
    fireEvent.scroll(list);
    act(() => {
      geometry.resize(list, 450);
    });
    expect(list.scrollTop).toBe(500);
  } finally {
    view.unmount();
    geometry.restore();
  }
});

it("compensates above-viewport remeasurement while scrolling upward", async () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const { view } = streamingView(Array.from({ length: 100 }, (_, index) => row(index + 1)));
  try {
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });
    const list = screen.getByRole("list");
    list.scrollTop = 2010;
    fireEvent.scroll(list);
    const preceding = screen.getAllByRole("listitem")[0];
    if (preceding === undefined) throw new Error("Expected an overscan message.");
    act(() => {
      geometry.resize(preceding, 60);
    });
    list.scrollTop = 1970;
    fireEvent.scroll(list);
    const before = list.scrollTop;
    act(() => {
      geometry.resize(preceding, 200);
    });
    expect(list.scrollTop).toBe(before + 140);
  } finally {
    view.unmount();
    geometry.restore();
  }
});

it.each([
  { height: 20, offset: 2010, expected: 1990 },
  { height: 200, offset: 2010, expected: 2170 },
  { height: 20, offset: 2030, expected: 2010 },
  { height: 200, offset: 2050, expected: 2210 },
])("anchors first measurement of $height px at offset $offset", async ({ height, offset, expected }) => {
  const geometry = installVirtualizedScrollGeometry(600);
  const { view } = streamingView(Array.from({ length: 100 }, (_, index) => row(index + 1)));
  try {
    await act(async () => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });
    const list = screen.getByRole("list");
    list.scrollTop = offset;
    fireEvent.scroll(list);
    const text = row(51).User?.Text;
    if (text === undefined) throw new Error("Expected a user message fixture.");
    const message = screen.getAllByRole("listitem").find((item) => within(item).queryByText(text) !== null);
    if (message === undefined) throw new Error("Expected a visible message row.");
    act(() => {
      geometry.resize(message, height);
    });
    expect(list.scrollTop).toBe(expected);
  } finally {
    view.unmount();
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
