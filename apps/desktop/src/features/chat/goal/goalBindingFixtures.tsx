import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import type { ChatGoalSetTarget, ChatSettingsTarget } from "@/api";
import { createNewChatGoalBinding, useNewChatGoalActions, type NewChatGoalHostDelivery } from "./goalBinding";
import { GoalSidebarPage, type GoalSidebarApi } from "./GoalSidebar";
import type { SettingsState } from "../chatSettingsState";

export function createGoalFixtureOwner(
  options: Readonly<{
    api: Pick<GoalSidebarApi, "setGoal">;
    target: Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
    unsupported?: boolean;
  }>,
) {
  const { target: input } = options;
  const target = Atom.make<ChatSettingsTarget>({
    kind: "new_chat",
    projectID: input.projectID,
    workspace: { workspaceID: input.workspaceID },
  });
  const settings = Atom.make<SettingsState>({
    kind: "ready-new-chat",
    initialSettings: input.initialSettings,
    catalog: {
      choices: options.unsupported
        ? [
            {
              agent: {
                role: input.initialSettings.agentRole,
                model: "fixture",
                thinking: "none",
                tools: [],
                customCapabilities: false,
                customSystemPrompt: false,
                agentCallable: true,
              },
              baseline: input.initialSettings,
              questions: { capable: false, enabled: false, editability: { kind: "editable" } },
              thinking: { kind: "unsupported" },
              fast: { kind: "unsupported" },
              supervisor: { value: "off", baseline: "off", editability: { kind: "editable" } },
              autoCompaction: {
                policy: "optional",
                stored: true,
                effective: true,
                editability: { kind: "editable" },
              },
            },
          ]
        : [],
    },
  });
  const draft = {
    text: Atom.make(input.initialInputDraft ?? ""),
    begin: Atom.fn(() => Effect.void),
    resume: Atom.fn(() => Effect.void),
  };
  const binding = createNewChatGoalBinding({
    api: options.api,
    client: new QueryClient(),
    target,
    settings: { state: settings },
    draft,
  });
  return { target, binding };
}
export function useGoalFixtureActions(
  owner: ReturnType<typeof createGoalFixtureOwner>,
  delivered: (value: NewChatGoalHostDelivery) => void,
) {
  const select = useAtomSet(owner.target);
  const actions = useNewChatGoalActions(owner.binding);
  const state = useAtomValue(owner.binding.state);
  return {
    state,
    select,
    setGoal: async (objective: string) =>
      actions.setGoal({
        objective,
        delivered: (value) => {
          delivered(value);
          select({ kind: "session", ...value.target });
        },
      }),
  };
}
export function GoalFixturePage({
  owner,
  api,
  delivered,
}: Readonly<{
  owner: ReturnType<typeof createGoalFixtureOwner>;
  api: GoalSidebarApi;
  delivered(value: NewChatGoalHostDelivery): void;
}>) {
  const actions = useGoalFixtureActions(owner, delivered);
  return (
    <GoalSidebarPage input={{ kind: "new_chat", api, binding: owner.binding, setGoal: actions.setGoal }} />
  );
}
