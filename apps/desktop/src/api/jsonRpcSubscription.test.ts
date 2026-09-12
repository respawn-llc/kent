import { z } from "zod";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TransportError } from "./errors";
import { runJsonSubscription } from "./jsonRpcSubscription";
import { runSocketDescriptorSubscription } from "./jsonRpcSocket";
import { create, decodeEnvelope, encode, encodeEnvelope, operationName } from "@app/server-api-contract";
import { MessageSchema, StreamService } from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { requireUnarySuccess } from "./protobufRpc";

const sockets: SubscriptionSocket[] = [];

class SubscriptionSocket extends EventTarget {
  static readonly OPEN = 1;
  static readonly CLOSED = 3;

  readonly sent: (string | Uint8Array)[] = [];
  readyState = SubscriptionSocket.OPEN;

  constructor() {
    super();
    sockets.push(this);
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    const text = z.string().safeParse(data);
    if (text.success) this.sent.push(text.data);
    else if (data instanceof ArrayBuffer) this.sent.push(new Uint8Array(data));
    else throw new Error("Unsupported subscription frame.");
  }

  close(): void {
    if (this.readyState === SubscriptionSocket.CLOSED) return;
    this.readyState = SubscriptionSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  }

  acknowledge(): void {
    const bytes = this.sent[0];
    if (!(bytes instanceof Uint8Array)) throw new Error("Expected binary subscription call.");
    const frame = decodeEnvelope(bytes).frame;
    if (frame.case !== "call") throw new Error("Expected subscription call.");
    const method = StreamService.method.subscribe;
    const response = encodeEnvelope({
      frame: {
        case: "result",
        value: {
          operation: operationName(method),
          correlation: frame.value.correlation,
          payload: encode(method.output, create(method.output, { outcome: { case: "success", value: {} } })),
        },
      },
    });
    this.dispatchEvent(new MessageEvent("message", { data: response }));
  }
}

describe("subscription establishment", () => {
  beforeEach(() => {
    sockets.length = 0;
    vi.stubGlobal("WebSocket", SubscriptionSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("keeps the ordinary 30-second establishment timeout", async () => {
    vi.useFakeTimers();
    const socket = new WebSocket("ws://subscription.test");
    const controller = new AbortController();
    const pending = runJsonSubscription({
      socket,
      method: "workflow.subscribeProject",
      params: {},
      handler: {
        onEvent: () => undefined,
        onComplete: () => undefined,
        onError: () => undefined,
      },
      signal: controller.signal,
    });
    const rejected = expect(pending).rejects.toBeInstanceOf(TransportError);

    await vi.advanceTimersByTimeAsync(30_001);
    await rejected;
  });

  it("lets a no-deadline Chat subscription open after 30 seconds or close while waiting", async () => {
    vi.useFakeTimers();
    const socket = new WebSocket("ws://subscription.test");
    const controlled = requiredSocket();
    const controller = new AbortController();
    const opened = vi.fn();
    const pending = runSocketDescriptorSubscription({
      socket,
      method: StreamService.method.subscribe,
      request: create(StreamService.method.subscribe.input, {
        sessionId: "123e4567-e89b-42d3-a456-426614174000",
      }),
      eventDescriptor: MessageSchema,
      completionDescriptor: StreamCompletionSchema,
      onStart: (result) => {
        requireUnarySuccess(StreamService.method.subscribe, result);
      },
      handler: {
        onOpen: opened,
        onEvent: () => undefined,
        onComplete: () => undefined,
        onError: () => undefined,
      },
      signal: controller.signal,
      establishmentTimeoutMs: null,
    });

    await vi.advanceTimersByTimeAsync(60_000);
    expect(opened).not.toHaveBeenCalled();
    controlled.acknowledge();
    await vi.advanceTimersByTimeAsync(0);
    expect(opened).toHaveBeenCalledOnce();
    controller.abort();
    await expect(pending).resolves.toBeUndefined();
  });

  it("lets a no-deadline Chat subscription close before acknowledgement", async () => {
    const socket = new WebSocket("ws://subscription.test");
    const controller = new AbortController();
    const opened = vi.fn();
    const pending = runSocketDescriptorSubscription({
      socket,
      method: StreamService.method.subscribe,
      request: create(StreamService.method.subscribe.input, {
        sessionId: "123e4567-e89b-42d3-a456-426614174000",
      }),
      eventDescriptor: MessageSchema,
      completionDescriptor: StreamCompletionSchema,
      onStart: (result) => {
        requireUnarySuccess(StreamService.method.subscribe, result);
      },
      handler: {
        onOpen: opened,
        onEvent: () => undefined,
        onComplete: () => undefined,
        onError: () => undefined,
      },
      signal: controller.signal,
      establishmentTimeoutMs: null,
    });

    controller.abort();

    await expect(pending).resolves.toBeUndefined();
    expect(opened).not.toHaveBeenCalled();
  });
});

function requiredSocket(): SubscriptionSocket {
  const socket = sockets[0];
  if (socket === undefined) throw new Error("Subscription socket is missing.");
  return socket;
}
