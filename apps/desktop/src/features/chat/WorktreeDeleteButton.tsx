import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  DirtyStateKind,
  errorMessage,
  hasDeletableWorktreeBranch,
  WorktreeError,
  type WorktreeDeletePreview,
} from "@/api";
import { useAppServices, useStatusController } from "@/app-facade";
import {
  Button,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { createWorktreeDelete, useWorktreeDelete } from "./WorktreeDelete";

type Props = Readonly<{
  sessionID: string;
  selector: string;
  refreshOpenWorktreeList(sessionID: string): void;
}>;

export function WorktreeDeleteButton(props: Props) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const close = useCallback(() => {
    setOpen(false);
  }, []);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button size="icon" variant="ghost" aria-label={t("chat.worktree.delete")}>
                <Trash2 size={16} />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <TooltipContent>{t("chat.worktree.delete")}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContent
        onEscapeKeyDown={(event) => {
          event.stopPropagation();
        }}
      >
        {open ? <WorktreeDeleteContent {...props} close={close} /> : null}
      </PopoverContent>
    </Popover>
  );
}

function WorktreeDeleteContent({ close, ...props }: Props & Readonly<{ close(): void }>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const [model] = useState(() => createWorktreeDelete({ ...props, client, api, close, push, t }));
  const { confirm } = useWorktreeDelete(model);
  const preview = useAtomValue(model.preview);
  const deletion = useAtomValue(model.deletion);
  const cleanliness = preview.data?.cleanliness;
  const inlineError = immediateDeleteError(deletion.error);
  return (
    <>
      {preview.isIdle || preview.isPending || deletion.isPending ? <Spinner size="sm" /> : null}
      {preview.isError ? (
        <p className="break-words text-sm text-[var(--color-error)]">{errorMessage(preview.error)}</p>
      ) : null}
      {cleanliness === undefined ? null : <CleanlinessFacts value={cleanliness} />}
      {inlineError !== undefined ? (
        <p className="break-words text-sm text-[var(--color-error)]">{inlineError}</p>
      ) : null}
      {preview.isSuccess ? (
        <TooltipProvider>
          <Tooltip {...(deletion.isPending ? {} : { open: false })}>
            <TooltipTrigger asChild>
              <div className="flex flex-wrap gap-[var(--space-2)]">
                <Button
                  disabled={deletion.isPending}
                  variant="danger"
                  onClick={() => {
                    confirm("confirm");
                  }}
                >
                  {t("chat.worktree.confirm")}
                </Button>
                {hasDeletableWorktreeBranch(preview.data) ? (
                  <Button
                    disabled={deletion.isPending}
                    variant="danger"
                    onClick={() => {
                      confirm("confirm_and_branch");
                    }}
                  >
                    {t("chat.worktree.confirmBranch")}
                  </Button>
                ) : null}
              </div>
            </TooltipTrigger>
            <TooltipContent>{t("chat.worktree.requestPending")}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : null}
      <Button onClick={close}>{t("app.close")}</Button>
    </>
  );
}

function immediateDeleteError(error: Error | null) {
  if (error === null) return undefined;
  if (error instanceof WorktreeError && error.detail.kind === "delete_precondition") return undefined;
  return errorMessage(error);
}

function CleanlinessFacts({ value }: Readonly<{ value: NonNullable<WorktreeDeletePreview["cleanliness"]> }>) {
  const { t } = useTranslation();
  const clean = value.kind === DirtyStateKind.DIRTY_STATE_CLEAN;
  const label = clean
    ? t("chat.worktree.clean")
    : value.kind === DirtyStateKind.DIRTY_STATE_DIRTY
      ? t("chat.worktree.dirty", { count: value.dirtyFileCount })
      : t("chat.worktree.unknown", { diagnostic: value.unknownCause });
  return (
    <p className={clean ? "break-words text-sm" : "break-words text-sm text-[var(--color-warning)]"}>
      {label}
    </p>
  );
}
