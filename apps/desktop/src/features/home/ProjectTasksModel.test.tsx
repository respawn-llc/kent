import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { createTestServices } from "@/test-support/app-services";
import { createTestSidebarController } from "@/test-support/sidebar";
import { deferred } from "@/test-support/chat-runtime";
import type { SidebarRootOutcome } from "@/app-facade";
import { createProjectTaskWorkflowModel } from "./projectTaskWorkflows";
import { createProjectTasksModel, useProjectTasksActions } from "./ProjectTasksModel";

it("rejects New Task when the current Workflow result does not permit creation", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "listWorkflows").mockResolvedValue({ workflows: [], nextOffset: null });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const workflows = createProjectTaskWorkflowModel(services.api, client, "project-1");
  const model = createProjectTasksModel(client, "project-1", workflows.available);
  const open = vi.fn(createTestSidebarController().open);
  const view = renderHook(
    () => ({
      request: useAtomValue(workflows.request),
      ...useProjectTasksActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await waitFor(() => {
    expect(view.result.current.request.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.newTask({ open, mode: "shift" });
  });
  expect(open).not.toHaveBeenCalled();
});

it("admits Link Workflow once while its owned sidebar remains open", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "listWorkflows").mockResolvedValue({ workflows: [], nextOffset: null });
  const client = new QueryClient();
  const workflows = createProjectTaskWorkflowModel(services.api, client, "project-1");
  const model = createProjectTasksModel(client, "project-1", workflows.available);
  const completion = deferred<SidebarRootOutcome>();
  const open = vi.fn(() => ({ lifecycle: completion.promise, release: vi.fn() }));
  const view = renderHook(() => useProjectTasksActions(model), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  await act(async () => {
    view.result.current.linkWorkflow({ open, mode: "shift" });
    view.result.current.linkWorkflow({ open, mode: "shift" });
  });
  expect(open).toHaveBeenCalledOnce();
  await act(async () => {
    completion.resolve("closed");
  });
  await act(async () => {
    view.result.current.linkWorkflow({ open, mode: "shift" });
  });
  expect(open).toHaveBeenCalledTimes(2);
});

it("admits New Task once while its owned sidebar remains open", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "listWorkflows").mockResolvedValue({
    workflows: [
      {
        id: "workflow-1",
        name: "Delivery",
        description: "",
        version: 1,
        projectLink: { isDefault: false },
        executionTargetPolicy: { customRef: null, mode: "default_branch" },
      },
    ],
    nextOffset: null,
  });
  const client = new QueryClient();
  const workflows = createProjectTaskWorkflowModel(services.api, client, "project-1");
  const model = createProjectTasksModel(client, "project-1", workflows.available);
  const completion = deferred<SidebarRootOutcome>();
  const open = vi.fn(() => ({ lifecycle: completion.promise, release: vi.fn() }));
  const view = renderHook(
    () => ({
      available: useAtomValue(workflows.available),
      ...useProjectTasksActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await waitFor(() => {
    expect(view.result.current.available).toBe(true);
  });
  await act(async () => {
    view.result.current.newTask({ open, mode: "shift" });
    view.result.current.newTask({ open, mode: "shift" });
  });
  expect(open).toHaveBeenCalledOnce();
  await act(async () => {
    completion.resolve("closed");
  });
});
