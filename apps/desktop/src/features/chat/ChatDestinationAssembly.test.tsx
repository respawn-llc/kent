import { afterEach, beforeEach, expect, it, onTestFinished, vi } from "bun:test";
import { installChatStorageFixture } from "./chatStorageFixture";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { SidebarHeaderActionProvider, SidebarHeaderActionSlot } from "@/app-facade";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { WorktreeBrowser } from "./WorktreeBrowser";

import {
  RpcError,
  pendingWorkItemIDFromProto,
  type ChatInputMutationResult,
  type ChatSettingsRead,
} from "@/api";
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
import { installVirtualizedScrollGeometry } from "@/test-support/resize-observer";
import { installAnimationFrameTestSupport } from "@/test-support/scheduling";
import { appI18n } from "@/i18n";

import {
  opening,
  catalog,
  navigation,
  renderDestination,
  sessionWithPrompts,
} from "@/test-support/chat-destination";
let restoreStorage: (() => void) | undefined;
beforeEach(() => {
  restoreStorage = installChatStorageFixture();
});
afterEach(() => {
  restoreStorage?.();
  restoreStorage = undefined;
});

it.each([question(), approval()])(
  "hides navigation and position for a single $kind prompt",
  async (prompt) => {
    const view = sessionWithPrompts();
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
    expect(await screen.findByRole("radiogroup")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: appI18n.t("chat.picker.previous") })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: appI18n.t("chat.picker.next") })).not.toBeInTheDocument();
    expect(
      screen.queryByText(appI18n.t("chat.picker.position", { current: 1, count: 1 })),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: appI18n.t("chatComposer.stop") })).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: appI18n.t("chat.picker.decline") }));
    await waitFor(() => {
      expect(view.answer).toHaveBeenCalledTimes(1);
    });
    expect(view.answer.mock.calls[0]?.[0].entries).toEqual([
      { toolCallID: prompt.toolCallID, kind: "declined" },
    ]);
    const decline = screen.getByRole("button", { name: appI18n.t("chat.picker.decline") });
    expect(decline).toBeDisabled();
    expect(within(decline).getByTestId("spinner")).toBeInTheDocument();
    view.unmount();
  },
);

it("shows original batch navigation while the remaining questions are still arriving", async () => {
  const view = sessionWithPrompts();
  const toolCallIDs = ["first", "second", "third", "fourth", "fifth"];
  const prompt = question("first", { batch: { toolCallIDs, unmaterializedCount: 4 } });
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
  expect(
    await screen.findByText(appI18n.t("chat.picker.position", { current: 1, count: 5 })),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: appI18n.t("chat.picker.previous") })).toBeDisabled();
  expect(screen.getByRole("button", { name: appI18n.t("chat.picker.next") })).toBeDisabled();
  const first = screen.getAllByRole("radio")[0];
  if (first === undefined) throw new Error("Question must offer an answer.");
  await userEvent.setup().click(first);
  expect(view.answer).not.toHaveBeenCalled();
  view.unmount();
});

it("keeps navigation for a five-question batch when four questions are answered", async () => {
  const view = sessionWithPrompts();
  const toolCallIDs = Array.from({ length: 5 }, (_, index) => `question-${String(index + 1)}`);
  const prompts = toolCallIDs.map((id) => question(id, { batch: { toolCallIDs, unmaterializedCount: 0 } }));
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
  for (let index = 0; index < 4; index += 1) {
    expect(
      await screen.findByText(appI18n.t("chat.picker.position", { current: index + 1, count: 5 })),
    ).toBeInTheDocument();
    const first = screen.getAllByRole("radio")[0];
    if (first === undefined) throw new Error("Question must offer an answer.");
    await user.click(first);
  }
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 5, count: 5 }))).toBeInTheDocument();
  expect(screen.getByRole("button", { name: appI18n.t("chat.picker.previous") })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: appI18n.t("chat.picker.next") })).toBeInTheDocument();
  expect(view.answer).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.picker.previous") }));
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 4, count: 5 }))).toBeInTheDocument();
  view.unmount();
});

