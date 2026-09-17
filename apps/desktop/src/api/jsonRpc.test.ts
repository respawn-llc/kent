import { createJsonRpcTransport } from "./jsonRpc";
import { ProtocolMismatchError, RpcError, ServerRootMismatchError, decodeWorkflowLabelError } from "./errors";
import { protocolVersion, subscriptionCompleteMethod } from "./jsonRpcSocket";
import { create, decodeEnvelope, encode, encodeEnvelope, operationName } from "@app/server-api-contract";
import {
  AttachSessionResultSchema,
  ConnectionService,
  HandshakeResultSchema,
} from "@app/server-api-contract/gen/kent/api/connection/connection_pb";
import {
  GetReadinessResultSchema,
  ServerNotReadyDetailsSchema,
  ServerNotReadyReason,
  ServerService,
} from "@app/server-api-contract/gen/kent/api/server/server_pb";
import { z } from "zod";
import { createChatApi } from "./chat";
import type { ChatTranscriptCompletion } from "./chatTypes";
import { ContractError, TransportError } from "./errors";
import { StreamService, MessageSchema } from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import { ConversationFreshness } from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { QuestionService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import type { RpcEventHandler } from "./transport";

type SentFrame = Readonly<{
  id: string;
  method: string;
}>;

const sentFrameSchema = z.object({
  id: z.string(),
  method: z.string(),
});

class MockWebSocket extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readonly sent: (string | Uint8Array)[] = [];
  binaryType: BinaryType = "blob";
  readyState = MockWebSocket.CONNECTING;

  constructor(readonly url: string) {
    super();
    sockets.push(this);
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    const text = z.string().safeParse(data);
    if (text.success) {
      this.sent.push(text.data);
      return;
    }
    if (ArrayBuffer.isView(data)) {
      this.sent.push(new Uint8Array(data.buffer, data.byteOffset, data.byteLength).slice());
      return;
    }
    if (data instanceof ArrayBuffer) {
      this.sent.push(new Uint8Array(data).slice());
      return;
    }
    throw new Error("Mock WebSocket does not support Blob sends.");
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  }

  open(): void {
    this.readyState = MockWebSocket.OPEN;
    this.dispatchEvent(new Event("open"));
  }

  async setup(): Promise<void> {
    this.open();
    await waitForSent(this, 1);
    ack(this, 0);
    await waitForSent(this, 2);
  }

  receive(data: string | ArrayBuffer): void {
    this.dispatchEvent(new MessageEvent("message", { data }));
  }
}

const sockets: MockWebSocket[] = [];

