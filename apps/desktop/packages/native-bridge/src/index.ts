/* eslint-disable max-lines -- Native bridge is the package-level contract surface; Phase 8 will revisit capability cleanup. */
import { invoke } from "@tauri-apps/api/core";
import { emitTo, listen } from "@tauri-apps/api/event";
import { WebviewWindow } from "@tauri-apps/api/webviewWindow";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { readText, writeText } from "@tauri-apps/plugin-clipboard-manager";

import {
  fitCurrentWindowToContent,
  openNativeDialogWindow,
  type NativeDialogContentSize,
  type NativeDialogWindowOptions,
} from "./dialogs";

export type { NativeDialogContentSize, NativeDialogTheme, NativeDialogWindowOptions } from "./dialogs";

export type NativeCapabilityState = Readonly<{
  platform: NativePlatform;
  clipboard: Readonly<{
    writeText: boolean;
    readText: boolean;
  }>;
  directories: Readonly<{
    select: boolean;
  }>;
  notifications: Readonly<{
    basic: boolean;
  }>;
  links: Readonly<{
    openExternal: boolean;
  }>;
  logging: Readonly<{
    localFile: boolean;
  }>;
  tray: boolean;
  appMenu: boolean;
  updater: boolean;
  windowControls: boolean;
  windowDrag: boolean;
  dialogWindows: boolean;
  projectCreationWindow: boolean;
  taskDetailWindow: boolean;
  macosVibrancy: boolean;
}>;

export type NativePlatform = "browser" | "linux" | "macos" | "unknown" | "windows";

export type NativeBridge = Readonly<{
  capabilities: NativeCapabilityState;
  clipboard: Readonly<{
    writeText(value: string): Promise<void>;
    readText(): Promise<string>;
  }>;
  directories: Readonly<{
    selectDirectory(options: NativeDirectoryPickerOptions): Promise<NativeDirectorySelection>;
  }>;
  notifications: Readonly<{
    notify(message: NativeNotification): Promise<void>;
  }>;
  links: Readonly<{
    openExternal(url: string): Promise<void>;
  }>;
  logging: Readonly<{
    append(entry: NativeLogEntry): Promise<void>;
  }>;
  builder: Readonly<{
    resolvePlatform(): Promise<NativePlatform>;
    resolveContext(): Promise<NativeBuilderContext>;
  }>;
  window: Readonly<{
    startDragging(): Promise<void>;
    closeCurrent(): Promise<void>;
    fitCurrentToContent(size: NativeDialogContentSize): Promise<void>;
    setCurrentGlassTint(tint: NativeWindowGlassTint | null): Promise<void>;
  }>;
  dialogs: Readonly<{
    openWindow(options: NativeDialogWindowOptions): Promise<void>;
  }>;
  projectCreation: Readonly<{
    openWindow(draft: NativeProjectCreationDraft): Promise<void>;
    notifyCreated(binding: NativeProjectBinding): Promise<void>;
    onCreated(handler: (binding: NativeProjectBinding) => void): Promise<NativeUnlisten>;
  }>;
  projectWorkspace: Readonly<{
    requestUnlink(target: NativeWorkspaceUnlinkTarget): Promise<void>;
    onUnlinkRequested(handler: (target: NativeWorkspaceUnlinkTarget) => void): Promise<NativeUnlisten>;
    notifyChanged(event: NativeProjectWorkspaceChanged): Promise<void>;
    onChanged(handler: (event: NativeProjectWorkspaceChanged) => void): Promise<NativeUnlisten>;
  }>;
  taskDetail: Readonly<{
    openWindow(target: NativeTaskDetailTarget): Promise<void>;
    onOpen(handler: (target: NativeTaskDetailTarget) => void): Promise<NativeUnlisten>;
    notifyChanged(event: NativeTaskDetailChanged): Promise<void>;
    onChanged(handler: (event: NativeTaskDetailChanged) => void): Promise<NativeUnlisten>;
  }>;
  workflowEditor: Readonly<{
    confirmGraphDelete(confirmation: NativeWorkflowGraphDeleteConfirmation): Promise<void>;
    onGraphDeleteConfirmed(
      handler: (confirmation: NativeWorkflowGraphDeleteConfirmation) => void,
    ): Promise<NativeUnlisten>;
  }>;
}>;

