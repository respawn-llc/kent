import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type * as Stream from "effect/Stream";
import { z } from "zod";
import { nativeObservation, type NativeOverflowReporter } from "./observation";

export type DesktopSelfUpdate = "enabled" | "disabled";
export type DesktopSettings = Readonly<{
  version: 1;
  selfUpdate: DesktopSelfUpdate;
  serverAddress: string;
  theme: "system" | "light" | "dark";
  reducedMotion: boolean;
  transparency: boolean;
  lowPower: boolean;
  notifications: "automatic" | "native" | "inApp";
  projectOpening: "lastUsed" | "tasks" | "sessions";
}>;

export type LaunchAppearance = Readonly<{ transparency: boolean; lowPower: boolean }>;
export type SettingsSnapshot = Readonly<{
  saved: DesktopSettings;
  launch: LaunchAppearance | null;
  activeAddress: string | null;
}>;
export type SettingsChange = {
  [K in Exclude<keyof DesktopSettings, "version">]: Readonly<{ field: K; value: DesktopSettings[K] }>;
}[Exclude<keyof DesktopSettings, "version">];

export type DesktopSettingsBridge = Readonly<{
  read(): Promise<SettingsSnapshot>;
  change(change: SettingsChange): Promise<DesktopSettings>;
  changes(reportOverflow: NativeOverflowReporter): Stream.Stream<undefined, Error>;
  connect(address: string, mode: "mainStartup" | "restart"): Promise<DesktopSettings>;
}>;

const settingsFailure = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("storage"), detail: z.string() }),
  z.object({
    kind: z.literal("address"),
    detail: z.enum(["invalidUrl", "unsupportedScheme", "missingHost"]),
  }),
]);

export class SettingsCommandError extends Error {
  constructor(readonly failure: z.infer<typeof settingsFailure>) {
    super(failure.detail);
    this.name = "SettingsCommandError";
  }
}

export function parseBrowserServerAddress(address: string): string {
  let parsed: URL;
  try {
    parsed = new URL(address.trim());
  } catch {
    throw new SettingsCommandError({ kind: "address", detail: "invalidUrl" });
  }
  if (parsed.protocol !== "ws:" && parsed.protocol !== "wss:") {
    throw new SettingsCommandError({ kind: "address", detail: "unsupportedScheme" });
  }
  return parsed.toString();
}

async function settingsCommand<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  try {
    return await invoke<T>(command, args);
  } catch (error) {
    const parsed = settingsFailure.safeParse(error);
    if (parsed.success) throw new SettingsCommandError(parsed.data);
    throw error instanceof Error ? error : new Error(String(error));
  }
}

export const browserSettingsFixture: SettingsSnapshot = {
  saved: {
    version: 1,
    selfUpdate: "enabled",
    serverAddress: "ws://127.0.0.1:53082/rpc",
    theme: "system",
    reducedMotion: false,
    transparency: true,
    lowPower: false,
    notifications: "automatic",
    projectOpening: "lastUsed",
  },
  launch: { transparency: true, lowPower: false },
  activeAddress: "ws://127.0.0.1:53082/rpc",
};

export function createTauriSettings(): DesktopSettingsBridge {
  return {
    read: async () => settingsCommand<SettingsSnapshot>("read_desktop_settings"),
    change: async (change) => settingsCommand<DesktopSettings>("change_desktop_setting", { change }),
    connect: async (address, mode) =>
      settingsCommand<DesktopSettings>("connect_desktop_server", { address, mode }),
    changes: (reportOverflow) =>
      nativeObservation<undefined>(
        async (emit) =>
          listen("desktop-settings-changed", () => {
            emit(undefined);
          }),
        reportOverflow,
      ),
  };
}

// A browser development fixture, not a persistence codec or native startup policy.
export function createBrowserSettings(
  endpoint = browserSettingsFixture.saved.serverAddress,
): DesktopSettingsBridge {
  let saved = { ...browserSettingsFixture.saved, serverAddress: endpoint };
  const listeners = new Set<(value: undefined) => void>();
  let activeAddress = saved.serverAddress;
  return {
    read: async () => ({ ...browserSettingsFixture, saved, activeAddress }),
    connect: async (address, mode) => {
      saved = { ...saved, serverAddress: parseBrowserServerAddress(address) };
      if (mode === "mainStartup") activeAddress = saved.serverAddress;
      for (const listener of listeners) listener(undefined);
      return saved;
    },
    change: async (change) => {
      saved = { ...saved, [change.field]: change.value };
      for (const listener of listeners) listener(undefined);
      return saved;
    },
    changes: (reportOverflow) =>
      nativeObservation<undefined>(async (emit) => {
        listeners.add(emit);
        return () => {
          listeners.delete(emit);
        };
      }, reportOverflow),
  };
}
