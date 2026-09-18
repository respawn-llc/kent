import { createChatStorageFixture } from "./chatStorageFixture";
import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import { RegistryProvider } from "@effect/atom-react";
import type { ReactNode } from "react";

import {
  parsePendingWorkItemID,
  type ChatInputMutationResult,
  type ChatSettingsRead,
  type InitialChatSettings,
} from "@/api";
import { createTestServices, TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import {
  deferred,
  hydration,
  mainViewRead,
  target as sessionTarget,
  transcriptPage,
} from "@/test-support/chat-runtime";
import { question, approval } from "@/test-support/chat-prompts";
import { approvalDecisionLabel } from "@/shared/prompt-presentation";
import {
  queryKeys,
  ChatPromptPresenceProvider,
  readBrowserStorage,
  writeBrowserStorage,
  SidebarRootContext,
  SidebarRootOwner,
} from "@/app-facade";
import { createTestSidebarController } from "@/test-support/sidebar";
import { useChatDestination } from "./useChatDestination";
import { ChatDestination } from "./ChatDestination";
import type { ChatDestinationOpening } from "./ChatDestinationViewModel";
import { appI18n } from "@/i18n";
import type { ComposerCommand } from "./composerCommands";
import type { ChatTranscriptHandler, PromptAnswerBatchResponse } from "@/api";

const opening = {
  kind: "new_chat",
  projectID: "project-1",
  workspace: { id: "workspace-1", name: "Default", rootPath: "/default", isDefault: true },
} as const;
const newChatTarget = {
  kind: "new_chat",
  projectID: opening.projectID,
  workspace: { workspaceID: opening.workspace.id },
} as const;
const baseline: InitialChatSettings = {
  agentRole: "default",
  supervisor: "edits",
  thinking: null,
  fast: null,
  questionsEnabled: true,
  autoCompactionEnabled: true,
};
const catalog: Extract<ChatSettingsRead, { kind: "new_chat" }> = {
  kind: "new_chat",
  initialSettings: baseline,
  catalog: {
    choices: [
      {
        agent: {
          role: "default",
          model: "local",
          thinking: "none",
          tools: [],
          customSystemPrompt: false,
          customCapabilities: false,
          agentCallable: true,
        },
        baseline,
        supervisor: { value: "edits", baseline: "edits", editability: { kind: "editable" } },
        thinking: { kind: "unsupported" },
        fast: { kind: "unsupported" },
        questions: { capable: true, enabled: true, editability: { kind: "editable" } },
        autoCompaction: {
          policy: "optional",
          stored: true,
          effective: true,
          editability: { kind: "editable" },
        },
      },
    ],
  },
};
const navigation = { openTask: vi.fn(), openParentSession: vi.fn() };
beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());
function setup(commands: readonly ComposerCommand[] = []) {
  writeBrowserStorage("local", "desktop.newChatDraft", "");
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getSettings").mockResolvedValue(catalog);
  const draft = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const read = vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("");
  const observe = vi.spyOn(services.api.chat, "getMainView");
  const steer = vi.spyOn(services.api.chat, "steer");
  const queue = vi.spyOn(services.api.chat, "queue");
  const goal = vi.spyOn(services.api.chat, "setGoal");
  const compact = vi.spyOn(services.api.chat, "compact");
  vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items: [] });
  const view = renderHook(() => useChatDestination({ opening, navigation, commands }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <RegistryProvider>
        <TestAppProviders services={services}>
          <SidebarRootContext.Provider value={createTestSidebarController()}>
            <SidebarRootOwner>{children}</SidebarRootOwner>
          </SidebarRootContext.Provider>
        </TestAppProviders>
      </RegistryProvider>
    ),
  });
  return { ...view, services, draft, read, observe, steer, queue, goal, compact };
}

