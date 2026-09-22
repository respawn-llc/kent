import { act, fireEvent, render, screen } from "@testing-library/react";
import { ChatProcessesChip } from "./ChatProcessesChip";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { SidebarRootContext, SidebarRootOwner } from "@/app-facade";
import { createTestSidebarController } from "@/test-support/sidebar";
import { target } from "@/test-support/chat-runtime";

it("reveals only after sustained active processes and hides at zero without polling", async () => {
  vi.useFakeTimers();
  try {
    const services = createTestServices([]);
    const list = vi.spyOn(services.api, "listProcesses");
    const open = vi.fn();
    const sidebar = createTestSidebarController(open);
    const selected = { kind: "session" as const, ...target };
    const renderChip = (count: number) => (
      <TestAppProviders services={services}>
        <SidebarRootContext.Provider value={sidebar}>
          <SidebarRootOwner>
            <ChatProcessesChip target={selected} count={count} />
          </SidebarRootOwner>
        </SidebarRootContext.Provider>
      </TestAppProviders>
    );
    const view = render(renderChip(1));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    await act(async () => vi.advanceTimersByTimeAsync(2999));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    view.rerender(renderChip(3));
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(screen.getByRole("button")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button"));
    expect(open).toHaveBeenCalledExactlyOnceWith({ kind: "processes", ...target });
    view.rerender(renderChip(0));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    view.rerender(renderChip(1));
    await act(async () => vi.advanceTimersByTimeAsync(1500));
    view.rerender(renderChip(0));
    view.rerender(renderChip(1));
    await act(async () => vi.advanceTimersByTimeAsync(1500));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(list).not.toHaveBeenCalled();
    view.unmount();
  } finally {
    vi.useRealTimers();
  }
});
