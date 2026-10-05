import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RegistryProvider } from "@effect/atom-react";
import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import { useAtomValue } from "@effect/atom-react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";

import {
  ChatRuntimeProvider,
  useChatExecutionTarget,
  createRefreshOpenWorktreeList,
  queryKeys,
  SidebarHeaderActionProvider,
  SidebarHeaderActionSlot,
  SidebarRootContext,
  SidebarRootOwner,
  worktreeTransitionOutcomeHandler,
} from "@/app-facade";
import { appI18n } from "@/i18n";
import {
  worktreeBrowserFixtureEntry,
  worktreeBrowserFixtureRoute,
  worktreeCommandFixtureRoutes,
} from "@/test-support/api";
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
import { useWorktreeList } from "./useWorktreeList";
import { ChatShell } from "./ChatShell";
import { errorMessage } from "@/api";
import { CreateTargetResolutionKind, RpcError, WorktreeError } from "@/api";
import { createWorktreeCreate, useWorktreeCreate } from "./WorktreeCreate";
import { WorktreeCreateForm } from "./WorktreeCreateForm";
import { createWorktreeActions } from "./WorktreeActions";
import * as ui from "@/ui";

function BrowserProviders({
  services,
  children,
}: Readonly<{ services: TestAppServices; children: ReactNode }>) {
  const [roots] = useState(() => createTestSidebarController());
  return (
    <TestAppProviders services={services}>
      <RegistryProvider>
        <SidebarRootContext.Provider value={roots}>
          <SidebarRootOwner>
            <SidebarHeaderActionProvider>
              <SidebarHeaderActionSlot />
              {children}
            </SidebarHeaderActionProvider>
          </SidebarRootOwner>
        </SidebarRootContext.Provider>
      </RegistryProvider>
    </TestAppProviders>
  );
}

function createFormModel() {
  const services = createTestServices([]);
  const push = vi.fn();
  const model = createWorktreeCreate({
    client: new QueryClient(),
    api: services.api,
    sessionID: "session-1",
    suggestion: "topic",
    navigator: createTestSidebarNavigator(),
    refreshOpenWorktreeList: vi.fn(),
    submitSwitch: vi.fn(),
    push,
    t: appI18n.t,
  });
  return { services, push, model };
}
const resolvedCreateTarget = {
  $typeName: "kent.api.worktree.CreateTargetResolveSuccess",
  resolution: {
    $typeName: "kent.api.worktree.CreateTargetResolution",
    input: "topic",
    kind: CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
  },
} as const;

