export {
  sessionSettingsSubscriptionRoute,
  hasSessionSettingsSubscription,
  emitSessionSettingsSnapshot,
} from "./sessionSettingsFixtures";
import {
  create,
  decode,
  encode,
  validate,
  operationName,
  type DescMessage,
  type DescMethod,
  type Message,
  type MessageShape,
} from "@app/server-api-contract";
import { ProjectAvailability } from "@app/server-api-contract/gen/kent/api/project/project_pb";
import {
  BranchCleanupOutcomeKind,
  CreateResultSchema,
  CreateService,
  CreateTargetResolutionKind,
  CreateTargetResolveResultSchema,
  CreateTargetService,
  DeletePreviewResultSchema,
  DeletePreviewService,
  DeleteResultSchema,
  DirtyStateKind,
  EnterResultSchema,
  ListService,
  ListEntrySchema,
  type ListEntry,
  SelectorResolveResultSchema,
  SelectorService,
  StatusService,
  SwitchOperationKind,
  TransitionService,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import {
  type RpcTransport,
  type DescriptorSubscriptionInput,
  type AttachedProjectDescriptorCall,
  type ProjectAttachment,
  type RpcDedicatedCallOptions,
  type RpcSubscription,
  type SessionAttachment,
  type RuntimeOwnerContext,
  type RuntimeOwnerOptions,
} from "@/api/composition";

export { worktreeCommandFixture, worktreeCommandFixtureRoutes } from "./worktreeCommandFixtures";
export { StreamFailureCode } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
export { partialWorktreeDeletionError } from "./worktreeErrorFixtures";
export { processObservationFixture, processObservationFixtureRoute } from "./processFixtures";

export async function unexpectedProjectOverflow(): Promise<never> {
  throw new Error("Unexpected Project event overflow in API fixture.");
}

type FakeDescriptorRoute = Readonly<{
  descriptor: DescMethod;
  result?: Message;
  error?: Error;
  resultFactory?: (request: Message, callIndex: number) => Message | Promise<Message>;
}>;

type FakeDescriptorSubscriptionRoute = Readonly<{
  subscriptionDescriptor: DescMethod;
  startResult: Message;
}>;

export type FakeRoute = FakeDescriptorRoute | FakeDescriptorSubscriptionRoute;

export function worktreeBrowserFixtureEntry(
  kind: "mainWorkspace" | "registered" | "missing",
  current: boolean,
): ListEntry {
  const git = {
    canonicalRoot: "/repo/feature",
    headObject: "abc123",
    branchName: "feature",
    detached: false,
    bare: false,
    isMainWorktree: kind === "mainWorkspace",
    pathAvailable: true,
  };
  const kent = {
    worktreeId: "worktree-1",
    canonicalRoot: git.canonicalRoot,
    displayName: "Feature",
    managed: true,
    createdBranch: true,
  };
  return create(ListEntrySchema, {
    topology: {
      topology:
        kind === "mainWorkspace"
          ? { case: kind, value: { git } }
          : kind === "registered"
            ? { case: kind, value: { git, kent } }
            : { case: kind, value: { kent } },
    },
    projection: {
      selector: kind === "mainWorkspace" ? "main" : "feature",
      isCurrent: current,
      ...(!current && kind !== "missing"
        ? {
            switch:
              kind === "mainWorkspace"
                ? { kind: SwitchOperationKind.WORKTREE_SWITCH_OPERATION_LEAVE_MAIN }
                : { kind: SwitchOperationKind.WORKTREE_SWITCH_OPERATION_ENTER, selector: "feature" },
          }
        : {}),
      ...(kind !== "mainWorkspace" ? { deletePreview: { selector: kent.worktreeId } } : {}),
    },
  });
}

export function worktreeBrowserFixtureRoute(worktrees: ListEntry[] = []) {
  return {
    descriptor: ListService.method.list,
    result: create(ListService.method.list.output, {
      outcome: {
        case: "success",
        value: {
          target: {
            workspaceId: "workspace-1",
            workspaceName: "Workspace",
            workspaceRoot: "/repo",
            workspaceAvailability: ProjectAvailability.AVAILABLE,
            cwdRelpath: ".",
            effectiveWorkdir: "/repo",
          },
          worktrees,
        },
      },
    }),
  };
}

export function worktreeQueryFixtureRoutes(): readonly FakeRoute[] {
  const topology = {
    topology: {
      case: "external",
      value: {
        git: {
          canonicalRoot: "/repo/feature",
          headObject: "abc123",
          branchName: "feature",
          detached: false,
          bare: false,
          isMainWorktree: false,
          pathAvailable: true,
        },
      },
    },
  } as const;
  const projected = (selector: string) => ({
    topology,
    projection: {
      selector,
      isCurrent: false,
      switch: {
        kind: SwitchOperationKind.WORKTREE_SWITCH_OPERATION_ENTER,
        selector,
      },
      deletePreview: { selector: "/repo/feature" },
    },
  });
  return [
    {
      descriptor: StatusService.method.get,
      result: create(StatusService.method.get.output, {
        outcome: {
          case: "success",
          value: {
            target: {
              workspaceId: "workspace-1",
              workspaceName: "Workspace",
              workspaceRoot: "/repo",
              workspaceAvailability: ProjectAvailability.AVAILABLE,
              cwdRelpath: ".",
              effectiveWorkdir: "/repo",
            },
            worktree: { recordedRoot: "/repo" },
          },
        },
      }),
    },
    {
      descriptor: CreateTargetService.method.resolve,
      resultFactory: (_request, callIndex) =>
        create(CreateTargetResolveResultSchema, {
          outcome: {
            case: "success",
            value: {
              resolution: {
                kind: CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_EXISTING_BRANCH,
                input: "feature",
                resolvedRef: `refs/heads/feature-${String(callIndex)}`,
              },
            },
          },
        }),
    },
    {
      descriptor: SelectorService.method.resolve,
      resultFactory: (_request, callIndex) =>
        create(SelectorResolveResultSchema, {
          outcome: {
            case: "success",
            value: { worktree: projected(`feature-${String(callIndex)}`) },
          },
        }),
    },
    {
      descriptor: DeletePreviewService.method.get,
      resultFactory: (_request, callIndex) =>
        create(DeletePreviewResultSchema, {
          outcome: {
            case: "success",
            value: {
              worktree: topology,
              deletionSelector: "/repo/feature",
              cleanliness:
                callIndex === 0
                  ? { kind: DirtyStateKind.DIRTY_STATE_CLEAN }
                  : { kind: DirtyStateKind.DIRTY_STATE_DIRTY, dirtyFileCount: callIndex },
            },
          },
        }),
    },
    {
      descriptor: CreateService.method.create,
      result: create(CreateResultSchema, {
        outcome: {
          case: "success",
          value: {
            target: {
              workspaceId: "workspace-1",
              workspaceName: "Workspace",
              workspaceRoot: "/repo",
              workspaceAvailability: ProjectAvailability.AVAILABLE,
              cwdRelpath: ".",
              effectiveWorkdir: "/repo",
            },
            worktree: projected("feature"),
          },
        },
      }),
    },
    {
      descriptor: TransitionService.method.enter,
      result: create(EnterResultSchema, {
        outcome: {
          case: "error",
          value: {
            code: "internal_failure",
            detail: {
              case: "internalFailure",
              value: { operation: "worktree.enter", cause: "fixture failure" },
            },
          },
        },
      }),
    },
    {
      descriptor: TransitionService.method.delete,
      result: create(DeleteResultSchema, {
        outcome: {
          case: "success",
          value: {
            cleanup: {
              kind: BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED,
            },
          },
        },
      }),
    },
  ];
}

export class FakeRpcTransport implements RpcTransport {
  readonly descriptorCalls: Readonly<{
    descriptor: DescMethod;
    request: Message;
    options?: RpcDedicatedCallOptions;
  }>[] = [];
  readonly attachedSessionCalls: Readonly<{
    sessionID: string;
    method: string;
  }>[] = [];
  readonly attachedProjectDescriptorCalls: Readonly<{
    projectID: string;
    selector: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
    descriptor: DescMethod;
    request: Message;
    options?: RpcDedicatedCallOptions;
  }>[] = [];
  readonly descriptorSubscriptionStarts: Readonly<{
    descriptor: DescMethod;
    request: Message;
  }>[] = [];
  runtimeOwnerRuns = 0;
  #descriptorRoutes = new Map<string, FakeDescriptorRoute>();
  #descriptorSubscriptionRoutes = new Map<string, FakeDescriptorSubscriptionRoute>();
  #callCounts = new Map<string, number>();
  #runtimeOwner: SessionAttachment | null = null;
  #descriptorSubscribers: Readonly<{
    descriptor: DescMethod;
    open(): void;
    event(payload: Uint8Array): void;
    complete(payload: Uint8Array): void;
    fail(error: Error): void;
  }>[] = [];

  constructor(routes: readonly FakeRoute[]) {
    for (const route of routes) {
      if ("subscriptionDescriptor" in route) {
        this.#descriptorSubscriptionRoutes.set(operationName(route.subscriptionDescriptor), route);
      } else {
        this.#descriptorRoutes.set(operationName(route.descriptor), route);
      }
    }
  }

  async callDescriptor<Method extends DescMethod>(
    descriptor: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcDedicatedCallOptions,
  ): Promise<MessageShape<Method["output"]>> {
    validate(descriptor.input, request);
    this.descriptorCalls.push(
      options === undefined ? { descriptor, request } : { descriptor, request, options },
    );
    const operation = operationName(descriptor);
    const route = this.#descriptorRoutes.get(operation);
    if (route === undefined) {
      throw new Error(`Missing fake descriptor route: ${operation}`);
    }
    if (route.error !== undefined) {
      throw route.error;
    }
    const callIndex = this.#callCounts.get(operation) ?? 0;
    this.#callCounts.set(operation, callIndex + 1);
    const result = await (route.resultFactory?.(request, callIndex) ?? route.result);
    if (result === undefined) {
      throw new Error(`Missing fake descriptor result: ${operation}`);
    }
    const payload = encode(route.descriptor.output, result);
    return decode<Method["output"]>(descriptor.output, payload);
  }

  subscribeDescriptor<
    Method extends DescMethod,
    EventDescriptor extends DescMessage,
    CompletionDescriptor extends DescMessage,
  >(input: DescriptorSubscriptionInput<Method, EventDescriptor, CompletionDescriptor>): RpcSubscription {
    const { method: descriptor, request, eventDescriptor, completionDescriptor, onStart, handler } = input;
    const operation = operationName(descriptor);
    const route = this.#descriptorSubscriptionRoutes.get(operation);
    if (route === undefined) throw new Error(`Missing fake descriptor subscription route: ${operation}`);
    this.descriptorSubscriptionStarts.push({ descriptor, request });
    const entry = {
      descriptor,
      open: () => {
        try {
          onStart(
            decode<Method["output"]>(
              descriptor.output,
              encode(route.subscriptionDescriptor.output, route.startResult),
            ),
          );
          handler.onOpen?.();
        } catch (cause) {
          handler.onError(cause instanceof Error ? cause : new Error("Descriptor subscription failed."));
        }
      },
      event: (payload: Uint8Array) => {
        try {
          handler.onEvent(decode(eventDescriptor, payload));
        } catch (cause) {
          handler.onError(cause instanceof Error ? cause : new Error("Descriptor event failed."));
        }
      },
      complete: (payload: Uint8Array) => {
        handler.onComplete(decode(completionDescriptor, payload));
      },
      fail: handler.onError,
    };
    this.#descriptorSubscribers.push(entry);
    return {
      close: () => {
        this.#descriptorSubscribers = this.#descriptorSubscribers.filter(
          (subscriber) => subscriber !== entry,
        );
      },
    };
  }

  async callDescriptorAttachedProject<Method extends DescMethod>(
    input: AttachedProjectDescriptorCall<Method>,
    options?: RpcDedicatedCallOptions,
  ): Promise<Readonly<{ result: MessageShape<Method["output"]>; attachment: ProjectAttachment }>> {
    const { projectID, selector, method, createRequest } = input;
    const attachment = this.#projectAttachment(projectID, selector);
    const request = createRequest(attachment);
    this.attachedProjectDescriptorCalls.push(
      options === undefined
        ? { projectID, selector, descriptor: method, request }
        : { projectID, selector, descriptor: method, request, options },
    );
    return {
      result: await this.callDescriptor(method, request, options),
      attachment,
    };
  }

  async callDescriptorAttachedSession<Method extends DescMethod>(
    target: Readonly<{ sessionID: string; projectID?: string }>,
    method: Method,
    request: MessageShape<Method["input"]>,
    options?: RpcDedicatedCallOptions,
  ): Promise<MessageShape<Method["output"]>> {
    this.attachedSessionCalls.push({ sessionID: target.sessionID, method: operationName(method) });
    return this.callDescriptor(method, request, options);
  }

  #projectAttachment(
    projectID: string,
    selector: Readonly<{ workspaceID: string } | { workspaceRoot: string }>,
  ): ProjectAttachment {
    return {
      projectID,
      workspaceID: "workspaceID" in selector ? selector.workspaceID : "workspace-1",
      workspaceRoot: "workspaceRoot" in selector ? selector.workspaceRoot : "/workspace",
      workspaceSelection:
        "workspaceID" in selector
          ? { kind: "workspaceID", workspaceID: selector.workspaceID }
          : {
              kind: "workspaceRoot",
              requestedRoot: selector.workspaceRoot,
              canonicalRoot: selector.workspaceRoot,
            },
    };
  }

  async runRuntimeOwner<Result>(
    sessionID: string,
    options: RuntimeOwnerOptions,
    run: (context: RuntimeOwnerContext) => Promise<Result>,
  ): Promise<Result> {
    this.runtimeOwnerRuns += 1;
    if (!options.createIfMissing && this.#runtimeOwner === null) {
      throw new Error("Runtime owner connection is unavailable.");
    }
    if (this.#runtimeOwner !== null && this.#runtimeOwner.sessionID !== sessionID) {
      throw new Error("Runtime owner connection is bound to another Session.");
    }
    this.#runtimeOwner ??= {
      projectID: "project-1",
      workspaceID: "workspace-1",
      workspaceRoot: "/workspace",
      sessionID,
    };
    const context: RuntimeOwnerContext = {
      attachment: this.#runtimeOwner,
      callDescriptor: async (descriptor, request) => this.callDescriptor(descriptor, request),
      poison: () => {
        this.#runtimeOwner = null;
      },
    };
    return run(context).then((result) => {
      if (options.closeAfter) {
        this.#runtimeOwner = null;
      }
      return result;
    });
  }

  openDescriptor(descriptor: DescMethod): void {
    for (const subscriber of this.#descriptorSubscribersFor(descriptor)) subscriber.open();
  }

  get descriptorSubscriptions(): readonly DescMethod[] {
    return this.#descriptorSubscribers.map(({ descriptor }) => descriptor);
  }

  emitDescriptor<EventMethod extends DescMethod>(
    subscriptionDescriptor: DescMethod,
    eventDescriptor: EventMethod,
    message: MessageShape<EventMethod["input"]>,
  ): void {
    const payload = encode(eventDescriptor.input, message);
    for (const subscriber of this.#descriptorSubscribersFor(subscriptionDescriptor)) {
      subscriber.event(payload);
    }
  }

  emitDescriptorBytes(subscriptionDescriptor: DescMethod, payload: Uint8Array): void {
    for (const subscriber of this.#descriptorSubscribersFor(subscriptionDescriptor)) {
      subscriber.event(payload);
    }
  }

  completeDescriptor<CompletionMethod extends DescMethod>(
    subscriptionDescriptor: DescMethod,
    completionDescriptor: CompletionMethod,
    message: MessageShape<CompletionMethod["input"]>,
  ): void {
    const payload = encode(completionDescriptor.input, message);
    for (const subscriber of this.#descriptorSubscribersFor(subscriptionDescriptor)) {
      subscriber.complete(payload);
    }
  }

  failDescriptor(descriptor: DescMethod, error: Error): void {
    for (const subscriber of this.#descriptorSubscribersFor(descriptor)) subscriber.fail(error);
  }

  #descriptorSubscribersFor(descriptor: DescMethod) {
    const operation = operationName(descriptor);
    return this.#descriptorSubscribers.filter(
      (subscriber) => operationName(subscriber.descriptor) === operation,
    );
  }
}
