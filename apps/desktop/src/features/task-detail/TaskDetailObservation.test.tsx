import { RegistryProvider } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { queryKeys } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTaskDetailTestServices, taskDetailResponse } from "@/test-support/task-detail";
import { projectEventsFixture } from "@/test-support/project-events";
import {
  createTaskDetailViewModel,
  useTaskDetailReads,
  useTaskDetailObservation,
} from "./TaskDetailViewModel";

it("releases Task observation and disables reads while inactive, and releases observation on disposal", async () => {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const events = projectEventsFixture(services.transport);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const model = createTaskDetailViewModel({
    services,
    client,
    taskID: "task-1",
    enabled: true,
    t: appI18n.t,
    push: vi.fn(),
  });
  const view = renderHook(
    ({ enabled }) => ({
      reads: useTaskDetailReads(model, enabled),
      observation: useTaskDetailObservation(model),
    }),
    {
      initialProps: { enabled: true },
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  await waitFor(() => {
    expect(events.activeCount).toBe(1);
  });
  view.rerender({ enabled: false });
  await waitFor(() => {
    expect(events.activeCount).toBe(0);
  });
  const readCount = services.transport.descriptorCalls.length;
  await act(async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: queryKeys.task("task-1") }),
      client.invalidateQueries({ queryKey: queryKeys.taskAttention("task-1") }),
      client.invalidateQueries({ queryKey: queryKeys.activity("task-1") }),
      client.invalidateQueries({ queryKey: queryKeys.comments("task-1") }),
    ]);
  });
  expect(services.transport.descriptorCalls).toHaveLength(readCount);
  view.rerender({ enabled: true });
  await waitFor(() => {
    expect(events.activeCount).toBe(1);
  });
  view.unmount();
  await waitFor(() => {
    expect(events.activeCount).toBe(0);
  });
});
