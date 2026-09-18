import { AlertTriangle, GitBranch } from "lucide-react";
import type { ChatExecutionTarget } from "@/api";
import { useOwnedSidebarRoots } from "@/app-facade";
import { Button, Spinner } from "@/ui";
import type { useWorktreeList } from "./useWorktreeList";
import { worktreeTarget } from "./worktreePresentation";

export function WorktreeControl({
  sessionID,
  target,
  query,
}: Readonly<{
  sessionID: string;
  target: ChatExecutionTarget | null;
  query: ReturnType<typeof useWorktreeList>;
}>) {
  const roots = useOwnedSidebarRoots();
  const label = target === null ? null : worktreeTarget(target, query.data);
  return (
    <div className="min-w-0">
      <Button
        className="flex max-w-full items-center gap-[var(--space-2)] text-sm"
        onClick={(event) => {
          const returnFocus = event.currentTarget;
          const handle = roots.open({ kind: "worktree", sessionID, page: "list" });
          void handle.lifecycle.then((outcome) => {
            if (outcome === "closed" && returnFocus.isConnected) returnFocus.focus();
          });
        }}
        variant={label?.warning === true ? "warning" : "ghost"}
      >
        {label?.warning === true ? (
          <AlertTriangle className="shrink-0" size={16} />
        ) : (
          <GitBranch className="shrink-0" size={16} />
        )}
        {label?.title === undefined ? <Spinner size="sm" /> : <span className="truncate">{label.title}</span>}
      </Button>
    </div>
  );
}
