import { type create, decodeEnvelope, encode, encodeEnvelope, operationName } from "@app/server-api-contract";
import type { ServerService } from "@app/server-api-contract/gen/kent/api/server/server_pb";
import type { ConnectionService } from "@app/server-api-contract/gen/kent/api/connection/connection_pb";
import type { QuestionService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import type { StreamService } from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import type { TaskLifecycleService } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import type { ProjectSubscriptionService } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import type { TaskReadService } from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { z } from "zod";

export function binaryAck<
  Method extends
    | typeof ServerService.method.getReadiness
    | typeof ConnectionService.method.handshake
    | typeof ConnectionService.method.attachSession
    | typeof QuestionService.method.listPending
    | typeof StreamService.method.subscribe
    | typeof TaskLifecycleService.method.delete
    | typeof ProjectSubscriptionService.method.subscribe
    | typeof TaskReadService.method.search,
>(
  socket: Readonly<{
    sent: readonly (string | Uint8Array)[];
    receive(data: string | ArrayBuffer): void;
  }>,
  sentIndex: number,
  method: Method,
  response: Readonly<{
    result: ReturnType<typeof create<Method["output"]>>;
    operation?: string;
  }>,
): void {
  const raw = socket.sent[sentIndex];
  if (raw === undefined) {
    throw new Error(`sent frame ${sentIndex.toString()} missing`);
  }
  const binary = z.instanceof(Uint8Array).safeParse(raw);
  if (!binary.success) {
    throw new Error("Mock WebSocket frame is text.");
  }
  const call = decodeEnvelope(binary.data).frame;
  if (call.case !== "call") {
    throw new Error("Mock WebSocket binary frame is not a call.");
  }
  const operation = operationName(method);
  if (call.value.operation !== operation || call.value.correlation === undefined) {
    throw new Error("Mock WebSocket binary call has the wrong operation or correlation.");
  }
  const payload = encode(method.output, response.result);
  const encodedResponse = encodeEnvelope({
    frame: {
      case: "result",
      value: {
        operation: response.operation ?? operation,
        correlation: call.value.correlation,
        payload,
      },
    },
  });
  const responseBuffer = new ArrayBuffer(encodedResponse.byteLength);
  new Uint8Array(responseBuffer).set(encodedResponse);
  socket.receive(responseBuffer);
}
