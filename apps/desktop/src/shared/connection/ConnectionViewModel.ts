import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import type { TFunction } from "i18next";
import {
  acceptSavedDesktopSettings,
  queryAction,
  requestSettingsRestart,
  SettingsCommandError,
  type AppServices,
  type StatusController,
} from "@/app-facade";
import { errorMessage } from "@/api";

export function createConnectionViewModel({
  services,
  client,
  mode,
  status,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  mode: "mainStartup" | "restart";
  status: StatusController;
  t: TFunction;
}>) {
  const draft = Atom.make<string | null>(null);
  const action = queryAction(
    new MutationObserver(client, {
      mutationFn: async (address: string) => services.nativeBridge.settings.connect(address, mode),
      retry: false,
      networkMode: "always",
      onSuccess: (saved) => {
        const snapshot = acceptSavedDesktopSettings(client, saved);
        if (mode === "mainStartup") services.nativeBridge.app.reloadHome(saved.serverAddress);
        else if (snapshot !== undefined) requestSettingsRestart(snapshot, services, status, t);
      },
      onError: (error) => {
        if (error instanceof SettingsCommandError && error.failure.kind === "address") return;
        status.push({
          id: "connection-failed",
          tone: "danger",
          title: t("settings.connection"),
          body: errorMessage(error),
        });
      },
    }),
  );
  return { draft, action };
}
