import { invoke } from "@tauri-apps/api/core";
import { emitTo, listen } from "@tauri-apps/api/event";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { getCurrentWebview } from "@tauri-apps/api/webview";
import { readText, writeText } from "@tauri-apps/plugin-clipboard-manager";
import type * as Stream from "effect/Stream";
import { nativeObservation, type NativeOverflowReporter } from "./observation";

import {
  fitCurrentWindowToContent,
  openNativeDialogWindow,
  type NativeDialogContentSize,
  type NativeDialogWindowOptions,
  type NativeDialogOpenResult,
} from "./dialogs";
import { createBrowserSettings, createTauriSettings, type DesktopSettingsBridge } from "./desktopSettings";
import {
  createBrowserDirectoryBridge,
  createBrowserFileBridge,
  createTauriDirectoryBridge,
  createTauriFileBridge,
  type NativeDirectoryBridge,
  type NativeFileBridge,
} from "./fileSystem";
import {
  createBrowserCapabilities,
  createTauriCapabilities,
  normalizeNativePlatform,
  type NativeCapabilityState,
  type NativePlatform,
} from "./capabilities";
import {
  createBrowserNativeNotifications,
  createTauriNativeNotifications,
  type NativeNotificationBridge,
} from "./notifications";
import { createBrowserWindowFocusControls, createTauriWindowFocusControls } from "./windowFocus";
import { createBrowserUpdates, createTauriUpdates, type NativeUpdateBridge } from "./updates";
export { nativeObservation, type NativeOverflowReporter } from "./observation";

export type { NativeDialogContentSize, NativeDialogWindowOptions, NativeDialogOpenResult } from "./dialogs";
export type {
  NativeDirectoryPickerOptions,
  NativeDirectorySelection,
  NativeFilePickerOptions,
  NativeFileSelection,
  NativeFileTarget,
} from "./fileSystem";
export type {
  DesktopSelfUpdate,
  DesktopSettings,
  SettingsSnapshot,
  SettingsChange,
  LaunchAppearance,
} from "./desktopSettings";
export { browserSettingsFixture, SettingsCommandError, parseBrowserServerAddress } from "./desktopSettings";
export {
  createBrowserCapabilities,
  createTauriCapabilities,
  normalizeNativePlatform,
  type NativeCapabilityState,
  type NativePlatform,
} from "./capabilities";
export { NativeNotificationIDMapper, type NativeNotificationBackendID } from "./notificationIds";
export {
  createUnavailableNativeNotifications,
  createBrowserNativeNotifications,
  createTauriNativeNotifications,
  validateNativeNotification,
  type NativeNotification,
  type NativeNotificationActivation,
  type NativeNotificationApprovalFocus,
  type NativeNotificationPermission,
  type NativeNotificationQuestionFocus,
  type NativeNotificationTarget,
  type NativeNotificationTaskDetailTarget,
  type NativeNotificationBridge,
  type TauriNativeNotificationOptions,
} from "./notifications";
export {
  createBrowserWindowFocusControls,
  createTauriWindowFocusControls,
  type NativeWindowFocusControls,
} from "./windowFocus";
export {
  createBrowserUpdates,
  createTauriUpdates,
  type NativeUpdateAvailability,
  type NativeUpdateBridge,
  type NativeUpdateDownloadProgress,
} from "./updates";

