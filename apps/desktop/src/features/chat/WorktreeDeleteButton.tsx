import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  hasDeletableWorktreeBranch,
  type WorktreeDeletePreview,
  type WorktreeDeleteConfirmationChoice,
} from "@/api";
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
import { WorktreeCleanliness } from "./WorktreeCleanliness";

type Props = Readonly<{
  open: boolean;
  onOpenChange(open: boolean): void;
  preview: WorktreeDeletePreview | undefined;
  pending: boolean;
  confirm(choice: WorktreeDeleteConfirmationChoice): void;
}>;

export function WorktreeDeleteButton(props: Props) {
  const { t } = useTranslation();
  return (
    <Popover open={props.open} onOpenChange={props.onOpenChange}>
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
        {props.open ? <WorktreeDeleteContent {...props} /> : null}
      </PopoverContent>
    </Popover>
  );
}

function WorktreeDeleteContent({ preview, pending, confirm, onOpenChange }: Props) {
  const { t } = useTranslation();
  const cleanliness = preview?.cleanliness;
  return (
    <>
      {preview === undefined || pending ? <Spinner size="sm" /> : null}
      {cleanliness === undefined ? null : <WorktreeCleanliness value={cleanliness} />}
      {preview !== undefined ? (
        <TooltipProvider>
          <Tooltip {...(pending ? {} : { open: false })}>
            <TooltipTrigger asChild>
              <div className="flex flex-wrap gap-[var(--space-2)]">
                <Button
                  disabled={pending}
                  variant="danger"
                  onClick={() => {
                    confirm("confirm");
                  }}
                >
                  {t("chat.worktree.confirm")}
                </Button>
                {hasDeletableWorktreeBranch(preview) ? (
                  <Button
                    disabled={pending}
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
      <Button
        onClick={() => {
          onOpenChange(false);
        }}
      >
        {t("app.close")}
      </Button>
    </>
  );
}
