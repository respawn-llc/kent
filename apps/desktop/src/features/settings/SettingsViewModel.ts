import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
  acceptSavedDesktopSettings,
  desktopSettingsOptions,
  queryAction,
  queryAtom,
  queryReadActions,
  requestSettingsRestart,
  type AppServices,
  type SettingsChange,
  type StatusController,
} from "@/app-facade";
import { errorMessage } from "@/api";

export function createSettingsViewModel({
  services,
  client,
  configPath,
  status,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  configPath: string | null;
  status: StatusController;
  t: TFunction;
}>) {
  const observer = new QueryObserver(client, desktopSettingsOptions(services));
  const settings = queryAtom(observer);
  const failure = (error: unknown) => {
    status.push({
      id: "settings-failed",
      tone: "danger",
      title: t("settings.title"),
      body: errorMessage(error),
    });
  };
  const change = queryAction(
    new MutationObserver(client, {
      mutationFn: async (input: SettingsChange) => services.nativeBridge.settings.change(input),
      retry: false,
      networkMode: "always",
      onError: failure,
      onSuccess: (saved) => {
        const snapshot = acceptSavedDesktopSettings(client, saved);
        if (snapshot !== undefined) requestSettingsRestart(snapshot, services, status, t);
      },
    }),
  );
  const configObserver = new QueryObserver(client, {
    queryKey: ["settings-config", configPath],
    queryFn: async () => ({
      path: configPath,
      available:
        configPath !== null && (await services.nativeBridge.files.fileAvailable({ path: configPath })),
    }),
    retry: false,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  });
  const config = queryAtom(configObserver);
  const openConfig = queryAction(
    new MutationObserver(client, {
      mutationFn: async () => {
        if (configPath === null) throw new Error("No server config path is available.");
        await services.nativeBridge.files.openDefault({ path: configPath });
      },
      retry: false,
      networkMode: "always",
      onError: failure,
    }),
  );
  return {
    settings,
    change,
    config,
    openConfig,
    retry: queryReadActions(observer).retry,
    retryConfig: queryReadActions(configObserver).retry,
  } as const;
}

export type SettingsViewModel = ReturnType<typeof createSettingsViewModel>;
