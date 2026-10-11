import {
  binaryFrameBytes,
  binaryFramePayload,
  completeDescriptorResponse,
  decodeDescriptorResponse,
  descriptorResponseCorrelation,
  encodeDescriptorCall,
} from "./descriptorRpc";
import { TransportError } from "./errors";
import {
  unaryConnectionPolicy,
  type DescMessage,
  type DescMethod,
  type MessageShape,
} from "@app/server-api-contract";
import {
  openSocket,
  sendSocketDescriptorRequest,
  runSocketDescriptorSubscription,
  setupSocket,
  requireSessionAttachment,
} from "./rpcSocket";
import { RpcRuntimeOwner } from "./rpcRuntimeOwner";
import { TerminalSubscriptionError } from "./subscriptionErrors";
import { requireProjectAttachment } from "./chatAttachment";
import type {
  RpcCallOptions,
  DescriptorSubscriptionInput,
  AttachedProjectDescriptorCall,
  RpcDedicatedCallOptions,
  RpcSubscription,
  RpcTransport,
  ProjectAttachment,
  SessionAttachment,
  SessionAttachmentTarget,
  RuntimeOwnerContext,
  RuntimeOwnerOptions,
} from "./transport";

const socketOpenTimeoutMs = 10_000;
const rpcRequestTimeoutMs = 30_000;

type PendingRequestBase = Readonly<{
  label: string;
  timeout: ReturnType<typeof setTimeout> | null;
  reject(error: Error): void;
}>;

type PendingRequest = PendingRequestBase &
  Readonly<{
    complete(response: ReturnType<typeof decodeDescriptorResponse>): void;
  }>;

export function createRpcTransport(endpoint: string, expectedRootId = ""): RpcTransport {
  return new RpcWebSocketTransport(endpoint, expectedRootId);
}

class RpcWebSocketTransport implements RpcTransport {
  #endpoint: string;
  #expectedRootId: string;
  #socket: WebSocket | null = null;
  #opening: Promise<WebSocket> | null = null;
  #nextID = 1;
  #pending = new Map<string, PendingRequest>();
  #runtimeOwner: RpcRuntimeOwner;

  constructor(endpoint: string, expectedRootId: string) {
    this.#endpoint = endpoint;
    this.#expectedRootId = expectedRootId;
    this.#runtimeOwner = new RpcRuntimeOwner(endpoint, expectedRootId);
  }