export type NativeBridge = Readonly<{
  capabilities: NativeCapabilityState;
  clipboard: Readonly<{
    paste: (() => Promise<string | null>) | null;
    writeText(value: string): Promise<void>;
    readText(): Promise<string>;
  }>;
  directories: NativeDirectoryBridge;
  files: NativeFileBridge;
  notifications: NativeNotificationBridge;
  links: Readonly<{
    openExternal(url: string): Promise<void>;
  }>;
  logging: Readonly<{
    append(entry: NativeLogEntry): Promise<void>;
  }>;
  updates: NativeUpdateBridge;
  settings: DesktopSettingsBridge;
  app: Readonly<{
    resolvePlatform(): Promise<NativePlatform>;
    resolveContext(): Promise<NativeContext>;
    isMainWindow(): boolean;
    reloadHome(address: string): void;
  }>;
  window: Readonly<{
    setTitle(title: string): Promise<void>;
    startDragging(): Promise<void>;
    closeCurrent(): Promise<void>;
    isFocused(): Promise<boolean>;
    focusMain(): Promise<void>;
    requestMainNavigation(destination: NativeMainNavigation): Promise<void>;
    mainNavigationRequests(
      reportOverflow: NativeOverflowReporter,
    ): Stream.Stream<NativeMainNavigation, Error>;
    focusChanges(reportOverflow: NativeOverflowReporter): Stream.Stream<boolean, Error>;
    fileDrops(reportOverflow: NativeOverflowReporter): Stream.Stream<readonly string[], Error>;
    fitCurrentToContent(size: NativeDialogContentSize): Promise<void>;
    setCurrentGlassTint(tint: NativeWindowGlassTint | null): Promise<void>;
  }>;
  dialogs: Readonly<{
    openWindow(options: NativeDialogWindowOptions): Promise<NativeDialogOpenResult>;
  }>;
  projectCreation: Readonly<{
    openWindow(draft: NativeProjectCreationDraft): Promise<void>;
    notifyCreated(binding: NativeProjectBinding): Promise<void>;
    onCreated(handler: (binding: NativeProjectBinding) => void): Promise<() => void>;
  }>;
  projectDeletion: Readonly<{
    notifyDeleted(event: NativeProjectDeleted): Promise<void>;
    deleted(reportOverflow: NativeOverflowReporter): Stream.Stream<NativeProjectDeleted, Error>;
  }>;
}>;

export type NativeWindowGlassTint = Readonly<{
  red: number;
  green: number;
  blue: number;
  alpha: number;
}>;

export type NativeMainNavigation =
  | Readonly<{ kind: "sessionChat"; projectID: string; sessionID: string }>
  | Readonly<{ kind: "taskDetail"; taskID: string }>;

const mainNavigationEvent = "app://main-navigation";

const nativeWindowGlassTintChannels = ["red", "green", "blue", "alpha"] as const;

export type NativeLogEntry = Readonly<{
  level: "debug" | "info" | "warn" | "error";
  message: string;
  context: Readonly<Record<string, string>>;
  occurredAt: string;
}>;

export type NativeContext = Readonly<{
  serverEndpoint: string;
  platform: NativePlatform;
  homePath: string;
}>;

export type NativeProjectCreationDraft = Readonly<{
  name: string;
  key: string;
  workspaceRoot: string;
}>;

export type NativeProjectBinding = Readonly<{
  projectID: string;
}>;

export type NativeProjectDeleted = Readonly<{
  projectID: string;
}>;

export const nativeDialogWindowHorizontalInsetPx = 16;
const projectDeletedEvent = "app://project-deleted";

declare global {
  interface Window {
    __TAURI_INTERNALS__?: unknown;
  }
}

export type BrowserNativeBridgeOptions = Readonly<{
  platform?: NativePlatform | undefined;
  serverEndpoint?: string | undefined;
}>;

