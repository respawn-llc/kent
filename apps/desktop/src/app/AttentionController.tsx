import { useCallback, useEffect, useEffectEvent, useReducer, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useLocation, useMatch } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type { NativeNotificationActivation, NativeNotificationTarget } from "@app/native-bridge";

import type {
  ApiSubscription,
  AttentionNotification,
  AttentionNotificationID,
  AttentionNotificationTarget,
  AttentionNotificationEventHandler,
} from "@/api";
import { errorMessage } from "@/api";
import {
  attentionToastID,
  attentionNotificationIDKey,
  advancesAttentionNotificationRevision,
  deliverPendingSurface,
  dismissSurface,
  notificationBody,
  notificationTitle,
  openNativeActivation,
  readCurrentPendingSurface,
  removeActiveNotification,
  taskDetailInitialFocus,
  type SurfaceRecord,
} from "./attentionNotificationSurfaces";
import { useAppServices } from "@/app-facade";
import { queryKeys } from "@/app-facade";
import { SidebarRootOwner, useOwnedSidebarRoots } from "@/app-facade";
import { useStatusController } from "@/app-facade";
import { useWindowFocus } from "@/app-facade";
import { useAppNavigation, useChatPromptPresence, useSidebarShell, sessionChatRoutePath } from "@/app-facade";
import { newChatRoutePath, sessionChatHistoryStateSchema } from "@/app-facade";
import { desktopChatEnabled } from "@/shared/feature-flags";

export function AttentionController() {
  return (
    <SidebarRootOwner>
      <OwnedAttentionController />
    </SidebarRootOwner>
  );
}

function OwnedAttentionController() {
  const { api, logger } = useAppServices();
  const queryClient = useQueryClient();
  const { handlePending, handleResolved } = useAttentionSurfacePresenter();
  const { t } = useTranslation();
  const { push, dismiss } = useStatusController();
  const [attempt, retry] = useReducer((value: number) => value + 1, 0);

  const refreshAttentionProjection = useCallback((): void => {
    void queryClient.invalidateQueries({
      queryKey: queryKeys.allAttention,
      refetchType: "active",
    });
  }, [queryClient]);

  const handlers = useEffectEvent(() => ({
    onEvent(event: Parameters<AttentionNotificationEventHandler["onEvent"]>[0]) {
      refreshAttentionProjection();
      if (event.type === "pending") {
        void handlePending(event.pending);
        return;
      }
      handleResolved(event.id);
    },
    onError(error: Error) {
      void logger.append("warn", "Attention notification stream failed.", {
        error: errorMessage(error),
      });
      push({
        id: "attention-listener-error",
        tone: "danger",
        title: t("app.attention.listenerFailed"),
        body: errorMessage(error),
        actionLabel: t("app.retry"),
        onAction: retry,
      });
    },
  }));
  useEffect(() => {
    dismiss("attention-listener-error");
    let subscription: ApiSubscription | null = api.subscribeAttentionNotifications({
      onEvent: (event) => {
        handlers().onEvent(event);
      },
      onComplete: (code) => {
        if (code === 0) subscription = null;
      },
      onError: (error) => {
        handlers().onError(error);
      },
    });
    return () => {
      subscription?.close();
    };
  }, [api, attempt, dismiss]);

  return null;
}

function useFocusedChatPicker() {
  const presence = useChatPromptPresence();
  const { activeDestination } = useSidebarShell();
  const chatMatch = useMatch({ from: sessionChatRoutePath, shouldThrow: false });
  const newChatMatch = useMatch({ from: newChatRoutePath, shouldThrow: false });
  const historyState = useLocation({ select: (location) => location.state });
  const bookmark = sessionChatHistoryStateSchema.parse(historyState).sessionChat;
  const projectID = (chatMatch ?? newChatMatch)?.params.projectId;
  const routeSessionID = chatMatch?.params.sessionId;
  const sessionID =
    bookmark?.projectID === projectID ? (bookmark?.deliveredSessionID ?? routeSessionID) : routeSessionID;
  return activeDestination === null &&
    presence.target !== null &&
    projectID === presence.target.projectID &&
    sessionID === presence.target.sessionID
    ? presence.target
    : null;
}