describe("JsonRpcWebSocketTransport", () => {
  beforeEach(() => {
    sockets.length = 0;
    vi.stubGlobal("WebSocket", MockWebSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("rejects pending mutations on disconnect and does not replay them on reconnect", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const mutation = transport.call("workflow.task.start", { task_id: "task-1" });
    const firstSocket = sockets[0] ?? failTest("first socket missing");

    await firstSocket.setup();
    expect(frame(firstSocket, 1)).toMatchObject({ method: "workflow.task.start" });

    firstSocket.close();
    await expect(mutation).rejects.toThrow("closed");
    expect(firstSocket.sent).toHaveLength(2);

    const retry = transport.call("workflow.task.start", { task_id: "task-1" });
    const secondSocket = sockets[1] ?? failTest("second socket missing");
    await secondSocket.setup();
    expect(secondSocket.sent).toHaveLength(2);
    ack(secondSocket, 1);

    await expect(retry).resolves.toEqual({});
    expect(firstSocket.sent).toHaveLength(2);
  });

  it("multiplexes generated binary calls with structured JSON-RPC errors on one control socket", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const readiness = callReadiness(transport);
    const socket = sockets[0] ?? failTest("control socket missing");

    await socket.setup();
    binaryAck(socket, 1, ServerService.method.getReadiness, { result: readinessResult() });
    await expect(readiness).resolves.toMatchObject({
      outcome: { case: "success", value: { readiness: { serverId: "server-1" } } },
    });

    const malformedReadiness = callReadiness(transport);
    const request = transport.call("workflow.project.label.create", {
      project_id: "project-1",
      name: "Priority",
    });
    await waitForSent(socket, 4);
    socket.receive(new Uint8Array([0xff]).buffer);
    malformedBinaryAck(socket, 2);
    await expect(malformedReadiness).rejects.toBeInstanceOf(Error);
    errorAck(socket, 3, {
      code: -32031,
      message: "label name already exists",
      data: {
        type: "workflow_label_error",
        reason: "name_conflict",
        project_id: "project-1",
      },
    });

    const error = await request.catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(RpcError);
    expect(error).toMatchObject({
      code: -32031,
      method: "workflow.project.label.create",
      data: {
        type: "workflow_label_error",
        reason: "name_conflict",
        project_id: "project-1",
      },
    });
    expect(decodeWorkflowLabelError(error)).toMatchObject({
      reason: "name_conflict",
      projectID: "project-1",
    });
  });

  it("runs dedicated calls on a one-use socket without disturbing the control socket", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const readiness = callReadiness(transport);
    const controlSocket = sockets[0] ?? failTest("control socket missing");
    await controlSocket.setup();
    ack(controlSocket, 1);
    await expect(readiness).resolves.toMatchObject({ outcome: { case: "success" } });

    const search = transport.callDedicated("workflow.task.search", { query: "needle" });
    const dedicatedSocket = sockets[1] ?? failTest("dedicated socket missing");
    await dedicatedSocket.setup();
    expect(frame(dedicatedSocket, 1)).toMatchObject({ method: "workflow.task.search" });
    ack(dedicatedSocket, 1);

    await expect(search).resolves.toEqual({});
    expect(dedicatedSocket.readyState).toBe(MockWebSocket.CLOSED);
    expect(controlSocket.readyState).toBe(MockWebSocket.OPEN);
  });

  it("attaches a dedicated Session before a Session-scoped call", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const method = QuestionService.method.listPending;
    const answer = transport.callDescriptorAttachedSession(
      { sessionID: "session-1", projectID: "project-1" },
      method,
      create(method.input, { sessionId: "session-1" }),
    );
    const socket = sockets[0] ?? failTest("attached Session socket missing");

    await socket.setup();
    expect(descriptorOperation(socket, 1)).toBe(operationName(ConnectionService.method.attachSession));
    ack(socket, 1);
    await waitForSent(socket, 3);
    expect(descriptorOperation(socket, 2)).toBe(operationName(method));
    binaryAck(socket, 2, method, {
      result: create(method.output, { outcome: { case: "success", value: { questions: [] } } }),
    });

    await expect(answer).resolves.toMatchObject({ outcome: { case: "success", value: { questions: [] } } });
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
  });

  it("does not send a Session-scoped call when Session attachment fails", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const method = QuestionService.method.listPending;
    const answer = transport.callDescriptorAttachedSession(
      { sessionID: "session-1" },
      method,
      create(method.input, { sessionId: "session-1" }),
    );
    const socket = sockets[0] ?? failTest("attached Session socket missing");

    await socket.setup();
    binaryAck(socket, 1, ConnectionService.method.attachSession, {
      result: create(AttachSessionResultSchema, {
        outcome: {
          case: "error",
          value: {
            code: "server_not_ready",
            detail: {
              case: "serverNotReady",
              value: create(ServerNotReadyDetailsSchema, {
                reason: ServerNotReadyReason.ONBOARDING_REQUIRED,
              }),
            },
          },
        },
      }),
    });

    const error = await answer.catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(RpcError);
    expect(error).toMatchObject({
      code: -32032,
      method: operationName(ConnectionService.method.attachSession),
      data: {
        code: "server_not_ready",
        detail: {
          case: "serverNotReady",
          value: {
            reason: ServerNotReadyReason.ONBOARDING_REQUIRED,
          },
        },
      },
    });
    expect(socket.sent).toHaveLength(2);
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
  });

  it("cancels a dedicated call by closing only its socket", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const controller = new AbortController();
    const search = transport.callDedicated(
      "workflow.task.search",
      { query: "needle" },
      { signal: controller.signal },
    );
    const socket = sockets[0] ?? failTest("dedicated socket missing");
    await socket.setup();

    controller.abort();

    await expect(search).rejects.toThrow("canceled");
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
  });

  it("falls back to a generic RPC error when error data is not valid JSON", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const request = transport.call("workflow.project.label.create", {
      project_id: "project-1",
      name: "Priority",
    });
    const socket = sockets[0] ?? failTest("control socket missing");

    await socket.setup();
    const sent = frame(socket, 1);
    socket.receive(
      `{"jsonrpc":"2.0","id":${JSON.stringify(sent.id)},"error":{"code":-32031,"message":"label request failed","data":{"limit":1e400}}}`,
    );

    const error = await request.catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(RpcError);
    expect(error).toMatchObject({
      code: -32031,
      method: "workflow.project.label.create",
      data: undefined,
    });
  });

  it("rejects control calls when the server serves a different persistence root", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc", "expected-root");
    const readiness = callReadiness(transport);
    const socket = sockets[0] ?? failTest("control socket missing");

    socket.open();
    await waitForSent(socket, 1);
    ackHandshakeRoot(socket, 0, "other-root");

    await expect(readiness).rejects.toBeInstanceOf(ServerRootMismatchError);
    expect(socket.sent).toHaveLength(1);
    expect(descriptorOperation(socket, 0)).toBe(operationName(ConnectionService.method.handshake));
  });

  it("accepts control calls when the server serves the expected persistence root", async () => {
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc", "expected-root");
    const readiness = callReadiness(transport);
    const socket = sockets[0] ?? failTest("control socket missing");

    socket.open();
    await waitForSent(socket, 1);
    ackHandshakeRoot(socket, 0, "expected-root");
    await waitForSent(socket, 2);
    expect(descriptorOperation(socket, 1)).toBe(operationName(ServerService.method.getReadiness));
    ack(socket, 1);

    await expect(readiness).resolves.toMatchObject({ outcome: { case: "success" } });
  });

  it("keeps no-timeout control calls pending past the generic request deadline", async () => {
    vi.useFakeTimers();
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const mutation = transport.call("workflow.task.start", { task_id: "task-1" }, { timeoutMs: null });
    let settled = false;
    mutation.then(
      () => {
        settled = true;
      },
      () => {
        settled = true;
      },
    );
    const socket = sockets[0] ?? failTest("control socket missing");

    await socket.setup();
    expect(frame(socket, 1)).toMatchObject({ method: "workflow.task.start" });

    await vi.advanceTimersByTimeAsync(31_000);
    expect(settled).toBe(false);
    ack(socket, 1);

    await expect(mutation).resolves.toEqual({});
  });

  it("installs subscription event listener before subscribe ack can race with first event", async () => {
    const events: string[] = [];
    const opens: string[] = [];
    const { socket } = subscribeProject({
      onOpen() {
        opens.push("open");
      },
      onEvent(method) {
        events.push(method);
      },
    });
    socket.open();
    await waitForSent(socket, 1);
    ack(socket, 0);
    await waitForSent(socket, 2);
    expect(frame(socket, 1)).toMatchObject({ method: "workflow.subscribeProject" });

    socket.receive(
      JSON.stringify({
        jsonrpc: "2.0",
        method: "workflow.project",
        params: { event: { project_id: "project-1" } },
      }),
    );
    ack(socket, 1);
    await flushPromises();

    expect(opens).toEqual(["open"]);
    expect(events).toEqual(["workflow.project"]);
  });

  it("rejects subscriptions on handshake protocol mismatch before sending the subscribe method", async () => {
    const errors: Error[] = [];
    const { subscription, socket } = subscribeProject({
      onError(error) {
        errors.push(error);
      },
    });

    socket.open();
    await waitForSent(socket, 1);
    handshakeProtocolMismatchAck(socket, 0);

    await vi.waitFor(() => {
      expect(errors[0]).toBeInstanceOf(ProtocolMismatchError);
    });
    expect(errors[0]).toMatchObject({
      requiredProtocolVersion: "126",
      clientProtocolVersion: protocolVersion,
    });
    expect(socket.sent).toHaveLength(1);
    expect(descriptorOperation(socket, 0)).toBe(operationName(ConnectionService.method.handshake));
    // A rejected handshake must close the socket; otherwise the reconnect loop
    // leaks a socket connected to the wrong server on every backoff.
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
    subscription.close();
  });

  it("ends a lost subscription and permits a fresh explicit subscription", async () => {
    vi.useFakeTimers();
    const errors: Error[] = [];
    const { subscription, socket: firstSocket } = subscribeProject({
      onError(error) {
        errors.push(error);
      },
    });
    await firstSocket.setup();
    ack(firstSocket, 1);
    await flushPromises();

    firstSocket.close();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(sockets).toHaveLength(1);
    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(TransportError);
    const next = subscribeProject({});
    const secondSocket = sockets[1] ?? failTest("resubscription socket missing");
    await secondSocket.setup();

    expect(frame(secondSocket, 1)).toMatchObject({ method: "workflow.subscribeProject" });
    subscription.close();
    next.subscription.close();
  });

  it("stops subscription retry after a definitive RPC rejection", async () => {
    const errors: Error[] = [];
    const { subscription, socket } = subscribeProject({
      onError(error) {
        errors.push(error);
      },
    });
    await socket.setup();
    vi.useFakeTimers();

    errorAck(socket, 1, { code: -32000, message: "Subscription rejected" });
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1_000);

    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(RpcError);
    expect(sockets).toHaveLength(1);
    expect(socket.sent).toHaveLength(2);
    subscription.close();
  });

  it("ends a subscription after unsuccessful completion without reopening", async () => {
    vi.useFakeTimers();
    const completions: number[] = [];
    const errors: Error[] = [];
    const { subscription, socket: firstSocket } = subscribeProject({
      onComplete(code) {
        completions.push(code);
      },
      onError(error) {
        errors.push(error);
      },
    });
    await firstSocket.setup();
    ack(firstSocket, 1);
    await flushPromises();

    firstSocket.receive(
      JSON.stringify({
        jsonrpc: "2.0",
        method: "workflow.project.complete",
        params: { code: 409, message: "stream gap" },
      }),
    );

    await vi.advanceTimersByTimeAsync(10_000);
    expect(sockets).toHaveLength(1);
    expect(completions).toEqual([409]);
    expect(errors).toHaveLength(1);
    subscription.close();
  });

  it("does not reconnect after normal server complete notification", async () => {
    const completions: string[] = [];
    const errors: string[] = [];
    const { subscription, socket } = subscribeProject({
      onComplete(code, message) {
        completions.push(`${code.toString()}:${message}`);
      },
      onError(error) {
        errors.push(error.message);
      },
    });
    await socket.setup();
    ack(socket, 1);
    await flushPromises();

    socket.receive(
      JSON.stringify({
        jsonrpc: "2.0",
        method: "workflow.project.complete",
        params: { code: 0, message: "" },
      }),
    );
    await flushPromises();

    expect(completions).toEqual(["0:"]);
    expect(errors).toEqual([]);
    expect(sockets).toHaveLength(1);
    subscription.close();
  });

  it("keeps subscriptions active for non-terminal events ending with complete", async () => {
    const events: string[] = [];
    const completions: string[] = [];
    const { subscription, socket } = subscribeProject({
      onEvent(method) {
        events.push(method);
      },
      onComplete(code, message) {
        completions.push(`${code.toString()}:${message}`);
      },
    });
    await socket.setup();
    ack(socket, 1);
    await flushPromises();

    socket.receive(
      JSON.stringify({
        jsonrpc: "2.0",
        method: "workflow.project.task.complete",
        params: { event: { project_id: "project-1" } },
      }),
    );
    await flushPromises();

    expect(events).toEqual(["workflow.project.task.complete"]);
    expect(completions).toEqual([]);
    expect(sockets).toHaveLength(1);
    subscription.close();
  });

  it("maps attention notification subscriptions to their complete method", () => {
    expect(subscriptionCompleteMethod("attention.notification.subscribe")).toBe(
      "attention.notification.complete",
    );
  });

  it("discards invalid transcript events on the same socket and makes invalid completion terminal", async () => {
    vi.useFakeTimers();
    const sessionID = "123e4567-e89b-42d3-a456-426614174000";
    const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
    const onEvent = vi.fn();
    const onError = vi.fn();
    const onOpen = vi.fn();
    const subscription = createChatApi(transport).subscribeTranscript(
      {
        projectID: "project-1",
        sessionID,
      },
      { onEvent, onError, onOpen, onComplete: vi.fn() },
    );
    const socket = sockets[0] ?? failTest("Transcript socket missing.");
    await prepareTranscriptSocket(socket, sessionID);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(onOpen).not.toHaveBeenCalled();
    binaryAck(socket, 2, StreamService.method.subscribe, {
      result: create(StreamService.method.subscribe.output, {
        outcome: { case: "success", value: {} },
      }),
    });
    expect(onOpen).toHaveBeenCalledOnce();
    // Sequence without the required Event is an invalid expected event, not a bad envelope.
    binaryNotification(socket, StreamService.method.event, Uint8Array.of(8, 2));
    binaryNotification(
      socket,
      StreamService.method.event,
      encode(
        MessageSchema,
        create(MessageSchema, {
          sequence: 3n,
          event: {
            payload: {
              case: "sessionIdentity",
              value: {
                sessionId: "223e4567-e89b-42d3-a456-426614174000",
                conversationFreshness: ConversationFreshness.FRESH,
              },
            },
          },
        }),
      ),
    );
    binaryNotification(
      socket,
      StreamService.method.event,
      encode(
        MessageSchema,
        create(MessageSchema, {
          sequence: 4n,
          event: {
            payload: {
              case: "sessionIdentity",
              value: { sessionId: sessionID, conversationFreshness: ConversationFreshness.FRESH },
            },
          },
        }),
      ),
    );
    expect(onEvent).toHaveBeenCalledOnce();
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ sequence: 4, kind: "session_identity" }));
    expect(onError).toHaveBeenCalledTimes(2);
    for (const [error] of onError.mock.calls) expect(error).toBeInstanceOf(ContractError);
    expect(socket.readyState).toBe(MockWebSocket.OPEN);
    // Present zero code is invalid; successful completion is the empty message.
    binaryNotification(socket, StreamService.method.complete, Uint8Array.of(8, 0));
    expect(onError).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
    expect(sockets).toHaveLength(1);
    subscription.close();
  });

  it("ends each transcript attempt on loss or completion and permits explicit observation", async () => {
    vi.useFakeTimers();
    const sessionID = "123e4567-e89b-42d3-a456-426614174000";
    const onComplete = vi.fn<(completion: ChatTranscriptCompletion) => void>();
    const onTransportLoss = vi.fn();
    const onError = vi.fn();
    const api = createChatApi(createJsonRpcTransport("ws://127.0.0.1:53082/rpc"));
    const observe = () =>
      api.subscribeTranscript(
        {
          projectID: "project-1",
          sessionID,
        },
        { onEvent: vi.fn(), onComplete, onTransportLoss, onError },
      );
    const subscription = observe();
    let socket = sockets[0] ?? failTest("Transcript socket missing.");
    await prepareTranscriptSocket(socket, sessionID);
    binaryAck(socket, 2, StreamService.method.subscribe, {
      result: create(StreamService.method.subscribe.output, { outcome: { case: "success", value: {} } }),
    });
    socket.close();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(onTransportLoss).toHaveBeenCalledOnce();
    expect(sockets).toHaveLength(1);
    const second = observe();
    socket = sockets[1] ?? failTest("Reconnected transcript socket missing.");
    await prepareTranscriptSocket(socket, sessionID);
    binaryAck(socket, 2, StreamService.method.subscribe, {
      result: create(StreamService.method.subscribe.output, { outcome: { case: "success", value: {} } }),
    });
    binaryNotification(
      socket,
      StreamService.method.complete,
      encode(
        StreamService.method.complete.input,
        create(StreamService.method.complete.input, {
          code: -17,
          message: "stream gap",
        }),
      ),
    );
    await vi.advanceTimersByTimeAsync(2_000);
    expect(onTransportLoss).toHaveBeenCalledTimes(2);
    expect(sockets).toHaveLength(2);
    const third = observe();
    socket = sockets[2] ?? failTest("Resubscribed transcript socket missing.");
    await prepareTranscriptSocket(socket, sessionID);
    binaryAck(socket, 2, StreamService.method.subscribe, {
      result: create(StreamService.method.subscribe.output, { outcome: { case: "success", value: {} } }),
    });
    binaryNotification(
      socket,
      StreamService.method.complete,
      encode(StreamService.method.complete.input, create(StreamService.method.complete.input)),
    );
    await vi.advanceTimersByTimeAsync(10_000);
    expect(onComplete.mock.calls.map(([completion]) => completion.code)).toEqual([-17, 0]);
    expect(onError).not.toHaveBeenCalled();
    expect(sockets).toHaveLength(3);
    subscription.close();
    second.close();
    third.close();
  });
});

