import { createChatStorageFixture } from "./chatStorageFixture";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { SidebarHeaderActionProvider, SidebarHeaderActionSlot } from "@/app-facade";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { WorktreeBrowser } from "./WorktreeBrowser";

import { parsePendingWorkItemID, type ChatInputMutationResult, type ChatSettingsRead } from "@/api";
import { createTestServices } from "@/test-support/app-services";
import {
  changedIdentity,
  deferred,
  hydration,
  target as sessionTarget,
  transcriptPage,
} from "@/test-support/chat-runtime";
import { question, approval } from "@/test-support/chat-prompts";
import { approvalDecisionLabel } from "@/shared/prompt-presentation";
import { row } from "@/test-support/transcript-window";
import { appI18n } from "@/i18n";

import {
  opening,
  catalog,
  navigation,
  renderDestination,
  sessionWithPrompts,
} from "@/test-support/chat-destination";
beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

function ContextualWorktrees() {
  const [open, setOpen] = useState(false);
  const [navigator] = useState(() => createTestSidebarNavigator());
  return (
    <>
      <button
        onClick={() => {
          setOpen(!open);
        }}
      >
        Toggle contextual Worktrees
      </button>
      {open && (
        <SidebarHeaderActionProvider>
          <SidebarHeaderActionSlot />
          <WorktreeBrowser
            sessionID={sessionTarget.sessionID}
            navigator={navigator}
            onCreate={vi.fn()}
            onSwitch={vi.fn()}
            refreshOpenWorktreeList={vi.fn()}
          />
        </SidebarHeaderActionProvider>
      )}
    </>
  );
}

it("keeps transcript and composer usable through a sidebar-owned Worktree refresh failure", async () => {
  const view = sessionWithPrompts(undefined, { kind: "session", ...sessionTarget }, <ContextualWorktrees />);
  const list = vi.spyOn(view.services.api, "listWorktrees");
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "retained input");
  await waitFor(() => {
    expect(view.client.isFetching()).toBe(0);
  });
  await act(async () =>
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...hydration(),
        TailSegment: { OlderCursor: null, HasMoreAbove: false, Entries: [row(1)] },
      },
    }),
  );
  await user.click(screen.getByRole("button", { name: "Toggle contextual Worktrees" }));
  await waitFor(() => {
    expect(view.client.isFetching()).toBe(0);
  });
  list.mockRejectedValueOnce(new Error("sidebar read unavailable"));
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.refresh") }));
  expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  expect(screen.getByRole("textbox")).toHaveValue("retained input");
  expect(screen.getByText("Message 1")).toBeInTheDocument();
  const calls = list.mock.calls.length;
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  await waitFor(() => expect(screen.queryByTestId("error-state")).not.toBeInTheDocument());
  expect(list).toHaveBeenCalledTimes(calls + 1);
  list.mockRejectedValueOnce(new Error("sidebar failed again"));
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.refresh") }));
  expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Toggle contextual Worktrees" }));
  expect(screen.getByRole("textbox")).toHaveValue("retained input");
  expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
});

it("keeps a failed Chat Worktree-label read owned by Chat through targeted Retry", async () => {
  const view = sessionWithPrompts();
  const list = vi.spyOn(view.services.api, "listWorktrees");
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "preserved label draft");
  await waitFor(() => {
    expect(view.client.isFetching()).toBe(0);
  });
  list.mockRejectedValueOnce(new Error("label read unavailable"));
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), SessionIdentity: changedIdentity() },
    });
  });
  expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  const reads = list.mock.calls.length;
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByRole("textbox")).toHaveValue("preserved label draft");
  expect(list).toHaveBeenCalledTimes(reads + 1);
  expect(view.services.api.chat.getMainView).toHaveBeenCalledOnce();
  expect(view.settings).toHaveBeenCalledOnce();
});

