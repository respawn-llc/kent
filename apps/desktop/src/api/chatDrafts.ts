import { create } from "@app/server-api-contract";
import { SessionLifecycleService } from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";

import { requireProjectAttachment } from "./chatAttachment";
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
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method,
        createRequest: () => create(method.input, { sessionId: requireChatSessionID(target) }),
      });
      requireProjectAttachment(call.attachment, target);
      return requireChatSuccess(method, call.result).input;
    },
    async persistDraft(target, input) {
      const method = SessionLifecycleService.method.persistInputDraft;
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method,
        createRequest: () => create(method.input, { sessionId: requireChatSessionID(target), input }),
      });
      requireProjectAttachment(call.attachment, target);
      requireChatSuccess(method, call.result);
    },
  };
}
