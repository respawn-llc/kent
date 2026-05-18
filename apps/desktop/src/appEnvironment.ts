import { createAutoNativeBridge } from "@builder/desktop-native-bridge";

import { BuilderApiClient, createJsonRpcTransport } from "./api";
import { ConnectionStore } from "./api/connectionStore";
import { StartupConfigurationError } from "./api/errors";
import type { JsonValue } from "./api/json";
import type { RpcEventHandler, RpcSubscription, RpcTransport } from "./api/transport";
import { createGuiLogger } from "./app/logging";
import type { AppServices } from "./app/services";

const defaultServerEndpoint = "ws://127.0.0.1:53082/rpc";
const builderThemeAttribute = "data-builder-theme";
const nativeDialogThemeSearchParam = "__builderTheme";
let productionContextMenuGuardInstalled = false;

export async function createDefaultAppServices(): Promise<AppServices> {
  installProductionContextMenuGuard(import.meta.env.PROD);
  applyNativeDialogThemeOverride();
  const nativeBridge = createAutoNativeBridge();
  const logger = createGuiLogger(nativeBridge);
  const context = await nativeBridge.builder
    .resolveContext()
    .catch((error: unknown) => new StartupConfigurationError(errorMessage(error)));
  if (context instanceof Error) {
    await logger.append("error", "Builder native context resolution failed.", { error: context.message });
    return {
      api: new BuilderApiClient(new BootstrapErrorTransport(context)),
      debugThemeOverrideEnabled: import.meta.env.DEV,
      endpoint: defaultServerEndpoint,
      logger,
      nativeBridge,
    };
  }
  applyConfiguredTheme(context.theme);
  applyNativeDialogThemeOverride();
  const endpoint = context.serverEndpoint.length > 0 ? context.serverEndpoint : defaultServerEndpoint;
  const api = new BuilderApiClient(createJsonRpcTransport(endpoint));
  return {
    api,
    debugThemeOverrideEnabled: import.meta.env.DEV,
    endpoint,
    logger,
    nativeBridge,
  };
}

export function applyConfiguredTheme(theme: string): void {
  if (typeof document === "undefined") {
    return;
  }
  if (theme === "light" || theme === "dark") {
    document.documentElement.setAttribute(builderThemeAttribute, theme);
    return;
  }
  document.documentElement.removeAttribute(builderThemeAttribute);
}

export function applyNativeDialogThemeOverride(): void {
  const theme = readNativeDialogThemeOverride();
  if (theme !== null) {
    setInMemoryThemeOverride(theme);
  }
}

export type BuilderTheme = "light" | "dark";

export function readEffectiveTheme(): BuilderTheme {
  if (typeof document !== "undefined") {
    const configured = document.documentElement.getAttribute(builderThemeAttribute);
    if (configured === "light" || configured === "dark") {
      return configured;
    }
  }
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return "dark";
  }
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function setInMemoryThemeOverride(theme: BuilderTheme): void {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.setAttribute(builderThemeAttribute, theme);
}

function readNativeDialogThemeOverride(): BuilderTheme | null {
  if (typeof window === "undefined" || !window.location.pathname.startsWith("/native-dialog/")) {
    return null;
  }
  const theme = new URLSearchParams(window.location.search).get(nativeDialogThemeSearchParam);
  return theme === "light" || theme === "dark" ? theme : null;
}

export function toggleInMemoryThemeOverride(): BuilderTheme {
  const nextTheme = readEffectiveTheme() === "dark" ? "light" : "dark";
  setInMemoryThemeOverride(nextTheme);
  return nextTheme;
}

export function installProductionContextMenuGuard(isProduction: boolean): void {
  if (!isProduction || productionContextMenuGuardInstalled || typeof document === "undefined") {
    return;
  }
  productionContextMenuGuardInstalled = true;
  document.addEventListener(
    "contextmenu",
    (event) => {
      event.preventDefault();
    },
    { capture: true },
  );
}

class BootstrapErrorTransport implements RpcTransport {
  readonly connection = new ConnectionStore();
  readonly #error: Error;

  constructor(error: Error) {
    this.#error = error;
    this.connection.set("disconnected", error.message);
  }

  async call(): Promise<unknown> {
    throw this.#error;
  }

  subscribe(_method: string, _params: JsonValue, handler: RpcEventHandler): RpcSubscription {
    handler.onError(this.#error);
    return {
      close() {
        return;
      },
    };
  }
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  return "Unknown native context error.";
}
