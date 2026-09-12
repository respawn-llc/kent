import { useEffect, useMemo, useState } from "react";

import type {
  ApiSubscription,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalSetResult,
  ChatGoalSetTarget,
  ChatSessionTarget,
} from "@/api";
import { TransportError } from "@/api";
import { Button } from "@/ui";
import { GoalAffordance } from "./GoalAffordance";
import { type GoalSidebarApi, type GoalSidebarInput } from "./GoalSidebar";
import { NewChatGoalBinding } from "./goalBinding";
import { useGoalSidebarLauncher } from "./useGoalSidebarLauncher";
import {
  goalFixtureConfigs,
  goalFixtureFact,
  goalFixtureStates,
  type GoalFixtureState,
} from "./goalFixtureState";

const fixtureSessionID = "123e4567-e89b-42d3-a456-426614174000";
const fixtureTarget: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID: fixtureSessionID,
};

type PendingMutation = Readonly<{
  resolve(): void;
  reject(error: Error): void;
}>;

type GoalFixtureRuntime = Readonly<{
  api: GoalSidebarApi;
  broadcast(): void;
  hydrate(): void;
  pending: PendingMutation | null;
  resolvePending(): void;
  failPending(): void;
}>;

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
  const [, rerender] = useState(0);
  const config = goalFixtureConfigs[state];
  const runtime = useMemo(
    () =>
      createFixtureRuntime(state, () => {
        rerender((value) => value + 1);
      }),
    [rerender, state],
  );
  const input = useMemo<GoalSidebarInput>(() => {
    if (config.mode === "new_chat") {
      const binding = new NewChatGoalBinding({
        api: { setGoal: runtime.api.setGoal },
        captureTarget: () => ({
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
        }),
      });
      return { kind: "new_chat", api: runtime.api, binding };
    }
    return { kind: "session", api: runtime.api, target: fixtureTarget };
  }, [config.mode, runtime.api, state]);
  return (
    <GoalBrowserFixtureControls
      key={state}
      config={config}
      input={input}
      runtime={runtime}
      setState={setState}
      state={state}
      onPromptOpen={onPromptOpen}
    />
  );
}