it("declines the current prompt from composer actions without stopping the run and submits the completed batch", async () => {
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "stop").mockResolvedValue("stopped");
  });
  const prompts = [question(), approval()];
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
  const decline = await screen.findByRole("button", { name: appI18n.t("chat.picker.decline") });
  expect(screen.getAllByRole("button", { name: appI18n.t("chat.picker.decline") })).toHaveLength(1);
  expect(screen.queryByRole("button", { name: appI18n.t("chatComposer.stop") })).not.toBeInTheDocument();
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 1, count: 2 }))).toBeInTheDocument();
  await user.click(decline);
  expect(view.answer).not.toHaveBeenCalled();
  expect(screen.getByText(appI18n.t("chat.picker.position", { current: 2, count: 2 }))).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.picker.previous") }));
  expect(screen.getByRole("button", { name: appI18n.t("chat.picker.decline") })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.picker.next") }));
  await user.click(screen.getByRole("button", { name: appI18n.t("chat.picker.decline") }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
  });
  expect(view.answer.mock.calls[0]?.[0].entries).toEqual(
    prompts.map((prompt) => ({ toolCallID: prompt.toolCallID, kind: "declined" })),
  );
  const pendingDecline = screen.getByRole("button", { name: appI18n.t("chat.picker.decline") });
  expect(pendingDecline).toBeDisabled();
  expect(within(pendingDecline).getByTestId("spinner")).toBeInTheDocument();
  expect(view.services.api.chat.stop).not.toHaveBeenCalled();
  await act(async () => {
    view.response.resolve({
      results: prompts.map((prompt) => ({ toolCallID: prompt.toolCallID, outcome: "resolved" })),
    });
  });
  const stop = await screen.findByRole("button", { name: appI18n.t("chatComposer.stop") });
  expect(screen.queryByRole("button", { name: appI18n.t("chat.picker.decline") })).not.toBeInTheDocument();
  await user.click(stop);
  await waitFor(() => {
    expect(view.services.api.chat.stop).toHaveBeenCalledTimes(1);
  });
  view.unmount();
});

it("holds busy Send readonly until acceptance during compaction, then allows continued typing", async () => {
  const accepted = deferred<ChatInputMutationResult>();
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "steer").mockReturnValue(accepted.promise);
  });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...hydration(),
        ActiveCompaction: { StepID: "compact", State: "started", Mode: "manual", Count: 1 },
      },
    });
  });
  const user = userEvent.setup();
  const editor = await screen.findByRole("textbox");
  await user.type(editor, "steer during compaction");
  await user.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
  await waitFor(() => {
    expect(view.services.api.chat.steer).toHaveBeenCalledTimes(1);
  });
  expect(editor).toHaveValue("steer during compaction");
  expect(editor).toHaveAttribute("readonly");
  await user.type(editor, "next draft");
  expect(editor).toHaveValue("steer during compaction");
  await act(async () => {
    accepted.resolve({
      sessionID: sessionTarget.sessionID,
      outcome: {
        kind: "accepted",
        disposition: "pending_steer",
        queueItemID: pendingWorkItemIDFromProto(crypto.randomUUID()),
        diagnostic: null,
      },
    });
  });
  expect(editor).toHaveValue("");
  expect(editor).not.toHaveAttribute("readonly");
  await user.type(editor, "next draft");
  expect(editor).toHaveValue("next draft");
});

