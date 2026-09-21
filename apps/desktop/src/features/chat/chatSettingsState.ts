import type {
  ChatSettings,
  ChatSettingsMutation,
  ChatSettingsRead,
  ChatSettingsMutationResponse,
  ChatSettingsSessionFacts,
  ChatSettingsTarget,
  InitialChatSettings,
  NewChatSettingsCatalog,
} from "@/api";

export type ReadyNewChat = Readonly<{
  kind: "ready-new-chat";
  catalog: NewChatSettingsCatalog;
  initialSettings: InitialChatSettings;
}>;
export type ReadySession = Readonly<{
  kind: "ready-session";
  settings: ChatSettings;
  session: ChatSettingsSessionFacts;
  lastDelivered: ChatSettings;
}>;
export type SettingsState =
  | Readonly<{ kind: "loading-new-chat" | "loading-session" }>
  | Readonly<{ kind: "failed-new-chat" | "failed-session"; error: unknown }>
  | ReadyNewChat
  | ReadySession;
export type SettingsAction =
  | Readonly<{ kind: "loaded"; response: ChatSettingsRead }>
  | Readonly<{ kind: "mutated"; response: ChatSettingsMutationResponse }>
  | Readonly<{ kind: "mutation-failed" }>
  | Readonly<{ kind: "activated"; operation: ChatSettingsMutation }>;

export function loadingState(kind: ChatSettingsTarget["kind"]): SettingsState {
  return { kind: kind === "new_chat" ? "loading-new-chat" : "loading-session" };
}
export function loadedState(response: ChatSettingsRead): ReadyNewChat | ReadySession {
  return response.kind === "session"
    ? {
        kind: "ready-session",
        settings: response.settings,
        session: response.session,
        lastDelivered: response.settings,
      }
    : { kind: "ready-new-chat", catalog: response.catalog, initialSettings: response.initialSettings };
}
export function settingsReducer(state: SettingsState, action: SettingsAction): SettingsState {
  switch (action.kind) {
    case "loaded":
      return loadedState(action.response);
    case "mutated":
      return loadedState({
        kind: "session",
        settings: action.response.settings,
        session: action.response.session,
      });
    case "mutation-failed":
      return state.kind === "ready-session" ? { ...state, settings: state.lastDelivered } : state;
    case "activated": {
      if (state.kind === "ready-session")
        return { ...state, settings: activateSession(state.settings, action.operation) };
      if (state.kind !== "ready-new-chat") return state;
      const initialSettings = activateNewChat(state, action.operation);
      return initialSettings === state.initialSettings ? state : { ...state, initialSettings };
    }
  }
}
function activateSession(current: ChatSettings, operation: ChatSettingsMutation): ChatSettings {
  switch (operation.kind) {
    case "agent":
      return activateSessionAgent(current, operation.role);
    case "supervisor":
      return { ...current, supervisor: { ...current.supervisor, value: operation.value } };
    case "thinking":
      return current.thinking.kind === "unsupported"
        ? current
        : {
            ...current,
            thinking: { ...current.thinking, value: operation.value },
            selectedAgent: { ...current.selectedAgent, thinking: operation.value },
          };
    case "fast":
      return current.fast.kind === "unsupported"
        ? current
        : { ...current, fast: { ...current.fast, value: operation.enabled } };
    case "questions":
      return { ...current, questions: { ...current.questions, enabled: operation.enabled } };
    case "auto_compaction":
      return current.autoCompaction.policy === "optional"
        ? {
            ...current,
            autoCompaction: {
              ...current.autoCompaction,
              stored: operation.enabled,
              effective: operation.enabled,
            },
          }
        : current;
  }
}

function activateSessionAgent(current: ChatSettings, role: string): ChatSettings {
  const choice = current.agentChoices.find((candidate) => candidate.role === role);
  if (choice === undefined) throw new Error("Selected Agent is not in the Session choices.");
  if (choice.model === null || choice.thinking === null) return current;
  return {
    ...current,
    selectedAgent: { role: choice.role, model: choice.model, thinking: choice.thinking },
  };
}
function activateNewChat(state: ReadyNewChat, operation: ChatSettingsMutation): InitialChatSettings {
  const current = state.initialSettings;
  switch (operation.kind) {
    case "agent": {
      if (operation.role === current.agentRole) return current;
      const selected = state.catalog.choices.find((choice) => choice.agent.role === operation.role);
      if (selected === undefined) throw new Error("Selected Agent is not in the New Chat catalog.");
      return selected.baseline;
    }
    case "supervisor":
      return { ...current, supervisor: operation.value };
    case "thinking": {
      const value = operation.value.trim();
      return value.length === 0 ? current : { ...current, thinking: value };
    }
    case "fast":
      return { ...current, fast: operation.enabled };
    case "questions":
      return { ...current, questionsEnabled: operation.enabled };
    case "auto_compaction":
      return { ...current, autoCompactionEnabled: operation.enabled };
  }
}
