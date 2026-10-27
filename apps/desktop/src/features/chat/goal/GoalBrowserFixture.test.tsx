import { describe, expect, it, vi } from "bun:test";
import { useEffect, useMemo, useState } from "react";
import userEvent from "@testing-library/user-event";
import { act, render, screen, waitFor, waitForElementToBeRemoved, within } from "@testing-library/react";
import { ChatPromptPresenceProvider } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices, startupRoutes, TestAppProviders } from "@/test-support/app-services";
import { TestSidebar } from "@/test-support/sidebar-rendering";
import { goalSource, type GoalObservationHandler } from "@/test-support/chat-runtime";

import type {
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalSetResult,
  ChatGoalSetTarget,
  ChatSessionTarget,
} from "@/api";
import { ChatOperationError, RpcError, TransportError } from "@/api";
import { Button } from "@/ui";
import { GoalAffordance } from "./GoalAffordance";
import { type GoalSidebarApi, type GoalSidebarInput } from "./GoalSidebar";
import { createGoalFixtureOwner, useGoalFixtureActions } from "./goalBindingFixtures";
import { useGoalSidebarLauncher } from "./useGoalSidebarLauncher";

const fixtureSessionID = "123e4567-e89b-42d3-a456-426614174000";
const fixtureTarget: ChatSessionTarget = {
  projectID: "project-1",
  sessionID: fixtureSessionID,
};

export const goalFixtureStates = [
  "absent",
  "active",
  "paused",
  "complete",
  "unsupported-agent",
  "workflow-session",
  "questions-off",
  "new-chat",
  "loading",
  "error-retry",
] as const;
export type GoalFixtureState = (typeof goalFixtureStates)[number];
type FixtureMutationMode = "success" | "pending" | "failure";
type FixtureSetMode = "success" | "rejection" | "diagnostic";
interface PendingMutation {
  resolve(): void;
  reject(): void;
}
interface GoalFixtureRuntime {
  api: GoalSidebarApi;
  hydrate(): void;
  pending: PendingMutation | null;
  resolvePending(): void;
  failPending(): void;
}

export type GoalBrowserPendingPrompt = Readonly<{
  kind: "question" | "approval";
  promptID: string;
}>;

export function GoalBrowserFixture({
  onPromptOpen,
}: Readonly<{
  onPromptOpen?: ((prompt: GoalBrowserPendingPrompt) => void) | undefined;
}> = {}) {
  const [state, setState] = useState<GoalFixtureState>("absent");
  const [mutationMode, setMutationMode] = useState<FixtureMutationMode>("success");
  const [setMode, setSetMode] = useState<FixtureSetMode>("success");
  const [, rerender] = useState(0);
  const runtime = useMemo(
    () =>
      createFixtureRuntime(state, mutationMode, setMode, () => {
        rerender((value) => value + 1);
      }),
    [mutationMode, rerender, setMode, state],
  );
  const owner = useMemo(
    () =>
      createGoalFixtureOwner({
        api: runtime.api,
        target: {
          kind: "new_chat",
          projectID: "project-1",
          workspaceID: "workspace-1",
          initialSettings: {
            agentRole: "default",
            supervisor: "off",
            thinking: null,
            fast: null,
            questionsEnabled: state !== "questions-off",
            autoCompactionEnabled: true,
          },
        },
      }),
    [runtime.api, state],
  );
  const actions = useGoalFixtureActions(owner, () => undefined);
  const input: GoalSidebarInput =
    state === "new-chat" || state === "questions-off"
      ? { kind: "new_chat", api: runtime.api, binding: owner.binding, setGoal: actions.setGoal }
      : { kind: "session", api: runtime.api, target: fixtureTarget };

  return (
    <GoalBrowserFixtureControls
      key={`${state}:${mutationMode}:${setMode}`}
      input={input}
      mutationMode={mutationMode}
      onMutationModeChange={setMutationMode}
      onPromptOpen={onPromptOpen}
      onSetModeChange={setSetMode}
      runtime={runtime}
      setMode={setMode}
      setState={setState}
      state={state}
    />
  );
}

