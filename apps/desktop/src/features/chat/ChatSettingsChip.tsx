import { Settings, Zap } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  InteractiveChip,
  PopoverTrigger,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";

type SummaryProps = Readonly<{
  role: string;
  model: string;
  thinking: string | null;
  fast: boolean;
}>;

export function ChatSettingsChip({ role, model, thinking, fast }: SummaryProps) {
  const { t } = useTranslation();
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <InteractiveChip aria-label={t("chatSettings.open")} className="min-w-0">
              <Settings className="shrink-0" size={14} />
              <span className="shrink-0 whitespace-nowrap">{role}:</span>
              <span className="min-w-0 truncate font-mono text-[var(--color-muted)] [direction:rtl] [text-align:left]">
                <bdi dir="ltr">{model}</bdi>
              </span>
              {thinking !== null && (
                <span className="shrink-0 font-mono text-[var(--color-muted)]">{thinking}</span>
              )}
              {fast && <Zap className="shrink-0 text-[var(--color-secondary)]" size={14} />}
            </InteractiveChip>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>
          {role}: {model}
          {thinking === null ? null : ` ${thinking}`}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
