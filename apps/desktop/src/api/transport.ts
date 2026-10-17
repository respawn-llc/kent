import type { DescMessage, DescMethod, MessageShape, MessageValidType } from "@app/server-api-contract";

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
  onStart(result: MessageValidType<Method["output"]>): void;
  handler: DescriptorSubscriptionHandler<
    MessageValidType<EventDescriptor>,
    MessageValidType<CompletionDescriptor>
  >;
  attachment?: SessionAttachmentTarget;
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
export type SessionAttachmentTarget = Readonly<{ sessionID: string }>;

export type AttachedProjectDescriptorCall<Method extends DescMethod> = Readonly<{
  projectID: string;
  selector: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
  method: Method;
  createRequest(attachment: ProjectAttachment): MessageShape<Method["input"]>;
}>;

export type RpcTransport = Readonly<{
  callDescriptorAttachedSession<Method extends DescMethod>(
    target: SessionAttachmentTarget,
    method: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcDedicatedCallOptions,
  ): Promise<MessageValidType<Method["output"]>>;
  callDescriptor<Method extends DescMethod>(
    method: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcDedicatedCallOptions,
  ): Promise<MessageValidType<Method["output"]>>;
  callDescriptorAttachedProject<Method extends DescMethod>(
    input: AttachedProjectDescriptorCall<Method>,
    options?: RpcDedicatedCallOptions,
  ): Promise<Readonly<{ result: MessageValidType<Method["output"]>; attachment: ProjectAttachment }>>;
  subscribeDescriptor<
    Method extends DescMethod,
    EventDescriptor extends DescMessage,
    CompletionDescriptor extends DescMessage,
  >(
    input: DescriptorSubscriptionInput<Method, EventDescriptor, CompletionDescriptor>,
  ): RpcSubscription;
}>;
