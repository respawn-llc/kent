import { createBrowserNativeBridge } from "@app/native-bridge";

import { ApiClient } from "../api";
import { FakeRpcTransport, type FakeRoute } from "../api/fakeTransport";
import { protocolVersion } from "../api/jsonRpcSocket";
import { createGuiLogger } from "../app/logging";
import type { AppServices } from "../app/services";

export type TestAppServices = AppServices &
  Readonly<{
    transport: FakeRpcTransport;
  }>;

export type CreateTestServicesOptions = Readonly<{
  debugThemeOverrideEnabled?: boolean | undefined;
  homePath?: string | undefined;
}>;

export function createTestServices(
  routes: readonly FakeRoute[],
  nativeBridge = createBrowserNativeBridge(),
  options: CreateTestServicesOptions = {},
): TestAppServices {
  const transport = new FakeRpcTransport(routes);
  return {
    api: new ApiClient(transport),
    debugThemeOverrideEnabled: options.debugThemeOverrideEnabled ?? false,
    endpoint: "ws://127.0.0.1:53082/rpc",
    homePath: options.homePath ?? "",
    logger: createGuiLogger(nativeBridge),
    nativeBridge,
    transport,
  };
}

export const startupRoutes: readonly FakeRoute[] = [
  {
    method: "server.readiness.get",
    result: {
      ready: true,
      server_id: "server-1",
      server_version: "1.3.0",
      server_build: "1.3.0",
      protocol_version: protocolVersion,
      auth_ready: true,
      auth_required: true,
      endpoint: "ws://127.0.0.1:53082/rpc",
      subagent_roles: [{ name: "default" }, { name: "fast" }, { name: "coder" }, { name: "reviewer" }],
    },
  },
  {
    method: "project.home.list",
    result: {
      projects: [],
      next_page_token: "",
      generated_at_unix_ms: 1,
    },
  },
  {
    method: "workflow.attention.list",
    result: {
      items: [],
      next_page_token: "",
      generated_at_unix_ms: 1,
    },
  },
  {
    method: "workflow.task.comment.list",
    result: {
      comments: [],
      next_page_token: "",
    },
  },
];
