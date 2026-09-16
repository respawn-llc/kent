import { act, renderHook, waitFor } from "@testing-library/react";
import { RegistryProvider } from "@effect/atom-react";
import type { ReactNode } from "react";

import {
  parsePendingWorkItemID,
  type ChatInputMutationResult,
  type ChatSettingsRead,
  type InitialChatSettings,
} from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { readBrowserStorage, writeBrowserStorage, SidebarRootContext, SidebarRootOwner } from "@/app-facade";
import { createTestSidebarController } from "@/test-support/sidebar";
import { useChatDestination } from "./useChatDestination";
import type { ComposerCommand } from "./composerCommands";

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
const catalog: ChatSettingsRead = {
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
beforeEach(() => {
  const values = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return values.size;
    },
    clear: () => {
      values.clear();
    },
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
    key: (index) => Array.from(values.keys())[index] ?? null,
  };
  vi.stubGlobal("localStorage", storage);
});
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
