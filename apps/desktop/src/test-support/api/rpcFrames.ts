import { toBinary } from "@bufbuild/protobuf";
import {
  decodeEnvelope,
  encode,
  encodeEnvelope,
  operationName,
  type DescMethod,
  type MessageShape,
} from "@app/server-api-contract";
import { z } from "zod";

export function binaryAck<Method extends DescMethod>(
  socket: Readonly<{
    sent: readonly (string | Uint8Array)[];
    receive(data: string | ArrayBuffer): void;
  }>,
  sentIndex: number,
  method: Method,
  response: Readonly<{
    result: MessageShape<Method["output"]>;
    operation?: string;
  }>,
): void {
  writeBinaryAck({
    socket,
    sentIndex,
    operation: operationName(method),
    responseOperation: response.operation,
    payload: encode(method.output, response.result),
  });
}

export function binaryAckUnvalidated<Method extends DescMethod>(
  socket: Readonly<{
    sent: readonly (string | Uint8Array)[];
    receive(data: string | ArrayBuffer): void;
  }>,
  sentIndex: number,
  method: Method,
  response: Readonly<{
    result: MessageShape<Method["output"]>;
    operation?: string;
  }>,
): void {
  writeBinaryAck({
    socket,
    sentIndex,
    operation: operationName(method),
    responseOperation: response.operation,
    payload: toBinary(method.output, response.result),
  });
}

function writeBinaryAck({
  socket,
  sentIndex,
  operation,
  responseOperation,
  payload,
}: Readonly<{
  socket: Readonly<{
    sent: readonly (string | Uint8Array)[];
    receive(data: string | ArrayBuffer): void;
  }>;
  sentIndex: number;
  operation: string;
  responseOperation: string | undefined;
  payload: Uint8Array;
}>): void {
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
  if (call.value.operation !== operation || call.value.correlation === undefined) {
    throw new Error("Mock WebSocket binary call has the wrong operation or correlation.");
  }
  const encodedResponse = encodeEnvelope({
    frame: {
      case: "result",
      value: {
        operation: responseOperation ?? operation,
        correlation: call.value.correlation,
        payload,
      },
    },
  });
  const responseBuffer = new ArrayBuffer(encodedResponse.byteLength);
  new Uint8Array(responseBuffer).set(encodedResponse);
  socket.receive(responseBuffer);
}
