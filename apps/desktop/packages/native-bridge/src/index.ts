export type NativeCapabilityState = Readonly<{
  clipboard: Readonly<{
    writeText: boolean;
    readText: boolean;
  }>;
  notifications: Readonly<{
    basic: boolean;
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
  notifications: Readonly<{
    notify(message: NativeNotification): Promise<void>;
  }>;
}>;

export type NativeNotification = Readonly<{
  title: string;
  body: string;
}>;

const unavailableCapabilities: NativeCapabilityState = {
  clipboard: {
    writeText: false,
    readText: false,
  },
  notifications: {
    basic: false,
  },
  tray: false,
  appMenu: false,
  updater: false,
  windowControls: false,
  macosVibrancy: false,
};

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
    notifications: {
      async notify(): Promise<void> {
        throw new Error("Native notifications are unavailable in this shell.");
      },
    },
  };
}
