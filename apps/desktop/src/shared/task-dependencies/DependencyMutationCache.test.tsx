import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { vi } from "vitest";

import type { TaskDetail } from "@/api";
import { queryKeys } from "@/app-facade";
import { TestAppProviders } from "@/test-support/app-services";
import {
  createTaskDetailTestServices,
  taskDetailResponse,
  taskBlockedByFixture,
  taskDependencyAddedRoute,
  taskDependencyRemovedRoute,
  taskDependencyRemovalFailure,
} from "@/test-support/task-detail";
import { useTaskDependencyActions } from "./dependencyActions";

describe("Task dependency removal", () => {
  it("adds an existing Task and invalidates both Tasks plus project views", async () => {
    const services = createTaskDetailTestServices(taskWithBlocker(), {
      routes: [
        taskDependencyAddedRoute({
          blockerTaskID: "task-3",
          blockerShortID: "T-3",
          blockedTaskID: "task-1",
          blockedShortID: "T-1",
        }),
      ],
    });
    const queryClient = new QueryClient();
    const blockerTaskKey = queryKeys.task("task-3");
    const blockedTaskKey = queryKeys.task("task-1");
    const taskListKey = queryKeys.projectTaskListsRoot("project-1");
    queryClient.setQueryData(blockerTaskKey, {});
    queryClient.setQueryData(blockedTaskKey, {});
    queryClient.setQueryData(taskListKey, {});
    const { result } = renderHook(
      () =>
        useTaskDependencyActions("project-1", "task-1", { blockerTaskID: "task-3", blockedTaskID: "task-1" }),
      {
        wrapper: testWrapper(services, queryClient),
      },
    );

    await act(async () => {
      result.current.submit({ kind: "add" });
    });

    expect(queryClient.getQueryState(blockerTaskKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(blockedTaskKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(taskListKey)?.isInvalidated).toBe(true);
  });

  it("patches the open Task immediately and invalidates both Tasks plus project views", async () => {
    const services = createTaskDetailTestServices(taskWithBlocker(), {
      routes: [
        taskDependencyRemovedRoute({
          blockerTaskID: "task-2",
          blockerShortID: "T-2",
          blockedTaskID: "task-1",
          blockedShortID: "T-1",
        }),
      ],
    });
    const detail = await services.api.getTask("task-1");
    const queryClient = new QueryClient();
    const relatedTaskKey = queryKeys.task("task-2");
    const boardKey = queryKeys.board("project-1", "workflow-1", { kind: "none" });
    const cardsKey = queryKeys.boardNodeCards({
      filter: { kind: "none" },
      nodeID: "node-1",
      projectID: "project-1",
      workflowID: "workflow-1",
    });
    const taskListKey = queryKeys.projectTaskListsRoot("project-1");
    queryClient.setQueryData(queryKeys.task("task-1"), detail);
    queryClient.setQueryData(relatedTaskKey, {});
    queryClient.setQueryData(boardKey, {});
    queryClient.setQueryData(cardsKey, {});
    queryClient.setQueryData(taskListKey, {});
    const { result } = renderHook(
      () =>
        useTaskDependencyActions("project-1", "task-1", { blockerTaskID: "task-2", blockedTaskID: "task-1" }),
      {
        wrapper: testWrapper(services, queryClient),
      },
    );

    await act(async () => {
      result.current.submit({ kind: "remove" });
    });

    expect(queryClient.getQueryData<TaskDetail>(queryKeys.task("task-1"))?.dependencies.blockerCount).toBe(0);
    expect(queryClient.getQueryState(relatedTaskKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(boardKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(cardsKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(taskListKey)?.isInvalidated).toBe(true);
  });

  it("restores the dependency after failure without a repair read", async () => {
    const services = createTaskDetailTestServices(taskWithBlocker(), {
      routes: [taskDependencyRemovalFailure(new Error("offline"))],
    });
    const detail = await services.api.getTask("task-1");
    const queryClient = new QueryClient();
    queryClient.setQueryData(queryKeys.task("task-1"), detail);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(
      () =>
        useTaskDependencyActions("project-1", "task-1", { blockerTaskID: "task-2", blockedTaskID: "task-1" }),
      {
        wrapper: testWrapper(services, queryClient),
      },
    );

    await act(async () => {
      result.current.submit({ kind: "remove" });
    });

    expect(invalidate).not.toHaveBeenCalled();
    expect(queryClient.getQueryData<TaskDetail>(queryKeys.task("task-1"))?.dependencies.blockerCount).toBe(1);
  });
});

function testWrapper(services: ReturnType<typeof createTaskDetailTestServices>, queryClient: QueryClient) {
  return function TestWrapper({ children }: Readonly<{ children: ReactNode }>) {
    return (
      <TestAppProviders services={services} queryClient={queryClient}>
        {children}
      </TestAppProviders>
    );
  };
}

function taskWithBlocker() {
  return taskBlockedByFixture(taskDetailResponse);
}
