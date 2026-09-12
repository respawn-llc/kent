import { Check, CircleDot, Pause, PauseCircle, Play, RotateCcw, Save, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ReactElement } from "react";

import type { ChatGoalStatus } from "@/api";
import {
  Button,
  DisabledInteractionGuard,
  IslandSurface,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { cx } from "@/ui";
import { formatGoalAge } from "./goalFormat";

export type GoalActionsModel = Readonly<{
  actionsDisabled: boolean;
  clear: () => void;
  displayedStatus: ChatGoalStatus | null;
  runLifecycle: () => void;
  t: ReturnType<typeof useTranslation>["t"];
  unavailable: boolean;
}>;

export function GoalActions({ model }: Readonly<{ model: GoalActionsModel }>) {
  const lifecycle = goalLifecycleAction(model.displayedStatus);
  const lifecycleUnavailable = lifecycle !== "pause" && model.unavailable;
  const lifecycleButton = (
    <Button
      disabled={model.actionsDisabled || lifecycleUnavailable}
      onClick={model.runLifecycle}
      variant={lifecycle === "pause" ? "secondary" : lifecycle === "resume" ? "primary" : "primary-outline"}
    >
      {lifecycle === "pause" ? (
        <Pause size={15} />
      ) : lifecycle === "resume" ? (
        <Play size={15} />
      ) : (
        <RotateCcw size={15} />
      )}
      {model.t(`chat.goal.${lifecycle}`)}
    </Button>
  );
  return (
    <div className="flex flex-wrap items-center gap-[var(--space-2)]" data-testid="goal-actions">
      <DisabledInteractionGuard
        disabled={lifecycleUnavailable}
        reason={model.t("chat.goal.unavailableForAgent")}
      >
        {lifecycleButton}
      </DisabledInteractionGuard>
      <Button disabled={model.actionsDisabled} onClick={model.clear} variant="danger">
        <Trash2 size={15} />
        {model.t("chat.goal.clear")}
      </Button>
    </div>
  );
}

export function GoalMetadata({
  createdAt,
  now,
  status,
}: Readonly<{ createdAt: string | null; now: number; status: ChatGoalStatus }>) {
  const { t } = useTranslation();
  const icon =
    status === "active" ? (
      <CircleDot size={16} />
    ) : status === "paused" ? (
      <PauseCircle size={16} />
    ) : (
      <Check size={16} />
    );
  return (
    <IslandSurface
      aria-label={t(`chat.goal.${status}`)}
      className="grid gap-[var(--space-1)] p-[var(--space-3)]"
      level={1}
    >
      <div
        className={cx(
          "flex items-center gap-[var(--space-2)] font-medium",
          status === "active"
            ? "text-[var(--color-primary)]"
            : status === "paused"
              ? "text-[var(--color-warning)]"
              : "text-[var(--color-success)]",
        )}
      >
        {icon}
        <span>{t(`chat.goal.${status}`)}</span>
      </div>
      {createdAt === null ? null : (
        <span className="text-sm text-[var(--color-muted)]">
          {t("chat.goal.setAt")}{" "}
          <time data-testid="goal-set-time" dateTime={createdAt}>
            {new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
              new Date(createdAt),
            )}
          </time>{" "}
          {t("chat.goal.age", { age: formatGoalAge(createdAt, now) })}
        </span>
      )}
    </IslandSurface>
  );
}

export function GoalSaveButton({
  disabled,
  label,
  onClick,
  pending,
}: Readonly<{
  disabled: boolean;
  label: string;
  onClick: () => void;
  pending: boolean;
}>): ReactElement {
  const button = (
    <Button
      aria-label={label}
      data-testid="goal-save"
      disabled={disabled || pending}
      onClick={onClick}
      size="icon"
      variant="primary"
    >
      <Save aria-hidden="true" size={16} />
    </Button>
  );
  return disabled ? (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    button
  );
}

export function goalLifecycleAction(status: ChatGoalStatus | null): "pause" | "resume" | "reopen" {
  if (status === "active") return "pause";
  if (status === "paused") return "resume";
  return "reopen";
}
