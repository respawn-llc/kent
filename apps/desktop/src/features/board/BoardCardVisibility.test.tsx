import { RegistryProvider, useAtomSet, useAtomValue } from "@effect/atom-react";
import { act, render, screen, waitFor } from "@testing-library/react";
import { useCallback } from "react";
import {
  BoardCardVisibilityContext,
  createBoardCardVisibility,
  useBoardCardInstanceVisibility,
} from "./BoardCardVisibilityRegistry";

it("releases card observations and disconnects visibility observation when its rail leaves", async () => {
  const observe = vi.fn();
  const unobserve = vi.fn();
  const disconnect = vi.fn();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe = observe;
      unobserve = unobserve;
      disconnect = disconnect;
    },
  );
  const model = createBoardCardVisibility();
  function Card() {
    const register = useAtomSet(model.register, { mode: "value" });
    const visible = useBoardCardInstanceVisibility({ columnID: "column", taskID: "task" });
    const ref = useCallback(
      (element: HTMLDivElement | null) => {
        register({ instance: { columnID: "column", taskID: "task" }, element });
      },
      [register],
    );
    return (
      <div ref={ref} data-testid="card">
        {String(visible)}
      </div>
    );
  }
  function Rail({ card }: { card: boolean }) {
    useAtomValue(model.resources);
    return (
      <BoardCardVisibilityContext.Provider value={model}>
        {card ? <Card /> : null}
      </BoardCardVisibilityContext.Provider>
    );
  }
  try {
    const view = render(
      <RegistryProvider>
        <Rail card />
      </RegistryProvider>,
    );
    const element = screen.getByTestId("card");
    expect(observe).toHaveBeenCalledWith(element);
    view.rerender(
      <RegistryProvider>
        <Rail card={false} />
      </RegistryProvider>,
    );
    await waitFor(() => {
      expect(unobserve).toHaveBeenCalledWith(element);
    });
    expect(disconnect).not.toHaveBeenCalled();
    await act(async () => {
      view.unmount();
    });
    await waitFor(() => {
      expect(disconnect).toHaveBeenCalledOnce();
    });
  } finally {
    vi.unstubAllGlobals();
  }
});
