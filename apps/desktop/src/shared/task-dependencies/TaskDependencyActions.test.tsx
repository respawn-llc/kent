import { act, renderHook } from "@testing-library/react";
import type { TaskDependencyMutationResponse } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { useTaskDependencyActions } from "./dependencyActions";

it.each(["add", "remove"] as const)(
  "admits %s once per pair while another relationship runs",
  async (kind) => {
    const services = createTestServices([]);
    const response = deferred<TaskDependencyMutationResponse>();
    const request = vi
      .spyOn(services.api, kind === "add" ? "addTaskDependency" : "removeTaskDependency")
      .mockReturnValue(response.promise);
    const view = renderHook(
      () => ({
        first: useTaskDependencyActions("project-1", "task-1", {
          blockerTaskID: "task-2",
          blockedTaskID: "task-1",
        }),
        same: useTaskDependencyActions("project-1", "task-1", {
          blockerTaskID: "task-2",
          blockedTaskID: "task-1",
        }),
        second: useTaskDependencyActions("project-1", "task-1", {
          blockerTaskID: "task-3",
          blockedTaskID: "task-1",
        }),
      }),
      {
        wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders>,
      },
    );
    const first = { blockerTaskID: "task-2", blockedTaskID: "task-1" };
    await act(async () => {
      view.result.current.first.submit({ kind });
      view.result.current.same.submit({ kind });
      view.result.current.second.submit({ kind });
    });
    expect(request).toHaveBeenCalledTimes(2);
    await act(async () => {
      response.resolve({
        ...first,
        blockerShortID: "KNT-2",
        blockedShortID: "KNT-1",
        outcome: kind === "add" ? "added" : "removed",
      });
    });
  },
);
