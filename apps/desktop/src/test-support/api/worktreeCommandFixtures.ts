import { create } from "@app/server-api-contract";
import {
  SelectorService,
  TransitionService,
  DeletePreviewService,
  StatusService,
  BranchCleanupMode,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { worktreeBrowserFixtureEntry, type FakeRoute } from "./index";

export const worktreeCommandFixture = {
  operationID: "123e4567-e89b-42d3-a456-426614174000",
  selector: "feature",
  currentRoot: "/repo/feature",
  deletionSelector: "worktree-1",
  methods: {
    resolve: SelectorService.method.resolve,
    enter: TransitionService.method.enter,
    leave: TransitionService.method.leave,
    preview: DeletePreviewService.method.get,
    status: StatusService.method.get,
    delete: TransitionService.method.delete,
  },
  branchCleanup: {
    confirm: BranchCleanupMode.WORKTREE_BRANCH_CLEANUP_MODE_AUTO_IF_KENT_CREATED,
    confirm_and_branch: BranchCleanupMode.WORKTREE_BRANCH_CLEANUP_MODE_DELETE_SAFE,
  },
} as const;

export function worktreeCommandFixtureRoutes(isCurrent = false): readonly FakeRoute[] {
  const { operationID, currentRoot, deletionSelector, methods } = worktreeCommandFixture;
  const entry = worktreeBrowserFixtureEntry("registered", isCurrent);
  return [
    {
      descriptor: methods.resolve,
      result: create(methods.resolve.output, {
        outcome: { case: "success", value: { worktree: entry } },
      }),
    },
    ...[methods.enter, methods.leave].map((descriptor) => ({
      descriptor,
      result: create(descriptor.output, {
        outcome: { case: "success", value: { operationId: operationID } },
      }),
    })),
    {
      descriptor: methods.status,
      result: create(methods.status.output, {
        outcome: {
          case: "success",
          value: {
            target: {
              workspaceId: "workspace",
              workspaceName: "Workspace",
              workspaceRoot: "/repo",
              workspaceAvailability: 1,
              cwdRelpath: ".",
              effectiveWorkdir: currentRoot,
            },
            worktree: { recordedRoot: currentRoot },
          },
        },
      }),
    },
    {
      descriptor: methods.preview,
      result: create(methods.preview.output, {
        outcome: {
          case: "success",
          value: { worktree: entry.topology, deletionSelector, cleanliness: { kind: 2, dirtyFileCount: 1 } },
        },
      }),
    },
    {
      descriptor: methods.delete,
      result: create(methods.delete.output, {
        outcome: { case: "success", value: { cleanup: { kind: 1 } } },
      }),
    },
  ];
}
