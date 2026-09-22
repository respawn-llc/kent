import { create } from "@app/server-api-contract";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import * as Effect from "effect/Effect";
import * as Cause from "effect/Cause";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import {
  ControlService,
  ListSuccessSchema,
  ViewService,
  type BackgroundProcess,
} from "@app/server-api-contract/gen/kent/api/process/process_pb";

import { timestampMillis } from "./clientTime";
import { requireUnarySuccess, streamCompletionFailure } from "./protobufRpc";
import { ProcessObservationError, type DesktopProcess } from "./processes";
import type { ChatSessionTarget } from "./chatTypes";
import type { DescriptorRpcTransport } from "./transport";

export function observeProcesses(
  transport: DescriptorRpcTransport,
  target: ChatSessionTarget,
): Stream.Stream<readonly DesktopProcess[], ProcessObservationError> {
  return Stream.callback<readonly DesktopProcess[], ProcessObservationError>(
    (queue) =>
      Effect.acquireRelease(
        Effect.try({
          try: () => {
            const method = ViewService.method.observe;
            return transport.subscribeDescriptor({
              method,
              request: create(method.input, { projectId: target.projectID, sessionId: target.sessionID }),
              attachment: target,
              eventDescriptor: ListSuccessSchema,
              completionDescriptor: StreamCompletionSchema,
              onStart: (result) => {
                requireUnarySuccess(method, result);
              },
              handler: {
                onEvent: (list) => {
                  if (!Queue.offerUnsafe(queue, list.processes.map(processFromGenerated))) {
                    Queue.failCauseUnsafe(
                      queue,
                      Cause.fail(new ProcessObservationError("Process observation buffer overflow.")),
                    );
                  }
                },
                onComplete: (completion) => {
                  const cause = streamCompletionFailure(completion);
                  const error = new ProcessObservationError(cause?.message ?? "Process observation ended.", {
                    cause,
                  });
                  Queue.failCauseUnsafe(queue, Cause.fail(error));
                  return error;
                },
                onError: (error) => {
                  Queue.failCauseUnsafe(
                    queue,
                    Cause.fail(new ProcessObservationError(error.message, { cause: error })),
                  );
                },
              },
            });
          },
          catch: (cause) => new ProcessObservationError("Process observation failed.", { cause }),
        }),
        (subscription) =>
          Effect.sync(() => {
            subscription.close();
          }),
      ),
    { bufferSize: 1, strategy: "dropping" },
  );
}

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
    backgrounded: process.backgrounded,
    killRequested: process.killRequested,
  };
}