  async callDescriptor<Method extends DescMethod>(
    method: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcDedicatedCallOptions,
  ): Promise<MessageShape<Method["output"]>> {
    switch (unaryConnectionPolicy(method)) {
      case "multiplexed": {
        if (options?.signal !== undefined) {
          throw new TransportError("AbortSignal requires a dedicated operation descriptor.");
        }
        const socket = await this.#open();
        return this.#sendDescriptor(socket, method, request, options);
      }
      case "dedicated":
        return this.#withDedicatedSocket(options, async (socket, requestOptions) =>
          sendSocketDescriptorRequest(socket, method, request, requestOptions),
        );
    }
  }

  async callDescriptorAttachedProject<Method extends DescMethod>(
    input: AttachedProjectDescriptorCall<Method>,
    options?: RpcDedicatedCallOptions,
  ): Promise<Readonly<{ result: MessageShape<Method["output"]>; attachment: ProjectAttachment }>> {
    const { projectID, selector, method, createRequest } = input;
    return this.#withDedicatedSocket(
      options,
      async (socket, requestOptions, attachment) => {
        const validatedAttachment = requireProjectAttachment(attachment, { projectID, workspace: selector });
        return {
          result: await sendSocketDescriptorRequest(
            socket,
            method,
            createRequest(validatedAttachment),
            requestOptions,
          ),
          attachment: validatedAttachment,
        };
      },
      { projectID, workspace: selector },
    );
  }

  async callDescriptorAttachedSession<Method extends DescMethod>(
    target: SessionAttachmentTarget,
    method: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcDedicatedCallOptions,
  ): Promise<MessageShape<Method["output"]>> {
    const attachedSessionID = target.sessionID.trim();
    if (attachedSessionID.length === 0) {
      throw new TransportError("Session attachment requires a Session ID.");
    }
    return this.#withDedicatedSocket(
      options,
      async (socket, requestOptions, attachment) => {
        requireSessionAttachment(attachment, { ...target, sessionID: attachedSessionID });
        return sendSocketDescriptorRequest(socket, method, request, requestOptions);
      },
      { sessionID: attachedSessionID },
    );
  }

  async runRuntimeOwner<Result>(
    sessionID: string,
    options: RuntimeOwnerOptions,
    run: (context: RuntimeOwnerContext) => Promise<Result>,
  ): Promise<Result> {
    return this.#runtimeOwner.run(sessionID, options, run);
  }

  async #withDedicatedSocket<Result>(
    options: RpcDedicatedCallOptions | undefined,
    run: (
      socket: WebSocket,
      requestOptions: Readonly<{ timeoutMilliseconds: number | null; signal?: AbortSignal }>,
      attachment: ProjectAttachment | SessionAttachment | null,
    ) => Promise<Result>,
    attachmentTarget?: Readonly<{
      sessionID?: string;
      projectID?: string;
      workspace?: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
    }>,
  ): Promise<Result> {
    const socket = await openSocket(this.#endpoint, socketOpenTimeoutMs, options?.signal);
    try {
      const setupAttachment = await setupSocket(
        socket,
        socketSetupOptions(this.#expectedRootId, options, attachmentTarget),
      );
      const timeoutMs = options?.timeoutMs === undefined ? rpcRequestTimeoutMs : options.timeoutMs;
      const requestOptions =
        options?.signal === undefined
          ? { timeoutMilliseconds: timeoutMs }
          : { timeoutMilliseconds: timeoutMs, signal: options.signal };
      return await run(socket, requestOptions, setupAttachment);
    } finally {
      socket.close();
    }
  }

  subscribeDescriptor<
    Method extends DescMethod,
    EventDescriptor extends DescMessage,
    CompletionDescriptor extends DescMessage,
  >(input: DescriptorSubscriptionInput<Method, EventDescriptor, CompletionDescriptor>): RpcSubscription {
    const controller = new AbortController();
    const { handler } = input;
    void this.#openSubscription(
      async (socket) =>
        runSocketDescriptorSubscription({
          socket,
          ...input,
          signal: controller.signal,
        }),
      handler.onError,
      controller.signal,
      input.attachment,
    );
    return {
      close: () => {
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
    this.#opening = this.#connectControl();
    try {
      return await this.#opening;
    } finally {
      this.#opening = null;
    }
  }

  async #connectControl(): Promise<WebSocket> {
    const socket = await openSocket(this.#endpoint, socketOpenTimeoutMs);
    socket.addEventListener("message", (event) => {
      this.#handleControlMessage(event);
    });
    socket.addEventListener("close", () => {
      this.#handleControlClose();
    });
    socket.addEventListener("error", () => {
      this.#handleControlError();
    });
    try {
      await setupSocket(socket, {
        timeoutMilliseconds: rpcRequestTimeoutMs,
        expectedRootId: this.#expectedRootId,
      });
    } catch (error) {
      socket.close();
      throw error;
    }
    this.#socket = socket;
    return socket;
  }

  async #sendDescriptor<Method extends DescMethod>(
    socket: WebSocket,
    method: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcCallOptions,
  ): Promise<MessageShape<Method["output"]>> {
    if (socket.readyState !== WebSocket.OPEN) {
      throw new TransportError("WebSocket is not open.");
    }
    const id = `gui-${this.#nextID.toString()}`;
    this.#nextID += 1;
    const { operation, bytes } = encodeDescriptorCall(method, request, id);
    return new Promise<MessageShape<Method["output"]>>((resolve, reject) => {
      const timeoutMs = options?.timeoutMs === undefined ? rpcRequestTimeoutMs : options.timeoutMs;
      const timeout =
        timeoutMs === null
          ? null
          : setTimeout(() => {
              if (!this.#pending.delete(id)) {
                return;
              }
              reject(new TransportError(`${operation} request timed out.`));
            }, timeoutMs);
      this.#pending.set(id, {
        label: operation,
        timeout,
        complete: (response) => {
          resolve(completeDescriptorResponse(method, id, response));
        },
        reject,
      });
      try {
        socket.send(binaryFramePayload(bytes));
      } catch (error) {
        if (timeout !== null) {
          clearTimeout(timeout);
        }
        this.#pending.delete(id);
        reject(error instanceof Error ? error : new TransportError(`${operation} request failed to send.`));
      }
    });
  }

  #handleControlMessage(event: MessageEvent<unknown>): void {
    const bytes = binaryFrameBytes(event.data);
    if (bytes === undefined) {
      return;
    }
    this.#handleBinaryControlMessage(bytes);
  }

  #handleBinaryControlMessage(bytes: Uint8Array): void {
    try {
      const response = decodeDescriptorResponse(bytes);
      const pending = this.#pending.get(response.correlation);
      if (pending === undefined) {
        return;
      }
      try {
        pending.complete(response);
        this.#takePending(response.correlation);
      } catch (error) {
        this.#takePending(response.correlation);
        pending.reject(
          error instanceof Error ? error : new TransportError("Binary response completion failed."),
        );
      }
    } catch (error) {
      const correlation = descriptorResponseCorrelation(bytes);
      if (correlation === undefined) {
        return;
      }
      const pending = this.#takePending(correlation);
      pending?.reject(
        error instanceof Error ? error : new TransportError("Binary response decoding failed."),
      );
    }
  }

  #takePending(id: string): PendingRequest | undefined {
    const pending = this.#pending.get(id);
    if (pending === undefined) {
      return undefined;
    }
    this.#pending.delete(id);
    if (pending.timeout !== null) {
      clearTimeout(pending.timeout);
    }
    return pending;
  }

  #handleControlClose(): void {
    this.#socket = null;
    this.#rejectAll(new TransportError("Kent service connection closed."));
  }

  #handleControlError(): void {
    this.#socket = null;
    this.#rejectAll(new TransportError("Kent service connection failed."));
  }

  #rejectAll(error: Error): void {
    const pending = [...this.#pending.values()];
    this.#pending.clear();
    for (const request of pending) {
      if (request.timeout !== null) {
        clearTimeout(request.timeout);
      }
      request.reject(error);
    }
  }

  async #openSubscription(
    run: (socket: WebSocket) => Promise<void>,
    onError: (error: Error) => void,
    signal: AbortSignal,
    attachmentTarget?: Readonly<{ sessionID?: string; projectID?: string }>,
  ): Promise<void> {
    try {
      await this.#withSubscriptionSocket(signal, run, attachmentTarget);
    } catch (error) {
      if (abortSignalWasRequested(signal) || error instanceof TerminalSubscriptionError) {
        return;
      }
      onError(error instanceof Error ? error : new TransportError("Subscription failed."));
    }
  }

  async #withSubscriptionSocket(
    signal: AbortSignal,
    run: (socket: WebSocket) => Promise<void>,
    attachmentTarget?: Readonly<{ sessionID?: string; projectID?: string }>,
  ): Promise<void> {
    const socket = await openSocket(this.#endpoint, socketOpenTimeoutMs, signal);
    const abort = () => {
      socket.close();
    };
    signal.addEventListener("abort", abort, { once: true });
    try {
      const attachment = await setupSocket(socket, {
        timeoutMilliseconds: rpcRequestTimeoutMs,
        expectedRootId: this.#expectedRootId,
        signal,
        ...(attachmentTarget?.sessionID === undefined ? {} : { sessionID: attachmentTarget.sessionID }),
      });
      if (attachmentTarget?.sessionID !== undefined) {
        requireSessionAttachment(attachment, {
          ...(attachmentTarget.projectID === undefined ? {} : { projectID: attachmentTarget.projectID }),
          sessionID: attachmentTarget.sessionID,
        });
      }
      await run(socket);
    } finally {
      signal.removeEventListener("abort", abort);
      socket.close();
    }
  }
}

function abortSignalWasRequested(signal: AbortSignal): boolean {
  return signal.aborted;
}

function socketSetupOptions(
  expectedRootId: string,
  options: RpcDedicatedCallOptions | undefined,
  attachmentTarget:
    | Readonly<{
        sessionID?: string;
        projectID?: string;
        workspace?: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
      }>
    | undefined,
): Parameters<typeof setupSocket>[1] {
  const result: {
    timeoutMilliseconds: number;
    expectedRootId: string;
    signal?: AbortSignal;
    sessionID?: string;
    projectSelector?: Readonly<{
      projectID: string;
      workspace: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
    }>;
  } = {
    timeoutMilliseconds: rpcRequestTimeoutMs,
    expectedRootId,
  };
  if (options?.signal !== undefined) {
    result.signal = options.signal;
  }
  if (attachmentTarget?.sessionID !== undefined) {
    result.sessionID = attachmentTarget.sessionID;
  }
  if (attachmentTarget?.projectID !== undefined && attachmentTarget.workspace !== undefined) {
    result.projectSelector = {
      projectID: attachmentTarget.projectID,
      workspace: attachmentTarget.workspace,
    };
  }
  return result;
}
