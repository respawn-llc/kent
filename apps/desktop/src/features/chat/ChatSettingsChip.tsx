import { ComposerIcon } from "./ComposerIcon";
import { useTranslation } from "react-i18next";
import {
  InteractiveChip,
  PopoverTrigger,
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
}>;

export function ChatSettingsChip({ role, model, thinking, fast }: SummaryProps) {
  const { t } = useTranslation();
  const { viewportRef, contentRef, overflowing } = useTextOverflow(model);
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <InteractiveChip variant="ghost" aria-label={t("chatSettings.open")} className="min-w-0">
              <ComposerIcon kind="settings" />
              <span className="shrink-0 whitespace-nowrap">{role}:</span>
              <span
                ref={viewportRef}
                className="min-w-0 truncate font-mono [direction:rtl] [text-align:left]"
              >
                <bdi className="inline-block" dir="ltr" ref={contentRef}>
                  {model}
                </bdi>
              </span>
              {thinking !== null && <span className="shrink-0 font-mono">{thinking}</span>}
              {fast && <ComposerIcon kind="fast" />}
            </InteractiveChip>
          </PopoverTrigger>
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
