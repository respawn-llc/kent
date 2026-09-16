import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RegistryProvider } from "@effect/atom-react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { StatusNotice } from "@/ui";
import { deferred } from "@/test-support/chat-runtime";
import type { ApiService } from "@/api";

import { WorkflowDeleteButton } from "./WorkflowDeleteButton";

const fixture = vi.hoisted(() => {
  const impact = {
    linkCount: 1,
    projectCount: 1,
    taskCount: 2,
    version: 7,
    workflowID: "workflow-1",
    defaultReplacementProjectCount: 0,
    currentNodeCount: 0,
    pendingApprovalCount: 0,
    blockedTaskCount: 0,
  };
  return {
    impact,
    deleteWorkflow: vi.fn<ApiService["deleteWorkflow"]>(async () => ({
      blockers: [],
      deleted: true,
      impact,
    })),
    previewWorkflowDelete: vi.fn(async () => impact),
    push: vi.fn<(notice: StatusNotice) => void>(),
    dismiss: vi.fn<(id: string) => void>(),
    matchRoute: vi.fn(() => false),
    openWorkflowLibrary: vi.fn(async () => "completed"),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal()),
  useMatchRoute: () => fixture.matchRoute,
}));

vi.mock("@/app-facade", async (importOriginal) => ({
  ...(await importOriginal()),
  useAppNavigation: () => ({ openWorkflowLibrary: fixture.openWorkflowLibrary }),
  useAppServices: () => ({
    api: fixture,
  }),
  useStatusController: () => ({ push: fixture.push, dismiss: fixture.dismiss }),
}));

describe("WorkflowDeleteButton completion", () => {
  beforeEach(() => {
    fixture.deleteWorkflow.mockClear();
    fixture.previewWorkflowDelete.mockClear();
    fixture.push.mockClear();
    fixture.dismiss.mockClear();
    fixture.matchRoute.mockReturnValue(false);
    fixture.openWorkflowLibrary.mockClear();
  });

  it("notifies its mounted owner only after deletion invalidation finishes", async () => {
    const queryClient = new QueryClient();
    let finishInvalidation: () => void = () => {
      return;
    };
    const invalidation = new Promise<void>((resolve) => {
      finishInvalidation = resolve;
    });
    vi.spyOn(queryClient, "invalidateQueries").mockImplementation(async () => invalidation);
    const onDeleted = vi.fn();
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <RegistryProvider>
          <WorkflowDeleteButton onDeleted={onDeleted} workflowID="workflow-1" />
        </RegistryProvider>
      </QueryClientProvider>,
    );

    await user.click(screen.getByRole("button", { name: "workflowEditor.workflowDelete" }));
    await user.click(await screen.findByRole("button", { name: "workflowEditor.workflowDeleteConfirm" }));
    await waitFor(() => {
      expect(fixture.deleteWorkflow).toHaveBeenCalledOnce();
    });
    expect(onDeleted).not.toHaveBeenCalled();

    finishInvalidation();
    await waitFor(() => {
      expect(onDeleted).toHaveBeenCalledOnce();
    });
    queryClient.clear();
  });

  it("retries a failed preview without deleting before confirmation", async () => {
    fixture.previewWorkflowDelete.mockRejectedValueOnce(new Error("preview unavailable"));
    const queryClient = new QueryClient();
    const user = userEvent.setup();
    const view = render(
      <QueryClientProvider client={queryClient}>
        <RegistryProvider>
          <WorkflowDeleteButton workflowID="workflow-1" />
        </RegistryProvider>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "workflowEditor.workflowDelete" }));
    await waitFor(() => {
      expect(fixture.push).toHaveBeenCalledOnce();
    });
    const notice = fixture.push.mock.calls[0]?.[0];
    if (notice === undefined) throw new Error("Missing preview failure");
    expect(notice.onAction).toBeTypeOf("function");
    await act(async () => notice.onAction?.());
    expect(fixture.previewWorkflowDelete).toHaveBeenCalledTimes(2);
    expect(fixture.deleteWorkflow).not.toHaveBeenCalled();
    await user.click(await screen.findByRole("button", { name: "workflowEditor.workflowDeleteConfirm" }));
    await waitFor(() => {
      expect(fixture.deleteWorkflow).toHaveBeenCalledOnce();
    });
    view.unmount();
    queryClient.clear();
  });

  it("releases a failed preview notification when its destination closes", async () => {
    fixture.previewWorkflowDelete.mockRejectedValueOnce(new Error("preview unavailable"));
    const queryClient = new QueryClient();
    const user = userEvent.setup();
    const view = render(
      <QueryClientProvider client={queryClient}>
        <RegistryProvider>
          <WorkflowDeleteButton workflowID="workflow-1" />
        </RegistryProvider>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "workflowEditor.workflowDelete" }));
    await waitFor(() => {
      expect(fixture.push).toHaveBeenCalledOnce();
    });
    const notice = fixture.push.mock.calls[0]?.[0];
    if (notice === undefined) throw new Error("Missing preview failure");
    const released = vi.fn();
    const unsubscribe = queryClient.getMutationCache().subscribe((event) => {
      if (event.type === "observerRemoved") released();
    });
    view.unmount();
    expect(fixture.dismiss).toHaveBeenCalledWith(notice.id);
    await waitFor(() => {
      expect(released).toHaveBeenCalled();
    });
    unsubscribe();
    await act(async () => notice.onAction?.());
    expect(fixture.previewWorkflowDelete).toHaveBeenCalledOnce();
    expect(fixture.deleteWorkflow).not.toHaveBeenCalled();
    queryClient.clear();
  });

  it("finishes accepted deletion and invalidation after navigation without redirecting the new destination", async () => {
    const queryClient = new QueryClient();
    const response = deferred<Awaited<ReturnType<ApiService["deleteWorkflow"]>>>();
    fixture.deleteWorkflow.mockReturnValueOnce(response.promise);
    fixture.matchRoute.mockReturnValue(true);
    const invalidated = vi.spyOn(queryClient, "invalidateQueries");
    const onDeleted = vi.fn();
    const user = userEvent.setup();
    const view = render(
      <QueryClientProvider client={queryClient}>
        <RegistryProvider>
          <WorkflowDeleteButton onDeleted={onDeleted} workflowID="workflow-1" />
        </RegistryProvider>
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "workflowEditor.workflowDelete" }));
    await user.click(await screen.findByRole("button", { name: "workflowEditor.workflowDeleteConfirm" }));
    fixture.matchRoute.mockReturnValue(false);
    view.unmount();
    await act(async () => {
      response.resolve({ deleted: true, blockers: [], impact: fixture.impact });
    });
    await waitFor(() => {
      expect(onDeleted).toHaveBeenCalledOnce();
    });
    expect(invalidated).toHaveBeenCalled();
    expect(fixture.openWorkflowLibrary).not.toHaveBeenCalled();
  });
});
