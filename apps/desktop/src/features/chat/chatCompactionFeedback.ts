import type { ChatSessionTarget } from "@/api";
import { recoverOrThrowDebugFailure, type AppServices, type ChatRuntimeHost } from "@/app-facade";
import { appI18n } from "@/i18n";
import { showStatusToast } from "@/ui";

export function chatCompactionFeedback(
  services: AppServices,
  target: ChatSessionTarget,
): Pick<ChatRuntimeHost, "onManualCompactionCompleted" | "onManualCompactionFailed"> {
  return {
    onManualCompactionCompleted: () => {
      void notifyCompletion(services, target).catch(async (error: unknown) =>
        recoverOrThrowDebugFailure({
          context: { sessionID: target.sessionID },
          error,
          logger: services.logger,
          message: "Compaction notification delivery failed.",
          recover: () => undefined,
        }),
      );
    },
    onManualCompactionFailed: (diagnostic) => {
      showStatusToast({
        id: `compaction:${target.sessionID}`,
        title: appI18n.t("chatComposer.context.failed"),
        tone: "danger",
        ...(diagnostic == null ? {} : { body: diagnostic.Detail }),
      });
    },
  };
}

async function notifyCompletion(services: AppServices, target: ChatSessionTarget) {
  const { window, notifications } = services.nativeBridge;
  if (await window.isFocused()) return;
  if ((await notifications.permissionState()) !== "granted") return;
  await notifications.notify({
    id: `compaction:${target.sessionID}`,
    title: appI18n.t("chatComposer.context.completedTitle"),
    body: appI18n.t("chatComposer.context.completedBody"),
    target: { kind: "session_prompt", projectID: target.projectID, sessionID: target.sessionID },
  });
}
