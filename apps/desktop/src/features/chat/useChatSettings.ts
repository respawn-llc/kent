import { createElement, type ReactElement } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import type { ChatContext, ChatSettingsMutation, ChatSettingsMutationResponse } from "@/api";
import { ChatSettingsView, type ChatSettingsViewProps } from "./ChatSettingsView";
import type { ChatSettingsViewModel } from "./ChatSettingsViewModel";
import type { ReadyNewChat, ReadySession, SettingsState } from "./chatSettingsState";

export type ChatSettingsNavigation = Readonly<{
  openTask(taskID: string): void;
  openParentSession(previousSessionID: string): void | Promise<void>;
}>;
export type ChatSettingsOptions = ChatSettingsNavigation &
  Readonly<{
    model: ChatSettingsViewModel;
    onContextChange?(context: ChatContext): void;
  }>;
export type ReadyChatSettings =
  | (ReadyNewChat & Readonly<{ activate(operation: ChatSettingsMutation): void }>)
  | (Omit<ReadySession, "lastDelivered"> &
      Readonly<{ activate(operation: ChatSettingsMutation): Promise<ChatSettingsMutationResponse> }>);
export type ChatSettingsFeature =
  | (Exclude<SettingsState, ReadyNewChat | ReadySession> & Readonly<{ retry(): void }>)
  | (ReadyChatSettings & Readonly<{ settingsChip: ReactElement }>);

export function useChatSettings(options: ChatSettingsOptions): ChatSettingsFeature {
  const state = useAtomValue(options.model.state);
  useAtomMount(options.model.requests);
  const activate = useAtomSet(options.model.activate);
  const refresh = useAtomSet(options.model.refresh);
  if (state.kind !== "ready-new-chat" && state.kind !== "ready-session")
    return {
      ...state,
      retry: () => {
        refresh(undefined);
      },
    };
  const apply = async (operation: ChatSettingsMutation) =>
    new Promise<ChatSettingsMutationResponse | null>((resolve, reject) => {
      activate({
        operation,
        completed: resolve,
        rejected: reject,
        ...(options.onContextChange === undefined ? {} : { onContextChange: options.onContextChange }),
      });
    });
  const feature: ReadyChatSettings =
    state.kind === "ready-new-chat"
      ? {
          ...state,
          activate: (operation) => {
            void apply(operation);
          },
        }
      : {
          ...state,
          activate: async (operation) => {
            const result = await apply(operation);
            if (result === null) throw new Error("Session Settings action requires a Session.");
            return result;
          },
        };
  const props: ChatSettingsViewProps =
    feature.kind === "ready-new-chat"
      ? { feature }
      : {
          feature,
          navigation: { openTask: options.openTask, openParentSession: options.openParentSession },
        };
  return { ...feature, settingsChip: createElement(ChatSettingsView, props) };
}
