import {
  createAutoNativeBridge,
  parseBrowserServerAddress,
  type NativeBridge,
  type NativePlatform,
  type SettingsSnapshot,
} from "@app/native-bridge";

import { StartupConfigurationError } from "@/api";
import { ApiClient, createRpcTransport, protocolVersion, type RpcTransport } from "@/api/composition";
import { projectEventDiagnostics, type AppServices, type AppStorageNamespace } from "@/app-facade";
import { createGuiLogger } from "../logging";
import { createSpeechTransfer } from "./speechTransfer";

const defaultServerEndpoint = "ws://127.0.0.1:53082/rpc";
const platformAttribute = "data-platform";
const themeAttribute = "data-theme";
let productionContextMenuGuardInstalled = false;

export type SettingsStartupFailure = Readonly<{
  kind: "settings";
  nativeBridge: NativeBridge;
  error: unknown;
  recovered: boolean;
}>;

export async function createDefaultAppServices(): Promise<AppServices | SettingsStartupFailure> {
  installProductionContextMenuGuard(import.meta.env.PROD);
  const bootstrapNativeBridge = createAutoNativeBridge("unknown");
  const platform = await bootstrapNativeBridge.app.resolvePlatform().catch(() => "unknown" as const);
  applyNativePlatform(platform);
  const browserEndpoint = platform === "browser" ? readBrowserRpcEndpoint() : null;
  const nativeBridge = createAutoNativeBridge(
    platform,
    browserEndpoint === null || browserEndpoint instanceof Error ? undefined : browserEndpoint,
  );
  let initialSettings: SettingsSnapshot;
  try {
    initialSettings = await nativeBridge.settings.read();
  } catch (error) {
    return { kind: "settings", nativeBridge, error, recovered: false };
  }
  if (initialSettings.launch === null) {
    return { kind: "settings", nativeBridge, error: null, recovered: true };
  }
  applyConfiguredTheme(initialSettings.saved.theme);
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
      initialSettings,
      speech: createSpeechTransfer(nativeBridge, defaultServerEndpoint),
      endpoint: defaultServerEndpoint,
      homePath: "",
      logger,
      nativeBridge,
      protocolVersion,
      storageNamespace: null,
    };
  }
  if (browserEndpoint instanceof Error) {
    await logger.append("error", "Browser RPC endpoint configuration failed.", {
      error: browserEndpoint.message,
    });
    return {
      api: new ApiClient(new BootstrapErrorTransport(browserEndpoint), projectEventDiagnostics(logger)),
      initialSettings,
      speech: createSpeechTransfer(nativeBridge, defaultServerEndpoint),
      endpoint: defaultServerEndpoint,
      homePath: context.homePath,
      logger,
      nativeBridge,
      protocolVersion,
      storageNamespace: null,
    };
  }
  const endpoint = browserEndpoint ?? context.serverEndpoint;
  const api = new ApiClient(
    createRpcTransport(endpoint, (error) => {
      void logger.append("error", "Invalid server response.", {
        error: error instanceof Error ? (error.stack ?? error.message) : String(error),
      });
    }),
    projectEventDiagnostics(logger),
  );
  return {
    api,
    initialSettings,
    speech: createSpeechTransfer(nativeBridge, endpoint),
    endpoint,
    homePath: context.homePath,
    logger,
    nativeBridge,
    protocolVersion,
    storageNamespace: resolveAppStorageNamespace(endpoint),
  };
}

export function resolveAppStorageNamespace(endpoint: string): AppStorageNamespace {
  return { kind: "server-address", identity: endpoint };
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

function readBrowserRpcEndpoint(): string | StartupConfigurationError {
  const address =
    new URLSearchParams(window.location.search).get("appRpcEndpoint") ?? __KENT_BROWSER_RPC_ENDPOINT__;
  try {
    return parseBrowserServerAddress(address);
  } catch (error) {
    return new StartupConfigurationError(errorMessage(error));
  }
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
  if (typeof error === "string") return error;
  return "Unknown native context error.";
}
