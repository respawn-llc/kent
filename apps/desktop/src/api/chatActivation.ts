import { create } from "@app/server-api-contract";
import {
  SessionLaunchMode,
  SessionLaunchService,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_launch_pb";
import { SessionRuntimeService } from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";

import { ContractError } from "./errors";
import { requireUnarySuccess } from "./protobufRpc";
import type { RpcTransport } from "./transport";
import type { ChatSessionTarget } from "./chatTypes";
import { requireChatSessionID } from "./chatTarget";

export async function activateRuntime(transport: RpcTransport, target: ChatSessionTarget): Promise<void> {
  const sessionID = requireChatSessionID(target);
  const planMethod = SessionLaunchService.method.plan;
  const result = await transport.callDescriptorAttachedSession(
    target,
    planMethod,
    create(planMethod.input, {
      mode: SessionLaunchMode.INTERACTIVE,
      intent: {
        intent: { case: "openExistingSessionId", value: { value: { case: "sessionId", value: sessionID } } },
      },
    }),
  );
  const plan = requireUnarySuccess(planMethod, result).plan;
  if (plan?.sessionId !== sessionID)
    throw new ContractError("Session Plan does not match the requested Session.");
  const method = SessionRuntimeService.method.activate;
  requireUnarySuccess(
    method,
    await transport.callDescriptorAttachedSession(
      target,
      method,
      create(method.input, {
        sessionId: { value: { case: "sessionId", value: sessionID } },
        options: {
          connectionScope: plan.connectionScope,
          activeSettings: plan.activeSettings,
          enabledToolIds: plan.enabledToolIds,
          questionsEnabled: plan.questionsEnabled,
          autoCompactionEnabled: plan.autoCompactionEnabled,
          thinkingOverrideExplicit: plan.thinkingOverrideExplicit,
          source: plan.source,
          agentSelection: plan.activationAgentSelection,
          explicitToolSelection: plan.explicitToolSelection,
        },
      }),
    ),
  );
}
