import { z } from "zod";

import { ConnectionStore } from "./connectionStore";
import { RpcError, TransportError } from "./errors";
import type { JsonValue } from "./json";
import type { RpcEventHandler, RpcSubscription, RpcTransport } from "./transport";

const protocolVersion = "2";
const jsonRpcVersion = "2.0";
const handshakeMethod = "protocol.handshake";
const socketOpenTimeoutMs = 10_000;
const rpcRequestTimeoutMs = 30_000;
const subscriptionReconnectBaseMs = 500;
const subscriptionReconnectMaxMs = 5_000;
const responseSchema = z.object({
  jsonrpc: z.literal(jsonRpcVersion),
  id: z.string().optional(),
  result: z.unknown().optional(),
  error: z
    .object({
      code: z.number(),
      message: z.string(),
    })
    .optional(),
});
const notificationSchema = z.object({
  jsonrpc: z.literal(jsonRpcVersion),
  method: z.string(),
  params: z.unknown().optional(),
});

type PendingRequest = Readonly<{
  method: string;
  timeout: ReturnType<typeof setTimeout>;
  resolve(value: unknown): void;
  reject(error: Error): void;
}>;

export function createJsonRpcTransport(endpoint: string): RpcTransport {
  return new JsonRpcWebSocketTransport(endpoint);
}

class JsonRpcWebSocketTransport implements RpcTransport {
  readonly connection = new ConnectionStore();
  #endpoint: string;
  #socket: WebSocket | null = null;
  #opening: Promise<WebSocket> | null = null;
  #nextID = 1;
  #pending = new Map<string, PendingRequest>();

  constructor(endpoint: string) {
    this.#endpoint = endpoint;
  }

  async call(method: string, params: JsonValue): Promise<unknown> {
    const socket = await this.#open();
    return this.#send(socket, method, params);
  }

  subscribe(method: string, params: JsonValue, handler: RpcEventHandler): RpcSubscription {
    const controller = new AbortController();
    void this.#openSubscription(method, params, handler, controller.signal);
    return {
      close() {
        controller.abort();
      },
    };
  }

  async #open(): Promise<WebSocket> {
    if (this.#socket?.readyState === WebSocket.OPEN) {
      return this.#socket;
    }
    if (this.#opening !== null) {
      return this.#opening;
    }
    this.connection.set("connecting");
    this.#opening = this.#connectControl();
    try {
      return await this.#opening;
    } finally {
      this.#opening = null;
    }
  }

  async #connectControl(): Promise<WebSocket> {
    const socket = await openSocket(this.#endpoint);
    socket.addEventListener("message", (event) => { this.#handleControlMessage(event); });
    socket.addEventListener("close", () => { this.#handleControlClose(); });
    socket.addEventListener("error", () => { this.#handleControlError(); });
    try {
      await this.#send(socket, handshakeMethod, { protocol_version: protocolVersion });
    } catch (error) {
      socket.close();
      throw error;
    }
    this.#socket = socket;
    this.connection.set("connected");
    return socket;
  }

  async #send(socket: WebSocket, method: string, params: JsonValue): Promise<unknown> {
    if (socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(new TransportError("WebSocket is not open."));
    }
    const id = `gui-${this.#nextID.toString()}`;
    this.#nextID += 1;
    const frame = JSON.stringify({ jsonrpc: jsonRpcVersion, id, method, params });
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        if (!this.#pending.delete(id)) {
          return;
        }
        reject(new TransportError(`${method} request timed out.`));
      }, rpcRequestTimeoutMs);
      this.#pending.set(id, { method, timeout, resolve, reject });
      try {
        socket.send(frame);
      } catch (error) {
        clearTimeout(timeout);
        this.#pending.delete(id);
        reject(error instanceof Error ? error : new TransportError(`${method} request failed to send.`));
      }
    });
  }

  #handleControlMessage(event: MessageEvent<unknown>): void {
    if (typeof event.data !== "string") {
      this.#rejectAll(new TransportError("Unsupported WebSocket frame type."));
      return;
    }
    const parsed = parseFrame(event.data);
    const response = responseSchema.safeParse(parsed);
    if (!response.success || response.data.id === undefined) {
      return;
    }
    this.#resolveResponse(response.data.id, response.data.result, response.data.error);
  }

  #resolveResponse(id: string, result: unknown, error: { code: number; message: string } | undefined): void {
    const pending = this.#pending.get(id);
    if (pending === undefined) {
      return;
    }
    this.#pending.delete(id);
    clearTimeout(pending.timeout);
    if (error !== undefined) {
      pending.reject(new RpcError({ code: error.code, message: error.message, method: pending.method }));
      return;
    }
    pending.resolve(result);
  }

  #handleControlClose(): void {
    this.#socket = null;
    this.connection.set("disconnected", "Builder service connection closed.");
    this.#rejectAll(new TransportError("Builder service connection closed."));
  }

  #handleControlError(): void {
    this.#socket = null;
    this.connection.set("disconnected", "Builder service connection failed.");
    this.#rejectAll(new TransportError("Builder service connection failed."));
  }

  #rejectAll(error: Error): void {
    const pending = [...this.#pending.values()];
    this.#pending.clear();
    for (const request of pending) {
      clearTimeout(request.timeout);
      request.reject(error);
    }
  }

  async #openSubscription(
    method: string,
    params: JsonValue,
    handler: RpcEventHandler,
    signal: AbortSignal,
  ): Promise<void> {
    let attempt = 0;
    while (!signal.aborted) {
      try {
        await this.#openSubscriptionSession(method, params, handler, signal);
        return;
      } catch (error) {
        handler.onError(error instanceof Error ? error : new TransportError("Subscription failed."));
        await delay(Math.min(subscriptionReconnectBaseMs * 2 ** attempt, subscriptionReconnectMaxMs), signal);
        attempt += 1;
      }
    }
  }

  async #openSubscriptionSession(
    method: string,
    params: JsonValue,
    handler: RpcEventHandler,
    signal: AbortSignal,
  ): Promise<void> {
    const socket = await openSocket(this.#endpoint);
    signal.addEventListener("abort", () => { socket.close(); }, { once: true });
    await handshakeSubscription(socket);
    const subscriptionListener = (event: MessageEvent<unknown>) => { handleSubscriptionMessage(event, handler); };
    socket.addEventListener("message", subscriptionListener);
    try {
      await sendSocketRequest(socket, method, params);
      await waitForSubscriptionEnd(socket, signal);
    } finally {
      socket.removeEventListener("message", subscriptionListener);
      socket.close();
    }
  }
}

