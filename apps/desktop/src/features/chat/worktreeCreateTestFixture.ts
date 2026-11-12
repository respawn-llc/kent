import { CreateTargetResolutionKind } from "@/api";

export const resolvedCreateTarget = {
  $typeName: "kent.api.worktree.CreateTargetResolveSuccess",
  resolution: {
    $typeName: "kent.api.worktree.CreateTargetResolution",
    input: "topic",
    kind: CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
  },
} as const;
