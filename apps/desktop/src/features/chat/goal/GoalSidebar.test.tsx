import { afterEach, describe, expect, it, vi } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode, useState } from "react";

import type { ChatSessionTarget } from "@/api";
import {
  goalSource,
  type GoalObservationHandler as ChatGoalObservationHandler,
} from "@/test-support/chat-runtime";
import { ChatOperationError, RpcError } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { TestSidebar } from "@/test-support/sidebar-rendering";
import { ChatPromptPresenceProvider } from "@/app-facade";
import * as ui from "@/ui";
import type { NewChatGoalHostDelivery } from "./goalBinding";
import { createGoalFixtureOwner, GoalFixturePage } from "./goalBindingFixtures";
import { GoalSidebarPage, type GoalSidebarApi } from "./GoalSidebar";
import { useGoalSidebarLauncher } from "./useGoalSidebarLauncher";

type GoalFact = Parameters<ChatGoalObservationHandler["onEvent"]>[0]["fact"];
type GoalObservation = Parameters<ChatGoalObservationHandler["onEvent"]>[0];
type GoalSetResult = Awaited<ReturnType<GoalSidebarApi["setGoal"]>>;

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  projectID: "project-1",
  sessionID,
} as const;

async function enterGoalEditor(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("textbox", { name: "Goal" }));
  return waitFor(() => {
    const editor = screen.getByRole("textbox", { name: "Goal" });
    expect(editor).not.toHaveAttribute("aria-readonly");
    return editor;
  });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Goal sidebar", () => {
  it("keeps a pinned exact Session when the main Chat changes", async () => {
    const observedTargets: ChatSessionTarget[] = [];
    const handlers: ChatGoalObservationHandler[] = [];
    const pauseGoal = vi.fn(async () => ({
      kind: "authoritative_goal" as const,
      fact: { goal: goalValue("pinned objective", "paused"), availability: "available" as const },
    }));
    const api = createGoalSidebarApi({
      goal: null,
      pauseGoal,
      subscribeGoal: (selected, handler) => {
        observedTargets.push(selected);
        handlers.push(handler);
        queueMicrotask(() => {
          handler.onEvent(hydration({ objective: "pinned objective" }));
        });
        return { close: vi.fn() };
      },
    });
    const services = createTestServices([]);
    function ChatHarness() {
      const [selected, setSelected] = useState<ChatSessionTarget>(target);
      const open = useGoalSidebarLauncher({ kind: "session", api, target: selected });
      return (
        <>
          <div data-testid="main-chat">{selected.sessionID}</div>
          <button onClick={open}>Open Goal destination</button>
          <button
            onClick={() => {
              setSelected({ ...target, sessionID: "different-session" });
            }}
          >
            Switch main Chat
          </button>
        </>
      );
    }
    render(
      <TestAppProviders skipAnimations services={services}>
        <ChatPromptPresenceProvider>
          <TestSidebar>
            <ChatHarness />
          </TestSidebar>
        </ChatPromptPresenceProvider>
      </TestAppProviders>,
    );
    await userEvent.setup().click(screen.getByRole("button", { name: "Open Goal destination" }));
    await screen.findByText("pinned objective");
    await userEvent.setup().click(screen.getByRole("button", { name: "Switch main Chat" }));
    expect(screen.getByTestId("main-chat")).toHaveTextContent("different-session");
    await userEvent.setup().click(screen.getByRole("button", { name: "Pause" }));
    expect(pauseGoal).toHaveBeenCalledWith(target);
    expect(observedTargets).toEqual([target]);
    act(() => {
      handlers[0]?.onEvent({
        ...hydration({ objective: "pinned update", status: "paused" }),
        sequence: 2,
        kind: "update",
      });
    });
    expect(await screen.findByText("pinned update")).toBeInTheDocument();
  });

  it("admits only one same-turn mutation", async () => {
    const pending = deferred<Awaited<ReturnType<GoalSidebarApi["pauseGoal"]>>>();
    const pauseGoal = vi.fn(async () => pending.promise);
    const api = createGoalSidebarApi({ goal: goalValue("saved", "active"), pauseGoal });
    render(
      <TestAppProviders skipAnimations services={createTestServices([])}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );
    const pause = await screen.findByRole("button", { name: "Pause" });
    act(() => {
      pause.click();
      pause.click();
    });
    await waitFor(() => {
      expect(pauseGoal).toHaveBeenCalledTimes(1);
    });
    await act(async () => {
      pending.resolve({
        kind: "authoritative_goal",
        fact: { goal: goalValue("saved", "paused"), availability: "available" },
      });
      await pending.promise;
    });
  });

  it("reports a closed exact destination failure without touching its reopened mutation", async () => {
    const first = deferred<Awaited<ReturnType<GoalSidebarApi["pauseGoal"]>>>();
    const second = deferred<Awaited<ReturnType<GoalSidebarApi["pauseGoal"]>>>();
    const pauseGoal = vi
      .fn()
      .mockImplementationOnce(async () => first.promise)
      .mockImplementationOnce(async () => second.promise);
    const api = createGoalSidebarApi({ goal: goalValue("saved", "active"), pauseGoal });
    const services = createTestServices([]);
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const page = (
      <TestAppProviders skipAnimations services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>
    );
    const view = render(page);
    await userEvent.setup().click(await screen.findByRole("button", { name: "Pause" }));
    view.rerender(
      <TestAppProviders skipAnimations services={services}>
        {null}
      </TestAppProviders>,
    );
    view.rerender(page);
    await userEvent.setup().click(await screen.findByRole("button", { name: "Pause" }));
    expect(pauseGoal).toHaveBeenCalledTimes(2);
    await act(async () => {
      first.reject(new Error("Rejected earlier operation"));
      await first.promise.catch(() => undefined);
    });
    await waitFor(() => {
      expect(notice).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByRole("button", { name: "Resume" })).toBeDisabled();
    await act(async () => {
      second.resolve({
        kind: "authoritative_goal",
        fact: { goal: goalValue("saved", "paused"), availability: "available" },
      });
      await second.promise;
    });
    expect(await screen.findByRole("button", { name: "Pause" })).toBeEnabled();
  });

  it("owns a fresh subscription for every StrictMode opening", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const closes: ReturnType<typeof vi.fn>[] = [];
    const subscribeGoal: GoalRegister = (_target, handler) => {
      handlers.push(handler);
      const close = vi.fn();
      closes.push(close);
      return { close };
    };
    const api = createGoalSidebarApi({ goal: null, subscribeGoal });
    const view = render(
      <TestAppProviders skipAnimations services={services}>
        <StrictMode>
          <GoalSidebarPage input={{ kind: "session", api, target }} />
        </StrictMode>
      </TestAppProviders>,
    );

    await waitFor(() => {
      expect(handlers.length).toBeGreaterThan(0);
    });
    const firstOpeningCount = handlers.length;
    act(() => {
      for (const handler of handlers.slice(0, -1)) {
        handler.onEvent(hydration({ objective: "stale opening" }));
      }
    });
    expect(screen.queryByText("stale opening")).not.toBeInTheDocument();
    act(() => {
      handlers[firstOpeningCount - 1]?.onEvent(hydration({ objective: "current opening" }));
    });
    expect(await screen.findByText("current opening")).toBeInTheDocument();
    view.unmount();
    await waitFor(() => {
      expect(closes.slice(0, firstOpeningCount).every((close) => close.mock.calls.length === 1)).toBe(true);
    });

    const { unmount: unmountReopened } = render(
      <TestAppProviders skipAnimations services={services}>
        <StrictMode>
          <GoalSidebarPage input={{ kind: "session", api, target }} />
        </StrictMode>
      </TestAppProviders>,
    );
    await waitFor(() => {
      expect(handlers.length).toBeGreaterThan(firstOpeningCount);
    });
    act(() => {
      for (const handler of handlers.slice(0, firstOpeningCount)) {
        handler.onEvent(hydration({ objective: "discarded opening" }));
      }
      handlers.at(-1)?.onEvent(hydration({ objective: "reopened opening" }));
    });
    expect(await screen.findByText("reopened opening")).toBeInTheDocument();
    expect(screen.queryByText("discarded opening")).not.toBeInTheDocument();
    unmountReopened();
    await waitFor(() => {
      expect(closes.every((close) => close.mock.calls.length === 1)).toBe(true);
    });
  });

  it("stops after an observed subscription failure and retries only explicitly", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const closes: ReturnType<typeof vi.fn>[] = [];
    const subscribeGoal: GoalRegister = (_target, handler) => {
      handlers.push(handler);
      const close = vi.fn();
      closes.push(close);
      return { close };
    };
    const api = createGoalSidebarApi({
      goal: goalValue("saved objective", "active"),
      subscribeGoal,
    });
    render(
      <TestAppProviders skipAnimations services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );

    await waitFor(() => {
      expect(handlers).toHaveLength(1);
    });
    act(() => {
      handlers[0]?.onEvent(hydration({ objective: "saved objective", status: "active" }));
    });
    const user = userEvent.setup();
    const editor = await enterGoalEditor(user);
    await user.clear(editor);
    await user.type(editor, "unsaved objective");
    act(() => {
      handlers[0]?.onError(new Error("Goal connection lost."));
    });

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(handlers).toHaveLength(1);
    expect(closes[0]).toHaveBeenCalledTimes(1);

    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => {
      expect(handlers).toHaveLength(2);
    });
    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();

    act(() => {
      handlers[1]?.onEvent(hydration({ objective: "recovered objective", status: "paused" }));
    });
    expect(await screen.findByDisplayValue("unsaved objective")).toBeInTheDocument();
  });

  it("uses subscription-only saved state after a successful action", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const pauseResult = deferred<Awaited<ReturnType<GoalSidebarApi["pauseGoal"]>>>();
    const subscribeGoal: GoalRegister = (_target, handler) => {
      handlers.push(handler);
      queueMicrotask(() => {
        handler.onEvent(hydration({ objective: "saved objective", status: "active" }));
      });
      return { close: vi.fn() };
    };
    const api = createGoalSidebarApi({
      goal: goalValue("saved objective", "active"),
      pauseGoal: vi.fn(async () => pauseResult.promise),
      subscribeGoal,
    });
    render(
      <TestAppProviders skipAnimations services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );

    await screen.findByText("saved objective");
    await userEvent.setup().click(screen.getByRole("button", { name: "Pause" }));
    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
    pauseResult.resolve({
      kind: "authoritative_goal",
      fact: { goal: goalValue("server paused objective", "paused"), availability: "available" },
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    });
    expect(screen.queryByRole("button", { name: "Resume" })).not.toBeInTheDocument();

    act(() => {
      handlers[0]?.onEvent({
        sequence: 2,
        kind: "update",
        fact: { goal: goalValue("server paused objective", "paused"), availability: "available" },
      });
    });
    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
  });

  it.each([false, true])(
    "preserves later drafts on Save success after leaving the editor: %s",
    async (leaveEditor) => {
      const services = createTestServices([]);
      const saveResult = deferred<GoalSetResult>();
      const subscribeGoal: GoalRegister = (_target, handler) => {
        queueMicrotask(() => {
          handler.onEvent(hydration({ objective: "saved objective", status: "active" }));
        });
        return { close: vi.fn() };
      };
      const api = createGoalSidebarApi({
        goal: goalValue("saved objective", "active"),
        setGoal: vi.fn(async () => saveResult.promise),
        subscribeGoal,
      });
      render(
        <TestAppProviders skipAnimations services={services}>
          <GoalSidebarPage input={{ kind: "session", api, target }} />
          <button>Outside the Goal editor</button>
        </TestAppProviders>,
      );

      const user = userEvent.setup();
      const editor = await enterGoalEditor(user);
      await user.clear(editor);
      await user.type(editor, "submitted objective");
      await user.click(screen.getByTestId("goal-save"));

      const laterEditor = await enterGoalEditor(user);
      const postSubmitDraft = "new unsaved objective";
      await user.clear(laterEditor);
      await user.type(laterEditor, postSubmitDraft);
      if (leaveEditor) await user.click(screen.getByRole("button", { name: "Outside the Goal editor" }));
      await act(async () => {
        saveResult.resolve({
          sessionID,
          outcome: {
            kind: "mutation",
            mutation: {
              kind: "authoritative_goal",
              fact: { goal: goalValue("submitted objective", "active"), availability: "available" },
            },
            diagnostic: null,
          },
        });
        await saveResult.promise;
      });

      if (leaveEditor) {
        expect(await screen.findByText(postSubmitDraft)).toBeInTheDocument();
        expect(screen.getByRole("textbox", { name: "Goal" })).toHaveAttribute("aria-readonly");
      } else {
        expect(await screen.findByDisplayValue(postSubmitDraft)).toHaveFocus();
      }
      expect(screen.getByTestId("goal-save")).toBeInTheDocument();
    },
  );

  it.each(["success", "failure"] as const)(
    "preserves later draft and preview after Pause %s",
    async (outcome) => {
      const pending = deferred<Awaited<ReturnType<GoalSidebarApi["pauseGoal"]>>>();
      const api = createGoalSidebarApi({
        goal: goalValue("saved objective", "active"),
        pauseGoal: vi.fn(async () => pending.promise),
      });
      render(
        <TestAppProviders skipAnimations services={createTestServices([])}>
          <GoalSidebarPage input={{ kind: "session", api, target }} />
          <button>Outside the Goal editor</button>
        </TestAppProviders>,
      );
      const user = userEvent.setup();
      const editor = await enterGoalEditor(user);
      await user.clear(editor);
      await user.type(editor, "unsaved before Pause");
      await user.click(screen.getByRole("button", { name: "Pause" }));
      const laterEditor = await enterGoalEditor(user);
      const latestDraft = "later lifecycle draft";
      await user.clear(laterEditor);
      await user.type(laterEditor, latestDraft);
      await user.click(screen.getByRole("button", { name: "Outside the Goal editor" }));
      await act(async () => {
        if (outcome === "success") {
          pending.resolve({
            kind: "authoritative_goal",
            fact: { goal: goalValue("saved objective", "paused"), availability: "available" },
          });
        } else {
          pending.reject(new Error("Pause failed."));
        }
        await pending.promise.catch(() => undefined);
      });
      expect(await screen.findByText(latestDraft)).toBeInTheDocument();
      expect(screen.getByRole("textbox", { name: "Goal" })).toHaveAttribute("aria-readonly");
      expect(await screen.findByRole("button", { name: "Pause" })).toBeEnabled();
    },
  );

  it("reopens and focuses the latest draft after a failed Save even after leaving the editor", async () => {
    const saveResult = deferred<GoalSetResult>();
    const api = createGoalSidebarApi({
      goal: goalValue("saved objective", "active"),
      setGoal: vi.fn(async () => saveResult.promise),
    });
    render(
      <TestAppProviders skipAnimations services={createTestServices([])}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
        <button>Outside the Goal editor</button>
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    const editor = await enterGoalEditor(user);
    await user.clear(editor);
    await user.type(editor, "submitted objective");
    await user.click(screen.getByTestId("goal-save"));
    const laterEditor = await enterGoalEditor(user);
    const latestDraft = "later unsaved objective";
    await user.clear(laterEditor);
    await user.type(laterEditor, latestDraft);
    await user.click(screen.getByRole("button", { name: "Outside the Goal editor" }));
    expect(screen.getByRole("textbox", { name: "Goal" })).toHaveAttribute("aria-readonly");
    await act(async () => {
      saveResult.reject(new Error("Save failed."));
      await saveResult.promise.catch(() => undefined);
    });
    expect(await screen.findByDisplayValue(latestDraft)).toHaveFocus();
    expect(screen.getByTestId("goal-save")).toBeEnabled();
  });

  it("restores a failed Save draft against the latest subscribed Goal", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const saveResult = deferred<Awaited<ReturnType<GoalSidebarApi["setGoal"]>>>();
    const subscribeGoal: GoalRegister = (_target, handler) => {
      handlers.push(handler);
      queueMicrotask(() => {
        handler.onEvent(hydration({ objective: "saved objective", status: "active" }));
      });
      return { close: vi.fn() };
    };
    const api = createGoalSidebarApi({
      goal: goalValue("saved objective", "active"),
      setGoal: vi.fn(async () => saveResult.promise),
      subscribeGoal,
    });
    render(
      <TestAppProviders skipAnimations services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    const editor = await enterGoalEditor(user);
    const draft = "restore this draft";
    await user.clear(editor);
    await user.type(editor, draft);
    await user.click(screen.getByTestId("goal-save"));
    act(() => {
      handlers[0]?.onEvent({
        sequence: 2,
        kind: "update",
        fact: { goal: goalValue("new authority", "paused"), availability: "available" },
      });
    });
    saveResult.reject(new Error("Save failed."));

    const restored = await screen.findByDisplayValue(draft);
    expect(restored).toHaveFocus();
    expect(screen.getByRole("button", { name: "Resume" })).toBeInTheDocument();
  });

  it("keeps a dirty New Chat draft after a Session-bearing rejection", async () => {
    const services = createTestServices([]);
    const hostDelivery = vi.fn<(delivery: NewChatGoalHostDelivery) => void>();
    const result: GoalSetResult = {
      sessionID,
      outcome: {
        kind: "rejected",
        error: new ChatOperationError(
          new RpcError({ code: "internal_failure", message: "Goal Set failed", method: "runtime.goal.set" }),
          { kind: "runtime_unavailable", sessionID },
        ),
      },
    };
    const api = createGoalSidebarApi({ goal: null });
    const owner = createGoalFixtureOwner({
      api: { setGoal: vi.fn(async () => result) },
      target: newChatTarget(),
    });
    render(
      <TestAppProviders skipAnimations services={services}>
        <GoalFixturePage api={api} owner={owner} delivered={hostDelivery} />
      </TestAppProviders>,
    );

    const objective = "Preserve this rejected draft";
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));

    expect(await screen.findByDisplayValue(objective)).toBeInTheDocument();
    expect(screen.getByTestId("goal-save")).toBeInTheDocument();
    expect(hostDelivery).toHaveBeenCalledTimes(1);
    expect(hostDelivery).toHaveBeenCalledWith({
      target: exactTarget(),
      origin: newChatTarget(),
      goal: null,
    });
  });

  it("keeps New Chat Save disabled for an unsupported Agent", async () => {
    const services = createTestServices([]);
    const setGoal = vi.fn(async () => {
      throw new Error("Unsupported Agent must not send Goal Set.");
    });
    const api = createGoalSidebarApi({ goal: null, setGoal });
    const owner = createGoalFixtureOwner({
      api: { setGoal },
      target: newChatTarget(),
      unsupported: true,
    });
    render(
      <TestAppProviders skipAnimations services={services}>
        <GoalFixturePage api={api} owner={owner} delivered={() => undefined} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "unsupported Agent Goal");

    const save = screen.getByTestId("goal-save");
    expect(save).toBeDisabled();
    expect(save).toHaveAccessibleName("Unavailable for this Agent");
    await user.click(save);
    expect(setGoal).not.toHaveBeenCalled();
  });

  it("delivers New Chat Session/Goal before one warning while the sidebar is Loading", async () => {
    const services = createTestServices([]);
    const order: string[] = [];
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation((value) => {
      if (value.tone === "warning") order.push("warning");
    });
    const hostDelivery = vi.fn<(delivery: NewChatGoalHostDelivery) => void>(() => {
      order.push("host");
    });
    const subscribeGoal: GoalRegister = () => ({ close: vi.fn() });
    const api = createGoalSidebarApi({
      goal: null,
      subscribeGoal,
    });
    const owner = createGoalFixtureOwner({
      api: { setGoal: vi.fn(async () => goalSetResult("New Chat Goal", "goal-new-chat")) },
      target: newChatTarget(),
    });
    render(
      <TestAppProviders skipAnimations services={services}>
        <GoalFixturePage api={api} owner={owner} delivered={hostDelivery} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "New Chat Goal");
    await user.click(screen.getByTestId("goal-save"));

    await waitFor(() => {
      expect(notice.mock.calls.filter(([value]) => value.tone === "warning")).toHaveLength(1);
    });
    expect(order).toEqual(["host", "warning"]);
    const delivered = hostDelivery.mock.calls[0]?.[0];
    expect(delivered?.target).toEqual(exactTarget());
    expect(delivered?.goal).toMatchObject({ objective: "New Chat Goal", status: "active" });
    expect(screen.getByTestId("loading-state")).toBeInTheDocument();
  });

  it("delivers New Chat completion after closure and isolates a replacement opening", async () => {
    const services = createTestServices([]);
    const pending = deferred<GoalSetResult>();
    const hostDelivery = vi.fn<(delivery: NewChatGoalHostDelivery) => void>();
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const subscribeGoal: GoalRegister = (_target, handler) => {
      queueMicrotask(() => {
        handler.onEvent(hydration({ objective: "hydrated replacement" }));
      });
      return { close: vi.fn() };
    };
    const api = createGoalSidebarApi({
      goal: null,
      subscribeGoal,
    });
    const owner = createGoalFixtureOwner({
      api: { setGoal: vi.fn(async () => pending.promise) },
      target: newChatTarget(),
    });
    const view = render(
      <TestAppProviders skipAnimations services={services}>
        <GoalFixturePage api={api} owner={owner} delivered={hostDelivery} />
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "close-safe success");
    await user.click(screen.getByTestId("goal-save"));
    // Closing a sidebar does not close its window or replace the window's Atom registry.
    view.rerender(
      <TestAppProviders skipAnimations services={services}>
        {null}
      </TestAppProviders>,
    );

    view.rerender(
      <TestAppProviders skipAnimations services={services}>
        <GoalFixturePage api={api} owner={owner} delivered={hostDelivery} />
      </TestAppProviders>,
    );
    await act(async () => {
      pending.resolve(goalSetResult("close-safe success", "goal-close-safe"));
      await pending.promise;
    });

    await waitFor(() => {
      expect(notice).toHaveBeenCalledTimes(1);
    });
    expect(hostDelivery).toHaveBeenCalledTimes(1);
    expect(hostDelivery).toHaveBeenCalledWith({
      target: exactTarget(),
      origin: newChatTarget(),
      goal: goalValue("close-safe success", "active", "goal-close-safe"),
    });
    expect(await screen.findByText("hydrated replacement")).toBeInTheDocument();
    expect(screen.queryByText("close-safe success")).not.toBeInTheDocument();
    view.unmount();
  });
});

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Value>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function newChatTarget() {
  return {
    kind: "new_chat" as const,
    projectID: "project-1",
    workspaceID: "workspace-1",
    initialSettings: {
      agentRole: "default" as const,
      supervisor: "off" as const,
      thinking: null,
      fast: null,
      questionsEnabled: true,
      autoCompactionEnabled: true,
    },
    initialInputDraft: "composer draft",
  };
}

