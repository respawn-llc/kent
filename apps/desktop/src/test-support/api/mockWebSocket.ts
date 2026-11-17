import { z } from "zod";

export const mockRpcWebSockets: MockWebSocket[] = [];

export class MockWebSocket extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readonly sent: (string | Uint8Array)[] = [];
  binaryType: BinaryType = "blob";
  readyState = MockWebSocket.CONNECTING;

  constructor(readonly url: string) {
    super();
    mockRpcWebSockets.push(this);
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBuffer | ArrayBufferView): void {
    const text = z.string().safeParse(data);
    if (text.success) {
      this.sent.push(text.data);
    } else if (ArrayBuffer.isView(data)) {
      this.sent.push(new Uint8Array(data.buffer, data.byteOffset, data.byteLength).slice());
    } else if (data instanceof ArrayBuffer) {
      this.sent.push(new Uint8Array(data).slice());
    } else {
      throw new Error("Mock WebSocket does not support Blob sends.");
    }
    this.dispatchEvent(new Event("sent"));
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  }

  open(): void {
    this.readyState = MockWebSocket.OPEN;
    this.dispatchEvent(new Event("open"));
  }

  receive(data: string | ArrayBuffer): void {
    this.dispatchEvent(new MessageEvent("message", { data }));
  }
}

export async function waitForMockWebSocketSent(socket: MockWebSocket, count: number): Promise<void> {
  if (socket.sent.length >= count) return;
  await new Promise<void>((resolve) => {
    const sent = () => {
      if (socket.sent.length < count) return;
      socket.removeEventListener("sent", sent);
      resolve();
    };
    socket.addEventListener("sent", sent);
    sent();
  });
}

export function installMockWebSocket(): () => void {
  const originalDescriptor = Object.getOwnPropertyDescriptor(globalThis, "WebSocket");
  mockRpcWebSockets.length = 0;
  Object.defineProperty(globalThis, "WebSocket", {
    configurable: true,
    value: MockWebSocket,
    writable: true,
  });
  return () => {
    if (originalDescriptor !== undefined) {
      Object.defineProperty(globalThis, "WebSocket", originalDescriptor);
    } else {
      Reflect.deleteProperty(globalThis, "WebSocket");
    }
  };
}