function GoalBrowserFixtureControls({
  config,
  input,
  runtime,
  setState,
  state,
  onPromptOpen,
}: Readonly<{
  config: (typeof goalFixtureConfigs)[GoalFixtureState];
  input: GoalSidebarInput;
  onPromptOpen?: ((prompt: GoalBrowserPendingPrompt) => void) | undefined;
  runtime: GoalFixtureRuntime;
  setState: (state: GoalFixtureState) => void;
  state: GoalFixtureState;
}>) {
  const activate = useGoalSidebarLauncher(input);
  useEffect(() => {
    activate();
  }, [activate]);

  const openPicker = (prompt: GoalBrowserPendingPrompt) => {
    onPromptOpen?.(prompt);
  };

  return (
    <div
      className="grid min-h-full gap-[var(--space-3)] p-[var(--space-4)]"
      data-testid="goal-browser-fixture"
    >
      <label className="grid gap-[var(--space-1)] text-sm">
        Goal fixture state
        <select
          aria-label="Goal fixture state"
          onChange={(event) => {
            const nextState = goalFixtureStates.find((candidate) => candidate === event.target.value);
            if (nextState !== undefined) {
              setState(nextState);
            }
          }}
          value={state}
        >
          {goalFixtureStates.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </label>
      <GoalAffordance goal={goalFixtureFact(state)} onActivate={activate} />
      <p className="m-0 text-sm text-[var(--color-muted)]" data-testid="goal-browser-fixture-description">
        {config.description}
      </p>
      {config.picker === "question" ? (
        <Button
          onClick={() => {
            openPicker({ kind: "question", promptID: "question-1" });
          }}
        >
          Open pending Question
        </Button>
      ) : config.picker === "approval" ? (
        <Button
          onClick={() => {
            openPicker({ kind: "approval", promptID: "approval-1" });
          }}
        >
          Open pending Approval
        </Button>
      ) : null}
      {config.observation === "loading" ? <Button onClick={runtime.hydrate}>Hydrate Goal</Button> : null}
      {runtime.pending === null ? null : (
        <div className="flex flex-wrap gap-[var(--space-2)]">
          <Button onClick={runtime.resolvePending}>Resolve pending Goal action</Button>
          <Button onClick={runtime.failPending} variant="danger">
            Fail pending Goal action
          </Button>
        </div>
      )}
      {state === "dirty-broadcast" || state === "overlapping-read" ? (
        <Button onClick={runtime.broadcast}>Broadcast authoritative Goal update</Button>
      ) : null}
    </div>
  );
}

function createFixtureRuntime(state: GoalFixtureState, notify: () => void): GoalFixtureRuntime {
  const config = goalFixtureConfigs[state];
  let currentFact = goalFixtureFact(state);
  let nextSequence = 1;
  let observationAttempt = 0;
  let pending: PendingMutation | null = null;
  const subscribers = new Set<Parameters<GoalSidebarApi["subscribeGoal"]>[1]>();
  const activeGoal = (): NonNullable<ChatGoalFact["goal"]> => ({
    id: "goal-fixture",
    objective: "Deterministic Goal fixture",
    status: "active",
    createdAt: "2026-09-11T10:00:00.000Z",
    updatedAt: "2026-09-11T10:00:00.000Z",
  });
  const emit = () => {
    for (const subscriber of subscribers) {
      subscriber.onEvent({
        sequence: nextSequence,
        kind: nextSequence === 1 ? "hydration" : "update",
        fact: currentFact,
      });
    }
  };
  const resultFor = (
    kind: "goal" | "clear",
    status: "active" | "paused" | "complete" = "active",
  ): ChatGoalMutationResult => {
    if (kind === "clear") {
      currentFact = { ...currentFact, goal: null };
      return {
        kind: "authoritative_clear" as const,
        fact: { ...currentFact, goal: null },
      };
    }
    const goal = { ...(currentFact.goal ?? activeGoal()), status };
    currentFact = { ...currentFact, goal };
    return { kind: "authoritative_goal", fact: { ...currentFact, goal } };
  };
  const mutation = async (
    kind: "goal" | "clear",
    status: "active" | "paused" | "complete" = "active",
  ): Promise<ChatGoalMutationResult> => {
    if (config.mutation === "error") {
      return Promise.reject(new Error("Fixture Goal mutation failed."));
    }
    if (config.mutation === "pending") {
      return new Promise<ChatGoalMutationResult>((resolve, reject) => {
        pending = {
          resolve: () => {
            const result = resultFor(kind, status);
            pending = null;
            notify();
            resolve(result);
          },
          reject: (error) => {
            pending = null;
            notify();
            reject(error);
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
      if (state === "new-chat-loss") {
        await mutation("goal", "active");
        throw new TransportError("Fixture connection loss.");
      }
      await mutation("goal", "active");
      const goal = { ...(currentFact.goal ?? activeGoal()), objective, status: "active" as const };
      const fact: Extract<ChatGoalMutationResult, { kind: "authoritative_goal" }>["fact"] = {
        ...currentFact,
        goal,
      };
      currentFact = fact;
      return {
        sessionID: target.kind === "new_chat" ? fixtureSessionID : target.sessionID,
        outcome: {
          kind: "mutation",
          mutation: { kind: "authoritative_goal", fact },
          diagnostic: null,
        },
      };
    },
    subscribeGoal: (_target, handler): ApiSubscription => {
      subscribers.add(handler);
      observationAttempt += 1;
      if (config.observation === "ready" || (config.observation === "error" && observationAttempt > 1)) {
        queueMicrotask(() => {
          if (!subscribers.has(handler)) return;
          nextSequence = 1;
          handler.onEvent({ sequence: 1, kind: "hydration", fact: currentFact });
        });
      } else if (config.observation === "error") {
        queueMicrotask(() => {
          if (subscribers.has(handler))
            handler.onError(new TransportError("Fixture Goal observation failed."));
        });
      }
      return {
        close: () => {
          subscribers.delete(handler);
        },
      };
    },
  };
  return {
    api,
    broadcast: () => {
      nextSequence += 1;
      currentFact = {
        ...currentFact,
        goal:
          currentFact.goal === null ? null : { ...currentFact.goal, objective: "Authoritative broadcast" },
      };
      emit();
    },
    hydrate: () => {
      nextSequence = 1;
      currentFact = currentFact.goal === null ? { ...currentFact, goal: activeGoal() } : currentFact;
      emit();
    },
    get pending() {
      return pending;
    },
    resolvePending: () => {
      pending?.resolve();
    },
    failPending: () => {
      pending?.reject(new Error("Fixture Goal mutation failed."));
    },
  };
}
