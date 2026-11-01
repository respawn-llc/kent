import { createAutoNativeBridge, type NativePlatform } from "@app/native-bridge";
import { z } from "zod";

import { StartupConfigurationError } from "@/api";
import { ApiClient, createRpcTransport, protocolVersion, type RpcTransport } from "@/api/composition";
import { projectEventDiagnostics, type AppServices, type AppStorageNamespace } from "@/app-facade";
import { readEffectiveTheme, type AppTheme } from "@/ui";
import { createGuiLogger } from "../logging";
import { createSpeechTransfer } from "./speechTransfer";

const defaultServerEndpoint = "ws://127.0.0.1:53082/rpc";
const platformAttribute = "data-platform";
const themeAttribute = "data-theme";
const nativeDialogThemeSearchParam = "__appTheme";
let productionContextMenuGuardInstalled = false;
const stringValueSchema = z.string();

export async function createDefaultAppServices(): Promise<AppServices> {
  installProductionContextMenuGuard(import.meta.env.PROD);
  applyNativeDialogThemeOverride();
  const bootstrapNativeBridge = createAutoNativeBridge("unknown");
  const platform = await bootstrapNativeBridge.app.resolvePlatform().catch(() => "unknown" as const);
  applyNativePlatform(platform);
  const nativeBridge = createAutoNativeBridge(platform);
  const logger = createGuiLogger(nativeBridge);
  const context = await nativeBridge.app
    .resolveContext()
    .catch((error: unknown) => new StartupConfigurationError(errorMessage(error)));
  if (context instanceof Error) {
    await logger.append("error", "Native context resolution failed.", {
      error: context.message,
    });
    return {
      api: new ApiClient(new BootstrapErrorTransport(context), projectEventDiagnostics(logger)),
      speech: createSpeechTransfer(nativeBridge, defaultServerEndpoint),
      debugThemeOverrideEnabled: import.meta.env.DEV,
      endpoint: defaultServerEndpoint,
      homePath: "",
      logger,
      nativeBridge,
      protocolVersion,
      storageNamespace: null,
    };
  }
  applyConfiguredTheme(context.theme);
  applyNativeDialogThemeOverride();
  const browserEndpoint =
    nativeBridge.capabilities.platform === "browser" ? __KENT_BROWSER_RPC_ENDPOINT__ : null;
  const endpoint =
    browserEndpoint ?? (context.serverEndpoint.length > 0 ? context.serverEndpoint : defaultServerEndpoint);
  // Browser QA points at an arbitrary endpoint, so root validation only applies
  // to the native-resolved server. context.persistenceRootId is empty for the
  // default root (validation skipped).
  const expectedRootId = browserEndpoint === null ? context.persistenceRootId : "";
  const api = new ApiClient(
    createRpcTransport(
      endpoint,
      (error) => {
        void logger.append("error", "Invalid server response.", {
          error: error instanceof Error ? (error.stack ?? error.message) : String(error),
        });
      },
      expectedRootId,
    ),
    projectEventDiagnostics(logger),
  );
  return {
    api,
    speech: createSpeechTransfer(nativeBridge, endpoint),
    debugThemeOverrideEnabled: import.meta.env.DEV,
    endpoint,
    homePath: context.homePath,
    logger,
    nativeBridge,
    protocolVersion,
    storageNamespace: resolveAppStorageNamespace(nativeBridge.capabilities.platform, {
      endpoint,
      persistenceRoot: context.persistenceRoot,
    }),
  };
}

export function resolveAppStorageNamespace(
  platform: NativePlatform,
  source: Readonly<{
    endpoint: string;
    persistenceRoot: string | null;
  }>,
): AppStorageNamespace | null {
  if (platform === "browser") {
    return {
      kind: "browser-endpoint",
      identity: source.endpoint,
    };
  }
  if (source.persistenceRoot === null || source.persistenceRoot.trim().length === 0) {
    return null;
  }
  return {
    kind: "native-persistence-root",
    identity: source.persistenceRoot,
  };
}

export function applyNativePlatform(platform: NativePlatform): void {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.setAttribute(platformAttribute, platform);
}

export function applyConfiguredTheme(theme: string): void {
  if (typeof document === "undefined") {
    return;
  }
  if (theme === "light" || theme === "dark") {
    document.documentElement.setAttribute(themeAttribute, theme);
    return;
  }
  document.documentElement.removeAttribute(themeAttribute);
}

export function applyNativeDialogThemeOverride(): void {
  if (!import.meta.env.DEV) return;
  const theme = readNativeDialogThemeOverride();
  if (theme !== null) {
    setInMemoryThemeOverride(theme);
  }
}

export function setInMemoryThemeOverride(theme: AppTheme): void {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.setAttribute(themeAttribute, theme);
}

function readNativeDialogThemeOverride(): AppTheme | null {
  if (typeof window === "undefined" || !window.location.pathname.startsWith("/native-dialog/")) {
    return null;
  }
  const theme = new URLSearchParams(window.location.search).get(nativeDialogThemeSearchParam);
  return theme === "light" || theme === "dark" ? theme : null;
}

export function toggleInMemoryThemeOverride(): AppTheme {
  const nextTheme = readEffectiveTheme() === "dark" ? "light" : "dark";
  setInMemoryThemeOverride(nextTheme);
  return nextTheme;
}

export function installProductionContextMenuGuard(isProduction: boolean): void {
  if (!isProduction || productionContextMenuGuardInstalled || typeof document === "undefined") {
    return;
  }
  productionContextMenuGuardInstalled = true;
  document.addEventListener("contextmenu", (event) => {
    event.preventDefault();
  });
}

class BootstrapErrorTransport implements RpcTransport {
  readonly #error: Error;

  constructor(error: Error) {
    this.#error = error;
  }

  async callDescriptor(): Promise<never> {
    throw this.#error;
  }

  async callDescriptorAttachedProject(): Promise<never> {
    throw this.#error;
  }

  readonly subscribeDescriptor: RpcTransport["subscribeDescriptor"] = (input) => {
    input.handler.onError(this.#error);
    return {
      close() {
        return;
      },
    };
  };

  async callDescriptorAttachedSession(): Promise<never> {
    throw this.#error;
  }
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  const parsed = stringValueSchema.safeParse(error);
  if (parsed.success) {
    return parsed.data;
  }
  return "Unknown native context error.";
}
