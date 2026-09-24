import * as Effect from "effect/Effect";
import type * as Atom from "effect/unstable/reactivity/Atom";
import type { NativeNotification, NativeNotificationTarget } from "@app/native-bridge";

import type {
  AttentionNotification,
  AttentionNotificationID,
  AttentionNotificationTaskDetailFocus,
} from "@/api";
import { errorMessage } from "@/api";
import type { AppServices } from "@/app-facade";
import type { TaskDetailInitialFocus } from "@/app-facade";
import type { StatusController } from "@/app-facade";
import { recoverOrThrowDebugFailure } from "@/app-facade";

export type SurfaceRecord = Readonly<{
  notification: AttentionNotification;
  state: "activating" | "activation_failed" | "dismissed" | "native" | "surfacing" | "toast";
}>;

export function advancesAttentionNotificationRevision(
  currentRevision: number | undefined,
  incomingRevision: number,
): boolean {
  return currentRevision === undefined || incomingRevision > currentRevision;
}

const attentionToastIDPrefix = "attention-";

type NativeNotifications = AppServices["nativeBridge"]["notifications"];
type NativeWindow = AppServices["nativeBridge"]["window"];
type Logger = AppServices["logger"];
type Translate = (key: string) => string;
type SurfaceOutcome = Readonly<{ status: "done" }> | Readonly<{ status: "retry" }>;
export type SurfaceState = Atom.Writable<ReadonlyMap<string, SurfaceRecord>>;

export function setSurface(
  get: Atom.AtomContext,
  surfaced: SurfaceState,
  id: string,
  record: SurfaceRecord | null,
): void {
  const next = new Map(get.once(surfaced));
  if (record === null) next.delete(id);
  else next.set(id, record);
  get.set(surfaced, next);
}

export const readCurrentPendingSurface = Effect.fn("readCurrentPendingSurface")(function* ({
  focused,
  get,
  id,
  logger,
  surfaced,
  windowControls,
}: Readonly<{
  focused: () => boolean | null;
  get: Atom.AtomContext;
  id: string;
  logger: Logger;
  surfaced: SurfaceState;
  windowControls: NativeWindow;
}>) {
  if (get.once(surfaced).get(id)?.state !== "surfacing") {
    return null;
  }
  const currentFocus = yield* resolveWindowFocus(windowControls, focused, logger);
  const record = get.once(surfaced).get(id);
  if (record?.state !== "surfacing") {
    return null;
  }
  return { focused: currentFocus, record };
});

export const deliverPendingSurface = Effect.fn("deliverPendingSurface")(function* ({
  get,
  focused,
  hasNativeNotifications,
  logger,
  notification,
  notifications,
  showToast,
  surfaced,
  t,
}: Readonly<{
  focused: boolean;
  get: Atom.AtomContext;
  hasNativeNotifications: boolean;
  logger: Logger;
  notification: AttentionNotification;
  notifications: NativeNotifications;
  showToast: (notification: AttentionNotification) => void;
  surfaced: SurfaceState;
  t: Translate;
}>): Effect.fn.Return<SurfaceOutcome> {
  yield* Effect.promise(async () =>
    logger.append("info", "Resolving attention notification surface.", {
      focused: String(focused),
      hasNativeNotifications: String(hasNativeNotifications),
      notificationID: attentionNotificationIDKey(notification.id),
      notificationKind: notification.kind,
      notificationRevision: String(notification.revision),
    }),
  );
  const latest = get.once(surfaced).get(attentionNotificationIDKey(notification.id));
  if (latest?.state !== "surfacing") return { status: "done" };
  if (latest.notification.revision !== notification.revision) return { status: "retry" };
  if (focused || !hasNativeNotifications) {
    yield* removeActiveNotification(notifications, logger, attentionNotificationIDKey(notification.id));
    if (get.once(surfaced).get(attentionNotificationIDKey(notification.id)) !== latest)
      return { status: "retry" };
    showToast(notification);
    return { status: "done" };
  }
  return yield* deliverNativePendingSurface({ get, logger, notification, notifications, surfaced, t });
});

