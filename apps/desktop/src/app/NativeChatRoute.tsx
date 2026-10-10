import { useMemo, type ReactNode } from "react";
import type { NativeMainNavigation } from "@app/native-bridge";
import { useStableCallback } from "@/ui";
import { ChatDestination } from "@/features/chat";
import { SidebarRootOwner, useAppServices, openNativeChat, useStatusController } from "@/app-facade";
import { errorMessage } from "@/api";
import { useTranslation } from "react-i18next";
import { SidebarProvider } from "./sidebarProvider";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { SidebarHost } from "./sidebar";
import { WindowChromeFrame } from "./WindowChromeFrame";
import { WindowChromeThemeToggle } from "./WindowChromeThemeToggle";

export function NativeChatRoute({
  projectID,
  sessionID,
}: Readonly<{ projectID: string; sessionID: string }>) {
  const { nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const reportFailure = useStableCallback((error: unknown, title: string) => {
    push({
      id: "native-chat-navigation-error",
      tone: "danger",
      title,
      body: errorMessage(error),
    });
  });
  const requestMainNavigation = useStableCallback(async (destination: NativeMainNavigation) => {
    try {
      await nativeBridge.window.requestMainNavigation(destination);
    } catch (error) {
      reportFailure(error, t("states.error"));
    }
  });
  const openChildSession = useStableCallback(async (childSessionID: string) => {
    try {
      await openNativeChat(nativeBridge, { projectID, sessionID: childSessionID });
    } catch (error) {
      reportFailure(error, t("app.popOutError"));
    }
  });
  const navigation = useMemo(
    () => ({
      openTask: async (taskID: string) => requestMainNavigation({ kind: "taskDetail", taskID }),
      openParentSession: async (previousSessionID: string) =>
        requestMainNavigation({
          kind: "sessionChat",
          projectID,
          sessionID: previousSessionID,
        }),
      openEditedSession: openChildSession,
    }),
    [projectID, requestMainNavigation, openChildSession],
  );
  return (
    <SidebarProvider policy={sidebarDestinationPolicy}>
      <NativeChatFrame>
        <SidebarRootOwner>
          <ChatDestination
            opening={{ kind: "session", projectID, sessionID }}
            navigation={navigation}
            openCreatedSession={openChildSession}
          />
        </SidebarRootOwner>
      </NativeChatFrame>
    </SidebarProvider>
  );
}

function NativeChatFrame({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <WindowChromeFrame controls={<WindowChromeThemeToggle />}>
      <div className="min-h-0 min-w-0 flex-1 overflow-visible">{children}</div>
      <SidebarHost />
    </WindowChromeFrame>
  );
}
