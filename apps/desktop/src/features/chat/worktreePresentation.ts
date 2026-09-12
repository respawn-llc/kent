import type { ChatExecutionTarget, WorktreeListEntry, WorktreeList } from "@/api";

export function worktreeRow(entry: WorktreeListEntry, workspaceName: string) {
  const topology = entry.topology?.topology;
  const projection = entry.projection;
  if (topology === undefined || projection === undefined) throw new Error("Worktree topology is required");
  const { title, git, indicator } = rowFacts(topology, workspaceName, projection.fallbackIdentity);
  if (title === undefined) throw new Error("Worktree identity is required");
  const ref = git?.branchName ?? git?.headObject;
  return {
    key: projection.selector,
    title,
    ref: ref === title ? undefined : ref,
    indicator: git?.pathAvailable === false ? ("missing" as const) : indicator,
    selected: projection.isCurrent,
    switch: projection.switch,
    delete: projection.deletePreview,
  };
}

function rowFacts(
  topology: NonNullable<WorktreeListEntry["topology"]>["topology"],
  workspaceName: string,
  fallbackIdentity: string | undefined,
) {
  switch (topology.case) {
    case "mainWorkspace":
      return { title: workspaceName, git: topology.value.git, indicator: undefined };
    case "external":
      return {
        title: topology.value.git?.branchName ?? fallbackIdentity,
        git: topology.value.git,
        indicator: "external" as const,
      };
    case "registered":
      return { title: topology.value.kent?.displayName, git: topology.value.git, indicator: undefined };
    case "missing":
      return { title: topology.value.kent?.displayName, git: undefined, indicator: "missing" as const };
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
