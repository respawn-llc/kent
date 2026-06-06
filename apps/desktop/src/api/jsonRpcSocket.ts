import { z } from "zod";

import protocolVersionDefinition from "../../../../shared/protocol/version.json";
import { ProtocolMismatchError, RpcError, TransportError } from "./errors";
import type { JsonValue } from "./json";
import type { RpcEventHandler } from "./transport";

export const protocolVersion = protocolVersionDefinition.version;
export const jsonRpcVersion = "2.0";
export const handshakeMethod = "protocol.handshake";
export const protocolVersionMismatchErrorCode = -32025;

export const responseSchema = z.object({
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

export async function openSocket(endpoint: string, timeoutMilliseconds: number): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(endpoint);
    const timeout = setTimeout(() => {
      fail(new TransportError(`Connection to ${endpoint} timed out.`));
    }, timeoutMilliseconds);
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
    const error = () => {
      fail(new TransportError(`Unable to connect to ${endpoint}.`));
    };
    const close = () => {
      fail(new TransportError(`Connection to ${endpoint} closed before opening.`));
    };
    socket.addEventListener("open", open, { once: true });
    socket.addEventListener("error", error, { once: true });
    socket.addEventListener("close", close, { once: true });
  });
}

export function parseFrame(data: string): unknown {
  try {
    return JSON.parse(data);
  } catch {
    return {};
  }
}

export async function handshakeSubscription(socket: WebSocket, timeoutMilliseconds: number): Promise<void> {
  await sendSocketRequest(
    socket,
    handshakeMethod,
    { protocol_version: protocolVersion },
    timeoutMilliseconds,
  );
}

export async function sendSocketRequest(
  socket: WebSocket,
  method: string,
  params: JsonValue,
  timeoutMilliseconds: number,
): Promise<unknown> {
  const id = `${method}-${Date.now().toString()}`;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      fail(new TransportError(`${method} request timed out.`));
    }, timeoutMilliseconds);
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
        reject(socketRequestError(method, response.data.error));
        return;
      }
      resolve(response.data.result);
    };
    const close = () => {
      fail(new TransportError(`${method} request closed before response.`));
    };
    const error = () => {
      fail(new TransportError(`${method} request failed before response.`));
    };
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

export function socketRequestError(
  method: string,
  error: Readonly<{ code: number; message: string }>,
): Error {
  if (method === handshakeMethod && error.code === protocolVersionMismatchErrorCode) {
    return new ProtocolMismatchError(error.message);
  }
  return new RpcError({ code: error.code, message: error.message, method });
}

export async function waitForSubscriptionEnd(socket: WebSocket, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted || socket.readyState === WebSocket.CLOSED || socket.readyState === WebSocket.CLOSING) {
      resolve();
      return;
    }
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

export async function delay(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const finish = () => {
      clearTimeout(timeout);
      signal.removeEventListener("abort", abort);
      resolve();
    };
    const abort = () => {
      finish();
    };
    const timeout = setTimeout(finish, milliseconds);
    signal.addEventListener("abort", abort, { once: true });
  });
}

export function handleSubscriptionMessage(event: MessageEvent<unknown>, handler: RpcEventHandler): void {
  if (typeof event.data !== "string") {
    return;
  }
  const parsed = parseFrame(event.data);
  const notification = notificationSchema.safeParse(parsed);
  if (!notification.success) {
    return;
  }
  if (notification.data.method.endsWith(".complete")) {
    const complete = z
      .object({ code: z.number().optional(), message: z.string().optional() })
      .safeParse(notification.data.params);
    handler.onComplete(
      complete.success ? (complete.data.code ?? 0) : 0,
      complete.success ? (complete.data.message ?? "") : "",
    );
    return;
  }
  handler.onEvent(notification.data.method, notification.data.params);
}
