import { AlertTriangle, GitBranch } from "lucide-react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import { useChatExecutionTarget, useOwnedSidebarRoots } from "@/app-facade";
import { Button, ErrorState, Spinner } from "@/ui";
import { useWorktreeList } from "./useWorktreeList";
import { worktreeTarget } from "./worktreePresentation";

export function WorktreeControl({
  sessionID,
}: Readonly<{
  sessionID: string;
}>) {
  const { t } = useTranslation();
  const roots = useOwnedSidebarRoots();
  const target = useChatExecutionTarget();
  const query = useWorktreeList(sessionID, target);
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
      {label?.title === undefined && query.isError ? (
        <ErrorState
          body={errorMessage(query.error)}
          fullPage={false}
          title={t("states.error")}
          retryLabel={t("app.retry")}
          onRetry={query.refresh}
        />
      ) : null}
    </div>
  );
}
