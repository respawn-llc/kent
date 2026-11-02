import { useState } from "react";
import { QueryClientProvider, useMutation } from "@tanstack/react-query";
import { I18nextProvider, useTranslation } from "react-i18next";
import { appI18n, initializeI18n } from "@/i18n";
import { Button, ErrorState } from "@/ui";
import { errorMessage } from "@/api";
import { createAppQueryClient } from "../queryClient";
import type { SettingsStartupFailure } from "./appEnvironment";

export function SettingsRecovery({ failure }: Readonly<{ failure: SettingsStartupFailure }>) {
  const [client] = useState(createAppQueryClient);
  void initializeI18n();
  return (
    <I18nextProvider i18n={appI18n}>
      <QueryClientProvider client={client}>
        <RecoveryContent failure={failure} />
      </QueryClientProvider>
    </I18nextProvider>
  );
}

function RecoveryContent({ failure }: Readonly<{ failure: SettingsStartupFailure }>) {
  const { t } = useTranslation();
  const retry = useMutation({ mutationFn: async () => failure.nativeBridge.settings.read(), retry: false });
  const restart = useMutation({
    mutationFn: async () => failure.nativeBridge.updates.relaunch(),
    retry: false,
  });
  if (failure.recovered || retry.isSuccess) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-4">
        <p>{t("settings.restartRequired")}</p>
        {restart.error !== null && <p>{errorMessage(restart.error)}</p>}
        <Button
          loading={restart.isPending}
          disabled={restart.isPending}
          onClick={() => {
            restart.mutate();
          }}
        >
          {t("settings.restart")}
        </Button>
      </div>
    );
  }
  return (
    <ErrorState title={t("settings.readFailed")} body={errorMessage(retry.error ?? failure.error)}>
      <Button
        loading={retry.isPending}
        disabled={retry.isPending}
        onClick={() => {
          retry.mutate();
        }}
      >
        {t("app.retry")}
      </Button>
    </ErrorState>
  );
}