it("keeps streamed output and drafts usable when reviewer suggestions are followed by a question", async () => {
  const geometry = installVirtualizedScrollGeometry(600);
  const view = sessionWithPrompts();
  const prompt = question("question-1", { recommendedOptionIndex: 1 });
  try {
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
      expect(view.client.isFetching()).toBe(0);
    });
    const editor = await screen.findByRole("textbox");
    const user = userEvent.setup();
    await user.type(editor, "Keep this draft");
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: {
          ...hydration(),
          TailSegment: { OlderCursor: null, HasMoreAbove: false, Entries: [row(1)] },
          ActiveAssistants: [
            {
              StepID: prompt.stepID,
              StreamID: "stream",
              Phase: "commentary",
              Text: "Current response",
            },
          ],
        },
      });
    });
    const list = screen.getByRole("list");
    act(() => {
      geometry.resize(list, 600);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 2,
        kind: "committed_row",
        payload: {
          ...row(2),
          Kind: "reviewer_feedback",
          User: null,
          ReviewerFeedback: {
            ID: crypto.randomUUID(),
            StepID: prompt.stepID,
            Suggestions: ["Check the result"],
            SuggestionCount: 1,
          },
        },
      });
    });
    await user.click(within(list).getByRole("button", { expanded: false }));
    expect(await screen.findByText("Check the result")).toBeInTheDocument();
    await act(async () => {
      for (let sequence = 3; sequence < 63; sequence++) {
        view.handlers[0]?.onEvent({
          sequence,
          kind: "assistant_delta",
          payload: { StepID: prompt.stepID, StreamID: "stream", Phase: "commentary", Delta: "a" },
        });
        await Promise.resolve();
      }
    });
    await act(async () => {
      view.handlers[0]?.onEvent({ sequence: 63, kind: "prompt", payload: { state: "pending", prompt } });
    });
    expect(view.services.logger.entries().filter((entry) => entry.level === "warn")).toEqual([]);
    expect(view.handlers).toHaveLength(1);
    expect(await screen.findByRole("radio", { name: "one" })).toBeInTheDocument();
    expect(editor).not.toBeVisible();
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 64,
        kind: "prompt",
        payload: { state: "resolved", toolCallID: prompt.toolCallID },
      });
      view.handlers[0]?.onEvent({
        sequence: 65,
        kind: "assistant_delta",
        payload: { StepID: prompt.stepID, StreamID: "stream", Phase: "commentary", Delta: " continued" },
      });
    });
    expect(await screen.findByRole("textbox")).toHaveValue("Keep this draft");
    expect(list).toHaveTextContent(`Current response${"a".repeat(60)} continued`);
    expect(screen.queryByRole("button", { name: appI18n.t("app.retry") })).not.toBeInTheDocument();
    expect(view.handlers).toHaveLength(1);
  } finally {
    view.unmount();
    geometry.restore();
  }
});

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
            onOpenSession={vi.fn()}
            projectID={sessionTarget.projectID}
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
  await waitFor(() => {
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });
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
  expect(view.services.api.chat.getMainView).toHaveBeenCalledTimes(1);
  expect(view.settings).toHaveBeenCalledTimes(1);
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
    await act(async () => {
      vi.advanceTimersByTime(499);
    });
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByTestId("chat-opening-controls")).toBeInTheDocument();
    await act(async () => {
      loading.resolve(catalog);
      await loading.promise;
      vi.advanceTimersByTime(1);
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
    const settingsRead = Promise.resolve(catalog);
    vi.spyOn(services.api.chat, "getSettings").mockReturnValue(settingsRead);
    vi.spyOn(services.api.chat, "getCommandCatalog").mockResolvedValue([]);
    const view = renderDestination(services, new QueryClient(), opening);
    await act(async () => {
      vi.advanceTimersByTime(50);
    });
    expect(screen.getByRole("textbox")).not.toHaveAttribute("readonly");
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    await act(async () => {
      await settingsRead;
    });
    await act(async () => {
      vi.advanceTimersByTime(600);
    });
    expect(screen.queryByTestId("chat-opening-controls")).not.toBeInTheDocument();
    view.unmount();
  } finally {
    vi.useRealTimers();
  }
});

