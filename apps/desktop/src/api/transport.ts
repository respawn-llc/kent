import type { JsonValue } from "./json";
import type { DescMessage, DescMethod, MessageShape } from "@app/server-api-contract";

export type RpcEventHandler = Readonly<{
  onOpen?(): void;
  onEvent(method: string, params: unknown): void;
  onComplete(code: number, message: string): void;
  onError(error: Error): void;
}>;

export type RpcSubscription = Readonly<{
  close(): void;
}>;

export type DescriptorSubscriptionHandler<Event, Completion> = Readonly<{
  onOpen?(): void;
  onEvent(event: Event): void;
  onComplete(completion: Completion): undefined | Error;
  onError(error: Error): void;
}>;

export type DescriptorSubscriptionInput<
  Method extends DescMethod,
  EventDescriptor extends DescMessage,
  CompletionDescriptor extends DescMessage,
> = Readonly<{
  method: Method;
  request: MessageShape<Method["input"]>;
  eventDescriptor: EventDescriptor;
  completionDescriptor: CompletionDescriptor;
  onStart(result: MessageShape<Method["output"]>): void;
  handler: DescriptorSubscriptionHandler<MessageShape<EventDescriptor>, MessageShape<CompletionDescriptor>>;
  attachment?: Readonly<{ projectID: string; sessionID: string }>;
  establishmentTimeoutMs?: number | null;
  transcriptRejection?: Readonly<{ onInvalidEvent(error: Error): void }>;
}>;

export type RpcCallOptions = Readonly<{
  timeoutMs?: number | null;
}>;

export type RpcDedicatedCallOptions = RpcCallOptions &
  Readonly<{
    signal?: AbortSignal;
  }>;

export type ProjectAttachment = Readonly<{
  projectID: string;
  workspaceID: string;
  workspaceRoot: string;
  workspaceSelection:
    | Readonly<{ kind: "workspaceID"; workspaceID: string }>
    | Readonly<{ kind: "workspaceRoot"; requestedRoot: string; canonicalRoot: string }>;
}>;

export type SessionAttachment = Readonly<{
  projectID: string;
  workspaceID: string;
  workspaceRoot: string;
  sessionID: string;
}>;
export type SessionAttachmentTarget = Readonly<{ sessionID: string; projectID?: string }>;

export type AttachedRequest =
  | Readonly<{ kind: "value"; value: JsonValue }>
  | Readonly<{ kind: "factory"; create(attachment: ProjectAttachment): JsonValue }>;

export type AttachedProjectCall = Readonly<{
  projectID: string;
  selector: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
  method: string;
  request: AttachedRequest;
}>;

export type AttachedProjectDescriptorCall<Method extends DescMethod> = Readonly<{
  projectID: string;
  selector: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
  method: Method;
  createRequest(attachment: ProjectAttachment): MessageShape<Method["input"]>;
}>;

export type RuntimeOwnerContext = Readonly<{
  attachment: SessionAttachment;
  call(method: string, params: JsonValue): Promise<unknown>;
  callDescriptor<Method extends DescMethod>(
    method: Method,
    request: MessageShape<Method["input"]>,
  ): Promise<MessageShape<Method["output"]>>;
  poison(): void;
}>;

export type RuntimeOwnerOptions = Readonly<{
  createIfMissing: boolean;
  closeAfter?: boolean;
}>;

export type RpcTransport = Readonly<{
  call(method: string, params: JsonValue, options?: RpcCallOptions): Promise<unknown>;
  callDedicated(method: string, params: JsonValue, options?: RpcDedicatedCallOptions): Promise<unknown>;
  callAttachedProject(
    input: AttachedProjectCall,
    options?: RpcDedicatedCallOptions,
  ): Promise<Readonly<{ result: unknown; attachment: ProjectAttachment }>>;
  runRuntimeOwner<Result>(
    sessionID: string,
    options: RuntimeOwnerOptions,
    run: (context: RuntimeOwnerContext) => Promise<Result>,
  ): Promise<Result>;
  subscribe(method: string, params: JsonValue, handler: RpcEventHandler): RpcSubscription;
}>;

export type DescriptorRpcTransport = RpcTransport &
  Readonly<{
    callDescriptorAttachedSession<Method extends DescMethod>(
      target: SessionAttachmentTarget,
      method: Method,
      request: MessageShape<Method["input"]>,
      options?: RpcDedicatedCallOptions,
    ): Promise<MessageShape<Method["output"]>>;
    callDescriptor<Method extends DescMethod>(
      method: Method,
      request: MessageShape<Method["input"]>,
      options?: RpcCallOptions,
    ): Promise<MessageShape<Method["output"]>>;
    callDescriptorAttachedProject<Method extends DescMethod>(
      input: AttachedProjectDescriptorCall<Method>,
      options?: RpcDedicatedCallOptions,
    ): Promise<Readonly<{ result: MessageShape<Method["output"]>; attachment: ProjectAttachment }>>;
    subscribeDescriptor<
      Method extends DescMethod,
      EventDescriptor extends DescMessage,
      CompletionDescriptor extends DescMessage,
    >(
      input: DescriptorSubscriptionInput<Method, EventDescriptor, CompletionDescriptor>,
    ): RpcSubscription;
  }>;
