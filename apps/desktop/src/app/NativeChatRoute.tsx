import { useMemo, type ReactNode } from "react";
import { House } from "lucide-react";
import { useMutation } from "@tanstack/react-query";
import type { NativeMainNavigation } from "@app/native-bridge";
import { IconTooltipButton, useStableCallback } from "@/ui";
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
      projectID,
      openParentSession: async (previousSessionID: string) => {
        await requestMainNavigation({
          kind: "sessionChat",
          projectID,
          sessionID: previousSessionID,
        });
      },
      openEditedSession: openChildSession,
    }),
    [projectID, requestMainNavigation, openChildSession],
  );
  return (
    <SidebarProvider policy={sidebarDestinationPolicy}>
      <NativeChatFrame sessionID={sessionID}>
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

function NativeChatFrame({ children, sessionID }: Readonly<{ children: ReactNode; sessionID: string }>) {
  const { nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const openHome = useMutation({
    mutationFn: async () => nativeBridge.window.requestMainNavigation({ kind: "home" }),
    onError: (error) => {
      push({
        id: "native-chat-home-error",
        tone: "danger",
        title: t("states.error"),
        body: errorMessage(error),
      });
    },
  });
  return (
    <WindowChromeFrame
      controls={
        <>
          <IconTooltipButton
            label={t("app.home")}
            loading={openHome.isPending}
            onClick={() => {
              openHome.mutate();
            }}
          >
            <House size={16} />
          </IconTooltipButton>
          <WindowChromeThemeToggle />
        </>
      }
      defaultTitle={sessionID}
    >
      <div className="min-h-0 min-w-0 flex-1 overflow-visible">{children}</div>
      <SidebarHost />
    </WindowChromeFrame>
  );
}
