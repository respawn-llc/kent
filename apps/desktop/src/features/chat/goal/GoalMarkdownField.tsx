import { useEffect, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { useAnimate } from "motion/react";

import { useTextFieldSubmitShortcutPolicy } from "@/app-facade";
import {
  CollapsibleMarkdownField,
  motionDurationFromCSSVar,
  Spinner,
  fieldActions,
  type MarkdownFieldSubmitIntent,
} from "@/ui";
import { useSpeechField } from "@/shared/speech-input";
import { GoalSaveButton } from "./GoalSidebarParts";

const editorHeight = 220;

export function NewChatGoalField({
  draft,
  editing,
  setDraft,
  setEditing,
  canSave,
  save,
  pending,
  label,
}: Readonly<{
  draft: string;
  editing: boolean;
  setDraft(text: string): void;
  setEditing(value: boolean): void;
  canSave: boolean;
  save(): void;
  pending: boolean;
  label: string;
}>) {
  const speech = useSpeechField({ value: draft, onChange: setDraft });
  const submit = () => {
    if (!speech.busy) save();
  };
  return (
    <div className="flex min-h-0 flex-col gap-[var(--space-3)]">
      <div className="min-h-0 shrink-0">
        <GoalMarkdownField
          editorRef={speech.editorRef}
          editing={editing}
          floatingAction={fieldActions(
            speech.control,
            pending ? (
              <Spinner size="sm" testID="goal-mutation-pending" />
            ) : draft.trim().length === 0 ? undefined : (
              <GoalSaveButton
                disabled={!canSave || speech.busy}
                label={speech.busy ? speech.busyReason : label}
                onClick={submit}
                pending={pending}
              />
            ),
          )}
          expanded
          onChange={setDraft}
          onEdit={() => {
            setEditing(true);
          }}
          onEditingChange={setEditing}
          onExpand={() => undefined}
          submitIntent={{ available: canSave && !speech.busy, onSubmitIntent: submit }}
          value={draft}
        />
      </div>
    </div>
  );
}

export type GoalMarkdownFieldProps = Readonly<{
  editing: boolean;
  editorRef?: RefObject<HTMLTextAreaElement | null>;
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
  editorRef,
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
        editorRef={editorRef}
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