function renderDestination(services: TestAppServices, client: QueryClient, opening: ChatDestinationOpening) {
  const router = createRouter({ history: createMemoryHistory(), routeTree: createRootRoute() });
  return render(
    <RouterContextProvider router={router}>
      <TestAppProviders services={services} queryClient={client}>
        <SidebarRootContext.Provider value={createTestSidebarController()}>
          <SidebarRootOwner>
            <ChatPromptPresenceProvider>
              <ChatDestination opening={opening} navigation={navigation} />
            </ChatPromptPresenceProvider>
          </SidebarRootOwner>
        </SidebarRootContext.Provider>
      </TestAppProviders>
    </RouterContextProvider>,
  );
}

function sessionWithPrompts() {
  const services = createTestServices([]);
  const choice = catalog.catalog.choices[0];
  if (choice === undefined) throw new Error("Missing settings choice");
  const settings = vi.spyOn(services.api.chat, "getSettings").mockResolvedValue({
    kind: "session",
    session: { sessionID: sessionTarget.sessionID, previousSessionID: null, task: null },
    settings: {
      selectedAgent: { role: choice.agent.role, model: choice.agent.model, thinking: choice.agent.thinking },
      agentChoices: [choice.agent],
      agentEditability: { kind: "editable" },
      agentLocked: false,
      cachingLocked: false,
      workflowLocked: false,
      supervisor: choice.supervisor,
      thinking: choice.thinking,
      fast: choice.fast,
      questions: choice.questions,
      autoCompaction: choice.autoCompaction,
    },
  });
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("");
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api.chat, "getMainView").mockResolvedValue(mainViewRead());
  vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
  const pending = vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items: [] });
  const handlers: ChatTranscriptHandler[] = [];
  vi.spyOn(services.api.chat, "subscribeTranscript").mockImplementation((_target, handler) => {
    handlers.push(handler);
    return { close: vi.fn() };
  });
  const response = deferred<PromptAnswerBatchResponse>();
  const answer = vi.spyOn(services.api.chat, "answerPromptBatch").mockReturnValue(response.promise);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderDestination(services, client, { kind: "session", ...sessionTarget });
  return { handlers, settings, pending, answer, client, response };
}

it("retains pending prompt answers and commentary through a Settings read failure without replay", async () => {
  const view = sessionWithPrompts();
  const prompts = [question(), approval(), question("last", { createdAt: "2026-09-12T00:00:02.000Z" })];
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  act(() =>
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: prompts.map((prompt) => ({ state: "pending", prompt })) },
    }),
  );
  const user = userEvent.setup();
  await user.type(await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }), "first answer");
  await user.click(screen.getByRole("radio", { name: "one" }));
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.picker.decline") }));
  await user.type(screen.getByRole("textbox", { name: appI18n.t("task.commentary") }), "last draft");
  view.settings.mockRejectedValueOnce(new Error("offline"));
  await act(async () => {
    await view.client.refetchQueries({ queryKey: ["chat-settings"] });
  });
  expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByDisplayValue("last draft")).toBeInTheDocument();
  expect(view.answer).not.toHaveBeenCalled();
  await user.click(screen.getByRole("radio", { name: "two" }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledOnce();
  });
  expect(view.answer.mock.calls[0]?.[0].entries).toMatchObject([
    {
      toolCallID: prompts[0]?.toolCallID,
      kind: "question",
      selectedOptionNumber: 1,
      freeform: "first answer",
    },
    { toolCallID: prompts[1]?.toolCallID, kind: "declined" },
    { toolCallID: prompts[2]?.toolCallID, kind: "question", selectedOptionNumber: 2, freeform: "last draft" },
  ]);
});

