import { act, renderHook } from "@testing-library/react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { executeTaskInitiatingAction, useTaskInitiatingActionController } from "@/shared/execution-target";
import type { TaskMovePreviewResponse } from "@/api";

it("admits independent Task previews and replaces the dialog with the newest received preview", async () => {
  const services = createTestServices([]);
  const first = deferred<TaskMovePreviewResponse>();
  const second = deferred<TaskMovePreviewResponse>();
  const preview = vi
    .spyOn(services.api, "previewMoveTask")
    .mockReturnValueOnce(first.promise)
    .mockReturnValueOnce(second.promise);
  const { result } = renderHook(
    () => {
      const actions = useTaskInitiatingActionController({
        execute: async (action, selection) => executeTaskInitiatingAction(services.api, action, selection),
        onApplied: vi.fn(),
        onAppliedError: vi.fn(),
        onError: vi.fn(),
      });
      return actions;
    },
    { wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders> },
  );
  await act(async () => {
    for (const taskID of ["task-1", "task-1", "task-2"]) {
      result.current.preview({
        taskID,
        targetNodeID: "node-2",
        execute: async () => services.api.previewMoveTask(taskID, "node-2"),
        onBlocked: vi.fn(),
        onError: vi.fn(),
      });
    }
  });
  expect(preview).toHaveBeenCalledTimes(2);
  expect(result.current.pendingStartMoveTaskIDs).toEqual(new Set(["task-1", "task-2"]));
  const response: TaskMovePreviewResponse = {
    outcome: "transition",
    transition: {
      choices: [{ transitionKey: "next", label: "Next", sourceNodeDisplayName: "Plan", requiredValues: [] }],
    },
  };
  await act(async () => {
    second.resolve(response);
  });
  expect(result.current.confirmationTaskID).toBe("task-2");
  await act(async () => {
    first.resolve(response);
  });
  expect(result.current.confirmationTaskID).toBe("task-1");
  expect(result.current.pendingStartMoveTaskIDs.size).toBe(0);
  act(() => {
    result.current.close();
  });
  expect(result.current.pending).toBeNull();
});
