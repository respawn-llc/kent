import { create } from "@app/server-api-contract";
import { ApprovalService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { listPendingAsks } from "./clientTaskDetail";
import { approvalPrompt, orderPendingPrompts } from "./promptPresentation";
import { requireUnarySuccess } from "./protobufRpc";
import type { PendingPrompt } from "./promptModels";
import type { DescriptorRpcTransport } from "./transport";

export async function listPendingPrompts(
  transport: DescriptorRpcTransport,
  sessionID: string,
): Promise<readonly PendingPrompt[]> {
  const method = ApprovalService.method.listPending;
  const [questions, approvals] = await Promise.all([
    listPendingAsks(transport, sessionID),
    transport.callDescriptorAttachedSession(
      sessionID,
      method,
      create(method.input, { sessionId: sessionID }),
    ),
  ]);
  return orderPendingPrompts([
    ...questions.map((question) => ({ ...question, kind: "ordinary" as const })),
    ...requireUnarySuccess(method, approvals).approvals.map(approvalPrompt),
  ]);
}