function useAttentionSurfacePresenter() {
  const { t } = useTranslation();
  const { logger, nativeBridge: bridge } = useAppServices();
  const { open } = useOwnedSidebarRoots();
  const status = useStatusController();
  const windowFocused = useWindowFocus();
  const { openSessionChat } = useAppNavigation();
  const pickerTarget = useFocusedChatPicker();
  const pickerRef = useRef(pickerTarget);
  const focusedRef = useRef<boolean | null>(windowFocused);
  const surfacedRef = useRef(new Map<string, SurfaceRecord>());

  const suppressFocusedPicker = useCallback(
    (notification: AttentionNotification): boolean => {
      if (
        notification.target.kind !== "session_prompt" ||
        focusedRef.current !== true ||
        pickerRef.current?.projectID !== notification.target.projectID ||
        pickerRef.current.sessionID !== notification.target.sessionID
      )
        return false;
      const id = attentionNotificationIDKey(notification.id);
      if (surfacedRef.current.get(id)?.state === "toast") status.dismiss(attentionToastID(id));
      removeActiveNotification(bridge.notifications, logger, id);
      surfacedRef.current.set(id, { notification, state: "dismissed" });
      return true;
    },
    [status, bridge.notifications, logger],
  );

  useEffect(() => {
    focusedRef.current = windowFocused;
    pickerRef.current = pickerTarget;
    if (windowFocused !== true || pickerTarget === null) return;
    for (const record of surfacedRef.current.values()) suppressFocusedPicker(record.notification);
  }, [windowFocused, pickerTarget, suppressFocusedPicker]);

  const openTarget = useCallback(
    async (target: AttentionNotificationTarget | NativeNotificationTarget): Promise<void> => {
      if (target.kind === "session_prompt" && !desktopChatEnabled) return;
      try {
        await bridge.window.focusMain();
      } catch (error) {
        await logger.append("warn", "Focusing the main window for attention failed.", {
          error: errorMessage(error),
        });
      }
      try {
        if (target.kind === "session_prompt") {
          await openSessionChat({ projectID: target.projectID, sessionID: target.sessionID });
          return;
        }
        await open({
          kind: "taskDetail",
          initialFocus: taskDetailInitialFocus(target.focus),
          inboxNav: true,
          mode: "overlay",
          taskID: target.taskID,
        }).lifecycle;
      } catch (error) {
        await logger.append("warn", "Opening attention notification target failed.", {
          error: errorMessage(error),
          targetKind: target.kind,
          targetID: target.kind === "session_prompt" ? target.sessionID : target.taskID,
        });
        throw error;
      }
    },
    [bridge.window, logger, open, openSessionChat],
  );

  const showAttentionToast = useCallback(
    (notification: AttentionNotification): void => {
      if (suppressFocusedPicker(notification)) return;
      const target = notification.target;
      const notificationKey = attentionNotificationIDKey(notification.id);
      const toastID = attentionToastID(notificationKey);
      const markDismissed = () => {
        const existing = surfacedRef.current.get(notificationKey);
        if (existing?.state === "toast") {
          surfacedRef.current.set(notificationKey, {
            notification: existing.notification,
            state: "dismissed",
          });
        }
      };
      const activate = () => {
        surfacedRef.current.set(notificationKey, { notification, state: "activating" });
        status.dismiss(toastID);
        void openTarget(target)
          .then(() => {
            const current = surfacedRef.current.get(notificationKey);
            if (current?.state === "activating") {
              surfacedRef.current.set(notificationKey, {
                notification: current.notification,
                state: "dismissed",
              });
            }
          })
          .catch(() => {
            const current = surfacedRef.current.get(notificationKey);
            if (current?.state === "activating") {
              surfacedRef.current.set(notificationKey, {
                notification: current.notification,
                state: "activation_failed",
              });
            }
          });
      };
      status.push({
        id: toastID,
        tone: "info",
        title: notificationTitle(notification, t),
        body: notificationBody(notification, t),
        onClick: activate,
        onDismiss: markDismissed,
        durationMs: Infinity,
      });
      surfacedRef.current.set(notificationKey, { notification, state: "toast" });
    },
    [openTarget, status, t, suppressFocusedPicker],
  );

  const surfaceCurrentPending = useCallback(
    async (id: string): Promise<void> => {
      for (;;) {
        const pending = await readCurrentPendingSurface({
          focusedRef,
          id,
          logger,
          surfaced: surfacedRef.current,
          windowControls: bridge.window,
        });
        if (pending === null) {
          return;
        }
        const outcome = await deliverPendingSurface({
          focused: pending.focused,
          hasNativeNotifications: bridge.capabilities.notifications.basic,
          logger,
          notification: pending.record.notification,
          notifications: bridge.notifications,
          showToast: showAttentionToast,
          surfaced: surfacedRef.current,
          t,
        });
        if (outcome.status !== "retry") {
          return;
        }
      }
    },
    [
      bridge.capabilities.notifications.basic,
      bridge.notifications,
      bridge.window,
      logger,
      showAttentionToast,
      t,
    ],
  );

  const handlePending = useCallback(
    async (notification: AttentionNotification): Promise<void> => {
      if (notification.target.kind === "session_prompt" && !desktopChatEnabled) {
        return;
      }
      const notificationKey = attentionNotificationIDKey(notification.id);
      const existing = surfacedRef.current.get(notificationKey);
      if (!advancesAttentionNotificationRevision(existing?.notification.revision, notification.revision)) {
        return;
      }
      if (existing !== undefined) {
        if (existing.state === "activation_failed" || existing.state === "toast") {
          showAttentionToast(notification);
          return;
        }
        surfacedRef.current.set(notificationKey, { notification, state: existing.state });
        if (existing.state === "native") {
          surfacedRef.current.set(notificationKey, { notification, state: "surfacing" });
          await surfaceCurrentPending(notificationKey);
        }
        return;
      }
      surfacedRef.current.set(notificationKey, { notification, state: "surfacing" });
      await surfaceCurrentPending(notificationKey);
    },
    [showAttentionToast, surfaceCurrentPending],
  );

  const handleResolved = useCallback(
    (id: AttentionNotificationID): void => {
      const notificationKey = attentionNotificationIDKey(id);
      dismissSurface(surfacedRef.current, status, notificationKey);
      removeActiveNotification(bridge.notifications, logger, notificationKey);
    },
    [bridge.notifications, logger, status],
  );

  useEffect(() => {
    if (!bridge.capabilities.notifications.basic) {
      return;
    }
    void bridge.notifications
      .permissionState()
      .then(async (permission) => {
        await logger.append("info", "Native notification permission state resolved.", {
          permission,
        });
        if (permission === "prompt") {
          try {
            const resolvedPermission = await bridge.notifications.requestPermission();
            await logger.append("info", "Native notification permission request completed.", {
              permission: resolvedPermission,
            });
          } catch (error) {
            await logger.append("warn", "Requesting native notification permission failed.", {
              error: errorMessage(error),
            });
            return;
          }
        }
      })
      .catch(async (error: unknown) => {
        await logger.append("warn", "Reading native notification permission failed.", {
          error: errorMessage(error),
        });
      });
  }, [bridge.capabilities.notifications.basic, bridge.notifications, logger]);

  useEffect(() => {
    let unlisten: (() => void) | null = null;
    let active = true;
    void bridge.notifications
      .onActivated((activation: NativeNotificationActivation) => {
        void openNativeActivation(activation, openTarget, logger);
      })
      .then((nextUnlisten) => {
        if (active) {
          unlisten = nextUnlisten;
        } else {
          nextUnlisten();
        }
      })
      .catch(async (error: unknown) => {
        await logger.append("warn", "Listening for native attention notification activation failed.", {
          error: errorMessage(error),
        });
      });
    return () => {
      active = false;
      unlisten?.();
    };
  }, [bridge.notifications, logger, openTarget]);

  return { handlePending, handleResolved };
}