it("keeps an admitted prompt submission pending through Pending Work read recovery", async () => {
  const view = sessionWithPrompts();
  const prompt = approval();
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  act(() =>
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    }),
  );
  const user = userEvent.setup();
  await user.type(
    await screen.findByRole("textbox", { name: appI18n.t("task.commentary") }),
    "approval commentary",
  );
  const option = approvalDecisionLabel("allow_once", appI18n.t);
  await user.click(screen.getByRole("radio", { name: option }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledOnce();
  });
  const reads = view.pending.mock.calls.length;
  view.pending.mockRejectedValueOnce(new Error("offline"));
  await act(async () => {
    await view.client.refetchQueries({ queryKey: ["chat-composer-pending"], type: "active" });
  });
  expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByRole("radio", { name: option })).toBeDisabled();
  expect(screen.getByDisplayValue("approval commentary")).toHaveAttribute("readonly");
  expect(view.pending).toHaveBeenCalledTimes(reads + 2);
  expect(view.answer).toHaveBeenCalledOnce();
  await act(async () => {
    view.response.resolve({ results: [{ toolCallID: prompt.toolCallID, outcome: "resolved" }] });
  });
  await waitFor(() => expect(screen.queryByRole("radio", { name: option })).not.toBeInTheDocument());
  expect(view.answer).toHaveBeenCalledOnce();
});

it.each(["settings", "workspace"] as const)(
  "replaces mounted Chat on cached %s failure and restores its unsent input after Retry",
  async (source) => {
    const services = createTestServices([]);
    const settings = vi.spyOn(services.api.chat, "getSettings").mockResolvedValue(catalog);
    const workspaces = vi.spyOn(services.api, "listWorkspaces").mockResolvedValue({
      projectID: opening.projectID,
      offset: 0,
      workspaces: [opening.workspace],
      nextOffset: null,
    });
    const read = source === "settings" ? settings : workspaces;
    const send = vi.spyOn(services.api.chat, "steer");
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderDestination(services, client, opening);
    const user = userEvent.setup();
    await user.type(await screen.findByRole("textbox"), "unsent input");
    if (source === "workspace") {
      await user.click(screen.getByRole("button", { name: opening.workspace.name }));
      await waitFor(() => {
        expect(workspaces).toHaveBeenCalledOnce();
      });
    }
    read.mockRejectedValueOnce(new Error("offline"));
    await act(async () => {
      await client.refetchQueries({
        queryKey:
          source === "settings" ? ["chat-settings"] : queryKeys.projectWorkspaceCatalog(opening.projectID),
      });
    });
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
    expect(await screen.findByDisplayValue("unsent input")).toBeInTheDocument();
    expect(read).toHaveBeenCalledTimes(3);
    expect(send).not.toHaveBeenCalled();
  },
);

it("keeps Workspace opening failure owned by Chat and retries with selection and input retained", async () => {
  const view = setup();
  const read = vi
    .spyOn(view.services.api, "listWorkspaces")
    .mockRejectedValueOnce(new Error("offline"))
    .mockResolvedValue({
      projectID: opening.projectID,
      offset: 0,
      workspaces: [opening.workspace],
      nextOffset: null,
    });
  expect(read).not.toHaveBeenCalled();
  act(() => {
    view.result.current.composer.edit("keep");
    view.result.current.setWorkspaceOpen(true);
  });
  await waitFor(() => {
    expect(view.result.current.workspaceCatalog.isError).toBe(true);
  });
  await act(async () => {
    await view.result.current.workspaceCatalog.refetch();
  });
  await waitFor(() => {
    expect(view.result.current.workspaceCatalog.isSuccess).toBe(true);
  });
  expect(view.result.current.workspaceOpen).toBe(true);
  expect(view.result.current.workspace).toEqual(opening.workspace);
  expect(view.result.current.composer.text).toBe("keep");
  expect(read).toHaveBeenCalledTimes(2);
  expect(view.steer).not.toHaveBeenCalled();
});

