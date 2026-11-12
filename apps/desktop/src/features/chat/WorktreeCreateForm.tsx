import { useAtomValue } from "@effect/atom-react";
import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { CreateTargetResolutionKind, errorMessage, type WorktreeScope } from "@/api";
import {
  replaceWorktreeListRead,
  useAppServices,
  usePublishSidebarHeaderAction,
  useStatusController,
  useTextFieldSubmitShortcut,
  worktreeListQueryOptions,
  type SidebarPageNavigator,
  type ChatWorktreeBinding,
} from "@/app-facade";
import {
  Button,
  ErrorState,
  FieldShell,
  fieldInputClassName,
  IconTooltipButton,
  LoadingState,
  Spinner,
  TextInput,
} from "@/ui";
import { createWorktreeCreate, useWorktreeCreate } from "./WorktreeCreate";
import type { createWorktreeActions } from "./WorktreeActions";

type Props = Readonly<{
  projectID: string;
  sessionID: string | null;
  scope?: WorktreeScope;
  chat?: ChatWorktreeBinding;
  openSession(sessionID: string): void | Promise<void>;
  navigator: SidebarPageNavigator;
  actions: ReturnType<typeof createWorktreeActions>;
}>;

export function WorktreeCreateForm(props: Props) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const scope = useMemo(
    () =>
      props.scope ??
      (props.sessionID === null ? null : { kind: "session" as const, sessionID: props.sessionID }),
    [props.scope, props.sessionID],
  );
  const query = useQuery(worktreeListQueryOptions(api, scope));
  const header = useMemo(
    () => (
      <IconTooltipButton
        label={t("chat.worktree.back")}
        onClick={() =>
          props.navigator.replace({
            kind: "worktree",
            page: "list",
            projectID: props.projectID,
            sessionID: props.sessionID,
            openSession: props.openSession,
            ...(props.chat === undefined ? {} : { chat: props.chat }),
          })
        }
      >
        <ArrowLeft size={16} />
      </IconTooltipButton>
    ),
    [props.navigator, props.projectID, props.sessionID, props.openSession, t],
  );
  usePublishSidebarHeaderAction(header);
  useEffect(() => {
    const back = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      event.preventDefault();
      props.navigator.replace({
        kind: "worktree",
        page: "list",
        projectID: props.projectID,
        sessionID: props.sessionID,
        openSession: props.openSession,
        ...(props.chat === undefined ? {} : { chat: props.chat }),
      });
    };
    document.addEventListener("keydown", back);
    return () => {
      document.removeEventListener("keydown", back);
    };
  }, [props.navigator, props.projectID, props.sessionID, props.openSession]);
  if (query.data !== undefined)
    return scope === null ? null : (
      <CreateFields {...props} scope={scope} suggestion={query.data.branchSuggestion} query={query} />
    );
  if (query.isError)
    return (
      <ErrorState
        surface="plain"
        title={t("states.error")}
        body={errorMessage(query.error)}
        retryLabel={t("app.retry")}
        onRetry={() => {
          if (scope !== null) void replaceWorktreeListRead(client, api, scope);
        }}
      />
    );
  return <LoadingState surface="plain" appearanceDelayMs={0} />;
}

function CreateFields({
  projectID,
  sessionID,
  openSession,
  navigator,
  actions,
  suggestion,
  query,
  scope,
  chat,
}: Props &
  Readonly<{
    suggestion: string | undefined;
    scope: WorktreeScope;
    query: UseQueryResult<Awaited<ReturnType<ReturnType<typeof useAppServices>["api"]["listWorktrees"]>>>;
  }>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const id = useId();
  const [model] = useState(() =>
    createWorktreeCreate({
      client,
      api,
      projectID,
      sessionID,
      scope,
      ...(chat === undefined ? {} : { chat }),
      openSession,
      navigator,
      suggestion,
      push,
      t,
      refreshOpenWorktreeList: actions.refreshOpenWorktreeList,
      submitSwitch: actions.submitSwitch,
    }),
  );
  const { editTarget, editBase, submit, retryResolution } = useWorktreeCreate(model);
  const state = useAtomValue(model.state);
  const resolution = useAtomValue(model.resolution);
  const switching = useAtomValue(actions.requestPending);
  const pending = state.pending || switching;
  const submitShortcut = useTextFieldSubmitShortcut({ kind: "form", available: !pending });
  const isNew =
    state.classification === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH;
  if (query.isError || resolution?.isError)
    return (
      <ErrorState
        surface="plain"
        title={t("states.error")}
        body={errorMessage(query.isError ? query.error : resolution?.error)}
        retryLabel={t("app.retry")}
        onRetry={() => {
          if (query.isError) void replaceWorktreeListRead(client, api, scope);
          else retryResolution(undefined);
        }}
      />
    );
  return (
    <form
      onKeyDown={submitShortcut}
      className="grid min-w-0 gap-[var(--space-3)]"
      onSubmit={(event) => {
        event.preventDefault();
        if (!switching) submit(undefined);
      }}
    >
      <FieldShell
        label={t("chat.worktree.targetLabel")}
        inputId={id}
        errorId={`${id}-error`}
        hintId={`${id}-hint`}
      >
        <CreateClassification
          pending={state.resolving}
          error={state.targetError}
          kind={state.classification}
        />
        <input
          id={id}
          className={fieldInputClassName}
          autoFocus
          value={state.target}
          disabled={pending}
          onChange={(event) => {
            editTarget(event.target.value);
          }}
        />
      </FieldShell>
      {isNew ? (
        <TextInput
          label={t("chat.worktree.baseLabel")}
          value={state.base}
          error={state.baseError}
          disabled={pending}
          onChange={(event) => {
            editBase(event.target.value);
          }}
        />
      ) : null}
      <Button type="submit" variant="primary" disabled={pending} loading={pending}>
        {t("chat.worktree.createSubmit")}
      </Button>
    </form>
  );
}

function CreateClassification({
  pending,
  error,
  kind,
}: Readonly<{
  pending: boolean;
  error: string | undefined;
  kind: CreateTargetResolutionKind | undefined;
}>) {
  const { t } = useTranslation();
  if (pending) return <Spinner size="sm" />;
  if (error !== undefined)
    return <span className="break-words text-sm text-[var(--color-error)]">{error}</span>;
  if (kind === undefined) return null;
  const label =
    kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH
      ? "chat.worktree.newBranch"
      : kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_EXISTING_BRANCH
        ? "chat.worktree.existingBranch"
        : "chat.worktree.detachedRef";
  return <strong className="break-words text-sm">{t(label)}</strong>;
}
