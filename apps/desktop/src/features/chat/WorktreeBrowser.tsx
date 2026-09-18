import { ArrowRightLeft, Plus, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactElement } from "react";
import type * as Atom from "effect/unstable/reactivity/Atom";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { errorMessage, type WorktreeDeletePreviewOperation, type WorktreeSwitch } from "@/api";
import {
  useAppServices,
  useStatusController,
  usePublishSidebarHeaderAction,
  type SidebarPageNavigator,
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
import { createWorktreeDelete, useWorktreeDelete } from "./WorktreeDelete";
import { WorktreeDeleteButton } from "./WorktreeDeleteButton";

export function WorktreeBrowser({
  sessionID,
  onCreate,
  onSwitch,
  navigator,
  switchPending = false,
  refreshOpenWorktreeList,
}: Readonly<{
  sessionID: string;
  onCreate(): void;
  onSwitch(operation: WorktreeSwitch): void;
  navigator: SidebarPageNavigator;
  switchPending?: boolean;
  refreshOpenWorktreeList(sessionID: string): void;
}>) {
  const { t } = useTranslation();
  const query = useWorktreeList(sessionID);
  const [selected, setSelected] = useState<WorktreeDeletePreviewOperation | null>(null);
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
  const content = (deletion: DeletionPresentation | null) => {
    const deleteButton = (operation: WorktreeDeletePreviewOperation) => (
      <WorktreeDeleteButton
        open={selected?.selector === operation.selector}
        onOpenChange={(open) => {
          setSelected(open ? operation : null);
        }}
        preview={deletion?.preview.data}
        pending={deletion?.pending ?? false}
        confirm={(choice) => deletion?.confirm(choice)}
      />
    );
    if (query.isError || deletion?.preview.isError)
      return (
        <ErrorState
          body={errorMessage(query.isError ? query.error : deletion?.preview.error)}
          title={t("states.error")}
          retryLabel={t("app.retry")}
          onRetry={() => {
            if (query.isError) query.refresh();
            else deletion?.retry(undefined);
          }}
        />
      );
    return (
      <div
        className="grid min-w-0 gap-[var(--space-2)]"
        ref={rows}
        onFocusCapture={(event) => {
          lastFocused.current = event.target;
        }}
      >
        {query.data === undefined ? <LoadingState fullPage={false} appearanceDelayMs={0} /> : null}
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
                  {row.delete === undefined ? null : deleteButton(row.delete)}
                </>
              }
            />
          );
        })}
      </div>
    );
  };
  return selected === null ? (
    content(null)
  ) : (
    <WorktreeDeletionSelection
      key={selected.selector}
      sessionID={sessionID}
      selector={selected.selector}
      close={() => {
        setSelected((current) => (current === selected ? null : current));
      }}
      refreshOpenWorktreeList={refreshOpenWorktreeList}
      content={content}
    />
  );
}

type DeletionPresentation = Readonly<{
  preview: Atom.Type<ReturnType<typeof createWorktreeDelete>["preview"]>;
  pending: boolean;
}> &
  ReturnType<typeof useWorktreeDelete>;

function WorktreeDeletionSelection({
  content,
  ...selection
}: Pick<
  Parameters<typeof createWorktreeDelete>[0],
  "sessionID" | "selector" | "close" | "refreshOpenWorktreeList"
> &
  Readonly<{ content(state: DeletionPresentation): ReactElement }>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const [model] = useState(() => createWorktreeDelete({ ...selection, api, client, push, t }));
  const actions = useWorktreeDelete(model);
  const preview = useAtomValue(model.preview);
  const deletion = useAtomValue(model.deletion);
  return content({ ...actions, preview, pending: deletion.isPending });
}
