import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

import { appI18n } from "@/i18n";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { processObservationFixture, processObservationFixtureRoute } from "@/test-support/api";
import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import { ProcessesSidebar } from "./ProcessesSidebar";

it("keeps the viewport in place when Stop moves the leading process below running processes", async () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const rowHeight = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (
    this: HTMLElement,
  ) {
    return this.getAttribute("role") === "list" ? 600 : 80;
  });
  const services = createTestServices([processObservationFixtureRoute]);
  const kill = vi.spyOn(services.api, "killProcess").mockResolvedValue(undefined);
  const processes = Array.from({ length: 100 }, (_, index) => ({
    id: index.toString(),
    state: "running",
    running: true,
  }));
  const publish = processObservationFixture(services.transport).publishRows;
  const view = render(
    <TestAppProviders services={services}>
      <ProcessesSidebar target={{ projectID: "project-1", sessionID: "session-1" }} />
    </TestAppProviders>,
  );
  try {
    await waitFor(() => {
      expect(services.transport.descriptorSubscriptions).toHaveLength(1);
    });
    await act(async () => {
      publish(processes);
    });
    const list = screen.getByRole("list");
    list.scrollTop = 160;
    fireEvent.scroll(list);
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("processes.terminate", { id: "2" }) }));
    await waitFor(() => {
      expect(kill).toHaveBeenCalledWith("2");
    });
    const stopped = processes[2];
    if (stopped === undefined) throw new Error("Expected process to stop.");
    await act(async () => {
      publish([
        ...processes.filter((process) => process.id !== stopped.id),
        {
          ...stopped,
          state: "killed",
          running: false,
        },
      ]);
    });
    expect(list.scrollTop).toBe(160);
  } finally {
    view.unmount();
    rowHeight.mockRestore();
    geometry.restore();
  }
});