it("retains Workspace rows on directional failure and retries only that page", async () => {
  const view = setup();
  const first = { projectID: opening.projectID, offset: 0, workspaces: [opening.workspace], nextOffset: 100 };
  const read = vi
    .spyOn(view.services.api, "listWorkspaces")
    .mockResolvedValueOnce(first)
    .mockRejectedValueOnce(new Error("offline"))
    .mockResolvedValue({ projectID: opening.projectID, offset: 100, workspaces: [], nextOffset: null });
  act(() => {
    view.result.current.setWorkspaceOpen(true);
  });
  await waitFor(() => {
    expect(view.result.current.workspaceCatalog.isSuccess).toBe(true);
  });
  await act(async () => {
    await view.result.current.workspaceCatalog.fetchNextPage();
  });
  await waitFor(() => {
    expect(view.result.current.workspaceCatalog.isFetchNextPageError).toBe(true);
  });
  expect(view.result.current.workspaceCatalog.data?.pages).toEqual([first]);
  await act(async () => {
    await view.result.current.workspaceCatalog.fetchNextPage();
  });
  await waitFor(() => {
    expect(view.result.current.workspaceCatalog.isSuccess).toBe(true);
  });
  expect(read.mock.calls).toEqual([
    [opening.projectID, 0],
    [opening.projectID, 100],
    [opening.projectID, 100],
  ]);
  expect(view.steer).not.toHaveBeenCalled();
});

it("keeps opening, text, settings edits and abandonment entirely client-only", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("unsent");
    const settings = view.result.current.settings;
    if (settings.kind !== "ready-new-chat") throw new Error("Settings unavailable");
    settings.activate({ kind: "supervisor", value: "off" });
  });
  expect(view.result.current.composer.text).toBe("unsent");
  expect(view.result.current.target).toEqual(newChatTarget);
  view.unmount();
  for (const operation of [
    view.draft,
    view.read,
    view.observe,
    view.steer,
    view.queue,
    view.goal,
    view.compact,
  ])
    expect(operation).not.toHaveBeenCalled();
});

it.each(["registered", "compact"])("adopts the identified rejection of a %s command", async (kind) => {
  const view = setup([
    {
      token: "/review",
      aliases: [],
      description: null,
      preview: null,
      execution: { kind: "prompt", catalogIdentity: "test:review" },
    },
  ]);
  const rejected = {
    sessionID: "command-session",
    outcome: { kind: "not_accepted", reason: { kind: "too_soon" } },
  } as const;
  view.steer.mockResolvedValue(rejected);
  view.compact.mockResolvedValue(rejected);
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  const text = kind === "registered" ? "/review\tchanges" : "/compact keep decisions";
  act(() => {
    view.result.current.composer.edit(text);
  });
  await act(async () => {
    view.result.current.composer.submit("send");
  });
  await waitFor(() => {
    expect(view.result.current.target).toMatchObject({ kind: "session", sessionID: "command-session" });
  });
  expect(view.result.current.composer.text).toBe(text);
  if (kind === "registered")
    expect(view.steer).toHaveBeenCalledWith(expect.objectContaining({ kind: "new_chat" }), {
      kind: "command",
      catalogIdentity: "test:review",
      token: "/review",
      separatorWhitespace: "\t",
      arguments: "changes",
    });
  else
    expect(view.compact).toHaveBeenCalledWith(expect.objectContaining({ kind: "new_chat" }), {
      token: "/compact",
      separatorWhitespace: " ",
      rawGuidance: "keep decisions",
    });
});

it("adopts accepted Send in place and saves only the visible unsent editor to its Session", async () => {
  const view = setup();
  const request = deferred<ChatInputMutationResult>();
  view.steer.mockReturnValue(request.promise);
  await waitFor(() => {
    expect(view.result.current.composer.draft.kind).toBe("ready");
  });
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("submitted");
  });
  act(() => {
    view.result.current.composer.submit("send");
  });
  expect(view.result.current.composer.text).toBe("");
  act(() => {
    view.result.current.composer.edit("later typing");
  });
  await act(async () => {
    request.resolve({
      sessionID: "session-created",
      outcome: {
        kind: "accepted",
        queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
        diagnostic: null,
      },
    });
  });
  expect(view.result.current.target).toEqual({
    kind: "session",
    projectID: opening.projectID,
    sessionID: "session-created",
  });
  expect(view.result.current.composer.text).toBe("later typing");
  await waitFor(() => {
    expect(view.draft).toHaveBeenCalledWith(
      expect.objectContaining({ sessionID: "session-created" }),
      "later typing",
    );
  });
  expect(view.read).not.toHaveBeenCalled();
});