async function prepareTranscriptSocket(socket: MockWebSocket, sessionID: string): Promise<void> {
  await socket.setup();
  binaryAck(socket, 1, ConnectionService.method.attachSession, {
    result: create(AttachSessionResultSchema, {
      outcome: {
        case: "success",
        value: {
          attachment: {
            case: "session",
            value: {
              projectId: "project-1",
              workspaceId: "workspace-1",
              workspaceRoot: "/workspace",
              sessionId: sessionID,
              reattachCapability: "capability",
            },
          },
        },
      },
    }),
  });
  await waitForSent(socket, 3);
}

function binaryNotification(
  socket: MockWebSocket,
  method: typeof StreamService.method.event | typeof StreamService.method.complete,
  payload: Uint8Array,
): void {
  const bytes = encodeEnvelope({
    frame: { case: "notificationEvent", value: { operation: operationName(method), payload } },
  });
  const frame = new ArrayBuffer(bytes.length);
  new Uint8Array(frame).set(bytes);
  socket.receive(frame);
}

function subscribeProject(handler: Partial<RpcEventHandler>) {
  const transport = createJsonRpcTransport("ws://127.0.0.1:53082/rpc");
  const subscription = transport.subscribe(
    "workflow.subscribeProject",
    { project_id: "project-1" },
    {
      onEvent() {
        return;
      },
      onComplete() {
        return;
      },
      onError(error) {
        throw error;
      },
      ...handler,
    },
  );
  return { subscription, socket: sockets[0] ?? failTest("subscription socket missing") };
}

