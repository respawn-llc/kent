import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type {
  ApiSubscription,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalObservationHandler,
  ChatGoalSetResult,
  ChatError,
} from "@/api";
import { ChatOperationError, RpcError, TransportError } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import * as ui from "@/ui";
import { NewChatGoalBinding } from "./goalBinding";
import { GoalSidebarPage, type GoalSidebarApi } from "./GoalSidebar";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID,
} as const;
const activeGoal = {
  id: "goal-1",
  objective: "Ship the Goal sidebar",
  status: "active",
  created_at: "2026-09-11T10:00:00Z",
  updated_at: "2026-09-11T10:00:00Z",
};

function goalSetError(detail: ChatError): ChatOperationError {
  return new ChatOperationError(
    new RpcError({ code: 500, message: "Goal Set failed", method: "runtime.goal.set" }),
    detail,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
});

function mountGoal() {
  const services = createTestServices([
    {
      method: "runtime.goal.pause",
      result: {
        result: {
          kind: "authoritative_goal",
          goal: { ...activeGoal, status: "paused" },
          availability: null,
        },
      },
    },
  ]);
  render(
    <TestAppProviders services={services}>
      <GoalSidebarPage input={{ kind: "session", api: services.api.chat, target }} />
    </TestAppProviders>,
  );
  return services;
}

