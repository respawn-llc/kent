import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

import { appI18n } from "@/i18n";
import { worktreeBrowserFixtureEntry, worktreeBrowserFixtureRoute } from "@/test-support/api";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createWorktreeActions, useWorktreeActions } from "./WorktreeActions";

it("ends Switch pending at acknowledgement without applying a target and admits one invocation", async () => {
  const services = createTestServices([
    worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)], "session-title"),
  ]);
  const list = await services.api.listWorktrees("session-1");
  const operation = list.worktrees[0]?.projection?.switch;
  if (operation === undefined) throw new Error("Switch authority required");
  const response = deferred<Awaited<ReturnType<typeof services.api.switchWorktree>>>();
  const send = vi.spyOn(services.api, "switchWorktree").mockReturnValue(response.promise);
  const navigator = createTestSidebarNavigator();
  const push = vi.fn();
  const model = createWorktreeActions({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    navigator,
    push,
    t: appI18n.t,
    refreshOpenWorktreeList: vi.fn(),
  });
  const view = renderHook(
    () => ({
      pending: useAtomValue(model.switching).isPending,
      ...useWorktreeActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await act(async () => {
    view.result.current.switchWorktree(operation);
    view.result.current.switchWorktree(operation);
  });
  expect(send).toHaveBeenCalledTimes(1);
  expect(view.result.current.pending).toBe(true);
  expect(navigator.close).not.toHaveBeenCalled();
  await act(async () => {
    response.resolve({
      $typeName: "kent.api.worktree.ScheduledAcknowledgement",
      operationId: crypto.randomUUID(),
    });
    await response.promise;
  });
  await waitFor(() => {
    expect(view.result.current.pending).toBe(false);
  });
  expect(navigator.close).toHaveBeenCalledTimes(1);
  expect(push).not.toHaveBeenCalled();
  expect(list.target?.worktree).toBeUndefined();
});

it("delivers immediate Switch failure once after its observer is disposed", async () => {
  const services = createTestServices([
    worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
  ]);
  const operation = (await services.api.listWorktrees("session-1")).worktrees[0]?.projection?.switch;
  if (operation === undefined) throw new Error("Switch authority required");
  const response = deferred<Awaited<ReturnType<typeof services.api.switchWorktree>>>();
  const send = vi.spyOn(services.api, "switchWorktree").mockReturnValue(response.promise);
  const navigator = createTestSidebarNavigator();
  const push = vi.fn();
  const model = createWorktreeActions({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    navigator,
    push,
    t: appI18n.t,
    refreshOpenWorktreeList: vi.fn(),
  });
  const view = renderHook(() => useWorktreeActions(model), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  await act(async () => {
    view.result.current.switchWorktree(operation);
  });
  view.unmount();
  await act(async () => {
    response.reject(new Error("Switch rejected"));
    await response.promise.catch(() => undefined);
  });
  expect(push).toHaveBeenCalledTimes(1);
  expect(navigator.close).not.toHaveBeenCalled();
  expect(send).toHaveBeenCalledTimes(1);
});
