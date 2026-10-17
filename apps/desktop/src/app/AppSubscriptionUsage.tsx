import { useState } from "react";
import { useAtomValue, useAtomSet } from "@effect/atom-react";
import { Gauge } from "lucide-react";
import { useTranslation } from "react-i18next";
import { UsageContent, useSubscriptionUsageModel } from "@/shared/subscription-usage";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";

export function AppSubscriptionUsage() {
  const { t } = useTranslation();
  const model = useSubscriptionUsageModel(null);
  const visible = useAtomValue(model.visible);
  const refresh = useAtomSet(model.open);
  const [open, setOpen] = useState(false);
  if (!visible && !open) return null;
  return (
    <Popover
      open={open}
      onOpenChange={(value) => {
        setOpen(value);
        if (value) refresh();
      }}
    >
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <button
                type="button"
                aria-label={t("subscriptionUsage.title")}
                className={`grid h-6 w-6 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)] ${visible ? "" : "invisible"}`}
              >
                <Gauge size={16} strokeWidth={1.25} />
              </button>
            </PopoverTrigger>
          </TooltipTrigger>
          <TooltipContent>{t("subscriptionUsage.title")}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContent align="end" className="w-80 max-w-[calc(100vw_-_var(--space-5))] text-sm">
        <UsageContent model={model} virtualized />
      </PopoverContent>
    </Popover>
  );
}
