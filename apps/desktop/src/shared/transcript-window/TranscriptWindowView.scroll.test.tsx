import { act, fireEvent, render, screen } from "@testing-library/react";
import { StrictMode } from "react";

import { TranscriptWindow } from "@/app-facade";
import { hydration, row } from "@/test-support/transcript-window";
import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import { TranscriptWindowView } from "./TranscriptWindowView";

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
