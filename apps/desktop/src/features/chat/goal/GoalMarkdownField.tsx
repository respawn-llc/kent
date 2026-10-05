import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { useTextFieldSubmitShortcutPolicy } from "@/app-facade";
import { CollapsibleMarkdownField, type MarkdownFieldSubmitIntent } from "@/ui";

export type GoalMarkdownFieldProps = Readonly<{
  editing: boolean;
  expanded: boolean;
  floatingAction: Awaited<ReactNode> | undefined;
  onChange: (value: string) => void;
  onEdit: () => void;
  onEditingChange: (editing: boolean) => void;
  onExpand: () => void;
  submitIntent?: Omit<MarkdownFieldSubmitIntent, "policy">;
  value: string;
}>;

export function GoalMarkdownField({
  editing,
  expanded,
  floatingAction,
  onChange,
  onEdit,
  onEditingChange,
  onExpand,
  submitIntent,
  value,
}: GoalMarkdownFieldProps) {
  const { t } = useTranslation();
  const submitPolicy = useTextFieldSubmitShortcutPolicy();
  return (
    <CollapsibleMarkdownField
      collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
      disabled={false}
      editorMinHeight={220}
      editing={editing}
      expandLabel={t("app.expand")}
      expanded={expanded}
      floatingAction={floatingAction}
      label={t("chat.goal.objective")}
      onChange={onChange}
      onEdit={onEdit}
      onEditingChange={onEditingChange}
      onExpand={onExpand}
      placeholder=""
      submitIntent={submitIntent === undefined ? undefined : { ...submitIntent, policy: submitPolicy }}
      value={value}
    />
  );
}
