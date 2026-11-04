import { Trash2 } from "lucide-react";
import { useRef } from "react";
import { useTranslation } from "react-i18next";

import type { WorktreeDeletePreview } from "@/api";
import {
  Button,
  Checkbox,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { WorktreeCleanliness } from "./WorktreeCleanliness";

type Props = Readonly<{
  open: boolean;
  onOpenChange(open: boolean): void;
  cleanliness: WorktreeDeletePreview["cleanliness"] | null;
  confirm(): void;
  includeBranch: boolean;
  canIncludeBranch: boolean;
  disabled?: boolean;
  onIncludeBranchChange(include: boolean): void;
}>;

export function WorktreeDeleteButton(props: Props) {
  const { t } = useTranslation();
  const trigger = useRef<HTMLButtonElement>(null);
  return (
    <Popover open={props.open} onOpenChange={props.onOpenChange}>
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                ref={trigger}
                disabled={props.disabled}
                size="icon-sm"
                variant="ghost"
                aria-label={t("chat.worktree.delete")}
              >
                <Trash2 size={16} />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <TooltipContent>{t("chat.worktree.delete")}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContent
        align="start"
        onOpenAutoFocus={(event) => {
          if (document.activeElement !== trigger.current) event.preventDefault();
        }}
      >
        <WorktreeDeleteContent {...props} />
      </PopoverContent>
    </Popover>
  );
}

function WorktreeDeleteContent({
  cleanliness,
  confirm,
  includeBranch,
  canIncludeBranch,
  onIncludeBranchChange,
}: Props) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-[var(--space-3)]">
      {cleanliness === null ? null : <WorktreeCleanliness value={cleanliness} />}
      <label className="flex items-center gap-[var(--space-2)] text-sm">
        <Checkbox
          checked={includeBranch && canIncludeBranch}
          onCheckedChange={(value) => {
            onIncludeBranchChange(value === true);
          }}
          disabled={!canIncludeBranch}
        />
        {t("chat.worktree.includeBranch")}
      </label>
      <Button variant="danger" onClick={confirm}>
        {t("chat.worktree.confirm")}
      </Button>
    </div>
  );
}
