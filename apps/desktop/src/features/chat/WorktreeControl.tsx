import { ComposerIcon } from "./ComposerIcon";
import type { ChatExecutionTarget } from "@/api";
import { useOwnedSidebarRoots, type ChatWorktreeBinding } from "@/app-facade";
import { useAtomValue } from "@effect/atom-react";
import { InteractiveChip } from "@/ui";
import type { useWorktreeList } from "./useWorktreeList";
import { worktreeTarget } from "./worktreePresentation";
import { worktreeRow } from "./worktreePresentation";

export function WorktreeControl({
  projectID,
  sessionID,
  target,
  query,
  openSession,
  chat,
}: Readonly<{
  projectID: string;
  sessionID: string | null;
  chat: ChatWorktreeBinding;
  openSession(sessionID: string): void | Promise<void>;
  target: ChatExecutionTarget | null;
  query: ReturnType<typeof useWorktreeList>;
}>) {
  const roots = useOwnedSidebarRoots();
  const selector = useAtomValue(chat.selector);
  const workspaceName = useAtomValue(chat.workspaceName);
  const selected =
    selector === null
      ? undefined
      : query.data?.worktrees.find((entry) => entry.projection.selector === selector);
  const label =
    target === null
      ? {
          title:
            selected === undefined || workspaceName === null
              ? (selector ?? workspaceName)
              : worktreeRow(selected, workspaceName).title,
          warning: false,
        }
      : worktreeTarget(target, query.data);
  if (label.title === undefined || label.title === null) return null;
  return (
    <div className="chat-composer-worktree">
      <InteractiveChip
        variant="ghost"
        className="flex max-w-full items-center gap-[var(--space-2)] text-sm"
        onClick={(event) => {
          const returnFocus = event.currentTarget;
          const handle = roots.toggle({
            kind: "worktree",
            projectID,
            sessionID,
            chat,
            page: "list",
            openSession,
          });
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
