import { fireEvent, render, screen } from "@testing-library/react";

import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import { VirtualizedInfiniteList } from "./VirtualizedInfiniteList";

it.each(["loading", "error"] as const)(
  "keeps the first message in view when an older page replaces its %s boundary",
  (state) => {
    const geometry = installVirtualizedScrollGeometry(600);
    const rowHeight = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (
      this: HTMLElement,
    ) {
      return this.getAttribute("role") === "list" ? 600 : 40;
    });
    const initial = Array.from({ length: 50 }, (_, index) => String(index + 50));
    const content = (items: readonly string[], loading: boolean) => (
      <VirtualizedInfiniteList
        items={items}
        getItemKey={(item) => item}
        renderItem={(item) => <div>{item}</div>}
        estimateSize={() => 40}
        hasNextPage={false}
        isFetchingNextPage={false}
        loadingLabel="Loading"
        onLoadMore={vi.fn()}
        previousBoundary={
          loading
            ? state === "loading"
              ? { state: "loading", label: "Loading" }
              : { state: "error", message: "Failed", retryLabel: "Retry", onRetry: vi.fn() }
            : undefined
        }
        endAnchoring={{ followEnabled: false, threshold: 80, onEndChange: vi.fn(), scrollRequest: null }}
      />
    );
    const view = render(content(initial, true));
    try {
      const list = screen.getByRole("list");
      list.scrollTop = 0;
      fireEvent.scroll(list);
      view.rerender(
        content(
          Array.from({ length: 100 }, (_, index) => String(index)),
          false,
        ),
      );
      expect(list.scrollTop).toBe(49 * 40);
      expect(screen.getByText("50")).toBeInTheDocument();
    } finally {
      view.unmount();
      rowHeight.mockRestore();
      geometry.restore();
    }
  },
);

it("retains an offscreen row without treating it as a visible pagination edge", () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const rowHeight = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (
    this: HTMLElement,
  ) {
    return this.getAttribute("role") === "list" ? 600 : 40;
  });
  const onLoadMore = vi.fn();
  const onVisible = vi.fn();
  const items = Array.from({ length: 100 }, (_, index) => String(index));
  const view = render(
    <VirtualizedInfiniteList
      items={items}
      getItemKey={(item) => item}
      renderItem={(item) => <div>{item}</div>}
      estimateSize={() => 40}
      hasNextPage
      isFetchingNextPage={false}
      loadingLabel="Loading"
      onLoadMore={onLoadMore}
      pinnedItemKeys={new Set(["99"])}
      visibilityTriggers={[
        {
          itemKey: "99",
          requestGeneration: "initial",
          enabled: true,
          fetching: false,
          onVisible,
        },
      ]}
    />,
  );
  try {
    const retained = screen.getByText("99");
    expect(retained).toBeInTheDocument();
    expect(onLoadMore).not.toHaveBeenCalled();
    expect(onVisible).not.toHaveBeenCalled();
    const list = screen.getByRole("list");
    list.scrollTop = list.scrollHeight - list.clientHeight;
    fireEvent.scroll(list);
    expect(screen.getByText("99")).toBe(retained);
    expect(onLoadMore).toHaveBeenCalledOnce();
    expect(onVisible).toHaveBeenCalledOnce();
  } finally {
    view.unmount();
    rowHeight.mockRestore();
    geometry.restore();
  }
});
