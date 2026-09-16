import { act, renderHook, waitFor } from "@testing-library/react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import type { CreatedTaskSummary } from "@/api";
import { useCreateTask } from "./useTaskMutations";

it("admits one Create and completes its accepted request after the destination leaves", async () => {
  const services = createTestServices([]);
  const response = deferred<CreatedTaskSummary>();
  const create = vi.spyOn(services.api, "createTask").mockReturnValue(response.promise);
  const onSuccess = vi.fn();
  const onError = vi.fn();
  const view = renderHook(() => useCreateTask("project-1", undefined, undefined), {
    wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders>,
  });
  const submission = {
    input: { projectID: "project-1", sourceWorkspaceID: "workspace-1", title: "Task", body: "", labelIDs: [], dependencyIntents: [] },
    onSuccess,
    onError,
  };
  await act(async () => {
    view.result.current.submit(submission);
    view.result.current.submit(submission);
  });
  expect(create).toHaveBeenCalledTimes(1);
  view.unmount();
  const created = { id: "task-1", shortID: "KENT-1", title: "Task", workflowID: "workflow-1" };
  await act(async () => { response.resolve(created); });
  await waitFor(() => { expect(onSuccess).toHaveBeenCalledWith(created); });
  expect(onError).not.toHaveBeenCalled();
});