describe("Goal sidebar", () => {
  it("starts with Loading and then presents authoritative Goal metadata", async () => {
    const testServices = mountGoal();
    expect(screen.getByTestId("loading-state-placeholder")).toBeInTheDocument();

    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: activeGoal, availability: "available" },
        },
      });
    });

    expect(await screen.findByText("Ship the Goal sidebar")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear" })).toBeInTheDocument();
  });

  it("routes lifecycle actions through the destination controller and updates state", async () => {
    const testServices = mountGoal();
    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: activeGoal, availability: "available" },
        },
      });
    });
    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();

    await userEvent.setup().click(screen.getByRole("button", { name: "Pause" }));

    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
    expect(screen.getByText("Paused")).toBeInTheDocument();
  });

  it("shows a compact retry state when Goal observation fails", async () => {
    const testServices = mountGoal();
    await screen.findByTestId("loading-state");
    await waitFor(() => {
      expect(testServices.transport.subscriptions).toHaveLength(1);
    });
    act(() => {
      testServices.transport.fail("goal.observe", new TransportError("Goal observation failed."));
    });

    await waitFor(() => {
      expect(testServices.transport.subscriptions).toHaveLength(0);
    });
    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));

    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: activeGoal, availability: "available" },
        },
      });
    });
    expect(await screen.findByText("Ship the Goal sidebar")).toBeInTheDocument();
  });

  it("keeps a dirty New Chat draft after a Session-bearing Goal rejection", async () => {
    const testServices = createTestServices([]);
    const result: ChatGoalSetResult = {
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
      <TestAppProviders services={testServices}>
        <GoalSidebarPage input={{ kind: "new_chat", api: testServices.api.chat, binding }} />
      </TestAppProviders>,
    );

    const objective = "Preserve this rejected draft";
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));

    await screen.findByTestId("loading-state");
    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: null, availability: "available" },
        },
      });
    });

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
    const result: ChatGoalSetResult = {
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
      subscribeGoal: vi.fn((): ApiSubscription => ({ close: vi.fn() })),
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
    expect(screen.getByText("New Chat Goal")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(goalVisibleWhenWarningWasShown).toBe(true);
  });

  it("notifies a New Chat Goal failure after the sidebar closes", async () => {
    const services = createTestServices([]);
    const pending = deferred<ChatGoalSetResult>();
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
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "close-safe failure");
    await user.click(screen.getByTestId("goal-save"));
    view.unmount();

    await act(async () => {
      pending.reject(new TransportError("connection loss"));
      await pending.promise.catch(() => undefined);
    });

    expect(notice).toHaveBeenCalledOnce();
    expect(notice.mock.lastCall?.[0].tone).toBe("danger");
  });

  it("reports a committed Set diagnostic after the exact Goal sidebar closes", async () => {
    const services = createTestServices([]);
    const pending = deferred<ChatGoalSetResult>();
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const api = createGoalSidebarApi({
      goal: null,
      setGoal: vi.fn(async () => pending.promise),
    });
    const view = render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "session", api, target }} />
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    await user.type(await screen.findByRole("textbox", { name: "Goal" }), "close-safe success");
    await user.click(screen.getByTestId("goal-save"));
    view.unmount();

    await act(async () => {
      pending.resolve({
        sessionID,
        outcome: {
          kind: "mutation",
          mutation: {
            kind: "authoritative_goal",
            fact: {
              goal: {
                id: "goal-close-safe",
                objective: "close-safe success",
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
      });
      await pending.promise;
    });

    await waitFor(() => {
      expect(notice).toHaveBeenCalledOnce();
    });
    expect(notice.mock.lastCall?.[0].tone).toBe("warning");
  });

  it("presents first exact-Session Save as Active without a timestamp while pending", async () => {
    const pending = deferred<ChatGoalSetResult>();
    const setGoal = vi.fn(async () => pending.promise);
    const api = createGoalSidebarApi({ goal: null, setGoal });
    mountExactGoal(api);

    const user = userEvent.setup();
    await user.type(await screen.findByRole("textbox", { name: "Goal" }), "Create this Goal");
    await user.click(screen.getByTestId("goal-save"));

    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByLabelText("Active")).toBeInTheDocument();
    expect(screen.queryByTestId("goal-set-time")).not.toBeInTheDocument();

    pending.resolve({
      sessionID,
      outcome: {
        kind: "mutation",
        mutation: {
          kind: "authoritative_goal",
          fact: {
            goal: {
              id: "goal-created",
              objective: "Create this Goal",
              status: "active",
              createdAt: "2026-09-12T10:00:00Z",
              updatedAt: "2026-09-12T10:00:00Z",
            },
            availability: "available",
          },
        },
        diagnostic: null,
      },
    });
    await waitFor(() => {
      expect(screen.getByTestId("goal-set-time")).toHaveAttribute("dateTime", "2026-09-12T10:00:00Z");
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

  it("shows both warnings after overlapping Goals are applied", async () => {
    const services = createTestServices([]);
    const first = deferred<ChatGoalSetResult>();
    const second = deferred<ChatGoalSetResult>();
    const warningGoalCounts: number[] = [];
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation((value) => {
      if (value.tone === "warning") {
        warningGoalCounts.push(screen.getAllByRole("button", { name: "Pause" }).length);
      }
    });
    const firstApi = createGoalSidebarApi({ goal: null, setGoal: vi.fn(async () => first.promise) });
    const secondApi = createGoalSidebarApi({ goal: null, setGoal: vi.fn(async () => second.promise) });
    render(
      <TestAppProviders services={services}>
        <div className="grid grid-cols-2">
          <div data-testid="first-goal">
            <GoalSidebarPage input={{ kind: "session", api: firstApi, target }} />
          </div>
          <div data-testid="second-goal">
            <GoalSidebarPage
              input={{
                kind: "session",
                api: secondApi,
                target: { ...target, sessionID: "223e4567-e89b-42d3-a456-426614174000" },
              }}
            />
          </div>
        </div>
      </TestAppProviders>,
    );

    const user = userEvent.setup();
    const firstGoal = within(screen.getByTestId("first-goal"));
    const secondGoal = within(screen.getByTestId("second-goal"));
    await user.click(await firstGoal.findByRole("textbox", { name: "Goal" }));
    await user.type(firstGoal.getByRole("textbox", { name: "Goal" }), "First overlapping Goal");
    await user.click(firstGoal.getByTestId("goal-save"));
    await user.click(await secondGoal.findByRole("textbox", { name: "Goal" }));
    await user.type(secondGoal.getByRole("textbox", { name: "Goal" }), "Second overlapping Goal");
    await user.click(secondGoal.getByTestId("goal-save"));

    await act(async () => {
      first.resolve(goalSetResult("First overlapping Goal", "goal-first"));
      second.resolve(
        goalSetResult("Second overlapping Goal", "goal-second", "223e4567-e89b-42d3-a456-426614174000"),
      );
      await Promise.all([first.promise, second.promise]);
    });

    await waitFor(() => {
      expect(warningGoalCounts).toHaveLength(2);
    });
    expect(warningGoalCounts).toEqual([2, 2]);
    expect(notice).toHaveBeenCalledTimes(2);
  });

  it("keeps the requested lifecycle presentation when authority clears during the request", async () => {
    const pending = deferred<ChatGoalMutationResult>();
    const api = createGoalSidebarApi({
      goal: {
        id: "goal-1",
        objective: "Keep this Goal",
        status: "paused",
        createdAt: "2026-09-12T10:00:00Z",
        updatedAt: "2026-09-12T10:00:00Z",
      },
      resumeGoal: vi.fn(async () => pending.promise),
    });
    mountExactGoal(api);

    await screen.findByRole("button", { name: "Resume" });
    await userEvent.setup().click(screen.getByRole("button", { name: "Resume" }));
    act(() => {
      api.emit?.({
        sequence: 2,
        kind: "update",
        fact: { goal: null, availability: "available" },
      });
    });

    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByText("Keep this Goal")).toBeInTheDocument();
    await act(async () => {
      pending.resolve({
        kind: "authoritative_goal",
        fact: {
          goal: {
            id: "goal-1",
            objective: "Keep this Goal",
            status: "active",
            createdAt: "2026-09-12T10:00:00Z",
            updatedAt: "2026-09-12T10:00:00Z",
          },
          availability: "available",
        },
      });
      await pending.promise;
    });
  });

  it.each(["deferred", "fast"] as const)(
    "does not submit a second Save during %s completion fade",
    async (completion) => {
      const result: ChatGoalSetResult = {
        sessionID,
        outcome: {
          kind: "mutation",
          mutation: {
            kind: "authoritative_goal",
            fact: {
              goal: {
                id: "goal-1",
                objective: "one objective",
                status: "active",
                createdAt: "2026-09-12T10:00:00Z",
                updatedAt: "2026-09-12T10:00:00Z",
              },
              availability: "available",
            },
          },
          diagnostic: null,
        },
      };
      const pending = deferred<ChatGoalSetResult>();
      const setGoal = vi.fn(async () => (completion === "fast" ? Promise.resolve(result) : pending.promise));
      const api = createGoalSidebarApi({ goal: null, setGoal });
      mountExactGoal(api);

      const user = userEvent.setup();
      await user.type(await screen.findByRole("textbox", { name: "Goal" }), "one objective");
      const save = screen.getByTestId("goal-save");
      await user.click(save);
      if (completion === "deferred") {
        await act(async () => {
          pending.resolve(result);
          await pending.promise;
        });
      }
      if (completion === "fast") {
        await screen.findByText("one objective");
      }
      fireEvent.click(save);

      expect(setGoal).toHaveBeenCalledOnce();
    },
  );
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

function goalSetResult(
  objective: string,
  id: string,
  resultSessionID: string = sessionID,
): ChatGoalSetResult {
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
  goal: ChatGoalFact["goal"];
  setGoal?: GoalSidebarApi["setGoal"];
  resumeGoal?: GoalSidebarApi["resumeGoal"];
}>): GoalSidebarApi &
  Readonly<{ emit?: (observation: Parameters<ChatGoalObservationHandler["onEvent"]>[0]) => void }> {
  let handler: ChatGoalObservationHandler | null = null;
  const api: GoalSidebarApi &
    Readonly<{
      emit?: (observation: Parameters<ChatGoalObservationHandler["onEvent"]>[0]) => void;
    }> = {
    clearGoal: vi.fn(async () => {
      throw new Error("Unexpected Goal mutation.");
    }),
    pauseGoal: vi.fn(async () => {
      throw new Error("Unexpected Goal mutation.");
    }),
    resumeGoal,
    setGoal,
    subscribeGoal: (_target, nextHandler): ApiSubscription => {
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
