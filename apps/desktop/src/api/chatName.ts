import { create } from "@app/server-api-contract";
import { SettingsService } from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { requireChatSuccess } from "./chatErrors";
import { requireChatSessionID } from "./chatTarget";
import type { ChatApi } from "./chatTypes";
import type { RpcTransport } from "./transport";

export function createChatNameApi(transport: RpcTransport): Pick<ChatApi, "setSessionName"> {
  return {
    async setSessionName(target, mutation) {
      const method = SettingsService.method.setSessionName;
      const response = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, {
          sessionId: requireChatSessionID(target),
          mutation: {
            action:
              mutation.kind === "set" ? { case: "set", value: mutation.name } : { case: "clear", value: {} },
          },
        }),
      );
      requireChatSuccess(method, response);
    },
  };
}
