import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import type { DesktopProcess } from "@/api";
import { useProcessesData } from "@/features/processes";
import { createAppQueryClient } from "./queryClient";

const fixture = vi.hoisted(() => ({
  focused: true,
  api: {
    listProcesses: vi.fn<() => Promise<readonly DesktopProcess[]>>(),
    killProcess: vi.fn<() => Promise<void>>(),
  },
}));
vi.mock("@/app-facade", async (original) => ({
  ...(await original()),
  useAppServices: () => ({ api: fixture.api }),
  useWindowFocus: () => fixture.focused,
}));

afterEach(() => {
  vi.useRealTimers();
  vi.resetAllMocks();
  fixture.focused = true;
});

it("keeps Processes polling, focus refresh and failed-Kill waiting under the production request policy", async () => {
  vi.useFakeTimers();
  const client = createAppQueryClient();
  fixture.api.listProcesses.mockRejectedValueOnce(new Error("unavailable")).mockResolvedValue([]);
  const view = renderHook(() => useProcessesData("project-1"), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  await act(async () => vi.advanceTimersByTimeAsync(1));
  expect(fixture.api.listProcesses).toHaveBeenCalledTimes(1);
  expect(view.result.current.isError).toBe(true);
  await act(async () => vi.advanceTimersByTimeAsync(1_500));
  expect(fixture.api.listProcesses).toHaveBeenCalledTimes(2);
  expect(view.result.current.processes).toEqual([]);

  fixture.focused = false;
  view.rerender();
  await act(async () => vi.advanceTimersByTimeAsync(3_000));
  expect(fixture.api.listProcesses).toHaveBeenCalledTimes(2);
  fixture.focused = true;
  view.rerender();
  await act(async () => vi.advanceTimersByTimeAsync(1));
  expect(fixture.api.listProcesses).toHaveBeenCalledTimes(3);

  const failure = new Error("delivery lost");
  fixture.api.killProcess.mockRejectedValueOnce(failure);
  fixture.api.listProcesses.mockRejectedValueOnce(new Error("refresh unavailable"));
  await act(async () => {
    await expect(view.result.current.terminate("process-1")).rejects.toBe(failure);
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(fixture.api.listProcesses).toHaveBeenCalledTimes(4);
  expect(view.result.current.pendingTerminationIDs.has("process-1")).toBe(true);
  await act(async () => vi.advanceTimersByTimeAsync(1_500));
  expect(fixture.api.listProcesses).toHaveBeenCalledTimes(5);
  expect(view.result.current.pendingTerminationIDs.size).toBe(0);
  expect(fixture.api.killProcess).toHaveBeenCalledTimes(1);
  view.unmount();
  client.clear();
});
