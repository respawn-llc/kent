import { ComposerIcon } from "../ComposerIcon";
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
      variant="ghost"
      aria-label={t("chat.goal.objective")}
      className="shrink-0 whitespace-nowrap"
      onClick={onActivate}
      size="default"
      tone={goal.goal?.status === "active" ? "primary" : "neutral"}
      data-state={goal.goal?.status === "active" ? "active" : "neutral"}
    >
      <ComposerIcon kind="goal" />
      <span>{t("chat.goal.objective")}</span>
    </InteractiveChip>
  );
}