function GoalBrowserFixtureControls({
  input,
  mutationMode,
  onMutationModeChange,
  onPromptOpen,
  onSetModeChange,
  runtime,
  setMode,
  setState,
  state,
}: Readonly<{
  input: GoalSidebarInput;
  mutationMode: FixtureMutationMode;
  onMutationModeChange(mode: FixtureMutationMode): void;
  onPromptOpen?: ((prompt: GoalBrowserPendingPrompt) => void) | undefined;
  onSetModeChange(mode: FixtureSetMode): void;
  runtime: GoalFixtureRuntime;
  setMode: FixtureSetMode;
  setState(state: GoalFixtureState): void;
  state: GoalFixtureState;
}>) {
  const activate = useGoalSidebarLauncher(input);
  useEffect(() => {
    activate();
  }, [activate]);

  return (
    <div
      className="grid min-h-full gap-[var(--space-3)] p-[var(--space-4)]"
      data-testid="goal-browser-fixture"
    >
      <select
        aria-label="Goal fixture state"
        onChange={(event) => {
          const nextState = goalFixtureStates.find((candidate) => candidate === event.target.value);
          if (nextState !== undefined) setState(nextState);
        }}
        value={state}
      >
        {goalFixtureStates.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
      <select
        aria-label="Goal mutation outcome"
        onChange={(event) => {
          onMutationModeChange(parseFixtureMutationMode(event.target.value));
        }}
        value={mutationMode}
      >
        <option value="success">success</option>
        <option value="pending">pending</option>
        <option value="failure">failure</option>
      </select>
      {state === "new-chat" || state === "questions-off" ? (
        <select
          aria-label="New Chat Set outcome"
          onChange={(event) => {
            onSetModeChange(parseFixtureSetMode(event.target.value));
          }}
          value={setMode}
        >
          <option value="success">success</option>
          <option value="rejection">rejection</option>
          <option value="diagnostic">diagnostic</option>
        </select>
      ) : null}
      <GoalAffordance goal={goalFixtureFact(state)} onActivate={activate} />
      {state === "loading" ? (
        <Button
          onClick={() => {
            runtime.hydrate();
          }}
        >
          Hydrate Goal
        </Button>
      ) : null}
      {state === "error-retry" ? (
        <Button
          onClick={() => {
            runtime.hydrate();
          }}
        >
          Hydrate Goal
        </Button>
      ) : null}
      {runtime.pending === null ? null : (
        <div className="flex flex-wrap gap-[var(--space-2)]">
          <Button
            onClick={() => {
              runtime.resolvePending();
            }}
          >
            Resolve pending Goal action
          </Button>
          <Button
            onClick={() => {
              runtime.failPending();
            }}
            variant="danger"
          >
            Fail pending Goal action
          </Button>
        </div>
      )}
      <Button onClick={() => onPromptOpen?.({ kind: "question", promptID: "question-1" })}>
        Open pending Question
      </Button>
      <Button onClick={() => onPromptOpen?.({ kind: "approval", promptID: "approval-1" })}>
        Open pending Approval
      </Button>
    </div>
  );
}

function createFixtureRuntime(
  state: GoalFixtureState,
  mutationMode: FixtureMutationMode,
  setMode: FixtureSetMode,
  notify: () => void,
): GoalFixtureRuntime {
  let currentFact = goalFixtureFact(state);
  let observationAttempt = 0;
  let pending: PendingMutation | null = null;
  type GoalSubscriber = GoalObservationHandler;
  const subscribers = new Map<GoalSubscriber, number>();
  const activeGoal = {
    id: "goal-fixture",
    objective: "Deterministic Goal fixture",
    status: "active",
    createdAt: "2026-09-11T10:00:00.000Z",
    updatedAt: "2026-09-11T10:00:00.000Z",
  } as const satisfies NonNullable<ChatGoalFact["goal"]>;
  const emit = () => {
    for (const [subscriber, previousSequence] of subscribers) {
      const sequence = previousSequence + 1;
      subscribers.set(subscriber, sequence);
      subscriber.onEvent({
        sequence,
        kind: previousSequence === 0 ? "hydration" : "update",
        fact: currentFact,
      });
    }
  };
  const resultFor = (
    kind: "goal" | "clear",
    status: "active" | "paused" | "complete" = "active",
  ): ChatGoalMutationResult => {
    if (kind === "clear") {
      const fact: ChatGoalFact & Readonly<{ goal: null }> = { ...currentFact, goal: null };
      currentFact = fact;
      return { kind: "authoritative_clear", fact };
    }
    const goal: NonNullable<ChatGoalFact["goal"]> = { ...(currentFact.goal ?? activeGoal), status };
    const fact: ChatGoalFact & Readonly<{ goal: NonNullable<ChatGoalFact["goal"]> }> = {
      ...currentFact,
      goal,
    };
    currentFact = fact;
    return { kind: "authoritative_goal", fact };
  };
  const mutation = async (
    kind: "goal" | "clear",
    status: "active" | "paused" | "complete" = "active",
  ): Promise<ChatGoalMutationResult> => {
    if (mutationMode === "failure") return Promise.reject(new Error("Fixture Goal mutation failed."));
    if (mutationMode === "pending") {
      return new Promise<ChatGoalMutationResult>((resolve, reject) => {
        pending = {
          resolve: () => {
            pending = null;
            const result = resultFor(kind, status);
            emit();
            notify();
            resolve(result);
          },
          reject: () => {
            pending = null;
            notify();
            reject(new Error("Fixture Goal mutation failed."));
          },
        };
        notify();
      });
    }
    return Promise.resolve(resultFor(kind, status));
  };
  const api: GoalSidebarApi = {
    clearGoal: async () => mutation("clear"),
    pauseGoal: async () => mutation("goal", "paused"),
    resumeGoal: async () => mutation("goal", "active"),
    setGoal: async (target: ChatGoalSetTarget, objective: string): Promise<ChatGoalSetResult> => {
      if ((state === "new-chat" || state === "questions-off") && setMode === "rejection") {
        return {
          sessionID: fixtureSessionID,
          outcome: {
            kind: "rejected",
            error: new ChatOperationError(
              new RpcError({
                code: "internal_failure",
                message: "Fixture Goal Set failed",
                method: "runtime.goal.set",
              }),
              { kind: "runtime_unavailable", sessionID: fixtureSessionID },
            ),
          },
        };
      }
      await mutation("goal", "active");
      const goal: NonNullable<ChatGoalFact["goal"]> = {
        ...(currentFact.goal ?? activeGoal),
        objective,
        status: "active",
      };
      const fact: ChatGoalFact & Readonly<{ goal: NonNullable<ChatGoalFact["goal"]> }> = {
        ...currentFact,
        goal,
      };
      currentFact = fact;
      emit();
      return {
        sessionID: target.kind === "new_chat" ? fixtureSessionID : target.sessionID,
        outcome: {
          kind: "mutation",
          mutation: { kind: "authoritative_goal", fact },
          diagnostic:
            (state === "new-chat" || state === "questions-off") && setMode === "diagnostic"
              ? new ChatOperationError(
                  new RpcError({
                    code: "internal_failure",
                    message: "Fixture warning",
                    method: "runtime.detach",
                  }),
                  { kind: "internal_failure", operation: "runtime.detach", cause: "fixture warning" },
                )
              : null,
        },
      };
    },
    subscribeGoal: () =>
      goalSource((handler) => {
        const close = () => subscribers.delete(handler);
        subscribers.set(handler, 0);
        observationAttempt += 1;
        if (state === "loading" || (state === "error-retry" && observationAttempt > 1)) return { close };
        queueMicrotask(() => {
          if (!subscribers.has(handler)) return;
          if (state === "error-retry") {
            subscribers.delete(handler);
            handler.onError(new TransportError("Fixture Goal observation failed."));
          } else {
            emit();
          }
        });
        return { close };
      }),
  };
  return {
    api,
    hydrate: () => {
      currentFact = currentFact.goal === null ? { ...currentFact, goal: activeGoal } : currentFact;
      emit();
    },
    get pending() {
      return pending;
    },
    resolvePending: () => {
      pending?.resolve();
    },
    failPending: () => {
      pending?.reject();
    },
  };
}

function parseFixtureMutationMode(value: string): FixtureMutationMode {
  if (value === "success" || value === "pending" || value === "failure") return value;
  throw new Error(`Unknown fixture mutation mode: ${value}`);
}

function parseFixtureSetMode(value: string): FixtureSetMode {
  if (value === "success" || value === "rejection" || value === "diagnostic") return value;
  throw new Error(`Unknown fixture Set mode: ${value}`);
}

function goalFixtureFact(state: GoalFixtureState): ChatGoalFact {
  const goal: NonNullable<ChatGoalFact["goal"]> | null =
    state === "absent" || state === "questions-off" || state === "new-chat" || state === "loading"
      ? null
      : {
          id: "goal-fixture",
          objective: "Deterministic Goal fixture",
          status: state === "paused" ? "paused" : state === "complete" ? "complete" : "active",
          createdAt: "2026-09-11T10:00:00.000Z",
          updatedAt: "2026-09-11T10:00:00.000Z",
        };
  return {
    goal,
    availability: state === "unsupported-agent" ? "agent_capability_missing" : "available",
  };
}

function renderFixture() {
  const services = createTestServices(startupRoutes);
  render(
    <TestAppProviders skipAnimations services={services}>
      <ChatPromptPresenceProvider>
        <TestSidebar>
          <GoalBrowserFixtureHarness />
        </TestSidebar>
      </ChatPromptPresenceProvider>
    </TestAppProviders>,
  );
}

function GoalBrowserFixtureHarness() {
  const [pendingPrompt, setPendingPrompt] = useState<GoalBrowserPendingPrompt | null>(null);
  return (
    <>
      <GoalBrowserFixture onPromptOpen={setPendingPrompt} />
      <div
        data-prompt-id={pendingPrompt?.promptID}
        data-prompt-kind={pendingPrompt?.kind}
        data-testid="pending-chat-prompt"
      />
    </>
  );
}

async function selectFixture(label: string, value: string) {
  await userEvent.setup().selectOptions(screen.getByRole("combobox", { name: label }), value);
}

describe("Goal browser fixture", () => {
  it("closes the Goal sidebar on repeated chip activation and opens it again", async () => {
    renderFixture();
    await screen.findByRole("textbox", { name: "Goal" });
    vi.useFakeTimers();
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
      const goalChip = () =>
        within(screen.getByTestId("goal-browser-fixture")).getByRole("button", { name: "Goal" });
      await user.click(goalChip());
      act(() => {
        vi.advanceTimersByTime(200);
      });
      expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
      vi.useRealTimers();
      await userEvent.setup().click(goalChip());
      expect(await screen.findByRole("textbox", { name: "Goal" })).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it.each([
    { state: "active", action: "pause", next: "resume" },
    { state: "paused", action: "resume", next: "pause" },
    { state: "complete", action: "reopen", next: "pause" },
  ] as const)(
    "keeps $action pending without showing an objective-save spinner",
    async ({ state, action, next }) => {
      renderFixture();
      await selectFixture("Goal fixture state", state);
      await selectFixture("Goal mutation outcome", "pending");
      await userEvent
        .setup()
        .click(await screen.findByRole("button", { name: appI18n.t(`chat.goal.${action}`) }));
      expect(screen.queryByTestId("goal-mutation-pending")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: appI18n.t(`chat.goal.${next}`) })).toBeDisabled();
      await userEvent.setup().click(screen.getByRole("button", { name: "Resolve pending Goal action" }));
      expect(await screen.findByRole("button", { name: appI18n.t(`chat.goal.${next}`) })).toBeInTheDocument();
      await waitFor(() => {
        expect(screen.queryByTestId("goal-mutation-pending")).not.toBeInTheDocument();
      });
    },
  );

  it("shows pending Clear and restores the objective and actions on failure", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "active");
    await selectFixture("Goal mutation outcome", "pending");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Clear" }));
    const pendingSpinner = screen.getByTestId("goal-mutation-pending");
    expect(pendingSpinner).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Goal" })).toHaveValue("");
    const spinnerRemoved = waitForElementToBeRemoved(pendingSpinner);
    await user.click(screen.getByRole("button", { name: "Fail pending Goal action" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Clear" })).toBeEnabled();
    await spinnerRemoved;
  });

  it("exercises hydration hold and observation retry through the destination", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "loading");
    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
    await selectFixture("Goal fixture state", "error-retry");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Try again" }));
    await userEvent.setup().click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
  });

  it("keeps New Chat rejection drafts and opaque prompt identity independent", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "new-chat");
    await selectFixture("New Chat Set outcome", "rejection");
    const user = userEvent.setup();
    const objective = "Preserve this New Chat draft";
    await user.type(await screen.findByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));
    expect(await screen.findByDisplayValue(objective)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Open pending Question" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "question");
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-id", "question-1");
    await user.click(screen.getByRole("button", { name: "Open pending Approval" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "approval");
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-id", "approval-1");
  });
});
