import type { useStatusController } from "@/app-facade";
import type { ChatError, ChatOperationError } from "@/api";
import type { useTranslation } from "react-i18next";

type StatusPush = ReturnType<typeof useStatusController>["push"];
type Translator = ReturnType<typeof useTranslation>["t"];

export function goalErrorMessage(error: ChatError, t: Translator): string {
  switch (error.kind) {
    case "runtime_unavailable":
      return t("chatSettings.errors.runtimeUnavailable");
    case "internal_failure":
      return error.cause ?? t("chatSettings.errors.internalFailure");
    case "unknown":
      return t("chatSettings.errors.unknown", { code: error.code });
    case "session_not_found":
    case "workspace_not_registered":
    case "agent_preparation":
    case "auth_required":
    case "server_not_ready":
      return t("chat.goal.mutationFailed");
  }
}

export function goalSetDiagnosticNotificationID(): string {
  return `goal-set-diagnostic:${crypto.randomUUID()}`;
}

export function reportGoalSetDiagnostic(
  push: StatusPush,
  t: Translator,
  diagnostic: ChatOperationError,
  id: string,
): void {
  push({
    id,
    title: t("chat.goal.savedWithWarning"),
    body: goalErrorMessage(diagnostic.detail, t),
    tone: "warning",
  });
}
