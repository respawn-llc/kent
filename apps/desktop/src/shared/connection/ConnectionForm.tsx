import { useMemo } from "react";
import { useAtomValue, useAtomSet } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  useAppServices,
  useDesktopSettings,
  useQueryAction,
  useStatusController,
  SettingsCommandError,
} from "@/app-facade";
import { Button, DisabledInteractionGuard, TextInput } from "@/ui";
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
      <DisabledInteractionGuard className="justify-self-start" disabled={action.isPending}>
        <Button type="submit" disabled={action.isPending} loading={action.isPending}>
          {t(mode === "mainStartup" ? "settings.connect" : "settings.saveConnection")}
        </Button>
      </DisabledInteractionGuard>
    </form>
  );
}
