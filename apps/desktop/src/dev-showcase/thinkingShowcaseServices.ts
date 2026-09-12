import { createBrowserNativeBridge } from "@app/native-bridge";

import { ApiClient, ConnectionStore, protocolVersion, type DescriptorRpcTransport } from "@/api/composition";
import type { AppServices } from "@/app-facade";

function unavailable(): never {
  throw new Error("This browser fixture does not connect to a Kent server.");
}

export function thinkingShowcaseServices(writeText: (value: string) => Promise<void>): AppServices {
  const browser = createBrowserNativeBridge();
  const nativeBridge = {
    ...browser,
    capabilities: {
      ...browser.capabilities,
      clipboard: { ...browser.capabilities.clipboard, writeText: true },
    },
    clipboard: { ...browser.clipboard, writeText },
  };
  const transport: DescriptorRpcTransport = {
    connection: new ConnectionStore(),
    call: unavailable,
    callDedicated: unavailable,
    callAttachedProject: unavailable,
    callAttachedSession: unavailable,
    callDescriptor: unavailable,
    callDescriptorAttachedProject: unavailable,
    runRuntimeOwner: unavailable,
    subscribe: unavailable,
    subscribeChatSession: unavailable,
    subscribeDescriptor: unavailable,
  };
  return {
    api: new ApiClient(transport),
    debugThemeOverrideEnabled: false,
    endpoint: "fixture-only",
    homePath: "/fixture-only",
    logger: {
      append: async (level, message, context = {}) => {
        await nativeBridge.logging.append({ level, message, context, occurredAt: new Date().toISOString() });
      },
    },
    nativeBridge,
    protocolVersion,
    storageNamespace: null,
  };
}
