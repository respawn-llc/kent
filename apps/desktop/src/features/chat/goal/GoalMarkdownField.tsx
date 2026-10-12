import { useEffect, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useAnimate } from "motion/react";

import { useTextFieldSubmitShortcutPolicy } from "@/app-facade";
import { CollapsibleMarkdownField, motionDurationFromCSSVar, type MarkdownFieldSubmitIntent } from "@/ui";

const editorHeight = 220;

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
  const [scope, animate] = useAnimate<HTMLDivElement>();
  useEffect(() => {
    const animation = animate(
      scope.current,
      { opacity: [0, 1] },
      { duration: motionDurationFromCSSVar("--motion-fast", 140) / 1000 },
    );
    return () => {
      animation.stop();
    };
  }, [animate, editing, scope]);
  return (
    <div
      ref={scope}
      className="grid min-w-0 [&_[data-slot=markdown-field-read-root]]:h-full"
      style={{ minHeight: editorHeight, height: editing ? editorHeight : undefined }}
    >
      <CollapsibleMarkdownField
        collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
        disabled={false}
        editorMinHeight={editorHeight}
        editing={editing}
        expandLabel={t("app.expand")}
        expanded={expanded}
        floatingAction={floatingAction}
        label={t("chat.goal.objective")}
        onChange={onChange}
        onEdit={onEdit}
        onEditingChange={onEditingChange}
        onExpand={onExpand}
        placeholder={t("chat.goal.guidance")}
        submitIntent={submitIntent === undefined ? undefined : { ...submitIntent, policy: submitPolicy }}
        surfaceRadius="l"
        value={value}
      />
    </div>
  );
}
