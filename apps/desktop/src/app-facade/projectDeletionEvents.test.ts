import { createBrowserNativeBridge } from "@app/native-bridge";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";

import { isProjectMissingError, RpcError } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { invalidateProjectDeleteQueries, useProjectDeletedEvents } from "./projectDeletionEvents";
import { queryKeys } from "./queryKeys";

describe("Project deletion owner refresh", () => {
  it.each([
    ["Project Edit", queryKeys.projectEdit("project-1")],
    ["Link Workflow", queryKeys.projectWorkflowLinks("project-1")],
    ["Project Workflow Editor", queryKeys.projectWorkflowLinks("project-1")],
    ["Project Workspace catalog", queryKeys.projectWorkspaceCatalog("project-1")],
    ["exact Project Workspace", queryKeys.projectWorkspace("project-1", "workspace-1")],
  ])("drives the native deletion event through a second %s owner read", async (_name, queryKey) => {
    const bridge = createBrowserNativeBridge();
    const services = createTestServices([], bridge);
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let requests = 0;
    const observer = new QueryObserver(queryClient, {
      queryFn: async () => {
        requests += 1;
        if (requests === 1) return { request: requests };
        throw new RpcError({
          code: -32014,
          message: "missing",
          method: "owner.read",
        });
      },
      queryKey,
    });
    const unsubscribe = observer.subscribe(() => undefined);
    await observer.refetch();
    let refresh: Promise<void> | undefined;
    const handler = vi.fn(async () => {
      refresh = invalidateProjectDeleteQueries(queryClient, "project-1");
      return refresh;
    });
    const wrapper = ({ children }: Readonly<{ children: ReactNode }>) =>
      createElement(TestAppProviders, { children, services });
    const view = renderHook(
      () => {
        useProjectDeletedEvents(bridge, handler);
      },
      { wrapper },
    );
    await Promise.resolve();

    await act(async () => {
      await bridge.projectDeletion.notifyDeleted({ projectID: "project-1" });
    });
    await waitFor(() => {
      expect(handler).toHaveBeenCalledOnce();
    });
    await refresh;

    expect(handler).toHaveBeenCalledOnce();
    expect(requests).toBe(2);
    expect(isProjectMissingError(observer.getCurrentResult().error)).toBe(true);
    view.unmount();
    unsubscribe();
    queryClient.clear();
  });
});