function ack(socket: MockWebSocket, sentIndex: number): void {
  const raw = socket.sent[sentIndex] ?? failTest(`sent frame ${sentIndex.toString()} missing`);
  if (z.string().safeParse(raw).success) {
    const sent = frame(socket, sentIndex);
    socket.receive(JSON.stringify({ jsonrpc: "2.0", id: sent.id, result: {} }));
    return;
  }
  const call = descriptorCall(socket, sentIndex);
  if (call.operation === operationName(ConnectionService.method.handshake)) {
    binaryAck(socket, sentIndex, ConnectionService.method.handshake, {
      result: handshakeResult(),
    });
    return;
  }
  if (call.operation === operationName(ConnectionService.method.attachSession)) {
    binaryAck(socket, sentIndex, ConnectionService.method.attachSession, {
      result: create(AttachSessionResultSchema, {
        outcome: {
          case: "success",
          value: {
            attachment: {
              case: "session",
              value: {
                projectId: "project-1",
                workspaceId: "workspace-1",
                workspaceRoot: "/workspace",
                sessionId: "session-1",
                reattachCapability: "reattach-capability-1",
              },
            },
          },
        },
      }),
    });
    return;
  }
  if (call.operation === operationName(ServerService.method.getReadiness)) {
    binaryAck(socket, sentIndex, ServerService.method.getReadiness, {
      result: readinessResult(),
    });
    return;
  }
  throw new Error(`Unsupported descriptor setup operation ${call.operation}.`);
}

