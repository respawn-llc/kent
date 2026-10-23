import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import type { AppServices } from "./services";
import type { StatusController } from "./statusContextValue";
import { settingsRequireRestart, type SettingsSnapshot } from "./desktopSettings";

export function requestSettingsRestart(
  snapshot: SettingsSnapshot,
  services: AppServices,
  status: StatusController,
  t: TFunction,
): void {
  const { push, dismiss } = status;
  if (!settingsRequireRestart(snapshot)) {
    dismiss("settings-restart");
    return;
  }
  push({
    id: "settings-restart",
    tone: "info",
    title: t("settings.restartRequired"),
    actionLabel: t("settings.restart"),
    durationMs: Infinity,
    onAction: () => {
      push({ id: "settings-restart", tone: "info", title: t("settings.restarting"), durationMs: Infinity });
      void services.nativeBridge.updates.relaunch().catch((error: unknown) => {
        push({
          id: "settings-restart",
          tone: "danger",
          title: t("settings.title"),
          body: errorMessage(error),
        });
      });
    },
  });
}
