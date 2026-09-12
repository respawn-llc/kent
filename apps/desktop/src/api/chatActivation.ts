import { create } from "@app/server-api-contract";
import {
  SessionLaunchMode,
  SessionLaunchService,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_launch_pb";
import { SessionRuntimeService } from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";

import { ContractError } from "./errors";
import { requireUnarySuccess } from "./protobufRpc";
import type { RuntimeOwnerContext } from "./transport";

export async function activateRuntime(
  owner: RuntimeOwnerContext,
  sessionID: string,
): Promise<Readonly<{ sessionID: string; generation: number }>> {
  const planMethod = SessionLaunchService.method.plan;
  const result = await owner.callDescriptor(
    planMethod,
    create(planMethod.input, {
      mode: SessionLaunchMode.INTERACTIVE,
      intent: { intent: { case: "openExistingSessionId", value: sessionID } },
    }),
  );
  const plan = requireUnarySuccess(planMethod, result).plan;
  if (plan?.sessionId !== sessionID)
    throw new ContractError("Session Plan does not match the requested Session.");
  try {
    const method = SessionRuntimeService.method.activate;
    const response = requireUnarySuccess(
      method,
      await owner.callDescriptor(
        method,
        create(method.input, {
          sessionId: sessionID,
          activeSettings: plan.activeSettings,
          enabledToolIds: plan.enabledToolIds,
          questionsEnabled: plan.questionsEnabled,
          autoCompactionEnabled: plan.autoCompactionEnabled,
          thinkingOverrideExplicit: plan.thinkingOverrideExplicit,
          source: plan.source,
          agentSelection: plan.activationAgentSelection,
        }),
      ),
    );
    if (response.attachment?.sessionId !== sessionID)
      throw new ContractError("Runtime activation does not match the requested Session.");
    return { sessionID: response.attachment.sessionId, generation: Number(response.attachment.generation) };
  } catch (error) {
    owner.poison();
    throw error;
  }
}
