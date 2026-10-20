import { ComposerIcon } from "./ComposerIcon";
import type { ChatExecutionTarget } from "@/api";
import { useOwnedSidebarRoots } from "@/app-facade";
import { InteractiveChip } from "@/ui";
import type { useWorktreeList } from "./useWorktreeList";
import { worktreeTarget } from "./worktreePresentation";

export function WorktreeControl({
  projectID,
  sessionID,
  target,
  query,
  openSession,
}: Readonly<{
  projectID: string;
  sessionID: string;
  openSession(sessionID: string): void | Promise<void>;
  target: ChatExecutionTarget | null;
  query: ReturnType<typeof useWorktreeList>;
}>) {
  const roots = useOwnedSidebarRoots();
  const label = target === null ? null : worktreeTarget(target, query.data);
  if (label?.title === undefined) return null;
  return (
    <div className="chat-composer-worktree">
      <InteractiveChip
        variant="ghost"
        className="flex max-w-full items-center gap-[var(--space-2)] text-sm"
        onClick={(event) => {
          const returnFocus = event.currentTarget;
          const handle = roots.toggle({ kind: "worktree", projectID, sessionID, page: "list", openSession });
          void handle?.lifecycle.then((outcome) => {
            if (outcome === "closed" && returnFocus.isConnected) returnFocus.focus();
          });
        }}
      >
        {label.warning ? (
          <ComposerIcon kind="warning" className="text-[var(--color-warning)]" />
        ) : (
          <ComposerIcon kind="worktree" />
        )}
        <span className="truncate">{label.title}</span>
      </InteractiveChip>
    </div>
  );
}
