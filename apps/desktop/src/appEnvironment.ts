import { createAutoNativeBridge } from "@builder/desktop-native-bridge";

import { BuilderApiClient, createJsonRpcTransport } from "./api";
import { ConnectionStore } from "./api/connectionStore";
import { StartupConfigurationError } from "./api/errors";
import type { JsonValue } from "./api/json";
import type { RpcEventHandler, RpcSubscription, RpcTransport } from "./api/transport";
import { createGuiLogger } from "./app/logging";
import type { AppServices } from "./app/services";

const defaultServerEndpoint = "ws://127.0.0.1:53082/rpc";

export async function createDefaultAppServices(): Promise<AppServices> {
  const nativeBridge = createAutoNativeBridge();
  const logger = createGuiLogger(nativeBridge);
  const context = await nativeBridge.builder.resolveContext().catch((error: unknown) => new StartupConfigurationError(errorMessage(error)));
  if (context instanceof Error) {
    await logger.append("error", "Builder native context resolution failed.", { error: context.message });
    return {
      api: new BuilderApiClient(new BootstrapErrorTransport(context)),
      endpoint: defaultServerEndpoint,
      logger,
      nativeBridge,
    };
  }
  const endpoint = context.serverEndpoint.length > 0 ? context.serverEndpoint : defaultServerEndpoint;
  const api = new BuilderApiClient(createJsonRpcTransport(endpoint));
  return {
    api,
    endpoint,
    logger,
    nativeBridge,
  };
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
