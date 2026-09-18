import { act, renderHook, waitFor } from "@testing-library/react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import type { ReactNode } from "react";
import type { ProjectObservation } from "@/api";
import { ProjectLabelsProvider } from "@/shared/labels";
import { TestAppProviders, createTestServices, startupRoutes } from "@/test-support/app-services";
import { useProjectBoardSubscription } from "./useBoardData";

it("keeps one Board observation across selection changes and releases it, with explicit retry after failure", async () => {
  const services = createTestServices(startupRoutes);
  const acquired = vi.fn();
  const released = vi.fn();
  const failure = new Error("Observation failed");
  vi.spyOn(services.api, "subscribeProject").mockReturnValue(
    Stream.unwrap(
      Effect.acquireRelease(
        Effect.sync(() => {
          acquired();
          return Stream.concat(
            Stream.make({ kind: "error", error: failure } satisfies ProjectObservation),
            Stream.never,
          );
        }),
        () =>
          Effect.sync(() => {
            released();
          }),
      ),
    ),
  );
  const report = vi.fn();
  const { result, rerender, unmount } = renderHook(
    ({ taskID }) =>
      useProjectBoardSubscription("project-1", "workflow-1", {
        selectedTaskID: taskID,
        selectedWorkflowID: "workflow-1",
        onBackgroundError: report,
        onSelectedTaskDeleted: () => undefined,
      }),
    {
      initialProps: { taskID: "task-a" },
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services}>
          <ProjectLabelsProvider projectID="project-1" subscribeToProject={false}>
            {children}
          </ProjectLabelsProvider>
        </TestAppProviders>
      ),
    },
  );
  await waitFor(() => {
    expect(result.current.error).toBe(failure);
  });
  expect(report).toHaveBeenCalledWith(failure);
  rerender({ taskID: "task-b" });
  expect(acquired).toHaveBeenCalledTimes(1);
  expect(released).not.toHaveBeenCalled();
  act(() => {
    result.current.retry();
  });
  await waitFor(() => {
    expect(acquired).toHaveBeenCalledTimes(2);
  });
  expect(released).toHaveBeenCalledTimes(1);
  unmount();
  await waitFor(() => {
    expect(released).toHaveBeenCalledTimes(2);
  });
});