export function createBrowserNativeBridge(options: BrowserNativeBridgeOptions = {}): NativeBridge {
  const capabilities = createBrowserCapabilities(options.platform ?? "browser");
  const projectDeletionHandlers = new Set<(event: NativeProjectDeleted) => void>();
  const browserWindowFocus = createBrowserWindowFocusControls();
  return {
    capabilities,
    clipboard: {
      paste: null,
      async writeText(): Promise<void> {
        throw new Error("Native clipboard is unavailable in this shell.");
      },
      async readText(): Promise<string> {
        throw new Error("Native clipboard is unavailable in this shell.");
      },
    },
    directories: createBrowserDirectoryBridge(),
    files: createBrowserFileBridge(),
    notifications: createBrowserNativeNotifications(),
    links: {
      async openExternal(url: string): Promise<void> {
        window.open(validateExternalUrl(url), "_blank", "noopener,noreferrer");
      },
    },
    logging: {
      async append(): Promise<void> {
        return Promise.resolve();
      },
    },
    updates: createBrowserUpdates(),
    settings: createBrowserSettings(options.serverEndpoint),
    app: {
      isMainWindow: () => !window.location.pathname.startsWith("/native-dialog/"),
      reloadHome: (address) => {
        const url = new URL("/", window.location.href);
        url.searchParams.set("appRpcEndpoint", address);
        window.location.assign(url);
      },
      async resolvePlatform(): Promise<NativePlatform> {
        return capabilities.platform;
      },
      async resolveContext(): Promise<NativeContext> {
        return {
          serverEndpoint: "ws://127.0.0.1:53082/rpc",
          platform: capabilities.platform,
          homePath: "",
        };
      },
    },
    window: {
      async setTitle(title): Promise<void> {
        document.title = title;
      },
      async startDragging(): Promise<void> {
        return Promise.resolve();
      },
      async closeCurrent(): Promise<void> {
        return Promise.resolve();
      },
      isFocused: browserWindowFocus.isFocused,
      focusMain: browserWindowFocus.focusMain,
      async requestMainNavigation(): Promise<void> {
        throw new Error("Native navigation is unavailable in this shell.");
      },
      mainNavigationRequests: (reportOverflow) =>
        nativeObservation(async () => () => undefined, reportOverflow),
      focusChanges: browserWindowFocus.focusChanges,
      fileDrops: (reportOverflow) => nativeObservation(async () => () => undefined, reportOverflow),
      async fitCurrentToContent(): Promise<void> {
        return Promise.resolve();
      },
      async setCurrentGlassTint(): Promise<void> {
        return Promise.resolve();
      },
    },
    dialogs: {
      async openWindow(): Promise<NativeDialogOpenResult> {
        throw new Error("Native dialog windows are unavailable in this shell.");
      },
    },
    projectCreation: {
      async openWindow(): Promise<void> {
        throw new Error("Native project creation window is unavailable in this shell.");
      },
      async notifyCreated(): Promise<void> {
        return Promise.resolve();
      },
      async onCreated(): Promise<() => void> {
        return () => undefined;
      },
    },
    projectDeletion: {
      async notifyDeleted(event: NativeProjectDeleted): Promise<void> {
        await Promise.all(
          Array.from(projectDeletionHandlers, async (handler) => {
            handler(event);
          }),
        );
      },
      deleted: (reportOverflow) =>
        nativeObservation(async (handler) => {
          projectDeletionHandlers.add(handler);
          return () => {
            projectDeletionHandlers.delete(handler);
          };
        }, reportOverflow),
    },
  };
}

