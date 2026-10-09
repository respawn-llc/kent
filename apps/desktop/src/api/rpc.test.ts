import { createRpcTransport } from "./rpc";
import { ProtocolMismatchError, RpcError, ServerRootMismatchError } from "./errors";
import { protocolVersion } from "./rpcSocket";
import {
  create,
  decodeEnvelope,
  encode,
  encodeEnvelope,
  operationName,
  type DescMethod,
} from "@app/server-api-contract";
import {
  ProjectEventSchema,
  ProjectEventAction,
  ProjectEventResource,
  ProjectSubscriptionService,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import {
  StreamCompletionSchema,
  StreamFailureCode,
} from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { subscribeWorkflowProject, type WorkflowProjectEventHandler } from "./workflowProjectEvents";
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
import { TaskReadService, SearchMode } from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { TaskLifecycleService } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { WorktreeError } from "./clientWorktree";
import { ApiClient } from "./client";
import { unexpectedProjectOverflow } from "@/test-support/api";
import { searchTasks } from "./clientTaskSearch";

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

describe("RpcWebSocketTransport", () => {
  beforeEach(() => {
    sockets.length = 0;
    vi.stubGlobal("WebSocket", MockWebSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("rejects pending mutations on disconnect and does not replay them on reconnect", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const mutation = callReadiness(transport);
    const firstSocket = sockets[0] ?? failTest("first socket missing");

    await firstSocket.setup();
    expect(descriptorOperation(firstSocket, 1)).toBe(operationName(ServerService.method.getReadiness));

    firstSocket.close();
    await expect(mutation).rejects.toThrow("closed");
    expect(firstSocket.sent).toHaveLength(2);

    const retry = callReadiness(transport);
    const secondSocket = sockets[1] ?? failTest("second socket missing");
    await secondSocket.setup();
    expect(secondSocket.sent).toHaveLength(2);
    ack(secondSocket, 1);

    await expect(retry).resolves.toMatchObject({ outcome: { case: "success" } });
    expect(firstSocket.sent).toHaveLength(2);
  });

  it("isolates malformed binary and text frames from pending and subsequent control calls", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const readiness = callReadiness(transport);
    const socket = sockets[0] ?? failTest("control socket missing");

    await socket.setup();
    binaryAck(socket, 1, ServerService.method.getReadiness, { result: readinessResult() });
    await expect(readiness).resolves.toMatchObject({
      outcome: { case: "success", value: { readiness: { serverId: "server-1" } } },
    });

    const concurrentReadiness = callReadiness(transport);
    const request = callReadiness(transport);
    await waitForSent(socket, 4);
    socket.receive(JSON.stringify({ id: descriptorCall(socket, 3).correlation, result: {} }));
    ack(socket, 3);
    ack(socket, 2);
    await expect(concurrentReadiness).resolves.toMatchObject({ outcome: { case: "success" } });
    await expect(request).resolves.toMatchObject({ outcome: { case: "success" } });
    const malformedReadiness = callReadiness(transport);
    await waitForSent(socket, 5);
    socket.receive(new Uint8Array([0xff]).buffer);
    malformedBinaryAck(socket, 4);
    await expect(malformedReadiness).rejects.toBeInstanceOf(Error);
    const next = callReadiness(transport);
    await waitForSent(socket, 6);
    ack(socket, 5);
    await expect(next).resolves.toMatchObject({ outcome: { case: "success" } });
    expect(sockets).toHaveLength(1);
    expect(socket.readyState).toBe(MockWebSocket.OPEN);
  });

  it("runs dedicated calls on a one-use socket without disturbing the control socket", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const readiness = callReadiness(transport);
    const controlSocket = sockets[0] ?? failTest("control socket missing");
    await controlSocket.setup();
    ack(controlSocket, 1);
    await expect(readiness).resolves.toMatchObject({ outcome: { case: "success" } });

    const method = TaskReadService.method.search;
    const search = transport.callDescriptor(
      method,
      create(method.input, {
        query: "needle",
        context: 20,
        pageSize: 25,
        mode: SearchMode.LITERAL,
      }),
    );
    const dedicatedSocket = sockets[1] ?? failTest("dedicated socket missing");
    await dedicatedSocket.setup();
    expect(descriptorOperation(dedicatedSocket, 1)).toBe(operationName(method));
    binaryAck(dedicatedSocket, 1, method, {
      result: create(method.output, {
        outcome: { case: "success", value: { groups: [], mode: SearchMode.LITERAL } },
      }),
    });

    await expect(search).resolves.toMatchObject({ outcome: { case: "success" } });
    expect(dedicatedSocket.readyState).toBe(MockWebSocket.CLOSED);
    expect(controlSocket.readyState).toBe(MockWebSocket.OPEN);
  });

  it("attaches a dedicated Session before a Session-scoped call", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
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
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
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
      code: "server_not_ready",
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

  it("rejects AbortSignal for multiplexed descriptors before opening a socket", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const method = ServerService.method.getReadiness;
    const result = transport
      .callDescriptor(method, create(method.input), {
        signal: new AbortController().signal,
      })
      .catch((error: unknown) => error);
    const socket = sockets[0];
    if (socket !== undefined) {
      await socket.setup();
      binaryAck(socket, 1, method, { result: readinessResult() });
    }
    expect(await result).toBeInstanceOf(TransportError);
    expect(sockets).toHaveLength(0);
  });

  it("cancels a dedicated call by closing only its socket", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const readiness = callReadiness(transport);
    const control = sockets[0] ?? failTest("control socket missing");
    await control.setup();
    ack(control, 1);
    await readiness;
    const controller = new AbortController();
    const search = searchTasks(
      transport,
      {
        mode: "literal",
        query: "needle",
        context: 20,
        caseSensitive: false,
        includeComments: false,
        pageSize: 25,
      },
      controller.signal,
    );
    const socket = sockets[1] ?? failTest("dedicated socket missing");
    await socket.setup();

    const operation = descriptorCall(socket, 1).operation;
    controller.abort();

    await expect(search).rejects.toThrow("canceled");
    expect(operation).toBe(operationName(TaskReadService.method.search));
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
    const nextReadiness = callReadiness(transport);
    await waitForSent(control, 3);
    ack(control, 2);
    await expect(nextReadiness).resolves.toMatchObject({ outcome: { case: "success" } });
    expect(control.readyState).toBe(MockWebSocket.OPEN);
    expect(sockets).toHaveLength(2);
  });

  it("retains the typed Worktree blocker through Task deletion without closing control", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    const deletion = client.deleteTask("task-1").catch((error: unknown) => error);
    const socket = sockets[0] ?? failTest("control socket missing");
    await socket.setup();
    const method = TaskLifecycleService.method.delete;
    binaryAck(socket, 1, method, {
      result: create(method.output, {
        outcome: {
          case: "error",
          value: {
            code: "worktree_blocked",
            detail: { case: "worktreeBlocked", value: {} },
          },
        },
      }),
    });
    const error = await deletion;
    expect(error).toBeInstanceOf(WorktreeError);
    if (!(error instanceof WorktreeError)) throw error;
    expect(error.detail.kind).toBe("blocked");
    const readiness = callReadiness(transport);
    await waitForSent(socket, 3);
    ack(socket, 2);
    await expect(readiness).resolves.toMatchObject({ outcome: { case: "success" } });
    expect(socket.readyState).toBe(MockWebSocket.OPEN);
  });

  it("rejects control calls when the server serves a different persistence root", async () => {
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc", "expected-root");
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
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc", "expected-root");
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
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
    const mutation = transport.callDescriptor(
      ServerService.method.getReadiness,
      create(ServerService.method.getReadiness.input),
      { timeoutMs: null },
    );
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
    expect(descriptorOperation(socket, 1)).toBe(operationName(ServerService.method.getReadiness));

    await vi.advanceTimersByTimeAsync(31_000);
    expect(settled).toBe(false);
    ack(socket, 1);

    await expect(mutation).resolves.toMatchObject({ outcome: { case: "success" } });
  });

  it("installs subscription event listener before subscribe ack can race with first event", async () => {
    const events: string[] = [];
    const opens: string[] = [];
    const { socket } = subscribeProject({
      onOpen() {
        opens.push("open");
      },
      onEvent(event) {
        events.push(event.primaryEntityID);
      },
    });
    socket.open();
    await waitForSent(socket, 1);
    ack(socket, 0);
    await waitForSent(socket, 2);
    expect(descriptorOperation(socket, 1)).toBe(operationName(ProjectSubscriptionService.method.subscribe));

    projectEvent(socket);
    projectAck(socket);
    await flushPromises();

    expect(opens).toEqual(["open"]);
    expect(events).toEqual(["task-1"]);
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
    projectAck(firstSocket);
    await flushPromises();

    firstSocket.close();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(sockets).toHaveLength(1);
    expect(errors).toHaveLength(1);
    expect(errors[0]).toBeInstanceOf(TransportError);
    const next = subscribeProject({});
    const secondSocket = sockets[1] ?? failTest("resubscription socket missing");
    await secondSocket.setup();

    expect(descriptorOperation(secondSocket, 1)).toBe(
      operationName(ProjectSubscriptionService.method.subscribe),
    );
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

    binaryAck(socket, 1, ProjectSubscriptionService.method.subscribe, {
      result: create(ProjectSubscriptionService.method.subscribe.output, {
        outcome: {
          case: "error",
          value: {
            code: "internal_failure",
            detail: { case: "internalFailure", value: { cause: "Subscription rejected" } },
          },
        },
      }),
    });
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
    const completions: (StreamFailureCode | null)[] = [];
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
    projectAck(firstSocket);
    await flushPromises();

    binaryNotification(
      firstSocket,
      ProjectSubscriptionService.method.complete,
      encode(
        StreamCompletionSchema,
        create(StreamCompletionSchema, { code: StreamFailureCode.STREAM_GAP, message: "stream gap" }),
      ),
    );

    await vi.advanceTimersByTimeAsync(10_000);
    expect(sockets).toHaveLength(1);
    expect(completions).toEqual([StreamFailureCode.STREAM_GAP]);
    expect(errors).toHaveLength(1);
    subscription.close();
  });

  it("does not reconnect after normal server complete notification", async () => {
    const completions: string[] = [];
    const errors: string[] = [];
    const { subscription, socket } = subscribeProject({
      onComplete(code, message) {
        completions.push(`${String(code)}:${String(message)}`);
      },
      onError(error) {
        errors.push(error.message);
      },
    });
    await socket.setup();
    projectAck(socket);
    await flushPromises();

    binaryNotification(
      socket,
      ProjectSubscriptionService.method.complete,
      encode(StreamCompletionSchema, create(StreamCompletionSchema)),
    );
    await flushPromises();

    expect(completions).toEqual(["null:null"]);
    expect(errors).toEqual([]);
    expect(sockets).toHaveLength(1);
    subscription.close();
  });

  it("keeps subscriptions active for Task completion events", async () => {
    const events: string[] = [];
    const completions: string[] = [];
    const { subscription, socket } = subscribeProject({
      onEvent(event) {
        events.push(event.action);
      },
      onComplete(code, message) {
        completions.push(`${String(code)}:${String(message)}`);
      },
    });
    await socket.setup();
    projectAck(socket);
    await flushPromises();

    projectEvent(socket, ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_COMPLETED);
    await flushPromises();

    expect(events).toEqual(["completed"]);
    expect(completions).toEqual([]);
    expect(sockets).toHaveLength(1);
    subscription.close();
  });

  it("discards invalid transcript events on the same socket and makes invalid completion terminal", async () => {
    vi.useFakeTimers();
    const sessionID = "123e4567-e89b-42d3-a456-426614174000";
    const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
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
    const api = createChatApi(createRpcTransport("ws://127.0.0.1:53082/rpc"));
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
          code: StreamFailureCode.STREAM_GAP,
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
    expect(onComplete.mock.calls.map(([completion]) => completion.code)).toEqual([
      StreamFailureCode.STREAM_GAP,
      null,
    ]);
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

function binaryNotification(socket: MockWebSocket, method: DescMethod, payload: Uint8Array): void {
  const bytes = encodeEnvelope({
    frame: { case: "notificationEvent", value: { operation: operationName(method), payload } },
  });
  const frame = new ArrayBuffer(bytes.length);
  new Uint8Array(frame).set(bytes);
  socket.receive(frame);
}

function projectAck(socket: MockWebSocket) {
  binaryAck(socket, 1, ProjectSubscriptionService.method.subscribe, {
    result: create(ProjectSubscriptionService.method.subscribe.output, {
      outcome: { case: "success", value: {} },
    }),
  });
}

function projectEvent(
  socket: MockWebSocket,
  action = ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_UPDATED,
) {
  binaryNotification(
    socket,
    ProjectSubscriptionService.method.event,
    encode(
      ProjectEventSchema,
      create(ProjectEventSchema, {
        projectId: "project-1",
        workflowId: "11111111-1111-4111-8111-111111111111",
        primaryEntityId: "task-1",
        action,
        resource: ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_TASK,
        occurredAt: { seconds: 1n, nanos: 0 },
      }),
    ),
  );
}

function subscribeProject(handler: Partial<WorkflowProjectEventHandler>) {
  const transport = createRpcTransport("ws://127.0.0.1:53082/rpc");
  const subscription = subscribeWorkflowProject(transport, "project-1", {
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
  });
  return { subscription, socket: sockets[0] ?? failTest("subscription socket missing") };
}

function ack(socket: MockWebSocket, sentIndex: number): void {
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

async function callReadiness(transport: ReturnType<typeof createRpcTransport>) {
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
          protocolVersion: "126",
          endpoint: "ws://127.0.0.1:53082/rpc",
        },
      },
    },
  });
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

function binaryAck<
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
