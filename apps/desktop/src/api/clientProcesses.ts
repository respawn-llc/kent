import { create } from "@app/server-api-contract";
import {
  ControlService,
  ViewService,
  type BackgroundProcess,
} from "@app/server-api-contract/gen/kent/api/process/process_pb";

import { timestampMillis } from "./clientTime";
import { requireUnarySuccess } from "./protobufRpc";
import type { DesktopProcess } from "./processes";
import type { ChatSessionTarget } from "./chatTypes";
import type { DescriptorRpcTransport } from "./transport";

export async function listProcesses(
  transport: DescriptorRpcTransport,
  target: ChatSessionTarget,
): Promise<readonly DesktopProcess[]> {
  const method = ViewService.method.list;
  const request = create(method.input, { projectId: target.projectID, ownerSessionId: target.sessionID });
  const success = requireUnarySuccess(method, await transport.callDescriptor(method, request));
  return success.processes.map(processFromGenerated);
}

export async function killProcess(transport: DescriptorRpcTransport, processID: string): Promise<void> {
  const method = ControlService.method.kill;
  const request = create(method.input, { processId: processID.trim() });
  requireUnarySuccess(method, await transport.callDescriptor(method, request));
}

function processFromGenerated(process: BackgroundProcess): DesktopProcess {
  if (process.startedAt === undefined) {
    throw new Error("Process start time is required.");
  }
  return {
    id: process.id,
    state: process.state,
    command: process.command,
    workdir: process.workdir,
    startedAt: timestampMillis(process.startedAt),
    finishedAt: process.finishedAt === undefined ? null : timestampMillis(process.finishedAt),
    exitCode: process.exitCode ?? null,
    recentOutput: process.recentOutput,
    running: process.running,
    killRequested: process.killRequested,
  };
}