it("keeps the opening editor usable and delays its one control indicator", async () => {
  vi.useFakeTimers();
  try {
    const services = createTestServices([]);
    const loading = deferred<ChatSettingsRead>();
    vi.spyOn(services.api.chat, "getSettings").mockReturnValue(loading.promise);
    vi.spyOn(services.api.chat, "getCommandCatalog").mockResolvedValue([]);
    const view = renderDestination(services, new QueryClient(), opening);
    const editor = screen.getByRole("textbox");
    expect(editor).not.toHaveAttribute("readonly");
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    await act(async () => vi.advanceTimersByTimeAsync(499));
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(screen.getByTestId("chat-opening-controls")).toBeInTheDocument();
    await act(async () => {
      loading.resolve(catalog);
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox")).toBe(editor);
    view.unmount();
  } finally {
    vi.useRealTimers();
  }
});

it("does not show opening indicators when settings arrive within the reveal delay", async () => {
  vi.useFakeTimers();
  try {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getSettings").mockResolvedValue(catalog);
    vi.spyOn(services.api.chat, "getCommandCatalog").mockResolvedValue([]);
    const view = renderDestination(services, new QueryClient(), opening);
    await act(async () => vi.advanceTimersByTimeAsync(50));
    expect(screen.getByRole("textbox")).not.toHaveAttribute("readonly");
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    await act(async () => vi.advanceTimersByTimeAsync(600));
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    view.unmount();
  } finally {
    vi.useRealTimers();
  }
});

it("opens Session editing independently of settings and merges the late saved draft", async () => {
  const draft = deferred<{ input: string; protectedInput: string | null }>();
  const view = sessionWithPrompts((services) => {
    vi.mocked(services.api.chat.getDraft).mockReturnValue(draft.promise);
    vi.mocked(services.api.chat.getSettings).mockReturnValue(deferred<ChatSettingsRead>().promise);
  });
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "new typing");
  expect(screen.getByRole("button", { name: appI18n.t("chatComposer.send") })).toBeDisabled();
  await act(async () => {
    draft.resolve({ input: "saved", protectedInput: null });
  });
  expect(screen.getByRole("textbox")).toHaveValue("saved\nnew typing");
  expect(view.services.api.chat.getMainView).toHaveBeenCalled();
  expect(view.services.api.chat.subscribeTranscript).toHaveBeenCalled();
});

it("blocks parent interaction while Edit is pending and restores its draft after rejection", async () => {
  const response = deferred<string>();
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "forkEdit").mockReturnValue(response.promise);
    vi.mocked(services.api.chat.getTranscriptPage).mockResolvedValue({
      ...transcriptPage(null),
      entries: [{ ...row(1), User: { Text: "original message", RollbackTargetID: "rollback" } }],
    });
  });
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "parent draft");
  await user.click(await screen.findByRole("button", { name: appI18n.t("chatTranscript.edit") }));
  expect(view.services.api.chat.forkEdit).toHaveBeenCalledWith(expect.objectContaining(sessionTarget), {
    rollbackTargetID: "rollback",
    initialInput: "original message\n\nparent draft",
  });
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  await act(async () => {
    response.reject(new Error("fork unavailable"));
  });
  expect(await screen.findByRole("textbox")).toHaveValue("parent draft");
  expect(screen.getByRole("textbox")).not.toHaveAttribute("readonly");
});

it("keeps the parent draft when child creation succeeds but departure saving fails", async () => {
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "forkEdit").mockResolvedValue("child-session");
    vi.mocked(services.api.chat.getTranscriptPage).mockResolvedValue({
      ...transcriptPage(null),
      entries: [{ ...row(1), User: { Text: "original", RollbackTargetID: "rollback" } }],
    });
  });
  navigation.openParentSession.mockImplementationOnce(async (sessionID: string) =>
    view.router.navigate({
      to: "/projects/$projectId/sessions/$sessionId",
      params: { projectId: sessionTarget.projectID, sessionId: sessionID },
    }),
  );
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "preserve parent");
  vi.mocked(view.services.api.chat.persistDraft).mockRejectedValue(new Error("save unavailable"));
  await user.click(await screen.findByRole("button", { name: appI18n.t("chatTranscript.edit") }));
  await waitFor(() => {
    expect(view.services.api.chat.persistDraft).toHaveBeenCalled();
  });
  expect(await screen.findByRole("textbox")).toHaveValue("preserve parent");
  expect(screen.getByRole("textbox")).not.toHaveAttribute("readonly");
  expect(view.router.state.location.pathname).toBe("/");
  expect(view.services.api.chat.forkEdit).toHaveBeenCalledOnce();
});