it("opens Session editing independently of settings and merges the late saved draft", async () => {
  const draft = deferred<{ input: string; protectedInput: string | null }>();
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "getDraft").mockReturnValue(draft.promise);
    vi.spyOn(services.api.chat, "getSettings").mockReturnValue(deferred<ChatSettingsRead>().promise);
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
    vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue({
      ...transcriptPage(null),
      entries: [
        { ...row(1), User: { Text: "original message", RollbackTargetID: "rollback", QueueItemIDs: [] } },
      ],
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
    vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue({
      ...transcriptPage(null),
      entries: [{ ...row(1), User: { Text: "original", RollbackTargetID: "rollback", QueueItemIDs: [] } }],
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
  vi.spyOn(view.services.api.chat, "persistDraft").mockRejectedValue(new Error("save unavailable"));
  await user.click(await screen.findByRole("button", { name: appI18n.t("chatTranscript.edit") }));
  await waitFor(() => {
    expect(view.services.api.chat.persistDraft).toHaveBeenCalled();
  });
  expect(await screen.findByRole("textbox")).toHaveValue("preserve parent");
  expect(screen.getByRole("textbox")).not.toHaveAttribute("readonly");
  expect(view.router.state.location.pathname).toBe("/");
  expect(view.services.api.chat.forkEdit).toHaveBeenCalledTimes(1);
});

it("preserves focus, selection and continued typing across successive deliveries with Worktree commands", async () => {
  onTestFinished(() => {
    vi.useRealTimers();
  });
  const first = deferred<ChatInputMutationResult>();
  const second = deferred<ChatInputMutationResult>();
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "getSettings").mockResolvedValueOnce(catalog);
    vi.spyOn(services.api.chat, "steer")
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    vi.spyOn(services.api.chat, "getMainView").mockReturnValue(
      deferred<Awaited<ReturnType<typeof services.api.chat.getMainView>>>().promise,
    );
    vi.spyOn(services.api.chat, "getTranscriptPage").mockReturnValue(
      deferred<Awaited<ReturnType<typeof services.api.chat.getTranscriptPage>>>().promise,
    );
  }, opening);
  const user = userEvent.setup();
  const send = () => screen.getByRole("button", { name: appI18n.t("chatComposer.send") });
  // Populate setup drafts in one input operation; only the final keystroke
  // exercises selection replacement after the asynchronous deliveries.
  await user.click(screen.getByRole("textbox"));
  await user.paste("/review one");
  await waitFor(() => {
    expect(send()).toBeEnabled();
  });
  vi.useFakeTimers();
  fireEvent.click(send());
  await waitFor(() => {
    expect(view.services.api.chat.steer).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    vi.advanceTimersByTime(2000);
  });
  await waitFor(() => {
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").readOnly).toBe(false);
  });
  vi.useRealTimers();
  expect(view.services.api.chat.steer).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").readOnly).toBe(false);
  await user.click(screen.getByRole("textbox"));
  await user.paste("/init two");
  vi.useFakeTimers();
  fireEvent.click(send());
  await waitFor(() => {
    expect(view.services.api.chat.steer).toHaveBeenCalledTimes(2);
  });
  await act(async () => {
    vi.advanceTimersByTime(2000);
  });
  await waitFor(() => {
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").readOnly).toBe(false);
  });
  vi.useRealTimers();
  expect(view.services.api.chat.steer).toHaveBeenCalledTimes(2);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").readOnly).toBe(false);
  await user.click(screen.getByRole("textbox"));
  await user.paste("continuing");
  const editor = screen.getByRole<HTMLTextAreaElement>("textbox");
  editor.setSelectionRange(2, 5);
  const accepted = (sessionID: string): ChatInputMutationResult => ({
    sessionID,
    outcome: {
      kind: "accepted",
      disposition: "started_turn",
      queueItemID: pendingWorkItemIDFromProto("99999999-9999-4999-8999-999999999999"),
      diagnostic: null,
    },
  });
  await act(async () => {
    first.resolve(accepted("33333333-3333-4333-8333-333333333333"));
  });
  await waitFor(() => {
    expect(view.services.api.chat.persistDraft).toHaveBeenCalled();
  });
  expect(screen.getByRole("textbox")).toHaveFocus();
  expect(editor.selectionStart).toBe(2);
  expect(editor.selectionEnd).toBe(5);
  expect(editor).toHaveValue("continuing");
  await act(async () => {
    second.resolve(accepted("44444444-4444-4444-8444-444444444444"));
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
  const failure = new Error("observation lost");
  act(() => view.handlers[0]?.onError(failure));
  const finalRow = within(screen.getByRole("list")).getAllByRole("listitem").at(-1);
  if (finalRow === undefined) throw new Error("Expected an inline transcript error.");
  expect(within(finalRow).getByText(failure.message)).toBeInTheDocument();
  expect(screen.getAllByText(failure.message)).toHaveLength(1);
  expect(screen.getByRole("textbox")).toHaveValue("unsent");
  expect(screen.getAllByText("Message 1")).toHaveLength(priorRows.length);
  await user.click(screen.getByRole("button", { name: appI18n.t("app.retry") }));
  expect(view.handlers).toHaveLength(2);
  expect(screen.getAllByText("Message 1")).toHaveLength(priorRows.length);
  expect(screen.getByRole("textbox")).toHaveValue("unsent");
  expect(view.services.api.chat.getMainView).toHaveBeenCalledTimes(1);
  expect(view.settings).toHaveBeenCalledTimes(1);
});

it("retries the failed opening page and observation independently once per activation", async () => {
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "getTranscriptPage").mockRejectedValueOnce(new Error("page unavailable"));
  });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  act(() => view.handlers[0]?.onError(new Error("observation unavailable")));
  const pageRetry = await within(screen.getByTestId("error-state")).findByRole("button", {
    name: appI18n.t("app.retry"),
  });
  await waitFor(() => {
    expect(pageRetry).toBeEnabled();
  });
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "keep input");
  await user.click(pageRetry);
  await waitFor(() => {
    expect(view.services.api.chat.getTranscriptPage).toHaveBeenCalledTimes(2);
  });
  expect(view.handlers).toHaveLength(2);
  expect(view.services.api.chat.getMainView).toHaveBeenCalledTimes(1);
  expect(view.settings).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("textbox")).toHaveValue("keep input");
});

