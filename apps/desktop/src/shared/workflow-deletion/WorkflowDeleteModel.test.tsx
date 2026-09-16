import { RegistryProvider, useAtomMount, useAtomSet } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import type { ApiService, WorkflowDeleteImpact } from "@/api";
import { createWorkflowDeleteModel } from "./WorkflowDeleteModel";

it("admits only one preview and one deletion for duplicate submissions", async () => {
  const services = createTestServices([]);
  const impact = deferred<WorkflowDeleteImpact>();
  const result = deferred<Awaited<ReturnType<ApiService["deleteWorkflow"]>>>();
  const preview = vi.spyOn(services.api, "previewWorkflowDelete").mockReturnValue(impact.promise);
  const deletion = vi.spyOn(services.api, "deleteWorkflow").mockReturnValue(result.promise);
  const onCompleted = vi.fn(async () => undefined);
  const model = createWorkflowDeleteModel({
    api: services.api,
    client: new QueryClient(),
    workflowID: "workflow-1",
    onCompletionError: vi.fn(),
  });
  const view = renderHook(
    () => {
      useAtomMount(model.preview);
      useAtomMount(model.deletion);
      return { open: useAtomSet(model.open), confirm: useAtomSet(model.confirm) };
    },
    { wrapper: ({ children }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await act(async () => {
    view.result.current.open({ onError: vi.fn() });
    view.result.current.open({ onError: vi.fn() });
  });
  expect(preview).toHaveBeenCalledOnce();
  const previewImpact: WorkflowDeleteImpact = {
    workflowID: "workflow-1",
    version: 1,
    projectCount: 1,
    taskCount: 1,
    linkCount: 1,
    defaultReplacementProjectCount: 0,
    currentNodeCount: 0,
    pendingApprovalCount: 0,
    blockedTaskCount: 0,
  };
  await act(async () => {
    impact.resolve(previewImpact);
  });
  await act(async () => {
    view.result.current.confirm({ onCompleted });
    view.result.current.confirm({ onCompleted });
  });
  expect(deletion).toHaveBeenCalledOnce();
  await act(async () => {
    result.resolve({ deleted: true, blockers: [], impact: previewImpact });
  });
  expect(onCompleted).toHaveBeenCalledOnce();
});
