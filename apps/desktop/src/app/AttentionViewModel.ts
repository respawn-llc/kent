import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { NativeNotificationActivation, NativeNotificationTarget } from "@app/native-bridge";
import { errorMessage, type AttentionNotification, type AttentionNotificationTarget } from "@/api";
import {
  queryKeys,
  shellObservationDiagnostics,
  type AppServices,
  type AppNavigation,
  type SidebarRootController,
  type StatusController,
} from "@/app-facade";
import { desktopChatEnabled } from "@/shared/feature-flags";
import {
  advancesAttentionNotificationRevision,
  attentionNotificationIDKey,
  attentionToastID,
  deliverPendingSurface,
  dismissSurface,
  notificationBody,
  notificationTitle,
  readCurrentPendingSurface,
  removeActiveNotification,
  setSurface,
  taskDetailInitialFocus,
  type SurfaceRecord,
} from "./attentionNotificationSurfaces";

type PresentationInputs = Readonly<{
  focused: boolean | null;
  picker: Readonly<{ projectID: string; sessionID: string }> | null;
}>;

export function createAttentionViewModel({
  services,
  client,
  t,
  status,
  roots,
  openSessionChat,
  inputs,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  t: TFunction;
  status: StatusController;
  roots: SidebarRootController;
  openSessionChat: AppNavigation["openSessionChat"];
  inputs: () => PresentationInputs;
}>) {
  return Atom.make((get) => {
    const { logger, nativeBridge: bridge } = services;
    const surfaces = Atom.make<ReadonlyMap<string, SurfaceRecord>>(new Map());
    get.mount(surfaces);
    const refresh = Atom.fn(
      () =>
        Effect.promise(async () =>
          client.invalidateQueries({ queryKey: queryKeys.allAttention, refetchType: "active" }),
        ).pipe(Effect.as(null)),
      { concurrent: true, initialValue: null },
    );
    const reportFailure = Atom.fn<Error>()(
      (error) =>
        Effect.promise(async () =>
          logger.append("warn", "Attention notification stream failed.", { error: errorMessage(error) }),
        ).pipe(Effect.as(null)),
      { concurrent: true, initialValue: null },
    );
    const remove = Atom.fn<string>()(
      (id) => removeActiveNotification(bridge.notifications, logger, id).pipe(Effect.as(null)),
      {
        concurrent: true,
        initialValue: null,
      },
    );
    const openTarget = Effect.fn("openAttentionTarget")(function* (
      target: AttentionNotificationTarget | NativeNotificationTarget,
    ) {
      if (target.kind === "session_prompt" && !desktopChatEnabled) return;
      yield* Effect.tryPromise(async () => bridge.window.focusMain()).pipe(
        Effect.catch((error) =>
          Effect.promise(async () =>
            logger.append("warn", "Focusing the main window for attention failed.", {
              error: errorMessage(error.cause),
            }),
          ),
        ),
      );
      if (target.kind === "session_prompt") {
        yield* Effect.tryPromise(async () =>
          openSessionChat({ projectID: target.projectID, sessionID: target.sessionID }),
        );
      } else {
        const handle = roots.open({
          kind: "taskDetail",
          initialFocus: taskDetailInitialFocus(target.focus),
          inboxNav: true,
          mode: "overlay",
          taskID: target.taskID,
        });
        yield* Effect.promise(async () => handle.lifecycle);
      }
    });
    const activate = Atom.fn<
      Readonly<{
        target: AttentionNotificationTarget | NativeNotificationTarget;
        notification: AttentionNotification | null;
      }>
    >()(
      ({ target, notification }) =>
        Effect.gen(function* () {
          const id = notification === null ? null : attentionNotificationIDKey(notification.id);
          if (notification !== null && id !== null) {
            const current = get.once(surfaces).get(id);
            if (current?.notification !== notification || current.state !== "toast") return null;
            setSurface(get, surfaces, id, { notification, state: "activating" });
            status.dismiss(attentionToastID(id));
          }
          const opened = yield* openTarget(target).pipe(
            Effect.as(true),
            Effect.catch((error) =>
              Effect.promise(async () =>
                logger.append("warn", "Opening attention notification target failed.", {
                  error: errorMessage(error.cause),
                  targetKind: target.kind,
                  targetID: target.kind === "session_prompt" ? target.sessionID : target.taskID,
                }),
              ).pipe(Effect.as(false)),
            ),
          );
          if (id !== null) {
            const current = get.once(surfaces).get(id);
            if (current?.state === "activating" && current.notification === notification) {
              setSurface(get, surfaces, id, {
                notification: current.notification,
                state: opened ? "dismissed" : "activation_failed",
              });
            }
          }
          return null;
        }),
      { concurrent: true, initialValue: null },
    );
    const suppress = (get: Atom.AtomContext, notification: AttentionNotification): boolean => {
      const { focused, picker } = inputs();
      if (
        notification.target.kind !== "session_prompt" ||
        focused !== true ||
        picker?.projectID !== notification.target.projectID ||
        picker.sessionID !== notification.target.sessionID
      )
        return false;
      const id = attentionNotificationIDKey(notification.id);
      if (get.once(surfaces).get(id)?.state === "toast") status.dismiss(attentionToastID(id));
      setSurface(get, surfaces, id, { notification, state: "dismissed" });
      get.set(remove, id);
      return true;
    };
    const showToast = (get: Atom.AtomContext, notification: AttentionNotification) => {
      if (suppress(get, notification)) return;
      const id = attentionNotificationIDKey(notification.id);
      setSurface(get, surfaces, id, { notification, state: "toast" });
      status.push({
        id: attentionToastID(id),
        tone: "info",
        title: notificationTitle(notification, t),
        body: notificationBody(notification, t),
        durationMs: Infinity,
        onClick: () => {
          get.set(activate, { target: notification.target, notification });
        },
        onDismiss: () => {
          const current = get.once(surfaces).get(id);
          if (current?.state === "toast" && current.notification === notification) {
            setSurface(get, surfaces, id, { notification, state: "dismissed" });
          }
        },
      });
    };
    const deliver = Atom.fn<string>()(
      (id) =>
        Effect.gen(function* () {
          for (;;) {
            const pending = yield* readCurrentPendingSurface({
              focused: () => inputs().focused,
              get,
              id,
              logger,
              surfaced: surfaces,
              windowControls: bridge.window,
            });
            if (pending === null) return null;
            const outcome = yield* deliverPendingSurface({
              get,
              focused: pending.focused,
              hasNativeNotifications: bridge.capabilities.notifications.basic,
              logger,
              notification: pending.record.notification,
              notifications: bridge.notifications,
              showToast: (notification) => {
                showToast(get, notification);
              },
              surfaced: surfaces,
              t,
            });
            if (outcome.status !== "retry") return null;
          }
        }),
      { concurrent: true, initialValue: null },
    );
    const focusChanged = Atom.fn<undefined>()(
      () =>
        Effect.sync(() => {
          for (const record of get.once(surfaces).values()) suppress(get, record.notification);
          return null;
        }),
      { concurrent: true, initialValue: null },
    );
    const pending = (get: Atom.AtomContext, notification: AttentionNotification) => {
      if (notification.target.kind === "session_prompt" && !desktopChatEnabled) return;
      const id = attentionNotificationIDKey(notification.id);
      const current = get.once(surfaces).get(id);
      if (!advancesAttentionNotificationRevision(current?.notification.revision, notification.revision))
        return;
      if (current?.state === "toast" || current?.state === "activation_failed") {
        showToast(get, notification);
      } else if (current === undefined || current.state === "native") {
        setSurface(get, surfaces, id, { notification, state: "surfacing" });
        get.set(deliver, id);
      } else {
        setSurface(get, surfaces, id, { notification, state: current.state });
      }
    };
    const inbox = Atom.make(
      (observation) =>
        services.api.subscribeAttentionNotifications(shellObservationDiagnostics(logger, "attention")).pipe(
          Stream.runForEach((value) =>
            Effect.sync(() => {
              if (value.kind === "event") {
                get.set(refresh, undefined);
                if (value.event.type === "pending") pending(get, value.event.pending);
                else {
                  const id = attentionNotificationIDKey(value.event.id);
                  dismissSurface(get, surfaces, status, id);
                  get.set(remove, id);
                }
              } else if (value.kind === "error") {
                get.set(reportFailure, value.error);
                status.push({
                  id: "attention-listener-error",
                  tone: "danger",
                  title: t("app.attention.listenerFailed"),
                  body: errorMessage(value.error),
                  actionLabel: t("app.retry"),
                  onAction: () => {
                    status.dismiss("attention-listener-error");
                    observation.refreshSelf();
                  },
                });
              }
            }),
          ),
          Effect.as(null),
        ),
      { initialValue: null },
    );
    const activation = Atom.make(
      bridge.notifications.activations(shellObservationDiagnostics(logger, "notification-activation")).pipe(
        Stream.runForEach((value: NativeNotificationActivation) =>
          Effect.sync(() => {
            get.set(activate, { target: value.target, notification: null });
          }),
        ),
        Effect.catch((error) =>
          Effect.promise(async () =>
            logger.append("warn", "Listening for native attention notification activation failed.", {
              error: errorMessage(error),
            }),
          ),
        ),
        Effect.as(null),
      ),
      { initialValue: null },
    );
    const permission = Atom.make(
      Effect.gen(function* () {
        if (!bridge.capabilities.notifications.basic) return null;
        const permission = yield* Effect.tryPromise(async () => bridge.notifications.permissionState()).pipe(
          Effect.catch((error) =>
            Effect.promise(async () =>
              logger.append("warn", "Reading native notification permission failed.", {
                error: errorMessage(error.cause),
              }),
            ).pipe(Effect.as(null)),
          ),
        );
        if (permission === null) return null;
        yield* Effect.promise(async () =>
          logger.append("info", "Native notification permission state resolved.", { permission }),
        );
        if (permission === "prompt") {
          yield* Effect.tryPromise(async () => bridge.notifications.requestPermission()).pipe(
            Effect.flatMap((permission) =>
              Effect.promise(async () =>
                logger.append("info", "Native notification permission request completed.", { permission }),
              ),
            ),
            Effect.catch((error) =>
              Effect.promise(async () =>
                logger.append("warn", "Requesting native notification permission failed.", {
                  error: errorMessage(error.cause),
                }),
              ),
            ),
          );
        }
        return null;
      }),
      { initialValue: null },
    );
    return {
      surfaces: Atom.make((get) => get(surfaces)),
      inbox,
      activation,
      permission,
      activate,
      deliver,
      remove,
      focusChanged,
      refresh,
      reportFailure,
    } as const;
  });
}