it.each(["page-first", "hydration-first"] as const)(
  "opens mounted Chat at the newest content through %s startup",
  async (order) => {
    const restoreAnimationFrameTestSupport = installAnimationFrameTestSupport();
    const geometry = installVirtualizedScrollGeometry(600);
    const pageRead = deferred<ReturnType<typeof transcriptPage>>();
    const entries = Array.from({ length: 100 }, (_, index) => row(index + 1));
    const view = sessionWithPrompts((services) => {
      vi.spyOn(services.api.chat, "getTranscriptPage").mockReturnValue(pageRead.promise);
    });
    try {
      await waitFor(() => {
        expect(view.handlers).toHaveLength(1);
      });
      const admitPage = async () => {
        await act(async () => {
          pageRead.resolve({ ...transcriptPage(null), entries });
          await pageRead.promise;
        });
      };
      const admitHydration = async () => {
        await act(async () => {
          view.handlers[0]?.onEvent({
            sequence: 1,
            kind: "hydration",
            payload: {
              ...hydration(),
              TailSegment: { Entries: entries, OlderCursor: null, HasMoreAbove: false },
            },
          });
        });
      };
      if (order === "page-first") {
        await admitPage();
        await admitHydration();
      } else {
        await admitHydration();
        await admitPage();
      }
      const list = await screen.findByRole("list");
      expect(list.scrollHeight).toBeGreaterThan(list.clientHeight);
      await waitFor(() => {
        expect(list.scrollTop).toBe(list.scrollHeight - list.clientHeight);
      });
      const latestText = entries.at(-1)?.User?.Text;
      if (latestText === undefined) throw new Error("Latest message is missing.");
      expect(await screen.findByText(latestText)).toBeInTheDocument();
    } finally {
      view.unmount();
      geometry.restore();
      restoreAnimationFrameTestSupport();
    }
  },
);

