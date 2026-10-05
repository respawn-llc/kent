import { create } from "@app/server-api-contract";
import { ApprovalService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { listPendingAsks } from "./clientTaskDetail";
import { approvalPrompt, orderPendingPrompts } from "./promptPresentation";
import { requireUnarySuccess } from "./protobufRpc";
import type { PendingPrompt } from "./promptModels";
import type { DescriptorRpcTransport, SessionAttachmentTarget } from "./transport";

export async function listPendingPrompts(
  transport: DescriptorRpcTransport,
  target: SessionAttachmentTarget,
): Promise<readonly PendingPrompt[]> {
  const method = ApprovalService.method.listPending;
  const [questions, approvals] = await Promise.all([
    listPendingAsks(transport, target),
    transport.callDescriptorAttachedSession(
      target,
      method,
      create(method.input, { sessionId: target.sessionID }),
    ),
  ]);
  return orderPendingPrompts([
    ...questions.map((question) => ({ ...question, kind: "ordinary" as const })),
    ...requireUnarySuccess(method, approvals).approvals.map(approvalPrompt),
  ]);
}
