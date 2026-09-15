import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";

import { ChatOperationError, RpcError } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import * as ui from "@/ui";
import { createNewChatGoalResource, NewChatGoalBinding } from "./goalBinding";
import { GoalSidebarPage, type GoalSidebarApi } from "./GoalSidebar";

type GoalObservationHandler = Parameters<GoalSidebarApi["subscribeGoal"]>[1];
type GoalSubscription = ReturnType<GoalSidebarApi["subscribeGoal"]>;
type GoalSetResult = Awaited<ReturnType<GoalSidebarApi["setGoal"]>>;
type GoalFact = Parameters<GoalObservationHandler["onEvent"]>[0]["fact"];
type ChatError = ConstructorParameters<typeof ChatOperationError>[1];
const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID,
} as const;

function goalSetError(detail: ChatError): ChatOperationError {
  return new ChatOperationError(
    new RpcError({ code: 500, message: "Goal Set failed", method: "runtime.goal.set" }),
    detail,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Goal sidebar", () => {
  it("creates a fresh live destination after StrictMode effect replay", async () => {
    const services = createTestServices([]);
    const handlers: GoalObservationHandler[] = [];
    const closes: ReturnType<typeof vi.fn>[] = [];
    const api = {
      ...createGoalSidebarApi({ goal: null }),
      subscribeGoal: vi.fn((_target: typeof target, handler: GoalObservationHandler): GoalSubscription => {
        handlers.push(handler);
        const close = vi.fn();
        closes.push(close);
        return { close };
      }),
    };
    const { unmount } = render(
      <TestAppProviders services={services}>
        <StrictMode>
          <GoalSidebarPage input={{ kind: "session", api, target }} />
        </StrictMode>
      </TestAppProviders>,
    );

    await waitFor(() => {
      expect(api.subscribeGoal).toHaveBeenCalled();
    });
    const firstSubscriptionCount = handlers.length;
    unmount();
    expect(closes.slice(0, firstSubscriptionCount).every((close) => close.mock.calls.length === 1)).toBe(
      true,
    );

    const { unmount: unmountReopened } = render(
      <TestAppProviders services={services}>
        <StrictMode>
          <GoalSidebarPage input={{ kind: "session", api, target }} />
        </StrictMode>
      </TestAppProviders>,
    );
    await waitFor(() => {
      expect(handlers.length).toBeGreaterThan(firstSubscriptionCount);
    });
    act(() => {
      for (const handler of handlers.slice(0, -1)) {
        handler.onEvent({
          sequence: 1,
          kind: "hydration",
          fact: {
            goal: {
              id: "stale-goal",
              objective: "stale destination",
              status: "active",
              createdAt: "2026-09-12T10:00:00Z",
              updatedAt: "2026-09-12T10:00:00Z",
            },
            availability: "available",
          },
        });
      }
      handlers.at(-1)?.onEvent({
        sequence: 1,
        kind: "hydration",
        fact: {
          goal: {
            id: "live-goal",
            objective: "live destination",
            status: "active",
            createdAt: "2026-09-12T10:00:00Z",
            updatedAt: "2026-09-12T10:00:00Z",
          },
          availability: "available",
        },
      });
    });

    expect(await screen.findByText("live destination")).toBeInTheDocument();
    expect(screen.queryByText("stale destination")).not.toBeInTheDocument();
    unmountReopened();
    expect(closes.at(-1)?.mock.calls.length).toBe(1);
  });

  it("keeps a dirty New Chat draft after a Session-bearing Goal rejection", async () => {
    const services = createTestServices([]);
    const api = createGoalSidebarApi({ goal: null });
    const result: GoalSetResult = {
      sessionID,
      outcome: {
        kind: "rejected",
        error: goalSetError({ kind: "runtime_unavailable", sessionID }),
      },
    };
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => result) },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
      }),
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
  });

  it("applies a New Chat authoritative Goal before warning while hydration is pending", async () => {
    const services = createTestServices([]);
    let goalVisibleWhenWarningWasShown = false;
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation((value) => {
      if (value.tone === "warning") {
        goalVisibleWhenWarningWasShown = screen.queryByText("New Chat Goal") !== null;
      }
    });
    const result: GoalSetResult = {
      sessionID,
      outcome: {
        kind: "mutation",
        mutation: {
          kind: "authoritative_goal",
          fact: {
            goal: {
              id: "goal-new-chat",
              objective: "New Chat Goal",
              status: "active",
              createdAt: "2026-09-12T10:00:00Z",
              updatedAt: "2026-09-12T10:00:00Z",
            },
            availability: "available",
          },
        },
        diagnostic: goalSetError({
          kind: "internal_failure",
          operation: "runtime.detach",
          cause: "release failed",
        }),
      },
    };
    const api: GoalSidebarApi = {
      clearGoal: vi.fn(async () => {
        throw new Error("Unexpected Goal mutation.");
      }),
      pauseGoal: vi.fn(async () => {
        throw new Error("Unexpected Goal mutation.");
      }),
      resumeGoal: vi.fn(async () => {
        throw new Error("Unexpected Goal mutation.");
      }),
      setGoal: vi.fn(async () => result),
      subscribeGoal: vi.fn((): GoalSubscription => ({ close: vi.fn() })),
    };
    const binding = new NewChatGoalBinding({
      api: { setGoal: api.setGoal },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
      }),
    });
    const resource = createNewChatGoalResource();
    binding.registerResource(resource);
    render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api, binding, resource }} />
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "New Chat Goal");
    await user.click(screen.getByTestId("goal-save"));

    await waitFor(() => {
      expect(notice.mock.calls.filter(([value]) => value.tone === "warning")).toHaveLength(1);
    });
    expect(screen.getByText("New Chat Goal")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(goalVisibleWhenWarningWasShown).toBe(true);
  });

  it("delivers a committed New Chat Goal warning after the sidebar closes", async () => {
    const services = createTestServices([]);
    const pending = deferred<GoalSetResult>();
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => pending.promise) },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
      }),
    });
    const view = render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api: services.api.chat, binding }} />
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "close-safe success");
    await user.click(screen.getByTestId("goal-save"));
    view.unmount();

    await act(async () => {
      pending.resolve(goalSetResult("close-safe success", "goal-close-safe"));
      await pending.promise;
    });

    await waitFor(() => {
      expect(notice).toHaveBeenCalledOnce();
    });
    expect(notice.mock.lastCall?.[0].tone).toBe("warning");
    expect(binding.snapshot).toEqual({
      kind: "resolved_session",
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID,
      },
    });
  });

  it("applies a committed Goal before showing one non-failure diagnostic warning", async () => {
    let goalVisibleWhenWarningWasShown = false;
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation((value) => {
      if (value.tone === "warning") {
        goalVisibleWhenWarningWasShown = screen.queryByRole("button", { name: "Pause" }) !== null;
      }
    });
    const api = createGoalSidebarApi({
      goal: null,
      setGoal: vi.fn(async () => ({
        sessionID,
        outcome: {
          kind: "mutation" as const,
          mutation: {
            kind: "authoritative_goal" as const,
            fact: {
              goal: {
                id: "goal-1",
                objective: "Save this Goal",
                status: "active" as const,
                createdAt: "2026-09-12T10:00:00Z",
                updatedAt: "2026-09-12T10:00:00Z",
              },
              availability: "available" as const,
            },
          },
          diagnostic: goalSetError({
            kind: "internal_failure",
            operation: "runtime.detach",
            cause: "release failed",
          }),
        },
      })),
    });
    mountExactGoal(api);

    const user = userEvent.setup();
    await user.type(await screen.findByRole("textbox", { name: "Goal" }), "Save this Goal");
    await user.click(screen.getByTestId("goal-save"));

    await waitFor(() => {
      expect(notice).toHaveBeenCalledOnce();
    });
    expect(notice.mock.lastCall?.[0].tone).toBe("warning");
    expect(goalVisibleWhenWarningWasShown).toBe(true);
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

function mountExactGoal(api: GoalSidebarApi) {
  const services = createTestServices([]);
  render(
    <TestAppProviders services={services}>
      <GoalSidebarPage input={{ kind: "session", api, target }} />
    </TestAppProviders>,
  );
}

function goalSetResult(objective: string, id: string, resultSessionID: string = sessionID): GoalSetResult {
  return {
    sessionID: resultSessionID,
    outcome: {
      kind: "mutation",
      mutation: {
        kind: "authoritative_goal",
        fact: {
          goal: {
            id,
            objective,
            status: "active",
            createdAt: "2026-09-12T10:00:00Z",
            updatedAt: "2026-09-12T10:00:00Z",
          },
          availability: "available",
        },
      },
      diagnostic: goalSetError({
        kind: "internal_failure",
        operation: "runtime.detach",
        cause: "release failed",
      }),
    },
  };
}

function createGoalSidebarApi({
  goal,
  setGoal = vi.fn(async () => {
    throw new Error("Unexpected Goal mutation.");
  }),
  resumeGoal = vi.fn(async () => {
    throw new Error("Unexpected Goal mutation.");
  }),
}: Readonly<{
  goal: GoalFact["goal"];
  setGoal?: GoalSidebarApi["setGoal"];
  resumeGoal?: GoalSidebarApi["resumeGoal"];
}>): GoalSidebarApi &
  Readonly<{ emit?: (observation: Parameters<GoalObservationHandler["onEvent"]>[0]) => void }> {
  let handler: GoalObservationHandler | null = null;
  const api: GoalSidebarApi &
    Readonly<{
      emit?: (observation: Parameters<GoalObservationHandler["onEvent"]>[0]) => void;
    }> = {
    clearGoal: vi.fn(async () => {
      throw new Error("Unexpected Goal mutation.");
    }),
    pauseGoal: vi.fn(async () => {
      throw new Error("Unexpected Goal mutation.");
    }),
    resumeGoal,
    setGoal,
    subscribeGoal: (_target, nextHandler): GoalSubscription => {
      handler = nextHandler;
      queueMicrotask(() => {
        nextHandler.onEvent({
          sequence: 1,
          kind: "hydration",
          fact: { goal, availability: "available" },
        });
      });
      return {
        close: () => {
          if (handler === nextHandler) {
            handler = null;
          }
        },
      };
    },
    emit: (observation) => {
      handler?.onEvent(observation);
    },
  };
  return api;
}