export function dismissSurface(
  get: Atom.AtomContext,
  surfaced: SurfaceState,
  status: StatusController,
  id: string,
): void {
  const existing = get.once(surfaced).get(id);
  if (existing?.state === "toast") {
    status.dismiss(attentionToastID(id));
  }
  setSurface(get, surfaced, id, null);
}

export const removeActiveNotification: (
  notifications: NativeNotifications,
  logger: Logger,
  id: string,
) => Effect.Effect<void> = Effect.fn("removeActiveNotification")(function* (
  notifications: NativeNotifications,
  logger: Logger,
  id: string,
): Effect.fn.Return<void> {
  yield* Effect.tryPromise(async () => notifications.removeActive(id)).pipe(
    Effect.catch((error) =>
      Effect.promise(async () =>
        logger.append("warn", "Removing native attention notification failed.", {
          error: errorMessage(error.cause),
          notificationID: id,
        }),
      ),
    ),
  );
});

export function taskDetailInitialFocus(focus: AttentionNotificationTaskDetailFocus): TaskDetailInitialFocus {
  if (focus.kind === "question") {
    return { kind: "question", askIDs: focus.askIDs };
  }
  if (focus.kind === "approval") {
    return { kind: "approval", approvalID: focus.approvalID };
  }
  return { kind: "interrupted_current_node" };
}

export function notificationTitle(notification: AttentionNotification, t: Translate): string {
  const shortID = notification.target.kind === "workflow_task" ? (notification.target.taskShortID ?? "") : "";
  const questionCount = notification.question?.displayCount ?? 1;
  const suffix =
    notification.kind === "question"
      ? questionCount > 1
        ? `${String(questionCount)} questions`
        : t("app.attention.questionTitle")
      : notification.kind === "approval" || notification.kind === "workflow_approval"
        ? t("app.attention.approvalTitle")
        : t("app.attention.interruptedCurrentNodeTitle");
  return shortID.length > 0 ? `${shortID}: ${suffix}` : suffix;
}

export function notificationBody(notification: AttentionNotification, t: Translate): string {
  if (notification.kind === "question") {
    return nonEmpty(notification.question?.preview) ?? t("app.attention.questionFallback");
  }
  if (notification.kind === "approval") {
    return nonEmpty(notification.approval?.message) ?? t("app.attention.approvalFallback");
  }
  if (notification.kind === "workflow_approval") {
    return nonEmpty(notification.workflowApproval?.message) ?? t("app.attention.approvalFallback");
  }
  return (
    nonEmpty(notification.interruptedCurrentNode?.message) ?? interruptedCurrentNodeFallback(notification, t)
  );
}

export function attentionToastID(id: string): string {
  return `${attentionToastIDPrefix}${id}`;
}

export function attentionNotificationIDKey(id: AttentionNotificationID): string {
  return `k${String(id.kind.length)}_${id.kind}u${String(id.uuid.length)}_${id.uuid}`;
}

const resolveWindowFocus = Effect.fn("resolveWindowFocus")(function* (
  windowControls: NativeWindow,
  focused: () => boolean | null,
  logger: Logger,
) {
  const current = focused();
  if (current !== null) return current;
  const result = yield* Effect.tryPromise(async () => windowControls.isFocused()).pipe(
    Effect.catch((error) =>
      Effect.promise(async () =>
        logger.append("warn", "Reading native window focus state failed.", {
          error: errorMessage(error.cause),
        }),
      ).pipe(Effect.as(false)),
    ),
  );
  return focused() ?? result;
});

