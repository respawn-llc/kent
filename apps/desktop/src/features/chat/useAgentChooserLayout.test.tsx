import { act, renderHook, waitFor } from "@testing-library/react";

import { useAgentChooserLayout } from "./useAgentChooserLayout";

it("scrolls the selected Agent into view when the chooser opens", async () => {
  const { result, rerender } = renderHook(({ choosing }) => useAgentChooserLayout(choosing), {
    initialProps: { choosing: false },
  });
  const overview = document.createElement("div");
  const scroll = document.createElement("div");
  const selected = document.createElement("div");
  overview.getBoundingClientRect = () => DOMRect.fromRect({ height: 240 });
  Object.defineProperty(selected, "offsetTop", { value: 500 });
  Object.defineProperty(selected, "offsetHeight", { value: 40 });
  Object.defineProperty(scroll, "clientHeight", { value: 480 });
  result.current.overviewRef.current = overview;
  result.current.scrollRef.current = scroll;
  result.current.selectedRef.current = selected;

  act(() => {
    result.current.capture();
  });
  rerender({ choosing: true });

  await waitFor(() => {
    expect(scroll.scrollTop).toBe(60);
  });
});
