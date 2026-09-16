import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";

import type { ChatGoalObservationHandler, ChatSessionTarget } from "@/api";
import { ChatOperationError, RpcError } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import * as ui from "@/ui";
import { NewChatGoalBinding, type NewChatGoalHostDelivery } from "./goalBinding";
import { GoalSidebarPage, type GoalSidebarApi } from "./GoalSidebar";

type GoalFact = Parameters<ChatGoalObservationHandler["onEvent"]>[0]["fact"];
type GoalObservation = Parameters<ChatGoalObservationHandler["onEvent"]>[0];
type GoalSetResult = Awaited<ReturnType<GoalSidebarApi["setGoal"]>>;

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID,
} as const;

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Goal sidebar", () => {
  it("owns a fresh subscription for every StrictMode opening", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const closes: ReturnType<typeof vi.fn>[] = [];
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = (_target, handler) => {
      handlers.push(handler);
      const close = vi.fn();
      closes.push(close);
      return { close };
    };
    const api = createGoalSidebarApi({ goal: null, subscribeGoal });
    const view = render(
      <TestAppProviders services={services}>
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
    expect(closes.slice(0, firstOpeningCount).every((close) => close.mock.calls.length === 1)).toBe(true);

    const { unmount: unmountReopened } = render(
      <TestAppProviders services={services}>
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
    expect(closes.every((close) => close.mock.calls.length === 1)).toBe(true);
  });

  it("stops after an observed subscription failure and retries only explicitly", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const closes: ReturnType<typeof vi.fn>[] = [];
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = (_target, handler) => {
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
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );

    await waitFor(() => {
      expect(handlers).toHaveLength(1);
    });
    act(() => {
      handlers[0]?.onEvent(hydration({ objective: "saved objective", status: "active" }));
      handlers[0]?.onError(new Error("Goal connection lost."));
    });

    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(handlers).toHaveLength(1);
    expect(closes[0]).toHaveBeenCalledOnce();

    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => {
      expect(handlers).toHaveLength(2);
    });
    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();

    act(() => {
      handlers[1]?.onEvent(hydration({ objective: "recovered objective", status: "paused" }));
    });
    expect(await screen.findByText("recovered objective")).toBeInTheDocument();
  });

  it("uses subscription-only saved state after a successful action", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const pauseResult = deferred<Awaited<ReturnType<GoalSidebarApi["pauseGoal"]>>>();
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = (_target, handler) => {
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
      <TestAppProviders services={services}>
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

  it("preserves a draft edited after Save begins when Save succeeds", async () => {
    const services = createTestServices([]);
    const saveResult = deferred<GoalSetResult>();
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = (_target, handler) => {
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
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole("textbox", { name: "Goal" }));
    await user.clear(screen.getByRole("textbox", { name: "Goal" }));
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "submitted objective");
    await user.click(screen.getByTestId("goal-save"));

    await user.click(await screen.findByRole("textbox", { name: "Goal" }));
    const postSubmitDraft = "new unsaved objective";
    await user.clear(screen.getByRole("textbox", { name: "Goal" }));
    await user.type(screen.getByRole("textbox", { name: "Goal" }), postSubmitDraft);
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

    expect(await screen.findByDisplayValue(postSubmitDraft)).toHaveFocus();
    expect(screen.getByTestId("goal-save")).toBeInTheDocument();
  });

  it("restores a failed Save draft against the latest subscribed Goal", async () => {
    const services = createTestServices([]);
    const handlers: ChatGoalObservationHandler[] = [];
    const saveResult = deferred<Awaited<ReturnType<GoalSidebarApi["setGoal"]>>>();
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = (_target, handler) => {
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
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    await user.click(await screen.findByRole("textbox", { name: "Goal" }));
    const draft = "restore this draft";
    await user.clear(screen.getByRole("textbox", { name: "Goal" }));
    await user.type(screen.getByRole("textbox", { name: "Goal" }), draft);
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
          new RpcError({ code: 500, message: "Goal Set failed", method: "runtime.goal.set" }),
          { kind: "runtime_unavailable", sessionID },
        ),
      },
    };
    const api = createGoalSidebarApi({ goal: null });
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => result) },
      captureTarget: () => newChatTarget(),
      onHostDelivery: hostDelivery,
    });
    render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api, binding }} />
      </TestAppProviders>,
    );

    const objective = "Preserve this rejected draft";
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));

    expect(await screen.findByDisplayValue(objective)).toBeInTheDocument();
    expect(screen.getByTestId("goal-save")).toBeInTheDocument();
    expect(hostDelivery).toHaveBeenCalledExactlyOnceWith({
      target: exactTarget(),
      goal: null,
    });
  });

  it("keeps New Chat Save disabled for an unsupported Agent", async () => {
    const services = createTestServices([]);
    const setGoal = vi.fn(async () => {
      throw new Error("Unsupported Agent must not send Goal Set.");
    });
    const api = createGoalSidebarApi({ goal: null, setGoal });
    const binding = new NewChatGoalBinding({
      api: { setGoal },
      captureTarget: () => newChatTarget(),
      onHostDelivery: () => undefined,
    });
    binding.setAvailability("agent_capability_missing");
    render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api, binding }} />
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
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = () => ({ close: vi.fn() });
    const api = createGoalSidebarApi({
      goal: null,
      subscribeGoal,
    });
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => goalSetResult("New Chat Goal", "goal-new-chat")) },
      captureTarget: () => newChatTarget(),
      onHostDelivery: hostDelivery,
    });
    render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api, binding }} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "New Chat Goal");
    await user.click(screen.getByTestId("goal-save"));

    await waitFor(() => {
      expect(notice.mock.calls.filter(([value]) => value.tone === "warning")).toHaveLength(1);
    });
    expect(order).toEqual(["host", "warning"]);
    expect(binding.snapshot).toEqual({ kind: "resolved_session", target: exactTarget() });
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
    const subscribeGoal: GoalSidebarApi["subscribeGoal"] = (_target, handler) => {
      queueMicrotask(() => {
        handler.onEvent(hydration({ objective: "hydrated replacement" }));
      });
      return { close: vi.fn() };
    };
    const api = createGoalSidebarApi({
      goal: null,
      subscribeGoal,
    });
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => pending.promise) },
      captureTarget: () => newChatTarget(),
      onHostDelivery: hostDelivery,
    });
    const view = render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api, binding }} />
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "close-safe success");
    await user.click(screen.getByTestId("goal-save"));
    view.unmount();

    const { unmount: unmountReplacement } = render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api, binding }} />
      </TestAppProviders>,
    );
    await act(async () => {
      pending.resolve(goalSetResult("close-safe success", "goal-close-safe"));
      await pending.promise;
    });

    await waitFor(() => {
      expect(notice).toHaveBeenCalledOnce();
    });
    expect(hostDelivery).toHaveBeenCalledExactlyOnceWith({
      target: exactTarget(),
      goal: goalValue("close-safe success", "active", "goal-close-safe"),
    });
    expect(await screen.findByText("hydrated replacement")).toBeInTheDocument();
    expect(screen.queryByText("close-safe success")).not.toBeInTheDocument();
    unmountReplacement();
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
    workspace: { workspaceID: "workspace-1" },
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
        new RpcError({ code: 500, message: "Fixture warning", method: "runtime.detach" }),
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
  subscribeGoal?: GoalSidebarApi["subscribeGoal"];
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
    subscribeGoal:
      subscribeGoal ??
      ((_target, handler) => {
        queueMicrotask(() => {
          handler.onEvent({
            sequence: 1,
            kind: "hydration",
            fact: { goal, availability: "available" },
          });
        });
        return { close: vi.fn() };
      }),
  };
}