it.each(["list", "target"] as const)(
  "preserves Create fields behind its %s read Error and Retry",
  async (source) => {
    const services = createTestServices([worktreeBrowserFixtureRoute()]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const list = vi.spyOn(services.api, "listWorktrees");
    const resolve = vi
      .spyOn(services.api, "resolveWorktreeCreateTarget")
      .mockResolvedValue(resolvedCreateTarget);
    const create = vi.spyOn(services.api, "createWorktree");
    const actions = createWorktreeActions({
      client,
      api: services.api,
      sessionID: "session-1",
      onAccepted: vi.fn(),
      push: vi.fn(),
      t: appI18n.t,
      refreshOpenWorktreeList: vi.fn(),
    });
    render(
      <BrowserProviders services={services}>
        <QueryClientProvider client={client}>
          <WorktreeCreateForm
            sessionID="session-1"
            navigator={createTestSidebarNavigator()}
            actions={actions}
          />
        </QueryClientProvider>
      </BrowserProviders>,
    );
    const user = userEvent.setup();
    const targetInput = await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") });
    await user.clear(targetInput);
    await user.type(targetInput, "topic");
    const baseInput = await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.baseLabel") });
    await user.clear(baseInput);
    await user.type(baseInput, "custom-base");
    if (source === "list") {
      list.mockRejectedValueOnce(new Error("offline"));
      await act(async () => {
        await client.refetchQueries({ queryKey: queryKeys.worktreeList("session-1") });
      });
    } else {
      resolve.mockRejectedValueOnce(new Error("offline"));
      await user.type(targetInput, "-changed");
    }
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    const beforeList = list.mock.calls.length;
    const beforeResolve = resolve.mock.calls.length;
    await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
    expect(await screen.findByDisplayValue("custom-base")).toBeInTheDocument();
    expect(screen.getByDisplayValue(source === "list" ? "topic" : "topic-changed")).toBeInTheDocument();
    expect(list).toHaveBeenCalledTimes(beforeList + (source === "list" ? 1 : 0));
    expect(resolve).toHaveBeenCalledTimes(beforeResolve + (source === "target" ? 1 : 0));
    expect(create).not.toHaveBeenCalled();
  },
);

it("retries a failed Create target read without continuing the failed submission", async () => {
  const { services, model, push } = createFormModel();
  const resolve = vi
    .spyOn(services.api, "resolveWorktreeCreateTarget")
    .mockRejectedValueOnce(new Error("offline"))
    .mockResolvedValue(resolvedCreateTarget);
  const create = vi.spyOn(services.api, "createWorktree");
  const { result } = renderHook(
    () => ({
      actions: useWorktreeCreate(model),
      state: useAtomValue(model.state),
      resolution: useAtomValue(model.resolution),
    }),
    { wrapper: RegistryProvider },
  );
  act(() => {
    result.current.actions.editBase("base");
    result.current.actions.submit(undefined);
  });
  await waitFor(() => {
    expect(result.current.resolution?.isError).toBe(true);
  });
  act(() => {
    result.current.actions.retryResolution(undefined);
  });
  await waitFor(() => {
    expect(result.current.resolution?.isSuccess).toBe(true);
  });
  expect(result.current.state).toMatchObject({ target: "topic", base: "base" });
  expect(resolve).toHaveBeenCalledTimes(2);
  expect(create).not.toHaveBeenCalled();
  expect(push).not.toHaveBeenCalled();
});

it.each(["base_ref", "form"] as const)("classifies Create rejection owned by %s", async (owner) => {
  const { services, model, push } = createFormModel();
  vi.spyOn(services.api, "resolveWorktreeCreateTarget").mockResolvedValue(resolvedCreateTarget);
  vi.spyOn(services.api, "createWorktree").mockRejectedValue(
    new WorktreeError(new RpcError({ code: 1, method: "create", message: "rejected" }), {
      kind: "create",
      owner,
      diagnostic: "rejected",
    }),
  );
  const { result } = renderHook(
    () => ({
      actions: useWorktreeCreate(model),
      state: useAtomValue(model.state),
      resolution: useAtomValue(model.resolution),
    }),
    { wrapper: RegistryProvider },
  );
  await waitFor(() => {
    expect(result.current.resolution?.isSuccess).toBe(true);
  });
  await act(async () => {
    result.current.actions.submit(undefined);
  });
  if (owner === "base_ref") {
    expect(result.current.state.baseError).toBeDefined();
    expect(push).not.toHaveBeenCalled();
  } else {
    expect(result.current.state.baseError).toBeUndefined();
    expect(push).toHaveBeenCalledOnce();
  }
});

function WorktreeBrowser(props: Readonly<{ sessionID: string }>) {
  const [navigator] = useState(() => createTestSidebarNavigator());
  return (
    <Browser
      {...props}
      navigator={navigator}
      onCreate={vi.fn()}
      onSwitch={vi.fn()}
      refreshOpenWorktreeList={vi.fn()}
    />
  );
}

it("opens with a fresh read even when the Session list is cached", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const cached = await services.api.listWorktrees("session-1");
  client.setQueryData(queryKeys.worktreeList("session-1"), cached);
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser sessionID="session-1" />
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
        <WorktreeBrowser sessionID={sessionID} />
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
        {open ? <WorktreeBrowser sessionID="session-1" /> : null}
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

function ControlOwner() {
  const executionTarget = useChatExecutionTarget();
  const query = useWorktreeList(target.sessionID, executionTarget);
  return (
    <ChatShell
      selectedSession={target}
      sessionName={null}
      state={
        query.isError
          ? { kind: "error", diagnostic: errorMessage(query.error), onRetry: query.refresh }
          : { kind: "ready" }
      }
      content={() => null}
      composer={() => <WorktreeControl sessionID={target.sessionID} target={executionTarget} query={query} />}
    />
  );
}

it("keeps the shared control and open list independent of transcript loss", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const runtime = runtimeApi({ reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))] });
  const api = { chat: runtime.api };
  const client = new QueryClient();
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
          <ControlOwner />
          <WorktreeBrowser sessionID={target.sessionID} />
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
    runtime.handlers[0]?.onTransportLoss?.();
  });
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(before);
  });
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  expect(runtime.getMainView).toHaveBeenCalledTimes(1);
});

