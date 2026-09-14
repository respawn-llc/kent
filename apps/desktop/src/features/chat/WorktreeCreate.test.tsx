import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { CreateTargetResolutionKind } from "@/api";

import { appI18n } from "@/i18n";
import {
  worktreeQueryFixtureRoutes,
  worktreeResolutionFixture,
  worktreeErrorFixture,
} from "@/test-support/api";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createWorktreeCreate, useWorktreeCreate } from "./WorktreeCreate";
import { RpcError, WorktreeError } from "@/api";

function setup(options?: Parameters<typeof worktreeQueryFixtureRoutes>[0]) {
  const services = createTestServices(worktreeQueryFixtureRoutes(options));
  const send = vi.spyOn(services.api, "createWorktree");
  const submitSwitch = vi.fn();
  const navigator = createTestSidebarNavigator();
  const push = vi.fn();
  const refresh = vi.fn();
  const model = createWorktreeCreate({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    suggestion: "feature",
    navigator,
    refreshOpenWorktreeList: refresh,
    submitSwitch,
    push,
    t: appI18n.t,
  });
  const view = renderHook(() => ({ state: useAtomValue(model.state), ...useWorktreeCreate(model) }), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  return { ...view, services, send, submitSwitch, navigator, push, refresh };
}

it("cancels a pending submit on edit and rejects an obsolete resolution", async () => {
  const view = setup();
  const old = deferred<Awaited<ReturnType<typeof view.services.api.resolveWorktreeCreateTarget>>>();
  const latest = deferred<Awaited<ReturnType<typeof view.services.api.resolveWorktreeCreateTarget>>>();
  const resolve = vi
    .spyOn(view.services.api, "resolveWorktreeCreateTarget")
    .mockReturnValueOnce(old.promise)
    .mockReturnValueOnce(latest.promise);
  await waitFor(() => {
    expect(resolve).toHaveBeenCalledTimes(1);
  });
  act(() => {
    view.result.current.submit(undefined);
    view.result.current.editTarget("new-target");
  });
  expect(view.result.current.state.classification).toBeUndefined();
  await waitFor(() => {
    expect(resolve).toHaveBeenCalledTimes(2);
  });
  await act(async () => {
    old.resolve(
      worktreeResolutionFixture(
        "feature",
        CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_EXISTING_BRANCH,
      ),
    );
    await old.promise;
  });
  expect(view.send).not.toHaveBeenCalled();
  expect(view.result.current.state.classification).toBeUndefined();
  await act(async () => {
    latest.resolve(
      worktreeResolutionFixture(
        "new-target",
        CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
      ),
    );
    await latest.promise;
  });
  await waitFor(() => {
    expect(view.result.current.state.classification).toBe(
      CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
    );
  });
  expect(view.send).not.toHaveBeenCalled();
});

it("notifies retained setup failure and returns the original form to a list entry without a duplicate refresh", async () => {
  const view = setup();
  view.send.mockRejectedValue(worktreeErrorFixture("setup"));
  await waitFor(() => {
    expect(view.result.current.state.classification).toBeDefined();
  });
  act(() => {
    view.result.current.submit(undefined);
  });
  await waitFor(() => {
    expect(view.push).toHaveBeenCalledTimes(1);
  });
  expect(view.navigator.replace).toHaveBeenCalledWith({
    kind: "worktree",
    sessionID: "session-1",
    page: "list",
  });
  expect(view.refresh).not.toHaveBeenCalled();
  expect(view.submitSwitch).not.toHaveBeenCalled();
});

it("starts automatic Switch once after accepted Create finishes even after form disposal", async () => {
  const view = setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.createWorktree>>>();
  view.send.mockReturnValue(response.promise);
  await waitFor(() => {
    expect(view.result.current.state.classification).toBeDefined();
  });
  act(() => {
    view.result.current.submit(undefined);
    view.result.current.submit(undefined);
  });
  await waitFor(() => {
    expect(view.send).toHaveBeenCalledTimes(1);
  });
  expect(view.result.current.state.pending).toBe(true);
  const input = view.send.mock.calls[0]?.[0];
  if (input === undefined) throw new Error("Create input required");
  view.unmount();
  view.send.mockRestore();
  const created = await view.services.api.createWorktree(input);
  await act(async () => {
    response.resolve(created);
    await response.promise;
  });
  await waitFor(() => {
    expect(view.submitSwitch).toHaveBeenCalledTimes(1);
  });
  expect(view.submitSwitch.mock.calls[0]?.[0]).toBe(created.worktree?.projection?.switch);
  expect(view.push).not.toHaveBeenCalled();
});

it("waits for the latest resolution before submitting its authority without Base ref for an existing branch", async () => {
  const view = setup();
  const resolved = await view.services.api.resolveWorktreeCreateTarget("session-1", "feature");
  const response = deferred<typeof resolved>();
  vi.spyOn(view.services.api, "resolveWorktreeCreateTarget").mockReturnValue(response.promise);
  act(() => {
    view.result.current.submit(undefined);
  });
  expect(view.send).not.toHaveBeenCalled();
  await act(async () => {
    response.resolve(resolved);
  });
  await waitFor(() => {
    expect(view.send).toHaveBeenCalledTimes(1);
  });
  expect(view.send.mock.calls[0]?.[0]).toMatchObject({ resolution: resolved.resolution, baseRef: null });
});

it("rejects empty target and required Base ref without a Create request", async () => {
  const view = setup({
    createKind: CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
  });
  const resolve = vi.spyOn(view.services.api, "resolveWorktreeCreateTarget");
  act(() => {
    view.result.current.editTarget("");
    view.result.current.submit(undefined);
  });
  expect(view.result.current.state.targetError).toBe(appI18n.t("chat.worktree.targetRequired"));
  expect(resolve).not.toHaveBeenCalled();
  act(() => {
    view.result.current.editTarget("feature");
  });
  await waitFor(() => {
    expect(view.result.current.state.classification).toBe(
      CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
    );
  });
  act(() => {
    view.result.current.editBase(" ");
    view.result.current.submit(undefined);
  });
  expect(view.result.current.state.baseError).toBe(appI18n.t("chat.worktree.baseRequired"));
  expect(view.send).not.toHaveBeenCalled();
});

it("does not submit a pending intent after its form owner is disposed", async () => {
  const view = setup();
  const response = deferred<Awaited<ReturnType<typeof view.services.api.resolveWorktreeCreateTarget>>>();
  const resolve = vi
    .spyOn(view.services.api, "resolveWorktreeCreateTarget")
    .mockReturnValue(response.promise);
  await waitFor(() => {
    expect(resolve).toHaveBeenCalledTimes(1);
  });
  act(() => {
    view.result.current.submit(undefined);
  });
  vi.useFakeTimers();
  try {
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
  } finally {
    vi.useRealTimers();
  }
  await act(async () => {
    response.resolve(
      worktreeResolutionFixture(
        "feature",
        CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
      ),
    );
    await response.promise;
  });
  expect(view.send).not.toHaveBeenCalled();
});

it.each(["base_ref", "form"] as const)(
  "projects typed %s errors without discarding inputs",
  async (owner) => {
    const view = setup({
      createKind: CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
    });
    view.send.mockRejectedValue(
      new WorktreeError(new RpcError({ code: 1, method: "create", message: "Creation failed" }), {
        kind: "create",
        owner,
        diagnostic: "Creation failed",
      }),
    );
    await waitFor(() => {
      expect(view.result.current.state.classification).toBe(
        CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
      );
    });
    act(() => {
      view.result.current.submit(undefined);
    });
    await waitFor(() => {
      expect(
        owner === "base_ref" ? view.result.current.state.baseError : view.result.current.state.formError,
      ).toBeDefined();
    });
    expect(view.result.current.state.target).toBe("feature");
    expect(view.result.current.state.base).toBe("HEAD");
  },
);