async function callReadiness(transport: ReturnType<typeof createJsonRpcTransport>) {
  return transport.callDescriptor(
    ServerService.method.getReadiness,
    create(ServerService.method.getReadiness.input),
  );
}

function readinessResult() {
  return create(GetReadinessResultSchema, {
    outcome: {
      case: "success",
      value: {
        readiness: {
          ready: true,
          serverId: "server-1",
          serverVersion: "test",
          serverBuild: "test",
          protocolVersion: "126",
          endpoint: "ws://127.0.0.1:53082/rpc",
        },
      },
    },
  });
}

function errorAck(
  socket: MockWebSocket,
  sentIndex: number,
  error: Readonly<{ code: number; message: string; data?: unknown }>,
): void {
  const raw = socket.sent[sentIndex] ?? failTest(`sent frame ${sentIndex.toString()} missing`);
  if (!z.string().safeParse(raw).success) {
    const call = descriptorCall(socket, sentIndex);
    if (call.operation === operationName(ConnectionService.method.attachSession)) {
      binaryAck(socket, sentIndex, ConnectionService.method.attachSession, {
        result: create(AttachSessionResultSchema, {
          outcome: {
            case: "error",
            value: {
              code: "internal_failure",
              detail: {
                case: "internalFailure",
                value: { operation: call.operation, cause: error.message },
              },
            },
          },
        }),
      });
      return;
    }
    throw new Error(`Unsupported descriptor setup error for ${call.operation}.`);
  }
  const sent = frame(socket, sentIndex);
  socket.receive(JSON.stringify({ jsonrpc: "2.0", id: sent.id, error }));
}