it("shows a newly arriving question in the open Chat without a notification click", async () => {
  const view = sessionWithPrompts();
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
  });
  const editor = screen.getByRole("textbox");
  const user = userEvent.setup();
  await user.type(editor, "unsent draft");
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 2,
      kind: "prompt",
      payload: { state: "pending", prompt: question() },
    });
  });
  expect(await screen.findByRole("radio", { name: "one" })).toBeInTheDocument();
  expect(editor).not.toBeVisible();
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 3,
      kind: "prompt",
      payload: { state: "resolved", toolCallID: question().toolCallID },
    });
  });
  expect(await screen.findByRole("textbox")).toHaveValue("unsent draft");
  view.unmount();
});

it("shows every accepted Pending Work item and restores only a discarded item", async () => {
  const accepted = Array.from({ length: 105 }, (_, index) => ({
    id: pendingWorkItemIDFromProto(crypto.randomUUID()),
    kind: "message" as const,
    state: "pending" as const,
    lane: "steer" as const,
    canonicalInput: `Pending input ${String(index)}`,
    message: { text: `Pending input ${String(index)}` },
  }));
  let pending = accepted;
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "listPendingWork").mockImplementation(async () => ({ items: pending }));
    vi.spyOn(services.api.chat, "removePendingWork").mockImplementation(async (_, id) => {
      const item = pending.find((entry) => entry.id.toJSONValue() === id.toJSONValue());
      if (item === undefined) throw new Error("Pending item is missing.");
      pending = pending.filter((entry) => entry !== item);
      return { kind: "message", canonicalInput: item.canonicalInput };
    });
  });
  const discard = await screen.findAllByRole("button", { name: appI18n.t("chatComposer.discard") });
  expect(discard).toHaveLength(accepted.length);
  const last = accepted.at(-1);
  const button = discard.at(-1);
  if (last === undefined || button === undefined) throw new Error("Missing accepted item.");
  await userEvent.setup().click(button);
  await waitFor(() => {
    expect(screen.getByRole("textbox")).toHaveValue(last.canonicalInput);
  });
  await waitFor(() => {
    expect(button).not.toBeInTheDocument();
  });
  expect(screen.getAllByRole("button", { name: appI18n.t("chatComposer.discard") })).toHaveLength(104);
  view.unmount();
});

it.each(["radio", "label"] as const)(
  "confirms an option with one %s click when commentary is empty",
  async (surface) => {
    const view = sessionWithPrompts();
    const prompt = question("quick-answer", { recommendedOptionIndex: 1 });
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
      });
    });
    const option = await screen.findByRole("radio", { name: "one" });
    await userEvent.setup().click(surface === "radio" ? option : screen.getByText("one"));
    await waitFor(() => {
      expect(view.answer).toHaveBeenCalledTimes(1);
    });
    expect(view.answer.mock.calls[0]?.[0].entries).toMatchObject([
      { selectedOptionNumber: 1, freeform: null },
    ]);
    view.unmount();
  },
);