export function createTauriNativeBridge(platform: NativePlatform = "unknown"): NativeBridge {
  const capabilities = createTauriCapabilities(platform);
  const tauriWindowFocus = createTauriWindowFocusControls();
  return {
    capabilities,
    clipboard: {
      paste: async () => invoke<string | null>("paste_clipboard"),
      async writeText(value: string): Promise<void> {
        await writeText(value);
      },
      async readText(): Promise<string> {
        return readText();
      },
    },
    directories: createTauriDirectoryBridge(),
    files: createTauriFileBridge(),
    notifications: createTauriNativeNotifications({ platform }),
    links: {
      async openExternal(url: string): Promise<void> {
        await invoke("open_external_url", { url: validateExternalUrl(url) });
      },
    },
    logging: {
      async append(entry: NativeLogEntry): Promise<void> {
        await invoke("append_gui_log", { entry: JSON.stringify(entry) });
      },
    },
    updates: createTauriUpdates(async () => invoke<boolean>("self_update_supported")),
    settings: createTauriSettings(),
    app: {
      isMainWindow: () => getCurrentWindow().label === "main",
      reloadHome: () => {
        window.location.assign("/");
      },
      async resolvePlatform(): Promise<NativePlatform> {
        return normalizeNativePlatform(await invoke<string>("resolve_native_platform"));
      },
      async resolveContext(): Promise<NativeContext> {
        return invoke<NativeContext>("resolve_native_context");
      },
    },
    window: {
      async setTitle(title): Promise<void> {
        await getCurrentWindow().setTitle(title);
      },
      async startDragging(): Promise<void> {
        await getCurrentWindow().startDragging();
      },
      async closeCurrent(): Promise<void> {
        await getCurrentWindow().close();
      },
      isFocused: tauriWindowFocus.isFocused,
      focusMain: tauriWindowFocus.focusMain,
      async requestMainNavigation(destination: NativeMainNavigation): Promise<void> {
        await emitTo("main", mainNavigationEvent, destination);
      },
      mainNavigationRequests: (reportOverflow) =>
        nativeObservation(
          async (handler) =>
            listen<NativeMainNavigation>(mainNavigationEvent, (event) => {
              handler(event.payload);
            }),
          reportOverflow,
        ),
      focusChanges: tauriWindowFocus.focusChanges,
      fileDrops: (reportOverflow) =>
        nativeObservation(async (handler) => {
          if (platform === "windows") return () => undefined;
          return getCurrentWebview().onDragDropEvent((event) => {
            if (event.payload.type === "drop") {
              handler(event.payload.paths);
            }
          });
        }, reportOverflow),
      async fitCurrentToContent(size: NativeDialogContentSize): Promise<void> {
        await fitCurrentWindowToContent(size);
      },
      async setCurrentGlassTint(tint: NativeWindowGlassTint | null): Promise<void> {
        validateNativeWindowGlassTint(tint);
        await invoke("set_native_window_glass_tint", {
          label: getCurrentWindow().label,
          tint,
        });
      },
    },
    dialogs: {
      async openWindow(options: NativeDialogWindowOptions): Promise<NativeDialogOpenResult> {
        return openNativeDialogWindow(options);
      },
    },
    projectCreation: {
      async openWindow(draft: NativeProjectCreationDraft): Promise<void> {
        await openNativeDialogWindow({
          initialHeight: 440,
          initialWidth: 640,
          label: `project-create-${Date.now().toString()}`,
          params: {
            key: draft.key,
            name: draft.name,
            workspaceRoot: draft.workspaceRoot,
          },
          route: "/native-dialog/project-create",
          title: "Create project",
        });
      },
      async notifyCreated(binding: NativeProjectBinding): Promise<void> {
        await emitTo("main", "app://project-created", binding);
      },
      async onCreated(handler: (binding: NativeProjectBinding) => void): Promise<() => void> {
        return listen<NativeProjectBinding>("app://project-created", (event) => {
          handler(event.payload);
        });
      },
    },
    projectDeletion: {
      async notifyDeleted(event: NativeProjectDeleted): Promise<void> {
        await emitTo("main", projectDeletedEvent, event);
      },
      deleted: (reportOverflow) =>
        nativeObservation(async (handler) => {
          return listen<NativeProjectDeleted>(projectDeletedEvent, (event) => {
            handler(event.payload);
          });
        }, reportOverflow),
    },
  };
}

export function createAutoNativeBridge(
  platform: NativePlatform = "unknown",
  browserEndpoint?: string,
): NativeBridge {
  return isTauriRuntime()
    ? createTauriNativeBridge(platform)
    : createBrowserNativeBridge({ platform: "browser", serverEndpoint: browserEndpoint });
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && window.__TAURI_INTERNALS__ !== undefined;
}

function validateExternalUrl(url: string): string {
  const parsed = new URL(url);
  if (!["http:", "https:", "mailto:"].includes(parsed.protocol)) {
    throw new Error("External link protocol is not allowed.");
  }
  return parsed.toString();
}

function validateNativeWindowGlassTint(tint: NativeWindowGlassTint | null): void {
  if (tint === null) {
    return;
  }
  for (const channel of nativeWindowGlassTintChannels) {
    const value = tint[channel];
    if (!Number.isFinite(value) || value < 0 || value > 1) {
      throw new Error(`Native glass tint ${channel} channel must be a finite number from 0 to 1.`);
    }
  }
}
