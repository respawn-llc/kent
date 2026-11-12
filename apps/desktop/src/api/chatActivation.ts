import { create } from "@app/server-api-contract";
import {
  SessionLaunchMode,
  SessionLaunchService,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_launch_pb";
import { requireUnarySuccess } from "./protobufRpc";
import type { RpcTransport } from "./transport";
import type { ChatMutationTarget } from "./chatTypes";
import { newChatToWire } from "./newChat";
import { requireProjectAttachment } from "./chatAttachment";

export async function planNewChat(
  transport: RpcTransport,
  target: Extract<ChatMutationTarget, { kind: "new_chat" }>,
  draft: string,
): Promise<string> {
  const method = SessionLaunchService.method.plan;
  const call = await transport.callDescriptorAttachedProject({
    projectID: target.projectID,
    selector: target.workspace,
    method,
    createRequest: (attachment) => {
      const creation = newChatToWire(target, attachment.workspaceID);
      return create(method.input, {
        mode: SessionLaunchMode.INTERACTIVE,
        intent: { intent: { case: "createNew", value: { origin: { case: "independent", value: {} } } } },
        initialChatSettings: creation.initialSettings,
        initialName: creation.name,
        initialInputDraft: draft,
      });
    },
  });
  requireProjectAttachment(call.attachment, target);
  return requireUnarySuccess(method, call.result).plan.sessionId;
}
