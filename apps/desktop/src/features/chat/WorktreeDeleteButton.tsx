import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { hasDeletableWorktreeBranch, WorktreeError } from "@/api";
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
import { worktreeErrorMessage } from "./worktreeErrorMessage";
import { WorktreeCleanliness } from "./WorktreeCleanliness";

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
  const inlineError = immediateDeleteError(deletion.error, t);
  return (
    <>
      {preview.isPending || deletion.isPending ? <Spinner size="sm" /> : null}
      {preview.isError ? (
        <p className="whitespace-pre-wrap break-words text-sm text-[var(--color-error)]">
          {worktreeErrorMessage(preview.error, t)}
        </p>
      ) : null}
      {cleanliness === undefined ? null : <WorktreeCleanliness value={cleanliness} />}
      {inlineError !== undefined ? (
        <p className="whitespace-pre-wrap break-words text-sm text-[var(--color-error)]">{inlineError}</p>
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

function immediateDeleteError(error: Error | null, t: TFunction) {
  if (error === null) return undefined;
  if (error instanceof WorktreeError && error.detail.kind === "delete_precondition") return undefined;
  return worktreeErrorMessage(error, t);
}
