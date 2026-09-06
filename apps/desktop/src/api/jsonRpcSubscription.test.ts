import { z } from "zod";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { runJsonSubscription } from "./jsonRpcSubscription";

const sockets: SubscriptionSocket[] = [];

class SubscriptionSocket extends EventTarget {
  static readonly OPEN = 1;
  static readonly CLOSED = 3;

  readonly sent: string[] = [];
  readyState = SubscriptionSocket.OPEN;

  constructor() {
    super();
    sockets.push(this);
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    const text = z.string().safeParse(data);
    if (!text.success) throw new Error("Expected JSON subscription request.");
    this.sent.push(text.data);
  }

  close(): void {
    if (this.readyState === SubscriptionSocket.CLOSED) return;
    this.readyState = SubscriptionSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  }

  acknowledge(): void {
    const request = z.looseObject({ id: z.string() }).parse(JSON.parse(this.sent[0] ?? "{}"));
    this.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({ jsonrpc: "2.0", id: request.id, result: {} }),
      }),
    );
  }
}

describe("JSON subscription establishment", () => {
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
    const rejected = expect(pending).rejects.toThrow("timed out");

    await vi.advanceTimersByTimeAsync(30_001);
    await rejected;
  });

  it("lets a no-deadline Chat subscription open after 30 seconds or close while waiting", async () => {
    vi.useFakeTimers();
    const socket = new WebSocket("ws://subscription.test");
    const controlled = requiredSocket();
    const controller = new AbortController();
    const opened = vi.fn();
    const pending = runJsonSubscription({
      socket,
      method: "session.subscribeTranscript",
      params: {},
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
});

function requiredSocket(): SubscriptionSocket {
  const socket = sockets[0];
  if (socket === undefined) throw new Error("Subscription socket is missing.");
  return socket;
}
