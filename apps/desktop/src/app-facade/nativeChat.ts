import type { NativeBridge, NativeDialogOpenResult } from "@app/native-bridge";
import { interpolatePath } from "@tanstack/react-router";
import { newChatRoutePath, type SessionChatTarget } from "./navigation";

export const nativeChatRoutePath = "/native-dialog/chat";

export async function openNativeNewChat(
  bridge: NativeBridge,
  projectID: string,
  title: string,
  agentRole?: string,
): Promise<NativeDialogOpenResult> {
  return bridge.dialogs.openWindow({
    ...chatWindowOptions,
    label: `new-chat-${crypto.randomUUID()}`,
    route: interpolatePath({ path: newChatRoutePath, params: { projectId: projectID } }).interpolatedPath,
    params: agentRole === undefined ? {} : { agentRole },
    title,
  });
}

const chatWindowOptions = {
  initialWidth: 960,
  initialHeight: 540,
  maximizable: true,
  resizable: true,
  chrome: "app",
} as const;

export async function openNativeChat(
  bridge: NativeBridge,
  target: SessionChatTarget,
): Promise<NativeDialogOpenResult> {
  return bridge.dialogs.openWindow({
    ...chatWindowOptions,
    label: `chat-${target.sessionID}`,
    route: nativeChatRoutePath,
    params: { projectID: target.projectID, sessionID: target.sessionID },
    title: target.sessionID,
  });
}
