import { invoke } from "@tauri-apps/api/core";

export type NativeCapabilityState = Readonly<{
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
  terminal: Readonly<{
    launchBuilderSession: boolean;
  }>;
  logging: Readonly<{
    localFile: boolean;
  }>;
  tray: boolean;
  appMenu: boolean;
  updater: boolean;
  windowControls: boolean;
  macosVibrancy: boolean;
}>;

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
  terminal: Readonly<{
    launchBuilderSession(target: NativeBuilderSessionLaunch): Promise<void>;
  }>;
  logging: Readonly<{
    append(entry: NativeLogEntry): Promise<void>;
  }>;
  builder: Readonly<{
    resolveContext(): Promise<NativeBuilderContext>;
  }>;
}>;

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

export type NativeBuilderSessionLaunch = Readonly<{
  sessionId: string;
  cwd: string;
}>;

export type NativeLogEntry = Readonly<{
  level: "debug" | "info" | "warn" | "error";
  message: string;
  context: Readonly<Record<string, string>>;
  occurredAt: string;
}>;

export type NativeBuilderContext = Readonly<{
  serverEndpoint: string;
  persistenceRoot: string;
}>;

const unavailableCapabilities: NativeCapabilityState = {
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
  terminal: {
    launchBuilderSession: false,
  },
  logging: {
    localFile: false,
  },
  tray: false,
  appMenu: false,
  updater: false,
  windowControls: false,
  macosVibrancy: false,
};

declare global {
  interface Window {
    __TAURI_INTERNALS__?: unknown;
  }
}

export function createBrowserNativeBridge(): NativeBridge {
  return {
    capabilities: unavailableCapabilities,
    clipboard: {
      async writeText(): Promise<void> {
        throw new Error("Clipboard write is unavailable in this shell.");
      },
      async readText(): Promise<string> {
        throw new Error("Clipboard read is unavailable in this shell.");
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
        window.open(url, "_blank", "noopener,noreferrer");
      },
    },
    terminal: {
      async launchBuilderSession(): Promise<void> {
        throw new Error("Terminal teleport is unavailable in this shell.");
      },
    },
    logging: {
      async append(): Promise<void> {
        return Promise.resolve();
      },
    },
    builder: {
      async resolveContext(): Promise<NativeBuilderContext> {
        return { serverEndpoint: "ws://127.0.0.1:53082/rpc", persistenceRoot: "" };
      },
    },
  };
}

export function createTauriNativeBridge(): NativeBridge {
  const capabilities = createTauriCapabilities();
  return {
    capabilities,
    clipboard: {
      async writeText(): Promise<void> {
        throw new Error("Clipboard write is unavailable in this shell.");
      },
      async readText(): Promise<string> {
        throw new Error("Clipboard read is unavailable in this shell.");
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
    terminal: {
      async launchBuilderSession(target: NativeBuilderSessionLaunch): Promise<void> {
        await invoke("launch_builder_session", { sessionId: target.sessionId, cwd: target.cwd });
      },
    },
    logging: {
      async append(entry: NativeLogEntry): Promise<void> {
        await invoke("append_gui_log", { entry: JSON.stringify(entry) });
      },
    },
    builder: {
      async resolveContext(): Promise<NativeBuilderContext> {
        return invoke<NativeBuilderContext>("resolve_builder_context");
      },
    },
  };
}

export function createAutoNativeBridge(): NativeBridge {
  return isTauriRuntime() ? createTauriNativeBridge() : createBrowserNativeBridge();
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

function createTauriCapabilities(): NativeCapabilityState {
  return {
    clipboard: {
      writeText: false,
      readText: false,
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
    terminal: {
      launchBuilderSession: isMacOS(),
    },
    logging: {
      localFile: true,
    },
    tray: false,
    appMenu: false,
    updater: false,
    windowControls: false,
    macosVibrancy: false,
  };
}

function isMacOS(): boolean {
  return /Mac OS|Macintosh/u.test(navigator.userAgent);
}
