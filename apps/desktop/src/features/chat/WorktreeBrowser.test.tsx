import { expect, it, vi } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RegistryProvider } from "@effect/atom-react";
import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { useAtomValue } from "@effect/atom-react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";

import {
  ChatRuntimeProvider,
  useChatExecutionTarget,
  createRefreshOpenWorktreeList,
  queryKeys,
  replaceWorktreeListRead,
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
import type { ApiService } from "@/api";
import { createWorktreeCreate, useWorktreeCreate } from "./WorktreeCreate";
import { WorktreeCreateForm } from "./WorktreeCreateForm";
import { createWorktreeActions } from "./WorktreeActions";
import * as ui from "@/ui";

type WorktreeList = Awaited<ReturnType<ApiService["listWorktrees"]>>;

function BrowserProviders({
  services,
  children,
}: Readonly<{ services: TestAppServices; children: ReactNode }>) {
  const [roots] = useState(() => createTestSidebarController());
  return (
    <TestAppProviders services={services} skipAnimations>
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
    openSession: vi.fn(),
    client: new QueryClient(),
    api: services.api,
    projectID: "project-1",
    sessionID: "33333333-3333-4333-8333-333333333333",
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

it("keeps an invalid typed Worktree name editable and resumes resolution after correction", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const client = new QueryClient();
  vi.spyOn(services.api, "resolveWorktreeCreateTarget").mockImplementation(async (_sessionID, target) => ({
    ...resolvedCreateTarget,
    resolution: {
      ...resolvedCreateTarget.resolution,
      input: target,
      kind:
        target === "Test WT"
          ? CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_INVALID
          : CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
    },
  }));
  const create = vi.spyOn(services.api, "createWorktree");
  const actions = createWorktreeActions({
    client,
    api: services.api,
    sessionID: "33333333-3333-4333-8333-333333333333",
    onAccepted: vi.fn(),
    push: vi.fn(),
    t: appI18n.t,
    refreshOpenWorktreeList: vi.fn(),
  });
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeCreateForm
          projectID="project-1"
          sessionID="33333333-3333-4333-8333-333333333333"
          openSession={vi.fn()}
          navigator={createTestSidebarNavigator()}
          actions={actions}
        />
      </QueryClientProvider>
    </BrowserProviders>,
  );
  const input = await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") });
  fireEvent.change(input, { target: { value: "Test WT" } });
  await screen.findByText(appI18n.t("chat.worktree.targetInvalid"));
  expect(input).toHaveValue("Test WT");
  expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.createSubmit") }));
  expect(create).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: "Test-WT" } });
  await screen.findByText(appI18n.t("chat.worktree.newBranch"));
  expect(screen.queryByText(appI18n.t("chat.worktree.targetInvalid"))).not.toBeInTheDocument();
  expect(input).toHaveValue("Test-WT");
});

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
      sessionID: "33333333-3333-4333-8333-333333333333",
      onAccepted: vi.fn(),
      push: vi.fn(),
      t: appI18n.t,
      refreshOpenWorktreeList: vi.fn(),
    });
    render(
      <BrowserProviders services={services}>
        <QueryClientProvider client={client}>
          <WorktreeCreateForm
            projectID="project-1"
            openSession={vi.fn()}
            sessionID="33333333-3333-4333-8333-333333333333"
            navigator={createTestSidebarNavigator()}
            actions={actions}
          />
        </QueryClientProvider>
      </BrowserProviders>,
    );
    const user = userEvent.setup({ delay: null });
    const targetInput = await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.targetLabel") });
    await user.clear(targetInput);
    await user.type(targetInput, "topic");
    const baseInput = await screen.findByRole("textbox", { name: appI18n.t("chat.worktree.baseLabel") });
    await user.clear(baseInput);
    await user.type(baseInput, "custom-base");
    if (source === "list") {
      list.mockRejectedValueOnce(new Error("offline"));
      await act(async () => {
        await client.refetchQueries({
          queryKey: queryKeys.worktreeList("33333333-3333-4333-8333-333333333333"),
        });
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
    new WorktreeError(new RpcError({ code: "internal_failure", method: "create", message: "rejected" }), {
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
    expect(push).toHaveBeenCalledTimes(1);
  }
});

function WorktreeBrowser({
  onOpenSession = vi.fn(),
  ...props
}: Readonly<{ projectID: string; sessionID: string; onOpenSession?: (sessionID: string) => void }>) {
  const [navigator] = useState(() => createTestSidebarNavigator());
  return (
    <Browser
      {...props}
      onOpenSession={onOpenSession}
      navigator={navigator}
      onCreate={vi.fn()}
      onSwitch={vi.fn()}
      refreshOpenWorktreeList={vi.fn()}
    />
  );
}

it.each(["33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"])(
  "opens origin Session %s without navigating to itself",
  async (originSessionID) => {
    const user = userEvent.setup({ delay: null });
    const entry = worktreeBrowserFixtureEntry("registered", false);
    if (entry.topology?.topology.case !== "registered" || entry.topology.topology.value.kent === undefined)
      throw new Error("Expected registered fixture");
    entry.topology.topology.value.kent.originSessionId = originSessionID;
    const services = createTestServices([worktreeBrowserFixtureRoute([entry])]);
    const onOpenSession = vi.fn();
    render(
      <BrowserProviders services={services}>
        <WorktreeBrowser
          projectID="project-1"
          sessionID="33333333-3333-4333-8333-333333333333"
          onOpenSession={onOpenSession}
        />
      </BrowserProviders>,
    );
    await user.click(
      await screen.findByRole("button", {
        name: appI18n.t(
          originSessionID === "33333333-3333-4333-8333-333333333333"
            ? "chat.worktree.currentOriginSession"
            : "chat.worktree.openOriginSession",
          { sessionID: originSessionID },
        ),
      }),
    );
    if (originSessionID === "33333333-3333-4333-8333-333333333333")
      expect(onOpenSession).not.toHaveBeenCalled();
    else expect(onOpenSession).toHaveBeenCalledWith(originSessionID);
  },
);

it.each(["registered", "missing"] as const)("copies the full %s path from its subtitle", async (kind) => {
  const user = userEvent.setup({ delay: null });
  const entry = worktreeBrowserFixtureEntry(kind, true);
  const topology = entry.topology?.topology;
  if (topology?.case === "registered" && topology.value.kent !== undefined) {
    topology.value.kent.displayName = "feature";
  }
  const services = createTestServices([worktreeBrowserFixtureRoute([entry])]);
  render(
    <BrowserProviders services={services}>
      <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
    </BrowserProviders>,
  );
  const copy = await screen.findByRole("button", {
    name: appI18n.t("chat.worktree.copyPath", { path: "/repo/feature" }),
  });
  expect(copy).toHaveTextContent("/repo/feature");
  await user.click(copy);
  expect(await navigator.clipboard.readText()).toBe("/repo/feature");
});

it("opens with a fresh read even when the Session list is cached", async () => {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const cached = await services.api.listWorktrees("33333333-3333-4333-8333-333333333333");
  client.setQueryData(queryKeys.worktreeList("33333333-3333-4333-8333-333333333333"), cached);
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
      </QueryClientProvider>
    </BrowserProviders>,
  );
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(2);
  });
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  expect(
    client.getQueryData<WorktreeList>(queryKeys.worktreeList("33333333-3333-4333-8333-333333333333")),
  ).not.toBe(cached);
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
    ...worktreeCommandFixtureRoutes(),
  ]);
  const client = new QueryClient();
  const surface = (sessionID: string) => (
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser projectID="project-1" sessionID={sessionID} />
      </QueryClientProvider>
    </BrowserProviders>
  );
  const view = render(surface("33333333-3333-4333-8333-333333333333"));
  await waitFor(() => {
    expect(services.transport.descriptorCalls).toHaveLength(1);
  });
  view.rerender(surface("44444444-4444-4444-8444-444444444444"));
  await waitFor(() => {
    expect(
      client.getQueryData<WorktreeList>(queryKeys.worktreeList("44444444-4444-4444-8444-444444444444")),
    ).toBeDefined();
  });
  const current = client.getQueryData<WorktreeList>(
    queryKeys.worktreeList("44444444-4444-4444-8444-444444444444"),
  );
  await act(async () => {
    old.resolve(result);
    await old.promise;
  });
  expect(
    client.getQueryData<WorktreeList>(queryKeys.worktreeList("44444444-4444-4444-8444-444444444444")),
  ).toBe(current);
  expect(services.transport.descriptorCalls).toHaveLength(2);
  const preview = vi.spyOn(services.api, "previewWorktreeDelete");
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  await screen.findByRole("checkbox");
  expect(preview.mock.calls[0]?.[0]).toBe("44444444-4444-4444-8444-444444444444");
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
        {open ? (
          <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
        ) : null}
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
  const key = queryKeys.worktreeList("33333333-3333-4333-8333-333333333333");
  await waitFor(() => {
    expect(client.getQueryData<WorktreeList>(key)).toBeDefined();
  });
  const current = client.getQueryData<WorktreeList>(key);
  await act(async () => {
    old.resolve(result);
    await old.promise;
  });
  expect(client.getQueryData<WorktreeList>(key)).toBe(current);
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
      composer={() => (
        <WorktreeControl
          projectID={target.projectID}
          sessionID={target.sessionID}
          target={executionTarget}
          query={query}
          openSession={vi.fn()}
        />
      )}
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
          <WorktreeBrowser projectID={target.projectID} sessionID={target.sessionID} />
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
        projectID: target.projectID,
        openSession: vi.fn(),
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
          <WorktreeBrowser projectID={target.projectID} sessionID={target.sessionID} />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </BrowserProviders>,
  );
  await waitFor(() => {
    expect(client.isFetching()).toBe(0);
  });
  const handler = runtime.handlers[0];
  if (handler === undefined) throw new Error("Missing transcript observer");
  await act(async () => {
    handler.onOpen?.();
    handler.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
  });
  const before = services.transport.descriptorCalls.length;
  const key = queryKeys.chatMainView(target.sessionID);
  type MainView = Awaited<ReturnType<typeof runtime.api.getMainView>>["mainView"];
  await waitFor(() => {
    expect(client.getQueryData<MainView>(key)?.version.sequence).toBe(
      hydration().RuntimeReadModelUpdate.Version.Sequence,
    );
  });
  const executionTarget = client.getQueryData<MainView>(key)?.executionTarget;
  await act(async () => {
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
  expect(client.getQueryData<MainView>(key)?.sessionID).toBe(target.sessionID);
  expect(client.getQueryData<MainView>(key)?.executionTarget).toEqual(executionTarget);
  await act(async () => {
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

it("Refresh retains visible rows on failure and reports it with a toast", async () => {
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
  const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  const client = new QueryClient();
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
      </QueryClientProvider>
    </BrowserProviders>,
  );
  const key = queryKeys.worktreeList("33333333-3333-4333-8333-333333333333");
  await waitFor(() => {
    expect(client.getQueryData<WorktreeList>(key)).toBeDefined();
  });
  const completed = client.getQueryData<WorktreeList>(key);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.refresh") }));
  expect(client.getQueryState(key)?.fetchStatus).toBe("fetching");
  expect(client.getQueryData<WorktreeList>(key)).toBe(completed);
  expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") })).toBeEnabled();
  await act(async () => {
    refreshing.reject(new Error("Read failed"));
    await refreshing.promise.catch(() => undefined);
  });
  await waitFor(() => {
    expect(client.getQueryState(key)?.status).toBe("error");
  });
  expect(client.getQueryData<WorktreeList>(key)).toBe(completed);
  expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") })).toBeEnabled();
  expect(notice).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.refresh") }));
  await waitFor(() => {
    expect(client.getQueryState(key)?.status).toBe("success");
  });
  expect(client.getQueryData<WorktreeList>(key)).not.toBe(completed);
  expect(services.transport.descriptorCalls).toHaveLength(3);
  notice.mockRestore();
});

it("reports failed deletion preview with a toast and permits reopening without changing the list", async () => {
  const services = createTestServices([
    worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
    ...worktreeCommandFixtureRoutes(),
  ]);
  const preview = vi.spyOn(services.api, "previewWorktreeDelete").mockRejectedValueOnce(new Error("offline"));
  const remove = vi.spyOn(services.api, "deleteWorktree");
  const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  render(
    <BrowserProviders services={services}>
      <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
    </BrowserProviders>,
  );
  const user = userEvent.setup({ delay: null });
  await user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  await waitFor(() => {
    expect(notice).toHaveBeenCalledTimes(1);
  });
  expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  expect(await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") })).toBeEnabled();
  expect(preview).toHaveBeenCalledTimes(2);
  expect(preview.mock.calls[1]).toEqual(preview.mock.calls[0]);
  expect(remove).not.toHaveBeenCalled();
  notice.mockRestore();
});

it("keeps Worktree rows mounted while opening a deletion preview", async () => {
  const services = createTestServices([
    worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
    ...worktreeCommandFixtureRoutes(),
  ]);
  const previewReady = deferred<undefined>();
  const readPreview = services.api.previewWorktreeDelete.bind(services.api);
  vi.spyOn(services.api, "previewWorktreeDelete").mockImplementation(async (...args) => {
    await previewReady.promise;
    return readPreview(...args);
  });
  render(
    <BrowserProviders services={services}>
      <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
    </BrowserProviders>,
  );
  await screen.findByRole("button", { name: appI18n.t("chat.worktree.switch") });
  const path = screen.getByRole("button", {
    name: appI18n.t("chat.worktree.copyPath", { path: "/repo/feature" }),
  });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  expect(screen.getByTestId("spinner")).toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  await act(async () => {
    previewReady.resolve(undefined);
  });
  await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") });
  expect(
    screen.getByRole("button", { name: appI18n.t("chat.worktree.copyPath", { path: "/repo/feature" }) }),
  ).toBe(path);
});

it("keeps Include branch only for the current list destination across different deletion selections", async () => {
  const first = worktreeBrowserFixtureEntry("registered", false);
  const second = structuredClone(first);
  if (second.projection?.deletePreview === undefined) throw new Error("Expected deletion selection");
  if (
    second.projection.switch === undefined ||
    second.topology?.topology.case !== "registered" ||
    second.topology.topology.value.kent === undefined
  )
    throw new Error("Expected registered Worktree");
  second.projection.selector = "second";
  second.projection.switch.selector = "second";
  second.projection.deletePreview.selector = "worktree-2";
  second.topology.topology.value.kent.worktreeId = "worktree-2";
  const services = createTestServices([
    worktreeBrowserFixtureRoute([first, second]),
    ...worktreeCommandFixtureRoutes(),
  ]);
  const preview = vi.spyOn(services.api, "previewWorktreeDelete");
  const remove = vi.spyOn(services.api, "deleteWorktree").mockRejectedValue(new Error("offline"));
  const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  const destination = (key: string) => (
    <BrowserProviders services={services}>
      <WorktreeBrowser key={key} projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
    </BrowserProviders>
  );
  const view = render(destination("first"));
  const firstDelete = (await screen.findAllByRole("button", { name: appI18n.t("chat.worktree.delete") }))[0];
  if (firstDelete === undefined) throw new Error("Expected first deletion action");
  fireEvent.click(firstDelete);
  const checkbox = await screen.findByRole("checkbox");
  expect(checkbox).toBeChecked();
  fireEvent.click(checkbox);
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
  await waitFor(() => {
    expect(remove).toHaveBeenCalledTimes(1);
  });
  expect(remove.mock.calls[0]?.[2]).toBe("confirm_without_branch");
  await waitFor(() => {
    expect(screen.queryByRole("checkbox") === null).toBe(true);
  });
  const secondDelete = screen.getAllByRole("button", { name: appI18n.t("chat.worktree.delete") })[1];
  if (secondDelete === undefined) throw new Error("Expected second deletion action");
  fireEvent.click(secondDelete);
  expect(await screen.findByRole("checkbox")).not.toBeChecked();
  expect(preview.mock.calls[1]?.[1]).toBe("worktree-2");
  view.rerender(destination("next"));
  const reopenedDelete = (
    await screen.findAllByRole("button", { name: appI18n.t("chat.worktree.delete") })
  )[0];
  if (reopenedDelete === undefined) throw new Error("Expected new destination deletion action");
  fireEvent.click(reopenedDelete);
  expect(await screen.findByRole("checkbox")).toBeChecked();
  expect(remove).toHaveBeenCalledTimes(1);
  notice.mockRestore();
});

it("closes confirmation eagerly and keeps deletion loading local to its row through refresh", async () => {
  const entry = worktreeBrowserFixtureEntry("registered", false);
  const services = createTestServices([
    worktreeBrowserFixtureRoute([entry, worktreeBrowserFixtureEntry("mainWorkspace", false)]),
    ...worktreeCommandFixtureRoutes(),
  ]);
  const client = new QueryClient();
  const removal = deferred<Awaited<ReturnType<ApiService["deleteWorktree"]>>>();
  const refreshed = deferred<Awaited<ReturnType<ApiService["listWorktrees"]>>>();
  const remove = vi.spyOn(services.api, "deleteWorktree").mockReturnValue(removal.promise);
  const navigator = createTestSidebarNavigator();
  render(
    <BrowserProviders services={services}>
      <QueryClientProvider client={client}>
        <Browser
          projectID="project-1"
          sessionID="33333333-3333-4333-8333-333333333333"
          navigator={navigator}
          onCreate={vi.fn()}
          onSwitch={vi.fn()}
          onOpenSession={vi.fn()}
          refreshOpenWorktreeList={async () => {
            await replaceWorktreeListRead(client, services.api, "33333333-3333-4333-8333-333333333333");
          }}
        />
      </QueryClientProvider>
    </BrowserProviders>,
  );
  const switches = await screen.findAllByRole("button", { name: appI18n.t("chat.worktree.switch") });
  const unaffected = switches[1];
  if (unaffected === undefined) throw new Error("Expected second Worktree");
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  const confirm = await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") });
  await waitFor(() => expect(confirm).toBeEnabled());
  const path = screen.getAllByRole("button", {
    name: appI18n.t("chat.worktree.copyPath", { path: "/repo/feature" }),
  })[0];
  if (path === undefined) throw new Error("Expected Worktree path");
  const list = vi.spyOn(services.api, "listWorktrees").mockReturnValue(refreshed.promise);
  fireEvent.click(confirm);
  await waitFor(() => {
    expect(remove).toHaveBeenCalledTimes(1);
  });
  expect(screen.getAllByTestId("spinner")).toHaveLength(1);
  expect(screen.getAllByRole("button", { name: appI18n.t("chat.worktree.switch") })).toEqual([unaffected]);
  await waitFor(() => {
    expect(screen.queryByRole("button", { name: appI18n.t("chat.worktree.confirm") }) === null).toBe(true);
  });
  await act(async () => {
    removal.resolve({ $typeName: "kent.api.worktree.DeleteSuccess" });
  });
  await waitFor(() => {
    expect(list).toHaveBeenCalledTimes(1);
  });
  expect(screen.getAllByTestId("spinner")).toHaveLength(1);
  expect(unaffected).toBeInTheDocument();
  await act(async () => {
    const previous = client.getQueryData<WorktreeList>(
      queryKeys.worktreeList("33333333-3333-4333-8333-333333333333"),
    );
    const remaining = previous?.worktrees[1];
    if (previous === undefined || remaining === undefined) throw new Error("Expected loaded Worktrees");
    refreshed.resolve({ ...previous, worktrees: [remaining] });
  });
  await waitFor(() => expect(path).not.toBeInTheDocument());
  expect(screen.getByRole("button", { name: appI18n.t("chat.worktree.switch") })).toBe(unaffected);
});

it("finishes an accepted deletion and its refresh after leaving the list destination", async () => {
  const services = createTestServices([
    worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
    ...worktreeCommandFixtureRoutes(),
  ]);
  const removal = deferred<Awaited<ReturnType<ApiService["deleteWorktree"]>>>();
  const remove = vi.spyOn(services.api, "deleteWorktree").mockReturnValue(removal.promise);
  const refresh = vi.fn();
  const view = render(
    <BrowserProviders services={services}>
      <Browser
        projectID="project-1"
        sessionID="33333333-3333-4333-8333-333333333333"
        navigator={createTestSidebarNavigator()}
        onCreate={vi.fn()}
        onSwitch={vi.fn()}
        onOpenSession={vi.fn()}
        refreshOpenWorktreeList={refresh}
      />
    </BrowserProviders>,
  );
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
  await waitFor(() => {
    expect(remove).toHaveBeenCalledTimes(1);
  });
  view.unmount();
  await act(async () => {
    removal.resolve({ $typeName: "kent.api.worktree.DeleteSuccess" });
  });
  await waitFor(() => {
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  expect(refresh).toHaveBeenCalledWith("33333333-3333-4333-8333-333333333333");
});

it.each(["operational", "precondition"] as const)(
  "closes deletion confirmation on %s rejection without rereading or replaying",
  async (kind) => {
    const services = createTestServices([
      worktreeBrowserFixtureRoute([worktreeBrowserFixtureEntry("registered", false)]),
      ...worktreeCommandFixtureRoutes(),
    ]);
    const preview = vi.spyOn(services.api, "previewWorktreeDelete");
    const remove = vi.spyOn(services.api, "deleteWorktree").mockRejectedValue(
      kind === "operational"
        ? new Error("offline")
        : new WorktreeError(
            new RpcError({ code: "internal_failure", method: "delete", message: "changed" }),
            {
              kind: "delete_precondition",
              details: {
                $typeName: "kent.api.worktree.DeletePreconditionDetails",
              },
            },
          ),
    );
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    render(
      <BrowserProviders services={services}>
        <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
      </BrowserProviders>,
    );
    const user = userEvent.setup({ delay: null });
    await user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.delete") }));
    await user.click(await screen.findByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
    await waitFor(() => {
      expect(notice).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: appI18n.t("chat.worktree.confirm") }) === null).toBe(true);
    });
    expect(preview).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledTimes(1);
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
          <WorktreeBrowser projectID="project-1" sessionID="33333333-3333-4333-8333-333333333333" />
        </QueryClientProvider>
      </BrowserProviders>,
    );
    await waitFor(() => {
      expect(
        client.getQueryState(queryKeys.worktreeList("33333333-3333-4333-8333-333333333333"))?.status,
      ).toBe("success");
    });
    const switchButtons = screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.switch") });
    const deleteButtons = screen.queryAllByRole("button", { name: appI18n.t("chat.worktree.delete") });
    expect(switchButtons).toHaveLength(switches);
    expect(deleteButtons).toHaveLength(deletes);
    expect(services.transport.descriptorCalls).toHaveLength(1);
  },
);
