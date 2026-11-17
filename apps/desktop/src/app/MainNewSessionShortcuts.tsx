import { useMatch, useRouterState } from "@tanstack/react-router";
import {
  newChatRoutePath,
  sessionChatRoutePath,
  sessionChatHistoryStateSchema,
  useAppNavigation,
} from "@/app-facade";
import { NewSessionShortcuts } from "./NewSessionShortcuts";
import type { NewSessionSource } from "./NewSessionShortcutViewModel";

export function MainNewSessionShortcuts() {
  const navigation = useAppNavigation();
  const home = useMatch({ from: "/", shouldThrow: false });
  const board = useMatch({ from: "/projects/$projectId", shouldThrow: false });
  const tasks = useMatch({ from: "/projects/$projectId/tasks", shouldThrow: false });
  const task = useMatch({ from: "/tasks/$taskId", shouldThrow: false });
  const chatSource = useChatSource();
  const projectID = board?.params.projectId ?? tasks?.params.projectId ?? home?.search.projectId;
  const source =
    chatSource ??
    (task !== undefined ? { kind: "task" as const, taskID: task.params.taskId } : projectSource(projectID));
  return <NewSessionShortcuts source={source} openNewChat={navigation.openNewChat} />;
}

function useChatSource(): NewSessionSource | null {
  const chat = useMatch({ from: sessionChatRoutePath, shouldThrow: false });
  const newChat = useMatch({ from: newChatRoutePath, shouldThrow: false });
  const history = useRouterState({ select: (state) => state.location.state });
  const bookmark = sessionChatHistoryStateSchema.parse(history).sessionChat;
  const projectID = chat?.params.projectId ?? newChat?.params.projectId;
  const sessionID =
    chat?.params.sessionId ?? (bookmark?.projectID === projectID ? bookmark?.deliveredSessionID : null);
  if (projectID === undefined) return null;
  return sessionID == null ? projectSource(projectID) : { kind: "session", projectID, sessionID };
}

function projectSource(projectID: string | undefined): NewSessionSource | null {
  return projectID === undefined ? null : { kind: "project", projectID };
}
