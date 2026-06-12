import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cx } from "../../ui/classes";

export type DetailTab = "comments" | "activity";

export function TaskTabs({
  activityCount,
  commentCount,
  selected,
  onSelect,
}: Readonly<{
  activityCount: number;
  commentCount: number;
  selected: DetailTab;
  onSelect: (tab: DetailTab) => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap gap-[var(--space-2)]" role="tablist">
      <TabButton
        count={commentCount}
        selected={selected === "comments"}
        onClick={() => {
          onSelect("comments");
        }}
      >
        {t("task.comments")}
      </TabButton>
      <TabButton
        selected={selected === "activity"}
        onClick={() => {
          onSelect("activity");
        }}
      >
        {t("task.activity")}
      </TabButton>
      <span className="sr-only">{t("task.activityCount", { count: activityCount })}</span>
    </div>
  );
}

function TabButton({
  children,
  count,
  selected,
  onClick,
}: Readonly<{ children: ReactNode; count?: number; selected: boolean; onClick: () => void }>) {
  return (
    <button
      aria-selected={selected}
      className={cx(
        "inline-flex items-center gap-[var(--space-2)] rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[var(--space-3)] py-[var(--space-2)] text-[var(--color-on-island)]",
        selected && "border-[var(--color-primary)] text-[var(--color-primary)]",
      )}
      onClick={onClick}
      role="tab"
      type="button"
    >
      {children}
      {count !== undefined ? <span className="text-xs text-[var(--color-muted)]">{count}</span> : null}
    </button>
  );
}
