import { useMemo } from "react";
import { useAtomValue, useAtomSet } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Save } from "lucide-react";
import {
  useAppServices,
  useDesktopSettings,
  useQueryAction,
  useStatusController,
  SettingsCommandError,
} from "@/app-facade";
import { Button, ControlTooltip, TextInput } from "@/ui";
import { createConnectionViewModel } from "./ConnectionViewModel";

export function ConnectionForm({ startup = false }: Readonly<{ startup?: boolean }>) {
  const services = useAppServices();
  const client = useQueryClient();
  const status = useStatusController();
  const { t } = useTranslation();
  const snapshot = useDesktopSettings();
  const mode =
    startup && snapshot.initialStartup && services.nativeBridge.app.isMainWindow()
      ? "mainStartup"
      : "restart";
  const model = useMemo(
    () => createConnectionViewModel({ services, client, mode, status, t }),
    [services, client, mode, status, t],
  );
  const draft = useAtomValue(model.draft);
  const setDraft = useAtomSet(model.draft);
  const action = useQueryAction(model.action);
  return (
    <form
      className="grid min-w-0 gap-[var(--space-3)]"
      onSubmit={(event) => {
        event.preventDefault();
        action.submit(draft ?? snapshot.saved.serverAddress);
      }}
    >
      <div className="flex min-w-0 flex-wrap items-end gap-[var(--space-2)]">
        <div className="min-w-[min(100%,16rem)] flex-1">
          <TextInput
            label={t("settings.serverAddress")}
            labelHelp={t(mode === "mainStartup" ? "settings.help.connect" : "settings.help.connection")}
            className="font-mono"
            value={draft ?? snapshot.saved.serverAddress}
            error={
              action.error instanceof SettingsCommandError && action.error.failure.kind === "address"
                ? t("settings.invalidAddress")
                : undefined
            }
            onChange={(event) => {
              setDraft(event.target.value);
            }}
          />
        </div>
        {mode === "restart" && (
          <ControlTooltip label={t("settings.saveConnection")} className="shrink-0">
            <Button
              type="submit"
              size="icon"
              variant="primary"
              aria-label={t("settings.saveConnection")}
              disabled={action.isPending}
              loading={action.isPending}
            >
              <Save size={16} strokeWidth={1.75} />
            </Button>
          </ControlTooltip>
        )}
      </div>
      {mode === "mainStartup" && (
        <Button
          className="justify-self-start"
          type="submit"
          disabled={action.isPending}
          loading={action.isPending}
        >
          {t("settings.connect")}
        </Button>
      )}
    </form>
  );
}