it("preserves focus, selection and continued typing across successive deliveries with Worktree commands", async () => {
  const first = deferred<ChatInputMutationResult>();
  const second = deferred<ChatInputMutationResult>();
  const view = sessionWithPrompts((services) => {
    vi.mocked(services.api.chat.getSettings).mockResolvedValueOnce(catalog);
    vi.spyOn(services.api.chat, "steer")
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    vi.mocked(services.api.chat.getMainView).mockReturnValue(
      deferred<Awaited<ReturnType<typeof services.api.chat.getMainView>>>().promise,
    );
    vi.mocked(services.api.chat.getTranscriptPage).mockReturnValue(
      deferred<Awaited<ReturnType<typeof services.api.chat.getTranscriptPage>>>().promise,
    );
  }, opening);
  const user = userEvent.setup();
  const send = () => screen.getByRole("button", { name: appI18n.t("chatComposer.send") });
  await user.type(screen.getByRole("textbox"), "/review one");
  await waitFor(() => expect(send()).toBeEnabled());
  await user.click(send());
  await user.type(screen.getByRole("textbox"), "/init two");
  await user.click(send());
  await user.type(screen.getByRole("textbox"), "continuing");
  const editor = screen.getByRole<HTMLTextAreaElement>("textbox");
  editor.setSelectionRange(2, 5);
  const accepted = (sessionID: string): ChatInputMutationResult => ({
    sessionID,
    outcome: {
      kind: "accepted",
      queueItemID: parsePendingWorkItemID("99999999-9999-4999-8999-999999999999"),
      diagnostic: null,
    },
  });
  await act(async () => {
    first.resolve(accepted("first-session"));
  });
  await waitFor(() => {
    expect(view.services.api.chat.persistDraft).toHaveBeenCalled();
  });
  expect(screen.getByRole("textbox")).toHaveFocus();
  expect(editor.selectionStart).toBe(2);
  expect(editor.selectionEnd).toBe(5);
  expect(editor).toHaveValue("continuing");
  await act(async () => {
    second.resolve(accepted("second-session"));
  });
  expect(screen.getByRole("textbox")).toHaveFocus();
  expect(editor.selectionStart).toBe(2);
  expect(editor.selectionEnd).toBe(5);
  await user.type(editor, "X", {
    skipClick: true,
    initialSelectionStart: editor.selectionStart,
    initialSelectionEnd: editor.selectionEnd,
  });
  expect(screen.getByRole("textbox")).toHaveValue("coXnuing");
});

it("retains committed history and input during explicit observation Retry without replay", async () => {
  const view = sessionWithPrompts();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "unsent");
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  act(() =>
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...hydration(),
        TailSegment: { OlderCursor: null, HasMoreAbove: false, Entries: [row(1)] },
      },
    }),
  );
  const priorRows = screen.getAllByText("Message 1");
  act(() => view.handlers[0]?.onError(new Error("observation lost")));
  expect(screen.getByRole("textbox")).toHaveValue("unsent");
  expect(screen.getAllByText("Message 1")).toHaveLength(priorRows.length);
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  expect(view.handlers).toHaveLength(2);
  expect(screen.getAllByText("Message 1")).toHaveLength(priorRows.length);
  expect(screen.getByRole("textbox")).toHaveValue("unsent");
  expect(view.services.api.chat.getMainView).toHaveBeenCalledOnce();
  expect(view.settings).toHaveBeenCalledOnce();
});

it("retries the failed opening page and observation independently once per activation", async () => {
  const view = sessionWithPrompts((services) => {
    vi.mocked(services.api.chat.getTranscriptPage).mockRejectedValueOnce(new Error("page unavailable"));
  });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  act(() => view.handlers[0]?.onError(new Error("observation unavailable")));
  await waitFor(() => expect(screen.getByRole("button", { name: appI18n.t("app.retry") })).toBeEnabled());
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "keep input");
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  await waitFor(() => {
    expect(view.services.api.chat.getTranscriptPage).toHaveBeenCalledTimes(2);
  });
  expect(view.handlers).toHaveLength(2);
  expect(view.services.api.chat.getMainView).toHaveBeenCalledOnce();
  expect(view.settings).toHaveBeenCalledOnce();
  expect(screen.getByRole("textbox")).toHaveValue("keep input");
});

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
