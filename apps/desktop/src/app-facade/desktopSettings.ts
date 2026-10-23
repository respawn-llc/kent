import type { DesktopSettings, SettingsChange, SettingsSnapshot } from "@app/native-bridge";
import { queryOptions, useQuery, type QueryClient } from "@tanstack/react-query";
import type { AppServices } from "./services";
import { useAppServices } from "./useAppServices";

export type { DesktopSettings, SettingsChange, SettingsSnapshot };
export { SettingsCommandError } from "@app/native-bridge";

export const desktopSettingsKey = ["desktop-settings"] as const;

export function acceptSavedDesktopSettings(
  client: QueryClient,
  saved: DesktopSettings,
): SettingsSnapshot | undefined {
  return client.setQueryData<SettingsSnapshot>(desktopSettingsKey, (current) =>
    current === undefined ? undefined : { ...current, saved },
  );
}

export function effectiveAppearance(snapshot: SettingsSnapshot, systemReducedMotion: boolean) {
  const lowPower = snapshot.launch?.lowPower ?? false;
  return {
    reducedMotion: systemReducedMotion || snapshot.saved.reducedMotion || lowPower,
    transparent: snapshot.launch !== null && snapshot.launch.transparency && !lowPower,
    lowPower,
  };
}

export function settingsRequireRestart(snapshot: SettingsSnapshot): boolean {
  return (
    snapshot.saved.transparency !== snapshot.launch?.transparency ||
    snapshot.saved.lowPower !== snapshot.launch.lowPower ||
    snapshot.saved.serverAddress !== snapshot.activeAddress
  );
}

export function desktopSettingsOptions(services: AppServices) {
  return queryOptions({
    queryKey: desktopSettingsKey,
    queryFn: async () => services.nativeBridge.settings.read(),
    initialData: services.initialSettings,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    staleTime: Infinity,
  });
}

export function useDesktopSettings(): SettingsSnapshot {
  return useQuery(desktopSettingsOptions(useAppServices())).data;
}
