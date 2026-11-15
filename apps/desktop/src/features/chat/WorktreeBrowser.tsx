import { ArrowRightLeft, MessageCircle, Plus, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence } from "motion/react";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  DirtyStateKind,
  hasDeletableWorktreeBranch,
  errorMessage,
  type WorktreeDeletePreviewOperation,
  type WorktreeSwitch,
  type WorktreeScope,
} from "@/api";
import { CopyableValue } from "@/shared/copyable-value";
import {
  useAppServices,
  useNativeChatOpening,
  usePublishSidebarHeaderAction,
  useStatusController,
  type SidebarPageNavigator,
  type ChatWorktreeBinding,
} from "@/app-facade";
import {
  ActionableListRow,
  AnimatedReveal,
  Badge,
  EmptyState,
  ErrorState,
  IconTooltipButton,
  LoadingState,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { useWorktreeList } from "./useWorktreeList";
import { worktreeRow } from "./worktreePresentation";
import { createWorktreeDelete, useWorktreeDelete } from "./WorktreeDelete";
import { WorktreeDeleteButton } from "./WorktreeDeleteButton";

export function WorktreeBrowser(props: Parameters<typeof WorktreeBrowserDestination>[0]) {
  const scope = useAtomValue(props.chat.scope);
  if (scope === null) return <LoadingState surface="plain" appearanceDelayMs={0} />;
  return <WorktreeBrowserDestination key={JSON.stringify(scope)} {...props} />;
}

function WorktreeBrowserDestination({
  projectID,
  onCreate,
  onSwitch,
  navigator,
  switchPending,
  refreshOpenWorktreeList,
  onOpenSession,
  chat,
}: Readonly<{
  projectID: string;
  chat: ChatWorktreeBinding;
  onOpenSession(sessionID: string): void | Promise<void>;
  onCreate(): void;
  onSwitch(operation: WorktreeSwitch): void;
  navigator: SidebarPageNavigator;
  switchPending?: boolean;
  refreshOpenWorktreeList(scope: WorktreeScope): void | Promise<void>;
}>) {
  const { t } = useTranslation();
  const { nativeBridge, api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const nativeWindowsAvailable = nativeBridge.capabilities.dialogWindows;
  const { managementScope, sessionID, workspace } = useBrowserTarget(chat);
  const query = useWorktreeList(managementScope);
  const [deletionModel] = useState(() =>
    createWorktreeDelete({
      api,
      client,
      scope: requireWorktreeScope(managementScope),
      refreshOpenWorktreeList,
      push,
      t,
    }),
  );
  const deletionActions = useWorktreeDelete(deletionModel);
  const selected = useAtomValue(deletionModel.selector);
  const previewQuery = useAtomValue(deletionModel.preview);
  const deletion = useAtomValue(deletionModel.deletion);
  const busySelectors = useAtomValue(deletionModel.busySelectors);
  useEffect(() => {
    if (query.isError && query.data !== undefined) {
      push({
        id: crypto.randomUUID(),
        tone: "danger",
        title: t("chat.worktree.title"),
        body: errorMessage(query.error),
      });
    }
  }, [query.isError, query.error, query.data, push, t]);
  const [includeBranch, setIncludeBranch] = useState(true);
  const rows = useRef<HTMLDivElement>(null);
  const headerElement = useRef<HTMLDivElement | null>(null);
  const lastFocused = useRef<HTMLElement | null>(null);
  const entered = useRef(false);
  const focusFirst = useCallback(() => {
    const action =
      rows.current?.querySelector<HTMLButtonElement>("button:not(:disabled)") ??
      headerElement.current?.querySelector<HTMLButtonElement>("button:not(:disabled)");
    action?.focus({ preventScroll: true });
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
  const preview = previewQuery?.data;
  const canIncludeBranch = preview !== undefined && hasDeletableWorktreeBranch(preview);
  const cleanliness = preview?.cleanliness;
  const deleteButton = (operation: Pick<WorktreeDeletePreviewOperation, "selector">) => (
    <WorktreeDeleteButton
      includeBranch={includeBranch}
      disabled={deletion.isPending}
      canIncludeBranch={canIncludeBranch}
      onIncludeBranchChange={setIncludeBranch}
      open={selected === operation.selector && previewQuery?.isSuccess === true}
      onOpenChange={(open) => {
        if (open) {
          if (!deletion.isPending) deletionActions.select(operation.selector);
        } else if (selected === operation.selector) {
          deletionActions.select(null);
        }
      }}
      cleanliness={
        cleanliness === undefined || cleanliness.kind === DirtyStateKind.DIRTY_STATE_CLEAN
          ? null
          : cleanliness
      }
      confirm={() => {
        deletionActions.confirm(
          includeBranch && canIncludeBranch ? "confirm_and_branch" : "confirm_without_branch",
        );
      }}
    />
  );
  if (query.isError && query.data === undefined)
    return (
      <ErrorState
        surface="plain"
        body={t("chat.worktree.readFailed")}
        title={t("states.error")}
        retryLabel={t("app.retry")}
        onRetry={() => {
          query.refresh();
        }}
      />
    );
  if (query.data === undefined) return <LoadingState surface="plain" appearanceDelayMs={0} />;
  const { target, worktrees } = query.data;
  const workspaceLabel = target?.workspaceName ?? workspace?.name;
  if (workspaceLabel === undefined) throw new Error("Workspace name is required");
  return (
    <div
      className="grid min-w-0 gap-[var(--space-2)]"
      ref={rows}
      onFocusCapture={(event) => {
        lastFocused.current = event.target;
      }}
    >
      {worktrees.length === 0 ? (
        <EmptyState fullPage={false} title={t("chat.worktree.empty")} body={null} />
      ) : null}
      <AnimatePresence initial={false}>
        {worktrees.map((entry) => {
          const row = worktreeRow(entry, workspaceLabel, workspace?.selector);
          const deletionOperation = row.delete;
          const switchOperation = row.switch;
          const localSwitch = row.localSwitch;
          const deleting = deletionOperation !== undefined && busySelectors.has(deletionOperation.selector);
          return (
            <AnimatedReveal
              key={row.key}
              enter={false}
              variant="collapse-center"
              className="min-w-0 overflow-hidden"
            >
              <ActionableListRow
                selected={row.selected}
                layout="stacked"
                subtitle={
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="min-w-0">
                          <CopyableValue
                            accessibleLabel={t("chat.worktree.copyPath", { path: row.path })}
                            clipboardValue={row.path}
                            copySucceeded={{ id: "worktree-copy-path", title: t("chat.worktree.pathCopied") }}
                            copyFailed={{ id: "worktree-copy-path", title: t("chat.worktree.copyFailed") }}
                            className="block w-full font-mono"
                            size="xs"
                            unpadded
                          >
                            <span className="block truncate">{row.ref ?? row.path}</span>
                          </CopyableValue>
                        </div>
                      </TooltipTrigger>
                      <TooltipContent>{t("chat.worktree.copyPath", { path: row.path })}</TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                }
                selectionControl={
                  <div className="flex min-w-0 flex-1 items-center gap-[var(--space-1)]">
                    <div className="min-w-0 truncate text-sm font-medium">{row.title}</div>
                    {row.selected && (
                      <Badge className="shrink-0" size="compact" tone="info">
                        {t("chat.worktree.current")}
                      </Badge>
                    )}
                    {row.badges.map(({ label, tone }) => (
                      <Badge key={label} className="shrink-0" size="compact" tone={tone}>
                        {t(`chat.worktree.${label}`)}
                      </Badge>
                    ))}
                  </div>
                }
                actions={
                  deleting ? (
                    <Spinner size="sm" />
                  ) : (
                    <>
                      {row.originSession === undefined ? null : (
                        <WorktreeOriginSessionAction
                          currentSessionID={sessionID}
                          projectID={projectID}
                          nativeWindowsAvailable={nativeWindowsAvailable}
                          onOpenSession={onOpenSession}
                          originSessionID={row.originSession.id}
                          originSessionLabel={row.originSession.label}
                        />
                      )}
                      {switchOperation === undefined ? null : (
                        <IconTooltipButton
                          size="icon-sm"
                          label={t(switchPending ? "chat.worktree.switchPending" : "chat.worktree.switch")}
                          disabled={switchPending}
                          loading={switchPending}
                          onClick={() => {
                            onSwitch(switchOperation);
                          }}
                        >
                          <ArrowRightLeft size={16} />
                        </IconTooltipButton>
                      )}
                      {workspace === undefined || localSwitch === undefined ? null : (
                        <IconTooltipButton
                          size="icon-sm"
                          label={t("chat.worktree.switch")}
                          onClick={() => {
                            workspace.select(localSwitch.selector);
                          }}
                        >
                          <ArrowRightLeft size={16} />
                        </IconTooltipButton>
                      )}
                      {deletionOperation === undefined ? null : deleteButton(deletionOperation)}
                    </>
                  )
                }
              />
            </AnimatedReveal>
          );
        })}
      </AnimatePresence>
    </div>
  );
}

function requireWorktreeScope(scope: WorktreeScope | null): WorktreeScope {
  if (scope === null) throw new Error("Mounted Worktree inventory requires a scope");
  return scope;
}

function useBrowserTarget(chat: ChatWorktreeBinding) {
  const managementScope = useAtomValue(chat.scope);
  const selector = useAtomValue(chat.selector);
  const workspaceName = useAtomValue(chat.workspaceName);
  const sessionID = managementScope?.kind === "session" ? managementScope.sessionID : null;
  const workspace =
    managementScope?.kind === "workspace" && workspaceName !== null
      ? { name: workspaceName, selector, select: chat.select }
      : undefined;
  return { managementScope, sessionID, workspace };
}

function WorktreeOriginSessionAction({
  currentSessionID,
  projectID,
  nativeWindowsAvailable,
  onOpenSession,
  originSessionID,
  originSessionLabel,
}: Readonly<{
  currentSessionID: string | null;
  projectID: string;
  nativeWindowsAvailable: boolean;
  onOpenSession(sessionID: string): void | Promise<void>;
  originSessionID: string;
  originSessionLabel: string;
}>) {
  const { t } = useTranslation();
  const nativeChat = useNativeChatOpening();
  const pending = nativeChat.pendingSessionID === originSessionID;
  const isCurrentSession = originSessionID === currentSessionID;
  const label = t(
    isCurrentSession ? "chat.worktree.currentOriginSession" : "chat.worktree.openOriginSession",
    { sessionID: originSessionLabel },
  );
  const tooltip = nativeWindowsAvailable ? t("app.shiftClickToOpenInNewWindow", { action: label }) : label;
  return (
    <IconTooltipButton
      size="icon-sm"
      label={label}
      tooltip={tooltip}
      loading={pending}
      disabled={pending}
      className={isCurrentSession ? "text-[var(--color-primary)]" : undefined}
      onClick={(event) => {
        if (!event.shiftKey || !nativeWindowsAvailable) {
          if (isCurrentSession) return;
          void onOpenSession(originSessionID);
          return;
        }
        nativeChat.open({ projectID, sessionID: originSessionID });
      }}
    >
      <MessageCircle size={16} />
    </IconTooltipButton>
  );
}
