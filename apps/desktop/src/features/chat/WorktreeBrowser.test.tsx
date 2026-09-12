import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";

import {
  ChatRuntimeProvider,
  queryKeys,
  replaceWorktreeListRead,
  SidebarHeaderActionProvider,
  SidebarHeaderActionSlot,
  SidebarRootContext,
  SidebarRootOwner,
  worktreeTransitionOutcomeHandler,
  type WorktreeBrowserActions,
  type WorktreeBrowserAction,
} from "@/app-facade";
import { appI18n } from "@/i18n";
import { worktreeBrowserFixtureEntry, worktreeBrowserFixtureRoute } from "@/test-support/api";
import { createTestServices, TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import { createTestSidebarController, createTestSidebarNavigator } from "@/test-support/sidebar";
import {
  deferred,
  hydration,
  mainViewRead,
  runtimeApi,
  runtimeHost,
  target,
} from "@/test-support/chat-runtime";
import { WorktreeBrowser as Browser } from "./WorktreeBrowser";
import { WorktreeControl } from "./WorktreeControl";

function BrowserProviders({
  services,
  children,
}: Readonly<{ services: TestAppServices; children: ReactNode }>) {
  const [roots] = useState(() => createTestSidebarController());
  return (
    <TestAppProviders services={services}>
      <SidebarRootContext.Provider value={roots}>
        <SidebarRootOwner>
          <SidebarHeaderActionProvider>
            <SidebarHeaderActionSlot />
            {children}
          </SidebarHeaderActionProvider>
        </SidebarRootOwner>
      </SidebarRootContext.Provider>
    </TestAppProviders>
  );
}

function WorktreeBrowser(props: Omit<Parameters<typeof Browser>[0], "navigator">) {
  const [navigator] = useState(() => createTestSidebarNavigator());
  return <Browser {...props} navigator={navigator} />;
}

function operationOf(action: WorktreeBrowserAction | undefined) {
  if (action === undefined || action.kind === "create") throw new Error("Expected a row action");
  return action.operation;
}

it("opens with a fresh read even when the Session list is cached", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const cached = await services.api.listWorktrees("session-1");
  client.setQueryData(queryKeys.worktreeList("session-1"), cached);
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser sessionID="session-1" onAction={vi.fn()} />
      </QueryClientProvider>
    </BrowserProviders>,
  );
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(2);
  });
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  expect(client.getQueryData(queryKeys.worktreeList("session-1"))).not.toBe(cached);
});

it("isolates late list delivery after changing Session", async () => {
  const route = worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]);
  const result = route.result;
  const old = deferred<typeof result>();
  const services = createTestServices([
    {
      descriptor: route.descriptor,
      resultFactory: async (_request, index) => (index === 0 ? old.promise : result),
    },
  ]);
  const client = new QueryClient();
  const surface = (sessionID: string) => (
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser sessionID={sessionID} onAction={vi.fn()} />
      </QueryClientProvider>
    </BrowserProviders>
  );
  const view = render(surface("session-1"));
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(1);
  });
  view.rerender(surface("session-2"));
  await waitFor(() => {
    expect(client.getQueryData(queryKeys.worktreeList("session-2"))).toBeDefined();
  });
  const current = client.getQueryData(queryKeys.worktreeList("session-2"));
  await act(async () => {
    old.resolve(result);
    await old.promise;
  });
  expect(client.getQueryData(queryKeys.worktreeList("session-2"))).toBe(current);
  expect(services.transport.descriptorCalls).toHaveLength(2);
});

it("reopening replaces an unfinished read without accepting its late delivery", async () => {
  const route = worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]);
  const result = route.result;
  const old = deferred<typeof result>();
  const services = createTestServices([
    {
      descriptor: route.descriptor,
      resultFactory: async (_request, index) => (index === 0 ? old.promise : result),
    },
  ]);
  const client = new QueryClient();
  const surface = (open: boolean) => (
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        {open ? <WorktreeBrowser sessionID="session-1" onAction={vi.fn()} /> : null}
      </QueryClientProvider>
    </BrowserProviders>
  );
  const view = render(surface(true));
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(1);
  });
  view.rerender(surface(false));
  view.rerender(surface(true));
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(2);
  });
  const key = queryKeys.worktreeList("session-1");
  await waitFor(() => {
    expect(client.getQueryData(key)).toBeDefined();
  });
  const current = client.getQueryData(key);
  await act(async () => {
    old.resolve(result);
    await old.promise;
  });
  expect(client.getQueryData(key)).toBe(current);
});

it("reconnect refreshes the shared control and open list once", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const runtime = runtimeApi({ reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))] });
  const api = { connection: services.api.connection, chat: runtime.api };
  const client = new QueryClient();
  const onReconnected = vi.fn(() => {
    void replaceWorktreeListRead(client, services.api, target.sessionID);
  });
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <ChatRuntimeProvider api={api} target={target} host={runtimeHost()} onReconnected={onReconnected}>
          <WorktreeControl sessionID={target.sessionID} onAction={vi.fn()} />
          <WorktreeBrowser sessionID={target.sessionID} onAction={vi.fn()} />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </BrowserProviders>,
  );
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  const before = services.transport.descriptorCalls.length;
  expect(before).toBeGreaterThan(0);
  act(() => {
    services.transport.connection.set("connecting");
    services.transport.connection.set("connected");
  });
  expect(onReconnected).not.toHaveBeenCalled();
  act(() => {
    services.transport.connection.set("disconnected");
    services.transport.connection.set("connecting");
    services.transport.connection.set("connected");
  });
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(before + 1);
  });
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  expect(runtime.getMainView).toHaveBeenCalledTimes(2);
  expect(onReconnected).toHaveBeenCalledOnce();
});

