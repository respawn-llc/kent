import { ComposerIcon } from "./ComposerIcon";
import type { ComponentPropsWithRef } from "react";
import { useTranslation } from "react-i18next";
import {
  InteractiveChip,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  useTextOverflow,
} from "@/ui";

type SummaryProps = Readonly<{
  role: string;
  model: string;
  thinking: string | null;
  fast: boolean;
  triggerProps: ComponentPropsWithRef<"button">;
}>;

export function ChatSettingsChip({ role, model, thinking, fast, triggerProps }: SummaryProps) {
  const { t } = useTranslation();
  const { viewportRef, contentRef, overflowing } = useTextOverflow(model);
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <InteractiveChip
            {...triggerProps}
            variant="ghost"
            aria-label={t("chatSettings.open")}
            className="min-w-0"
          >
            <ComposerIcon kind="settings" />
            <span className="shrink-0 whitespace-nowrap">{role}:</span>
            <span ref={viewportRef} className="min-w-0 truncate font-mono [direction:rtl] [text-align:left]">
              <bdi className="inline-block" dir="ltr" ref={contentRef}>
                {model}
              </bdi>
            </span>
            {thinking !== null && <span className="shrink-0 font-mono">{thinking}</span>}
            {fast && <ComposerIcon kind="fast" />}
          </InteractiveChip>
        </TooltipTrigger>
        {overflowing && (
          <TooltipContent>
            {role}: {model}
            {thinking === null ? null : ` ${thinking}`}
          </TooltipContent>
        )}
      </Tooltip>
    </TooltipProvider>
  );
}
