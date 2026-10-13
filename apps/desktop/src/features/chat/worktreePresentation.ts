import type { ChatExecutionTarget, WorktreeListEntry, WorktreeList } from "@/api";

export function worktreeRow(entry: WorktreeListEntry, workspaceName: string) {
  const topology = entry.topology?.topology;
  const projection = entry.projection;
  if (topology === undefined || projection === undefined) throw new Error("Worktree topology is required");
  const facts = rowFacts(topology, workspaceName, projection.fallbackIdentity);
  const identity = worktreeIdentity(facts);
  const { git, indicator } = facts;
  const ref = git?.branchName ?? git?.headObject;
  return {
    key: projection.selector,
    ...identity,
    ref: ref === identity.title ? undefined : ref,
    indicator: git?.pathAvailable === false ? ("missing" as const) : indicator,
    selected: projection.isCurrent,
    switch: projection.switch,
    delete: projection.deletePreview,
  };
}

function worktreeIdentity({ title, git, kent }: ReturnType<typeof rowFacts>) {
  const path = git?.canonicalRoot ?? kent?.canonicalRoot;
  if (title === undefined || path === undefined) throw new Error("Worktree identity is required");
  return { title, path, originSessionID: kent?.originSessionId };
}

function rowFacts(
  topology: NonNullable<WorktreeListEntry["topology"]>["topology"],
  workspaceName: string,
  fallbackIdentity: string | undefined,
) {
  switch (topology.case) {
    case "mainWorkspace":
      return { title: workspaceName, git: topology.value.git, kent: undefined, indicator: undefined };
    case "external":
      return {
        title: topology.value.git?.branchName ?? fallbackIdentity,
        git: topology.value.git,
        kent: undefined,
        indicator: "external" as const,
      };
    case "registered":
      return {
        title: topology.value.kent?.displayName,
        git: topology.value.git,
        kent: topology.value.kent,
        indicator: undefined,
      };
    case "missing":
      return {
        title: topology.value.kent?.displayName,
        git: undefined,
        kent: topology.value.kent,
        indicator: "missing" as const,
      };
    case undefined:
      throw new Error("Worktree topology is required");
  }
}

export function worktreeTarget(target: ChatExecutionTarget, list: WorktreeList | undefined) {
  const worktree = target.worktree;
  if (worktree === null) {
    return { title: target.workspaceName, warning: target.workspaceAvailability !== "available" };
  }
  if (worktree.Availability !== "available") return { title: worktree.Name, warning: true };
  const match = list?.worktrees.find(({ topology }) => {
    const facts = topology?.topology;
    return (
      (facts?.case === "registered" || facts?.case === "missing") &&
      facts.value.kent?.worktreeId === worktree.ID
    );
  })?.topology?.topology;
  return {
    title: match?.case === "registered" ? (match.value.git?.branchName ?? worktree.Name) : undefined,
    warning: false,
  };
}