it("refreshes after a completed typed transition without changing Chat target or invoking mutations", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const runtime = runtimeApi();
  const api = { connection: services.api.connection, chat: runtime.api };
  const client = new QueryClient();
  const host = runtimeHost({
    onWorktreeTransitionOutcome: worktreeTransitionOutcomeHandler(client, services.api, target.sessionID),
  });
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <ChatRuntimeProvider api={api} target={target} host={host}>
          <WorktreeBrowser sessionID={target.sessionID} onAction={vi.fn()} />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </BrowserProviders>,
  );
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  const handler = runtime.handlers[0];
  if (handler === undefined) throw new Error("Missing transcript observer");
  act(() => {
    handler.onOpen?.();
    handler.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
  });
  const before = services.transport.descriptorCalls.length;
  const key = queryKeys.chatMainView(target.sessionID);
  const view = client.getQueryData(key);
  act(() => {
    handler.onEvent({
      sequence: 2,
      kind: "worktree_transition_outcome",
      payload: {
        OperationID: "operation-1",
        Transition: "enter",
        State: "completed",
        Failure: null,
        SelectorError: null,
        DeletePrecondition: null,
      },
    });
  });
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(before + 1);
  });
  expect(client.getQueryData(key)).toBe(view);
  act(() => {
    handler.onEvent({
      sequence: 3,
      kind: "worktree_transition_outcome",
      payload: {
        OperationID: "operation-2",
        Transition: "enter",
        State: "failed",
        Failure: null,
        SelectorError: null,
        DeletePrecondition: null,
      },
    });
  });
  await act(async () => {
    await Promise.resolve();
  });
  expect(services.transport.descriptorCalls).toHaveLength(before + 1);
  expect(
    services.transport.descriptorCalls.every(
      (call) => call.descriptor === services.transport.descriptorCalls[0]?.descriptor,
    ),
  ).toBe(true);
});

it("Refresh retains the completed list on failure and Retry replaces it", async () => {
  const route = worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]);
  const result = route.result;
  const refreshing = deferred<typeof result>();
  const services = createTestServices([
    {
      descriptor: route.descriptor,
      resultFactory: async (_request, index) => {
        if (index === 1) return refreshing.promise;
        return result;
      },
    },
  ]);
  const client = new QueryClient();
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser sessionID="session-1" onAction={vi.fn()} />
      </QueryClientProvider>
    </BrowserProviders>,
  );
  const key = queryKeys.worktreeList("session-1");
  await waitFor(() => {
    expect(client.getQueryData(key)).toBeDefined();
  });
  const completed = client.getQueryData(key);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.refresh") }));
  expect(client.getQueryState(key)?.fetchStatus).toBe("fetching");
  expect(client.getQueryData(key)).toBe(completed);
  expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") })).toBeEnabled();
  await act(async () => {
    refreshing.reject(new Error("Read failed"));
    await refreshing.promise.catch(() => undefined);
  });
  await waitFor(() => {
    expect(client.getQueryState(key)?.status).toBe("error");
  });
  expect(client.getQueryData(key)).toBe(completed);
  expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  await waitFor(() => {
    expect(client.getQueryState(key)?.status).toBe("success");
  });
  expect(client.getQueryData(key)).not.toBe(completed);
  expect(services.transport.descriptorCalls).toHaveLength(3);
});

it.each([
  ["mainWorkspace", true, 0, 0],
  ["mainWorkspace", false, 1, 0],
  ["registered", true, 0, 1],
  ["registered", false, 1, 1],
  ["missing", false, 0, 1],
] as const)(
  "dispatches only server-projected actions for %s current=%s",
  async (kind, current, switches, deletes) => {
    const services = createTestServices([
      worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry(kind, current)]),
    ]);
    const client = new QueryClient();
    const onAction = vi.fn<WorktreeBrowserActions>();
    render(
      <BrowserProviders services={services}>
        <QueryClientProvider client={client}>
          <WorktreeBrowser sessionID="session-1" onAction={onAction} />
        </QueryClientProvider>
      </BrowserProviders>,
    );
    await waitFor(() => {
      expect(client.getQueryState(queryKeys.worktreeList("session-1"))?.status).toBe("success");
    });
    const list = client.getQueryData<Awaited<ReturnType<typeof services.api.listWorktrees>>>(
      queryKeys.worktreeList("session-1"),
    );
    const projection = list?.worktrees[0]?.projection;
    if (projection === undefined) throw new Error("Missing fixture projection");
    const user = userEvent.setup();
    await user.click(screen.getByText(kind === "mainWorkspace" ? "Workspace" : "Feature"));
    expect(onAction).not.toHaveBeenCalled();
    const switchButtons = screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.switch") });
    const deleteButtons = screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.delete") });
    expect(switchButtons).toHaveLength(switches);
    expect(deleteButtons).toHaveLength(deletes);
    for (const button of switchButtons) {
      await user.click(button);
      expect(onAction.mock.lastCall?.[0]).toMatchObject({ kind: "switch", sessionID: "session-1" });
      expect(operationOf(onAction.mock.lastCall?.[0])).toBe(projection.switch);
    }
    for (const button of deleteButtons) {
      await user.click(button);
      expect(onAction.mock.lastCall?.[0]).toMatchObject({ kind: "delete", sessionID: "session-1" });
      expect(operationOf(onAction.mock.lastCall?.[0])).toBe(projection.deletePreview);
    }
    await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.create") }));
    expect(onAction.mock.lastCall?.[0]).toMatchObject({ kind: "create", sessionID: "session-1" });
    expect(services.transport.descriptorCalls).toHaveLength(1);
  },
);
