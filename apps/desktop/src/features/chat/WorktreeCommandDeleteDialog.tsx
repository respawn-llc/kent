import { useTranslation } from "react-i18next";
import {
  DirtyStateKind,
  hasDeletableWorktreeBranch,
  type WorktreeDeletePreview,
  type WorktreeDeleteConfirmationChoice,
} from "@/api";
import { Button, Dialog, Spinner, Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/ui";
import { WorktreeCleanliness } from "./WorktreeCleanliness";

export function WorktreeCommandDeleteDialog({
  preview,
  pending,
  onDismiss,
  onChoice,
}: Readonly<{
  preview: WorktreeDeletePreview;
  pending: boolean;
  onDismiss(): void;
  onChoice(choice: WorktreeDeleteConfirmationChoice): void;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      open
      chrome="title-only"
      title={t("chat.worktree.commandDeleteTitle")}
      closeLabel={t("chat.worktree.no")}
      onClose={onDismiss}
    >
      <p>{t("chat.worktree.commandDeleteBody")}</p>
      {preview.cleanliness !== undefined && preview.cleanliness.kind !== DirtyStateKind.DIRTY_STATE_CLEAN ? (
        <WorktreeCleanliness value={preview.cleanliness} />
      ) : null}
      {pending ? <Spinner size="sm" /> : null}
      <div className="flex flex-wrap gap-[var(--space-2)]">
        <Button onClick={onDismiss}>{t("chat.worktree.no")}</Button>
        <TooltipProvider>
          <Tooltip {...(pending ? {} : { open: false })}>
            <TooltipTrigger asChild>
              <div className="flex flex-wrap gap-[var(--space-2)]">
                <Button
                  variant="danger"
                  disabled={pending}
                  onClick={() => {
                    onChoice("confirm");
                  }}
                >
                  {t("chat.worktree.yes")}
                </Button>
                {hasDeletableWorktreeBranch(preview) ? (
                  <Button
                    variant="danger"
                    disabled={pending}
                    onClick={() => {
                      onChoice("confirm_and_branch");
                    }}
                  >
                    {t("chat.worktree.yesAndBranch")}
                  </Button>
                ) : null}
              </div>
            </TooltipTrigger>
            <TooltipContent>{t("chat.worktree.requestPending")}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </Dialog>
  );
}