it("adopts an identified rejection and restores submitted text once without comparing its content", async () => {
  const view = setup();
  const request = deferred<ChatInputMutationResult>();
  view.queue.mockReturnValue(request.promise);
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("repeat");
  });
  act(() => {
    view.result.current.composer.submit("queue");
  });
  act(() => {
    view.result.current.composer.edit("repeat");
  });
  await act(async () => {
    request.resolve({
      sessionID: "rejected-session",
      outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
    });
  });
  expect(view.result.current.target).toMatchObject({ kind: "session", sessionID: "rejected-session" });
  expect(view.result.current.composer.text).toBe("repeat\nrepeat");
  await waitFor(() => {
    expect(view.draft).toHaveBeenCalledWith(
      expect.objectContaining({ sessionID: "rejected-session" }),
      "repeat\nrepeat",
    );
  });
  expect(view.read).not.toHaveBeenCalled();
});

it.each(["creation failed", "response lost"])(
  "retains New Chat text without discovery or replay when %s",
  async (message) => {
    const view = setup();
    const request = deferred<ChatInputMutationResult>();
    view.steer.mockReturnValue(request.promise);
    await waitFor(() => {
      expect(view.result.current.settings.kind).toBe("ready-new-chat");
    });
    act(() => {
      view.result.current.composer.edit("original");
    });
    act(() => {
      view.result.current.composer.submit("send");
    });
    expect(readBrowserStorage("local", "desktop.newChatDraft")).toEqual({ ok: true, value: "original" });
    act(() => {
      view.result.current.composer.edit("later");
    });
    await act(async () => {
      request.reject(new Error(message));
    });
    expect(view.result.current.target).toEqual(newChatTarget);
    expect(view.result.current.composer.text).toBe("later\noriginal");
    expect(readBrowserStorage("local", "desktop.newChatDraft")).toEqual({
      ok: true,
      value: "later\noriginal",
    });
    expect(view.steer).toHaveBeenCalledOnce();
    expect(view.draft).not.toHaveBeenCalled();
    expect(view.services.transport.calls).toHaveLength(0);
    expect(view.services.transport.descriptorCalls).toHaveLength(0);
  },
);

it("applies concurrent results in delivery order while draft writes retain captured targets", async () => {
  const view = setup();
  const first = deferred<ChatInputMutationResult>();
  const second = deferred<ChatInputMutationResult>();
  const firstSave = deferred<undefined>();
  view.steer.mockReturnValue(first.promise);
  view.queue.mockReturnValue(second.promise);
  view.draft.mockReturnValueOnce(firstSave.promise);
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("first");
  });
  act(() => {
    view.result.current.composer.submit("send");
  });
  act(() => {
    view.result.current.composer.edit("second");
  });
  act(() => {
    view.result.current.composer.submit("queue");
  });
  act(() => {
    view.result.current.composer.edit("during");
  });
  await act(async () => {
    second.resolve({
      sessionID: "second",
      outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
    });
  });
  expect(view.result.current.target).toMatchObject({ sessionID: "second" });
  expect(view.result.current.composer.text).toBe("during\nsecond");
  await act(async () => {
    first.resolve({
      sessionID: "first",
      outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
    });
  });
  expect(view.result.current.target).toMatchObject({ sessionID: "first" });
  expect(view.result.current.composer.text).toBe("during\nsecond\nfirst");
  expect(view.draft).toHaveBeenCalledTimes(1);
  await act(async () => {
    firstSave.resolve(undefined);
  });
  await waitFor(() => {
    expect(view.draft).toHaveBeenCalledWith(
      expect.objectContaining({ sessionID: "first" }),
      "during\nsecond\nfirst",
    );
  });
  expect(view.draft.mock.calls[0]).toEqual([
    expect.objectContaining({ sessionID: "second" }),
    "during\nsecond",
  ]);
});

