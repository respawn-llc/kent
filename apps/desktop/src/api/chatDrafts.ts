import { create } from "@app/server-api-contract";
import { SessionLifecycleService } from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";

import { requireChatSuccess } from "./chatErrors";
import { requireChatSessionID } from "./chatTarget";
import type { ChatApi } from "./chatTypes";
import type { DescriptorRpcTransport } from "./transport";

export function createChatDraftApi(
  transport: DescriptorRpcTransport,
): Pick<ChatApi, "getDraft" | "persistDraft"> {
  return {
    async getDraft(target) {
      const method = SessionLifecycleService.method.getInitialInput;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, { sessionId: requireChatSessionID(target) }),
      );
      return requireChatSuccess(method, result).input;
    },
    async persistDraft(target, input) {
      const method = SessionLifecycleService.method.persistInputDraft;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, { sessionId: requireChatSessionID(target), input }),
      );
      requireChatSuccess(method, result);
    },
  };
}