export type NativeWindowGlassTint = Readonly<{
  red: number;
  green: number;
  blue: number;
  alpha: number;
}>;

const nativeWindowGlassTintChannels = ["red", "green", "blue", "alpha"] as const;

export type NativeNotification = Readonly<{
  title: string;
  body: string;
}>;

export type NativeDirectoryPickerOptions = Readonly<{
  title: string;
}>;

export type NativeDirectorySelection = Readonly<{
  path: string;
}> | null;

export type NativeLogEntry = Readonly<{
  level: "debug" | "info" | "warn" | "error";
  message: string;
  context: Readonly<Record<string, string>>;
  occurredAt: string;
}>;

export type NativeBuilderTheme = "auto" | "light" | "dark";

export type NativeBuilderContext = Readonly<{
  serverEndpoint: string;
  persistenceRoot: string;
  platform: NativePlatform;
  theme: NativeBuilderTheme;
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

export type NativeWorkspaceUnlinkTarget = Readonly<{
  projectID: string;
  workspaceID: string;
  rootPath: string;
}>;

export type NativeProjectWorkspaceChanged = Readonly<{
  projectID: string;
}>;

export type NativeTaskDetailTarget = Readonly<{
  taskId: string;
  resumeRunId: string;
}>;

export type NativeTaskDetailChanged = Readonly<{
  taskId: string;
}>;

export type NativeWorkflowGraphDeleteConfirmation = Readonly<{
  requestID: string;
}>;

export type NativeUnlisten = () => void;

const unavailableCapabilities: NativeCapabilityState = {
  platform: "browser",
  clipboard: {
    writeText: false,
    readText: false,
  },
  directories: {
    select: false,
  },
  notifications: {
    basic: false,
  },
  links: {
    openExternal: false,
  },
  logging: {
    localFile: false,
  },
  tray: false,
  appMenu: false,
  updater: false,
  windowControls: false,
  windowDrag: false,
  dialogWindows: false,
  projectCreationWindow: false,
  taskDetailWindow: false,
  macosVibrancy: false,
};

const taskDetailWindowLabel = "native-dialog-task-detail";
const taskDetailOpenEvent = "builder://task-detail-open";
const taskDetailChangedEvent = "builder://task-detail-changed";
export const taskDetailContentMaxWidthPx = 1200;
export const taskDetailDialogHorizontalPaddingPx = 16;
export const nativeDialogWindowHorizontalInsetPx = 16;
export const taskDetailDialogOuterMaxWidthPx =
  taskDetailContentMaxWidthPx + taskDetailDialogHorizontalPaddingPx;
export const taskDetailNativeWindowInitialWidthPx = 840;
const workspaceUnlinkRequestEvent = "builder://workspace-unlink-request";
const projectWorkspaceChangedEvent = "builder://project-workspace-changed";
const workflowGraphDeleteConfirmEvent = "builder://workflow-graph-delete-confirm";

export function taskDetailNativeDialogWindowOptions(
  target: NativeTaskDetailTarget,
): NativeDialogWindowOptions {
  return {
    initialHeight: 760,
    initialWidth: taskDetailNativeWindowInitialWidthPx,
    label: taskDetailWindowLabel,
    maximizable: true,
    params: {
      resumeRunId: target.resumeRunId,
      taskId: target.taskId,
    },
    resizable: true,
    route: "/native-dialog/task-detail",
    title: "Task",
  };
}

declare global {
  interface Window {
    __TAURI_INTERNALS__?: unknown;
  }
}

export type BrowserNativeBridgeOptions = Readonly<{
  platform?: NativePlatform | undefined;
}>;

export function createBrowserNativeBridge(options: BrowserNativeBridgeOptions = {}): NativeBridge {
  const capabilities = { ...unavailableCapabilities, platform: options.platform ?? "browser" };
  return {
    capabilities,
    clipboard: {
      async writeText(): Promise<void> {
        throw new Error("Native clipboard is unavailable in this shell.");
      },
      async readText(): Promise<string> {
        throw new Error("Native clipboard is unavailable in this shell.");
      },
    },
    directories: {
      async selectDirectory(): Promise<NativeDirectorySelection> {
        throw new Error("Directory selection is unavailable in this shell.");
      },
    },
    notifications: {
      async notify(): Promise<void> {
        throw new Error("Native notifications are unavailable in this shell.");
      },
    },
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
    builder: {
      async resolvePlatform(): Promise<NativePlatform> {
        return capabilities.platform;
      },
      async resolveContext(): Promise<NativeBuilderContext> {
        return {
          serverEndpoint: "ws://127.0.0.1:53082/rpc",
          persistenceRoot: "",
          platform: capabilities.platform,
          theme: "auto",
          homePath: "",
        };
      },
    },
    window: {
      async startDragging(): Promise<void> {
        return Promise.resolve();
      },
      async closeCurrent(): Promise<void> {
        return Promise.resolve();
      },
      async fitCurrentToContent(): Promise<void> {
        return Promise.resolve();
      },
      async setCurrentGlassTint(): Promise<void> {
        return Promise.resolve();
      },
    },
    dialogs: {
      async openWindow(): Promise<void> {
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
      async onCreated(): Promise<NativeUnlisten> {
        return () => undefined;
      },
    },
    projectWorkspace: {
      async requestUnlink(): Promise<void> {
        return Promise.resolve();
      },
      async onUnlinkRequested(): Promise<NativeUnlisten> {
        return () => undefined;
      },
      async notifyChanged(): Promise<void> {
        return Promise.resolve();
      },
      async onChanged(): Promise<NativeUnlisten> {
        return () => undefined;
      },
    },
    taskDetail: {
      async openWindow(): Promise<void> {
        throw new Error("Native task detail window is unavailable in this shell.");
      },
      async onOpen(): Promise<NativeUnlisten> {
        return () => undefined;
      },
      async notifyChanged(): Promise<void> {
        return Promise.resolve();
      },
      async onChanged(): Promise<NativeUnlisten> {
        return () => undefined;
      },
    },
    workflowEditor: {
      async confirmGraphDelete(): Promise<void> {
        return Promise.resolve();
      },
      async onGraphDeleteConfirmed(): Promise<NativeUnlisten> {
        return () => undefined;
      },
    },
  };
}

export function createTauriNativeBridge(platform: NativePlatform = "unknown"): NativeBridge {
  const capabilities = createTauriCapabilities(platform);
  return {
    capabilities,
    clipboard: {
      async writeText(value: string): Promise<void> {
        await writeText(value);
      },
      async readText(): Promise<string> {
        return readText();
      },
    },
    directories: {
      async selectDirectory(options: NativeDirectoryPickerOptions): Promise<NativeDirectorySelection> {
        const path = await invoke<string | null>("select_directory", { title: options.title });
        return path === null ? null : { path };
      },
    },
    notifications: {
      async notify(): Promise<void> {
        throw new Error("Native notifications are unavailable in this shell.");
      },
    },
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
    builder: {
      async resolvePlatform(): Promise<NativePlatform> {
        return normalizeNativePlatform(await invoke<string>("resolve_native_platform"));
      },
      async resolveContext(): Promise<NativeBuilderContext> {
        return invoke<NativeBuilderContext>("resolve_builder_context");
      },
    },
    window: {
      async startDragging(): Promise<void> {
        await getCurrentWindow().startDragging();
      },
      async closeCurrent(): Promise<void> {
        await getCurrentWindow().close();
      },
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
      async openWindow(options: NativeDialogWindowOptions): Promise<void> {
        await openNativeDialogWindow(options);
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
        await emitTo("main", "builder://project-created", binding);
      },
      async onCreated(handler: (binding: NativeProjectBinding) => void): Promise<NativeUnlisten> {
        return listen<NativeProjectBinding>("builder://project-created", (event) => {
          handler(event.payload);
        });
      },
    },
    projectWorkspace: {
      async requestUnlink(target: NativeWorkspaceUnlinkTarget): Promise<void> {
        await emitTo("main", workspaceUnlinkRequestEvent, target);
      },
      async onUnlinkRequested(
        handler: (target: NativeWorkspaceUnlinkTarget) => void,
      ): Promise<NativeUnlisten> {
        return listen<NativeWorkspaceUnlinkTarget>(workspaceUnlinkRequestEvent, (event) => {
          handler(event.payload);
        });
      },
      async notifyChanged(event: NativeProjectWorkspaceChanged): Promise<void> {
        await emitTo("main", projectWorkspaceChangedEvent, event);
      },
      async onChanged(handler: (event: NativeProjectWorkspaceChanged) => void): Promise<NativeUnlisten> {
        return listen<NativeProjectWorkspaceChanged>(projectWorkspaceChangedEvent, (event) => {
          handler(event.payload);
        });
      },
    },
    taskDetail: {
      async openWindow(target: NativeTaskDetailTarget): Promise<void> {
        const existing = await WebviewWindow.getByLabel(taskDetailWindowLabel);
        if (existing !== null) {
          await retargetTaskDetailWindow(existing, target);
          return;
        }
        try {
          await openNativeDialogWindow(taskDetailNativeDialogWindowOptions(target));
        } catch (cause) {
          const raced = await WebviewWindow.getByLabel(taskDetailWindowLabel);
          if (raced !== null) {
            await retargetTaskDetailWindow(raced, target);
            return;
          }
          throw cause;
        }
      },
      async onOpen(handler: (target: NativeTaskDetailTarget) => void): Promise<NativeUnlisten> {
        return listen<NativeTaskDetailTarget>(taskDetailOpenEvent, (event) => {
          handler(event.payload);
        });
      },
      async notifyChanged(event: NativeTaskDetailChanged): Promise<void> {
        await emitTo("main", taskDetailChangedEvent, event);
      },
      async onChanged(handler: (event: NativeTaskDetailChanged) => void): Promise<NativeUnlisten> {
        return listen<NativeTaskDetailChanged>(taskDetailChangedEvent, (event) => {
          handler(event.payload);
        });
      },
    },
    workflowEditor: {
      async confirmGraphDelete(confirmation: NativeWorkflowGraphDeleteConfirmation): Promise<void> {
        await emitTo("main", workflowGraphDeleteConfirmEvent, confirmation);
      },
      async onGraphDeleteConfirmed(
        handler: (confirmation: NativeWorkflowGraphDeleteConfirmation) => void,
      ): Promise<NativeUnlisten> {
        return listen<NativeWorkflowGraphDeleteConfirmation>(workflowGraphDeleteConfirmEvent, (event) => {
          handler(event.payload);
        });
      },
    },
  };
}

export function createAutoNativeBridge(platform: NativePlatform = "unknown"): NativeBridge {
  return isTauriRuntime()
    ? createTauriNativeBridge(platform)
    : createBrowserNativeBridge({ platform: "browser" });
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && window.__TAURI_INTERNALS__ !== undefined;
}

function normalizeNativePlatform(platform: string): NativePlatform {
  if (platform === "linux" || platform === "macos" || platform === "windows") {
    return platform;
  }
  return "unknown";
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

async function retargetTaskDetailWindow(
  window: WebviewWindow,
  target: NativeTaskDetailTarget,
): Promise<void> {
  await window.emit(taskDetailOpenEvent, target);
  await window.setFocus();
}

function createTauriCapabilities(platform: NativePlatform): NativeCapabilityState {
  return {
    platform,
    clipboard: {
      writeText: true,
      readText: true,
    },
    directories: {
      select: true,
    },
    notifications: {
      basic: false,
    },
    links: {
      openExternal: true,
    },
    logging: {
      localFile: true,
    },
    tray: false,
    appMenu: false,
    updater: false,
    windowControls: false,
    windowDrag: true,
    dialogWindows: true,
    projectCreationWindow: true,
    taskDetailWindow: true,
    macosVibrancy: false,
  };
}
