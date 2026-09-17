import { useTranslation } from "react-i18next";
import {
  DirtyStateKind,
  hasDeletableWorktreeBranch,
  type WorktreeDeletePreview,
  type WorktreeDeleteConfirmationChoice,
} from "@/api";
import { Button, Dialog } from "@/ui";
import { WorktreeCleanliness } from "./WorktreeCleanliness";

export function WorktreeCommandDeleteDialog({
  preview,
  onDismiss,
  onChoice,
}: Readonly<{
  preview: WorktreeDeletePreview;
  onDismiss(): void;
  onChoice(choice: WorktreeDeleteConfirmationChoice): void;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      open
      chrome="title-only"
      width={540}
      style={{ padding: "var(--space-6)" }}
      title={t("chat.worktree.commandDeleteTitle")}
      closeLabel={t("chat.worktree.no")}
      onClose={onDismiss}
    >
      <div className="grid gap-[var(--space-6)]">
        <div className="grid gap-[var(--space-3)]">
          <p className="m-0 break-words">{t("chat.worktree.commandDeleteBody")}</p>
          {preview.cleanliness !== undefined &&
          preview.cleanliness.kind !== DirtyStateKind.DIRTY_STATE_CLEAN ? (
            <WorktreeCleanliness value={preview.cleanliness} />
          ) : null}
        </div>
        <div className="flex flex-wrap justify-end gap-[var(--space-2)]">
          <Button onClick={onDismiss}>{t("chat.worktree.no")}</Button>
          <Button
            variant="danger"
            onClick={() => {
              onChoice("confirm");
            }}
          >
            {t("chat.worktree.yes")}
          </Button>
          {hasDeletableWorktreeBranch(preview) ? (
            <Button
              variant="danger"
              onClick={() => {
                onChoice("confirm_and_branch");
              }}
            >
              {t("chat.worktree.yesAndBranch")}
            </Button>
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}
