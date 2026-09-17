import { useAtomValue } from "@effect/atom-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { CreateTargetResolutionKind, errorMessage } from "@/api";
import {
  replaceWorktreeListRead,
  useAppServices,
  usePublishSidebarHeaderAction,
  useStatusController,
  useTextFieldSubmitShortcut,
  worktreeListQueryOptions,
  type SidebarPageNavigator,
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
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { createWorktreeCreate, useWorktreeCreate } from "./WorktreeCreate";
import type { createWorktreeActions } from "./WorktreeActions";

type Props = Readonly<{
  sessionID: string;
  navigator: SidebarPageNavigator;
  actions: ReturnType<typeof createWorktreeActions>;
}>;

export function WorktreeCreateForm(props: Props) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const query = useQuery(worktreeListQueryOptions(api, props.sessionID));
  const header = useMemo(
    () => (
      <IconTooltipButton
        label={t("chat.worktree.back")}
        onClick={() =>
          props.navigator.replace({ kind: "worktree", page: "list", sessionID: props.sessionID })
        }
      >
        <ArrowLeft size={16} />
      </IconTooltipButton>
    ),
    [props.navigator, props.sessionID, t],
  );
  usePublishSidebarHeaderAction(header);
  useEffect(() => {
    const back = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      event.preventDefault();
      props.navigator.replace({ kind: "worktree", page: "list", sessionID: props.sessionID });
    };
    document.addEventListener("keydown", back);
    return () => {
      document.removeEventListener("keydown", back);
    };
  }, [props.navigator, props.sessionID]);
  if (query.data !== undefined) return <CreateFields {...props} suggestion={query.data.branchSuggestion} />;
  if (query.isError)
    return (
      <ErrorState
        fullPage={false}
        title={t("states.error")}
        body={errorMessage(query.error)}
        retryLabel={t("app.retry")}
        onRetry={() => {
          void replaceWorktreeListRead(client, api, props.sessionID);
        }}
      />
    );
  return <LoadingState fullPage={false} appearanceDelayMs={0} />;
}

function CreateFields({
  sessionID,
  navigator,
  actions,
  suggestion,
}: Props & Readonly<{ suggestion: string | undefined }>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const id = useId();
  const [model] = useState(() =>
    createWorktreeCreate({
      client,
      api,
      sessionID,
      navigator,
      suggestion,
      push,
      t,
      refreshOpenWorktreeList: actions.refreshOpenWorktreeList,
      submitSwitch: actions.submitSwitch,
    }),
  );
  const { editTarget, editBase, submit } = useWorktreeCreate(model);
  const state = useAtomValue(model.state);
  const switching = useAtomValue(actions.requestPending);
  const pending = state.pending || switching;
  const submitShortcut = useTextFieldSubmitShortcut({ kind: "form", available: !pending });
  const isNew =
    state.classification === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH;
  return (
    <TooltipProvider>
      <Tooltip {...(pending ? {} : { open: false })}>
        <TooltipTrigger asChild>
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
            {state.formError === undefined ? null : (
              <p className="break-words text-sm text-[var(--color-error)]">{state.formError}</p>
            )}
            <Button type="submit" variant="primary" disabled={pending}>
              {pending ? <Spinner size="sm" /> : t("chat.worktree.createSubmit")}
            </Button>
          </form>
        </TooltipTrigger>
        <TooltipContent>{t("chat.worktree.requestPending")}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
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