it.each(["enter", "send"] as const)(
  "Neither focuses commentary and waits for explicit %s",
  async (method) => {
    const view = sessionWithPrompts();
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox"), "unsent message draft");
    const prompt = question("neither-test");
    await waitFor(() => {
      expect(view.handlers).toHaveLength(1);
    });
    await act(async () => {
      view.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
      });
    });
    const commentary = await screen.findByRole("textbox", { name: appI18n.t("task.commentary") });
    await user.type(commentary, "unfinished");
    const neither = screen.getByRole("radio", { name: appI18n.t("task.neitherOption") });
    await user.click(neither);
    expect(commentary).toHaveFocus();
    await user.click(neither);
    expect(view.answer).not.toHaveBeenCalled();
    await user.type(commentary, " answer");
    if (method === "enter") await user.keyboard("{Enter}");
    else await user.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
    await waitFor(() => {
      expect(view.answer).toHaveBeenCalledTimes(1);
    });
    expect(view.answer.mock.calls[0]?.[0].entries).toEqual([
      {
        kind: "question",
        toolCallID: prompt.toolCallID,
        selectedOptionNumber: null,
        freeform: "unfinished answer",
      },
    ]);
    await act(async () => {
      view.response.resolve({ results: [{ toolCallID: prompt.toolCallID, outcome: "resolved" }] });
    });
    expect(await screen.findByRole("textbox")).toHaveValue("unsent message draft");
    view.unmount();
  },
);

it("protects typed commentary even for the preselected recommendation and resets protection after more typing", async () => {
  const view = sessionWithPrompts();
  const prompt = question("recommended-test", { recommendedOptionIndex: 1 });
  await waitFor(() => {
    expect(view.handlers).toHaveLength(1);
  });
  await act(async () => {
    view.handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
    });
  });
  const user = userEvent.setup();
  const field = await screen.findByRole("textbox", { name: appI18n.t("task.commentary") });
  const option = screen.getByRole("radio", { name: "one" });
  await user.type(field, "still writing");
  await user.click(option);
  expect(view.answer).not.toHaveBeenCalled();
  await user.type(field, " commentary");
  await user.click(option);
  expect(view.answer).not.toHaveBeenCalled();
  await user.click(option);
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
  });
  expect(view.answer.mock.calls[0]?.[0].entries).toMatchObject([
    { selectedOptionNumber: 1, freeform: "still writing commentary" },
  ]);
  view.unmount();
});

it("preserves failed answer diagnostics separately while retaining answers for explicit resubmission", async () => {
  const failure = new RpcError({
    code: "internal_failure",
    method: "answer_batch",
    message: crypto.randomUUID(),
  });
  const view = sessionWithPrompts();
  const prompt = question();
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
    "retained answer",
  );
  await user.click(screen.getByRole("radio", { name: "one" }));
  expect(view.answer).not.toHaveBeenCalled();
  await user.click(screen.getByRole("radio", { name: "one" }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    view.response.reject(failure);
  });
  await waitFor(() => {
    expect(
      view.services.logger
        .entries()
        .some(
          ({ context }) => context.error === failure.message && context.sessionID === sessionTarget.sessionID,
        ),
    ).toBe(true);
  });
  expect(screen.getByRole("radio", { name: "one" })).toBeChecked();
  expect(screen.getByDisplayValue("retained answer")).toBeInTheDocument();
  expect(view.answer).toHaveBeenCalledTimes(1);
  view.answer.mockResolvedValue({ results: [{ toolCallID: prompt.toolCallID, outcome: "resolved" }] });
  await user.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(2);
  });
  expect(view.answer.mock.calls[1]?.[0]).toEqual(view.answer.mock.calls[0]?.[0]);
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
  await user.click(screen.getByRole("radio", { name: "two" }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
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
  await user.click(screen.getByRole("radio", { name: option }));
  await waitFor(() => {
    expect(view.answer).toHaveBeenCalledTimes(1);
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
  expect(view.answer).toHaveBeenCalledTimes(1);
  await act(async () => {
    view.response.resolve({ results: [{ toolCallID: prompt.toolCallID, outcome: "resolved" }] });
  });
  await waitFor(() => {
    expect(screen.queryByRole("radio", { name: option })).not.toBeInTheDocument();
  });
  expect(view.answer).toHaveBeenCalledTimes(1);
});
