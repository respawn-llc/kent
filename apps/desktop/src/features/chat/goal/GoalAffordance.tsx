import { Target } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { ChatGoalFact } from "@/api";
import { InteractiveChip } from "@/ui";

export type GoalAffordanceProps = Readonly<{
  goal: ChatGoalFact;
  onActivate: () => void;
}>;

export function GoalAffordance({ goal, onActivate }: GoalAffordanceProps) {
  const { t } = useTranslation();
  return (
    <InteractiveChip
      aria-label={t("chat.goal.objective")}
      className="self-start justify-self-start min-h-9 px-[var(--space-3)]"
      onClick={onActivate}
      size="default"
      tone={goal.goal?.status === "active" ? "primary" : "neutral"}
      data-state={goal.goal?.status === "active" ? "active" : "neutral"}
    >
      <Target aria-hidden="true" size={17} strokeWidth={1.7} />
      <span>{t("chat.goal.objective")}</span>
    </InteractiveChip>
  );
}