const deliverNativePendingSurface = Effect.fn("deliverNativePendingSurface")(function* ({
  get,
  logger,
  notification,
  notifications,
  surfaced,
  t,
}: Readonly<{
  logger: Logger;
  get: Atom.AtomContext;
  notification: AttentionNotification;
  notifications: NativeNotifications;
  surfaced: SurfaceState;
  t: Translate;
}>): Effect.fn.Return<SurfaceOutcome> {
  const id = attentionNotificationIDKey(notification.id);
  yield* Effect.promise(async () =>
    logger.append("info", "Sending native attention notification.", {
      notificationID: id,
      notificationKind: notification.kind,
      notificationRevision: String(notification.revision),
    }),
  );
  const delivered = yield* Effect.tryPromise(async () =>
    notifications.notify(nativeNotification(notification, t)),
  ).pipe(
    Effect.as(true),
    Effect.catch((error) =>
      handleNativeDeliveryError({ get, error: error.cause, logger, notification, surfaced }).pipe(
        Effect.as(false),
      ),
    ),
  );
  if (!delivered) return { status: "done" };
  yield* Effect.promise(async () =>
    logger.append("info", "Native attention notification accepted.", {
      notificationID: id,
      notificationKind: notification.kind,
      notificationRevision: String(notification.revision),
    }),
  );
  const latest = get.once(surfaced).get(id);
  if (latest?.state !== "surfacing") {
    yield* removeActiveNotification(notifications, logger, id);
    return { status: "done" };
  }
  if (latest.notification.revision !== notification.revision) {
    return { status: "retry" };
  }
  setSurface(get, surfaced, id, { notification, state: "native" });
  return { status: "done" };
});

const handleNativeDeliveryError = Effect.fn("handleNativeDeliveryError")(function* ({
  get,
  error,
  logger,
  notification,
  surfaced,
}: Readonly<{
  error: unknown;
  get: Atom.AtomContext;
  logger: Logger;
  notification: AttentionNotification;
  surfaced: SurfaceState;
}>) {
  const id = attentionNotificationIDKey(notification.id);
  const latest = get.once(surfaced).get(id);
  if (latest?.state !== "surfacing") {
    return;
  }
  yield* Effect.promise(async () =>
    recoverOrThrowDebugFailure({
      context: {
        notificationID: id,
      },
      error,
      logger,
      message: "Native attention notification delivery failed.",
      recover() {
        setSurface(get, surfaced, id, { notification: latest.notification, state: "dismissed" });
      },
    }),
  );
});

function nativeNotification(notification: AttentionNotification, t: Translate): NativeNotification {
  const target = nativeTarget(notification.target);
  if (target === null) {
    throw new Error("Attention notification target is not native-openable.");
  }
  return {
    id: attentionNotificationIDKey(notification.id),
    title: notificationTitle(notification, t),
    body: notificationBody(notification, t),
    target,
  };
}

function nativeTarget(target: AttentionNotification["target"]): NativeNotificationTarget | null {
  if (target.kind === "session_prompt") return target;
  const focus = taskDetailInitialFocus(target.focus);
  if (focus.kind === "question" && focus.askIDs.length === 0) {
    return null;
  }
  if (focus.kind === "dependencies") {
    return null;
  }
  return {
    kind: "task_detail",
    taskID: target.taskID,
    focus:
      focus.kind === "question"
        ? { kind: "question", askIDs: [focus.askIDs[0] ?? "", ...focus.askIDs.slice(1)] }
        : focus,
  };
}

function nonEmpty(value: string | undefined): string | undefined {
  const trimmed = value?.trim() ?? "";
  return trimmed.length > 0 ? trimmed : undefined;
}

function interruptedCurrentNodeFallback(notification: AttentionNotification, t: Translate): string {
  const fallback = t("app.attention.interruptedCurrentNodeFallback");
  const reason = nonEmpty(notification.interruptedCurrentNode?.reason);
  if (reason === undefined) {
    return fallback;
  }
  const reasonCopy: Readonly<Record<string, string>> = {
    context_selection_required: t("task.contextSelectionRequired"),
    workflow_protocol_violation_cap: t("app.attention.interruptedCurrentNodeProtocolCap"),
    workflow_runtime_start_failed: t("app.attention.interruptedCurrentNodeRuntimeFailed"),
    workflow_script_completion_failed: t("app.attention.interruptedCurrentNodeScriptFailed"),
    workflow_script_execution_failed: t("app.attention.interruptedCurrentNodeScriptFailed"),
    workflow_script_failed: t("app.attention.interruptedCurrentNodeScriptFailed"),
  };
  return reasonCopy[reason] ?? fallback;
}
