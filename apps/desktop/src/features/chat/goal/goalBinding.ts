import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import {
  ContractError,
  type ChatApi,
  type ChatGoal,
  type ChatGoalSetTarget,
  type ChatSessionTarget,
  type ChatSettingsTarget,
  type ChatGoalSetResult,
} from "@/api";
import { queryAtom } from "@/app-facade";
import type { ChatSettingsViewModel } from "../ChatSettingsViewModel";
import type { ComposerDraftViewModel } from "../ComposerDraftViewModel";

export type NewChatGoalHostDelivery = Readonly<{
  target: ChatSessionTarget;
  origin: Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
  goal: ChatGoal | null;
}>;
export type NewChatGoalBindingSnapshot =
  | Readonly<{
      kind: "unresolved";
      ready: boolean;
      pending: boolean;
      availability: "available" | "agent_capability_missing" | null;
    }>
  | Readonly<{ kind: "resolved_session"; target: ChatSessionTarget }>;
type GoalRequest = Readonly<{
  target: Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
  objective: string;
  delivered(delivery: NewChatGoalHostDelivery): void;
  completed(result: ChatGoalSetResult): void;
  rejected(error: unknown): void;
}>;
export function createNewChatGoalBinding(
  options: Readonly<{
    api: Pick<ChatApi, "setGoal">;
    client: QueryClient;
    target: Atom.Atom<ChatSettingsTarget>;
    settings: Pick<ChatSettingsViewModel, "state">;
    draft: Pick<ComposerDraftViewModel, "text" | "begin" | "resume">;
  }>,
) {
  const request = Atom.make((get) => {
    const observer = new MutationObserver(options.client, {
      retry: false,
      networkMode: "always",
      mutationFn: async (input: GoalRequest) => options.api.setGoal(input.target, input.objective),
      onSuccess: (result, input) => {
        if (result.sessionID.trim().length === 0)
          throw new ContractError("Goal Set success Session is required.");
        const goal = result.outcome.kind === "mutation" ? committedGoal(result.outcome.mutation) : null;
        input.delivered({
          target: { projectID: input.target.projectID, sessionID: result.sessionID },
          origin: input.target,
          goal,
        });
        input.completed(result);
      },
      onError: (error: Error, input) => {
        get.set(options.draft.resume, undefined);
        input.rejected(error);
      },
    });
    return { observer, read: queryAtom(observer) };
  });
  const requests = Atom.make((get) => get(get(request).read));
  const pending = Atom.make((get) => get(requests).isPending);
  const state = Atom.make((get): NewChatGoalBindingSnapshot => {
    const target = get(options.target);
    if (target.kind === "session") return { kind: "resolved_session", target };
    const settings = get(options.settings.state);
    const choice =
      settings.kind === "ready-new-chat"
        ? settings.catalog.choices.find((item) => item.agent.role === settings.initialSettings.agentRole)
        : undefined;
    return {
      kind: "unresolved",
      ready: settings.kind === "ready-new-chat",
      pending: get(pending),
      availability:
        choice === undefined ? null : choice.questions.capable ? "available" : "agent_capability_missing",
    };
  });
  const setGoal = Atom.fn<
    Readonly<{
      objective: string;
      delivered(delivery: NewChatGoalHostDelivery): void;
      completed(result: ChatGoalSetResult): void;
      rejected(error: unknown): void;
    }>
  >()(
    (input, get) =>
      Effect.gen(function* () {
        const { observer } = get(request);
        const target = get(options.target);
        const settings = get(options.settings.state);
        if (observer.getCurrentResult().isPending) {
          input.rejected(new ContractError("New Chat Goal creation is already pending."));
          return;
        }
        if (
          target.kind !== "new_chat" ||
          settings.kind !== "ready-new-chat" ||
          !("workspaceID" in target.workspace)
        ) {
          input.rejected(new ContractError("New Chat Settings are not ready."));
          return;
        }
        const captured: GoalRequest = {
          target: {
            kind: "new_chat",
            projectID: target.projectID,
            workspaceID: target.workspace.workspaceID,
            initialSettings: settings.initialSettings,
            initialInputDraft: get(options.draft.text),
          },
          objective: input.objective,
          delivered: input.delivered,
          completed: input.completed,
          rejected: input.rejected,
        };
        yield* get.setResult(options.draft.begin, undefined);
        yield* Effect.tryPromise(async () => observer.mutate(captured)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { state, requests, pending, setGoal } as const;
}
export type NewChatGoalBinding = ReturnType<typeof createNewChatGoalBinding>;
export type NewChatGoalBindingOptions = Parameters<typeof createNewChatGoalBinding>[0];
export function useNewChatGoalActions(binding: NewChatGoalBinding) {
  useAtomMount(binding.requests);
  const set = useAtomSet(binding.setGoal);
  return {
    setGoal: async (
      input: Readonly<{ objective: string; delivered(delivery: NewChatGoalHostDelivery): void }>,
    ) =>
      new Promise<ChatGoalSetResult>((resolve, reject) => {
        set({ ...input, completed: resolve, rejected: reject });
      }),
  };
}
function committedGoal(
  mutation: Extract<ChatGoalSetResult["outcome"], { kind: "mutation" }>["mutation"],
): ChatGoal {
  if (mutation.kind !== "authoritative_goal")
    throw new ContractError("New Chat Goal Set returned an illegal mutation result.");
  return mutation.fact.goal;
}