function handshakeProtocolMismatchAck(socket: MockWebSocket, sentIndex: number): void {
  binaryAck(socket, sentIndex, ConnectionService.method.handshake, {
    result: create(HandshakeResultSchema, {
      outcome: {
        case: "error",
        value: {
          code: "protocol_version_mismatch",
          detail: {
            case: "protocolVersionMismatch",
            value: { requiredProtocolVersion: "126" },
          },
        },
      },
    }),
  });
}

function ackHandshakeRoot(socket: MockWebSocket, sentIndex: number, rootId: string): void {
  binaryAck(socket, sentIndex, ConnectionService.method.handshake, {
    result: handshakeResult(rootId),
  });
}

function handshakeResult(persistenceRootId?: string) {
  return create(HandshakeResultSchema, {
    outcome: {
      case: "success",
      value: {
        identity: {
          protocolVersion: "126",
          serverId: "server-1",
          pid: 1,
          ...(persistenceRootId === undefined ? {} : { persistenceRootId }),
        },
      },
    },
  });
}

function descriptorCall(
  socket: MockWebSocket,
  sentIndex: number,
): Readonly<{ operation: string; correlation: string }> {
  const raw = socket.sent[sentIndex] ?? failTest(`sent frame ${sentIndex.toString()} missing`);
  if (!(raw instanceof Uint8Array)) {
    throw new Error("Mock WebSocket frame is text.");
  }
  const call = decodeEnvelope(raw).frame;
  if (call.case !== "call" || call.value.correlation === undefined) {
    throw new Error("Mock WebSocket binary frame is not a correlated call.");
  }
  return { operation: call.value.operation, correlation: call.value.correlation };
}

