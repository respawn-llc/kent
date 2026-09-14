import { ArrowRightLeft, Plus, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage, type WorktreeDeletePreviewOperation, type WorktreeSwitch } from "@/api";
import { usePublishSidebarHeaderAction, type SidebarPageNavigator } from "@/app-facade";
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
  onCreate,
  onSwitch,
  navigator,
  switchPending = false,
  renderDelete,
}: Readonly<{
  sessionID: string;
  onCreate(): void;
  onSwitch(operation: WorktreeSwitch): void;
  navigator: SidebarPageNavigator;
  switchPending?: boolean;
  renderDelete(operation: WorktreeDeletePreviewOperation): ReactNode;
}>) {
  const { t } = useTranslation();
  const query = useWorktreeList(sessionID);
  const rows = useRef<HTMLDivElement>(null);
  const headerElement = useRef<HTMLDivElement | null>(null);
  const lastFocused = useRef<HTMLElement | null>(null);
  const entered = useRef(false);
  const focusFirst = useCallback(() => {
    const action =
      rows.current?.querySelector<HTMLButtonElement>("button:not(:disabled)") ??
      headerElement.current?.querySelector<HTMLButtonElement>("button:not(:disabled)");
    action?.focus();
  }, []);
  const enter = useCallback(
    (header: HTMLDivElement | null) => {
      headerElement.current = header;
      if (header === null || entered.current) return;
      entered.current = true;
      focusFirst();
    },
    [focusFirst],
  );
  useEffect(() => {
    if (
      lastFocused.current !== null &&
      !lastFocused.current.isConnected &&
      document.activeElement === document.body
    )
      focusFirst();
  }, [query.data, focusFirst]);
  const { refresh, isFetching } = query;
  const header = useMemo(
    () => (
      <div className="flex items-center gap-[var(--space-1)]" ref={enter}>
        <IconTooltipButton label={t("chat.worktree.create")} onClick={onCreate}>
          <Plus size={16} />
        </IconTooltipButton>
        <IconTooltipButton label={t("chat.worktree.refresh")} onClick={refresh}>
          {isFetching ? <Spinner size="sm" /> : <RefreshCw size={16} />}
        </IconTooltipButton>
      </div>
    ),
    [enter, isFetching, onCreate, refresh, t],
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
    <div
      className="grid min-w-0 gap-[var(--space-2)]"
      ref={rows}
      onFocusCapture={(event) => {
        lastFocused.current = event.target;
      }}
    >
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
                    tone={row.indicator === "missing" ? "danger" : "warning"}
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
                    label={t(switchPending ? "chat.worktree.switchPending" : "chat.worktree.switch")}
                    disabled={switchPending}
                    onClick={() => {
                      if (row.switch !== undefined) onSwitch(row.switch);
                    }}
                  >
                    {switchPending ? <Spinner size="sm" /> : <ArrowRightLeft size={16} />}
                  </IconTooltipButton>
                )}
                {row.delete === undefined ? null : renderDelete(row.delete)}
              </>
            }
          />
        );
      })}
    </div>
  );
}