function exactTarget(): ChatSessionTarget {
  return {
    projectID: "project-1",
    sessionID,
  };
}

function goalSetResult(objective: string, id: string): GoalSetResult {
  return {
    sessionID,
    outcome: {
      kind: "mutation",
      mutation: {
        kind: "authoritative_goal",
        fact: {
          goal: goalValue(objective, "active", id),
          availability: "available",
        },
      },
      diagnostic: new ChatOperationError(
        new RpcError({ code: "internal_failure", message: "Fixture warning", method: "runtime.detach" }),
        { kind: "internal_failure", operation: "runtime.detach", cause: "fixture warning" },
      ),
    },
  };
}

function goalValue(objective: string, status: "active" | "paused" | "complete", id = "goal-1") {
  return {
    id,
    objective,
    status,
    createdAt: "2026-09-12T10:00:00Z",
    updatedAt: "2026-09-12T10:00:00Z",
  } as const;
}

function hydration({
  objective,
  status = "active",
}: Readonly<{ objective: string; status?: "active" | "paused" | "complete" }>): GoalObservation {
  return {
    sequence: 1,
    kind: "hydration",
    fact: { goal: goalValue(objective, status), availability: "available" },
  };
}

type GoalRegister = (target: ChatSessionTarget, handler: ChatGoalObservationHandler) => { close(): void };

function createGoalSidebarApi({
  goal,
  pauseGoal = vi.fn(async () => {
    throw new Error("Unexpected Goal mutation.");
  }),
  setGoal = vi.fn(async () => {
    throw new Error("Unexpected Goal mutation.");
  }),
  subscribeGoal,
}: Readonly<{
  goal: GoalFact["goal"];
  pauseGoal?: GoalSidebarApi["pauseGoal"];
  setGoal?: GoalSidebarApi["setGoal"];
  subscribeGoal?: GoalRegister;
}>): GoalSidebarApi {
  return {
    clearGoal: vi.fn(async () => {
      throw new Error("Unexpected Goal mutation.");
    }),
    pauseGoal,
    resumeGoal: vi.fn(async () => {
      throw new Error("Unexpected Goal mutation.");
    }),
    setGoal,
    subscribeGoal: (selected) =>
      goalSource((handler) =>
        subscribeGoal !== undefined
          ? subscribeGoal(selected, handler)
          : (() => {
              queueMicrotask(() => {
                handler.onEvent({
                  sequence: 1,
                  kind: "hydration",
                  fact: { goal, availability: "available" },
                });
              });
              return { close: vi.fn() };
            })(),
      ),
  };
}
