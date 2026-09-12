import { ArrowRightLeft, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import {
  usePublishSidebarHeaderAction,
  type SidebarPageNavigator,
  type WorktreeBrowserActions,
} from "@/app-facade";
import {
  ActionableListRow,
  Badge,
  EmptyState,
  ErrorState,
  IconTooltipButton,
  LoadingState,
  Spinner,
} from "@/ui";
import { useWorktreeList } from "./useWorktreeList";
import { worktreeRow } from "./worktreePresentation";

export function WorktreeBrowser({
  sessionID,
  onAction,
  navigator,
}: Readonly<{ sessionID: string; onAction: WorktreeBrowserActions; navigator: SidebarPageNavigator }>) {
  const { t } = useTranslation();
  const query = useWorktreeList(sessionID);
  const rows = useRef<HTMLDivElement>(null);
  const entered = useRef(false);
  const enter = useCallback((header: HTMLDivElement | null) => {
    if (header === null || entered.current) return;
    entered.current = true;
    const action =
      rows.current?.querySelector<HTMLButtonElement>("button:not(:disabled)") ??
      header.querySelector<HTMLButtonElement>("button:not(:disabled)");
    action?.focus();
  }, []);
  const { refresh, isFetching } = query;
  const header = useMemo(
    () => (
      <div className="flex items-center gap-[var(--space-1)]" ref={enter}>
        <IconTooltipButton
          label={t("chat.worktree.create")}
          onClick={(event) => {
            onAction({ kind: "create", sessionID, returnFocus: event.currentTarget });
          }}
        >
          <Plus size={16} />
        </IconTooltipButton>
        <IconTooltipButton label={t("chat.worktree.refresh")} onClick={refresh}>
          {isFetching ? <Spinner size="sm" /> : <RefreshCw size={16} />}
        </IconTooltipButton>
      </div>
    ),
    [enter, isFetching, onAction, refresh, sessionID, t],
  );
  usePublishSidebarHeaderAction(header);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      event.preventDefault();
      navigator.close();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [navigator]);
  return (
    <div className="grid min-w-0 gap-[var(--space-2)]" ref={rows}>
      {query.data === undefined && !query.isError ? (
        <LoadingState fullPage={false} appearanceDelayMs={0} />
      ) : null}
      {query.isError ? (
        <ErrorState
          body={errorMessage(query.error)}
          fullPage={false}
          title={t("states.error")}
          retryLabel={t("app.retry")}
          onRetry={query.refresh}
        />
      ) : null}
      {query.data?.worktrees.length === 0 ? (
        <EmptyState fullPage={false} title={t("chat.worktree.empty")} body={null} />
      ) : null}
      {query.data?.worktrees.map((entry) => {
        const target = query.data.target;
        if (target === undefined) throw new Error("Worktree list target is required");
        const row = worktreeRow(entry, target.workspaceName);
        return (
          <ActionableListRow
            key={row.key}
            selected={row.selected}
            wrapActions
            selectionControl={
              <div className="flex min-w-0 flex-[1_1_16rem] items-center gap-[var(--space-2)] px-[var(--space-2)]">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{row.title}</div>
                  {row.ref === undefined ? null : (
                    <div className="truncate font-mono text-xs text-[var(--color-muted)]">{row.ref}</div>
                  )}
                </div>
                {row.indicator === undefined ? null : (
                  <Badge
                    className="shrink-0"
                    size="compact"
                    tone={row.indicator === "missing" ? "warning" : "neutral"}
                  >
                    {t(`chat.worktree.${row.indicator}`)}
                  </Badge>
                )}
              </div>
            }
            actions={
              <>
                {row.switch === undefined ? null : (
                  <IconTooltipButton
                    label={t("chat.worktree.switch")}
                    onClick={(event) => {
                      if (row.switch !== undefined)
                        onAction({
                          kind: "switch",
                          sessionID,
                          operation: row.switch,
                          returnFocus: event.currentTarget,
                        });
                    }}
                  >
                    <ArrowRightLeft size={16} />
                  </IconTooltipButton>
                )}
                {row.delete === undefined ? null : (
                  <IconTooltipButton
                    label={t("chat.worktree.delete")}
                    onClick={(event) => {
                      if (row.delete !== undefined)
                        onAction({
                          kind: "delete",
                          sessionID,
                          operation: row.delete,
                          returnFocus: event.currentTarget,
                        });
                    }}
                  >
                    <Trash2 size={16} />
                  </IconTooltipButton>
                )}
              </>
            }
          />
        );
      })}
    </div>
  );
}