it("refreshes after a completed typed transition without changing Chat target or invoking mutations", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const runtime = runtimeApi();
  const api = { chat: runtime.api };
  const client = new QueryClient();
  const host = runtimeHost({
    onWorktreeTransitionOutcome: worktreeTransitionOutcomeHandler(
      createRefreshOpenWorktreeList(client, services.api, () => ({
        kind: "worktree",
        sessionID: target.sessionID,
        page: "list",
      })),
      target.sessionID,
      vi.fn(),
      appI18n.t,
    ),
  });
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <ChatRuntimeProvider api={api} target={target} host={host}>
          <WorktreeBrowser sessionID={target.sessionID} />
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
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(before + 2);
  });
  expect(
    services.transport.descriptorCalls.every(
      (call) => call.descriptor === services.transport.descriptorCalls[0]?.descriptor,
    ),
  ).toBe(true);
});

it("Refresh retains cached data but replaces the visible list on failure until Retry", async () => {
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
        <WorktreeBrowser sessionID="session-1" />
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
  expect(screen.queryByRole("button", { name: appI18n.t("chat.worktree.switch") })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  await waitFor(() => {
    expect(client.getQueryState(key)?.status).toBe("success");
  });
  expect(client.getQueryData(key)).not.toBe(completed);
  expect(services.transport.descriptorCalls).toHaveLength(3);
});

it("owns failed deletion preview at the Worktrees page and restores its popup on read-only Retry", async () => {
  const services = createTestServices([
    worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
    ...worktreeCommandFixtureRoutes(),
  ]);
  const preview = vi.spyOn(services.api, "previewWorktreeDelete").mockRejectedValueOnce(new Error("offline"));
  const remove = vi.spyOn(services.api, "deleteWorktree");
  render(
    <BrowserProviders services={services}>
      <WorktreeBrowser sessionID="session-1" />
    </BrowserProviders>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: appI18n.t("chat.worktree.switch") })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") })).toBeEnabled();
  expect(preview).toHaveBeenCalledTimes(2);
  expect(preview.mock.calls[1]).toEqual(preview.mock.calls[0]);
  expect(remove).not.toHaveBeenCalled();
});

it.each(["operational", "precondition"] as const)(
  "retains deletion confirmation on %s rejection without rereading or replaying",
  async (kind) => {
    const services = createTestServices([
      worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
      ...worktreeCommandFixtureRoutes(),
    ]);
    const preview = vi.spyOn(services.api, "previewWorktreeDelete");
    const remove = vi.spyOn(services.api, "deleteWorktree").mockRejectedValue(
      kind === "operational"
        ? new Error("offline")
        : new WorktreeError(new RpcError({ code: 1, method: "delete", message: "changed" }), {
            kind: "delete_precondition",
            details: {
              $typeName: "kent.api.worktree.DeletePreconditionDetails",
            },
          }),
    );
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    render(
      <BrowserProviders services={services}>
        <WorktreeBrowser sessionID="session-1" />
      </BrowserProviders>,
    );
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.delete") }));
    await user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
    await waitFor(() => {
      expect(notice).toHaveBeenCalledOnce();
    });
    expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.confirm") })).toBeEnabled();
    expect(preview).toHaveBeenCalledOnce();
    expect(remove).toHaveBeenCalledOnce();
    notice.mockRestore();
  },
);

it.each([
  ["mainWorkspace", true, 0, 0],
  ["mainWorkspace", false, 1, 0],
  ["registered", true, 0, 1],
  ["registered", false, 1, 1],
  ["missing", false, 0, 1],
] as const)(
  "offers only server-projected actions for %s current=%s",
  async (kind, current, switches, deletes) => {
    const services = createTestServices([
      worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry(kind, current)]),
    ]);
    const client = new QueryClient();
    render(
      <BrowserProviders services={services}>
        <QueryClientProvider client={client}>
          <WorktreeBrowser sessionID="session-1" />
        </QueryClientProvider>
      </BrowserProviders>,
    );
    await waitFor(() => {
      expect(client.getQueryState(queryKeys.worktreeList("session-1"))?.status).toBe("success");
    });
    const switchButtons = screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.switch") });
    const deleteButtons = screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.delete") });
    expect(switchButtons).toHaveLength(switches);
    expect(deleteButtons).toHaveLength(deletes);
    expect(services.transport.descriptorCalls).toHaveLength(1);
  },
);
