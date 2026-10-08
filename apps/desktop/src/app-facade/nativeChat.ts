import type { NativeBridge, NativeDialogOpenResult } from "@app/native-bridge";
import type { SessionChatTarget } from "./navigation";

export const nativeChatRoutePath = "/native-dialog/chat";

export async function openNativeChat(
  bridge: NativeBridge,
  target: SessionChatTarget,
): Promise<NativeDialogOpenResult> {
  return bridge.dialogs.openWindow({
    label: `chat-${target.sessionID}`,
    route: nativeChatRoutePath,
    params: { projectID: target.projectID, sessionID: target.sessionID },
    title: "",
    initialWidth: 960,
    initialHeight: 540,
    maximizable: true,
    resizable: true,
    presentation: "content",
    titleBar: bridge.capabilities.platform === "macos" ? "integrated" : "native",
  });
}