it("restores a late creation failure into the Session already adopted by another action", async () => {
  const view = setup();
  const first = deferred<ChatInputMutationResult>();
  const second = deferred<ChatInputMutationResult>();
  view.steer.mockReturnValue(first.promise);
  view.queue.mockReturnValue(second.promise);
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("first");
  });
  act(() => {
    view.result.current.composer.submit("send");
  });
  act(() => {
    view.result.current.composer.edit("second");
  });
  act(() => {
    view.result.current.composer.submit("queue");
  });
  await act(async () => {
    second.resolve({
      sessionID: "second",
      outcome: {
        kind: "accepted",
        queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
        diagnostic: null,
      },
    });
  });
  await act(async () => {
    first.reject(new Error("lost"));
  });
  expect(view.result.current.target).toMatchObject({ kind: "session", sessionID: "second" });
  expect(view.result.current.composer.text).toBe("first");
  expect(readBrowserStorage("local", "desktop.newChatDraft")).toEqual({ ok: true, value: "" });
});

it("keeps text but replaces edited settings when selecting another workspace before dispatch", async () => {
  const view = setup();
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("keep me");
    const settings = view.result.current.settings;
    if (settings.kind !== "ready-new-chat") throw new Error("Settings unavailable");
    settings.activate({ kind: "supervisor", value: "off" });
  });
  const changed = { ...baseline, agentRole: "other", supervisor: "all" as const };
  const choice = catalog.catalog.choices[0];
  if (choice === undefined) throw new Error("Missing choice");
  const loading = deferred<ChatSettingsRead>();
  vi.mocked(view.services.api.chat.getSettings).mockReturnValueOnce(loading.promise);
  act(() => {
    view.result.current.selectWorkspace({
      id: "workspace-2",
      name: "Second",
      rootPath: "/second",
      isDefault: false,
    });
  });
  expect(view.result.current.composer.text).toBe("keep me");
  expect(view.result.current.composer.canSubmit).toBe(false);
  await act(async () => {
    loading.resolve({
      kind: "new_chat",
      initialSettings: changed,
      catalog: { choices: [{ ...choice, agent: { ...choice.agent, role: "other" }, baseline: changed }] },
    });
  });
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  expect(view.result.current.composer.canSubmit).toBe(true);
  view.steer.mockReturnValue(deferred<ChatInputMutationResult>().promise);
  act(() => {
    view.result.current.composer.submit("send");
  });
  await waitFor(() => {
    expect(view.steer).toHaveBeenCalledWith(
      {
        kind: "new_chat",
        projectID: opening.projectID,
        workspace: { workspaceID: "workspace-2" },
        initialSettings: changed,
      },
      { kind: "text", text: "keep me" },
    );
  });
});

it("rejects a previously rendered workspace action while any first input request is pending", async () => {
  const view = setup();
  view.steer.mockReturnValue(deferred<ChatInputMutationResult>().promise);
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  const selectWorkspace = view.result.current.selectWorkspace;
  act(() => {
    view.result.current.composer.edit("pending input");
  });
  act(() => {
    view.result.current.composer.submit("send");
  });
  await waitFor(() => {
    expect(view.steer).toHaveBeenCalledOnce();
  });
  act(() => {
    selectWorkspace({ id: "workspace-2", name: "Second", rootPath: "/second", isDefault: false });
  });
  expect(view.result.current.target).toEqual(newChatTarget);
});
