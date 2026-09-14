import {
  create,
  decode,
  encode,
  type DescMessage,
  type DescMethod,
  type Message,
} from "@app/server-api-contract";
import {
  CreateService,
  CreateTargetService,
  DeletePreviewService,
  DeletePreviewResultSchema,
  EnterResultSchema,
  LeaveResultSchema,
  TransitionService,
  DeleteResultSchema,
  DirtyStateKind,
  type CreateTargetResolutionKind,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { ApiClient } from "@/api/composition";
import { WorktreeError } from "@/api";
import {
  FakeRpcTransport,
  fakeDescriptorResult,
  worktreeBrowserFixtureEntry,
  worktreeBrowserFixtureRoute,
  worktreeQueryFixtureRoutes,
} from "./api";
import { createFixtureChat, deferred, hydration, mainViewRead, target, transcriptPage } from "./chat";
import { worktreeAcknowledgementFixture, worktreeDeleteSuccessFixture } from "./worktreeResponses";

export type FixtureRequestKind = "list" | "resolve" | "preview" | "create" | "switch" | "delete";
export type WorktreeShowcaseConfig = Readonly<{
  topology: "registered" | "external" | "missing" | "detached" | "empty";
  cleanliness: "clean" | "dirty" | "unknown";
  createKind: CreateTargetResolutionKind;
  suggestion: string;
  deferred: readonly FixtureRequestKind[];
  scheduledDelete: boolean;
  retainCleanup: boolean;
}>;
export type PendingFixtureRequest = Readonly<{
  id: string;
  kind: FixtureRequestKind;
  input: string;
  succeed(): void;
  fail(error: Error): void;
}>;

export function createWorktreeShowcaseFixture(
  config: WorktreeShowcaseConfig,
  enqueue: (request: PendingFixtureRequest) => void,
) {
  let present = config.topology !== "empty";
  let selected = false;
  let sequence = 0;
  let read = mainViewRead();
  const runtime = createFixtureChat(
    async () => read,
    async () => transcriptPage(null),
  );
  const options = {
    detached: config.topology === "detached",
    createKind: config.createKind,
    cleanliness:
      config.cleanliness === "clean"
        ? { kind: DirtyStateKind.DIRTY_STATE_CLEAN }
        : config.cleanliness === "dirty"
          ? { kind: DirtyStateKind.DIRTY_STATE_DIRTY, dirtyFileCount: 4 }
          : { kind: DirtyStateKind.DIRTY_STATE_UNKNOWN, unknownCause: "Fixture Git status unavailable" },
  };
  const gate = async (kind: FixtureRequestKind, input: string) => {
    if (!config.deferred.includes(kind)) return;
    const pending = deferred<undefined>();
    enqueue({
      id: crypto.randomUUID(),
      kind,
      input,
      succeed: () => {
        pending.resolve(undefined);
      },
      fail: (error) => {
        if (error instanceof WorktreeError && error.detail.kind === "delete_precondition") {
          options.cleanliness = { kind: DirtyStateKind.DIRTY_STATE_DIRTY, dirtyFileCount: 5 };
        }
        pending.reject(error);
      },
    });
    await pending.promise;
  };
  const rows = () => {
    const main = worktreeBrowserFixtureEntry("mainWorkspace", !selected);
    if (!present) return [main];
    return [
      main,
      worktreeBrowserFixtureEntry(
        config.topology === "missing"
          ? "missing"
          : config.topology === "external"
            ? "external"
            : "registered",
        selected,
        { detached: config.topology === "detached" },
      ),
    ];
  };
  const list = worktreeBrowserFixtureRoute();
  const routes = worktreeQueryFixtureRoutes(options).map((route) => {
    if (!("descriptor" in route)) return route;
    const descriptor = route.descriptor;
    const kind = requestKind(descriptor);
    if (kind === undefined) return route;
    return {
      descriptor,
      resultFactory: async (request: Message, index: number) => {
        const input =
          kind === "resolve"
            ? decode(CreateTargetService.method.resolve.input, encode<DescMessage>(descriptor.input, request))
                .target
            : kind;
        await gate(kind, input);
        if (kind === "switch")
          return create(EnterResultSchema, {
            outcome: { case: "success", value: worktreeAcknowledgementFixture() },
          });
        if (kind === "delete") {
          if (!config.scheduledDelete) present = false;
          return create(DeleteResultSchema, {
            outcome: {
              case: "success",
              value: worktreeDeleteSuccessFixture(
                config.retainCleanup
                  ? {
                      retainedBranch: config.topology === "detached" ? undefined : "feature",
                      diagnostic: "Fixture branch is retained",
                      leftoverRoot: "/repo/feature",
                    }
                  : {},
              ),
            },
          });
        }
        if (kind === "create") present = true;
        if (kind === "preview" && config.topology === "missing") {
          return create(DeletePreviewResultSchema, {
            outcome: {
              case: "success",
              value: {
                worktree: worktreeBrowserFixtureEntry("missing", false).topology,
                deletionSelector: "worktree-1",
                cleanliness: { kind: DirtyStateKind.DIRTY_STATE_CLEAN },
              },
            },
          });
        }
        return fakeDescriptorResult(route, request, index);
      },
    };
  });
  const transport = new FakeRpcTransport([
    {
      descriptor: list.descriptor,
      resultFactory: async () => {
        await gate("list", target.sessionID);
        return worktreeBrowserFixtureRoute(
          rows(),
          config.suggestion.length === 0 ? undefined : config.suggestion,
        ).result;
      },
    },
    ...routes,
    {
      descriptor: TransitionService.method.leave,
      resultFactory: async () => {
        await gate("switch", "main");
        return create(LeaveResultSchema, {
          outcome: { case: "success", value: worktreeAcknowledgementFixture() },
        });
      },
    },
  ]);
  return {
    api: new ApiClient(transport),
    transport,
    listReadCount: () =>
      transport.descriptorCalls.filter((call) => call.descriptor === list.descriptor).length,
    runtime,
    hydrate: () => {
      runtime.open();
      runtime.emit({ sequence: ++sequence, kind: "hydration", payload: hydration() });
    },
    outcome: (state: "completed" | "failed") => {
      if (state === "completed" && config.scheduledDelete) present = false;
      runtime.emit({
        sequence: ++sequence,
        kind: "worktree_transition_outcome",
        payload: {
          OperationID: crypto.randomUUID(),
          Transition: "delete",
          State: state,
          Failure:
            state === "failed"
              ? { Code: "fixture_failure", Detail: "Fixture later transition failed" }
              : null,
        },
      });
    },
    applyTarget: (worktree: boolean) => {
      selected = worktree;
      const execution = {
        ...read.mainView.executionTarget,
        worktree: worktree
          ? {
              ID: "worktree-1",
              Name: "Feature",
              Root: "/repo/feature",
              Availability: config.topology === "missing" ? "missing" : "available",
            }
          : null,
      };
      read = {
        ...read,
        mainView: {
          ...read.mainView,
          executionTarget: execution,
          version: { ...read.mainView.version, sequence: sequence + 1 },
        },
      };
      runtime.emit({
        sequence: ++sequence,
        kind: "session_identity",
        payload: {
          SessionID: target.sessionID,
          SessionName: read.mainView.sessionName,
          ConversationFreshness: 1,
          ExecutionTarget: {
            WorkspaceID: execution.workspaceID,
            WorkspaceName: execution.workspaceName,
            WorkspaceRoot: execution.workspaceRoot,
            WorkspaceAvailability: execution.workspaceAvailability,
            Worktree: execution.worktree,
            CwdRelpath: execution.cwdRelpath,
            EffectiveWorkdir: execution.effectiveWorkdir,
          },
        },
      });
    },
    prompt: (kind: "question" | "approval") => {
      runtime.emit({
        sequence: ++sequence,
        kind: "prompt",
        payload: {
          Kind: kind,
          State: "pending",
          ToolCallID: crypto.randomUUID(),
          SessionID: target.sessionID,
          StepID: crypto.randomUUID(),
          Question: "Keep this pending prompt unchanged while using Worktree.",
          CreatedAt: new Date().toISOString(),
          Suggestions: [],
          ApprovalOptions: [],
          AccessTargets: [],
        },
      });
    },
  };
}

function requestKind(descriptor: DescMethod): FixtureRequestKind | undefined {
  if (descriptor === CreateService.method.create) return "create";
  if (descriptor === CreateTargetService.method.resolve) return "resolve";
  if (descriptor === DeletePreviewService.method.get) return "preview";
  if (descriptor === TransitionService.method.enter) return "switch";
  if (descriptor === TransitionService.method.delete) return "delete";
  return undefined;
}
