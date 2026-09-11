import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { CollapsibleMarkdownField } from "@/ui";

export type GoalMarkdownFieldProps = Readonly<{
  editing: boolean;
  error: string | undefined;
  expanded: boolean;
  floatingAction: ReactNode | undefined;
  onChange: (value: string) => void;
  onEdit: () => void;
  onEditingChange: (editing: boolean) => void;
  onExpand: () => void;
  value: string;
}>;

export function GoalMarkdownField({
  editing,
  error,
  expanded,
  floatingAction,
  onChange,
  onEdit,
  onEditingChange,
  onExpand,
  value,
}: GoalMarkdownFieldProps) {
  const { t } = useTranslation();
  return (
    <CollapsibleMarkdownField
      collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
      disabled={false}
      editorMinHeight={220}
      editing={editing}
      error={error}
      expandLabel={t("app.expand")}
      expanded={expanded}
      floatingAction={floatingAction}
      label={t("chat.goal.objective")}
      onChange={onChange}
      onEdit={onEdit}
      onEditingChange={onEditingChange}
      onExpand={onExpand}
      placeholder={t("chat.goal.objectivePlaceholder")}
      value={value}
    />
  );
}