function descriptorOperation(socket: MockWebSocket, sentIndex: number): string {
  return descriptorCall(socket, sentIndex).operation;
}

function frame(socket: MockWebSocket, sentIndex: number): SentFrame {
  const raw = socket.sent[sentIndex] ?? failTest(`sent frame ${sentIndex.toString()} missing`);
  const text = z.string().safeParse(raw);
  if (!text.success) {
    throw new Error("Mock WebSocket frame is binary.");
  }
  const parsed: unknown = JSON.parse(text.data);
  if (!isSentFrame(parsed)) {
    throw new Error("Mock WebSocket frame missing id or method.");
  }
  return { id: parsed.id, method: parsed.method };
}

function binaryAck<
  Method extends
    | typeof ServerService.method.getReadiness
    | typeof ConnectionService.method.handshake
    | typeof ConnectionService.method.attachSession
    | typeof QuestionService.method.listPending
    | typeof StreamService.method.subscribe,
>(
  socket: MockWebSocket,
  sentIndex: number,
  method: Method,
  response: Readonly<{
    result: ReturnType<typeof create<Method["output"]>>;
    operation?: string;
  }>,
): void {
  const raw = socket.sent[sentIndex] ?? failTest(`sent frame ${sentIndex.toString()} missing`);
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

function malformedBinaryAck(socket: MockWebSocket, sentIndex: number): void {
  const call = descriptorCall(socket, sentIndex);
  const correlation = new TextEncoder().encode(call.correlation);
  if (correlation.byteLength > 127) {
    throw new Error("Mock WebSocket correlation is too long for the malformed fixture.");
  }
  // Encode an envelope result with correlation but no required operation,
  // bypassing contract validation to exercise malformed server input.
  const result = Uint8Array.of(0x12, correlation.byteLength, ...correlation);
  const encodedResponse = Uint8Array.of(0x12, result.byteLength, ...result);
  const responseBuffer = new ArrayBuffer(encodedResponse.byteLength);
  new Uint8Array(responseBuffer).set(encodedResponse);
  socket.receive(responseBuffer);
}

function isSentFrame(value: unknown): value is SentFrame {
  return sentFrameSchema.safeParse(value).success;
}

async function flushPromises(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

async function waitForSent(socket: MockWebSocket, count: number): Promise<void> {
  await vi.waitFor(() => {
    expect(socket.sent.length).toBeGreaterThanOrEqual(count);
  });
}

function failTest(message: string): never {
  throw new Error(message);
}