async function openSocket(endpoint: string): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(endpoint);
    const timeout = setTimeout(() => { fail(new TransportError(`Connection to ${endpoint} timed out.`)); }, socketOpenTimeoutMs);
    const cleanup = () => {
      clearTimeout(timeout);
      socket.removeEventListener("open", open);
      socket.removeEventListener("error", error);
      socket.removeEventListener("close", close);
    };
    const fail = (cause: Error) => {
      cleanup();
      socket.close();
      reject(cause);
    };
    const open = () => {
      cleanup();
      resolve(socket);
    };
    const error = () => { fail(new TransportError(`Unable to connect to ${endpoint}.`)); };
    const close = () => { fail(new TransportError(`Connection to ${endpoint} closed before opening.`)); };
    socket.addEventListener("open", open, { once: true });
    socket.addEventListener("error", error, { once: true });
    socket.addEventListener("close", close, { once: true });
  });
}

function parseFrame(data: string): unknown {
  try {
    return JSON.parse(data);
  } catch {
    return {};
  }
}

async function handshakeSubscription(socket: WebSocket): Promise<void> {
  await sendSocketRequest(socket, handshakeMethod, { protocol_version: protocolVersion });
}

async function sendSocketRequest(socket: WebSocket, method: string, params: JsonValue): Promise<unknown> {
  const id = `${method}-${Date.now().toString()}`;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => { fail(new TransportError(`${method} request timed out.`)); }, rpcRequestTimeoutMs);
    const cleanup = () => {
      clearTimeout(timeout);
      socket.removeEventListener("message", listener);
      socket.removeEventListener("close", close);
      socket.removeEventListener("error", error);
    };
    const fail = (cause: Error) => {
      cleanup();
      reject(cause);
    };
    const listener = (event: MessageEvent<unknown>) => {
      if (typeof event.data !== "string") {
        return;
      }
      const response = responseSchema.safeParse(parseFrame(event.data));
      if (!response.success || response.data.id !== id) {
        return;
      }
      cleanup();
      if (response.data.error !== undefined) {
        reject(new RpcError({ code: response.data.error.code, message: response.data.error.message, method }));
        return;
      }
      resolve(response.data.result);
    };
    const close = () => { fail(new TransportError(`${method} request closed before response.`)); };
    const error = () => { fail(new TransportError(`${method} request failed before response.`)); };
    socket.addEventListener("message", listener);
    socket.addEventListener("close", close, { once: true });
    socket.addEventListener("error", error, { once: true });
    try {
      socket.send(JSON.stringify({ jsonrpc: jsonRpcVersion, id, method, params }));
    } catch (cause) {
      fail(cause instanceof Error ? cause : new TransportError(`${method} request failed to send.`));
    }
  });
}

async function waitForSubscriptionEnd(socket: WebSocket, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      socket.removeEventListener("close", close);
      socket.removeEventListener("error", error);
      signal.removeEventListener("abort", abort);
    };
    const close = () => {
      cleanup();
      if (signal.aborted) {
        resolve();
        return;
      }
      reject(new TransportError("Subscription socket closed."));
    };
    const error = () => {
      cleanup();
      reject(new TransportError("Subscription socket failed."));
    };
    const abort = () => {
      cleanup();
      resolve();
    };
    socket.addEventListener("close", close, { once: true });
    socket.addEventListener("error", error, { once: true });
    signal.addEventListener("abort", abort, { once: true });
  });
}

async function delay(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const finish = () => {
      clearTimeout(timeout);
      signal.removeEventListener("abort", abort);
      resolve();
    };
    const abort = () => { finish(); };
    const timeout = setTimeout(finish, milliseconds);
    signal.addEventListener("abort", abort, { once: true });
  });
}

function handleSubscriptionMessage(event: MessageEvent<unknown>, handler: RpcEventHandler): void {
  if (typeof event.data !== "string") {
    return;
  }
  const parsed = parseFrame(event.data);
  const notification = notificationSchema.safeParse(parsed);
  if (!notification.success) {
    return;
  }
  if (notification.data.method.endsWith(".complete")) {
    const complete = z.object({ code: z.number().optional(), message: z.string().optional() }).safeParse(notification.data.params);
    handler.onComplete(complete.success ? (complete.data.code ?? 0) : 0, complete.success ? (complete.data.message ?? "") : "");
    return;
  }
  handler.onEvent(notification.data.method, notification.data.params);
}
