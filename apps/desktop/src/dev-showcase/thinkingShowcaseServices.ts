import { createBrowserNativeBridge } from "@app/native-bridge";

import { ApiClient, protocolVersion, type DescriptorRpcTransport } from "@/api/composition";
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
    call: unavailable,
    callDedicated: unavailable,
    callAttachedProject: unavailable,
    callDescriptorAttachedSession: unavailable,
    callDescriptor: unavailable,
    callDescriptorAttachedProject: unavailable,
    runRuntimeOwner: unavailable,
    subscribe: unavailable,
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
